package fatfs

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// maxChainClusters caps cluster-chain traversal so corrupt FATs cannot
// cause unbounded work or allocation.
const maxChainClusters = 1 << 20

// maxDirBytes caps how much directory data is read for one directory.
const maxDirBytes = 1 << 26

// maxDirDepth caps subdirectory recursion depth.
const maxDirDepth = 512

const (
	attrLFN     = 0x0F
	attrVolID   = 0x08
	attrDir     = 0x10
	deletedMark = 0xE5
)

type lfnEnt struct {
	raw     [32]byte
	deleted bool
}

type shortEnt struct {
	name         [11]byte
	attr         byte
	ntres        byte
	createTenths byte
	createTime   uint16
	createDate   uint16
	accessDate   uint16
	clusHigh     uint16
	modTime      uint16
	modDate      uint16
	clusLow      uint16
	size         uint32
	deleted      bool
}

func parseShort(raw []byte, deleted bool) shortEnt {
	var s shortEnt
	copy(s.name[:], raw[0:11])
	s.attr = raw[11]
	s.ntres = raw[12]
	s.createTenths = raw[13]
	s.createTime = binary.LittleEndian.Uint16(raw[14:16])
	s.createDate = binary.LittleEndian.Uint16(raw[16:18])
	s.accessDate = binary.LittleEndian.Uint16(raw[18:20])
	s.clusHigh = binary.LittleEndian.Uint16(raw[20:22])
	s.modTime = binary.LittleEndian.Uint16(raw[22:24])
	s.modDate = binary.LittleEndian.Uint16(raw[24:26])
	s.clusLow = binary.LittleEndian.Uint16(raw[26:28])
	s.size = binary.LittleEndian.Uint32(raw[28:32])
	s.deleted = deleted
	return s
}

func (s *shortEnt) firstCluster() uint32 {
	return uint32(s.clusHigh)<<16 | uint32(s.clusLow)
}

func (s *shortEnt) isDot() bool {
	if s.name[0] != '.' {
		return false
	}
	if s.name[1] == ' ' {
		return isSpacePadded(s.name[1:])
	}
	return s.name[1] == '.' && isSpacePadded(s.name[2:])
}

func isSpacePadded(b []byte) bool {
	for _, c := range b {
		if c != ' ' {
			return false
		}
	}
	return true
}

// lfnChecksum computes the short-name checksum used by LFN entries.
func lfnChecksum(name [11]byte) byte {
	var sum byte
	for i := 0; i < 11; i++ {
		sum = ((sum & 1) << 7) + (sum >> 1) + name[i]
	}
	return sum
}

// legalStoredFirstChar reports whether b may appear as the stored first
// byte of a short name. 0x05 is the on-disk form of a real 0xE5.
func legalStoredFirstChar(b byte) bool {
	if b == 0x05 {
		return true
	}
	return legalShortChar(b)
}

func legalShortChar(b byte) bool {
	if b >= 0x80 {
		return true
	}
	if b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' {
		return true
	}
	switch b {
	case '$', '%', '\'', '-', '_', '@', '~', '`', '!', '(', ')', '{', '}', '^', '#', '&':
		return true
	}
	return false
}

// restoreFirstChar finds the deleted short name's original first byte by
// matching the LFN checksum. Returns false when no legal byte fits.
func restoreFirstChar(name [11]byte, csum byte) (byte, bool) {
	var matches []byte
	for c := 0; c < 256; c++ {
		b := byte(c)
		if !legalStoredFirstChar(b) {
			continue
		}
		name[0] = b
		if lfnChecksum(name) == csum {
			matches = append(matches, b)
		}
	}
	if len(matches) == 0 {
		return 0, false
	}
	for _, m := range matches {
		if m > 0x20 && m < 0x7F {
			return m, true
		}
	}
	return matches[0], true
}

// validLiveLFNs checks sequence contiguity and checksum for LFN entries
// preceding a live short entry. lfns are in disk order.
func validLiveLFNs(lfns []lfnEnt, csum byte) bool {
	n := len(lfns)
	if n == 0 || n > 20 {
		return false
	}
	for i, l := range lfns {
		if l.deleted {
			return false
		}
		seq := l.raw[0]
		expect := byte(n - i)
		if i == 0 {
			if seq != expect|0x40 {
				return false
			}
		} else if seq != expect {
			return false
		}
		if l.raw[13] != csum {
			return false
		}
	}
	return true
}

// validDeletedLFNs checks that all entries are deleted and share one
// checksum; sequence bytes are destroyed (0xE5) and cannot be verified.
func validDeletedLFNs(lfns []lfnEnt) (byte, bool) {
	if len(lfns) == 0 || len(lfns) > 20 {
		return 0, false
	}
	csum := lfns[0].raw[13]
	for _, l := range lfns {
		if !l.deleted || l.raw[13] != csum {
			return 0, false
		}
	}
	return csum, true
}

