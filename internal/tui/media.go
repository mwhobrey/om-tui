package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/maxghenis/openmessage/internal/localapi"
)

// mediaCacheMaxAge bounds how long downloaded/pasted scratch files sit in
// the OS temp dir before a purge cleans them up. These are open/paste
// working copies, not user-requested exports (which go to
// mediaExportDir and are never purged).
const mediaCacheMaxAge = 7 * 24 * time.Hour

var mediaCachePurgeOnce sync.Once

// purgeOldCacheFiles removes regular files in dir whose mtime is older than
// maxAge. Best-effort: errors are ignored since this is opportunistic
// cleanup, not something a user action should ever fail on.
func purgeOldCacheFiles(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func mediaKind(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case mime == "":
		return "file"
	default:
		return "file"
	}
}

func mediaLabel(msg localapi.Message) string {
	kind := mediaKind(msg.MimeType)
	label := "[" + kind + "]"
	if body := strings.TrimSpace(msg.Body); body != "" {
		return label + " " + body
	}
	if mime := strings.TrimSpace(msg.MimeType); mime != "" && kind == "file" {
		return label + " " + mime
	}
	return label
}

func latestMediaMessage(msgs []localapi.Message) (localapi.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].HasMedia() {
			return msgs[i], true
		}
	}
	return localapi.Message{}, false
}

func latestDownloadableMediaMessage(msgs []localapi.Message) (localapi.Message, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].HasDownloadableMedia() {
			return msgs[i], true
		}
	}
	return localapi.Message{}, false
}

func extensionForMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/heic", "image/heif":
		return ".heic"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}

func sanitizeFilenamePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "media"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "media"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func mediaCacheDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "OpenMessage", "media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	mediaCachePurgeOnce.Do(func() { purgeOldCacheFiles(dir, mediaCacheMaxAge) })
	return dir, nil
}

func mediaExportDir() (string, error) {
	if root := strings.TrimSpace(os.Getenv("OPENMESSAGES_EXPORT_DIR")); root != "" {
		dir := filepath.Join(root, "media")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Documents", "OpenMessage", "media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func mediaFilename(msg localapi.Message, contentType string) string {
	mime := msg.MimeType
	if mime == "" {
		mime = contentType
	}
	base := sanitizeFilenamePart(msg.MessageID)
	return base + extensionForMIME(mime)
}

func writeMediaFile(dir string, msg localapi.Message, data []byte, contentType string) (string, error) {
	path := filepath.Join(dir, mediaFilename(msg, contentType))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// openFileImpl is overridden on Windows (attach_windows.go) with a
// PowerShell Start-Process call that can actually surface "no application
// is associated with this file" instead of the rundll32 fire-and-forget
// launch, which reports success as soon as the shell hand-off starts
// regardless of whether anything visibly opened.
var openFileImpl = defaultOpenFile

func openFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	return openFileImpl(path)
}

func defaultOpenFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Run()
	default:
		return exec.Command("xdg-open", path).Run()
	}
}

func materializeMedia(msg localapi.Message, data []byte, contentType string, export bool) (string, error) {
	var (
		dir string
		err error
	)
	if export {
		dir, err = mediaExportDir()
	} else {
		dir, err = mediaCacheDir()
	}
	if err != nil {
		return "", err
	}
	path, err := writeMediaFile(dir, msg, data, contentType)
	if err != nil {
		return "", fmt.Errorf("write media file: %w", err)
	}
	return path, nil
}
