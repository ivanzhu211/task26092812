package fatfs

import (
	"bytes"
	"testing"
)

func TestWalkLiveFileAndRead(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	content := make([]byte, 600)
	for i := range content {
		content[i] = byte('A' + i%26)
	}
	// file occupies clusters 2,3 chained
	b.setFAT(2, 3)
	b.setFAT(3, b.eoc())
	b.writeCluster(2, content[:512])
	b.writeCluster(3, content[512:])
	name := name11("DATA", "BIN")
	ents := lfnEnts(name, "very important data file.bin")
	ents = append(ents, mkShort(name, 0x20, 2, len(content),
		withModDate(2021, 3, 4), withModTime(10, 20, 30),
		withCreate(2021, 3, 4, 10, 20, 30, 150),
		withAccess(2021, 3, 5)))
	b.putRootEntries(ents...)

	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	var got *Entry
	if err := v.Walk(func(e *Entry) bool {
		got = e
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no entries")
	}
	if got.LongName != "very important data file.bin" {
		t.Fatalf("long name = %q", got.LongName)
	}
	if got.ShortName != "DATA.BIN" {
		t.Fatalf("short name = %q", got.ShortName)
	}
	if got.Path != "/very important data file.bin" {
		t.Fatalf("path = %q", got.Path)
	}
	if got.Deleted || got.Recovery != RecoveryNone {
		t.Fatalf("deleted=%v recovery=%v", got.Deleted, got.Recovery)
	}
	if !got.ModTime.Valid || got.ModTime.Time.Hour() != 10 || got.ModTime.Time.Minute() != 20 {
		t.Fatalf("modtime = %+v", got.ModTime)
	}
	if !got.CreateTime.Valid || got.CreateTime.Time.Nanosecond() != 150*1e6 {
		t.Fatalf("createtime = %+v", got.CreateTime)
	}
	if got.Anomalies != AnomNone {
		t.Fatalf("anomalies = %#x", got.Anomalies)
	}

	var buf bytes.Buffer
	if err := v.ReadFile(got, &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Fatalf("content = %q", buf.Bytes())
	}
}

func TestShortNameCaseFlags(t *testing.T) {
	b := newImg(t, FAT16, 5000)
	name := name11("README", "TXT")
	b.putRootEntries(mkShort(name, 0x20, 0, 0, withNTRes(0x18)))
	v, err := Open(b.writeImage())
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	var got *Entry
	v.Walk(func(e *Entry) bool { got = e; return true })
	if got == nil || got.ShortName != "readme.txt" {
		t.Fatalf("short name = %+v", got)
	}
}
