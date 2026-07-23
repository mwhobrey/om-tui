package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestPaintLineExactWidth(t *testing.T) {
	got := paintLine(mutedStyle, "hello", 10)
	plain := stripForWidthTest(got)
	if runewidth.StringWidth(plain) != 10 {
		t.Fatalf("width = %d, want 10 (%q)", runewidth.StringWidth(plain), plain)
	}
}

func TestPaintLineTruncatesEmojiSafely(t *testing.T) {
	got := paintLine(mutedStyle, "hi 👍🎉 extra text here", 8)
	plain := stripForWidthTest(got)
	if runewidth.StringWidth(plain) != 8 {
		t.Fatalf("width = %d, want 8 (%q)", runewidth.StringWidth(plain), plain)
	}
}

func TestPadLines(t *testing.T) {
	got := padLines("a\nb", 4, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	if runewidth.StringWidth(lines[3]) != 3 {
		t.Fatalf("blank width = %d", runewidth.StringWidth(lines[3]))
	}
}

func stripForWidthTest(s string) string {
	// Drop CSI sequences so runewidth measures the visible cells.
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
