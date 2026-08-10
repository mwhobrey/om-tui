package tui

import (
	"fmt"
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

func TestComposeCharLimitBadge(t *testing.T) {
	m := NewModel(nil)
	if got := m.composeCharLimitBadge(); got != "" {
		t.Fatalf("empty draft should show no badge, got %q", got)
	}

	m.compose.SetValue(strings.Repeat("a", m.compose.CharLimit-composeCharLimitThreshold-1))
	if got := m.composeCharLimitBadge(); got != "" {
		t.Fatalf("draft outside the threshold should show no badge, got %q", got)
	}

	m.compose.SetValue(strings.Repeat("a", m.compose.CharLimit-100))
	badge := stripForWidthTest(m.composeCharLimitBadge())
	want := fmt.Sprintf("%d/%d", m.compose.CharLimit-100, m.compose.CharLimit)
	if badge != want {
		t.Fatalf("badge = %q, want %q", badge, want)
	}

	m.compose.SetValue(strings.Repeat("a", m.compose.CharLimit))
	atLimit := stripForWidthTest(m.composeCharLimitBadge())
	wantAtLimit := fmt.Sprintf("%d/%d", m.compose.CharLimit, m.compose.CharLimit)
	if atLimit != wantAtLimit {
		t.Fatalf("at-limit badge = %q, want %q", atLimit, wantAtLimit)
	}
}

func TestRenderThreadTitleLineExactWidth(t *testing.T) {
	m := NewModel(nil)
	m.compose.SetValue(strings.Repeat("a", m.compose.CharLimit))
	for _, width := range []int{20, 40, 80} {
		line := m.renderThreadTitleLine("A Very Long Conversation Name That Might Overflow", width)
		if w := cellWidth(stripForWidthTest(line)); w != width {
			t.Fatalf("width=%d: title line width = %d (%q)", width, w, stripForWidthTest(line))
		}
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
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "└") {
		t.Fatal("frame missing bottom pane borders — MaxHeight likely clipping content-box Height")
	}
}
