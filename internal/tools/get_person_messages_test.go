package tools

import (
	"testing"

	"github.com/maxghenis/openmessage/internal/db"
)

func TestFindPersonConversationsIncludesGroupsOnlyWhenAsked(t *testing.T) {
	a := testApp(t)
	for _, conversation := range []*db.Conversation{
		{ConversationID: "dm", Name: "Alice", Participants: `[{"name":"Alice","number":"+1"}]`, LastMessageTS: 3},
		{ConversationID: "group", Name: "Alice friends", IsGroup: true, Participants: `[{"name":"Alice","number":"+1"}]`, LastMessageTS: 2},
		{ConversationID: "other", Name: "Bob", LastMessageTS: 1},
	} {
		if err := a.Store.UpsertConversation(conversation); err != nil {
			t.Fatalf("UpsertConversation(%q): %v", conversation.ConversationID, err)
		}
	}

	withGroups, err := findPersonConversations(a.Store, "Alice", true)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationIDs(t, withGroups, "dm", "group")

	withoutGroups, err := findPersonConversations(a.Store, "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationIDs(t, withoutGroups, "dm")
}

func assertConversationIDs(t *testing.T, conversations []*db.Conversation, want ...string) {
	t.Helper()
	if len(conversations) != len(want) {
		t.Fatalf("conversation IDs = %d rows %+v, want %d %v", len(conversations), conversations, len(want), want)
	}
	got := make(map[string]bool, len(conversations))
	for _, conversation := range conversations {
		got[conversation.ConversationID] = true
	}
	for _, wantID := range want {
		if !got[wantID] {
			t.Fatalf("missing conversation %q in %+v", wantID, conversations)
		}
	}
}
