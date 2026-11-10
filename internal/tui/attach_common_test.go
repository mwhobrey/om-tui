package tui

import (
	"testing"
)

func TestParseFirstURI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"comments only", "# comment 1\n# comment 2", ""},
		{"single file", "file:///home/user/photo.jpg", "/home/user/photo.jpg"},
		{"multiple files", "# Files:\nfile:///home/user/one.png\nfile:///home/user/two.png", "/home/user/one.png"},
		{"remote share skipped", "file://server/share/x.png\nfile:///tmp/actual.jpg", "/tmp/actual.jpg"},
		{"localhost accepted", "file://localhost/tmp/actual.jpg", "/tmp/actual.jpg"},
		{"http skipped", "https://example.com/image.png\nfile:///tmp/actual.jpg", "/tmp/actual.jpg"},
		{"percent encoded", "file:///home/user/my%20photo.jpg", "/home/user/my photo.jpg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseFirstURI(c.in)
			if got != c.want {
				t.Fatalf("parseFirstURI(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
