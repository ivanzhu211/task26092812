package fat

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

const (
	attrReadOnly = 0x01
	attrHidden   = 0x02
	attrSystem   = 0x04
	attrVolumeID = 0x08
	attrDir      = 0x10
	attrArchive  = 0x20
	attrLFN      = 0x0F // LFN entries have the low four bits all set

	ntLowerBase = 0x08 // NTRes: basename is lowercase
	ntLowerExt  = 0x10 // NTRes: extension is lowercase
)

// lfnChecksum computes the LFN checksum over the 11-byte short name.
func lfnChecksum(short []byte) byte {
	var sum byte
	for i := 0; i < 11; i++ {
		sum = ((sum & 1) << 7) + (sum >> 1) + short[i]
	}
	return sum
}

// lfnChars extracts the 13 UTF-16 code units of an LFN entry.
func lfnChars(ent []byte) [13]uint16 {
	le := binary.LittleEndian
	var out [13]uint16
	pos := 0
	put := func(off int) {
		out[pos] = le.Uint16(ent[off : off+2])
		pos++
	}
	for _, off := range []int{1, 3, 5, 7, 9} {
		put(off)
	}
	for _, off := range []int{14, 16, 18, 20, 22, 24} {
		put(off)
	}
	for _, off := range []int{28, 30} {
		put(off)
	}
	return out
}

// assembleLFN decodes LFN parts (in name order) into a string, stopping at
// the 0x0000 terminator and skipping 0xFFFF padding.
func assembleLFN(parts [][13]uint16) string {
	var units []uint16
	for _, p := range parts {
		for _, u := range p {
			if u == 0x0000 {
				return string(utf16.Decode(units))
			}
			if u == 0xFFFF {
				continue
			}
			units = append(units, u)
		}
	}
	return string(utf16.Decode(units))
}

// legalShortByte reports whether b may appear in an 8.3 short name.
func legalShortByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b >= 0x80: // DBCS lead bytes etc.
		return true
	}
	switch b {
	case '$', '%', '\'', '-', '_', '@', '~', '`', '!', '(', ')', '{', '}', '^', '#', '&':
		return true
	}
	return false
}

// deriveFirstShortChar finds the character a short name generated from lfn
// would start with: the first character whose upcased form is a legal
// short-name byte. ok is false when no such character exists.
func deriveFirstShortChar(lfn string) (c byte, ok bool) {
	for _, r := range lfn {
		if r > 0xFF {
			continue
		}
		b := byte(r)
		if b >= 'a' && b <= 'z' {
			b -= 'a' - 'A'
		}
		if legalShortByte(b) {
			return b, true
		}
	}
	return 0, false
}

// formatShortName renders an 11-byte raw short name as "BASE.EXT",
// applying the NTRes case flags. firstByte, when nonzero, replaces the raw
// first byte (used to restore deleted entries).
func formatShortName(raw []byte, ntres byte, firstByte byte) string {
	base := make([]byte, 8)
	copy(base, raw[0:8])
	ext := make([]byte, 3)
	copy(ext, raw[8:11])
	if firstByte != 0 {
		base[0] = firstByte
	}
	if ntres&ntLowerBase != 0 {
		lowerASCII(base)
	}
	if ntres&ntLowerExt != 0 {
		lowerASCII(ext)
	}
	b := strings.TrimRight(string(base), " ")
	e := strings.TrimRight(string(ext), " ")
	if e == "" {
		return b
	}
	return b + "." + e
}

func lowerASCII(s []byte) {
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			s[i] = c + ('a' - 'A')
		}
	}
}

// parseShortFields extracts the fixed fields of a short directory entry.
func parseShortFields(ent []byte, e *Entry) {
	le := binary.LittleEndian
	e.Attr = ent[11]
	e.Size = le.Uint32(ent[28:32])
	first := uint32(le.Uint16(ent[26:28]))
	if v := le.Uint16(ent[20:22]); v != 0 { // FAT32 high word
		first |= uint32(v) << 16
	}
	e.FirstCluster = first
	e.CreateTime = decodeDateTime(le.Uint16(ent[16:18]), le.Uint16(ent[14:16]), ent[13])
	e.AccessTime = decodeDate(le.Uint16(ent[18:20]))
	e.ModTime = decodeDateTime(le.Uint16(ent[24:26]), le.Uint16(ent[22:24]), 0)
}
