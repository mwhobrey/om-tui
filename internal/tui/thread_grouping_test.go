package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestSameThreadTurn(t *testing.T) {
	base := int64(1_700_000_000_000)
	alice1 := localapi.Message{SenderName: "Alice", TimestampMS: base}
	alice2 := localapi.Message{SenderName: "Alice", TimestampMS: base + 60_000}
	aliceLater := localapi.Message{SenderName: "Alice", TimestampMS: base + 10*60_000}
	bob := localapi.Message{SenderName: "Bob", TimestampMS: base + 60_000}
	reply := localapi.Message{SenderName: "Alice", TimestampMS: base + 60_000, ReplyToID: "slack:c:1"}
	fromReply := localapi.Message{SenderName: "Alice", TimestampMS: base + 60_000}

	if !sameThreadTurn(alice1, alice2, "Alice") {
		t.Fatal("expected same sender within the group window to group")
	}
	if sameThreadTurn(alice1, aliceLater, "Alice") {
		t.Fatal("expected a 10-minute gap to break the group")
	}
	if sameThreadTurn(alice1, bob, "Bob") {
		t.Fatal("expected a different sender to break the group")
	}
	reply.ReplyToID = "slack:c:1"
	if sameThreadTurn(reply, fromReply, "Alice") {
		t.Fatal("expected a reply to never be grouped into")
	}
}

func TestRenderMessagesGroupsConsecutiveSameSender(t *testing.T) {
	base := int64(1_700_000_000_000)
	msgs := []localapi.Message{
		{MessageID: "1", SenderName: "Alice", Body: "hey are you around?", TimestampMS: base},
		{MessageID: "2", SenderName: "Alice", Body: "sorry to bug you", TimestampMS: base + 30_000},
		{MessageID: "3", IsFromMe: true, Body: "yeah what's up", TimestampMS: base + 90_000},
	}
	out := renderMessages(msgs, 60, 2, func(string) string { return "" }, "Alice")
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 rendered lines (day divider + 3 messages), got %d: %q", len(lines), out)
	}
	// lines[0] is the day divider; message rows start at lines[1].
	if !strings.Contains(lines[1], "Alice:") {
		t.Fatalf("first message line should carry the sender name: %q", lines[1])
	}
	if strings.Contains(lines[2], "Alice:") {
		t.Fatalf("grouped continuation line should not repeat the sender name: %q", lines[2])
	}
	if !strings.Contains(lines[2], "sorry to bug you") {
		t.Fatalf("grouped line should still carry its body: %q", lines[2])
	}
}

func TestRenderMessagesDayDividerOnDateChange(t *testing.T) {
	day1 := time.Date(2024, time.March, 5, 9, 0, 0, 0, time.Local)
	day2 := time.Date(2024, time.March, 6, 9, 30, 0, 0, time.Local)
	msgs := []localapi.Message{
		{MessageID: "1", SenderName: "Alice", Body: "morning", TimestampMS: day1.UnixMilli()},
		{MessageID: "2", SenderName: "Alice", Body: "next day", TimestampMS: day2.UnixMilli()},
	}
	out := renderMessages(msgs, 60, -1, func(string) string { return "" }, "Alice")
	if got := strings.Count(out, "Mar 5"); got != 1 {
		t.Fatalf("expected exactly one Mar 5 divider, got %d in %q", got, out)
	}
	if got := strings.Count(out, "Mar 6"); got != 1 {
		t.Fatalf("expected exactly one Mar 6 divider, got %d in %q", got, out)
	}
	if strings.Contains(out, "09:00") == false || strings.Contains(out, "09:30") == false {
		t.Fatalf("expected time-only prefixes for both messages: %q", out)
	}
}

func TestRenderMessagesDoesNotGroupAcrossReply(t *testing.T) {
	base := int64(1_700_000_000_000)
	msgs := []localapi.Message{
		{MessageID: "1", SenderName: "Alice", Body: "root message", TimestampMS: base},
		{MessageID: "2", SenderName: "Alice", Body: "a reply", TimestampMS: base + 5_000, ReplyToID: "slack:c:1"},
	}
	out := renderMessages(msgs, 60, -1, func(string) string { return "" }, "Alice")
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 rendered lines (day divider + 2 messages), got %d: %q", len(lines), out)
	}
	// lines[0] is the day divider; message rows start at lines[1].
	if !strings.Contains(lines[2], "Alice:") {
		t.Fatalf("a reply line should still carry its own sender name, not be grouped: %q", lines[2])
	}
}
