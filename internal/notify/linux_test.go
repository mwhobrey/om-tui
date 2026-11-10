package notify

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
)

func TestLinuxNotifierDedupesByMessageID(t *testing.T) {
	notifier := NewLinuxNotifier(zerolog.Nop(), true, nil, "")

	calls := make(chan string, 2)
	notifier.run = func(name string, args ...string) error {
		calls <- name
		return nil
	}

	message := &db.Message{
		MessageID:      "m1",
		SenderName:     "Alice",
		Body:           "Hello",
		TimestampMS:    time.Now().UnixMilli(),
		IsFromMe:       false,
		ConversationID: "c1",
	}

	notifier.NotifyIncomingMessage(message)
	notifier.NotifyIncomingMessage(message)

	select {
	case <-calls:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected notification command to run once")
	}

	select {
	case extra := <-calls:
		t.Fatalf("unexpected duplicate notification command: %q", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLinuxNotifierSkipsOutgoingMessages(t *testing.T) {
	notifier := NewLinuxNotifier(zerolog.Nop(), true, nil, "")

	called := false
	notifier.run = func(name string, args ...string) error {
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

func TestLinuxNotifierArgsAndExecution(t *testing.T) {
	notifier := NewLinuxNotifier(zerolog.Nop(), true, nil, "")

	type call struct {
		name string
		args []string
	}
	calls := make(chan call, 1)
	notifier.run = func(name string, args ...string) error {
		calls <- call{name: name, args: args}
		return nil
	}

	notifier.NotifyIncomingMessage(&db.Message{
		MessageID:      "m1",
		ConversationID: "conv-1",
		SenderName:     "Alice",
		Body:           "Hello from Linux",
		TimestampMS:    time.Now().UnixMilli(),
	})

	var got call
	select {
	case got = <-calls:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected notification command to run")
	}

	if got.name != "notify-send" {
		t.Fatalf("command name = %q, want notify-send", got.name)
	}
	wantArgs := []string{
		"-a", "OM-TUI",
		"-c", "im.received",
		"-i", "mail-unread",
		"Alice",
		"Hello from Linux",
	}
	if !reflect.DeepEqual(got.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", got.args, wantArgs)
	}
}

func TestLinuxNotifierSkipsStaleMessages(t *testing.T) {
	notifier := NewLinuxNotifier(zerolog.Nop(), true, nil, "")

	called := false
	notifier.run = func(name string, args ...string) error {
		called = true
		return nil
	}

	notifier.NotifyIncomingMessage(&db.Message{
		MessageID:   "m1",
		Body:        "old message",
		TimestampMS: time.Now().Add(-10 * time.Minute).UnixMilli(),
	})

	if called {
		t.Fatal("notifier should skip messages older than 2 minutes")
	}
}

func TestLinuxNotifierHonorsNotificationModeMuted(t *testing.T) {
	store, err := db.New(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer store.Close()

	if err := store.UpsertConversation(&db.Conversation{
		ConversationID:   "muted-thread",
		NotificationMode: db.NotificationModeMuted,
	}); err != nil {
		t.Fatalf("UpsertConversation: %v", err)
	}

	notifier := NewLinuxNotifier(zerolog.Nop(), true, store, "Bob")
	called := false
	notifier.run = func(name string, args ...string) error {
		called = true
		return nil
	}

	notifier.NotifyIncomingMessage(&db.Message{
		MessageID:      "m1",
		ConversationID: "muted-thread",
		Body:           "Hello Bob",
		TimestampMS:    time.Now().UnixMilli(),
	})

	if called {
		t.Fatal("expected no notification for muted conversation")
	}
}
