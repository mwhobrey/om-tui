package tui

import (
	"fmt"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func msgs(from, to int) []localapi.Message {
	var out []localapi.Message
	for i := from; i <= to; i++ {
		out = append(out, localapi.Message{MessageID: fmt.Sprintf("m%03d", i), TimestampMS: int64(i) * 1000})
	}
	return out
}

func TestMergeOlderPrependsAndDedupes(t *testing.T) {
	loaded := msgs(50, 60)
	page := msgs(40, 50) // m050 overlaps
	got := mergeOlder(loaded, page)
	if len(got) != 21 || got[0].MessageID != "m040" || got[10].MessageID != "m050" || got[20].MessageID != "m060" {
		t.Fatalf("got %d, first=%s", len(got), got[0].MessageID)
	}
}

func TestLoadedWindowKeepsScrolledHistory(t *testing.T) {
	for _, tc := range []struct{ n, want int }{{0, 100}, {99, 100}, {250, 250}, {5000, 1000}} {
		m := Model{messages: make([]localapi.Message, tc.n)}
		if got := m.loadedWindow(); got != tc.want {
			t.Fatalf("n=%d got %d want %d", tc.n, got, tc.want)
		}
	}
}

func TestHandleOlderMessages(t *testing.T) {
	m := Model{activeID: "c1", msgGeneration: 3, olderLoading: true, selectedMsg: 0, messages: msgs(200, 210)}

	// Stale generation is ignored and does not clear the in-flight flag.
	next, _ := m.handleOlderMessages(olderMessagesMsg{conversationID: "c1", generation: 2, messages: msgs(100, 199)})
	if got := next.(Model); len(got.messages) != 11 || !got.olderLoading {
		t.Fatalf("stale page applied: %d msgs loading=%v", len(got.messages), got.olderLoading)
	}

	// A full page keeps the selection on the same message and stays non-exhausted.
	next, _ = m.handleOlderMessages(olderMessagesMsg{conversationID: "c1", generation: 3, messages: msgs(100, 199)})
	got := next.(Model)
	if len(got.messages) != 111 || got.olderLoading || got.localExhausted {
		t.Fatalf("full page: %d loading=%v exhausted=%v", len(got.messages), got.olderLoading, got.localExhausted)
	}
	if got.messages[got.selectedMsg].MessageID != "m200" {
		t.Fatalf("selection moved to %s", got.messages[got.selectedMsg].MessageID)
	}

	// A short page means the store has nothing older.
	next, _ = got.handleOlderMessages(olderMessagesMsg{conversationID: "c1", generation: 3, messages: msgs(90, 99)})
	if g := next.(Model); !g.localExhausted {
		t.Fatal("short page should mark the store exhausted")
	}
}

func TestHistoryDoneNeedsPhoneOnlyForGoogleMessages(t *testing.T) {
	sms := Model{activeID: "c1", localExhausted: true}
	if sms.historyDone() {
		t.Fatal("SMS thread with an empty store should still try the phone")
	}
	sms.phoneExhausted = true
	if !sms.historyDone() {
		t.Fatal("SMS thread is done once the phone is exhausted too")
	}

	wa := Model{
		activeID: "w1", activeRiverID: "whatsapp-default", localExhausted: true,
		rivers: []localapi.River{{ID: "whatsapp-default", Provider: "whatsapp"}},
	}
	if !wa.historyDone() {
		t.Fatal("WhatsApp has no phone fetch; empty store means done")
	}

	imported := Model{
		activeID: "g1", localExhausted: true,
		paletteConvs: []localapi.Conversation{{ConversationID: "g1", SourcePlatform: "gchat"}},
	}
	if imported.phoneCapable() || !imported.historyDone() {
		t.Fatal("non-SMS threads in the Messages river can't be fetched from the phone")
	}
}

func TestHandlePhoneOlderStopsWhenNothingLeft(t *testing.T) {
	m := Model{activeID: "c1", msgGeneration: 1, olderLoading: true, localExhausted: true}

	// Stale generation is ignored.
	next, _ := m.handlePhoneOlder(phoneOlderMsg{conversationID: "c1", generation: 0})
	if !next.(Model).olderLoading {
		t.Fatal("stale result cleared the in-flight flag")
	}

	next, cmd := m.handlePhoneOlder(phoneOlderMsg{conversationID: "c1", generation: 1,
		result: localapi.OlderPhoneResult{Exhausted: true}})
	got := next.(Model)
	if cmd != nil || got.olderLoading || !got.phoneExhausted || got.info != "Start of history" {
		t.Fatalf("exhausted: cmd=%v loading=%v phoneExhausted=%v info=%q", cmd != nil, got.olderLoading, got.phoneExhausted, got.info)
	}

	// Walked its page cap without reaching the cutoff: not done, ask again.
	next, cmd = m.handlePhoneOlder(phoneOlderMsg{conversationID: "c1", generation: 1})
	got = next.(Model)
	if cmd != nil || got.phoneExhausted || got.historyDone() {
		t.Fatal("a zero-result, non-exhausted fetch must stay retryable")
	}
}
