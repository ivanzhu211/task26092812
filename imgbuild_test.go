package fatfs

import (
	"encoding/binary"
	"os"
	"testing"
)

// imgBuilder constructs FAT images byte by byte in memory.
type imgBuilder struct {
	t          *testing.T
	bps        int
	spc        int
	resvd      int
	numFATs    int
	rootEntCnt int
	fatSz      int
	totSec     int
	rootClus   int
	fatType    FATType
	filSys     string // BS_FilSysType string to write
	data       []byte
}

// newImg creates an image whose data area holds exactly nClus clusters.
func newImg(t *testing.T, ft FATType, nClus int) *imgBuilder {
	t.Helper()
	b := &imgBuilder{
		t:       t,
		bps:     512,
		spc:     1,
		resvd:   1,
		numFATs: 1,
		fatType: ft,
	}
	switch ft {
	case FAT12:
		b.rootEntCnt = 16
		b.filSys = "FAT12   "
	case FAT16:
		b.rootEntCnt = 16
		b.filSys = "FAT16   "
	case FAT32:
		b.resvd = 2
		b.rootClus = 2
		b.filSys = "FAT32   "
	}
	rootDirSecs := (b.rootEntCnt*32 + b.bps - 1) / b.bps
	// Solve totSec so that the data area holds nClus clusters:
	// dataSec = totSec - resvd - numFATs*fatSz - rootDirSecs = nClus*spc
	// fatSz depends on nClus only.
	b.fatSz = fatSecsFor(ft, nClus, b.bps)
	b.totSec = b.resvd + b.numFATs*b.fatSz + rootDirSecs + nClus*b.spc
	b.data = make([]byte, b.totSec*b.bps)
	b.writeBoot()
	return b
}

func fatSecsFor(ft FATType, nClus, bps int) int {
	var perEntry int
	switch ft {
	case FAT12:
		// 1.5 bytes per entry, plus reserved entries
		return (nClus+2)*3/2/bps + 1
	case FAT16:
		perEntry = 2
	default:
		perEntry = 4
	}
	return ((nClus+2)*perEntry + bps - 1) / bps
}

func (b *imgBuilder) writeBoot() {
	d := b.data
	copy(d[0:3], []byte{0xEB, 0x3C, 0x90})
	copy(d[3:11], "MSDOS5.0")
	binary.LittleEndian.PutUint16(d[11:13], uint16(b.bps))
	d[13] = byte(b.spc)
	binary.LittleEndian.PutUint16(d[14:16], uint16(b.resvd))
	d[16] = byte(b.numFATs)
	binary.LittleEndian.PutUint16(d[17:19], uint16(b.rootEntCnt))
	if b.totSec < 65536 {
		binary.LittleEndian.PutUint16(d[19:21], uint16(b.totSec))
	} else {
		binary.LittleEndian.PutUint32(d[32:36], uint32(b.totSec))
	}
	d[21] = 0xF8
	if b.fatType == FAT32 {
		binary.LittleEndian.PutUint16(d[22:24], 0)
		binary.LittleEndian.PutUint32(d[36:40], uint32(b.fatSz))
		binary.LittleEndian.PutUint32(d[44:48], uint32(b.rootClus))
		copy(d[54:62], "        ")
		copy(d[82:90], b.filSys)
	} else {
		binary.LittleEndian.PutUint16(d[22:24], uint16(b.fatSz))
		copy(d[54:62], b.filSys)
	}
	d[510], d[511] = 0x55, 0xAA
}

func (b *imgBuilder) fatOff() int  { return b.resvd * b.bps }
func (b *imgBuilder) rootOff() int { return (b.resvd + b.numFATs*b.fatSz) * b.bps }
func (b *imgBuilder) dataOff() int {
	rootDirSecs := (b.rootEntCnt*32 + b.bps - 1) / b.bps
	return (b.resvd + b.numFATs*b.fatSz + rootDirSecs) * b.bps
}
func (b *imgBuilder) clusOff(n int) int {
	return b.dataOff() + (n-2)*b.spc*b.bps
}

// setFAT writes a FAT entry for cluster n.
func (b *imgBuilder) setFAT(n int, val uint32) {
	switch b.fatType {
	case FAT12:
		off := b.fatOff() + n + n/2
		cur := binary.LittleEndian.Uint16(b.data[off : off+2])
		if n&1 == 0 {
			cur = cur&0xF000 | uint16(val&0x0FFF)
		} else {
			cur = cur&0x000F | uint16(val&0x0FFF)<<4
		}
		binary.LittleEndian.PutUint16(b.data[off:off+2], cur)
	case FAT16:
		binary.LittleEndian.PutUint16(b.data[b.fatOff()+n*2:], uint16(val))
	default:
		off := b.fatOff() + n*4
		old := binary.LittleEndian.Uint32(b.data[off : off+4])
		binary.LittleEndian.PutUint32(b.data[off:off+4], old&0xF0000000|val&0x0FFFFFFF)
	}
}

