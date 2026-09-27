package fatfs

import (
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// TestSparse100GiB builds a nominal >=100 GiB FAT32 image as a sparse
// file and requires Open+Walk to finish in a few seconds.
func TestSparse100GiB(t *testing.T) {
	const (
		bps     = 512
		spc     = 64 // 32 KiB clusters
		resvd   = 2
		numFATs = 1
	)
	totSec := uint64(100) << 30 / bps // 100 GiB
	dataSec := totSec - resvd
	nClus := dataSec / spc
	fatSz := (nClus*4 + bps - 1) / bps
	// recompute with FAT size accounted
	dataSec = totSec - resvd - fatSz
	nClus = dataSec / spc
	dataOff := int64(resvd+fatSz) * bps

	path := t.TempDir() + "/sparse.dd"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// boot sector
	var bs [512]byte
	copy(bs[0:3], []byte{0xEB, 0x3C, 0x90})
	copy(bs[3:11], "MSDOS5.0")
	binary.LittleEndian.PutUint16(bs[11:13], bps)
	bs[13] = spc
	binary.LittleEndian.PutUint16(bs[14:16], resvd)
	bs[16] = numFATs
	binary.LittleEndian.PutUint32(bs[32:36], uint32(totSec))
	bs[21] = 0xF8
	binary.LittleEndian.PutUint32(bs[36:40], uint32(fatSz))
	binary.LittleEndian.PutUint32(bs[44:48], 2) // root cluster
	copy(bs[82:90], "FAT32   ")
	bs[510], bs[511] = 0x55, 0xAA
	if _, err := f.WriteAt(bs[:], 0); err != nil {
		t.Fatal(err)
	}
	// FAT: cluster 2 (root) = EOC
	var fatEnt [4]byte
	binary.LittleEndian.PutUint32(fatEnt[:], 0x0FFFFFF8)
	if _, err := f.WriteAt(fatEnt[:], int64(resvd*bps+2*4)); err != nil {
		t.Fatal(err)
	}
	// root dir: one small live file entry at cluster 2
	root := mkShort(name11("HELLO", "TXT"), 0x20, 0, 0)
	if _, err := f.WriteAt(root, dataOff); err != nil {
		t.Fatal(err)
	}
	// extend to nominal size
	if err := f.Truncate(int64(totSec) * bps); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if v.FATType() != FAT32 {
		t.Fatalf("type = %v, want FAT32", v.FATType())
	}
	n := 0
	if err := v.Walk(func(e *Entry) bool { n++; return true }); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if n != 1 {
		t.Fatalf("got %d entries, want 1", n)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Open+Walk took %v, too slow", elapsed)
	}
	t.Logf("Open+Walk on 100 GiB sparse image took %v", elapsed)
}
