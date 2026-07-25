package tui

import (
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestConversationFilterValueIncludesNameIDParticipants(t *testing.T) {
	hay := conversationFilterValue(localapi.Conversation{
		ConversationID:     "slack:T1:C123",
		Name:               "#general",
		Participants:       `["Alice","Bob"]`,
		LastMessagePreview: "hello world",
	})
	for _, want := range []string{"#general", "slack:T1:C123", "Alice", "Bob", "hello world"} {
		if !strings.Contains(hay, want) {
			t.Fatalf("FilterValue missing %q in %q", want, hay)
		}
	}
	if strings.Contains(hay, `"`) || strings.Contains(hay, `[`) {
		t.Fatalf("FilterValue should strip JSON punctuation: %q", hay)
	}
}

func TestConvItemFilterValue(t *testing.T) {
	item := convItem{conv: localapi.Conversation{Name: "Pat", ConversationID: "sms:1"}}
	if item.FilterValue() != conversationFilterValue(item.conv) {
		t.Fatal("convItem.FilterValue mismatch")
	}
}

func TestConversationMatchesSlackFilterTokens(t *testing.T) {
	dm := localapi.Conversation{
		Name: "#not-used", StreamKind: "im", UnreadCount: 2,
		LastMessagePreview: "release check",
	}
	channel := localapi.Conversation{
		Name: "#general", StreamKind: "public_channel",
	}
	if !conversationMatchesFilter(dm, "type:dm is:unread release") {
		t.Fatal("expected unread DM to match combined filter")
	}
	if conversationMatchesFilter(channel, "type:dm") {
		t.Fatal("channel matched DM filter")
	}
	if conversationMatchesFilter(channel, "is:unread") {
		t.Fatal("read channel matched unread filter")
	}
	if !conversationMatchesFilter(channel, "type:channel general") {
		t.Fatal("channel did not match channel + text filter")
	}
}
