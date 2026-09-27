package fat

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// open opens the image and parses its BPB. The FAT type is derived solely
// from the count of clusters in the data region; BS_FilSysType and the
// pattern of zero/non-zero BPB fields are never consulted for typing.
func open(path string) (*Volume, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var sec [512]byte
	if _, err := f.ReadAt(sec[:], 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("read boot sector: %w", err)
	}
	v, err := parseBPB(sec[:])
	if err != nil {
		f.Close()
		return nil, err
	}
	v.r = f
	return v, nil
}

func parseBPB(sec []byte) (*Volume, error) {
	le := binary.LittleEndian
	bytesPerSec := uint32(le.Uint16(sec[11:13]))
	secPerClus := uint32(sec[13])
	reserved := uint32(le.Uint16(sec[14:16]))
	numFATs := uint32(sec[16])
	rootEntCnt := uint32(le.Uint16(sec[17:19]))
	totSec16 := uint32(le.Uint16(sec[19:21]))
	fatSz16 := uint32(le.Uint16(sec[22:24]))
	totSec32 := uint64(le.Uint32(sec[32:36]))
	fatSz32 := uint32(le.Uint32(sec[36:40]))
	rootClus := le.Uint32(sec[44:48])

	switch bytesPerSec {
	case 512, 1024, 2048, 4096:
	default:
		return nil, fmt.Errorf("unsupported bytes/sector %d", bytesPerSec)
	}
	if secPerClus == 0 || secPerClus&(secPerClus-1) != 0 {
		return nil, errors.New("sectors/cluster not a power of two")
	}
	if reserved == 0 {
		return nil, errors.New("reserved sector count is zero")
	}
	if numFATs == 0 {
		return nil, errors.New("FAT count is zero")
	}
	fatSize := fatSz16
	if fatSize == 0 {
		fatSize = fatSz32
	}
	if fatSize == 0 {
		return nil, errors.New("FAT size is zero")
	}
	totSec := uint64(totSec16)
	if totSec == 0 {
		totSec = totSec32
	}
	if totSec == 0 {
		return nil, errors.New("total sector count is zero")
	}

	rootDirSecs := (rootEntCnt*32 + bytesPerSec - 1) / bytesPerSec
	firstDataSec := uint64(reserved) + uint64(numFATs)*uint64(fatSize) + uint64(rootDirSecs)
	if firstDataSec >= totSec {
		return nil, errors.New("no data region")
	}
	dataSec := totSec - firstDataSec
	countOfClusters := dataSec / uint64(secPerClus)
	if countOfClusters < 1 || countOfClusters > 0x0FFFFFF4 {
		return nil, fmt.Errorf("implausible cluster count %d", countOfClusters)
	}

	var typ Type
	switch {
	case countOfClusters < 4085:
		typ = FAT12
	case countOfClusters < 65525:
		typ = FAT16
	default:
		typ = FAT32
	}

	return &Volume{
		typ:          typ,
		clusterSize:  bytesPerSec * secPerClus,
		clusterCount: uint32(countOfClusters),
		bytesPerSec:  bytesPerSec,
		secPerClus:   secPerClus,
		reservedSecs: reserved,
		numFATs:      numFATs,
		fatSize:      fatSize,
		rootEntCnt:   rootEntCnt,
		totSec:       totSec,
		rootClus:     rootClus,
		firstDataSec: firstDataSec,
		fatStartSec:  reserved,
		rootDirSec:   uint64(reserved) + uint64(numFATs)*uint64(fatSize),
		rootDirSecs:  rootDirSecs,
	}, nil
}

// readAt reads len(p) bytes at absolute offset off. io.EOF is returned when
// the image is truncated before off+len(p).
func (v *Volume) readAt(p []byte, off uint64) error {
	_, err := v.r.ReadAt(p, int64(off))
	return err
}

// clusterOffset returns the absolute byte offset of cluster n (n >= 2).
func (v *Volume) clusterOffset(n uint32) uint64 {
	return (v.firstDataSec + uint64(n-2)*uint64(v.secPerClus)) * uint64(v.bytesPerSec)
}

var errShortRead = io.ErrUnexpectedEOF
