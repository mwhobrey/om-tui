package googlecookies

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func lookPathChrome() (string, error) {
	// Well-known Chrome install paths first. Edge is often easier to find on
	// PATH, and launching Edge against a Chrome profile never starts DevTools.
	for _, candidate := range chromeWellKnownPaths() {
		if isEdgeBinary(candidate) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	for _, name := range chromeBinaryNames() {
		if isEdgeBinary(name) {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	for _, candidate := range chromeWellKnownPaths() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	for _, name := range chromeBinaryNames() {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errChromeNotFound
}

func isEdgeBinary(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.Contains(base, "msedge") || strings.Contains(base, "microsoft edge")
}

func chromeBinaryNames() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"chrome.exe", "msedge.exe"}
	case "darwin":
		return []string{"Google Chrome", "Chromium", "Microsoft Edge"}
	default:
		return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"}
	}
}

func chromeWellKnownPaths() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		seen := map[string]bool{}
		add := func(path string) {
			if path == "" || seen[path] {
				return
			}
			seen[path] = true
			out = append(out, path)
		}
		for _, root := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LOCALAPPDATA"),
			`C:\Program Files`,
			`C:\Program Files (x86)`,
		} {
			if strings.TrimSpace(root) == "" {
				continue
			}
			add(filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"))
			add(filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
		return out
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		return nil
	}
}
