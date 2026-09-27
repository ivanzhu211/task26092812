package fat

import "testing"

// dotEnt builds a "." or ".." directory entry.
func dotEnt(name string, clus uint32) []byte {
	n := make([]byte, 11)
	for i := range n {
		n[i] = ' '
	}
	copy(n, name)
	e := shortEnt(string(n), 0x10, clus, 0)
	return e
}

// A subdirectory pointing back to an ancestor terminates the walk with an
// annotation instead of recursing forever.
func TestDirCycleAnnotated(t *testing.T) {
	g := geomFAT32()
	m := newImage(g)
	// Root (cluster 2) holds subdir A at cluster 3.
	m.setFAT(2, 0x0FFFFFFF)
	m.setFAT(3, 0x0FFFFFFF)
	place(m.clus(2),
		dotEnt(".", 2),
		dotEnt("..", 0),
		shortEnt("DIRA       ", 0x10, 3, 0),
	)
	// A holds B, whose first cluster points back to the root (an ancestor).
	place(m.clus(3),
		dotEnt(".", 3),
		dotEnt("..", 2),
		shortEnt("DIRB       ", 0x10, 2, 0),
	)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v) // must terminate
	b := findEntry(t, ents, "/DIRA/DIRB")
	if !b.HasAnomaly(AnomalyDirCycle) {
		t.Fatalf("DIRB anomalies = %v, want dir-cycle", b.Anomalies)
	}
	// B must not have been descended into: no /DIRA/DIRB/... entries.
	for _, e := range ents {
		if len(e.Path) > len("/DIRA/DIRB") && e.Path[:len("/DIRA/DIRB/")] == "/DIRA/DIRB/" {
			t.Fatalf("descended into cycling dir: %s", e.Path)
		}
	}
}

// A subdirectory pointing at itself is also a cycle.
func TestDirSelfCycle(t *testing.T) {
	g := geomFAT32()
	m := newImage(g)
	m.setFAT(2, 0x0FFFFFFF)
	m.setFAT(3, 0x0FFFFFFF)
	place(m.clus(2),
		dotEnt(".", 2),
		dotEnt("..", 0),
		shortEnt("DIRA       ", 0x10, 3, 0),
	)
	place(m.clus(3),
		dotEnt(".", 3),
		dotEnt("..", 2),
		shortEnt("SELF       ", 0x10, 3, 0),
	)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	self := findEntry(t, ents, "/DIRA/SELF")
	if !self.HasAnomaly(AnomalyDirCycle) {
		t.Fatalf("anomalies = %v", self.Anomalies)
	}
}

// Normal nested directories recurse and build full paths.
func TestNestedDirs(t *testing.T) {
	g := geomFAT32()
	m := newImage(g)
	m.setFAT(2, 0x0FFFFFFF)
	m.setFAT(3, 0x0FFFFFFF)
	m.setFAT(4, 0x0FFFFFFF)
	place(m.clus(2),
		dotEnt(".", 2),
		dotEnt("..", 0),
		shortEnt("DIRA       ", 0x10, 3, 0),
	)
	place(m.clus(3),
		dotEnt(".", 3),
		dotEnt("..", 2),
		shortEnt("DIRB       ", 0x10, 4, 0),
	)
	place(m.clus(4),
		dotEnt(".", 4),
		dotEnt("..", 3),
		shortEnt("LEAF    TXT", 0x20, 0, 0),
	)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	leaf := findEntry(t, ents, "/DIRA/DIRB/LEAF.TXT")
	if leaf.Deleted {
		t.Fatal("leaf wrongly marked deleted")
	}
}

// A deleted directory with a free first cluster and valid dot entries is
// entered; only its first cluster is parsed and everything inside is
// reported as deleted.
func TestDeletedDirEntered(t *testing.T) {
	g := geomFAT32()
	m := newImage(g)
	m.setFAT(2, 0x0FFFFFFF)
	// Cluster 3 is the deleted dir's first cluster: FAT[3] = 0 (free).
	// Cluster 4 would be its second cluster; must NOT be parsed.
	de := shortEnt("OLDDIR     ", 0x10, 3, 0)
	de[0] = 0xE5
	place(m.clus(2),
		dotEnt(".", 2),
		dotEnt("..", 0),
		de,
	)
	live := shortEnt("INSIDE  TXT", 0x20, 0, 0) // not 0xE5, still "deleted"
	gone := shortEnt("GONE    TXT", 0x20, 0, 0)
	gone[0] = 0xE5
	place(m.clus(3),
		dotEnt(".", 3),
		dotEnt("..", 2),
		live,
		gone,
	)
	// Chain claims cluster 3 continues at 4, but FAT[3]==0 anyway; put a
	// decoy entry in cluster 4 to prove it is not read.
	place(m.clus(4), shortEnt("DECOY   TXT", 0x20, 0, 0))

	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	d := findEntry(t, ents, "/_LDDIR")
	if !d.Deleted || !d.IsDir() {
		t.Fatalf("dir entry: deleted=%v attr=%#x", d.Deleted, d.Attr)
	}
	in := findEntry(t, ents, "/_LDDIR/INSIDE.TXT")
	if !in.Deleted {
		t.Fatal("entry inside deleted dir must be marked deleted")
	}
	g2 := findEntry(t, ents, "/_LDDIR/_ONE.TXT")
	if !g2.Deleted {
		t.Fatal("deleted entry inside deleted dir must be marked deleted")
	}
	for _, e := range ents {
		if e.ShortName == "DECOY.TXT" {
			t.Fatal("parsed past the first cluster of a deleted dir")
		}
	}
}

// A deleted directory whose first cluster is allocated is not entered.
func TestDeletedDirClusterAllocated(t *testing.T) {
	g := geomFAT32()
	m := newImage(g)
	m.setFAT(2, 0x0FFFFFFF)
	m.setFAT(3, 0x0FFFFFFF) // allocated: do not enter
	de := shortEnt("OLDDIR     ", 0x10, 3, 0)
	de[0] = 0xE5
	place(m.clus(2), dotEnt(".", 2), dotEnt("..", 0), de)
	place(m.clus(3),
		dotEnt(".", 3),
		dotEnt("..", 2),
		shortEnt("INSIDE  TXT", 0x20, 0, 0),
	)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries, want only the deleted dir", len(ents))
	}
}

// A deleted directory without valid dot entries is not entered.
func TestDeletedDirBadDots(t *testing.T) {
	g := geomFAT32()
	m := newImage(g)
	m.setFAT(2, 0x0FFFFFFF)
	de := shortEnt("OLDDIR     ", 0x10, 3, 0)
	de[0] = 0xE5
	place(m.clus(2), dotEnt(".", 2), dotEnt("..", 0), de)
	// Cluster 3 is free but does not start with "." and "..".
	place(m.clus(3), shortEnt("INSIDE  TXT", 0x20, 0, 0))
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries, want only the deleted dir", len(ents))
	}
}
