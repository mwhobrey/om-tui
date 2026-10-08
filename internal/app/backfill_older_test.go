package app

import (
	"fmt"
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func olderTestPages() [][]*gmproto.Message {
	// Newest-first, as the phone returns them.
	return [][]*gmproto.Message{
		{makeMsg("m9", "c1", "nine", 900), makeMsg("m8", "c1", "eight", 800), makeMsg("m7", "c1", "seven", 700)},
		{makeMsg("m6", "c1", "six", 600), makeMsg("m5", "c1", "five", 500), makeMsg("m4", "c1", "four", 400)},
		{makeMsg("m3", "c1", "three", 300), makeMsg("m2", "c1", "two", 200), makeMsg("m1", "c1", "one", 100)},
	}
}

func TestFetchOlderGoogleHistoryWalksFromSavedCursor(t *testing.T) {
	mock := &mockGMClient{
		messages:   map[string][][]*gmproto.Message{"c1": olderTestPages()},
		fetchCalls: map[string]int{},
	}
	a := newTestApp(t, mock)

	// Cutoff 650: page 0 is all newer (walked, stored, not counted); page 1
	// supplies the 3 older messages, which satisfies want=2.
	res, err := a.FetchOlderGoogleHistory("c1", 650, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 3 || res.Exhausted {
		t.Fatalf("first call = %+v", res)
	}
	if mock.fetchCalls["c1"] != 2 {
		t.Fatalf("first call fetched %d pages, want 2", mock.fetchCalls["c1"])
	}

	// The next call resumes from the saved cursor (page 2), not the newest
	// message, and reaches the end of history.
	res, err = a.FetchOlderGoogleHistory("c1", 400, 100)
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 3 || !res.Exhausted {
		t.Fatalf("second call = %+v", res)
	}
	if mock.fetchCalls["c1"] != 3 {
		t.Fatalf("resume re-walked from the start: %d page fetches, want 3 total", mock.fetchCalls["c1"])
	}

	// Once exhausted, no more phone traffic.
	res, err = a.FetchOlderGoogleHistory("c1", 100, 100)
	if err != nil || !res.Exhausted || res.Fetched != 0 {
		t.Fatalf("third call = %+v, %v", res, err)
	}
	if mock.fetchCalls["c1"] != 3 {
		t.Fatalf("exhausted call hit the phone: %d fetches", mock.fetchCalls["c1"])
	}

	stored, err := a.Store.GetMessagesByConversation("c1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 9 {
		t.Fatalf("store has %d messages, want 9", len(stored))
	}
}

func TestFetchOlderGoogleHistoryErrorsResetCursor(t *testing.T) {
	mock := &mockGMClient{
		messages:   map[string][][]*gmproto.Message{"c1": olderTestPages()},
		fetchCalls: map[string]int{},
	}
	a := newTestApp(t, mock)

	if _, err := a.FetchOlderGoogleHistory("c1", 650, 2); err != nil {
		t.Fatal(err)
	}
	mock.fetchMsgErrors = map[string]error{"c1": fmt.Errorf("boom")}
	if _, err := a.FetchOlderGoogleHistory("c1", 400, 100); err == nil {
		t.Fatal("want error from phone")
	}
	if _, ok := a.older.state.Load("c1"); ok {
		t.Fatal("failed fetch should drop the saved cursor")
	}
}

func TestFetchOlderGoogleHistoryGuards(t *testing.T) {
	a := &App{}
	if _, err := a.FetchOlderGoogleHistory("c1", 100, 10); err == nil {
		t.Fatal("want error when client is nil")
	}

	a = newTestApp(t, &mockGMClient{})
	if _, err := a.FetchOlderGoogleHistory("", 100, 10); err == nil {
		t.Fatal("want error for empty conversation id")
	}
	if _, err := a.FetchOlderGoogleHistory("c1", 0, 10); err == nil {
		t.Fatal("want error for zero cutoff")
	}

	a.backfillRunning.Store(true)
	if _, err := a.FetchOlderGoogleHistory("c1", 100, 10); err == nil {
		t.Fatal("want error while a backfill is running")
	}
}
