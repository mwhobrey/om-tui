package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestActiveSlackBadge(t *testing.T) {
	m := NewModel(nil)
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}

	if got := m.activeSlackBadge(); got != "" {
		t.Fatalf("no status entries yet: got %q, want empty", got)
	}

	m.status.Slack = []localapi.SlackRiverStatus{
		{RiverID: "slack-other", SocketConnected: true},
	}
	if got := m.activeSlackBadge(); got != "" {
		t.Fatalf("status for a different river should not badge: got %q", got)
	}

	m.status.Slack = []localapi.SlackRiverStatus{
		{RiverID: "slack-T1", SocketConfigured: true, SocketConnected: true},
	}
	if got := m.activeSlackBadge(); !strings.Contains(got, "live") {
		t.Fatalf("connected socket should show live: got %q", got)
	}

	m.status.Slack = []localapi.SlackRiverStatus{
		{RiverID: "slack-T1", SocketConfigured: true, SocketConnected: false},
	}
	if got := m.activeSlackBadge(); !strings.Contains(got, "reconnecting") {
		t.Fatalf("configured but disconnected should show reconnecting: got %q", got)
	}

	m.status.Slack = []localapi.SlackRiverStatus{
		{RiverID: "slack-T1", SocketConfigured: false, SocketConnected: false},
	}
	if got := m.activeSlackBadge(); !strings.Contains(got, "poll") {
		t.Fatalf("no app token should show poll: got %q", got)
	}

	m.activeRiverID = "messages-default"
	m.rivers = nil
	if got := m.activeSlackBadge(); got != "" {
		t.Fatalf("non-Slack active river should never badge: got %q", got)
	}
}

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
