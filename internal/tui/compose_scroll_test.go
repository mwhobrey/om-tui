package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/maxghenis/openmessage/internal/localapi"
)

var errFakeSend = errors.New("send failed")

func TestComposerLongDraftKeepsTailVisible(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	long := strings.Repeat("aaaa ", 60) + "UNIQUE_TAIL_XYZ"
	for _, r := range long {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
		// Bubble Tea renders between keystrokes; textarea scroll depends on View()
		// refreshing the inner viewport line cache before the next reposition.
		_ = m.View()
	}
	if got := m.compose.Value(); got != long {
		t.Fatalf("draft len=%d want %d", len(got), len(long))
	}
	if m.composeHeight < composeMinHeight || m.composeHeight > composeMaxHeight {
		t.Fatalf("composeHeight=%d, want %d..%d", m.composeHeight, composeMinHeight, composeMaxHeight)
	}

	raw := m.compose.View()
	for i, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if w := lipgloss.Width(line); w > m.viewportWidth {
			t.Fatalf("compose line %d width %d > pane %d: %q", i, w, m.viewportWidth, ansi.Strip(line))
		}
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "UNIQUE_TAIL_XYZ") {
		t.Fatalf("tail missing from composed view (composeH=%d width=%d)\n%s",
			m.composeHeight, m.compose.Width(), view)
	}
}

func TestComposerSetValueKeepsTailVisible(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	long := strings.Repeat("bbbb ", 60) + "UNIQUE_SETVALUE_TAIL"
	m.compose.SetValue(long)
	m.syncComposeHeight()
	m.syncComposeViewport()
	view := ansi.Strip(m.compose.View())
	if !strings.Contains(view, "UNIQUE_SETVALUE_TAIL") {
		t.Fatalf("after SetValue, tail missing (viewport likely stuck at top)\n%s", view)
	}
	if m.composeHeight < 4 {
		t.Fatalf("expected composer to grow for wrapped draft, got height %d", m.composeHeight)
	}
}

func TestComposerOpenConversationRestoresDraftScrolled(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.drafts = map[string]string{
		"c1": strings.Repeat("cccc ", 60) + "UNIQUE_DRAFT_TAIL",
	}

	next, _ = m.openConversation("c1", "Chat", "")
	opened := next.(Model)
	view := ansi.Strip(opened.compose.View())
	if !strings.Contains(view, "UNIQUE_DRAFT_TAIL") {
		t.Fatalf("restored draft tail missing from composer\n%s", view)
	}
}

// slackSendableModel returns a Model whose active river is Slack, so
// canSend() passes unconditionally (default Google-river gating requires a
// paired status these unit tests don't set up) — same pattern as
// slack_test.go.
func slackSendableModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	m.activeRiverID = "slack-T1"
	m.rivers = []localapi.River{{ID: "slack-T1", Provider: "slack"}}
	_ = m.compose.Focus()
	return m
}

// TestComposerCollapsesAfterClear covers the real send path: pressing enter
// dispatches sendCmd and optimistically clears the composer immediately
// (not on the async response — see clearComposeOptimistically), so the
// returned tea.Cmd is deliberately never invoked here.
func TestComposerCollapsesAfterClear(t *testing.T) {
	m := slackSendableModel(t)

	m.compose.SetValue(strings.Repeat("dddd ", 60))
	m.syncComposeHeight()
	if m.composeHeight <= composeMinHeight {
		t.Fatalf("expected grow before clear, got %d", m.composeHeight)
	}

	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyEnter})
	cleared := next.(Model)
	if cmd == nil {
		t.Fatal("expected sendCmd to be dispatched")
	}
	if cleared.composeHeight != composeMinHeight {
		t.Fatalf("after send composeHeight=%d, want %d", cleared.composeHeight, composeMinHeight)
	}
	if cleared.compose.Height() != composeMinHeight {
		t.Fatalf("after send textarea height=%d, want %d", cleared.compose.Height(), composeMinHeight)
	}
}

