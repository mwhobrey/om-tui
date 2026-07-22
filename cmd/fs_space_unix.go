//go:build !windows

package cmd

import (
	"fmt"
	"math"
	"syscall"
)

func filesystemAvailableBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	blockSize := uint64(stat.Bsize)
	availableBlocks := uint64(stat.Bavail)
	if blockSize == 0 || availableBlocks > math.MaxUint64/blockSize {
		return 0, fmt.Errorf("filesystem free-space value overflows uint64")
	}
	return availableBlocks * blockSize, nil
}
