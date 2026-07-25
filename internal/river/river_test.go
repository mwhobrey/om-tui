package river

import "testing"

func TestSlackConversationIDRoundTrip(t *testing.T) {
	id := SlackConversationID("T123", "C456")
	if id != "slack:T123:C456" {
		t.Fatalf("id = %q", id)
	}
	team, ch, ok := ParseSlackConversationID(id)
	if !ok || team != "T123" || ch != "C456" {
		t.Fatalf("parse = %q %q %v", team, ch, ok)
	}
	if _, _, ok := ParseSlackConversationID("sms:1"); ok {
		t.Fatal("expected non-slack id to fail")
	}
}

func TestSlackRiverID(t *testing.T) {
	if got := SlackRiverID("T1"); got != "slack-T1" {
		t.Fatalf("got %q", got)
	}
}
