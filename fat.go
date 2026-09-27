package fatfs

import (
	"encoding/binary"
	"errors"
	"io"
)

// errImageTruncated means a read ran past the end of the image file.
var errImageTruncated = errors.New("fatfs: image truncated")

// fatEntry returns the raw FAT value for cluster n from the first FAT.
func (v *Volume) fatEntry(n uint32) (uint32, error) {
	var off int64
	switch v.fatType {
	case FAT12:
		off = v.fatOffset + int64(n) + int64(n)/2
	case FAT16:
		off = v.fatOffset + int64(n)*2
	default:
		off = v.fatOffset + int64(n)*4
	}
	var buf [4]byte
	need := 2
	if v.fatType != FAT12 {
		need = 4
	}
	if _, err := v.f.ReadAt(buf[:need], off); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, errImageTruncated
		}
		return 0, err
	}
	switch v.fatType {
	case FAT12:
		val := uint32(binary.LittleEndian.Uint16(buf[:2]))
		if n&1 == 0 {
			return val & 0x0FFF, nil
		}
		return val >> 4, nil
	case FAT16:
		return uint32(binary.LittleEndian.Uint16(buf[:2])), nil
	default:
		return binary.LittleEndian.Uint32(buf[:4]) & 0x0FFFFFFF, nil
	}
}

func (v *Volume) isFreeVal(val uint32) bool { return val == 0 }

func (v *Volume) isBadVal(val uint32) bool {
	switch v.fatType {
	case FAT12:
		return val == 0xFF7
	case FAT16:
		return val == 0xFFF7
	default:
		return val == 0x0FFFFFF7
	}
}

func (v *Volume) isEOCVal(val uint32) bool {
	switch v.fatType {
	case FAT12:
		return val >= 0xFF8
	case FAT16:
		return val >= 0xFFF8
	default:
		return val >= 0x0FFFFFF8
	}
}

// chainInfo describes a followed cluster chain.
type chainInfo struct {
	clusters []uint32
	anom     Anomaly
}

// followChain walks the FAT chain starting at cluster start. It never
// loops forever and never allocates based on untrusted sizes.
func (v *Volume) followChain(start uint32) chainInfo {
	ci := chainInfo{}
	if !v.validCluster(start) {
		ci.anom |= AnomClusterOOB
		return ci
	}
	seen := make(map[uint32]struct{})
	cur := start
	for {
		if _, dup := seen[cur]; dup {
			ci.anom |= AnomChainLoop
			return ci
		}
		seen[cur] = struct{}{}
		ci.clusters = append(ci.clusters, cur)
		if len(ci.clusters) >= maxChainClusters {
			ci.anom |= AnomChainLong
			return ci
		}
		val, err := v.fatEntry(cur)
		if err != nil {
			ci.anom |= AnomImageTruncated
			return ci
		}
		if v.isEOCVal(val) {
			return ci
		}
		if v.isFreeVal(val) {
			ci.anom |= AnomChainFree
			return ci
		}
		if v.isBadVal(val) {
			ci.anom |= AnomChainBad
			return ci
		}
		if !v.validCluster(val) {
			ci.anom |= AnomClusterOOB
			return ci
		}
		cur = val
	}
}
