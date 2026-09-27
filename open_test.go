package fat

import "testing"

func TestOpenFAT12(t *testing.T) {
	g := geomFAT12()
	m := newImage(g)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Type() != FAT12 {
		t.Fatalf("type = %v, want FAT12", v.Type())
	}
	if v.ClusterSize() != 512 {
		t.Fatalf("cluster size = %d", v.ClusterSize())
	}
	if uint64(v.ClusterCount()) != g.clusters() {
		t.Fatalf("clusters = %d, want %d", v.ClusterCount(), g.clusters())
	}
}

func TestOpenFAT16(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Type() != FAT16 {
		t.Fatalf("type = %v, want FAT16", v.Type())
	}
	if uint64(v.ClusterCount()) != g.clusters() {
		t.Fatalf("clusters = %d, want %d", v.ClusterCount(), g.clusters())
	}
}

// The FAT32 image lies in BS_FilSysType ("FAT16"); typing must still be
// derived from the data-region cluster count alone.
func TestOpenFAT32WrongSysType(t *testing.T) {
	g := geomFAT32()
	g.sysType = "FAT16   "
	m := newImage(g)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Type() != FAT32 {
		t.Fatalf("type = %v, want FAT32", v.Type())
	}
	if uint64(v.ClusterCount()) != g.clusters() {
		t.Fatalf("clusters = %d, want %d", v.ClusterCount(), g.clusters())
	}
}

// A FAT12 image claiming to be FAT32 in BS_FilSysType is still FAT12.
func TestOpenFAT12WrongSysType(t *testing.T) {
	g := geomFAT12()
	g.sysType = "FAT32   "
	m := newImage(g)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.Type() != FAT12 {
		t.Fatalf("type = %v, want FAT12", v.Type())
	}
}

func TestOpenGarbage(t *testing.T) {
	m := &img{data: make([]byte, 512)}
	for i := range m.data {
		m.data[i] = byte(i * 7)
	}
	if _, err := Open(m.write(t)); err == nil {
		t.Fatal("expected error for garbage boot sector")
	}
}
