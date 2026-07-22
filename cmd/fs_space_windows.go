//go:build windows

package cmd

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func filesystemAvailableBytes(path string) (uint64, error) {
	dir := path
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(
		windows.StringToUTF16Ptr(abs),
		&freeBytesAvailable,
		&totalNumberOfBytes,
		&totalNumberOfFreeBytes,
	)
	if err != nil {
		return 0, err
	}
	return freeBytesAvailable, nil
}
