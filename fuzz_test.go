package fat

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// fuzzBaseImage builds a small but feature-rich valid FAT12 image: live
// and deleted files, LFNs, a subdirectory, and a deleted directory.
func fuzzBaseImage() []byte {
	g := geomFAT12()
	m := newImage(g)
	short := "HIDDE~1 TXT"
	sum := lfnChecksum([]byte(short))
	var ents [][]byte
	ents = append(ents, lfnEntries(".hidden deleted file 01.txt", sum)...)
	ents = append(ents, shortEnt("LIVE    TXT", 0x20, 7, 100))
	de := shortEnt(short, 0x20, 5, 1000)
	de[0] = 0xE5
	for _, le := range lfnEntries(".hidden deleted file 01.txt", sum) {
		le[0] = 0xE5
		ents = append(ents, le)
	}
	ents = append(ents, de)
	ents = append(ents, shortEnt("SUB        ", 0x10, 8, 0))
	dd := shortEnt("OLDDIR     ", 0x10, 9, 0)
	dd[0] = 0xE5
	ents = append(ents, dd)
	place(m.rootDir(), ents...)
	m.setFAT(5, 6)
	m.setFAT(6, 0x0FFF)
	m.setFAT(7, 0x0FFF)
	m.setFAT(8, 0x0FFF)
	place(m.clus(8),
		dotEnt(".", 8),
		dotEnt("..", 0),
		shortEnt("NESTED  TXT", 0x20, 0, 0),
	)
	place(m.clus(9),
		dotEnt(".", 9),
		dotEnt("..", 0),
		shortEnt("INSIDE  TXT", 0x20, 0, 0),
	)
	return m.data
}

// FuzzOpenWalk mutates a valid image and requires Open and Walk to never
// panic and never hang.
func FuzzOpenWalk(f *testing.F) {
	base := fuzzBaseImage()
	f.Add([]byte{1, 2, 3, 4})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	f.Add(make([]byte, 256))
	f.Fuzz(func(t *testing.T, mut []byte) {
		data := make([]byte, len(base))
		copy(data, base)
		// Apply up to 512 mutations: 3 bytes offset, 1 byte value.
		n := len(mut) / 4
		if n > 512 {
			n = 512
		}
		for i := 0; i < n; i++ {
			off := (uint32(mut[i*4]) | uint32(mut[i*4+1])<<8 | uint32(mut[i*4+2])<<16) % uint32(len(data))
			data[off] = mut[i*4+3]
		}
		p := filepath.Join(t.TempDir(), "fuzz.dd")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		v, err := Open(p)
		if err != nil {
			return
		}
		_ = v.Walk(func(e *Entry) error {
			_ = v.ReadFile(e, io.Discard)
			return nil
		})
	})
}