func (b *imgBuilder) eoc() uint32 {
	switch b.fatType {
	case FAT12:
		return 0xFF8
	case FAT16:
		return 0xFFF8
	default:
		return 0x0FFFFFF8
	}
}

// writeCluster places data at cluster n (truncated/padded to cluster size).
func (b *imgBuilder) writeCluster(n int, data []byte) {
	off := b.clusOff(n)
	end := off + b.spc*b.bps
	if end > len(b.data) {
		b.t.Fatalf("cluster %d past image end", n)
	}
	copy(b.data[off:end], data)
}

// putRootEntries writes directory entries into the root directory.
func (b *imgBuilder) putRootEntries(entries ...[]byte) {
	var off int
	if b.fatType == FAT32 {
		b.setFAT(b.rootClus, b.eoc())
		off = b.clusOff(b.rootClus)
	} else {
		off = b.rootOff()
	}
	for _, e := range entries {
		copy(b.data[off:off+32], e)
		off += 32
	}
}

// --- directory entry helpers ---

// name11 converts "FOO     TXT" style or 8.3 to the 11-byte packed form.
func name11(base, ext string) [11]byte {
	var n [11]byte
	for i := range n {
		n[i] = ' '
	}
	copy(n[0:8], base)
	copy(n[8:11], ext)
	return n
}

type shortOpt func(*[32]byte)

func withNTRes(v byte) shortOpt { return func(e *[32]byte) { e[12] = v } }
func withModTime(h, m, s int) shortOpt {
	return func(e *[32]byte) {
		binary.LittleEndian.PutUint16(e[22:24], uint16(h<<11|m<<5|s/2))
	}
}
func withModDate(y, mo, d int) shortOpt {
	return func(e *[32]byte) {
		binary.LittleEndian.PutUint16(e[24:26], uint16((y-1980)<<9|mo<<5|d))
	}
}
func withCreate(y, mo, d, h, mi, s, ms int) shortOpt {
	return func(e *[32]byte) {
		e[13] = byte(ms / 10)
		binary.LittleEndian.PutUint16(e[14:16], uint16(h<<11|mi<<5|s/2))
		binary.LittleEndian.PutUint16(e[16:18], uint16((y-1980)<<9|mo<<5|d))
	}
}
func withAccess(y, mo, d int) shortOpt {
	return func(e *[32]byte) {
		binary.LittleEndian.PutUint16(e[18:20], uint16((y-1980)<<9|mo<<5|d))
	}
}

// mkShort builds a 32-byte short directory entry.
func mkShort(name [11]byte, attr byte, clus int, size int, opts ...shortOpt) []byte {
	var e [32]byte
	copy(e[0:11], name[:])
	e[11] = attr
	binary.LittleEndian.PutUint16(e[20:22], uint16(clus>>16))
	binary.LittleEndian.PutUint16(e[26:28], uint16(clus))
	binary.LittleEndian.PutUint32(e[28:32], uint32(size))
	for _, o := range opts {
		o(&e)
	}
	return e[:]
}

// deleteEnt returns a copy of the entry with the first byte set to 0xE5.
func deleteEnt(e []byte) []byte {
	c := make([]byte, 32)
	copy(c, e)
	c[0] = 0xE5
	return c
}

// testLFNChecksum duplicates the spec checksum for test construction.
func testLFNChecksum(name [11]byte) byte {
	var sum byte
	for i := 0; i < 11; i++ {
		sum = ((sum & 1) << 7) + (sum >> 1) + name[i]
	}
	return sum
}

// lfnEnts builds the on-disk LFN entry sequence (last part first) for s.
func lfnEnts(shortName [11]byte, s string) [][]byte {
	csum := testLFNChecksum(shortName)
	runes := []rune(s)
	var parts [][]rune
	for i := 0; i < len(runes); i += 13 {
		end := i + 13
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, runes[i:end])
	}
	n := len(parts)
	var out [][]byte
	for i := n - 1; i >= 0; i-- {
		var e [32]byte
		seq := byte(i + 1)
		if i == n-1 {
			seq |= 0x40
		}
		e[0] = seq
		e[11] = 0x0F
		e[13] = csum
		var units [13]uint16
		for j := range units {
			units[j] = 0xFFFF
		}
		for j, r := range parts[i] {
			units[j] = uint16(r)
		}
		if len(parts[i]) < 13 {
			units[len(parts[i])] = 0x0000
		}
		for j := 0; j < 5; j++ {
			binary.LittleEndian.PutUint16(e[1+j*2:], units[j])
		}
		for j := 0; j < 6; j++ {
			binary.LittleEndian.PutUint16(e[14+j*2:], units[5+j])
		}
		for j := 0; j < 2; j++ {
			binary.LittleEndian.PutUint16(e[28+j*2:], units[11+j])
		}
		out = append(out, e[:])
	}
	return out
}

// writeImage writes the image to a temp file and returns its path.
func (b *imgBuilder) writeImage() string {
	b.t.Helper()
	f := b.t.TempDir() + "/img.dd"
	if err := writeFile(f, b.data); err != nil {
		b.t.Fatal(err)
	}
	return f
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
