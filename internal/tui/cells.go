package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// cellWidth returns terminal cell width using grapheme clustering, matching
// lipgloss and Windows Terminal. go-runewidth under-counts emoji presentation
// sequences like ❤️ (U+2764+U+FE0F) as 1 cell; WT renders them as 2, which
// made paintLine under-pad and left ghosts when the contact list scrolled.
func cellWidth(s string) int {
	return ansi.StringWidth(s)
}

func truncateCells(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func wrapLines(s string, width int) []string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\r", "")), " ")
	if width < 1 {
		width = 1
	}
	if s == "" {
		return nil
	}
	words := strings.Fields(s)
	var lines []string
	var cur string
	for _, word := range words {
		candidate := word
		if cur != "" {
			candidate = cur + " " + word
		}
		if cellWidth(candidate) <= width {
			cur = candidate
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		if cellWidth(word) <= width {
			cur = word
			continue
		}
		lines = append(lines, truncateCells(word, width))
		cur = ""
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
