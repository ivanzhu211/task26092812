package fat

import "testing"

// First byte 0x05 means the real first character is 0xE5, not deletion.
func TestLiveFirstByte05(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	se := shortEnt("\x05OO     TXT", 0x20, 0, 0)
	place(m.rootDir(), se)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries", len(ents))
	}
	if ents[0].Deleted {
		t.Fatal("0x05 entry must not be marked deleted")
	}
	if ents[0].ShortName != "\xe5OO.TXT" {
		t.Fatalf("short name = %q", ents[0].ShortName)
	}
}

// A 0x00 first byte ends the directory: later slots produce nothing.
func TestZeroByteStopsDirectory(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	a := shortEnt("AAAA    TXT", 0x20, 0, 0)
	zero := make([]byte, 32)
	b := shortEnt("BBBB    TXT", 0x20, 0, 0)
	place(m.rootDir(), a, zero, b)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 || ents[0].ShortName != "AAAA.TXT" {
		t.Fatalf("got %v entries", ents)
	}
}

// NTRes case flags lower-case the base and/or extension when no LFN exists.
func TestCaseFlags(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	se := shortEnt("FOO     TXT", 0x20, 0, 0)
	se[12] = ntLowerBase | ntLowerExt
	se2 := shortEnt("BAR     JPG", 0x20, 0, 0)
	se2[12] = ntLowerBase
	place(m.rootDir(), se, se2)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if ents[0].ShortName != "foo.txt" {
		t.Fatalf("short name = %q", ents[0].ShortName)
	}
	if ents[1].ShortName != "bar.JPG" {
		t.Fatalf("short name = %q", ents[1].ShortName)
	}
}

// A live short entry combined with valid preceding LFN entries.
func TestLiveLFN(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	short := "LONGF~1 TXT"
	sum := lfnChecksum([]byte(short))
	var ents [][]byte
	ents = append(ents, lfnEntries("long file name.txt", sum)...)
	ents = append(ents, shortEnt(short, 0x20, 0, 0))
	place(m.rootDir(), ents...)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents2 := collect(t, v)
	if len(ents2) != 1 {
		t.Fatalf("got %d entries", len(ents2))
	}
	if ents2[0].LongName != "long file name.txt" {
		t.Fatalf("long name = %q", ents2[0].LongName)
	}
	if ents2[0].ShortName != "LONGF~1.TXT" {
		t.Fatalf("short name = %q", ents2[0].ShortName)
	}
	if ents2[0].Path != "/long file name.txt" {
		t.Fatalf("path = %q", ents2[0].Path)
	}
}

// Orphan LFN entries (no following short entry, broken sequence, or bad
// checksum) are discarded and produce no entries of their own.
func TestOrphanLFNDiscarded(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	sum := lfnChecksum([]byte("FOO     TXT"))
	var ents [][]byte
	// LFNs with no short entry after them.
	ents = append(ents, lfnEntries("orphan name.txt", sum)...)
	// Broken sequence: ord jumps 1 -> 3.
	bad := lfnEntries("another long name.txt", sum)
	bad[1][0] = 0x03
	ents = append(ents, bad...)
	ents = append(ents, shortEnt("FOO     TXT", 0x20, 0, 0))
	place(m.rootDir(), ents...)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents2 := collect(t, v)
	if len(ents2) != 1 {
		t.Fatalf("got %d entries", len(ents2))
	}
	if ents2[0].LongName != "" {
		t.Fatalf("long name = %q, want empty", ents2[0].LongName)
	}
	if ents2[0].ShortName != "FOO.TXT" {
		t.Fatalf("short name = %q", ents2[0].ShortName)
	}
}

// Volume labels are skipped.
func TestVolumeLabelSkipped(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	lbl := shortEnt("MYVOLUME   ", 0x08, 0, 0)
	f := shortEnt("FILE    TXT", 0x20, 0, 0)
	place(m.rootDir(), lbl, f)
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 || ents[0].ShortName != "FILE.TXT" {
		t.Fatalf("got %v", ents)
	}
}
