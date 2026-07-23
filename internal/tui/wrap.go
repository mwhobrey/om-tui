package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// wrapText word-wraps text to width terminal cells. Empty input yields {""}.
func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\t", " ")
	if text == "" {
		return []string{""}
	}

	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		rest := para
		for runewidth.StringWidth(rest) > width {
			cut := breakOffset(rest, width)
			if cut <= 0 {
				cut = 1
			}
			piece := strings.TrimRight(rest[:cut], " ")
			if piece == "" {
				// Hard-break a single overlong token.
				cut = hardCut(rest, width)
				piece = rest[:cut]
			}
			lines = append(lines, piece)
			rest = strings.TrimLeft(rest[cut:], " ")
		}
		lines = append(lines, rest)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func breakOffset(s string, width int) int {
	accum := 0
	lastSpace := -1
	cut := 0
	for i, r := range s {
		w := runewidth.RuneWidth(r)
		if accum+w > width {
			break
		}
		accum += w
		cut = i + len(string(r))
		if r == ' ' {
			lastSpace = cut
		}
	}
	if lastSpace > 0 {
		return lastSpace
	}
	return cut
}

func hardCut(s string, width int) int {
	accum := 0
	cut := 0
	for i, r := range s {
		w := runewidth.RuneWidth(r)
		if cut > 0 && accum+w > width {
			break
		}
		accum += w
		cut = i + len(string(r))
	}
	if cut == 0 {
		return len(s)
	}
	return cut
}
