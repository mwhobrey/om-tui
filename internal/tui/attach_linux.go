//go:build linux

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// linuxCommandRunner is overridable in tests.
var linuxCommandRunner = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// lookPathLinux is overridable in tests.
var lookPathLinux = exec.LookPath

func pickMediaFile() (string, error) {
	if path, err := lookPathLinux("zenity"); err == nil {
		out, err := linuxCommandRunner(path,
			"--file-selection",
			"--title=Attach media",
			"--file-filter=Media files | *.jpg *.jpeg *.png *.gif *.webp *.mp4 *.mov *.webm *.mp3 *.m4a *.wav *.ogg *.pdf",
			"--file-filter=All files | *",
		)
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				return "", nil
			}
			return "", fmt.Errorf("zenity file picker: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	if path, err := lookPathLinux("kdialog"); err == nil {
		out, err := linuxCommandRunner(path,
			"--getopenfilename",
			".",
			"*.jpg *.jpeg *.png *.gif *.webp *.mp4 *.mov *.webm *.mp3 *.m4a *.wav *.ogg *.pdf|Media files\n*|All files",
			"--title", "Attach media",
		)
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				return "", nil
			}
			return "", fmt.Errorf("kdialog file picker: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	if path, err := lookPathLinux("qarma"); err == nil {
		out, err := linuxCommandRunner(path,
			"--file-selection",
			"--title=Attach media",
		)
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				return "", nil
			}
			return "", fmt.Errorf("qarma file picker: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	return "", fmt.Errorf("no supported file picker found (please install zenity or kdialog)")
}

func clipboardMediaPath() (string, bool, error) {
	dir, err := clipboardCacheDir()
	if err != nil {
		return "", false, err
	}
	stamp := time.Now().UnixNano()
	outPath := filepath.Join(dir, fmt.Sprintf("clip-%d.png", stamp))

	// Check Wayland first (wl-paste)
	if isWayland() {
		if path, err := lookPathLinux("wl-paste"); err == nil {
			typesRaw, err := linuxCommandRunner(path, "--list-types")
			if err == nil {
				types := string(typesRaw)
				if strings.Contains(types, "image/png") || strings.Contains(types, "image/jpeg") {
					mime := "image/png"
					if !strings.Contains(types, "image/png") && strings.Contains(types, "image/jpeg") {
						mime = "image/jpeg"
						outPath = filepath.Join(dir, fmt.Sprintf("clip-%d.jpg", stamp))
					}
					data, err := linuxCommandRunner(path, "--type", mime)
					if err == nil && len(data) > 0 {
						if err := os.WriteFile(outPath, data, 0o600); err == nil {
							return outPath, true, nil
						}
					}
				}
				if strings.Contains(types, "text/uri-list") {
					data, err := linuxCommandRunner(path, "--type", "text/uri-list")
					if err == nil {
						if filePath := parseFirstURI(string(data)); filePath != "" {
							if _, err := os.Stat(filePath); err == nil {
								return filePath, true, nil
							}
						}
					}
				}
			}
		}
	}

	// X11 / fallback: xclip
	if path, err := lookPathLinux("xclip"); err == nil {
		targetsRaw, err := linuxCommandRunner(path, "-selection", "clipboard", "-t", "TARGETS", "-out")
		if err == nil {
			targets := string(targetsRaw)
			if strings.Contains(targets, "image/png") {
				data, err := linuxCommandRunner(path, "-selection", "clipboard", "-t", "image/png", "-out")
				if err == nil && len(data) > 0 {
					if err := os.WriteFile(outPath, data, 0o600); err == nil {
						return outPath, true, nil
					}
				}
			}
			if strings.Contains(targets, "text/uri-list") {
				data, err := linuxCommandRunner(path, "-selection", "clipboard", "-t", "text/uri-list", "-out")
				if err == nil {
					if filePath := parseFirstURI(string(data)); filePath != "" {
						if _, err := os.Stat(filePath); err == nil {
							return filePath, true, nil
						}
					}
				}
			}
		}
	}

	return "", false, nil
}

func clipboardText() (string, error) {
	if isWayland() {
		if path, err := lookPathLinux("wl-paste"); err == nil {
			out, err := linuxCommandRunner(path, "--no-newline", "--type", "text/plain")
			if err == nil {
				return string(out), nil
			}
		}
	}

	if path, err := lookPathLinux("xclip"); err == nil {
		out, err := linuxCommandRunner(path, "-selection", "clipboard", "-out")
		if err == nil {
			return strings.TrimRight(string(out), "\r\n"), nil
		}
	}

	if path, err := lookPathLinux("xsel"); err == nil {
		out, err := linuxCommandRunner(path, "--clipboard", "--output")
		if err == nil {
			return strings.TrimRight(string(out), "\r\n"), nil
		}
	}

	return "", nil
}

func isWayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != ""
}
