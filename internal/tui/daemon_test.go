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
