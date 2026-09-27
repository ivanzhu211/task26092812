package fat

import "testing"

func TestTimestampZero(t *testing.T) {
	if ts := decodeDateTime(0, 0, 0); ts.Valid {
		t.Fatal("date 0 must be invalid")
	}
}

func TestTimestampOutOfRange(t *testing.T) {
	cases := []struct{ date, tm uint16 }{
		{(45 << 9) | (13 << 5) | 1, 0},        // month 13
		{(45 << 9) | (0 << 5) | 1, 0},         // month 0
		{(45 << 9) | (1 << 5) | 0, 0},         // day 0
		{(45 << 9) | (2 << 5) | 30, 0},        // Feb 30
		{(45 << 9) | (1 << 5) | 32, 0},        // day 32 -> masked to 0? 31 max in 5 bits; 32 wraps
		{(45 << 9) | (1 << 5) | 15, 24 << 11}, // hour 24
		{(45 << 9) | (1 << 5) | 15, 60 << 5},  // minute 60
		{(45 << 9) | (1 << 5) | 15, 30},       // sec field 30 -> 60s
	}
	for i, c := range cases {
		if ts := decodeDateTime(c.date, c.tm, 0); ts.Valid {
			t.Fatalf("case %d: date=%#x time=%#x must be invalid, got %v", i, c.date, c.tm, ts.T)
		}
	}
	// 10ms field out of range
	if ts := decodeDateTime((45<<9)|(1<<5)|15, 0, 200); ts.Valid {
		t.Fatal("tenth=200 must be invalid")
	}
}

func TestTimestampValid(t *testing.T) {
	// 2025-02-28 13:45:22, tenth=7 -> 70ms
	date := uint16((45 << 9) | (2 << 5) | 28)
	tm := uint16((13 << 11) | (45 << 5) | 11)
	ts := decodeDateTime(date, tm, 7)
	if !ts.Valid {
		t.Fatal("must be valid")
	}
	got := ts.T.Format("2006-01-02 15:04:05.000")
	if got != "2025-02-28 13:45:22.070" {
		t.Fatalf("got %s", got)
	}
	// Leap day 2024-02-29 is valid and must not normalize to Mar 1.
	ts = decodeDateTime(uint16((44<<9)|(2<<5)|29), 0, 0)
	if !ts.Valid || ts.T.Day() != 29 || ts.T.Month() != 2 {
		t.Fatalf("leap day: %v", ts.T)
	}
	// 2023-02-29 is invalid, must not normalize.
	if ts := decodeDateTime(uint16((43<<9)|(2<<5)|29), 0, 0); ts.Valid {
		t.Fatal("2023-02-29 must be invalid")
	}
}
