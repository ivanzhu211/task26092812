package fatfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Volume is an opened FAT12/16/32 image.
type Volume struct {
	f      *os.File
	closer io.Closer

	fatType     FATType
	clusterSize uint32 // bytes per cluster
	clusterCnt  uint64 // clusters in the data area

	bytesPerSec    uint32
	secPerClus     uint32
	resvdSecCnt    uint32
	numFATs        uint32
	fatSize        uint64 // sectors per FAT
	rootEntCnt     uint32
	totSec         uint64
	rootDirSectors uint64 // sectors occupied by the FAT12/16 root dir
	firstDataSec   uint64
	rootClus       uint32 // FAT32 root cluster
	fatOffset      int64  // byte offset of first FAT
	dataOffset     int64  // byte offset of data area
	imageSize      int64  // actual file size
}

// Open parses the boot sector of the image at path and returns the volume.
func Open(path string) (*Volume, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	v, err := openReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	v.closer = f
	return v, nil
}

func openReader(f *os.File) (*Volume, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var bs [512]byte
	if _, err := f.ReadAt(bs[:], 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	v := &Volume{f: f, imageSize: st.Size()}
	if err := v.parseBPB(&bs); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Volume) parseBPB(bs *[512]byte) error {
	bps := binary.LittleEndian.Uint16(bs[11:13])
	switch bps {
	case 512, 1024, 2048, 4096:
	default:
		return fmt.Errorf("fatfs: invalid bytes per sector %d", bps)
	}
	spc := bs[13]
	if spc == 0 || spc&(spc-1) != 0 {
		return fmt.Errorf("fatfs: invalid sectors per cluster %d", spc)
	}
	v.bytesPerSec = uint32(bps)
	v.secPerClus = uint32(spc)
	v.clusterSize = v.bytesPerSec * v.secPerClus
	if v.clusterSize > 32<<20 {
		return fmt.Errorf("fatfs: implausible cluster size %d", v.clusterSize)
	}
	v.resvdSecCnt = uint32(binary.LittleEndian.Uint16(bs[14:16]))
	if v.resvdSecCnt == 0 {
		return errors.New("fatfs: reserved sector count is zero")
	}
	v.numFATs = uint32(bs[16])
	if v.numFATs == 0 {
		return errors.New("fatfs: number of FATs is zero")
	}
	v.rootEntCnt = uint32(binary.LittleEndian.Uint16(bs[17:19]))
	totSec16 := binary.LittleEndian.Uint16(bs[19:21])
	totSec32 := binary.LittleEndian.Uint32(bs[32:36])
	fatSz16 := binary.LittleEndian.Uint16(bs[22:24])
	fatSz32 := binary.LittleEndian.Uint32(bs[36:40])

	if totSec16 != 0 {
		v.totSec = uint64(totSec16)
	} else {
		v.totSec = uint64(totSec32)
	}
	if fatSz16 != 0 {
		v.fatSize = uint64(fatSz16)
	} else {
		v.fatSize = uint64(fatSz32)
	}
	if v.totSec == 0 {
		return errors.New("fatfs: total sectors is zero")
	}
	if v.fatSize == 0 {
		return errors.New("fatfs: FAT size is zero")
	}
	v.rootClus = binary.LittleEndian.Uint32(bs[44:48])

	v.rootDirSectors = (uint64(v.rootEntCnt)*32 + uint64(v.bytesPerSec) - 1) / uint64(v.bytesPerSec)
	fatSecs := uint64(v.numFATs) * v.fatSize
	nonData := uint64(v.resvdSecCnt) + fatSecs + v.rootDirSectors
	if nonData >= v.totSec {
		return errors.New("fatfs: no room for data area")
	}
	dataSec := v.totSec - nonData
	v.clusterCnt = dataSec / uint64(v.secPerClus)
	if v.clusterCnt < 1 {
		return errors.New("fatfs: no clusters in data area")
	}

	// FAT type is determined ONLY by the count of clusters (Microsoft FAT
	// spec, "FAT Type Determination"). BS_FilSysType and which BPB fields
	// are non-zero are irrelevant.
	switch {
	case v.clusterCnt < 4085:
		v.fatType = FAT12
	case v.clusterCnt < 65525:
		v.fatType = FAT16
	default:
		v.fatType = FAT32
	}

	v.firstDataSec = nonData
	v.fatOffset = int64(v.resvdSecCnt) * int64(v.bytesPerSec)
	v.dataOffset = int64(v.firstDataSec) * int64(v.bytesPerSec)
	return nil
}

// Close releases the underlying file handle.
func (v *Volume) Close() error {
	if v.closer != nil {
		return v.closer.Close()
	}
	return nil
}

// FATType returns the FAT variant, determined only by the data-area
// cluster count per the Microsoft FAT specification.
func (v *Volume) FATType() FATType { return v.fatType }

// ClusterSize returns the cluster size in bytes.
func (v *Volume) ClusterSize() uint32 { return v.clusterSize }

// ClusterCount returns the number of clusters in the data area.
func (v *Volume) ClusterCount() uint64 { return v.clusterCnt }

// clusterOffset returns the byte offset of cluster n (n >= 2).
func (v *Volume) clusterOffset(n uint32) int64 {
	return v.dataOffset + int64(uint64(n-2)*uint64(v.clusterSize))
}

// validCluster reports whether n is a usable cluster number.
func (v *Volume) validCluster(n uint32) bool {
	return n >= 2 && uint64(n) < v.clusterCnt+2
}