// assembleLFN decodes the UTF-16 name from LFN entries in disk order
// (last part first on disk).
func assembleLFN(lfns []lfnEnt) string {
	var units []uint16
	for i := len(lfns) - 1; i >= 0; i-- {
		raw := lfns[i].raw
		var parts [13]uint16
		for j := 0; j < 5; j++ {
			parts[j] = binary.LittleEndian.Uint16(raw[1+j*2:])
		}
		for j := 0; j < 6; j++ {
			parts[5+j] = binary.LittleEndian.Uint16(raw[14+j*2:])
		}
		for j := 0; j < 2; j++ {
			parts[11+j] = binary.LittleEndian.Uint16(raw[28+j*2:])
		}
		done := false
		for _, u := range parts {
			if u == 0x0000 {
				done = true
				break
			}
			if u == 0xFFFF {
				continue
			}
			units = append(units, u)
		}
		if done {
			break
		}
	}
	return string(utf16.Decode(units))
}

// buildShortName renders the 8.3 name, applying the 0x05->0xE5 mapping
// and, when no LFN is present, the NT lowercase flags.
func buildShortName(name [11]byte, ntres byte, hasLFN bool) string {
	if name[0] == 0x05 {
		name[0] = 0xE5
	}
	base := strings.TrimRight(string(name[0:8]), " ")
	ext := strings.TrimRight(string(name[8:11]), " ")
	if !hasLFN {
		if ntres&0x08 != 0 {
			base = strings.ToLower(base)
		}
		if ntres&0x10 != 0 {
			ext = strings.ToLower(ext)
		}
	}
	if ext != "" {
		return base + "." + ext
	}
	return base
}

type walker struct {
	v    *Volume
	fn   func(*Entry) bool
	stop bool
}

// Walk calls fn once for every directory entry in the volume, including
// deleted entries. Returning false from fn stops the walk.
func (v *Volume) Walk(fn func(*Entry) bool) error {
	w := &walker{v: v, fn: fn}
	if v.fatType == FAT32 {
		w.walkClusterDir(v.rootClus, "", 0, map[uint32]bool{v.rootClus: true}, false, 0)
	} else {
		w.walkRootDir("")
	}
	return nil
}

// walkRootDir parses the fixed FAT12/16 root directory region.
func (w *walker) walkRootDir(dirPath string) {
	v := w.v
	off := (int64(v.resvdSecCnt) + int64(v.numFATs)*int64(v.fatSize)) * int64(v.bytesPerSec)
	size := int64(v.rootDirSectors) * int64(v.bytesPerSec)
	if size <= 0 {
		return
	}
	if off+size > v.imageSize {
		size = v.imageSize - off
		if size <= 0 {
			return
		}
	}
	buf := make([]byte, size)
	n, err := v.f.ReadAt(buf, off)
	if err != nil && n == 0 {
		return
	}
	w.parseEntries(buf[:n], dirPath, 0, map[uint32]bool{}, false, 0)
}

// walkClusterDir parses a cluster-chained directory (FAT32 root or a
// subdirectory).
func (w *walker) walkClusterDir(clus uint32, dirPath string, parentClus uint32, ancestors map[uint32]bool, forceDeleted bool, depth int) {
	v := w.v
	var data []byte
	if forceDeleted {
		// Deleted directories: only the first cluster is parsed.
		data = make([]byte, v.clusterSize)
		n, err := v.f.ReadAt(data, v.clusterOffset(clus))
		if err != nil && n == 0 {
			return
		}
		data = data[:n]
	} else {
		ci := v.followChain(clus)
		if len(ci.clusters) == 0 {
			return
		}
		var sb []byte
		for _, c := range ci.clusters {
			if len(sb) >= maxDirBytes {
				break
			}
			chunk := make([]byte, v.clusterSize)
			n, err := v.f.ReadAt(chunk, v.clusterOffset(c))
			if err != nil && n == 0 {
				break
			}
			sb = append(sb, chunk[:n]...)
			if n < len(chunk) {
				break
			}
		}
		data = sb
	}
	w.parseEntries(data, dirPath, clus, ancestors, forceDeleted, depth)
}

// parseEntries scans 32-byte directory entries, combining LFN sets with
// their short entries and recursing into subdirectories.
func (w *walker) parseEntries(data []byte, dirPath string, parentClus uint32, ancestors map[uint32]bool, forceDeleted bool, depth int) {
	var pending []lfnEnt
	for off := 0; off+32 <= len(data); off += 32 {
		if w.stop {
			return
		}
		raw := data[off : off+32]
		b0 := raw[0]
		if b0 == 0x00 {
			return
		}
		attr := raw[11]
		if attr == attrLFN {
			var l lfnEnt
			copy(l.raw[:], raw)
			l.deleted = b0 == deletedMark
			pending = append(pending, l)
			continue
		}
		if attr&attrVolID != 0 {
			pending = pending[:0]
			continue
		}
		s := parseShort(raw, b0 == deletedMark)
		if s.isDot() {
			pending = pending[:0]
			continue
		}
		w.emit(dirPath, parentClus, &s, pending, ancestors, forceDeleted, depth)
		pending = pending[:0]
	}
}

