package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestOpenBlockPaletteSingleURLOpens(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusThread
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.messages = []localapi.Message{{
		MessageID: "m1",
		Body:      "[View]",
		BlockActions: []localapi.MessageAction{{
			Label: "View",
			Kind:  "url",
			URL:   "https://example.com/pr",
		}},
	}}
	m.selectedMsg = 0
	next, cmd := m.openBlockPalette()
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("type %T", next)
	}
	if got.blockPalette {
		t.Fatal("single URL action should not wait on a palette")
	}
	if cmd == nil {
		t.Fatal("single URL action should open immediately")
	}
	if !strings.Contains(got.info, "Opening View") {
		t.Fatalf("info = %q", got.info)
	}
}

func TestOpenBlockPaletteMultipleWaitsForDigit(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusThread
	m.messages = []localapi.Message{{
		MessageID: "m1",
		BlockActions: []localapi.MessageAction{
			{Label: "View", Kind: "url", URL: "https://example.com"},
			{Label: "Approve", Kind: "app", URL: "slack://channel?team=T1&id=C1&message=1"},
		},
	}}
	m.selectedMsg = 0
	next, cmd := m.openBlockPalette()
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("type %T", next)
	}
	if cmd != nil {
		t.Fatal("multi-action palette should wait")
	}
	if !got.blockPalette {
		t.Fatal("expected block palette")
	}
	if !strings.Contains(got.info, "1 View") || !strings.Contains(got.info, "2 Approve") {
		t.Fatalf("info = %q", got.info)
	}

	next, cmd = got.updateThreadKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got, ok = next.(Model)
	if !ok {
		t.Fatalf("type %T", next)
	}
	if got.blockPalette {
		t.Fatal("digit should close the palette")
	}
	if cmd == nil {
		t.Fatal("app action should open Slack")
	}
	if !strings.Contains(got.info, "Opening in Slack") {
		t.Fatalf("info = %q", got.info)
	}
}

func TestRenderContextHelpBlockPalette(t *testing.T) {
	m := NewModel(nil)
	m.blockPalette = true
	help := renderContextHelp(m)
	if !strings.Contains(help, "1-9 block") || !strings.Contains(help, "esc cancel") {
		t.Fatalf("help = %q", help)
	}
}

func TestRenderContextHelpShowsBlockKitChord(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusCompose
	m.activeID = "hashed-slack-conv"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.status.V2Primary = true
	m.messages = []localapi.Message{{
		MessageID: "hashed-slack-msg",
		Body:      "[View]",
		BlockActions: []localapi.MessageAction{{
			Label: "View",
			Kind:  "url",
			URL:   "https://example.com/pr",
		}},
	}}
	m.selectedMsg = 0
	help := renderContextHelp(m)
	if !strings.Contains(help, "ctrl+b blocks") {
		t.Fatalf("help missing block kit chord: %q", help)
	}
}

func TestComposeCtrlBOpensBlockPalette(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusCompose
	m.activeID = "slack:T1:C1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	m.messages = []localapi.Message{{
		MessageID: "m1",
		BlockActions: []localapi.MessageAction{
			{Label: "View", Kind: "url", URL: "https://example.com"},
			{Label: "Approve", Kind: "app", URL: "slack://channel?team=T1&id=C1&message=1"},
		},
	}}
	m.selectedMsg = 0
	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyCtrlB})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("type %T", next)
	}
	if cmd != nil || !got.blockPalette {
		t.Fatalf("ctrl+b palette=%v cmd=%v", got.blockPalette, cmd != nil)
	}
}
