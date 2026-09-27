package fatfs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fuzzBase builds a deterministic valid image of the given variant,
// containing a live LFN file, a deleted file and a subdirectory.
func fuzzBase(variant int) []byte {
	var b *imgBuilder
	t := &testing.T{}
	switch variant {
	case 0:
		b = newImg(t, FAT12, 200)
	case 1:
		b = newImg(t, FAT16, 4086)
	default:
		b = newImg(t, FAT32, 65526)
	}
	// live file with LFN in clusters 2..3
	b.setFAT(2, 3)
	b.setFAT(3, b.eoc())
	b.writeCluster(2, []byte("live file content cluster one"))
	b.writeCluster(3, []byte("cluster two"))
	// subdir at cluster 4 with a deleted file inside
	b.setFAT(4, b.eoc())
	dot := append(mkShort(name11(".", ""), 0x10, 4, 0), mkShort(name11("..", ""), 0x10, 0, 0)...)
	sub := append(dot, deleteEnt(mkShort(name11("INNER", "DAT"), 0x20, 5, 10))...)
	b.writeCluster(4, sub)
	// deleted file content assumed at cluster 5
	b.writeCluster(5, []byte("deleted!"))

	name := name11("LIVE", "TXT")
	ents := lfnEnts(name, "live long file name.txt")
	ents = append(ents, mkShort(name, 0x20, 2, 41, withModDate(2020, 1, 2)))
	ents = append(ents, deleteEnt(mkShort(name11("GONE", "BIN"), 0x20, 5, 8)))
	ents = append(ents, mkShort(name11("SUBDIR", ""), 0x10, 4, 0))
	b.putRootEntries(ents...)
	return b.data
}

// FuzzOpenWalk mutates a valid image and requires Open and Walk to
// neither panic nor hang.
func FuzzOpenWalk(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{1, 255, 0, 0, 10, 20, 30})
	f.Add([]byte{2, 0xE5, 0xE5, 0xE5})
	f.Fuzz(func(t *testing.T, mut []byte) {
		if len(mut) == 0 {
			return
		}
		variant := int(mut[0]) % 3
		img := fuzzBase(variant)
		// apply mutations as (offset16, xor) triples
		for i := 1; i+2 < len(mut); i += 3 {
			off := int(binary.LittleEndian.Uint16(mut[i:i+2])) % len(img)
			img[off] ^= mut[i+2]
		}
		path := filepath.Join(t.TempDir(), "fuzz.dd")
		if err := os.WriteFile(path, img, 0o644); err != nil {
			t.Fatal(err)
		}
		v, err := Open(path)
		if err != nil {
			return
		}
		defer v.Close()
		n := 0
		v.Walk(func(e *Entry) bool {
			n++
			return n < 100000
		})
	})
}
