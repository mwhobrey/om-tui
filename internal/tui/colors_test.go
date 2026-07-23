package tui

import (
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestParticipantKeyPrefersNumber(t *testing.T) {
	key := participantKey(localapi.Message{
		SenderNumber: "+15551212",
		SenderName:   "Alice",
	})
	if key != "+15551212" {
		t.Fatalf("key = %q, want number", key)
	}
}

func TestParticipantKeyFallsBackToName(t *testing.T) {
	key := participantKey(localapi.Message{SenderName: "Bob"})
	if key != "bob" {
		t.Fatalf("key = %q, want bob", key)
	}
}

func TestParticipantKeyDefault(t *testing.T) {
	if got := participantKey(localapi.Message{}); got != "them" {
		t.Fatalf("key = %q, want them", got)
	}
}

func TestParticipantStyleStableForSameKey(t *testing.T) {
	a := localapi.Message{SenderNumber: "+15550001", Body: "hi"}
	b := localapi.Message{SenderNumber: "+15550001", Body: "again"}
	if participantStyle(a).GetForeground() != participantStyle(b).GetForeground() {
		t.Fatal("same sender should map to the same color")
	}
}

func TestParticipantStyleFromMeUsesMeStyle(t *testing.T) {
	got := participantStyle(localapi.Message{IsFromMe: true, SenderNumber: "+1"})
	if got.GetForeground() != meStyle.GetForeground() {
		t.Fatal("IsFromMe should use meStyle")
	}
}

func TestParticipantStyleDifferentKeysOftenDiffer(t *testing.T) {
	keys := []string{"+15550001", "+15550002", "carol", "dave", "erin", "frank"}
	seen := map[int]bool{}
	for _, k := range keys {
		seen[participantColorIndex(k)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected multiple palette slots across keys, got %d", len(seen))
	}
}
