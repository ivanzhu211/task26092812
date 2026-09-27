package fat

const maxDepth = 128

type pendingLFN struct {
	deleted bool
	ord     byte
	sum     byte
	chars   [13]uint16
}

type parseState struct {
	pending []pendingLFN
}

type walker struct {
	v         *Volume
	fn        func(*Entry) error
	ancestors map[uint32]bool
}

func (v *Volume) walk(fn func(*Entry) error) error {
	w := &walker{v: v, fn: fn, ancestors: map[uint32]bool{}}
	if v.typ == FAT32 {
		w.ancestors[v.rootClus] = true
		defer delete(w.ancestors, v.rootClus)
		return w.walkChainDir(v.rootClus, "/", false, 0)
	}
	off := v.rootDirSec * uint64(v.bytesPerSec)
	size := uint64(v.rootDirSecs) * uint64(v.bytesPerSec)
	return w.walkRegionDir(off, size, "/", false, 0)
}

// walkRegionDir scans a fixed-size directory region (FAT12/16 root dir).
func (w *walker) walkRegionDir(off, size uint64, path string, forceDeleted bool, depth int) error {
	st := &parseState{}
	buf := make([]byte, w.v.bytesPerSec)
	for pos := uint64(0); pos < size; pos += uint64(len(buf)) {
		if err := w.v.readAt(buf, off+pos); err != nil {
			return nil // truncated image: stop this directory
		}
		stop, err := w.processDirBytes(buf, path, forceDeleted, depth, st)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return nil
}

// walkChainDir scans a directory stored as a cluster chain.
func (w *walker) walkChainDir(first uint32, path string, forceDeleted bool, depth int) error {
	st := &parseState{}
	buf := make([]byte, w.v.clusterSize)
	var stop bool
	var err error
	w.v.walkChain(first, func(n uint32) bool {
		if e := w.v.readAt(buf, w.v.clusterOffset(n)); e != nil {
			return false // truncated image: stop this directory
		}
		stop, err = w.processDirBytes(buf, path, forceDeleted, depth, st)
		return !stop && err == nil
	})
	return err
}

// walkSingleCluster scans only the first cluster of a deleted directory.
func (w *walker) walkSingleCluster(first uint32, path string, depth int) error {
	st := &parseState{}
	buf := make([]byte, w.v.clusterSize)
	if err := w.v.readAt(buf, w.v.clusterOffset(first)); err != nil {
		return nil
	}
	_, err := w.processDirBytes(buf, path, true, depth, st)
	return err
}

// processDirBytes handles one buffer of directory data. stop is true once
// a 0x00 first byte is seen: no more entries are produced by this directory.
func (w *walker) processDirBytes(buf []byte, path string, forceDeleted bool, depth int, st *parseState) (bool, error) {
	for off := 0; off+32 <= len(buf); off += 32 {
		stop, err := w.handleEntry(buf[off:off+32], path, forceDeleted, depth, st)
		if err != nil {
			return false, err
		}
		if stop {
			return true, nil
		}
	}
	return false, nil
}

func (w *walker) handleEntry(ent []byte, path string, forceDeleted bool, depth int, st *parseState) (bool, error) {
	b0 := ent[0]
	if b0 == 0x00 {
		return true, nil
	}
	attr := ent[11]
	if attr&0x0F == attrLFN {
		st.pending = append(st.pending, pendingLFN{
			deleted: b0 == 0xE5,
			ord:     b0,
			sum:     ent[13],
			chars:   lfnChars(ent),
		})
		return false, nil
	}
	if attr&attrVolumeID != 0 {
		st.pending = nil
		return false, nil
	}
	if b0 == '.' {
		st.pending = nil
		return false, nil
	}

	e := &Entry{Deleted: forceDeleted || b0 == 0xE5, Recovery: RecoveryNA}
	parseShortFields(ent, e)

	var longName string
	var firstByte byte
	if b0 == 0xE5 {
		longName, firstByte = matchDeletedLFN(st.pending, ent)
		if firstByte == 0 {
			firstByte = '_'
		}
	} else {
		longName = matchLiveLFN(st.pending, ent)
		if b0 == 0x05 {
			firstByte = 0xE5
		}
	}
	st.pending = nil
	e.LongName = longName
	e.ShortName = formatShortName(ent[0:11], ent[12], firstByte)

	name := longName
	if name == "" {
		name = e.ShortName
	}
	e.Path = path + name

	isDir := attr&attrDir != 0
	descend := false
	deletedDir := false
	if isDir {
		if e.Deleted {
			if w.deletedDirOK(e.FirstCluster) && !w.ancestors[e.FirstCluster] && depth < maxDepth {
				descend = true
				deletedDir = true
			}
		} else {
			switch {
			case w.ancestors[e.FirstCluster]:
				e.Anomalies = append(e.Anomalies, AnomalyDirCycle)
			case depth >= maxDepth:
				e.Anomalies = append(e.Anomalies, AnomalyDepthLimit)
			default:
				descend = true
			}
		}
	} else {
		if e.Deleted {
			e.Recovery = w.v.recoveryStatus(e.FirstCluster, e.Size)
		} else {
			e.Anomalies = append(e.Anomalies, w.v.analyzeChain(e.FirstCluster, e.Size)...)
		}
	}

	if err := w.fn(e); err != nil {
		return false, err
	}

	if descend {
		childPath := e.Path + "/"
		w.ancestors[e.FirstCluster] = true
		defer delete(w.ancestors, e.FirstCluster)
		if deletedDir {
			return false, w.walkSingleCluster(e.FirstCluster, childPath, depth+1)
		}
		return false, w.walkChainDir(e.FirstCluster, childPath, false, depth+1)
	}
	return false, nil
}

// deletedDirOK reports whether a deleted directory is safe to enter: its
// first cluster is free and begins with "." and ".." entries pointing to
// itself and its parent.
func (w *walker) deletedDirOK(first uint32) bool {
	v := w.v
	if first < 2 || first > v.maxCluster() {
		return false
	}
	if e, err := v.fatEntry(first); err != nil || e != 0 {
		return false
	}
	buf := make([]byte, 64)
	if err := v.readAt(buf, v.clusterOffset(first)); err != nil {
		return false
	}
	dot, dotdot := buf[:32], buf[32:64]
	if dot[0] != '.' || dot[1] != ' ' || dot[11]&attrDir == 0 {
		return false
	}
	if dotdot[0] != '.' || dotdot[1] != '.' || dotdot[2] != ' ' || dotdot[11]&attrDir == 0 {
		return false
	}
	self := uint32(dot[26]) | uint32(dot[27])<<8 | uint32(dot[20])<<16 | uint32(dot[21])<<24
	return self == first
}

// matchLiveLFN validates pending LFN entries against a live short entry:
// they must be undeleted, immediately preceding, sequentially numbered and
// checksum-matching. Returns the long name or "".
func matchLiveLFN(pending []pendingLFN, ent []byte) string {
	k := len(pending)
	if k == 0 {
		return ""
	}
	sum := lfnChecksum(ent[0:11])
	for i, p := range pending {
		if p.deleted || p.sum != sum {
			return ""
		}
		if p.ord&0x1F != byte(k-i) {
			return ""
		}
		if i == 0 && p.ord&0x40 == 0 {
			return ""
		}
		if i != 0 && p.ord&0x40 != 0 {
			return ""
		}
	}
	parts := make([][13]uint16, 0, k)
	for i := k - 1; i >= 0; i-- {
		parts = append(parts, pending[i].chars)
	}
	return assembleLFN(parts)
}

// matchDeletedLFN validates pending deleted LFN entries against a deleted
// short entry and restores the short name's first character. The LFN
// checksums must agree with each other and with the checksum of the short
// name once its first character is restored from the long name.
func matchDeletedLFN(pending []pendingLFN, ent []byte) (name string, first byte) {
	k := len(pending)
	if k == 0 {
		return "", 0
	}
	sum := pending[0].sum
	for _, p := range pending {
		if !p.deleted || p.sum != sum {
			return "", 0
		}
	}
	parts := make([][13]uint16, 0, k)
	for i := k - 1; i >= 0; i-- {
		parts = append(parts, pending[i].chars)
	}
	lfn := assembleLFN(parts)
	if lfn == "" {
		return "", 0
	}
	c, ok := deriveFirstShortChar(lfn)
	if !ok {
		return "", 0
	}
	var short [11]byte
	copy(short[:], ent[0:11])
	short[0] = c
	if lfnChecksum(short[:]) != sum {
		return "", 0
	}
	return lfn, c
}