// emit builds the Entry for one short entry, reports it, and recurses.
func (w *walker) emit(dirPath string, parentClus uint32, s *shortEnt, lfns []lfnEnt, ancestors map[uint32]bool, forceDeleted bool, depth int) {
	v := w.v
	deleted := s.deleted || forceDeleted
	name := s.name

	var longName string
	hasLFN := false
	if len(lfns) > 0 {
		if s.deleted && !forceDeleted {
			if csum, ok := validDeletedLFNs(lfns); ok {
				if first, ok := restoreFirstChar(name, csum); ok {
					name[0] = first
					longName = assembleLFN(lfns)
					hasLFN = true
				}
			}
		} else {
			if validLiveLFNs(lfns, lfnChecksum(name)) {
				longName = assembleLFN(lfns)
				hasLFN = true
			}
		}
	}
	if s.deleted && !forceDeleted && !hasLFN {
		name[0] = '_'
	}

	e := &Entry{
		LongName:     longName,
		ShortName:    buildShortName(name, s.ntres, hasLFN),
		Attr:         s.attr,
		Size:         s.size,
		FirstCluster: s.firstCluster(),
		ModTime:      decodeFATTime(s.modDate, s.modTime, 0),
		CreateTime:   decodeFATTime(s.createDate, s.createTime, s.createTenths),
		AccessTime:   decodeFATTime(s.accessDate, 0, 0),
		Deleted:      deleted,
	}
	if hasLFN {
		e.Path = joinPath(dirPath, longName)
	} else {
		e.Path = joinPath(dirPath, e.ShortName)
	}

	isDir := s.attr&attrDir != 0
	needed := clustersNeeded(uint64(s.size), uint64(v.clusterSize))

	if deleted {
		e.Recovery = v.recoveryStatus(e.FirstCluster, s.size)
	} else {
		if uint64(needed) > v.clusterCnt && needed > 0 {
			e.Anomalies |= AnomSizeExcessive
		}
		if isDir || s.size > 0 {
			ci := v.followChain(e.FirstCluster)
			e.Anomalies |= ci.anom
			if !isDir {
				if uint64(len(ci.clusters)) < needed {
					e.Anomalies |= AnomChainShort
				} else if uint64(len(ci.clusters)) > needed {
					e.Anomalies |= AnomChainLong
				}
			}
		}
	}

	// Cycle detection must happen before the callback so the entry
	// carries the annotation.
	recurse := false
	recurseDeleted := false
	if isDir && e.FirstCluster != 0 && depth < maxDirDepth {
		if ancestors[e.FirstCluster] {
			e.Anomalies |= AnomDirCycle
		} else if deleted {
			recurseDeleted = w.checkDeletedDir(e.FirstCluster, parentClus)
			recurse = recurseDeleted
		} else {
			recurse = true
		}
	}

	if !w.fn(e) {
		w.stop = true
		return
	}

	if recurse {
		childAncestors := make(map[uint32]bool, len(ancestors)+1)
		for k, val := range ancestors {
			childAncestors[k] = val
		}
		childAncestors[e.FirstCluster] = true
		w.walkClusterDir(e.FirstCluster, e.Path, e.FirstCluster, childAncestors, recurseDeleted, depth+1)
	}
}

// checkDeletedDir reports whether a deleted directory's first cluster is
// free and begins with "." and ".." entries pointing to itself and its
// parent.
func (w *walker) checkDeletedDir(clus uint32, parentClus uint32) bool {
	v := w.v
	if !v.validCluster(clus) {
		return false
	}
	val, err := v.fatEntry(clus)
	if err != nil || val != 0 {
		return false
	}
	buf := make([]byte, 64)
	if _, err := v.f.ReadAt(buf, v.clusterOffset(clus)); err != nil {
		return false
	}
	dot := parseShort(buf[0:32], false)
	dotdot := parseShort(buf[32:64], false)
	if !dot.isDot() || dot.name[0] != '.' || dot.name[1] != ' ' {
		return false
	}
	if dotdot.name[0] != '.' || dotdot.name[1] != '.' || !isSpacePadded(dotdot.name[2:]) {
		return false
	}
	if dot.firstCluster() != clus {
		return false
	}
	return dotdot.firstCluster() == parentClus
}

// recoveryStatus classifies a deleted file assuming contiguous storage
// from its first cluster.
func (v *Volume) recoveryStatus(firstClus uint32, size uint32) Recovery {
	needed := clustersNeeded(uint64(size), uint64(v.clusterSize))
	if needed == 0 {
		return RecoveryComplete
	}
	if !v.validCluster(firstClus) {
		return RecoveryUnrecoverable
	}
	if uint64(firstClus)+needed-1 > v.clusterCnt+1 {
		return RecoveryUnrecoverable
	}
	status := RecoveryComplete
	for c := uint64(firstClus); c < uint64(firstClus)+needed; c++ {
		val, err := v.fatEntry(uint32(c))
		if err != nil {
			return RecoveryUnrecoverable
		}
		if val != 0 {
			status = RecoveryPartial
		}
	}
	return status
}

func clustersNeeded(size, clusterSize uint64) uint64 {
	if size == 0 {
		return 0
	}
	return (size + clusterSize - 1) / clusterSize
}

func joinPath(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}
