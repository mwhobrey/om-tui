package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/export"
)

func TestLoadExportMessagesPagesAndHonorsRange(t *testing.T) {
	store := newThreadTestStore(t)
	// Enough messages to force several pages, several sharing one millisecond.
	base := dayT("2026-04-01", 0)
	n := exportPageSize*2 + 37
	for i := 0; i < n; i++ {
		mustUpsertMsg(t, store, &db.Message{
			MessageID: fmt.Sprintf("p%05d", i), ConversationID: "ava-1",
			Body: "x", TimestampMS: base + int64(i/3),
		})
	}

	all, err := loadExportMessages(store, "ava-1", export.Range{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != n+1 { // +1 for the seeded Ava message
		t.Fatalf("got %d messages, want %d", len(all), n+1)
	}
	for i := 1; i < len(all); i++ {
		if all[i].TimestampMS < all[i-1].TimestampMS {
			t.Fatalf("not chronological at %d", i)
		}
	}

	// Inclusive bounds on both ends.
	lo, hi := base+10, base+20
	got, err := loadExportMessages(store, "ava-1", export.Range{SinceMS: lo, UntilMS: hi})
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for i := 0; i < n; i++ {
		if ts := base + int64(i/3); ts >= lo && ts <= hi {
			want++
		}
	}
	if len(got) != want || want == 0 {
		t.Fatalf("range got %d, want %d", len(got), want)
	}
	if got[0].TimestampMS != lo || got[len(got)-1].TimestampMS != hi {
		t.Fatalf("bounds not inclusive: %d..%d", got[0].TimestampMS, got[len(got)-1].TimestampMS)
	}
}

func TestResolveExportConversation(t *testing.T) {
	store := newThreadTestStore(t)

	c, err := resolveExportConversation(store, "2787")
	if err != nil || c.ConversationID != "2787" {
		t.Fatalf("by id: %v %v", c, err)
	}
	c, err = resolveExportConversation(store, "korey")
	if err != nil || c.ConversationID != "2787" {
		t.Fatalf("by name: %v %v", c, err)
	}
	if _, err = resolveExportConversation(store, "nobody-here"); err == nil {
		t.Fatal("want error for no match")
	}
	// "+1555" is in both participants lists: must refuse to guess.
	if _, err = resolveExportConversation(store, "+1555"); err == nil || !strings.Contains(err.Error(), "2 conversations") {
		t.Fatalf("want ambiguity error, got %v", err)
	}
}
