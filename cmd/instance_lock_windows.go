//go:build windows

package cmd

import (
	"os"

	"golang.org/x/sys/windows"
)

func tryLockFile(file *os.File) error {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&ol,
	)
	if err != nil {
		if err == windows.ERROR_LOCK_VIOLATION || err == windows.ERROR_IO_PENDING {
			return errInstanceLockHeld
		}
		return err
	}
	return nil
}

func unlockFile(file *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &ol)
}
