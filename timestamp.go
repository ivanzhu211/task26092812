package fat

import "time"

// daysInMonth for non-leap/leap years.
func daysInMonth(year int, m time.Month) int {
	switch m {
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	}
	return 31
}

// decodeDateTime converts a raw FAT date/time pair to a Timestamp kept
// exactly as recorded (local time, no timezone conversion). date==0 or any
// out-of-range field yields an invalid (empty) Timestamp; values are never
// normalized by the date library. tenth is the 10ms creation-time
// resolution field (pass 0 when not applicable).
func decodeDateTime(date, tm uint16, tenth uint8) Timestamp {
	if date == 0 {
		return Timestamp{}
	}
	year := int(date>>9) + 1980
	month := time.Month((date >> 5) & 0xF)
	day := int(date & 0x1F)
	if month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return Timestamp{}
	}
	hour := int(tm >> 11)
	min := int((tm >> 5) & 0x3F)
	sec := int(tm&0x1F) * 2
	if hour > 23 || min > 59 || sec > 59 {
		return Timestamp{}
	}
	nsec := 0
	if tenth != 0 {
		if tenth > 199 {
			return Timestamp{}
		}
		nsec = int(tenth) * 10 * int(time.Millisecond)
	}
	return Timestamp{
		T:     time.Date(year, month, day, hour, min, sec, nsec, time.UTC),
		Valid: true,
	}
}

// decodeDate converts a date-only FAT field (access date).
func decodeDate(date uint16) Timestamp {
	return decodeDateTime(date, 0, 0)
}
