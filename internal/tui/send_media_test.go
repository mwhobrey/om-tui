package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLooksLikeExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, ok := looksLikeExistingFile(`"` + path + `"`)
	if !ok || got != path {
		t.Fatalf("quoted path: got (%q, %v)", got, ok)
	}

	if _, ok := looksLikeExistingFile(filepath.Join(dir, "missing.jpg")); ok {
		t.Fatal("missing file should not match")
	}
	if _, ok := looksLikeExistingFile("hello world"); ok {
		t.Fatal("plain text should not match")
	}
	if _, ok := looksLikeExistingFile(dir); ok {
		t.Fatal("directory should not match")
	}
}

func TestExpandUserPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := expandUserPath(`~\Pictures\a.png`)
	want := filepath.Join(home, "Pictures", "a.png")
	if got != want {
		t.Fatalf("expand ~ = %q, want %q", got, want)
	}
	got = expandUserPath(`%USERPROFILE%\Documents\b.jpg`)
	want = filepath.Join(home, "Documents", "b.jpg")
	if got != want {
		t.Fatalf("expand USERPROFILE = %q, want %q", got, want)
	}
}

func TestDetectSendMIME(t *testing.T) {
	if got := detectSendMIME("shot.png", nil); got != "image/png" {
		t.Fatalf("png ext = %q", got)
	}
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if got := detectSendMIME("blob", pngHeader); !strings.HasPrefix(got, "image/") {
		t.Fatalf("sniff = %q", got)
	}
}

func TestCaptionForAttach(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jpg")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := captionForAttach("hello"); got != "hello" {
		t.Fatalf("caption = %q", got)
	}
	if got := captionForAttach(path); got != "" {
		t.Fatalf("path caption = %q, want empty", got)
	}
}
