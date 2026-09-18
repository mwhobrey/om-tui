package migration

import (
	"strings"
	"testing"
)

func TestReadOnlySQLiteDSNNormalizesWindowsDrivePaths(t *testing.T) {
	got := readOnlySQLiteDSN(`C:\Users\inthe\openmessage\messages.db`)
	if !strings.HasPrefix(got, "file:///C:/Users/inthe/openmessage/messages.db?") {
		t.Fatalf("readOnlySQLiteDSN() = %q, want file:///C:/... Windows URL", got)
	}
	if !strings.Contains(got, "mode=ro") {
		t.Fatalf("readOnlySQLiteDSN() = %q, want mode=ro", got)
	}
}

func TestReadOnlySQLiteDSNKeepsPOSIXPaths(t *testing.T) {
	got := readOnlySQLiteDSN("/home/user/.local/share/openmessage/messages.db")
	if !strings.HasPrefix(got, "file:///home/user/.local/share/openmessage/messages.db?") {
		t.Fatalf("readOnlySQLiteDSN() = %q, want POSIX file URL", got)
	}
}
