package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestDataDirsMatch(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "OpenMessage")
	b := filepath.Join(base, "OpenMessage")
	if !dataDirsMatch(a, b) {
		t.Fatalf("expected %q and %q to match", a, b)
	}
	if dataDirsMatch(a, filepath.Join(base, "other")) {
		t.Fatal("expected mismatch")
	}
	if dataDirsMatch("", a) {
		t.Fatal("empty should not match non-empty")
	}
}

func TestReadOwnedPID(t *testing.T) {
	dir := t.TempDir()
	if err := writeOwnedPID(dir, 4242); err != nil {
		t.Fatal(err)
	}
	got, err := readOwnedPID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 4242 {
		t.Fatalf("pid = %d", got)
	}
}

func TestAdoptOwnedDaemonRequiresLivePid(t *testing.T) {
	dir := t.TempDir()
	if err := writeOwnedPID(dir, 1); err != nil {
		t.Fatal(err)
	}
	session := &Session{DataDir: dir}
	adoptOwnedDaemon(session)
	if session.Owned {
		t.Fatal("should not adopt a pid that is not this executable")
	}
}

func TestAdoptOwnedDaemonLeavesLiveOwner(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "30", "127.0.0.1")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	dir := t.TempDir()
	body := "1\n" + strconv.Itoa(cmd.Process.Pid) + "\n"
	if err := os.WriteFile(filepath.Join(dir, ownedDaemonPIDFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	session := &Session{DataDir: dir}
	adoptOwnedDaemon(session)
	if session.Owned {
		t.Fatal("must not steal a daemon from a live TUI owner")
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("current process should look alive")
	}
	if processAlive(0) {
		t.Fatal("pid 0 should not look alive")
	}
}
