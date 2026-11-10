package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestFormatReactions(t *testing.T) {
	raw := `[{"emoji":"👍","count":2},{"emoji":"❤️","count":1}]`
	got := formatReactions(raw, nil, "")
	if got != "👍2 ❤️" {
		t.Fatalf("format = %q", got)
	}
	if formatReactions("", nil, "") != "" || formatReactions("not-json", nil, "") != "" {
		t.Fatal("empty/invalid should format empty")
	}
}

func TestFormatReactionsWithActors(t *testing.T) {
	participants := `[{"name":"Alice Smith","number":"+15551234567","id":"p1"},{"name":"Me","id":"p-me","is_me":true}]`
	resolve := reactionResolver(participants, "Alice Smith", nil)
	raw := `[{"emoji":"👍","count":1,"actors":["p1"]},{"emoji":"❤️","count":1,"actors":["me"]}]`
	got := formatReactions(raw, resolve, "Alice Smith")
	if got != "👍 Alice ❤️ you" {
		t.Fatalf("format = %q", got)
	}
}

func TestFormatReactionsPeerFallback(t *testing.T) {
	raw := `[{"emoji":"😂","count":1,"actors":["unknown-id"]}]`
	got := formatReactions(raw, nil, "Bob Jones")
	if got != "😂 Bob" {
		t.Fatalf("format = %q", got)
	}
}

func TestReactionResolverByNumber(t *testing.T) {
	participants := `[{"name":"Carol","number":"+1 (555) 999-0000"}]`
	resolve := reactionResolver(participants, "Carol", nil)
	if got := resolve("+15559990000"); got != "Carol" {
		t.Fatalf("resolve = %q", got)
	}
	if got := resolve("15559990000@s.whatsapp.net"); got != "Carol" {
		t.Fatalf("jid resolve = %q", got)
	}
}

func TestReactionActionToggle(t *testing.T) {
	raw := `[{"emoji":"👍","count":1}]`
	if got := reactionAction(raw, "👍"); got != "remove" {
		t.Fatalf("action = %q, want remove", got)
	}
	if got := reactionAction(raw, "❤️"); got != "add" {
		t.Fatalf("action = %q, want add", got)
	}
	if got := reactionAction("", "😂"); got != "add" {
		t.Fatalf("action = %q, want add", got)
	}
}

func TestClampMessageIndex(t *testing.T) {
	if got := clampMessageIndex(0, 5); got != -1 {
		t.Fatalf("empty = %d", got)
	}
	if got := clampMessageIndex(3, -1); got != 2 {
		t.Fatalf("default latest = %d", got)
	}
	if got := clampMessageIndex(3, 1); got != 1 {
		t.Fatalf("in range = %d", got)
	}
	if got := clampMessageIndex(3, 99); got != 2 {
		t.Fatalf("overflow = %d", got)
	}
}

func TestSelectedMessage(t *testing.T) {
	msgs := []localapi.Message{
		{MessageID: "a"},
		{MessageID: "b"},
	}
	got, ok := selectedMessage(msgs, -1)
	if !ok || got.MessageID != "b" {
		t.Fatalf("latest = %+v ok=%v", got, ok)
	}
	got, ok = selectedMessage(msgs, 0)
	if !ok || got.MessageID != "a" {
		t.Fatalf("first = %+v ok=%v", got, ok)
	}
}

func TestReactPaletteSize(t *testing.T) {
	if len(reactPaletteEmojis) != 9 {
		t.Fatalf("palette len = %d, want 9", len(reactPaletteEmojis))
	}
}

func TestReactionSettledMsgUpdatesStatus(t *testing.T) {
	m := Model{
		session: &Session{},
	}

	// Settled: confirmed
	updated, _ := m.Update(reactionSettledMsg{
		conversationID: "conv-1",
		emoji:          "👍",
		delivery: localapi.Delivery{
			State: "confirmed",
		},
	})
	m2 := updated.(Model)
	if m2.err != "" {
		t.Fatalf("unexpected error: %s", m2.err)
	}

	// Settled: failed
	updated, _ = m.Update(reactionSettledMsg{
		conversationID: "conv-1",
		emoji:          "👍",
		delivery: localapi.Delivery{
			State: "failed",
		},
	})
	m2 = updated.(Model)
	if m2.err != "Reaction 👍 delivery failed (failed)" {
		t.Fatalf("unexpected error message: %q", m2.err)
	}

	// Settled: canceled
	updated, _ = m.Update(reactionSettledMsg{
		conversationID: "conv-1",
		emoji:          "❤️",
		delivery: localapi.Delivery{
			State: "canceled",
		},
	})
	m2 = updated.(Model)
	if m2.info != "Reaction ❤️ canceled" {
		t.Fatalf("unexpected info message: %q", m2.info)
	}
}

func TestResolveEmoji(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{":fire:", "🔥"},
		{"fire", "🔥"},
		{"rocket", "🚀"},
		{":tada:", "🎉"},
		{":100:", "💯"},
		{"eyes", "👀"},
		{"+1", "👍"},
		{"👍", "👍"},
		{"🔥", "🔥"},
		{"invalidxyz123", ""},
		{"", ""},
	}

	for _, tc := range cases {
		got := resolveEmoji(tc.input)
		if got != tc.want {
			t.Errorf("resolveEmoji(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCustomReactionPicker(t *testing.T) {
	m := NewModel(&Session{Client: localapi.NewClient("http://127.0.0.1:1", "")})
	m.ready = true
	m.width = 80
	m.height = 24
	m.activeID = "conv-1"
	m.status.Google.Paired = true
	m.status.Google.Connected = true
	m.messages = []localapi.Message{
		{MessageID: "msg-1", Body: "Awesome news!"},
	}
	m.selectedMsg = 0
	m.focus = focusThread

	// 1. Open react palette
	m1, _ := m.openReactPalette()
	model1 := m1.(Model)
	if !model1.reactPalette || model1.reactCustom {
		t.Fatalf("expected reactPalette=true reactCustom=false, got palette=%v custom=%v", model1.reactPalette, model1.reactCustom)
	}

	// 2. Press ':' to enter custom emoji input
	m2, _ := model1.updateThreadKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model2 := m2.(Model)
	if !model2.reactCustom {
		t.Fatal("expected reactCustom=true after typing ':'")
	}

	// 3. Type 'fire' and enter
	model2.reactInput.SetValue("fire")
	m3, cmd := model2.updateThreadKeys(tea.KeyMsg{Type: tea.KeyEnter})
	model3 := m3.(Model)
	if model3.reactPalette || model3.reactCustom {
		t.Fatal("expected react palette and custom mode to close after sending")
	}
	if cmd == nil {
		t.Fatal("expected reaction command to be returned")
	}

	// 4. Test cancel with esc
	m4, _ := m.openReactPalette()
	model4 := m4.(Model)
	m5, _ := model4.updateThreadKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model5 := m5.(Model)
	m6, _ := model5.updateThreadKeys(tea.KeyMsg{Type: tea.KeyEsc})
	model6 := m6.(Model)
	if model6.reactPalette || model6.reactCustom {
		t.Fatal("expected esc to cancel custom reaction picker")
	}
}

