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

func TestNextExtraRiverIDSkipsDefaultAndUsed(t *testing.T) {
	got := NextExtraRiverID(ProviderWhatsApp, []string{DefaultWhatsAppRiverID, "whatsapp-2"})
	if got != "whatsapp-3" {
		t.Fatalf("got %q", got)
	}
	if got := NextExtraRiverID(ProviderSignal, nil); got != "signal-2" {
		t.Fatalf("empty used = %q", got)
	}
	if got := NextExtraRiverID(ProviderMessages, []string{DefaultMessagesRiverID}); got != "messages-2" {
		t.Fatalf("messages = %q", got)
	}
}

func TestSafeLiveRiverID(t *testing.T) {
	if SafeLiveRiverID(DefaultWhatsAppRiverID) || SafeLiveRiverID("") {
		t.Fatal("default/empty must be rejected")
	}
	if !SafeLiveRiverID("whatsapp-2") || !SafeLiveRiverID("signal-3") {
		t.Fatal("extra live ids should pass")
	}
	for _, id := range []string{"../whatsapp-2", "whatsapp-2/../etc", `whatsapp\..\tmp`, "messages-2", "slack-T1"} {
		if SafeLiveRiverID(id) {
			t.Fatalf("%q should be rejected", id)
		}
	}
}

func TestScopedIDKeepsDefaultForm(t *testing.T) {
	if got := ScopedID(ProviderWhatsApp, DefaultWhatsAppRiverID, "1555@s.whatsapp.net"); got != "whatsapp:1555@s.whatsapp.net" {
		t.Fatalf("default wa = %q", got)
	}
	if got := ScopedID(ProviderWhatsApp, "whatsapp-2", "1555@s.whatsapp.net"); got != "whatsapp/whatsapp-2/1555@s.whatsapp.net" {
		t.Fatalf("extra wa = %q", got)
	}
	if got := UnscopeID("whatsapp/whatsapp-2/1555@s.whatsapp.net"); got != "1555@s.whatsapp.net" {
		t.Fatalf("unscope = %q", got)
	}
	if got := UnscopeID("whatsapp:1555@s.whatsapp.net"); got != "1555@s.whatsapp.net" {
		t.Fatalf("unscope default = %q", got)
	}
	if got := RiverIDFromScoped("whatsapp/whatsapp-2/1555@s.whatsapp.net"); got != "whatsapp-2" {
		t.Fatalf("river from scoped = %q", got)
	}
	if got := RiverIDFromScoped("whatsapp:1555@s.whatsapp.net"); got != "" {
		t.Fatalf("default river from scoped = %q", got)
	}
}

func TestScopedGroupID(t *testing.T) {
	if got := ScopedGroupID(DefaultSignalRiverID, "abc"); got != "signal-group:abc" {
		t.Fatalf("default group = %q", got)
	}
	if got := ScopedGroupID("signal-2", "abc"); got != "signal-group/signal-2/abc" {
		t.Fatalf("extra group = %q", got)
	}
	if got := UnscopeID("signal-group/signal-2/abc"); got != "abc" {
		t.Fatalf("unscope group = %q", got)
	}
}
func TestDefaultRiverID(t *testing.T) {
	cases := []struct {
		platform string
		want     string
	}{
		{"", DefaultMessagesRiverID},
		{"sms", DefaultMessagesRiverID},
		{"rcs", DefaultMessagesRiverID},
		{"whatsapp", DefaultWhatsAppRiverID},
		{"signal", DefaultSignalRiverID},
		{"slack", ""},
		{"gchat", ""},
		{"imessage", ""},
	}
	for _, tc := range cases {
		if got := DefaultRiverID(tc.platform); got != tc.want {
			t.Fatalf("DefaultRiverID(%q) = %q, want %q", tc.platform, got, tc.want)
		}
	}
}