// TestComposerViewClearsAfterSendShortDraft is a regression test for a
// short single-line draft (e.g. a URL) appearing to "duplicate" after
// send — the message shows correctly in history but the composer kept
// rendering the same text. Waiting for the send's own round-trip to clear
// the composer raced against the SSE-driven message refresh, which has no
// ordering guarantee against it; clearing must happen immediately on
// dispatch (enter), not on the async sentMsg response.
func TestComposerViewClearsAfterSendShortDraft(t *testing.T) {
	m := slackSendableModel(t)

	const draft = "https://gprivate.com/6ludx"
	m.compose.SetValue(draft)
	m.syncComposeHeight()
	if m.composeHeight != composeMinHeight {
		t.Fatalf("short draft should not grow composeHeight: got %d, want %d", m.composeHeight, composeMinHeight)
	}

	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyEnter})
	cleared := next.(Model)
	if cmd == nil {
		t.Fatal("expected sendCmd to be dispatched")
	}
	if got := cleared.compose.Value(); got != "" {
		t.Fatalf("compose value after pressing enter = %q, want empty (cleared immediately, not after round-trip)", got)
	}
	view := ansi.Strip(cleared.compose.View())
	if strings.Contains(view, draft) {
		t.Fatalf("composer still rendering sent draft right after enter:\n%s", view)
	}
}

// TestComposerRestoresDraftOnSendFailure ensures the optimistic clear from
// TestComposerViewClearsAfterSendShortDraft doesn't silently lose the
// user's message if the send actually fails.
func TestComposerRestoresDraftOnSendFailure(t *testing.T) {
	m := slackSendableModel(t)
	const draft = "hello there"
	m.compose.SetValue(draft)

	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyEnter})
	sent := next.(Model)
	if cmd == nil {
		t.Fatal("expected sendCmd to be dispatched")
	}
	if got := sent.compose.Value(); got != "" {
		t.Fatalf("compose should be optimistically cleared, got %q", got)
	}

	failed, _ := sent.Update(sendFailedMsg{conversationID: "c1", body: draft, err: errFakeSend})
	restored := failed.(Model)
	if got := restored.compose.Value(); got != draft {
		t.Fatalf("draft not restored after send failure: got %q, want %q", got, draft)
	}
	if restored.sending {
		t.Fatal("sending flag should clear on failure")
	}
}

// TestComposerRestoresDraftOnSendFailureDoesNotClobberNewDraft ensures that
// if the user typed something new during the failed send's round-trip,
// the restore doesn't overwrite it.
func TestComposerRestoresDraftOnSendFailureDoesNotClobberNewDraft(t *testing.T) {
	m := slackSendableModel(t)
	const original = "original message"
	m.compose.SetValue(original)

	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyEnter})
	sent := next.(Model)
	if cmd == nil {
		t.Fatal("expected sendCmd to be dispatched")
	}

	const newDraft = "something else entirely"
	sent.compose.SetValue(newDraft)

	failed, _ := sent.Update(sendFailedMsg{conversationID: "c1", body: original, err: errFakeSend})
	restored := failed.(Model)
	if got := restored.compose.Value(); got != newDraft {
		t.Fatalf("new draft was clobbered by failed-send restore: got %q, want %q", got, newDraft)
	}
}

func TestComposerBareLettersAlwaysType(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	// First letters of a fresh message must not trigger media actions.
	// (The textarea may return a cursor-blink command; that's fine.)
	for _, r := range "so" {
		next, _ := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if got := m.compose.Value(); got != "so" {
		t.Fatalf("draft = %q, want %q", got, "so")
	}
	if m.info != "" {
		t.Fatalf("info = %q, want empty (no media action)", m.info)
	}
}

func TestComposerCollapsesOnBackspace(t *testing.T) {
	m := NewModel(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focus = focusCompose
	m.activeID = "c1"
	_ = m.compose.Focus()

	long := strings.Repeat("eeee ", 60)
	for _, r := range long {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
		_ = m.View()
	}
	if m.composeHeight <= composeMinHeight {
		t.Fatalf("expected grow, got %d", m.composeHeight)
	}

	for m.compose.Value() != "" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = next.(Model)
		_ = m.View()
	}
	if m.composeHeight != composeMinHeight {
		t.Fatalf("after backspace-clear composeHeight=%d, want %d", m.composeHeight, composeMinHeight)
	}
}
