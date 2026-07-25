package db

import "testing"

func TestEnsureMessagesRiverBackfill(t *testing.T) {
	store := newTestStore(t)
	if err := store.UpsertConversation(&Conversation{
		ConversationID: "c1",
		Name:           "Alice",
		SourcePlatform: "sms",
		LastMessageTS:  100,
		UnreadCount:    2,
	}); err != nil {
		t.Fatal(err)
	}
	r, err := store.EnsureMessagesRiver()
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.ID != "messages-default" {
		t.Fatalf("river = %+v", r)
	}
	got, err := store.GetConversation("c1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RiverID != "messages-default" {
		t.Fatalf("river_id = %q", got.RiverID)
	}
	unread, err := store.UnreadCountsByRiver()
	if err != nil {
		t.Fatal(err)
	}
	if unread["messages-default"] != 2 {
		t.Fatalf("unread = %#v", unread)
	}
	listed, err := store.ListConversationsByRiver("messages-default", 10)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %v err=%v", listed, err)
	}
}
