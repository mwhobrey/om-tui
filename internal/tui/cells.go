package tui

import (
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
