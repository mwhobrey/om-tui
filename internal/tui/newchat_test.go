package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
	"github.com/maxghenis/openmessage/internal/river"
)

func TestLooksLikePhoneNumber(t *testing.T) {
	for _, s := range []string{"+15551234567", "(555) 123-4567", "5551234567"} {
		if !looksLikePhoneNumber(s) {
			t.Fatalf("%q should look like a phone number", s)
		}
	}
	for _, s := range []string{"Alice", "Mom", "555", ""} {
		if looksLikePhoneNumber(s) {
			t.Fatalf("%q should not look like a phone number", s)
		}
	}
}

func TestNewChatHiddenOnSlack(t *testing.T) {
	m := Model{activeRiverID: "slack-T1", rivers: []localapi.River{{ID: "slack-T1", Provider: river.ProviderSlack}}}
	if m.canStartNewChat() {
		t.Fatal("slack should not offer new chat")
	}
	if _, ok := m.newChatPlatform(); ok {
		t.Fatal("slack platform should be rejected")
	}
}

func TestNewChatPlatformFollowsRiver(t *testing.T) {
	m := Model{}
	if p, ok := m.newChatPlatform(); !ok || p != "sms" {
		t.Fatalf("default platform = %q ok=%v, want sms", p, ok)
	}
	m.activeRiverID = river.DefaultWhatsAppRiverID
	if p, ok := m.newChatPlatform(); !ok || p != "whatsapp" {
		t.Fatalf("whatsapp platform = %q ok=%v", p, ok)
	}
	m.activeRiverID = river.DefaultSignalRiverID
	if p, ok := m.newChatPlatform(); !ok || p != "signal" {
		t.Fatalf("signal platform = %q ok=%v", p, ok)
	}
}

func TestOpenNewChatOverlayFromListN(t *testing.T) {
	m := Model{width: 80, height: 24, focus: focusList, compose: textarea.New()}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("type %T", next)
	}
	if !got.newChat.open {
		t.Fatal("n on the list should open new chat")
	}
	view := got.renderNewChatOverlay()
	if !strings.Contains(view, "New chat") {
		t.Fatalf("overlay missing title:\n%s", view)
	}
}

func TestNewChatPhoneMatchIsSelectable(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.openNewChatOverlay()
	got := next.(Model)
	got.newChat.input.SetValue("+15551234567")
	got.newChat.syncMatches()
	phone, err := got.selectedNewChatNumber()
	if err != nil {
		t.Fatal(err)
	}
	if phone != "+15551234567" {
		t.Fatalf("phone = %q", phone)
	}
}

func TestApplyNewChatContactsIgnoresStaleQuery(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.openNewChatOverlay()
	got := next.(Model)
	got.newChat.input.SetValue("bob")
	got, _ = got.applyNewChatContacts(newChatContactsMsg{
		query: "alice",
		contacts: []localapi.Contact{{
			Name:   "Alice",
			Number: "+15550001111",
		}},
	})
	if len(got.newChat.contacts) != 0 {
		t.Fatalf("stale contacts applied: %+v", got.newChat.contacts)
	}
	got, _ = got.applyNewChatContacts(newChatContactsMsg{
		query: "bob",
		contacts: []localapi.Contact{{
			Name:   "Bob",
			Number: "+15550002222",
		}},
	})
	if len(got.newChat.contacts) != 1 || got.newChat.contacts[0].Name != "Bob" {
		t.Fatalf("contacts = %+v", got.newChat.contacts)
	}
}

func TestNewChatPicksContactNumber(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.openNewChatOverlay()
	got := next.(Model)
	got.newChat.contacts = []localapi.Contact{{Name: "Alice", Number: "+15550001111"}}
	got.newChat.input.SetValue("ali")
	got.newChat.syncMatches()
	if len(got.newChat.matches) != 1 || got.newChat.matches[0].Number != "+15550001111" {
		t.Fatalf("matches = %+v", got.newChat.matches)
	}
	phone, err := got.selectedNewChatNumber()
	if err != nil {
		t.Fatal(err)
	}
	if phone != "+15550001111" {
		t.Fatalf("phone = %q", phone)
	}
}

func TestNewChatEscCloses(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.openNewChatOverlay()
	opened := next.(Model)
	closed, _ := opened.updateNewChatKeys(tea.KeyMsg{Type: tea.KeyEsc})
	got := closed.(Model)
	if got.newChat.open {
		t.Fatal("esc should close new chat")
	}
}

func TestNewChatNameWithoutNumberErrors(t *testing.T) {
	m := Model{width: 80, height: 24}
	next, _ := m.openNewChatOverlay()
	got := next.(Model)
	got.newChat.input.SetValue("nobody")
	got.newChat.syncMatches()
	if _, err := got.selectedNewChatNumber(); err == nil {
		t.Fatal("expected error for a name with no contact match")
	}
}

func TestRenderContextHelpListIncludesNewChat(t *testing.T) {
	m := NewModel(nil)
	m.focus = focusList
	help := renderContextHelp(m)
	if !strings.Contains(help, "n new") {
		t.Fatalf("list help missing new chat: %q", help)
	}
}
