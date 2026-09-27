// Package fatfs provides read-only forensic parsing of FAT12/16/32 volumes
// from raw dd images.
package fatfs

import "time"

// FATType identifies the FAT variant of a volume. It is determined solely
// by the number of clusters in the data area, per the Microsoft FAT spec.
type FATType int

const (
	FAT12 FATType = iota
	FAT16
	FAT32
)

func (t FATType) String() string {
	switch t {
	case FAT12:
		return "FAT12"
	case FAT16:
		return "FAT16"
	case FAT32:
		return "FAT32"
	}
	return "unknown"
}

// Timestamp is a volume-local timestamp. Zero Valid means the on-disk
// fields were zero or out of range; no normalization is ever applied.
type Timestamp struct {
	Valid bool
	Time  time.Time // wall clock as stored on disk, no timezone conversion
}

// Recovery describes the recoverability of a deleted file.
type Recovery int

const (
	RecoveryNone          Recovery = iota // not a deleted file
	RecoveryComplete                      // all needed clusters are free
	RecoveryPartial                       // at least one needed cluster is allocated
	RecoveryUnrecoverable                 // first cluster invalid or range past volume end
)

func (r Recovery) String() string {
	switch r {
	case RecoveryNone:
		return "none"
	case RecoveryComplete:
		return "complete"
	case RecoveryPartial:
		return "partial"
	case RecoveryUnrecoverable:
		return "unrecoverable"
	}
	return "unknown"
}

// Anomaly is a bitmask of abnormalities detected on an entry.
type Anomaly uint32

const (
	AnomNone           Anomaly = 0
	AnomChainLoop      Anomaly = 1 << (iota - 1) // cluster chain contains a cycle
	AnomChainShort                               // chain shorter than size requires
	AnomChainLong                                // chain longer than size requires
	AnomChainFree                                // chain passes through a free cluster
	AnomChainBad                                 // chain passes through a bad cluster
	AnomClusterOOB                               // cluster number out of volume range
	AnomDirCycle                                 // subdirectory points back to an ancestor
	AnomSizeExcessive                            // size field exceeds what the volume can hold
	AnomImageTruncated                           // image ended before expected data
)

func (a Anomaly) Has(f Anomaly) bool { return a&f != 0 }

// Entry describes a single directory entry (live or deleted).
type Entry struct {
	Path         string // full path from volume root, using long names when available
	LongName     string // long file name, "" if none
	ShortName    string // 8.3 name, restored for deleted entries
	Attr         uint8
	Size         uint32
	FirstCluster uint32
	ModTime      Timestamp
	CreateTime   Timestamp
	AccessTime   Timestamp
	Deleted      bool
	Recovery     Recovery
	Anomalies    Anomaly
}

// IsDir reports whether the entry is a directory.
func (e *Entry) IsDir() bool { return e.Attr&0x10 != 0 }
