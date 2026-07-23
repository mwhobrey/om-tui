package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
)

func TestWindowsNotifierDedupesByMessageID(t *testing.T) {
	notifier := NewWindowsNotifier(zerolog.Nop(), true, nil, "")

	calls := make(chan string, 2)
	notifier.run = func(appID, title, body, script string) error {
		calls <- title
		return nil
	}

	message := &db.Message{
		MessageID:      "m1",
		SenderName:     "Alice",
		Body:           "Hello",
		TimestampMS:    100,
		IsFromMe:       false,
		ConversationID: "c1",
	}

	notifier.NotifyIncomingMessage(message)
	notifier.NotifyIncomingMessage(message)

	select {
	case got := <-calls:
		if got != "Alice" {
			t.Fatalf("title = %q", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected notification command to run once")
	}

	select {
	case extra := <-calls:
		t.Fatalf("unexpected duplicate notification: %q", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWindowsNotifierSkipsOutgoingMessages(t *testing.T) {
	notifier := NewWindowsNotifier(zerolog.Nop(), true, nil, "")

	called := false
	notifier.run = func(appID, title, body, script string) error {
		called = true
		return nil
	}

	notifier.NotifyIncomingMessage(&db.Message{
		MessageID: "m1",
		Body:      "sent",
		IsFromMe:  true,
	})

	if called {
		t.Fatal("notifier should not run for outgoing messages")
	}
}

func TestWindowsNotifierSkipsMutedConversation(t *testing.T) {
	store, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertConversation(&db.Conversation{
		ConversationID:   "c-muted",
		Name:             "Muted",
		NotificationMode: db.NotificationModeMuted,
	}); err != nil {
		t.Fatal(err)
	}

	notifier := NewWindowsNotifier(zerolog.Nop(), true, store, "Max Ghenis")
	called := false
	notifier.run = func(appID, title, body, script string) error {
		called = true
		return nil
	}

	notifier.NotifyIncomingMessage(&db.Message{
		MessageID:      "m-muted",
		ConversationID: "c-muted",
		SenderName:     "Alice",
		Body:           "hello there",
	})

	time.Sleep(50 * time.Millisecond)
	if called {
		t.Fatal("notifier should not run for muted conversation")
	}
}

func TestWindowsToastScriptUsesFileParam(t *testing.T) {
	script := windowsToastPowerShellScript()
	if !strings.Contains(script, "PayloadPath") {
		t.Fatal("script should take -PayloadPath")
	}
	if !strings.Contains(script, "CreateToastNotifier") {
		t.Fatal("script missing toast API")
	}
	if strings.Contains(script, "@\"") {
		t.Fatal("script must not use nested here-strings")
	}
}

func TestDefaultWindowsAppIDIsRegisteredPowerShell(t *testing.T) {
	if !strings.Contains(defaultWindowsAppID, "WindowsPowerShell") {
		t.Fatalf("default app id = %q, want PowerShell AUMID", defaultWindowsAppID)
	}
}
