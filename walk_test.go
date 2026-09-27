package fat

import (
	"bytes"
	"testing"
)

// buildDeletedLFNImage builds a FAT16 image holding one deleted file with
// a three-part LFN whose long name starts with '.', plus content clusters.
func buildDeletedLFNImage(t *testing.T) *img {
	t.Helper()
	g := geomFAT16()
	m := newImage(g)

	longName := ".hidden deleted file 01.txt" // 27 chars -> 3 LFN parts
	short := "HIDDE~1 TXT"
	sum := lfnChecksum([]byte(short))

	var ents [][]byte
	for _, le := range lfnEntries(longName, sum) {
		le[0] = 0xE5 // ordinal overwritten by deletion marker
		ents = append(ents, le)
	}
	se := shortEnt(short, 0x20, 5, 1000)
	se[0] = 0xE5
	ents = append(ents, se)
	place(m.rootDir(), ents...)

	// Content: clusters 5 and 6, both free in the FAT.
	for i := range m.clus(5) {
		m.clus(5)[i] = byte(i)
	}
	for i := range m.clus(6) {
		m.clus(6)[i] = byte(i + 1)
	}
	return m
}

func deletedEntry(t *testing.T, m *img) (*Volume, *Entry) {
	t.Helper()
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries, want 1", len(ents))
	}
	return v, ents[0]
}

func TestDeletedLFNRecovery(t *testing.T) {
	m := buildDeletedLFNImage(t)
	v, e := deletedEntry(t, m)

	if !e.Deleted {
		t.Fatal("entry not marked deleted")
	}
	if e.LongName != ".hidden deleted file 01.txt" {
		t.Fatalf("long name = %q", e.LongName)
	}
	if e.ShortName != "HIDDE~1.TXT" {
		t.Fatalf("short name = %q", e.ShortName)
	}
	if e.Recovery != RecoveryFull {
		t.Fatalf("recovery = %v, want full", e.Recovery)
	}
	if e.Path != "/.hidden deleted file 01.txt" {
		t.Fatalf("path = %q", e.Path)
	}

	var buf bytes.Buffer
	if err := v.ReadFile(e, &buf); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 0, 1000)
	for i := 0; i < 512; i++ {
		want = append(want, byte(i))
	}
	for i := 0; i < 488; i++ {
		want = append(want, byte(i+1))
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("recovered %d bytes, content mismatch", buf.Len())
	}
}

func TestDeletedLFNPartialOverwrite(t *testing.T) {
	m := buildDeletedLFNImage(t)
	m.setFAT(6, 0xFFFF) // second content cluster reallocated
	_, e := deletedEntry(t, m)
	if e.Recovery != RecoveryPartial {
		t.Fatalf("recovery = %v, want partial-overwrite", e.Recovery)
	}
}

func TestDeletedLFNUnrecoverable(t *testing.T) {
	m := buildDeletedLFNImage(t)
	// Point the first cluster past the end of the volume.
	root := m.rootDir()
	// short entry is the 4th entry (3 LFNs before it)
	se := root[3*32 : 4*32]
	se[26], se[27] = 0xFF, 0xFF
	_, e := deletedEntry(t, m)
	if e.Recovery != RecoveryUnrecoverable {
		t.Fatalf("recovery = %v, want unrecoverable", e.Recovery)
	}
}

// A deleted entry whose LFN checksum does not match loses the long name
// and gets "_" as first short-name character.
func TestDeletedBadChecksum(t *testing.T) {
	m := buildDeletedLFNImage(t)
	root := m.rootDir()
	root[13]++ // corrupt first LFN entry's checksum
	_, e := deletedEntry(t, m)
	if e.LongName != "" {
		t.Fatalf("long name = %q, want empty", e.LongName)
	}
	if e.ShortName != "_IDDE~1.TXT" {
		t.Fatalf("short name = %q", e.ShortName)
	}
}

// A deleted entry with no LFN at all gets "_" as first character.
func TestDeletedNoLFN(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	se := shortEnt("FOO     TXT", 0x20, 0, 0)
	se[0] = 0xE5
	place(m.rootDir(), se)
	_, e := deletedEntry(t, m)
	if e.ShortName != "_OO.TXT" {
		t.Fatalf("short name = %q", e.ShortName)
	}
	if e.LongName != "" {
		t.Fatalf("long name = %q", e.LongName)
	}
}

// Deleted file whose contiguous range runs past the volume end.
func TestDeletedRangePastVolumeEnd(t *testing.T) {
	m := buildDeletedLFNImage(t)
	root := m.rootDir()
	se := root[3*32 : 4*32]
	// Huge size: first cluster valid, but first+needed-1 exceeds the volume.
	se[28], se[29], se[30], se[31] = 0xFF, 0xFF, 0xFF, 0xFF
	_, e := deletedEntry(t, m)
	if e.Recovery != RecoveryUnrecoverable {
		t.Fatalf("recovery = %v, want unrecoverable", e.Recovery)
	}
}
