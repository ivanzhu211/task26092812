package fatfs

import (
	"testing"
	"time"
)

func collect(v *Volume) []*Entry {
	var out []*Entry
	v.Walk(func(e *Entry) bool { out = append(out, e); return true })
	return out
}

// First byte 0x05 means the real first character is 0xE5, not deleted.
func TestFirstByte05(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	name := name11("", "TXT")
	name[0] = 0x05
	copy(name[1:8], "FILE   ")
	b.putRootEntries(mkShort(name, 0x20, 0, 0))
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ents := collect(v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries", len(ents))
	}
	if ents[0].Deleted {
		t.Fatal("0x05 first byte must not be treated as deleted")
	}
	if ents[0].ShortName[0] != 0xE5 {
		t.Fatalf("short name = %q, want leading 0xE5", ents[0].ShortName)
	}
}

// A 0x00 first byte ends the directory: nothing after it is produced.
func TestZeroByteTerminatesDir(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	ents := [][]byte{
		mkShort(name11("FIRST", "TXT"), 0x20, 0, 0),
		mkShort(name11("SECOND", "TXT"), 0x20, 0, 0),
	}
	ents[1][0] = 0x00 // terminator
	ents = append(ents, mkShort(name11("THIRD", "TXT"), 0x20, 0, 0))
	b.putRootEntries(ents...)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	got := collect(v)
	if len(got) != 1 || got[0].ShortName != "FIRST.TXT" {
		t.Fatalf("got %v entries", got)
	}
}

func TestTimestampEdges(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	mk := func() []byte { return mkShort(name11("A", "T"), 0x20, 0, 0) }
	// date zero -> empty
	e1 := mk()
	// Feb 30 -> invalid, must not normalize to March
	e2 := mk()
	e2[24] = 30 | (2 << 5) // day=30 month=2
	e2[25] = byte((2021 - 1980) >> 1)
	// valid full timestamp
	e3 := mkShort(name11("C", "T"), 0x20, 0, 0,
		withModDate(1999, 12, 31), withModTime(23, 59, 58),
		withCreate(1999, 12, 31, 23, 59, 58, 990))
	b.putRootEntries(e1, e2, e3)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ents := collect(v)
	if len(ents) != 3 {
		t.Fatalf("got %d entries", len(ents))
	}
	if ents[0].ModTime.Valid {
		t.Fatalf("zero date must be empty, got %v", ents[0].ModTime.Time)
	}
	if ents[1].ModTime.Valid {
		t.Fatalf("Feb 30 must be empty, got %v", ents[1].ModTime.Time)
	}
	mt := ents[2].ModTime
	if !mt.Valid || mt.Time.Year() != 1999 || mt.Time.Month() != 12 || mt.Time.Day() != 31 ||
		mt.Time.Hour() != 23 || mt.Time.Minute() != 59 || mt.Time.Second() != 58 {
		t.Fatalf("modtime = %v", mt.Time)
	}
	ct := ents[2].CreateTime
	if !ct.Valid || ct.Time.Nanosecond() != 990*int(time.Millisecond) {
		t.Fatalf("createtime = %v", ct.Time)
	}
}

func TestChainAnomalies(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	// short chain: size needs 3 clusters, chain has 1
	b.setFAT(10, b.eoc())
	// long chain: size needs 1 cluster, chain has 3
	b.setFAT(20, 21)
	b.setFAT(21, 22)
	b.setFAT(22, b.eoc())
	// chain through a free cluster
	b.setFAT(30, 31) // 31 is free
	// chain through a bad cluster
	b.setFAT(40, 41)
	b.setFAT(41, 0xFFF7)
	// loop
	b.setFAT(50, 51)
	b.setFAT(51, 50)
	// out of range
	b.setFAT(60, 9999)
	b.putRootEntries(
		mkShort(name11("SHORT", "T"), 0x20, 10, 1500),
		mkShort(name11("LONG", "T"), 0x20, 20, 10),
		mkShort(name11("FREE", "T"), 0x20, 30, 10),
		mkShort(name11("BAD", "T"), 0x20, 40, 10),
		mkShort(name11("LOOP", "T"), 0x20, 50, 10),
		mkShort(name11("OOB", "T"), 0x20, 60, 10),
	)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ents := collect(v)
	if len(ents) != 6 {
		t.Fatalf("got %d entries", len(ents))
	}
	check := func(i int, f Anomaly, name string) {
		if !ents[i].Anomalies.Has(f) {
			t.Fatalf("%s: anomalies = %#x, want %#x", name, ents[i].Anomalies, f)
		}
	}
	check(0, AnomChainShort, "short")
	check(1, AnomChainLong, "long")
	check(2, AnomChainFree, "free")
	check(3, AnomChainBad, "bad")
	check(4, AnomChainLoop, "loop")
	check(5, AnomClusterOOB, "oob")
}

func TestDeletedUnrecoverable(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	// first cluster beyond volume end
	b.putRootEntries(deleteEnt(mkShort(name11("GONE", "T"), 0x20, 99999, 100)))
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ents := collect(v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries", len(ents))
	}
	if ents[0].Recovery != RecoveryUnrecoverable {
		t.Fatalf("recovery = %v, want unrecoverable", ents[0].Recovery)
	}
}

// A deleted directory whose first cluster is free and starts with
// "." and ".." is entered; only its first cluster is parsed and all
// entries inside are reported as deleted.
func TestDeletedDirectory(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	// cluster 8: deleted dir content (FAT entry left free)
	dot := append(mkShort(name11(".", ""), 0x10, 8, 0), mkShort(name11("..", ""), 0x10, 0, 0)...)
	inner := append(dot, mkShort(name11("INSIDE", "TXT"), 0x20, 0, 0)...)
	b.writeCluster(8, inner)
	b.putRootEntries(deleteEnt(mkShort(name11("OLD", ""), 0x10, 8, 0)))
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ents := collect(v)
	if len(ents) != 2 {
		t.Fatalf("got %d entries", len(ents))
	}
	if ents[0].ShortName != "_LD" || !ents[0].Deleted {
		t.Fatalf("dir entry = %+v", ents[0])
	}
	if ents[1].ShortName != "INSIDE.TXT" || !ents[1].Deleted {
		t.Fatalf("inner entry = %+v", ents[1])
	}
	if ents[1].Path != "/_LD/INSIDE.TXT" {
		t.Fatalf("inner path = %q", ents[1].Path)
	}
}

// A deleted directory whose first cluster is NOT free is not entered.
func TestDeletedDirectoryClusterAllocated(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	b.setFAT(8, b.eoc()) // cluster 8 still allocated
	dot := append(mkShort(name11(".", ""), 0x10, 8, 0), mkShort(name11("..", ""), 0x10, 0, 0)...)
	inner := append(dot, mkShort(name11("INSIDE", "TXT"), 0x20, 0, 0)...)
	b.writeCluster(8, inner)
	b.putRootEntries(deleteEnt(mkShort(name11("OLD", ""), 0x10, 8, 0)))
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ents := collect(v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries, want 1 (dir not entered)", len(ents))
	}
}
