package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestPerConversationDrafts(t *testing.T) {
	compose := textarea.New()
	m := Model{
		compose:  compose,
		drafts:   make(map[string]string),
		activeID: "conv-a",
	}
	m.compose.SetValue("hello alice")
	m.saveComposeDraft()

	m.activeID = "conv-b"
	m.compose.SetValue("hello bob")
	m.saveComposeDraft()

	if m.drafts["conv-a"] != "hello alice" {
		t.Fatalf("draft a = %q", m.drafts["conv-a"])
	}
	if m.drafts["conv-b"] != "hello bob" {
		t.Fatalf("draft b = %q", m.drafts["conv-b"])
	}

	m.compose.SetValue("")
	m.saveComposeDraft()
	if _, ok := m.drafts["conv-b"]; ok {
		t.Fatal("empty compose should clear draft")
	}
}
