//go:build !windows

package tui

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return true
	}
	cmd := strings.ReplaceAll(string(raw), "\x00", " ")
	return strings.Contains(cmd, "serve") && strings.Contains(cmd, "--api")
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func killDaemonTree(pid int) {}
