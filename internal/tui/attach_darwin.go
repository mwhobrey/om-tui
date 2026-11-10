//go:build darwin

package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// osascriptRunner is overridable in tests.
var osascriptRunner = func(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

// pbpasteRunner is overridable in tests.
var pbpasteRunner = func() (string, error) {
	cmd := exec.Command("pbpaste")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func pickMediaFile() (string, error) {
	out, err := osascriptRunner(openFileDialogScriptMacOS())
	if err != nil {
		return "", fmt.Errorf("file picker: %w", err)
	}
	path := strings.TrimSpace(out)
	if path == "" || strings.EqualFold(path, "CANCEL") {
		return "", nil
	}
	return path, nil
}

func openFileDialogScriptMacOS() string {
	return `try
set chosen to choose file with prompt "Attach media"
return POSIX path of chosen
on error number -128
return "CANCEL"
end try`
}

func clipboardMediaPath() (string, bool, error) {
	dir, err := clipboardCacheDir()
	if err != nil {
		return "", false, err
	}
	stamp := time.Now().UnixNano()
	outPath := filepath.Join(dir, fmt.Sprintf("clip-%d.png", stamp))
	out, err := osascriptRunner(clipboardMediaScriptMacOS(outPath))
	if err != nil {
		return "", false, fmt.Errorf("clipboard media: %w", err)
	}
	result := strings.TrimSpace(out)
	if result == "" || strings.EqualFold(result, "NONE") {
		return "", false, nil
	}
	if strings.HasPrefix(result, "file://") {
		if u, err := url.Parse(result); err == nil {
			result = u.Path
		}
	}
	if _, err := os.Stat(result); err != nil {
		return "", false, fmt.Errorf("clipboard media missing: %w", err)
	}
	return result, true, nil
}

func clipboardMediaScriptMacOS(savePNGPath string) string {
	escaped := strings.ReplaceAll(savePNGPath, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `try
set clipInfo to (clipboard info) as string
if clipInfo contains "«class furl»" then
	set f to (the clipboard as «class furl»)
	tell application "System Events" to return POSIX path of disk item (f as text)
end if
if clipInfo contains "class PNG " or clipInfo contains "TIFF picture" or clipInfo contains "«class PNG »" then
	set pngData to the clipboard as «class PNG »
	set fp to open for access POSIX file "` + escaped + `" with write permission
	set eof fp to 0
	write pngData to fp
	close access fp
	return "` + escaped + `"
end if
on error
	try
		close access POSIX file "` + escaped + `"
	end try
end try
return "NONE"`
}

func clipboardText() (string, error) {
	out, err := pbpasteRunner()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\r\n"), nil
}
