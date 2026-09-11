//go:build windows

package tui

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureDaemonProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}

func attachKillOnCloseJob(pid int) io.Closer {
	if pid <= 0 {
		return nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	err = windows.AssignProcessToJobObject(job, proc)
	_ = windows.CloseHandle(proc)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	return jobCloser(job)
}

type jobCloser windows.Handle

func (j jobCloser) Close() error {
	if j == 0 {
		return nil
	}
	return windows.CloseHandle(windows.Handle(j))
}

func processLooksLikeDaemon(pid int) bool {
	if pid <= 0 {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	image, err := windowsProcessImage(pid)
	if err != nil {
		return false
	}
	return sameExePath(self, image)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func windowsProcessImage(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func sameExePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}

func killDaemonTree(pid int) {
	if pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
