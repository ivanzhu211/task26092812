package fat

import (
	"bytes"
	"testing"
)

// oneFileImage builds a FAT16 image with a single live file at cluster 5.
func oneFileImage(size uint32) *img {
	g := geomFAT16()
	m := newImage(g)
	place(m.rootDir(), shortEnt("FILE    BIN", 0x20, 5, size))
	return m
}

func entryOf(t *testing.T, m *img) (*Volume, *Entry) {
	t.Helper()
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 1 {
		t.Fatalf("got %d entries", len(ents))
	}
	return v, ents[0]
}

func TestChainLoop(t *testing.T) {
	m := oneFileImage(2048) // needs 4 clusters
	m.setFAT(5, 6)
	m.setFAT(6, 5) // cycle
	_, e := entryOf(t, m)
	if !e.HasAnomaly(AnomalyChainLoop) {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

func TestChainThroughFree(t *testing.T) {
	m := oneFileImage(2048)
	m.setFAT(5, 6)
	// FAT[6] = 0: chain passes through a free cluster
	_, e := entryOf(t, m)
	if !e.HasAnomaly(AnomalyChainFree) || !e.HasAnomaly(AnomalyChainShort) {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

func TestChainThroughBad(t *testing.T) {
	m := oneFileImage(2048)
	m.setFAT(5, 6)
	m.setFAT(6, 0xFFF7)
	_, e := entryOf(t, m)
	if !e.HasAnomaly(AnomalyChainBad) {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

func TestChainOutOfRange(t *testing.T) {
	m := oneFileImage(2048)
	m.setFAT(5, 0xF000) // beyond cluster count but below EOC
	_, e := entryOf(t, m)
	if !e.HasAnomaly(AnomalyChainOutOfRange) {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

func TestChainTooShort(t *testing.T) {
	m := oneFileImage(2048)
	m.setFAT(5, 0xFFFF) // EOC after 1 cluster, needs 4
	_, e := entryOf(t, m)
	if !e.HasAnomaly(AnomalyChainShort) {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

func TestChainTooLong(t *testing.T) {
	m := oneFileImage(512) // needs 1 cluster
	m.setFAT(5, 6)
	m.setFAT(6, 0xFFFF)
	_, e := entryOf(t, m)
	if !e.HasAnomaly(AnomalyChainLong) {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

func TestChainClean(t *testing.T) {
	m := oneFileImage(1000)
	m.setFAT(5, 6)
	m.setFAT(6, 0xFFFF)
	_, e := entryOf(t, m)
	if len(e.Anomalies) != 0 {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
}

// ReadFile emits only bytes really reachable and within the size field.
func TestReadFileChain(t *testing.T) {
	m := oneFileImage(1000)
	m.setFAT(5, 6)
	m.setFAT(6, 0xFFFF)
	for i := range m.clus(5) {
		m.clus(5)[i] = 0x41
	}
	for i := range m.clus(6) {
		m.clus(6)[i] = 0x42
	}
	v, e := entryOf(t, m)
	var buf bytes.Buffer
	if err := v.ReadFile(e, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1000 {
		t.Fatalf("got %d bytes", buf.Len())
	}
	if !bytes.Equal(buf.Bytes()[:512], bytes.Repeat([]byte{0x41}, 512)) ||
		!bytes.Equal(buf.Bytes()[512:], bytes.Repeat([]byte{0x42}, 488)) {
		t.Fatal("content mismatch")
	}
}

// ReadFile stops at a broken chain and emits only reachable bytes.
func TestReadFileBrokenChain(t *testing.T) {
	m := oneFileImage(2048)
	m.setFAT(5, 6)
	// FAT[6] free: the link 5->6 is real, the chain ends after cluster 6.
	for i := range m.clus(5) {
		m.clus(5)[i] = 0x41
	}
	v, e := entryOf(t, m)
	var buf bytes.Buffer
	if err := v.ReadFile(e, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1024 {
		t.Fatalf("got %d bytes, want 1024", buf.Len())
	}
}

// A looped chain must not hang ReadFile; output is capped by size.
func TestReadFileLoop(t *testing.T) {
	m := oneFileImage(1500)
	m.setFAT(5, 6)
	m.setFAT(6, 5)
	v, e := entryOf(t, m)
	var buf bytes.Buffer
	if err := v.ReadFile(e, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > 1500 {
		t.Fatalf("got %d bytes, exceeds size", buf.Len())
	}
}

// Truncated image: directory region missing entirely -> no entries, no error.
func TestTruncatedImage(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	place(m.rootDir(), shortEnt("FILE    BIN", 0x20, 5, 10))
	m.data = m.data[:1024] // cut before the root dir region
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 0 {
		t.Fatalf("got %d entries", len(ents))
	}
}

// FAT12 chain walking with odd/even 12-bit packing.
func TestFAT12Chain(t *testing.T) {
	g := geomFAT12()
	m := newImage(g)
	place(m.rootDir(), shortEnt("FILE    BIN", 0x20, 5, 1500))
	m.setFAT(5, 6) // odd cluster
	m.setFAT(6, 7) // even cluster
	m.setFAT(7, 0x0FFF)
	for i := range m.clus(5) {
		m.clus(5)[i] = 0x41
	}
	for i := range m.clus(6) {
		m.clus(6)[i] = 0x42
	}
	for i := range m.clus(7) {
		m.clus(7)[i] = 0x43
	}
	v, e := entryOf(t, m)
	if len(e.Anomalies) != 0 {
		t.Fatalf("anomalies = %v", e.Anomalies)
	}
	var buf bytes.Buffer
	if err := v.ReadFile(e, &buf); err != nil {
		t.Fatal(err)
	}
	want := append(bytes.Repeat([]byte{0x41}, 512), bytes.Repeat([]byte{0x42}, 512)...)
	want = append(want, bytes.Repeat([]byte{0x43}, 476)...)
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("got %d bytes, content mismatch", buf.Len())
	}
}

// A broken chain on one entry must not stop other entries from appearing.
func TestAnomalyDoesNotStopWalk(t *testing.T) {
	g := geomFAT16()
	m := newImage(g)
	bad := shortEnt("BAD     BIN", 0x20, 5, 2048)
	good := shortEnt("GOOD    BIN", 0x20, 0, 0)
	place(m.rootDir(), bad, good)
	m.setFAT(5, 6)
	m.setFAT(6, 5) // loop
	v, err := Open(m.write(t))
	if err != nil {
		t.Fatal(err)
	}
	ents := collect(t, v)
	if len(ents) != 2 {
		t.Fatalf("got %d entries", len(ents))
	}
	if !findEntry(t, ents, "/BAD.BIN").HasAnomaly(AnomalyChainLoop) {
		t.Fatal("BAD.BIN missing chain-loop anomaly")
	}
	if e := findEntry(t, ents, "/GOOD.BIN"); len(e.Anomalies) != 0 {
		t.Fatalf("GOOD.BIN anomalies = %v", e.Anomalies)
	}
}
