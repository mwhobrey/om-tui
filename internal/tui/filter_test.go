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
