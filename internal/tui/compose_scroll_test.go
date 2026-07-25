package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestComposerLongDraftKeepsTailVisible(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	long := strings.Repeat("aaaa ", 60) + "UNIQUE_TAIL_XYZ"
	for _, r := range long {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
		// Bubble Tea renders between keystrokes; textarea scroll depends on View()
		// refreshing the inner viewport line cache before the next reposition.
		_ = m.View()
	}
	if got := m.compose.Value(); got != long {
		t.Fatalf("draft len=%d want %d", len(got), len(long))
	}
	if m.composeHeight < composeMinHeight || m.composeHeight > composeMaxHeight {
		t.Fatalf("composeHeight=%d, want %d..%d", m.composeHeight, composeMinHeight, composeMaxHeight)
	}

	raw := m.compose.View()
	for i, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if w := lipgloss.Width(line); w > m.viewportWidth {
			t.Fatalf("compose line %d width %d > pane %d: %q", i, w, m.viewportWidth, ansi.Strip(line))
		}
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "UNIQUE_TAIL_XYZ") {
		t.Fatalf("tail missing from composed view (composeH=%d width=%d)\n%s",
			m.composeHeight, m.compose.Width(), view)
	}
}

func TestComposerSetValueKeepsTailVisible(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	long := strings.Repeat("bbbb ", 60) + "UNIQUE_SETVALUE_TAIL"
	m.compose.SetValue(long)
	m.syncComposeHeight()
	m.syncComposeViewport()
	view := ansi.Strip(m.compose.View())
	if !strings.Contains(view, "UNIQUE_SETVALUE_TAIL") {
		t.Fatalf("after SetValue, tail missing (viewport likely stuck at top)\n%s", view)
	}
	if m.composeHeight < 4 {
		t.Fatalf("expected composer to grow for wrapped draft, got height %d", m.composeHeight)
	}
}

func TestComposerOpenConversationRestoresDraftScrolled(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.drafts = map[string]string{
		"c1": strings.Repeat("cccc ", 60) + "UNIQUE_DRAFT_TAIL",
	}

	next, _ = m.openConversation("c1", "Chat", "")
	opened := next.(Model)
	view := ansi.Strip(opened.compose.View())
	if !strings.Contains(view, "UNIQUE_DRAFT_TAIL") {
		t.Fatalf("restored draft tail missing from composer\n%s", view)
	}
}

func TestComposerCollapsesAfterClear(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	m.compose.SetValue(strings.Repeat("dddd ", 60))
	m.syncComposeHeight()
	if m.composeHeight <= composeMinHeight {
		t.Fatalf("expected grow before clear, got %d", m.composeHeight)
	}

	next, _ = m.Update(sentMsg{conversationID: "c1"})
	cleared := next.(Model)
	if cleared.composeHeight != composeMinHeight {
		t.Fatalf("after send composeHeight=%d, want %d", cleared.composeHeight, composeMinHeight)
	}
	if cleared.compose.Height() != composeMinHeight {
		t.Fatalf("after send textarea height=%d, want %d", cleared.compose.Height(), composeMinHeight)
	}
}

func TestComposerBareLettersAlwaysType(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	// First letters of a fresh message must not trigger media actions.
	// (The textarea may return a cursor-blink command; that's fine.)
	for _, r := range "so" {
		next, _ := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if got := m.compose.Value(); got != "so" {
		t.Fatalf("draft = %q, want %q", got, "so")
	}
	if m.info != "" {
		t.Fatalf("info = %q, want empty (no media action)", m.info)
	}
}

func TestComposerCollapsesOnBackspace(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	long := strings.Repeat("eeee ", 60)
	for _, r := range long {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
		_ = m.View()
	}
	if m.composeHeight <= composeMinHeight {
		t.Fatalf("expected grow, got %d", m.composeHeight)
	}

	for m.compose.Value() != "" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = next.(Model)
		_ = m.View()
	}
	if m.composeHeight != composeMinHeight {
		t.Fatalf("after backspace-clear composeHeight=%d, want %d", m.composeHeight, composeMinHeight)
	}
}
