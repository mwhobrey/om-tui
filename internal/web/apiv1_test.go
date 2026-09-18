package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/ingest"
	"github.com/maxghenis/openmessage/internal/media"
	"github.com/maxghenis/openmessage/internal/messaging"
	"github.com/maxghenis/openmessage/internal/storage/blob"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2keys"
	"github.com/maxghenis/openmessage/internal/v2wire"
)

func TestV1RoutesReturnServiceUnavailableWhenDisabled(t *testing.T) {
	ts := newV1RecorderHarness(t, APIOptions{})

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/outbox/messages"},
		{http.MethodPost, "/api/v1/outbox/media"},
		{http.MethodGet, "/api/v1/outbox"},
		{http.MethodGet, "/api/v1/outbox/outbox-1"},
		{http.MethodPost, "/api/v1/outbox/outbox-1/cancel"},
		{http.MethodPost, "/api/v1/outbox/outbox-1/retry"},
		{http.MethodPost, "/api/v1/outbox/outbox-1/send-again"},
		{http.MethodPost, "/api/v1/outbox/outbox-1/repair"},
		{http.MethodGet, "/api/v1/outbox/outbox-1/media"},
		{http.MethodGet, "/api/v1/messages/message-1/attachments/0"},
		{http.MethodGet, "/api/v1/not-a-route"},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			req := httptest.NewRequest(test.method, "http://127.0.0.1"+test.path, bytes.NewReader(nil))
			resp := ts.do(t, req)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusServiceUnavailable {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, body)
			}
			var payload map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["error"] != "v2_send_disabled" {
				t.Fatalf("error = %q, want v2_send_disabled", payload["error"])
			}
		})
	}
}

func TestStatusReportsV2SendAvailability(t *testing.T) {
	tests := []struct {
		name string
		v2   *V2Options
		want bool
	}{
		{name: "disabled", want: false},
		{name: "enabled", v2: &V2Options{}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := newV1RecorderHarness(t, APIOptions{V2: test.v2})
			resp := ts.do(t, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/status", nil))
			defer resp.Body.Close()

			var payload map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if got, ok := payload["v2_send"].(bool); !ok || got != test.want {
				t.Fatalf("v2_send = %#v, want %t", payload["v2_send"], test.want)
			}
		})
	}
}

