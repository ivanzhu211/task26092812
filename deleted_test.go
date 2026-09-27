package fatfs

import (
	"bytes"
	"testing"
)

// buildDeletedLFNImage builds a FAT16 image containing one deleted file
// with a 3-part LFN whose long name starts with '.', stored (logically)
// in clusters 10,11 which are free in the FAT.
func buildDeletedLFNImage(t *testing.T, markCluster11Allocated bool) (string, []byte) {
	t.Helper()
	b := newImg(t, FAT16, 5000)
	content := make([]byte, 600)
	for i := range content {
		content[i] = byte(i % 251)
	}
	b.writeCluster(10, content[:512])
	b.writeCluster(11, content[512:])
	if markCluster11Allocated {
		b.setFAT(11, b.eoc()) // cluster 11 reused by something else
	}

	short := name11("HIDDE~1", "TXT")
	longName := ".hidden important document.txt" // 30 chars -> 3 LFN parts
	ents := lfnEnts(short, longName)
	if len(ents) != 3 {
		t.Fatalf("want 3 LFN parts, got %d", len(ents))
	}
	for i := range ents {
		ents[i] = deleteEnt(ents[i])
	}
	ents = append(ents, deleteEnt(mkShort(short, 0x20, 10, len(content),
		withModDate(2022, 7, 8), withModTime(9, 8, 6))))
	b.putRootEntries(ents...)
	return b.writeImage(), content
}

func TestDeletedLFNFileRecover(t *testing.T) {
	path, content := buildDeletedLFNImage(t, false)
	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	var got *Entry
	if err := v.Walk(func(e *Entry) bool { got = e; return true }); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no entries")
	}
	if !got.Deleted {
		t.Fatal("entry not marked deleted")
	}
	if got.LongName != ".hidden important document.txt" {
		t.Fatalf("long name = %q", got.LongName)
	}
	if got.ShortName != "HIDDE~1.TXT" {
		t.Fatalf("restored short name = %q", got.ShortName)
	}
	if got.Recovery != RecoveryComplete {
		t.Fatalf("recovery = %v, want complete", got.Recovery)
	}

	var buf bytes.Buffer
	if err := v.ReadFile(got, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Fatalf("recovered %d bytes, mismatch vs original %d", buf.Len(), len(content))
	}
}

func TestDeletedLFNFilePartialOverwrite(t *testing.T) {
	path, _ := buildDeletedLFNImage(t, true)
	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	var got *Entry
	if err := v.Walk(func(e *Entry) bool { got = e; return true }); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no entries")
	}
	if got.Recovery != RecoveryPartial {
		t.Fatalf("recovery = %v, want partial", got.Recovery)
	}
}

func TestDeletedNoLFNUnderscore(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	short := name11("NOTES", "TXT")
	b.putRootEntries(deleteEnt(mkShort(short, 0x20, 0, 0)))
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	var got *Entry
	v.Walk(func(e *Entry) bool { got = e; return true })
	if got == nil {
		t.Fatal("no entries")
	}
	if got.ShortName != "_OTES.TXT" {
		t.Fatalf("short name = %q, want _OTES.TXT", got.ShortName)
	}
	if got.LongName != "" {
		t.Fatalf("long name = %q", got.LongName)
	}
}

// A deleted short entry whose preceding LFN checksum cannot be produced
// by any legal first character must be emitted without the LFN, and the
// orphan LFNs produce no entries.
func TestDeletedInvalidLFNDropped(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	short := name11("REPORT", "DOC")
	ents := lfnEnts(short, "some long report name.doc")
	// Corrupt the checksum so the only matching first byte would be the
	// illegal short-name character 0x22 ('"'), invalidating the group.
	bad := short
	bad[0] = 0x22
	badSum := testLFNChecksum(bad)
	for i := range ents {
		ents[i] = deleteEnt(ents[i])
		ents[i][13] = badSum
	}
	ents = append(ents, deleteEnt(mkShort(short, 0x20, 0, 0)))
	b.putRootEntries(ents...)
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	var entries []*Entry
	v.Walk(func(e *Entry) bool { entries = append(entries, e); return true })
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (orphan LFNs dropped)", len(entries))
	}
	if entries[0].LongName != "" {
		t.Fatalf("long name = %q, want empty", entries[0].LongName)
	}
	if entries[0].ShortName != "_EPORT.DOC" {
		t.Fatalf("short name = %q", entries[0].ShortName)
	}
}
