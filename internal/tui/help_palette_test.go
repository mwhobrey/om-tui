package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestRenderContextHelpList(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusList
	help := renderContextHelp(m)
	for _, want := range []string{"q quit", "[ ] river", "/ jump", "ctrl+k commands"} {
		if !strings.Contains(help, want) {
			t.Fatalf("list help missing %q in %q", want, help)
		}
	}
}

func TestRenderContextHelpComposeSlack(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusCompose
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.messages = []localapi.Message{{MessageID: "slack:C1:1.0", Body: "hi"}}
	m.selectedMsg = 0
	help := renderContextHelp(m)
	for _, want := range []string{"esc list", "enter send", "ctrl+t thread", "pgup older", "ctrl+k commands"} {
		if !strings.Contains(help, want) {
			t.Fatalf("slack compose help missing %q in %q", want, help)
		}
	}
	if strings.Contains(help, "ctrl+e react") {
		t.Fatalf("slack compose help should hide react: %q", help)
	}
}

func TestRenderContextHelpReactPalette(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusThread
	m.reactPalette = true
	help := renderContextHelp(m)
	if help != "1-9 react  esc cancel" {
		t.Fatalf("react help = %q", help)
	}
}

func TestRenderContextHelpSlackReplyThread(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusCompose
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.threadRootID = "slack:C1:1.0"
	help := renderContextHelp(m)
	if help != "esc channel  enter send  ctrl+k commands" {
		t.Fatalf("thread help = %q", help)
	}
}

func TestPaletteOpenClosePreservesComposeDraft(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusCompose
	m.activeID = "c1"
	m.compose.SetValue("draft text")
	m.compose.Focus()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	opened := next.(Model)
	if !opened.palette.open {
		t.Fatal("palette should open")
	}
	if opened.compose.Value() != "draft text" {
		t.Fatalf("draft mutated on open: %q", opened.compose.Value())
	}

	next, _ = opened.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	typed := next.(Model)
	if typed.compose.Value() != "draft text" {
		t.Fatalf("draft mutated while palette open: %q", typed.compose.Value())
	}
	if !strings.Contains(typed.palette.filter.Value(), "x") {
		t.Fatalf("filter = %q, want to contain x", typed.palette.filter.Value())
	}

	next, _ = typed.Update(tea.KeyMsg{Type: tea.KeyEsc})
	closed := next.(Model)
	if closed.palette.open {
		t.Fatal("palette should close")
	}
	if closed.compose.Value() != "draft text" {
		t.Fatalf("draft mutated on close: %q", closed.compose.Value())
	}
	if closed.focus != focusCompose {
		t.Fatalf("focus = %v, want compose", closed.focus)
	}
}

func TestParsePaletteQueryModes(t *testing.T) {
	mode, q := parsePaletteQuery("alice")
	if mode != "jump" || q != "alice" {
		t.Fatalf("got mode=%q q=%q", mode, q)
	}
	mode, q = parsePaletteQuery(">react")
	if mode != "commands" || q != "react" {
		t.Fatalf("got mode=%q q=%q", mode, q)
	}
	mode, q = parsePaletteQuery("  > msg alice::hi ")
	if mode != "commands" || q != "msg alice::hi" {
		t.Fatalf("got mode=%q q=%q", mode, q)
	}
	mode, q = parsePaletteQuery("")
	if mode != "jump" || q != "" {
		t.Fatalf("empty got mode=%q q=%q", mode, q)
	}
}

func TestPaletteFuzzySlackThreadVisibility(t *testing.T) {
	slack := NewModel(nil)
	slack.focus = focusCompose
	slack.activeID = "slack:T1:C1"
	slack.activeRiverID = "slack-T1"
	slack.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	slack.messages = []localapi.Message{{MessageID: "slack:C1:1.0", Body: "root"}}
	slack.selectedMsg = 0

	next, _ := slack.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	opened := next.(Model)
	opened.palette.filter.SetValue(">thread")
	opened.refreshPaletteMatches()
	if !containsActionID(opened.palette.matches, "slack-thread") {
		t.Fatalf("slack palette matches = %v, want slack-thread", summarizeItems(opened.palette.matches))
	}

	msgs := NewModel(nil)
	msgs.focus = focusCompose
	msgs.activeID = "sms:1"
	msgs.activeRiverID = "messages-default"
	msgs.rivers = []localapi.River{{ID: "messages-default", Provider: "messages"}}
	msgs.messages = []localapi.Message{{MessageID: "m1", Body: "hi"}}
	msgs.selectedMsg = 0

	next, _ = msgs.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	opened = next.(Model)
	opened.palette.filter.SetValue(">thread")
	opened.refreshPaletteMatches()
	if containsActionID(opened.palette.matches, "slack-thread") {
		t.Fatalf("messages palette should hide slack-thread: %v", summarizeItems(opened.palette.matches))
	}
}

