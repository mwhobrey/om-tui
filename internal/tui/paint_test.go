package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPaintLineExactWidth(t *testing.T) {
	got := paintLine(mutedStyle, "hello", 10)
	plain := stripForWidthTest(got)
	if cellWidth(plain) != 10 {
		t.Fatalf("width = %d, want 10 (%q)", cellWidth(plain), plain)
	}
}

func TestPaintLineTruncatesEmojiSafely(t *testing.T) {
	got := paintLine(mutedStyle, "hi 👍🎉 extra text here", 8)
	plain := stripForWidthTest(got)
	if cellWidth(plain) != 8 {
		t.Fatalf("width = %d, want 8 (%q)", cellWidth(plain), plain)
	}
}

func TestPaintLineHeartEmojiMatchesTerminal(t *testing.T) {
	// ❤️ is the classic go-runewidth trap (counts 1; WT/lipgloss count 2).
	got := paintLine(mutedStyle, "caption and ❤️", 20)
	plain := stripForWidthTest(got)
	if cellWidth(plain) != 20 {
		t.Fatalf("width = %d, want 20 (%q)", cellWidth(plain), plain)
	}
	if lipgloss.Width(got) != 20 {
		t.Fatalf("lipgloss width = %d, want 20", lipgloss.Width(got))
	}
}

func TestPadLines(t *testing.T) {
	got := padLines("a\nb", 4, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	if cellWidth(lines[3]) != 3 {
		t.Fatalf("blank width = %d", cellWidth(lines[3]))
	}
}

func TestPadViewBoxExactDimensions(t *testing.T) {
	got := padViewBox("a\nbb", 4, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	for i, line := range lines {
		if w := cellWidth(stripForWidthTest(line)); w != 4 {
			t.Fatalf("line %d width = %d (%q)", i, w, stripForWidthTest(line))
		}
	}
}

func stripForWidthTest(s string) string {
	// Drop CSI sequences so cellWidth measures the visible cells.
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
