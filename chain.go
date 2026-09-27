package fat

import (
	"encoding/binary"
	"io"
)

// maxCluster is the highest valid cluster number (clusters are 2..N+1).
func (v *Volume) maxCluster() uint32 { return v.clusterCount + 1 }

// fatEntry reads the raw FAT entry for cluster n.
func (v *Volume) fatEntry(n uint32) (uint32, error) {
	if n < 2 || n > v.maxCluster() {
		return 0, errShortRead
	}
	base := uint64(v.fatStartSec) * uint64(v.bytesPerSec)
	switch v.typ {
	case FAT12:
		off := base + uint64(n) + uint64(n)/2
		var b [2]byte
		if err := v.readAt(b[:], off); err != nil {
			return 0, err
		}
		val := uint32(binary.LittleEndian.Uint16(b[:]))
		if n&1 != 0 {
			val >>= 4
		}
		return val & 0x0FFF, nil
	case FAT16:
		var b [2]byte
		if err := v.readAt(b[:], base+uint64(n)*2); err != nil {
			return 0, err
		}
		return uint32(binary.LittleEndian.Uint16(b[:])), nil
	default: // FAT32
		var b [4]byte
		if err := v.readAt(b[:], base+uint64(n)*4); err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint32(b[:]) & 0x0FFFFFFF, nil
	}
}

func (v *Volume) badClusterValue() uint32 {
	switch v.typ {
	case FAT12:
		return 0x0FF7
	case FAT16:
		return 0xFFF7
	default:
		return 0x0FFFFFF7
	}
}

func (v *Volume) eocValue() uint32 {
	switch v.typ {
	case FAT12:
		return 0x0FF8
	case FAT16:
		return 0xFFF8
	default:
		return 0x0FFFFFF8
	}
}

// nextCluster resolves the successor of cluster c in a chain.
// done is true on a clean end-of-chain; a is set on a structural problem.
func (v *Volume) nextCluster(c uint32) (next uint32, a Anomaly, done bool) {
	e, err := v.fatEntry(c)
	if err != nil {
		return 0, AnomalyTruncated, true
	}
	switch {
	case e == 0:
		return 0, AnomalyChainFree, true
	case e == v.badClusterValue():
		return 0, AnomalyChainBad, true
	case e >= v.eocValue():
		return 0, "", true
	case e < 2 || e > v.maxCluster():
		return 0, AnomalyChainOutOfRange, true
	}
	return e, "", false
}

// walkChain emits each cluster of the chain starting at first to fn, until
// fn returns false, the chain ends, or a problem occurs. Cycles are
// detected with Brent's algorithm (constant memory). It returns the
// terminating anomaly, or "" on clean termination / fn stopping.
func (v *Volume) walkChain(first uint32, fn func(n uint32) bool) Anomaly {
	if first < 2 || first > v.maxCluster() {
		return AnomalyChainOutOfRange
	}
	tort := first
	power, lam := uint64(1), uint64(0)
	cur := first
	for {
		if !fn(cur) {
			return ""
		}
		if lam > 0 && cur == tort {
			return AnomalyChainLoop
		}
		if lam == power {
			tort = cur
			power *= 2
			lam = 0
		}
		next, a, done := v.nextCluster(cur)
		if a != "" {
			return a
		}
		if done {
			return ""
		}
		cur = next
		lam++
	}
}

// chainNeeded returns how many clusters a file of size bytes occupies.
func (v *Volume) chainNeeded(size uint32) uint64 {
	if size == 0 {
		return 0
	}
	return (uint64(size) + uint64(v.clusterSize) - 1) / uint64(v.clusterSize)
}

// analyzeChain inspects the chain of an allocated entry and reports
// anomalies relative to the entry's size field.
func (v *Volume) analyzeChain(first uint32, size uint32) []Anomaly {
	needed := v.chainNeeded(size)
	if needed == 0 {
		return nil
	}
	var anomalies []Anomaly
	add := func(a Anomaly) {
		for _, x := range anomalies {
			if x == a {
				return
			}
		}
		anomalies = append(anomalies, a)
	}
	var count uint64
	term := v.walkChain(first, func(n uint32) bool {
		count++
		return count <= needed
	})
	switch term {
	case "":
		// Either clean EOC or fn stopped because count exceeded needed.
		if count < needed {
			add(AnomalyChainShort)
		} else {
			// Stopped after needed+? clusters: count>needed means chain
			// continues past what size requires.
			if count > needed {
				add(AnomalyChainLong)
			}
		}
	case AnomalyChainLoop, AnomalyChainFree, AnomalyChainBad,
		AnomalyChainOutOfRange, AnomalyTruncated:
		add(term)
		if count < needed {
			add(AnomalyChainShort)
		}
	}
	return anomalies
}

// recoveryStatus classifies a deleted file's recoverability.
func (v *Volume) recoveryStatus(first uint32, size uint32) Recovery {
	needed := v.chainNeeded(size)
	if needed == 0 {
		return RecoveryFull
	}
	if first < 2 || uint64(first) > uint64(v.maxCluster()) {
		return RecoveryUnrecoverable
	}
	if uint64(first)+needed-1 > uint64(v.maxCluster()) {
		return RecoveryUnrecoverable
	}
	status := RecoveryFull
	for i := uint64(0); i < needed; i++ {
		e, err := v.fatEntry(first + uint32(i))
		if err != nil {
			return RecoveryUnrecoverable
		}
		if e != 0 {
			status = RecoveryPartial
		}
	}
	return status
}

// readFile streams the entry's content to w.
func (v *Volume) readFile(e *Entry, w io.Writer) error {
	if e.Size == 0 {
		return nil
	}
	if e.Deleted {
		return v.readDeleted(e, w)
	}
	remaining := uint64(e.Size)
	if e.FirstCluster < 2 || e.FirstCluster > v.maxCluster() {
		return nil
	}
	var werr error
	v.walkChain(e.FirstCluster, func(n uint32) bool {
		if remaining == 0 {
			return false
		}
		chunk := uint64(v.clusterSize)
		if chunk > remaining {
			chunk = remaining
		}
		if err := v.streamCluster(n, chunk, w); err != nil {
			werr = err
			return false
		}
		remaining -= chunk
		return true
	})
	return werr
}

// readDeleted recovers a deleted file assuming contiguous allocation from
// the first cluster, capped by the size field and the volume end.
func (v *Volume) readDeleted(e *Entry, w io.Writer) error {
	needed := v.chainNeeded(e.Size)
	if needed == 0 {
		return nil
	}
	first := uint64(e.FirstCluster)
	if first < 2 || first > uint64(v.maxCluster()) {
		return nil
	}
	avail := uint64(v.maxCluster()) - first + 1
	if needed > avail {
		needed = avail
	}
	remaining := uint64(e.Size)
	for i := uint64(0); i < needed && remaining > 0; i++ {
		chunk := uint64(v.clusterSize)
		if chunk > remaining {
			chunk = remaining
		}
		if err := v.streamCluster(uint32(first)+uint32(i), chunk, w); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}

// streamCluster writes up to n bytes of cluster c to w. A short read
// (truncated image) stops the stream without error beyond what was read.
func (v *Volume) streamCluster(c uint32, n uint64, w io.Writer) error {
	off := v.clusterOffset(c)
	buf := make([]byte, min(uint64(v.clusterSize), n))
	read := 0
	for read < len(buf) {
		m, err := v.r.ReadAt(buf[read:], int64(off)+int64(read))
		read += m
		if err != nil {
			break
		}
	}
	_, werr := w.Write(buf[:read])
	return werr
}
