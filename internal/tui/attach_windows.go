//go:build windows

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// powershellRunner is overridable in tests.
var powershellRunner = func(script string) (string, error) {
	cmd := exec.Command("powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func pickMediaFile() (string, error) {
	out, err := powershellRunner(openFileDialogScript())
	if err != nil {
		return "", fmt.Errorf("file picker: %w", err)
	}
	path := strings.TrimSpace(out)
	if path == "" || strings.EqualFold(path, "CANCEL") {
		return "", nil
	}
	return path, nil
}

func clipboardMediaPath() (string, bool, error) {
	dir, err := clipboardCacheDir()
	if err != nil {
		return "", false, err
	}
	stamp := time.Now().UnixNano()
	outPath := filepath.Join(dir, fmt.Sprintf("clip-%d.png", stamp))
	out, err := powershellRunner(clipboardMediaScript(outPath))
	if err != nil {
		return "", false, fmt.Errorf("clipboard media: %w", err)
	}
	result := strings.TrimSpace(out)
	if result == "" || strings.EqualFold(result, "NONE") {
		return "", false, nil
	}
	if _, err := os.Stat(result); err != nil {
		return "", false, fmt.Errorf("clipboard media missing: %w", err)
	}
	return result, true, nil
}

func clipboardCacheDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "OpenMessage", "clipboard")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func openFileDialogScript() string {
	return `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Title = 'Attach media'
$dialog.Filter = 'Media|*.jpg;*.jpeg;*.png;*.gif;*.webp;*.heic;*.bmp;*.mp4;*.mov;*.webm;*.m4v;*.mp3;*.m4a;*.aac;*.wav;*.ogg;*.pdf|Images|*.jpg;*.jpeg;*.png;*.gif;*.webp;*.heic;*.bmp|Videos|*.mp4;*.mov;*.webm;*.m4v|All files|*.*'
$dialog.Multiselect = $false
$dialog.CheckFileExists = $true
if ($dialog.ShowDialog() -ne [System.Windows.Forms.DialogResult]::OK) {
  Write-Output 'CANCEL'
  exit 0
}
Write-Output $dialog.FileName
`
}

func clipboardMediaScript(savePNGPath string) string {
	// Escape single quotes for PowerShell single-quoted string.
	escaped := strings.ReplaceAll(savePNGPath, "'", "''")
	return `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$savePath = '` + escaped + `'
if ([System.Windows.Forms.Clipboard]::ContainsFileDropList()) {
  $files = [System.Windows.Forms.Clipboard]::GetFileDropList()
  if ($files -ne $null -and $files.Count -gt 0) {
    Write-Output $files[0]
    exit 0
  }
}
if ([System.Windows.Forms.Clipboard]::ContainsImage()) {
  $img = [System.Windows.Forms.Clipboard]::GetImage()
  if ($img -eq $null) {
    Write-Output 'NONE'
    exit 0
  }
  $img.Save($savePath, [System.Drawing.Imaging.ImageFormat]::Png)
  $img.Dispose()
  Write-Output $savePath
  exit 0
}
Write-Output 'NONE'
`
}

func clipboardText() (string, error) {
	out, err := powershellRunner(clipboardTextScript())
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\r\n"), nil
}

func clipboardTextScript() string {
	return `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
if (-not [System.Windows.Forms.Clipboard]::ContainsText()) {
  exit 0
}
[Console]::Out.Write([System.Windows.Forms.Clipboard]::GetText())
`
}
