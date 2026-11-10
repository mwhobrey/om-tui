package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestQuoteReplyLifecycle(t *testing.T) {
	msgs := []localapi.Message{
		{MessageID: "msg-1", Body: "Hello world", SenderName: "Alice"},
		{MessageID: "msg-2", Body: "How are you?", SenderName: "Bob"},
	}

	m := NewModel(nil)
	m.width = 80
	m.height = 24
	m.ready = true
	m.status.Google.Paired = true
	m.status.Google.Connected = true
	m.messages = msgs
	m.selectedMsg = 0
	m.activeID = "conv-1"
	m.activeName = "Alice"
	m.focus = focusThread
	m.layout()

	// 1. Press 'r' in thread focus to start quote-reply
	m2, _ := m.updateThreadKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model2 := m2.(Model)

	if model2.replyTarget == nil {
		t.Fatal("expected replyTarget to be set")
	}
	if model2.replyTarget.MessageID != "msg-1" {
		t.Fatalf("expected replyTarget msg-1, got %q", model2.replyTarget.MessageID)
	}
	if model2.focus != focusCompose {
		t.Fatalf("expected focusCompose, got %v", model2.focus)
	}

	// 2. Banner renders in View
	view := model2.View()
	if !strings.Contains(view, "Replying to Alice") {
		t.Fatalf("expected 'Replying to Alice' in view, got:\n%s", view)
	}

	// 3. Pressing 'esc' in compose cancels replyTarget without leaving compose
	m3, _ := model2.updateComposeKeys(tea.KeyMsg{Type: tea.KeyEsc})
	model3 := m3.(Model)
	if model3.replyTarget != nil {
		t.Fatal("expected replyTarget to be cleared after esc")
	}
	if model3.focus != focusCompose {
		t.Fatalf("expected focus to stay in compose after cancelling reply, got %v", model3.focus)
	}

	// 4. Starting quote-reply with ctrl+q on msg-2
	model3.focus = focusThread
	model3.selectedMsg = 1
	m4, _ := model3.updateThreadKeys(tea.KeyMsg{Type: tea.KeyCtrlQ})
	model4 := m4.(Model)
	if model4.replyTarget == nil || model4.replyTarget.MessageID != "msg-2" {
		t.Fatalf("expected replyTarget msg-2 from ctrl+q, got %+v", model4.replyTarget)
	}

	// 5. Typing in compose and pressing Enter sends with replyTarget.MessageID
	model4.compose.SetValue("I'm doing well!")
	m5, cmd := model4.updateComposeKeys(tea.KeyMsg{Type: tea.KeyEnter})
	model5 := m5.(Model)
	if cmd == nil {
		t.Fatal("expected send command from enter")
	}
	if model5.replyTarget != nil {
		t.Fatal("expected replyTarget to be cleared upon sending")
	}
}
