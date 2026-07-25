package tui

import (
	"strings"
	"testing"
)

func TestPaneDimsFitTerminal(t *testing.T) {
	m := NewModel(nil)
	m.width = 120
	m.height = 40
	m.ready = true
	m.layout()

	d := m.paneDims()
	borderX := borderStyle.GetHorizontalBorderSize()
	borderY := borderStyle.GetVerticalBorderSize()

	totalW := d.leftW + d.rightW + 2*borderX
	if totalW != m.width {
		t.Fatalf("pane widths+borders = %d, want terminal width %d (left=%d right=%d borderX=%d)",
			totalW, m.width, d.leftW, d.rightW, borderX)
	}
	totalH := 1 + d.mainH + borderY + 1 // status + pane content+borders + help
	if totalH != m.height {
		t.Fatalf("frame height = %d, want terminal height %d (mainH=%d borderY=%d)",
			totalH, m.height, d.mainH, borderY)
	}
	if m.viewport.Width != d.threadInnerW {
		t.Fatalf("viewport width = %d, want threadInnerW %d", m.viewport.Width, d.threadInnerW)
	}
	if m.viewportWidth != d.threadInnerW {
		t.Fatalf("viewportWidth field = %d, want %d", m.viewportWidth, d.threadInnerW)
	}
}

func TestViewLinesExactWidth(t *testing.T) {
	m := NewModel(nil)
	m.width = 100
	m.height = 30
	m.ready = true
	m.layout()
	m.setThreadContent(mutedStyle.Render("hello"))

	view := m.View()
	lines := strings.Split(view, "\n")
	// Place may not emit a trailing newline for the last row; accept height or height-1.
	if len(lines) < m.height-1 || len(lines) > m.height {
		t.Fatalf("view lines = %d, want ~%d", len(lines), m.height)
	}
	for i, line := range lines {
		if i >= m.height {
			break
		}
		w := cellWidth(stripForWidthTest(line))
		if w != m.width {
			t.Fatalf("line %d width = %d, want %d (%q)", i, w, m.width, stripForWidthTest(line))
		}
	}
}
