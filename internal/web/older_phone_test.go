package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
)

func TestOlderPhoneRoute(t *testing.T) {
	store, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, c := range []*db.Conversation{
		{ConversationID: "sms-1", Name: "Alice", SourcePlatform: "sms"},
		{ConversationID: "wa-1", Name: "Bob", SourcePlatform: "whatsapp"},
	} {
		if err := store.UpsertConversation(c); err != nil {
			t.Fatal(err)
		}
	}

	var gotID string
	var gotBefore int64
	var gotWant int
	var fail error
	h := APIHandlerWithOptions(store, nil, zerolog.Nop(), nil, APIOptions{
		FetchOlderGoogleHistory: func(id string, before int64, want int) (int, bool, error) {
			gotID, gotBefore, gotWant = id, before, want
			return 7, true, fail
		},
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	post := func(path string) *http.Response {
		t.Helper()
		resp, err := http.Post(srv.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	resp := post("/api/conversations/sms-1/older-phone?before=12345&limit=40")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Fetched   int  `json:"fetched"`
		Exhausted bool `json:"exhausted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Fetched != 7 || !body.Exhausted || gotID != "sms-1" || gotBefore != 12345 || gotWant != 40 {
		t.Fatalf("body=%+v id=%q before=%d want=%d", body, gotID, gotBefore, gotWant)
	}

	if s := post("/api/conversations/sms-1/older-phone").StatusCode; s != 400 {
		t.Fatalf("missing before: status %d, want 400", s)
	}
	if s := post("/api/conversations/wa-1/older-phone?before=1").StatusCode; s != 400 {
		t.Fatalf("non-Google conversation: status %d, want 400", s)
	}

	fail = fmt.Errorf("phone unreachable")
	if s := post("/api/conversations/sms-1/older-phone?before=1").StatusCode; s != 502 {
		t.Fatalf("daemon error: status %d, want 502", s)
	}

	get, err := http.Get(srv.URL + "/api/conversations/sms-1/older-phone?before=1")
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	if get.StatusCode != 405 {
		t.Fatalf("GET: status %d, want 405", get.StatusCode)
	}
}
