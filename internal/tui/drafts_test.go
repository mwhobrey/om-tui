package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/maxghenis/openmessage/internal/localapi"
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

func TestPersistentDraftsFile(t *testing.T) {
	tmpDir := t.TempDir()

	initial := map[string]string{
		"conv-1": "draft one",
		"conv-2": "draft two",
		"conv-3": "   ", // should be pruned
	}
	if err := saveDrafts(tmpDir, initial); err != nil {
		t.Fatalf("saveDrafts: %v", err)
	}

	loaded := loadDrafts(tmpDir)
	if len(loaded) != 2 {
		t.Fatalf("expected 2 drafts, got %d: %+v", len(loaded), loaded)
	}
	if loaded["conv-1"] != "draft one" || loaded["conv-2"] != "draft two" {
		t.Fatalf("unexpected loaded drafts: %+v", loaded)
	}

	// Model with session saves to disk on saveComposeDraft
	compose := textarea.New()
	compose.SetValue("persisted text")
	m := Model{
		session:  &Session{DataDir: tmpDir},
		compose:  compose,
		drafts:   loaded,
		activeID: "conv-1",
	}
	m.saveComposeDraft()

	reloaded := loadDrafts(tmpDir)
	if reloaded["conv-1"] != "persisted text" {
		t.Fatalf("reloaded draft = %q, want 'persisted text'", reloaded["conv-1"])
	}

	// Clearing draft clears from disk
	m.clearComposeOptimistically()
	reloadedAfterClear := loadDrafts(tmpDir)
	if _, ok := reloadedAfterClear["conv-1"]; ok {
		t.Fatal("expected conv-1 draft to be deleted from disk after send/clear")
	}
}

func TestCycleRiverSavesDraft(t *testing.T) {
	compose := textarea.New()
	compose.SetValue("switching river draft")
	m := Model{
		compose:       compose,
		drafts:        make(map[string]string),
		activeID:      "conv-switch",
		activeRiverID: "r1",
		rivers: []localapi.River{
			{ID: "r1", DisplayName: "River 1"},
			{ID: "r2", DisplayName: "River 2"},
		},
	}

	cycled, _ := m.cycleRiver(1)
	cycledModel := cycled.(Model)

	if cycledModel.drafts["conv-switch"] != "switching river draft" {
		t.Fatalf("draft not saved on river switch: got %q", cycledModel.drafts["conv-switch"])
	}
}

