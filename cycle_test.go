package fatfs

import (
	"testing"
	"time"
)

// Root contains SUB (cluster 5); SUB contains CHILD (cluster 6); CHILD
// contains BACK whose first cluster points back to SUB's cluster 5.
// Walk must terminate and BACK must carry AnomDirCycle.
func TestDirCycleToAncestor(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	b.setFAT(5, b.eoc())
	b.setFAT(6, b.eoc())

	dot := func(self, parent int) []byte {
		d := mkShort(name11(".", ""), 0x10, self, 0)
		dd := mkShort(name11("..", ""), 0x10, parent, 0)
		return append(d, dd...)
	}
	// SUB's cluster: dot entries + CHILD
	c5 := append(dot(5, 0), mkShort(name11("CHILD", ""), 0x10, 6, 0)...)
	b.writeCluster(5, c5)
	// CHILD's cluster: dot entries + BACK -> cluster 5 (ancestor)
	c6 := append(dot(6, 5), mkShort(name11("BACK", ""), 0x10, 5, 0)...)
	b.writeCluster(6, c6)

	b.putRootEntries(mkShort(name11("SUB", ""), 0x10, 5, 0))

	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	var entries []*Entry
	done := make(chan struct{})
	go func() {
		v.Walk(func(e *Entry) bool {
			entries = append(entries, e)
			return true
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Walk did not terminate (directory cycle)")
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %v", len(entries), entries)
	}
	var back *Entry
	for _, e := range entries {
		if e.ShortName == "BACK" {
			back = e
		}
	}
	if back == nil {
		t.Fatal("BACK entry not found")
	}
	if !back.Anomalies.Has(AnomDirCycle) {
		t.Fatalf("BACK anomalies = %#x, want AnomDirCycle", back.Anomalies)
	}
	if back.Path != "/SUB/CHILD/BACK" {
		t.Fatalf("BACK path = %q", back.Path)
	}
}
