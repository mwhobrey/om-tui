package db

import "testing"

func TestUpsertSlackConversationPreservesUnreadAndDerivesKind(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conv := &Conversation{
		ConversationID: "slack:T1:C1",
		Name:           "#general",
		Participants:   `[{"id":"C1","kind":"private_channel"}]`,
		UnreadCount:    3,
		SourcePlatform: "slack",
		RiverID:        "slack-T1",
	}
	if err := store.UpsertConversation(conv); err != nil {
		t.Fatal(err)
	}
	conv.Name = "#renamed"
	conv.UnreadCount = 0
	if err := store.UpsertSlackConversation(conv); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetConversation(conv.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UnreadCount != 3 || got.Name != "#renamed" || got.StreamKind != "private_channel" {
		t.Fatalf("conversation = %+v", got)
	}
}

func TestSlackUserAndCursorRoundTrip(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertSlackUser(SlackUser{
		RiverID: "slack-T1", UserID: "U1", DisplayName: "Alice", UpdatedAtMS: 42,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := store.GetSlackUser("slack-T1", "U1")
	if err != nil || user.Name() != "Alice" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
	if err := store.UpsertSlackSyncCursor(SlackSyncCursor{
		RiverID: "slack-T1", ChannelID: "C1", OldestTS: "1.0", NewestTS: "2.0",
	}); err != nil {
		t.Fatal(err)
	}
	cursor, err := store.GetSlackSyncCursor("slack-T1", "C1")
	if err != nil || cursor.OldestTS != "1.0" || cursor.NewestTS != "2.0" {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
}

func TestSearchMessagesFilteredByRiver(t *testing.T) {
	store, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, conv := range []*Conversation{
		{ConversationID: "slack:T1:C1", Name: "#one", SourcePlatform: "slack", RiverID: "slack-T1"},
		{ConversationID: "slack:T2:C2", Name: "#two", SourcePlatform: "slack", RiverID: "slack-T2"},
	} {
		if err := store.UpsertSlackConversation(conv); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertMessage(&Message{
			MessageID: conv.ConversationID + ":1", ConversationID: conv.ConversationID,
			Body: "needle", SourcePlatform: "slack", SourceID: conv.ConversationID + ":1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.SearchMessagesFiltered("needle", SearchFilter{RiverID: "slack-T2", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ConversationID != "slack:T2:C2" {
		t.Fatalf("river search = %+v", got)
	}
}
