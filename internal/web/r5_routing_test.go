package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/media"
	"github.com/maxghenis/openmessage/internal/messaging"
	"github.com/maxghenis/openmessage/internal/readsource"
	"github.com/maxghenis/openmessage/internal/storage/blob"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2read"
)

type routingReadSource struct {
	readsource.ReadSource
	calls map[string]int
}

func newRoutingReadSource() *routingReadSource {
	return &routingReadSource{calls: make(map[string]int)}
}

func (s *routingReadSource) ListConversations(limit int) ([]*db.Conversation, error) {
	s.calls["list"]++
	return []*db.Conversation{{
		ConversationID: "v2-conversation",
		Name:           "V2 Alice",
		LastMessageTS:  200,
		SourcePlatform: "sms",
		Participants:   `[{"name":"V2 Alice","number":"+15550001111"}]`,
	}}, nil
}

func (s *routingReadSource) GetConversation(id string) (*db.Conversation, error) {
	s.calls["conversation"]++
	return &db.Conversation{
		ConversationID: id,
		Name:           "V2 Alice",
		LastMessageTS:  200,
		SourcePlatform: "sms",
	}, nil
}

func (s *routingReadSource) GetMessagesByConversation(conversationID string, limit int) ([]*db.Message, error) {
	s.calls["messages"]++
	return []*db.Message{routingMessage("latest from v2")}, nil
}

func (s *routingReadSource) GetMessagesByConversationBefore(conversationID string, beforeMS int64, beforeID string, limit int) ([]*db.Message, error) {
	s.calls["before"]++
	return []*db.Message{routingMessage("before from v2")}, nil
}

func (s *routingReadSource) GetMessagesByConversationAfter(conversationID string, afterMS int64, afterID string, limit int) ([]*db.Message, error) {
	s.calls["after"]++
	return []*db.Message{routingMessage("after from v2")}, nil
}

func (s *routingReadSource) GetMessagesAroundMessage(conversationID, messageID string, before, after int) ([]*db.Message, error) {
	s.calls["around"]++
	return []*db.Message{routingMessage("around from v2")}, nil
}

func (s *routingReadSource) SearchMessagesFiltered(query string, filter db.SearchFilter) ([]*db.Message, error) {
	s.calls["search"]++
	return []*db.Message{routingMessage("search from v2")}, nil
}

func (s *routingReadSource) PlatformStats() ([]db.PlatformStat, error) {
	s.calls["platform_stats"]++
	return []db.PlatformStat{{Platform: "sms", Count: 42, LatestMS: 200, LatestRecvMS: 100}}, nil
}

func (s *routingReadSource) MessageCount(sourcePlatform string) (int, error) {
	s.calls["message_count"]++
	if sourcePlatform == "" {
		return 42, nil
	}
	if sourcePlatform == "sms" {
		return 42, nil
	}
	return 0, nil
}

func (s *routingReadSource) ConversationCount(sourcePlatform string) (int, error) {
	s.calls["conversation_count"]++
	if sourcePlatform == "" || sourcePlatform == "sms" {
		return 3, nil
	}
	return 0, nil
}

func (s *routingReadSource) LatestTimestamp(sourcePlatform string) (int64, error) {
	s.calls["latest_timestamp"]++
	if sourcePlatform == "sms" {
		return 200, nil
	}
	return 0, nil
}

func (s *routingReadSource) LatestConversationPreviews(ids []string) (map[string]string, error) {
	s.calls["previews"]++
	return map[string]string{"v2-conversation": "preview from v2"}, nil
}

func (s *routingReadSource) GetMessagesByConversations(conversationIDs []string, limit int) ([]*db.Message, error) {
	s.calls["by_conversations"]++
	return []*db.Message{routingMessage("latest from v2")}, nil
}

func (s *routingReadSource) GetMessagesByConversationsRange(conversationIDs []string, afterMS, beforeMS int64, limit int) ([]*db.Message, error) {
	s.calls["by_conversations_range"]++
	return []*db.Message{routingMessage("latest from v2")}, nil
}

func (s *routingReadSource) GetMessageByID(messageID string) (*db.Message, error) {
	s.calls["message"]++
	msg := routingMessage("latest from v2")
	msg.MessageID = messageID
	msg.MediaID = "v2msg:" + messageID + ":1"
	msg.MimeType = "image/png"
	return msg, nil
}

