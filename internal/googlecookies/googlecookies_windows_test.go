//go:build windows

package googlecookies

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestChromeProfileInUseIgnoresWindowsLockfile(t *testing.T) {
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userData, "lockfile"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Network", "Cookies"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if chromeProfileInUse(profile) {
		t.Fatal("stale User Data\\lockfile should not mean Chrome is open")
	}
}

func TestChromeProfileInUseDetectsHeldWindowsLockfile(t *testing.T) {
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(userData, "lockfile")
	if err := os.WriteFile(lock, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Network", "Cookies"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := windows.UTF16PtrFromString(lock)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.CloseHandle(handle) })

	if !chromeProfileInUse(profile) {
		t.Fatal("held lockfile should mean Chrome is still releasing the profile")
	}
}

func TestShadowUserDataDirCleanupDoesNotDeleteSource(t *testing.T) {
	src := t.TempDir()
	marker := filepath.Join(src, "Local State")
	if err := os.WriteFile(marker, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst, cleanup, err := shadowUserDataDir(src)
	if err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(filepath.Join(dst, "Local State"))
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	if string(copied) != "{}" {
		cleanup()
		t.Fatalf("junction did not see source: %q", copied)
	}
	cleanup()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("cleanup deleted the real Chrome profile: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("junction still present: %v", err)
	}
}

func TestIsSameUserDataDirResolvesJunction(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Local State"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst, cleanup, err := shadowUserDataDir(src)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if !isSameUserDataDir(dst, src) {
		t.Fatalf("junction %q should resolve to %q", dst, src)
	}
}
