package fatfs

import "testing"

func TestOpenFAT12(t *testing.T) {
	b := newImg(t, FAT12, 100)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if v.FATType() != FAT12 {
		t.Fatalf("type = %v, want FAT12", v.FATType())
	}
	if v.ClusterSize() != 512 {
		t.Fatalf("cluster size = %d", v.ClusterSize())
	}
	if v.ClusterCount() != 100 {
		t.Fatalf("clusters = %d, want 100", v.ClusterCount())
	}
}

func TestOpenFAT16(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if v.FATType() != FAT16 {
		t.Fatalf("type = %v, want FAT16", v.FATType())
	}
	if v.ClusterCount() != 5000 {
		t.Fatalf("clusters = %d, want 5000", v.ClusterCount())
	}
}

// The FAT32 image carries a BS_FilSysType string claiming "FAT12   ";
// type determination must ignore it and use the cluster count.
func TestOpenFAT32WrongFilSysType(t *testing.T) {
	b := newImg(t, FAT32, 70000)
	b.filSys = "FAT12   "
	b.writeBoot() // rewrite boot sector with the lying string
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if v.FATType() != FAT32 {
		t.Fatalf("type = %v, want FAT32", v.FATType())
	}
	if v.ClusterCount() != 70000 {
		t.Fatalf("clusters = %d, want 70000", v.ClusterCount())
	}
}

func TestWalkEmptyRoot(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	n := 0
	if err := v.Walk(func(e *Entry) bool { n++; return true }); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("got %d entries from empty root", n)
	}
}