func routingMessage(body string) *db.Message {
	return &db.Message{
		MessageID:      "v2-message",
		ConversationID: "v2-conversation",
		Body:           body,
		TimestampMS:    200,
		SourcePlatform: "sms",
	}
}

func TestR5CanonicalReadRoutesUseConfiguredSource(t *testing.T) {
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if err := legacy.UpsertConversation(&db.Conversation{
		ConversationID: "legacy-conversation",
		Name:           "Legacy Alice",
		LastMessageTS:  100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.UpsertMessage(&db.Message{
		MessageID:      "legacy-message",
		ConversationID: "legacy-conversation",
		Body:           "legacy body",
		TimestampMS:    100,
	}); err != nil {
		t.Fatal(err)
	}

	reads := newRoutingReadSource()
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     reads,
		V2Primary: true,
	})

	tests := []struct {
		name     string
		path     string
		wantBody string
		wantCall string
	}{
		{name: "conversations", path: "/api/conversations", wantBody: "V2 Alice", wantCall: "list"},
		{name: "messages", path: "/api/conversations/v2-conversation/messages", wantBody: "latest from v2", wantCall: "messages"},
		{name: "before", path: "/api/conversations/v2-conversation/messages?before=200&before_id=v2-message", wantBody: "before from v2", wantCall: "before"},
		{name: "after", path: "/api/conversations/v2-conversation/messages?after=200&after_id=v2-message", wantBody: "after from v2", wantCall: "after"},
		{name: "around", path: "/api/conversations/v2-conversation/messages-around?message_id=v2-message", wantBody: "around from v2", wantCall: "around"},
		{name: "thread search", path: "/api/conversations/v2-conversation/search?q=search", wantBody: "search from v2", wantCall: "search"},
		{name: "global search", path: "/api/search?q=search", wantBody: "search from v2", wantCall: "search"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := reads.calls[test.wantCall]
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+test.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Fatalf("body = %s, want content %q", recorder.Body.String(), test.wantBody)
			}
			if strings.Contains(recorder.Body.String(), "legacy") {
				t.Fatalf("body leaked legacy data: %s", recorder.Body.String())
			}
			if reads.calls[test.wantCall] <= before {
				t.Fatalf("configured read source method %q was not called", test.wantCall)
			}
		})
	}

	if reads.calls["previews"] == 0 {
		t.Fatal("conversation previews did not use configured read source")
	}
}

