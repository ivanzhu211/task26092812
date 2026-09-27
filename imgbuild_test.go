package fat

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// volGeom describes a synthetic volume layout for tests.
type volGeom struct {
	typ         Type
	bytesPerSec uint32
	secPerClus  uint32
	reserved    uint32
	numFATs     uint32
	rootEntCnt  uint32
	fatSize     uint32 // sectors per FAT
	totSec      uint64
	rootClus    uint32 // FAT32 only
	sysType     string // bytes written into BS_FilSysType
}

func (g volGeom) rootDirSecs() uint32 {
	return (g.rootEntCnt*32 + g.bytesPerSec - 1) / g.bytesPerSec
}

func (g volGeom) firstData() uint64 {
	return uint64(g.reserved) + uint64(g.numFATs)*uint64(g.fatSize) + uint64(g.rootDirSecs())
}

func (g volGeom) clusters() uint64 {
	return (g.totSec - g.firstData()) / uint64(g.secPerClus)
}

func (g volGeom) clusOff(n uint32) uint64 {
	return (g.firstData() + uint64(n-2)*uint64(g.secPerClus)) * uint64(g.bytesPerSec)
}

func (g volGeom) fatOff(n uint32) uint64 {
	base := uint64(g.reserved) * uint64(g.bytesPerSec)
	switch g.typ {
	case FAT12:
		return base + uint64(n) + uint64(n)/2
	case FAT16:
		return base + uint64(n)*2
	default:
		return base + uint64(n)*4
	}
}

// geomFAT12 returns a valid small FAT12 geometry.
func geomFAT12() volGeom {
	return volGeom{
		typ: FAT12, bytesPerSec: 512, secPerClus: 1, reserved: 1,
		numFATs: 1, rootEntCnt: 16, fatSize: 1, totSec: 100,
		sysType: "FAT12   ",
	}
}

// geomFAT16 returns a valid FAT16 geometry (>= 4085 clusters).
func geomFAT16() volGeom {
	g := volGeom{
		typ: FAT16, bytesPerSec: 512, secPerClus: 1, reserved: 1,
		numFATs: 1, rootEntCnt: 512, fatSize: 17, totSec: 4250,
		sysType: "FAT16   ",
	}
	if g.clusters() < 4085 || g.clusters() >= 65525 {
		panic("bad FAT16 geometry")
	}
	return g
}

// geomFAT32 returns a valid FAT32 geometry (>= 65525 clusters).
func geomFAT32() volGeom {
	g := volGeom{
		typ: FAT32, bytesPerSec: 512, secPerClus: 1, reserved: 32,
		numFATs: 1, rootEntCnt: 0, fatSize: 512, totSec: 66144,
		rootClus: 2,
		sysType:  "FAT32   ",
	}
	if g.clusters() < 65525 {
		panic("bad FAT32 geometry")
	}
	return g
}

type img struct {
	geom volGeom
	data []byte
}

func newImage(g volGeom) *img {
	m := &img{geom: g, data: make([]byte, g.totSec*uint64(g.bytesPerSec))}
	le := binary.LittleEndian
	le.PutUint16(m.data[11:13], uint16(g.bytesPerSec))
	m.data[13] = byte(g.secPerClus)
	le.PutUint16(m.data[14:16], uint16(g.reserved))
	m.data[16] = byte(g.numFATs)
	le.PutUint16(m.data[17:19], uint16(g.rootEntCnt))
	if g.totSec < 65536 {
		le.PutUint16(m.data[19:21], uint16(g.totSec))
	} else {
		le.PutUint32(m.data[32:36], uint32(g.totSec))
	}
	if g.typ == FAT32 {
		le.PutUint32(m.data[36:40], g.fatSize)
		le.PutUint32(m.data[44:48], g.rootClus)
		copy(m.data[82:90], g.sysType)
	} else {
		le.PutUint16(m.data[22:24], uint16(g.fatSize))
		copy(m.data[54:62], g.sysType)
	}
	m.data[510], m.data[511] = 0x55, 0xAA
	return m
}

func (m *img) clus(n uint32) []byte {
	off := m.geom.clusOff(n)
	sz := uint64(m.geom.secPerClus) * uint64(m.geom.bytesPerSec)
	return m.data[off : off+sz]
}

