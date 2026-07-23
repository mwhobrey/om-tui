package tui

import (
	"testing"

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
