package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestSlackThreadNavigationPreservesChannelMessages(t *testing.T) {
	m := NewModel(nil)
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.focus = focusCompose
	m.messages = []localapi.Message{
		{MessageID: "slack:C1:1.0", Body: "root", ReplyCount: 2},
	}
	m.selectedMsg = 0

	// Opening a chat focuses the composer; bare letters type into the draft, so
	// Slack thread open is Ctrl+T (same pattern as Ctrl+O / Ctrl+A).
	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyCtrlT})
	opened := next.(Model)
	if cmd == nil || opened.threadRootID != "slack:C1:1.0" || len(opened.channelMessages) != 1 {
		t.Fatalf("opened thread state = root %q channel=%d cmd=%v", opened.threadRootID, len(opened.channelMessages), cmd != nil)
	}

	opened.focus = focusThread
	next, cmd = opened.updateThreadKeys(tea.KeyMsg{Type: tea.KeyEsc})
	closed := next.(Model)
	if closed.threadRootID != "" || len(closed.messages) != 1 || cmd == nil {
		t.Fatalf("closed thread state = root %q messages=%d cmd=%v", closed.threadRootID, len(closed.messages), cmd != nil)
	}
}

func TestBareTDoesNotOpenSlackThread(t *testing.T) {
	m := NewModel(nil)
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.focus = focusThread
	m.messages = []localapi.Message{
		{MessageID: "slack:C1:1.0", Body: "root", ReplyCount: 2},
	}
	m.selectedMsg = 0

	next, cmd := m.updateThreadKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	got := next.(Model)
	if got.threadRootID != "" || cmd != nil {
		t.Fatalf("bare t opened thread root=%q cmd=%v", got.threadRootID, cmd != nil)
	}
}

func TestComposeCtrlChordsReachMessageActions(t *testing.T) {
	m := NewModel(nil)
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.focus = focusCompose
	m.messages = []localapi.Message{
		{MessageID: "slack:C1:1.0", Body: "root", ReplyCount: 2},
	}
	m.selectedMsg = 0
	m.compose.SetValue("draft stays")

	next, _ := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyCtrlE})
	reacting := next.(Model)
	if !reacting.reactPalette || reacting.focus != focusThread {
		t.Fatalf("ctrl+e react = palette=%v focus=%v", reacting.reactPalette, reacting.focus)
	}

	m.focus = focusCompose
	m.reactPalette = false
	next, _ = m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyCtrlF})
	searching := next.(Model)
	if searching.focus != focusSearch {
		t.Fatalf("ctrl+f focus = %v, want search", searching.focus)
	}

	m.focus = focusCompose
	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyPgUp})
	older := next.(Model)
	if cmd == nil || older.info != "Loading older Slack history…" {
		t.Fatalf("pgup older history info=%q cmd=%v", older.info, cmd != nil)
	}
}
