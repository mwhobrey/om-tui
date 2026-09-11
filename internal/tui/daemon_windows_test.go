//go:build windows

package tui

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestProcessLooksLikeDaemonSelf(t *testing.T) {
	if !processLooksLikeDaemon(os.Getpid()) {
		t.Fatal("current process should match our executable")
	}
	if processLooksLikeDaemon(1) {
		t.Fatal("pid 1 should not look like this daemon")
	}
}

func TestKillOnCloseJobReapsChild(t *testing.T) {
	cmd := exec.Command("ping", "-n", "30", "127.0.0.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaper := attachKillOnCloseJob(cmd.Process.Pid)
	if reaper == nil {
		_ = cmd.Process.Kill()
		t.Fatal("expected a job reaper")
	}
	if err := reaper.Close(); err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child still running after job close")
	}
}

func TestProcessDefinitelyGone(t *testing.T) {
	if processDefinitelyGone(windows.ERROR_ACCESS_DENIED) {
		t.Fatal("access denied is unknown, not gone")
	}
	if !processDefinitelyGone(windows.ERROR_INVALID_PARAMETER) {
		t.Fatal("invalid parameter means the pid is gone")
	}
}
