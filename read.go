package fatfs

import (
	"errors"
	"fmt"
	"io"
)

// ReadFile writes the contents of entry to w. Live files follow their
// FAT chain; deleted files are read assuming contiguous allocation from
// the first cluster. Output never exceeds the entry's size field and
// only includes bytes actually reachable in the image.
func (v *Volume) ReadFile(e *Entry, w io.Writer) error {
	remaining := uint64(e.Size)
	if remaining == 0 {
		return nil
	}
	buf := make([]byte, v.clusterSize)
	if e.Deleted {
		if !v.validCluster(e.FirstCluster) {
			return fmt.Errorf("fatfs: invalid first cluster %d", e.FirstCluster)
		}
		c := e.FirstCluster
		for remaining > 0 {
			if !v.validCluster(c) {
				return errImageTruncated
			}
			if err := v.writeCluster(w, c, buf, &remaining); err != nil {
				return err
			}
			c++
		}
		return nil
	}
	ci := v.followChain(e.FirstCluster)
	if len(ci.clusters) == 0 && e.Size > 0 {
		return fmt.Errorf("fatfs: no reachable clusters for %q", e.Path)
	}
	for _, c := range ci.clusters {
		if remaining == 0 {
			break
		}
		if err := v.writeCluster(w, c, buf, &remaining); err != nil {
			return err
		}
	}
	return nil
}

// writeCluster reads cluster c and writes up to *remaining bytes to w.
func (v *Volume) writeCluster(w io.Writer, c uint32, buf []byte, remaining *uint64) error {
	n, err := v.f.ReadAt(buf, v.clusterOffset(c))
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n == 0 {
		return errImageTruncated
	}
	out := buf[:n]
	if uint64(len(out)) > *remaining {
		out = out[:*remaining]
	}
	if _, werr := w.Write(out); werr != nil {
		return werr
	}
	*remaining -= uint64(len(out))
	if err != nil {
		return errImageTruncated
	}
	return nil
}
