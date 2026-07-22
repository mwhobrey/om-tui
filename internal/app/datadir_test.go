package app

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultDataDirWindowsUsesLocalAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only default path")
	}
	t.Setenv("OPENMESSAGES_DATA_DIR", "")
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	got := DefaultDataDir()
	want := filepath.Join(local, "OpenMessage")
	if got != want {
		t.Fatalf("DefaultDataDir() = %q, want %q", got, want)
	}
}

func TestDefaultDataDirHonorsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENMESSAGES_DATA_DIR", dir)
	if got := DefaultDataDir(); got != dir {
		t.Fatalf("DefaultDataDir() = %q, want %q", got, dir)
	}
}
