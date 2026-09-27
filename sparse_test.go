package fat

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A nominal >=100GiB FAT32 image built as a sparse file must open and
// walk within seconds.
func TestSparseHugeImage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sparse image test in -short mode")
	}
	const gib = 1 << 30
	g := volGeom{
		typ: FAT32, bytesPerSec: 512, secPerClus: 8, reserved: 32,
		numFATs: 1, rootEntCnt: 0, fatSize: 204800,
		totSec:   100 * gib / 512,
		rootClus: 2, sysType: "FAT32   ",
	}
	if g.clusters() < 65525 {
		t.Fatalf("geometry yields %d clusters", g.clusters())
	}
	p := filepath.Join(t.TempDir(), "huge.dd")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Boot sector.
	sec := make([]byte, 512)
	le := binary.LittleEndian
	le.PutUint16(sec[11:13], 512)
	sec[13] = 8
	le.PutUint16(sec[14:16], 32)
	sec[16] = 1
	le.PutUint32(sec[32:36], uint32(g.totSec))
	le.PutUint32(sec[36:40], g.fatSize)
	le.PutUint32(sec[44:48], g.rootClus)
	copy(sec[82:90], g.sysType)
	sec[510], sec[511] = 0x55, 0xAA
	if _, err := f.WriteAt(sec, 0); err != nil {
		t.Fatal(err)
	}
	// One file entry in the root cluster.
	ent := shortEnt("SPARSE  TXT", 0x20, 0, 0)
	if _, err := f.WriteAt(ent, int64(g.clusOff(2))); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(g.totSec) * 512); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	v, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if v.Type() != FAT32 {
		t.Fatalf("type = %v", v.Type())
	}
	n := 0
	if err := v.Walk(func(e *Entry) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d entries", n)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("open+walk took %v", el)
	} else {
		t.Logf("open+walk took %v", el)
	}
}