func TestR5ConversationPlatformFilterDoesNotReadLegacyInV2Primary(t *testing.T) {
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if err := legacy.UpsertConversation(&db.Conversation{
		ConversationID: "legacy-slack",
		Name:           "Legacy Slack",
		SourcePlatform: "slack",
		LastMessageTS:  100,
	}); err != nil {
		t.Fatal(err)
	}

	reads := newRoutingReadSource()
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     reads,
		V2Primary: true,
	})

	sms := httptest.NewRecorder()
	handler.ServeHTTP(sms, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/conversations?source_platform=sms", nil))
	if sms.Code != http.StatusOK {
		t.Fatalf("sms status = %d, body=%s", sms.Code, sms.Body.String())
	}
	if !strings.Contains(sms.Body.String(), "V2 Alice") {
		t.Fatalf("sms body = %s, want v2 conversation", sms.Body.String())
	}
	if strings.Contains(strings.ToLower(sms.Body.String()), "legacy") {
		t.Fatalf("sms filter leaked legacy data: %s", sms.Body.String())
	}
	if reads.calls["list"] == 0 {
		t.Fatal("sms platform filter did not use configured read source")
	}

	slack := httptest.NewRecorder()
	handler.ServeHTTP(slack, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/conversations?source_platform=slack", nil))
	if slack.Code != http.StatusOK {
		t.Fatalf("slack status = %d, body=%s", slack.Code, slack.Body.String())
	}
	if strings.Contains(slack.Body.String(), "Legacy Slack") {
		t.Fatal("platform filter leaked legacy slack conversations")
	}
	if strings.Contains(slack.Body.String(), "V2 Alice") {
		t.Fatal("sms conversation included in slack filter")
	}
}

func TestR5SearchIncludesConversationNameHitsInV2Primary(t *testing.T) {
	v2, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2.Close() })
	nowMS := time.Now().UnixMilli()
	if err := v2.UpsertAccount(sqlite.Account{
		AccountID:   "google-primary",
		BridgeKey:   "google_messages",
		DisplayName: "Google",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := v2.UpsertConversation(sqlite.Conversation{
		ConversationID:       "v2-nathan",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-nathan",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Nathan",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      nowMS,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}

	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if err := legacy.UpsertConversation(&db.Conversation{
		ConversationID: "legacy-nathan",
		Name:           "Nathan Legacy",
		LastMessageTS:  100,
	}); err != nil {
		t.Fatal(err)
	}

	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     v2read.New(v2),
		V2Primary: true,
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/search?q=Nathan", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "v2-nathan") {
		t.Fatalf("body = %s, want v2 conversation name hit", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "legacy-nathan") {
		t.Fatalf("search leaked legacy conversation: %s", recorder.Body.String())
	}
}

func TestR5FavoriteAndMutePersistInV2Primary(t *testing.T) {
	v2, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2.Close() })
	nowMS := time.Now().UnixMilli()
	if err := v2.UpsertAccount(sqlite.Account{
		AccountID:   "google-primary",
		BridgeKey:   "google_messages",
		DisplayName: "Google",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := v2.UpsertConversation(sqlite.Conversation{
		ConversationID:       "v2-flags",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-v2-flags",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Flags",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      nowMS,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if err := legacy.UpsertConversation(&db.Conversation{
		ConversationID: "v2-flags",
		Name:           "Legacy Flags",
		LastMessageTS:  100,
	}); err != nil {
		t.Fatal(err)
	}

	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     v2read.New(v2),
		V2Primary: true,
		V2:        &V2Options{V2Store: v2},
	})

	fav := httptest.NewRecorder()
	handler.ServeHTTP(fav, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/conversations/v2-flags/favorite", strings.NewReader(`{"favorite":true}`)))
	if fav.Code != http.StatusOK {
		t.Fatalf("favorite status = %d, body=%s", fav.Code, fav.Body.String())
	}
	if !strings.Contains(fav.Body.String(), `"is_favorite":true`) {
		t.Fatalf("favorite body = %s", fav.Body.String())
	}
	got, err := v2.GetConversation("v2-flags")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsFavorite {
		t.Fatal("v2 conversation favorite was not persisted")
	}
	legacyConv, _ := legacy.GetConversation("v2-flags")
	if legacyConv != nil && legacyConv.IsFavorite {
		t.Fatal("favorite leaked into the legacy store")
	}

	mute := httptest.NewRecorder()
	handler.ServeHTTP(mute, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/conversations/v2-flags/notification-mode", strings.NewReader(`{"notification_mode":"muted"}`)))
	if mute.Code != http.StatusOK {
		t.Fatalf("mute status = %d, body=%s", mute.Code, mute.Body.String())
	}
	got, err = v2.GetConversation("v2-flags")
	if err != nil {
		t.Fatal(err)
	}
	if got.NotificationMode != sqlite.NotificationModeMuted {
		t.Fatalf("notification mode = %q, want muted", got.NotificationMode)
	}
}

func TestR5LegacySendEndpointsQuiescedInV2Primary(t *testing.T) {
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{V2Primary: true})

	for _, path := range []string{"/api/send", "/api/send-media", "/api/send-gif"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+path, strings.NewReader("{}")))
			response := recorder.Result()
			defer response.Body.Close()
			if response.StatusCode != http.StatusConflict {
				raw, _ := io.ReadAll(response.Body)
				t.Fatalf("status = %d, want 409; body=%s", response.StatusCode, raw)
			}
			var payload map[string]string
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["error"] != "v2 primary: use /api/v1/outbox" {
				t.Fatalf("error = %q", payload["error"])
			}
		})
	}
}

