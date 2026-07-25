package tui

import (
	"strings"

	"github.com/rivo/uniseg"
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
		for cellWidth(rest) > width {
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
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		cluster := gr.Str()
		w := cellWidth(cluster)
		if accum+w > width {
			break
		}
		accum += w
		_, to := gr.Positions()
		cut = to
		if cluster == " " {
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
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		cluster := gr.Str()
		w := cellWidth(cluster)
		if cut > 0 && accum+w > width {
			break
		}
		accum += w
		_, to := gr.Positions()
		cut = to
	}
	if cut == 0 {
		return len(s)
	}
	return cut
}
