package fatfs

import "time"

// decodeFATTime converts packed FAT date/time fields to a Timestamp.
// Zero dates and any out-of-range field yield an invalid (empty) timestamp;
// values are never carried over or normalized by the date library.
func decodeFATTime(date, timeField uint16, tenths uint8) Timestamp {
	if date == 0 {
		return Timestamp{}
	}
	day := int(date & 0x1F)
	month := int((date >> 5) & 0x0F)
	year := int(date>>9) + 1980
	hour := int(timeField >> 11)
	minute := int((timeField >> 5) & 0x3F)
	sec2 := int(timeField & 0x1F)
	if month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) ||
		hour > 23 || minute > 59 || sec2 > 29 || tenths > 199 {
		return Timestamp{}
	}
	sec := sec2 * 2
	nsec := int(tenths) * 10 * int(time.Millisecond)
	return Timestamp{
		Valid: true,
		Time:  time.Date(year, time.Month(month), day, hour, minute, sec, nsec, time.UTC),
	}
}

func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	}
	return 0
}