func TestR5V1SubmitUsesNativeV2ConversationIDInV2Primary(t *testing.T) {
	harness := newA3Harness(t, false)
	nowMS := time.Now().UnixMilli()
	if err := harness.v2.UpsertAccount(sqlite.Account{
		AccountID:   "google-primary",
		BridgeKey:   "google_messages",
		DisplayName: "Google",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	const conversationID = "v2-native-conversation"
	if err := harness.v2.UpsertConversation(sqlite.Conversation{
		ConversationID:       conversationID,
		AccountID:            "google-primary",
		RemoteConversationID: "remote-v2-native-conversation",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Native v2 thread",
		NotificationMode:     sqlite.NotificationModeAll,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}

	handler := APIHandlerWithOptions(harness.legacy, nil, zerolog.Nop(), nil, APIOptions{
		V2Primary: true,
		V2: &V2Options{
			Service:  harness.service,
			V2Store:  harness.v2,
			Registry: harness.registry,
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1/api/v1/outbox/messages",
		strings.NewReader(`{"conversation_id":"v2-native-conversation","body":"native","idempotency_key":"native-route-key"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if legacyConversation, _ := harness.legacy.GetConversation(conversationID); legacyConversation != nil {
		t.Fatalf("native v2 id unexpectedly created a legacy mirror row: conversation=%+v", legacyConversation)
	}
}

func TestR5CanonicalMediaRouteServesV2PrimaryAttachment(t *testing.T) {
	ctx := context.Background()
	v2, err := sqlite.Open(filepath.Join(t.TempDir(), "v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v2.Close() })
	blobs, err := blob.New(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("v2 canonical attachment")
	ref, err := blobs.Put(ctx, bytes.NewReader(content), "text/plain", int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	nowMS := time.Now().UnixMilli()
	if err := v2.UpsertAccount(sqlite.Account{
		AccountID:   "google-primary",
		BridgeKey:   "google_messages",
		DisplayName: "Google",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	if err := v2.UpsertConversation(sqlite.Conversation{
		ConversationID:       "v2-media-conversation",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-v2-media-conversation",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "V2 media",
		NotificationMode:     sqlite.NotificationModeAll,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := sqlite.NewMessageRepository(v2, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	const messageID = "v2-media-message"
	if err := messages.ImportMessage(ctx, sqlite.MessageProjection{
		Message: sqlite.Message{
			MessageID:       messageID,
			ConversationID:  "v2-media-conversation",
			AccountID:       "google-primary",
			RemoteMessageID: "remote-v2-media-message",
			Direction:       sqlite.MessageDirectionIncoming,
			State:           sqlite.MessageStateActive,
			OccurredAtMS:    nowMS,
		},
		Attachments: []sqlite.MessageAttachment{{
			Ordinal:  0,
			RemoteID: "remote-attachment",
			Filename: "attachment.txt",
			MIME:     "text/plain",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	attachments, err := sqlite.NewMessageAttachmentRepository(v2, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := attachments.MarkDownloaded(ctx, messageID, 0, ref.Hash, int64(len(content)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	mediaService, err := media.NewService(v2, &a3Registry{accountID: "google-primary"}, blobs, messaging.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		V2Primary: true,
		V2: &V2Options{
			V2Store: v2,
			Media:   mediaService,
		},
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/media/"+messageID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Equal(recorder.Body.Bytes(), content) {
		t.Fatalf("body = %q, want %q", recorder.Body.Bytes(), content)
	}
}

func TestR5StatusCountsRouteToConfiguredSourceInV2Primary(t *testing.T) {
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	// Legacy has one throwaway conversation with a different platform, so if the
	// handler read legacy the counts would not match the spy's v2 figures.
	reads := newRoutingReadSource()
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     reads,
		V2Primary: true,
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/diagnostics", nil))
	response := recorder.Result()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var payload struct {
		MessageCount      int            `json:"message_count"`
		ConversationCount int            `json:"conversation_count"`
		MessageCounts     map[string]int `json:"message_counts"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.MessageCount != 42 || payload.ConversationCount != 3 {
		t.Fatalf("counts = (msg %d, conv %d), want (42, 3) from the v2 source", payload.MessageCount, payload.ConversationCount)
	}
	if payload.MessageCounts["sms"] != 42 {
		t.Fatalf("message_counts[sms] = %d, want 42 from the v2 source", payload.MessageCounts["sms"])
	}
	if reads.calls["message_count"] == 0 || reads.calls["conversation_count"] == 0 {
		t.Fatalf("status did not consult the configured source: calls=%v", reads.calls)
	}
}

func TestR5StatsAndStoryUseConfiguredSourceInV2Primary(t *testing.T) {
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	reads := newRoutingReadSource()
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     reads,
		V2Primary: true,
	})

	statsRec := httptest.NewRecorder()
	handler.ServeHTTP(statsRec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/stats/v2-conversation", nil))
	if got := statsRec.Result().StatusCode; got != http.StatusOK {
		t.Fatalf("/api/stats status = %d, want 200", got)
	}
	if reads.calls["messages"] == 0 {
		t.Fatalf("stats did not consult configured source: calls=%v", reads.calls)
	}

	storyRec := httptest.NewRecorder()
	handler.ServeHTTP(storyRec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/story/v2-conversation", nil))
	if got := storyRec.Result().StatusCode; got != http.StatusOK {
		t.Fatalf("/api/story status = %d body=%s, want 200", got, storyRec.Body.String())
	}

	contactsRec := httptest.NewRecorder()
	handler.ServeHTTP(contactsRec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/contacts", nil))
	if got := contactsRec.Result().StatusCode; got != http.StatusOK {
		t.Fatalf("/api/contacts status = %d, want 200", got)
	}
	var contacts []*db.Contact
	if err := json.NewDecoder(contactsRec.Result().Body).Decode(&contacts); err != nil {
		t.Fatalf("decode contacts: %v", err)
	}
	if len(contacts) != 1 || contacts[0].Name != "V2 Alice" {
		t.Fatalf("contacts = %+v, want V2 Alice from v2 source", contacts)
	}

	peopleRec := httptest.NewRecorder()
	handler.ServeHTTP(peopleRec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/people", nil))
	if got := peopleRec.Result().StatusCode; got != http.StatusOK {
		t.Fatalf("/api/people status = %d, want 200", got)
	}
	var people []map[string]any
	if err := json.NewDecoder(peopleRec.Result().Body).Decode(&people); err != nil {
		t.Fatalf("decode people: %v", err)
	}
	if len(people) != 1 || people[0]["name"] != "V2 Alice" {
		t.Fatalf("people = %#v, want V2 Alice from v2 source", people)
	}

	syncRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/contacts/sync", nil)
	handler.ServeHTTP(syncRec, req)
	if got := syncRec.Result().StatusCode; got != http.StatusConflict {
		t.Fatalf("/api/contacts/sync status = %d, want 409", got)
	}

	draftSend := httptest.NewRecorder()
	sendReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/drafts/send", strings.NewReader(`{"draft_id":"d1","body":"x"}`))
	sendReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(draftSend, sendReq)
	if got := draftSend.Result().StatusCode; got != http.StatusServiceUnavailable {
		t.Fatalf("/api/drafts/send status = %d, want 503 without a v2 outbox", got)
	}
}

func TestPRAScheduleAndStatusInV2Primary(t *testing.T) {
	legacy, err := db.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	handler := APIHandlerWithOptions(legacy, nil, zerolog.Nop(), nil, APIOptions{
		Reads:     newRoutingReadSource(),
		V2Primary: true,
	})

	// /api/status exposes the flip signal the composer keys on.
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/status", nil))
	var status map[string]any
	if err := json.NewDecoder(statusRec.Result().Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status["v2_primary"] != true {
		t.Fatalf("status v2_primary = %#v, want true", status["v2_primary"])
	}

	// Schedule writes stay 409 in v2-primary. New-conversation, tabs, and
	// drafts now live on v2 when a store is configured.
	for _, tc := range []struct {
		method, path, body, ctype string
	}{
		{http.MethodPost, "http://127.0.0.1/api/schedule", `{"conversation_id":"c","body":"b","send_at":9999999999999}`, "application/json"},
		{http.MethodDelete, "http://127.0.0.1/api/schedule/some-id", "", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.ctype != "" {
			req.Header.Set("Content-Type", tc.ctype)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Result().StatusCode; got != http.StatusConflict {
			t.Fatalf("%s %s = %d, want 409 in v2-primary", tc.method, tc.path, got)
		}
	}
	// No legacy scheduled row was written by the refused POST.
	if list, err := legacy.ListScheduledMessages("c"); err != nil {
		t.Fatalf("ListScheduledMessages: %v", err)
	} else if len(list) != 0 {
		t.Fatalf("legacy scheduled rows = %d, want 0 (nothing persisted in v2-primary)", len(list))
	}

	// GET returns empty (not an error) so the pre-tray UI shows nothing.
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/schedule?conversation_id=c", nil))
	if got := getRec.Result().StatusCode; got != http.StatusOK {
		t.Fatalf("GET /api/schedule = %d, want 200 (empty list) in v2-primary", got)
	}
}
