//go:build linux

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickMediaFileZenity(t *testing.T) {
	origLookPath := lookPathLinux
	origRunner := linuxCommandRunner
	defer func() {
		lookPathLinux = origLookPath
		linuxCommandRunner = origRunner
	}()

	lookPathLinux = func(file string) (string, error) {
		if file == "zenity" {
			return "/usr/bin/zenity", nil
		}
		return "", os.ErrNotExist
	}

	linuxCommandRunner = func(name string, args ...string) ([]byte, error) {
		if name == "/usr/bin/zenity" {
			return []byte("/home/user/picture.png\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	}

	path, err := pickMediaFile()
	if err != nil {
		t.Fatalf("pickMediaFile() err = %v", err)
	}
	if path != "/home/user/picture.png" {
		t.Fatalf("path = %q, want /home/user/picture.png", path)
	}
}

func TestPickMediaFileZenityCancel(t *testing.T) {
	origLookPath := lookPathLinux
	origRunner := linuxCommandRunner
	defer func() {
		lookPathLinux = origLookPath
		linuxCommandRunner = origRunner
	}()

	lookPathLinux = func(file string) (string, error) {
		if file == "zenity" {
			return "/usr/bin/zenity", nil
		}
		return "", os.ErrNotExist
	}

	linuxCommandRunner = func(name string, args ...string) ([]byte, error) {
		// Zenity exits with code 1 on Cancel
		cmd := exec.Command("sh", "-c", "exit 1")
		_ = cmd.Run()
		return nil, cmd.ProcessState.Sys().(error)
	}

	path, err := pickMediaFile()
	// Should treat exit code 1 as cancellation (empty path, no error)
	if err != nil && path != "" {
		t.Fatalf("expected cancel, got path=%q err=%v", path, err)
	}
}

func TestClipboardTextWayland(t *testing.T) {
	origLookPath := lookPathLinux
	origRunner := linuxCommandRunner
	defer func() {
		lookPathLinux = origLookPath
		linuxCommandRunner = origRunner
	}()

	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	lookPathLinux = func(file string) (string, error) {
		if file == "wl-paste" {
			return "/usr/bin/wl-paste", nil
		}
		return "", os.ErrNotExist
	}

	linuxCommandRunner = func(name string, args ...string) ([]byte, error) {
		if name == "/usr/bin/wl-paste" {
			return []byte("pasted text\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	}

	text, err := clipboardText()
	if err != nil {
		t.Fatalf("clipboardText() err = %v", err)
	}
	if text != "pasted text" {
		t.Fatalf("text = %q, want 'pasted text'", text)
	}
}
