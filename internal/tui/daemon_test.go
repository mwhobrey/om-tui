package tui

import (
	"path/filepath"
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
