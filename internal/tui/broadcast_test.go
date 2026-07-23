package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestToggleBroadcast(t *testing.T) {
	m := Model{}
	m.toggleBroadcast("a", "Alice")
	m.toggleBroadcast("b", "Bob")
	if m.broadcastCount() != 2 {
		t.Fatalf("count = %d", m.broadcastCount())
	}
	m.toggleBroadcast("a", "Alice")
	if m.broadcastCount() != 1 {
		t.Fatalf("count after untoggle = %d", m.broadcastCount())
	}
	if _, ok := m.broadcastIDs["b"]; !ok {
		t.Fatal("expected b to remain")
	}
	m.clearBroadcast()
	if m.broadcastCount() != 0 {
		t.Fatal("expected clear")
	}
}

func TestSyncComposePlaceholder(t *testing.T) {
	m := Model{compose: textarea.New()}
	m.syncComposePlaceholder()
	if m.compose.Placeholder != "Write a message…" {
		t.Fatalf("placeholder = %q", m.compose.Placeholder)
	}
	m.toggleBroadcast("a", "Alice")
	m.syncComposePlaceholder()
	if m.compose.Placeholder != "Message to 1 chats…" {
		t.Fatalf("placeholder = %q", m.compose.Placeholder)
	}
}
