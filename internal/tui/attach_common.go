package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var clipboardCachePurgeOnce sync.Once

func clipboardCacheDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "OpenMessage", "clipboard")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	clipboardCachePurgeOnce.Do(func() { purgeOldCacheFiles(dir, mediaCacheMaxAge) })
	return dir, nil
}

// parseFirstURI extracts the first valid local file path from a text/uri-list
// string (RFC 2483), ignoring comments and non-file URIs.
func parseFirstURI(uriList string) string {
	for _, line := range strings.Split(uriList, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "file://") {
			if u, err := url.Parse(line); err == nil {
				return u.Path
			}
		}
	}
	return ""
}