func TestPaletteEnterReactOpensPalette(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusCompose
	m.activeID = "sms:1"
	m.activeRiverID = "messages-default"
	m.rivers = []localapi.River{{ID: "messages-default", Provider: "messages"}}
	m.messages = []localapi.Message{{MessageID: "m1", Body: "hi"}}
	m.selectedMsg = 0
	m.compose.SetValue("keep me")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	opened := next.(Model)
	opened.palette.filter.SetValue(">react")
	opened.refreshPaletteMatches()
	idx := indexOfActionID(opened.palette.matches, "react")
	if idx < 0 {
		t.Fatalf("react not in matches: %v", summarizeItems(opened.palette.matches))
	}
	opened.palette.cursor = idx

	next, _ = opened.Update(tea.KeyMsg{Type: tea.KeyEnter})
	ran := next.(Model)
	if ran.palette.open {
		t.Fatal("command palette should close after run")
	}
	if !ran.reactPalette {
		t.Fatal("react palette should open")
	}
	if ran.compose.Value() != "keep me" {
		t.Fatalf("draft changed: %q", ran.compose.Value())
	}

	next, _ = ran.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cancelled := next.(Model)
	if cancelled.reactPalette {
		t.Fatal("esc should cancel react palette")
	}
	if cancelled.compose.Value() != "keep me" {
		t.Fatalf("draft changed after cancel: %q", cancelled.compose.Value())
	}
}

func TestPaletteJumpListsConversations(t *testing.T) {
	m := NewModel(nil)
	m.rivers = []localapi.River{
		{ID: "messages-default", DisplayName: "Messages"},
		{ID: "slack-T1", DisplayName: "Acme"},
	}
	m.paletteConvs = []localapi.Conversation{
		{ConversationID: "sms:1", Name: "Alice", RiverID: "messages-default", LastMessageTS: 2},
		{ConversationID: "slack:T1:C1", Name: "general", RiverID: "slack-T1", LastMessageTS: 1},
	}
	m.paletteConvsAt = time.Now()
	m.palette.open = true
	m.palette.filter.SetValue("alice")
	m.refreshPaletteMatches()
	if m.palette.mode != "jump" {
		t.Fatalf("mode = %q", m.palette.mode)
	}
	if len(m.palette.matches) == 0 || m.palette.matches[0].Kind != paletteKindConv {
		t.Fatalf("matches = %#v", m.palette.matches)
	}
	if m.palette.matches[0].Conv == nil || m.palette.matches[0].Conv.ConversationID != "sms:1" {
		t.Fatalf("want alice conversation, got %#v", m.palette.matches[0])
	}
}

func TestOpenConversationAcrossRivers(t *testing.T) {
	m := NewModel(nil)
	m.activeRiverID = "messages-default"
	m.rivers = []localapi.River{
		{ID: "messages-default", DisplayName: "Messages"},
		{ID: "slack-T1", DisplayName: "Acme"},
	}
	next, _ := m.openConversationAcrossRivers(localapi.Conversation{
		ConversationID: "slack:T1:C9",
		Name:           "#random",
		RiverID:        "slack-T1",
	})
	opened := next.(Model)
	if opened.activeRiverID != "slack-T1" {
		t.Fatalf("river = %q", opened.activeRiverID)
	}
	if opened.activeID != "slack:T1:C9" {
		t.Fatalf("activeID = %q", opened.activeID)
	}
}

func TestFrecencyBumpAndCorruptSafe(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, frecencyFileName)
	if err := os.WriteFile(bad, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadFrecency(dir)
	if s.score("conv:x") != 0 {
		t.Fatal("corrupt file should yield empty store")
	}
	s.bump("conv:x")
	s.bump("conv:x")
	if s.score("conv:x") <= 0 {
		t.Fatal("expected positive score after bump")
	}
	s.save()
	s2 := loadFrecency(dir)
	if s2.score("conv:x") <= 0 {
		t.Fatal("expected persisted score")
	}
}

func TestCustomCommandsLoadAndSkipBad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, customCommandsFileName)
	body := `{
  "commands": [
    {"id":"ok","label":"OK","action":"open","conversation_id":"c1","river_id":"messages-default"},
    {"id":"bad","label":"Bad","action":"nope","conversation_id":"c2"},
    {"label":"missing-id","action":"send","conversation_id":"c3"}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadCustomCommands(dir)
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseQuickMsgArgs(t *testing.T) {
	c, b, ok := parseQuickMsgArgs("alice::hello there")
	if !ok || c != "alice" || b != "hello there" {
		t.Fatalf("got %q %q %v", c, b, ok)
	}
	if _, _, ok := parseQuickMsgArgs("alice"); ok {
		t.Fatal("expected failure without ::")
	}
}

func TestResolvePaletteContact(t *testing.T) {
	m := NewModel(nil)
	m.paletteConvs = []localapi.Conversation{
		{ConversationID: "1", Name: "Alice Smith", RiverID: "messages-default", LastMessageTS: 1},
		{ConversationID: "2", Name: "Bob", RiverID: "messages-default", LastMessageTS: 2},
	}
	got, ok := m.resolvePaletteContact("alice")
	if !ok || got.ConversationID != "1" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func containsActionID(items []paletteItem, id string) bool {
	return indexOfActionID(items, id) >= 0
}

func indexOfActionID(items []paletteItem, id string) int {
	for i, it := range items {
		if it.Kind == paletteKindAction && it.ActionID == id {
			return i
		}
	}
	return -1
}

func summarizeItems(items []paletteItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Kind+":"+it.ActionID+it.CustomID+it.Label)
	}
	return out
}