// rootDir returns the FAT12/16 root directory region.
func (m *img) rootDir() []byte {
	off := (uint64(m.geom.reserved) + uint64(m.geom.numFATs)*uint64(m.geom.fatSize)) * uint64(m.geom.bytesPerSec)
	sz := uint64(m.geom.rootDirSecs()) * uint64(m.geom.bytesPerSec)
	return m.data[off : off+sz]
}

func (m *img) setFAT(n uint32, val uint32) {
	off := m.geom.fatOff(n)
	le := binary.LittleEndian
	switch m.geom.typ {
	case FAT12:
		old := uint32(le.Uint16(m.data[off : off+2]))
		if n&1 != 0 {
			old = (old & 0x000F) | (val << 4)
		} else {
			old = (old & 0xF000) | (val & 0x0FFF)
		}
		le.PutUint16(m.data[off:off+2], uint16(old))
	case FAT16:
		le.PutUint16(m.data[off:off+2], uint16(val))
	default:
		old := le.Uint32(m.data[off:off+4]) & 0xF0000000
		le.PutUint32(m.data[off:off+4], old|(val&0x0FFFFFFF))
	}
}

// shortEnt builds a 32-byte short directory entry. name is the raw
// 11-byte 8.3 name (space padded).
func shortEnt(name string, attr byte, clus uint32, size uint32) []byte {
	if len(name) != 11 {
		panic("short name must be 11 bytes")
	}
	e := make([]byte, 32)
	copy(e[0:11], name)
	e[11] = attr
	le := binary.LittleEndian
	le.PutUint16(e[26:28], uint16(clus))
	le.PutUint16(e[20:22], uint16(clus>>16))
	le.PutUint32(e[28:32], size)
	return e
}

// lfnEnt builds an LFN entry. chars are up to 13 UTF-16 code units.
func lfnEnt(ord byte, sum byte, chars []uint16) []byte {
	if len(chars) > 13 {
		panic("too many chars")
	}
	e := make([]byte, 32)
	e[0] = ord
	e[11] = 0x0F
	e[13] = sum
	all := make([]uint16, 13)
	for i := range all {
		all[i] = 0xFFFF
	}
	copy(all, chars)
	if len(chars) < 13 {
		all[len(chars)] = 0x0000
	}
	le := binary.LittleEndian
	put := func(off int, v uint16) { le.PutUint16(e[off:off+2], v) }
	for i, off := range []int{1, 3, 5, 7, 9} {
		put(off, all[i])
	}
	for i, off := range []int{14, 16, 18, 20, 22, 24} {
		put(off, all[5+i])
	}
	for i, off := range []int{28, 30} {
		put(off, all[11+i])
	}
	return e
}

// lfnEntries builds the on-disk LFN entry sequence (last part first) for
// name, with the given short-name checksum.
func lfnEntries(name string, sum byte) [][]byte {
	runes := []rune(name)
	units := make([]uint16, 0, len(runes))
	for _, r := range runes {
		if r > 0xFFFF {
			panic("surrogate not supported in test builder")
		}
		units = append(units, uint16(r))
	}
	nparts := (len(units) + 12) / 13
	var out [][]byte
	for i := nparts - 1; i >= 0; i-- {
		end := (i + 1) * 13
		if end > len(units) {
			end = len(units)
		}
		ord := byte(i + 1)
		if i == nparts-1 {
			ord |= 0x40
		}
		out = append(out, lfnEnt(ord, sum, units[i*13:end]))
	}
	return out
}

// place writes directory entries sequentially into a directory buffer.
func place(dir []byte, ents ...[]byte) {
	off := 0
	for _, e := range ents {
		copy(dir[off:off+32], e)
		off += 32
	}
}

func (m *img) write(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "img.dd")
	if err := os.WriteFile(p, m.data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// collect walks a volume and gathers all entries.
func collect(t *testing.T, v *Volume) []*Entry {
	t.Helper()
	var out []*Entry
	err := v.Walk(func(e *Entry) error {
		cp := *e
		out = append(out, &cp)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return out
}

func findEntry(t *testing.T, ents []*Entry, path string) *Entry {
	t.Helper()
	for _, e := range ents {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("entry %s not found", path)
	return nil
}
