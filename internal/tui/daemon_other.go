//go:build !windows

package tui

import (
	"io"
	"os/exec"
	"syscall"
)

func configureDaemonProc(cmd *exec.Cmd) {}

func attachKillOnCloseJob(pid int) io.Closer {
	return nil
}

func processLooksLikeDaemon(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func killDaemonTree(pid int) {}
