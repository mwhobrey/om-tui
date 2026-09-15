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

func TestEnsureBuiltinRiversBackfillWhatsAppAndSignal(t *testing.T) {
	store := newTestStore(t)
	for _, c := range []*Conversation{
		{ConversationID: "sms:1", Name: "Alice", SourcePlatform: "sms", LastMessageTS: 1},
		{ConversationID: "wa:1", Name: "Bob", SourcePlatform: "whatsapp", LastMessageTS: 2},
		{ConversationID: "sig:1", Name: "Carol", SourcePlatform: "signal", LastMessageTS: 3},
		{ConversationID: "gchat:1", Name: "Work", SourcePlatform: "gchat", LastMessageTS: 4},
	} {
		if err := store.UpsertConversation(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.EnsureBuiltinRivers(); err != nil {
		t.Fatal(err)
	}

	assertRiver := func(id, want string) {
		t.Helper()
		got, err := store.GetConversation(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.RiverID != want {
			t.Fatalf("%s river_id = %q, want %q", id, got.RiverID, want)
		}
	}
	assertRiver("sms:1", "messages-default")
	assertRiver("wa:1", "whatsapp-default")
	assertRiver("sig:1", "signal-default")
	assertRiver("gchat:1", "")

	if _, err := store.db.Exec(`UPDATE conversations SET river_id = '' WHERE conversation_id IN ('wa:1', 'sig:1')`); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBuiltinRivers(); err != nil {
		t.Fatal(err)
	}
	assertRiver("wa:1", "whatsapp-default")
	assertRiver("sig:1", "signal-default")

	rivers, err := store.ListRivers()
	if err != nil {
		t.Fatal(err)
	}
	if len(rivers) < 3 {
		t.Fatalf("rivers = %d, want at least 3", len(rivers))
	}
	if rivers[0].ID != "messages-default" || rivers[1].ID != "whatsapp-default" || rivers[2].ID != "signal-default" {
		t.Fatalf("order = %s %s %s", rivers[0].ID, rivers[1].ID, rivers[2].ID)
	}

	wa, err := store.ListConversationsByRiver("whatsapp-default", 10)
	if err != nil || len(wa) != 1 || wa[0].ConversationID != "wa:1" {
		t.Fatalf("whatsapp list = %v err=%v", wa, err)
	}
	sig, err := store.ListConversationsByRiver("signal-default", 10)
	if err != nil || len(sig) != 1 || sig[0].ConversationID != "sig:1" {
		t.Fatalf("signal list = %v err=%v", sig, err)
	}
}

func TestUpsertConversationAssignsDefaultRiver(t *testing.T) {
	store := newTestStore(t)
	if err := store.UpsertConversation(&Conversation{
		ConversationID: "wa:new",
		Name:           "Dana",
		SourcePlatform: "whatsapp",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetConversation("wa:new")
	if err != nil {
		t.Fatal(err)
	}
	if got.RiverID != "whatsapp-default" {
		t.Fatalf("river_id = %q", got.RiverID)
	}
}

