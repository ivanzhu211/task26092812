// Package fat implements a read-only forensic parser for FAT12/16/32
// volumes stored as raw dd images (byte 0 is the volume boot sector).
package fat

import (
	"io"
	"time"
)

// Type identifies the FAT variant of a volume. It is determined solely by
// the count of clusters in the data region, per the Microsoft FAT spec.
type Type int

const (
	FAT12 Type = iota
	FAT16
	FAT32
)

func (t Type) String() string {
	switch t {
	case FAT12:
		return "FAT12"
	case FAT16:
		return "FAT16"
	case FAT32:
		return "FAT32"
	}
	return "UNKNOWN"
}

// Recovery describes how completely a deleted file can be recovered.
type Recovery int

const (
	// RecoveryNA applies to entries that are not deleted files.
	RecoveryNA Recovery = iota
	// RecoveryFull: every cluster the file needs is free in the FAT.
	RecoveryFull
	// RecoveryPartial: at least one needed cluster has been reallocated.
	RecoveryPartial
	// RecoveryUnrecoverable: first cluster invalid or range past volume end.
	RecoveryUnrecoverable
)

func (r Recovery) String() string {
	switch r {
	case RecoveryNA:
		return "n/a"
	case RecoveryFull:
		return "full"
	case RecoveryPartial:
		return "partial-overwrite"
	case RecoveryUnrecoverable:
		return "unrecoverable"
	}
	return "unknown"
}

// Anomaly flags a structural problem found while parsing an entry.
type Anomaly string

const (
	AnomalyChainLoop       Anomaly = "chain-loop"         // cluster chain contains a cycle
	AnomalyChainOutOfRange Anomaly = "chain-out-of-range" // cluster number past volume end
	AnomalyChainFree       Anomaly = "chain-through-free" // allocated chain passes a free cluster
	AnomalyChainBad        Anomaly = "chain-through-bad"  // allocated chain passes a bad cluster
	AnomalyChainShort      Anomaly = "chain-too-short"    // chain shorter than size requires
	AnomalyChainLong       Anomaly = "chain-too-long"     // chain longer than size requires
	AnomalyDirCycle        Anomaly = "dir-cycle"          // subdirectory points back to an ancestor
	AnomalyTruncated       Anomaly = "truncated"          // read hit end of image unexpectedly
	AnomalyDepthLimit      Anomaly = "depth-limit"        // nesting too deep, not descended
)

// Timestamp is a FAT timestamp kept exactly as recorded on the volume
// (local time, no timezone conversion). Valid is false when the raw fields
// were zero or out of range; T is then the zero time.
type Timestamp struct {
	T     time.Time
	Valid bool
}

// Entry describes one directory entry, live or deleted.
type Entry struct {
	Path         string // full path, "/" separated, rooted at "/"
	LongName     string // long file name, "" if none (valid)
	ShortName    string // 8.3 name, restored for deleted entries
	Attr         uint8
	Size         uint32
	FirstCluster uint32
	ModTime      Timestamp
	CreateTime   Timestamp // 10ms resolution
	AccessTime   Timestamp
	Deleted      bool
	Recovery     Recovery
	Anomalies    []Anomaly
}

// IsDir reports whether the entry is a directory.
func (e *Entry) IsDir() bool { return e.Attr&0x10 != 0 }

// HasAnomaly reports whether the entry carries the given anomaly.
func (e *Entry) HasAnomaly(a Anomaly) bool {
	for _, x := range e.Anomalies {
		if x == a {
			return true
		}
	}
	return false
}

// Volume is an opened FAT image.
type Volume struct {
	typ          Type
	clusterSize  uint32 // bytes per cluster
	clusterCount uint32 // clusters in the data region

	r            io.ReaderAt
	bytesPerSec  uint32
	secPerClus   uint32
	reservedSecs uint32
	numFATs      uint32
	fatSize      uint32 // sectors per FAT
	rootEntCnt   uint32
	totSec       uint64
	rootClus     uint32 // FAT32 root dir first cluster
	firstDataSec uint64
	fatStartSec  uint32 // start of first FAT
	rootDirSec   uint64 // first sector of FAT12/16 root dir region
	rootDirSecs  uint32 // sectors in FAT12/16 root dir region
}

// Type returns the FAT variant, derived only from the data cluster count.
func (v *Volume) Type() Type { return v.typ }

// ClusterSize returns the cluster size in bytes.
func (v *Volume) ClusterSize() uint32 { return v.clusterSize }

// ClusterCount returns the number of clusters in the data region.
func (v *Volume) ClusterCount() uint32 { return v.clusterCount }

// Open parses the boot sector of the image at path.
func Open(path string) (*Volume, error) {
	return open(path)
}

// Walk invokes fn once for every directory entry in the volume, including
// deleted ones. Returning a non-nil error from fn stops the walk.
func (v *Volume) Walk(fn func(*Entry) error) error {
	return v.walk(fn)
}

// ReadFile writes the entry's content to w. Deleted files are recovered
// under the assumption that clusters were allocated contiguously from the
// first cluster. Only bytes actually reachable and within the size field
// are written.
func (v *Volume) ReadFile(e *Entry, w io.Writer) error {
	return v.readFile(e, w)
}
