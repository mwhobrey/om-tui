package v2wire

import (
	"path/filepath"
	"testing"

	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2keys"
)

func TestEnsureConversationIsIdempotentAndDerived(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first, err := EnsureConversation(store, ConversationSpec{
		AccountID:            googleAccountID,
		Platform:             "sms",
		RemoteConversationID: "remote-sms-1",
		Title:                "Alice",
	})
	if err != nil {
		t.Fatalf("EnsureConversation: %v", err)
	}
	wantID := v2keys.DeriveID("conversation", googleAccountID, "remote-sms-1")
	if first.ConversationID != wantID || first.Title != "Alice" {
		t.Fatalf("conversation = %+v, want id %s", first, wantID)
	}

	second, err := EnsureConversation(store, ConversationSpec{
		AccountID:            googleAccountID,
		Platform:             "sms",
		RemoteConversationID: "remote-sms-1",
		Title:                "Alice renamed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ConversationID != first.ConversationID {
		t.Fatalf("second id = %q, want %q", second.ConversationID, first.ConversationID)
	}
}