func TestStatusReportsV2IngestCounters(t *testing.T) {
	tests := []struct {
		name           string
		provider       func() map[string]ingest.CounterSnapshot
		wantEnabled    bool
		wantPerAccount map[string]ingest.CounterSnapshot
	}{
		{
			name:           "disabled",
			wantPerAccount: map[string]ingest.CounterSnapshot{},
		},
		{
			name: "enabled",
			provider: func() map[string]ingest.CounterSnapshot {
				return map[string]ingest.CounterSnapshot{
					"google:primary": {
						Appended:          7,
						DecodedEvents:     6,
						Projected:         5,
						ReactionsApplied:  4,
						ReactionsRemoved:  3,
						ReactionsOrphaned: 2,
						Quarantined:       1,
					},
				}
			},
			wantEnabled: true,
			wantPerAccount: map[string]ingest.CounterSnapshot{
				"google:primary": {
					Appended:          7,
					DecodedEvents:     6,
					Projected:         5,
					ReactionsApplied:  4,
					ReactionsRemoved:  3,
					ReactionsOrphaned: 2,
					Quarantined:       1,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := newV1RecorderHarness(t, APIOptions{V2IngestCounters: test.provider})
			resp := ts.do(t, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/status", nil))
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}

			var payload struct {
				V2Ingest struct {
					Enabled    bool                              `json:"enabled"`
					PerAccount map[string]ingest.CounterSnapshot `json:"per_account"`
				} `json:"v2_ingest"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.V2Ingest.Enabled != test.wantEnabled {
				t.Fatalf("v2_ingest.enabled = %t, want %t", payload.V2Ingest.Enabled, test.wantEnabled)
			}
			if !reflect.DeepEqual(payload.V2Ingest.PerAccount, test.wantPerAccount) {
				t.Fatalf("v2_ingest.per_account = %#v, want %#v", payload.V2Ingest.PerAccount, test.wantPerAccount)
			}
			if test.wantEnabled {
				assertReactionCounterJSONNames(t, body, "google:primary")
			}
		})
	}
}

func assertReactionCounterJSONNames(t *testing.T, body []byte, accountID string) {
	t.Helper()
	var payload struct {
		V2Ingest struct {
			PerAccount map[string]map[string]json.RawMessage `json:"per_account"`
		} `json:"v2_ingest"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	counters, ok := payload.V2Ingest.PerAccount[accountID]
	if !ok {
		t.Fatalf("v2_ingest.per_account missing %q", accountID)
	}
	for _, key := range []string{"reactions_applied", "reactions_removed", "reactions_orphaned"} {
		if _, ok := counters[key]; !ok {
			t.Errorf("v2_ingest.per_account[%q] missing JSON counter %q", accountID, key)
		}
	}
	if _, ok := counters["reactions_dropped"]; ok {
		t.Errorf("v2_ingest.per_account[%q] retained retired JSON counter %q", accountID, "reactions_dropped")
	}
}

func TestV1SubmitRejectsTooShortScheduleAtHandler(t *testing.T) {
	// The dependencies are deliberately otherwise empty: schedule validation must
	// happen before mirroring or submission and must therefore return 400 rather
	// than dereferencing a v2 dependency.
	ts := newV1RecorderHarness(t, APIOptions{V2: &V2Options{}})
	notBeforeMS := time.Now().Add(time.Second).UnixMilli()
	body := fmt.Sprintf(`{
		"conversation_id":"conversation-1",
		"body":"later",
		"idempotency_key":"schedule-too-soon",
		"not_before_ms":%d
	}`, notBeforeMS)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/outbox/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := ts.do(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, raw)
	}
}

func TestV1SubmitRequiresIdempotencyKeyBeforeMirroring(t *testing.T) {
	ts := newV1RecorderHarness(t, APIOptions{V2: &V2Options{}})
	req := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1/api/v1/outbox/messages",
		bytes.NewBufferString(`{"conversation_id":"conversation-1","body":"hello"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp := ts.do(t, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, raw)
	}
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "idempotency_key is required" {
		t.Fatalf("error = %q, want required-key error", payload["error"])
	}
}

func TestV1SubmitRequiresConversationBeforeMirroring(t *testing.T) {
	ts := newV1RecorderHarness(t, APIOptions{V2: &V2Options{}})
	req := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1/api/v1/outbox/messages",
		bytes.NewBufferString(`{"body":"hello","idempotency_key":"missing-conversation"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp := ts.do(t, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, raw)
	}
}

func TestV1ErrorStatusMapping(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{name: "idempotency conflict", err: messaging.ErrIdempotencyConflict, wantStatus: http.StatusConflict},
		{name: "invalid command", err: messaging.ErrInvalidCommand, wantStatus: http.StatusBadRequest},
		{name: "invalid state", err: messaging.ErrInvalidState, wantStatus: http.StatusConflict},
		{name: "platform", err: v2wire.ErrPlatformNotSendable, wantStatus: http.StatusNotImplemented},
		{name: "reply", err: v2wire.ErrReplyTargetUnavailable, wantStatus: http.StatusUnprocessableEntity, wantMessage: "reply_target_unavailable"},
		{name: "media unavailable", err: media.ErrUnavailable, wantStatus: http.StatusServiceUnavailable},
		{name: "not found", err: sqlite.ErrNotFound, wantStatus: http.StatusNotFound, wantMessage: "not found"},
		{name: "too large", err: messaging.ErrTooLarge, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "internal", err: fmt.Errorf("boom"), wantStatus: http.StatusInternalServerError, wantMessage: "internal server error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, message := v1ErrorResponse(test.err)
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
			if test.wantMessage != "" && message != test.wantMessage {
				t.Fatalf("message = %q, want %q", message, test.wantMessage)
			}
		})
	}
}

func TestLegacyMediaServesV2BlobBeforePlatformDownloader(t *testing.T) {
	blobs, err := blob.New(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("v2 projected media")
	ref, err := blobs.Put(context.Background(), bytes.NewReader(content), "text/plain", int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}

	downloaderCalled := false
	ts := newV1RecorderHarness(t, APIOptions{
		V2: &V2Options{Blobs: blobs},
		DownloadWhatsAppMedia: func(*db.Message) ([]byte, string, error) {
			downloaderCalled = true
			return nil, "", fmt.Errorf("platform downloader must not be called")
		},
	})
	if err := ts.store.UpsertConversation(&db.Conversation{
		ConversationID: "whatsapp:thread-1",
		SourcePlatform: "whatsapp",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ts.store.UpsertMessage(&db.Message{
		MessageID:      "whatsapp:message-1",
		ConversationID: "whatsapp:thread-1",
		MediaID:        "v2blob:" + ref.Hash,
		MimeType:       "text/plain",
		SourcePlatform: "whatsapp",
		TimestampMS:    1,
	}); err != nil {
		t.Fatal(err)
	}

	resp := ts.do(t, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/media/whatsapp:message-1", nil))
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, got)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("body = %q, want %q", got, content)
	}
	if downloaderCalled {
		t.Fatal("WhatsApp downloader was called for v2blob media")
	}
}

func TestMarkReadBestEffortWritesV2Cursor(t *testing.T) {
	v2Store, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2Store.Close() })

	ts := newV1RecorderHarness(t, APIOptions{V2: &V2Options{V2Store: v2Store}})
	if err := ts.store.UpsertConversation(&db.Conversation{
		ConversationID: "google-thread-1",
		Name:           "Alice",
		SourcePlatform: "sms",
		UnreadCount:    3,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/mark-read", bytes.NewBufferString(`{"conversation_id":"google-thread-1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := ts.do(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, raw)
	}

	cursor, err := v2Store.GetReadCursor("local-primary:google-primary", "google-thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.AccountID != "google-primary" || cursor.LastReadMessageID != nil {
		t.Fatalf("cursor = %+v, want google account with nil message ref", cursor)
	}
	if cursor.LastReadAtMS <= 0 || cursor.UpdatedAtMS <= 0 {
		t.Fatalf("cursor timestamps = %+v, want positive", cursor)
	}
}

func TestMarkReadV2PrimaryWritesNativeCursor(t *testing.T) {
	v2Store, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2Store.Close() })
	nowMS := time.Now().UnixMilli()
	if err := v2Store.UpsertAccount(sqlite.Account{
		AccountID:   "slack-T1",
		BridgeKey:   "slack_web",
		DisplayName: "Acme",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := v2Store.UpsertConversation(sqlite.Conversation{
		ConversationID:       "hashed-slack-conv",
		AccountID:            "slack-T1",
		RemoteConversationID: "C99",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#general",
		NotificationMode:     sqlite.NotificationModeAll,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}

	ts := newV1RecorderHarness(t, APIOptions{
		V2Primary: true,
		V2:        &V2Options{V2Store: v2Store},
	})
	if err := ts.store.UpsertConversation(&db.Conversation{
		ConversationID: "legacy-should-stay-unread",
		Name:           "Alice",
		SourcePlatform: "sms",
		UnreadCount:    3,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/mark-read", bytes.NewBufferString(`{"conversation_id":"hashed-slack-conv"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := ts.do(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, raw)
	}

	legacy, err := ts.store.GetConversation("legacy-should-stay-unread")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.UnreadCount != 3 {
		t.Fatalf("legacy unread count = %d, want 3", legacy.UnreadCount)
	}
	cursor, err := v2Store.GetReadCursor("local-primary:slack-T1", "hashed-slack-conv")
	if err != nil {
		t.Fatal(err)
	}
	if cursor.AccountID != "slack-T1" {
		t.Fatalf("cursor = %+v, want slack-T1", cursor)
	}
}

func TestResolveSlackLiveIDsMapsHashedV2Rows(t *testing.T) {
	v2Store, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2Store.Close() })
	nowMS := time.Now().UnixMilli()
	if err := v2Store.UpsertAccount(sqlite.Account{
		AccountID:   "slack-T1",
		BridgeKey:   "slack_web",
		DisplayName: "Acme",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := v2Store.UpsertConversation(sqlite.Conversation{
		ConversationID:       "hashed-slack-conv",
		AccountID:            "slack-T1",
		RemoteConversationID: "C99",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#general",
		NotificationMode:     sqlite.NotificationModeAll,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	repository, err := sqlite.NewMessageRepository(v2Store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ImportMessage(context.Background(), sqlite.MessageProjection{
		Message: sqlite.Message{
			MessageID:       "v2-root",
			ConversationID:  "hashed-slack-conv",
			AccountID:       "slack-T1",
			RemoteMessageID: "123.456",
			Direction:       sqlite.MessageDirectionIncoming,
			Body:            "root",
			State:           sqlite.MessageStateActive,
			OccurredAtMS:    nowMS,
		},
	}); err != nil {
		t.Fatal(err)
	}

	primary := APIOptions{V2Primary: true, V2: &V2Options{V2Store: v2Store}}
	legacy := APIOptions{}
	tests := []struct {
		name           string
		opts           APIOptions
		conversationID string
		rootID         string
		wantConv       string
		wantRoot       string
	}{
		{
			name:           "passthrough without primary",
			opts:           legacy,
			conversationID: "hashed-slack-conv",
			rootID:         "v2-root",
			wantConv:       "hashed-slack-conv",
			wantRoot:       "v2-root",
		},
		{
			name:           "passthrough legacy slack ids",
			opts:           primary,
			conversationID: "slack:T1:C99",
			rootID:         "slack:C99:123.456",
			wantConv:       "slack:T1:C99",
			wantRoot:       "slack:C99:123.456",
		},
		{
			name:           "hashed conversation only",
			opts:           primary,
			conversationID: "hashed-slack-conv",
			wantConv:       "slack:T1:C99",
		},
		{
			name:           "legacy root on hashed conversation",
			opts:           primary,
			conversationID: "hashed-slack-conv",
			rootID:         "slack:C99:123.456",
			wantConv:       "slack:T1:C99",
			wantRoot:       "slack:C99:123.456",
		},
		{
			name:           "v2 message id",
			opts:           primary,
			conversationID: "hashed-slack-conv",
			rootID:         "v2-root",
			wantConv:       "slack:T1:C99",
			wantRoot:       "slack:C99:123.456",
		},
		{
			name:           "raw ts fallback",
			opts:           primary,
			conversationID: "hashed-slack-conv",
			rootID:         "123.456",
			wantConv:       "slack:T1:C99",
			wantRoot:       "slack:C99:123.456",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotConv, gotRoot := resolveSlackLiveIDs(test.opts, test.conversationID, test.rootID)
			if gotConv != test.wantConv || gotRoot != test.wantRoot {
				t.Fatalf("resolveSlackLiveIDs() = (%q, %q), want (%q, %q)", gotConv, gotRoot, test.wantConv, test.wantRoot)
			}
		})
	}
}

func TestMapSlackLiveMessagesToV2RewritesHashedIDs(t *testing.T) {
	v2Store, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2Store.Close() })
	nowMS := time.Now().UnixMilli()
	if err := v2Store.UpsertAccount(sqlite.Account{
		AccountID:   "slack-T1",
		BridgeKey:   "slack_web",
		DisplayName: "Acme",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := v2Store.UpsertConversation(sqlite.Conversation{
		ConversationID:       "hashed-slack-conv",
		AccountID:            "slack-T1",
		RemoteConversationID: "C99",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#general",
		NotificationMode:     sqlite.NotificationModeAll,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}

	live := []*db.Message{
		{
			MessageID:      "slack:C99:111.111",
			ConversationID: "slack:T1:C99",
			Body:           "root",
			SourcePlatform: "slack",
			SourceID:       "C99:111.111",
		},
		{
			MessageID:      "slack:C99:222.222",
			ConversationID: "slack:T1:C99",
			ReplyToID:      "slack:C99:111.111",
			Body:           "reply",
			SourcePlatform: "slack",
			SourceID:       "C99:222.222",
		},
	}
	primary := APIOptions{V2Primary: true, V2: &V2Options{V2Store: v2Store}}
	mapped := mapSlackLiveMessagesToV2(primary, "hashed-slack-conv", live)
	wantRoot := v2keys.MessageID("slack-T1", "C99", "111.111")
	wantReply := v2keys.MessageID("slack-T1", "C99", "222.222")
	if mapped[0].ConversationID != "hashed-slack-conv" || mapped[0].MessageID != wantRoot {
		t.Fatalf("root = %+v, want conv hashed-slack-conv id %s", mapped[0], wantRoot)
	}
	if mapped[1].ConversationID != "hashed-slack-conv" ||
		mapped[1].MessageID != wantReply ||
		mapped[1].ReplyToID != wantRoot {
		t.Fatalf("reply = %+v, want id %s reply %s", mapped[1], wantReply, wantRoot)
	}

	passthrough := mapSlackLiveMessagesToV2(APIOptions{}, "hashed-slack-conv", []*db.Message{{
		MessageID:      "slack:C99:111.111",
		ConversationID: "slack:T1:C99",
	}})
	if passthrough[0].MessageID != "slack:C99:111.111" || passthrough[0].ConversationID != "slack:T1:C99" {
		t.Fatalf("non-primary passthrough = %+v", passthrough[0])
	}
}

func TestMarkReadV2FailureDoesNotFailLegacyResponse(t *testing.T) {
	// A nonnil V2 option with no store simulates a local v2 write failure. The
	// legacy unread-count mutation remains authoritative for this response.
	ts := newV1RecorderHarness(t, APIOptions{V2: &V2Options{}})
	if err := ts.store.UpsertConversation(&db.Conversation{
		ConversationID: "google-thread-v2-failure",
		SourcePlatform: "sms",
		UnreadCount:    2,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/mark-read", bytes.NewBufferString(`{"conversation_id":"google-thread-v2-failure"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := ts.do(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, raw)
	}
	conversation, err := ts.store.GetConversation("google-thread-v2-failure")
	if err != nil {
		t.Fatal(err)
	}
	if conversation.UnreadCount != 0 {
		t.Fatalf("legacy unread count = %d, want 0", conversation.UnreadCount)
	}
}

type v1RecorderHarness struct {
	store   *db.Store
	handler http.Handler
}

func newV1RecorderHarness(t *testing.T, opts APIOptions) *v1RecorderHarness {
	t.Helper()
	store, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &v1RecorderHarness{
		store:   store,
		handler: APIHandlerWithOptions(store, nil, zerolog.Nop(), nil, opts),
	}
}

func (h *v1RecorderHarness) do(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder.Result()
}
