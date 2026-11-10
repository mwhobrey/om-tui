package tui

import (
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestExtractFirstURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"check this out https://github.com/openmessage/om-tui", "https://github.com/openmessage/om-tui"},
		{"look at http://example.com/page?x=1&y=2. Great right?", "http://example.com/page?x=1&y=2"},
		{"(see https://wikipedia.org/wiki/Go)", "https://wikipedia.org/wiki/Go"},
		{"no url here at all", ""},
		{"ftp://files.example.com/not-http", ""},
		{"just a domain like google.com without scheme", ""},
	}

	for _, tc := range cases {
		got := extractFirstURL(tc.input)
		if got != tc.want {
			t.Errorf("extractFirstURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatLinkPreview(t *testing.T) {
	preview := &localapi.LinkPreview{
		URL:         "https://example.com/article",
		Title:       "Exciting News",
		Description: "This is a detailed summary of the exciting news article.",
		Domain:      "example.com",
		SiteName:    "Example News",
	}

	lines := formatLinkPreview(preview, 60)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Exciting News") || !strings.Contains(lines[0], "Example News") {
		t.Fatalf("unexpected line 0: %q", lines[0])
	}
	if !strings.Contains(lines[1], "detailed summary") {
		t.Fatalf("unexpected line 1: %q", lines[1])
	}

	// Empty preview returns nil
	if formatLinkPreview(nil, 60) != nil {
		t.Fatal("expected nil for nil preview")
	}
	if formatLinkPreview(&localapi.LinkPreview{}, 60) != nil {
		t.Fatal("expected nil for empty preview")
	}
}

func TestSanitizePreviewText(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Hello World", "Hello World"},
		{"Line 1\nLine 2\tEnd", "Line 1 Line 2 End"},
		{"\x1b]0;Title\x07Malicious", "]0;TitleMalicious"},
		{"\x00\x01\x1fNull bytes and controls", "Null bytes and controls"},
		{"Safe emoji: 🚀 and text", "Safe emoji: 🚀 and text"},
	}
	for _, tc := range cases {
		got := sanitizePreviewText(tc.input)
		if got != tc.want {
			t.Errorf("sanitizePreviewText(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRenderMessagesWithLinkPreview(t *testing.T) {
	msgs := []localapi.Message{
		{
			MessageID:   "m1",
			SenderName:  "Alice",
			Body:        "Check out https://golang.org today!",
			TimestampMS: 1700000000000,
		},
	}
	previews := map[string]*localapi.LinkPreview{
		"https://golang.org": {
			URL:         "https://golang.org",
			Title:       "The Go Programming Language",
			Description: "Go is an open source programming language.",
			Domain:      "golang.org",
		},
	}

	rendered := renderMessages(msgs, 70, -1, nil, "", previews)
	if !strings.Contains(rendered, "https://golang.org") {
		t.Fatalf("expected original body in rendered output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "The Go Programming Language") {
		t.Fatalf("expected link preview title in rendered output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "open source programming language") {
		t.Fatalf("expected link preview description in rendered output, got:\n%s", rendered)
	}
}
