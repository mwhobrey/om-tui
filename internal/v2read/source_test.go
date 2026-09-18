package v2read

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/readsource"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2keys"
)

const sourceTestTimeMS int64 = 2_000_000_000_000

var _ readsource.ReadSource = (*Source)(nil)

func TestSourceMapsConversationAndMessagesToLegacyDTOs(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-account", "google_messages")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-google",
		AccountID:            "google-account",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "Byte-exact group",
		NotificationMode:     sqlite.NotificationModeMuted,
		IsFavorite:           true,
		LastMessageAtMS:      300,
	})
	identity := sqlite.Identity{
		IdentityID:     "identity-alice",
		AccountID:      "google-account",
		Kind:           sqlite.IdentityKind("e164"),
		CanonicalValue: "+15550000001",
		RawValue:       "+1 (555) 000-0001",
		DisplayName:    "Alice",
		MetadataJSON:   `{}`,
		CreatedAtMS:    sourceTestTimeMS,
		UpdatedAtMS:    sourceTestTimeMS,
	}
	if err := store.UpsertIdentity(identity); err != nil {
		t.Fatalf("UpsertIdentity(): %v", err)
	}
	if err := store.ReplaceConversationParticipants(
		"conversation-google",
		[]sqlite.ConversationParticipant{{
			AccountID:      "google-account",
			ConversationID: "conversation-google",
			IdentityID:     identity.IdentityID,
			Role:           sqlite.ParticipantRoleMember,
			DisplayName:    "Alice in group",
			IsActive:       true,
		}},
	); err != nil {
		t.Fatalf("ReplaceConversationParticipants(): %v", err)
	}

	replyRemoteID := "remote-parent"
	incoming := sqlite.Message{
		MessageID:        "message-incoming",
		ConversationID:   "conversation-google",
		AccountID:        "google-account",
		RemoteMessageID:  "remote-incoming",
		SenderIdentityID: stringPointer(identity.IdentityID),
		Direction:        sqlite.MessageDirectionIncoming,
		Body:             "body\x00kept byte-for-byte",
		ReplyToRemoteID:  &replyRemoteID,
		State:            sqlite.MessageStateEdited,
		OccurredAtMS:     200,
	}
	size := int64(123)
	importSourceMessage(t, messages, incoming, sqlite.MessageAttachment{
		Ordinal:   0,
		RemoteID:  "remote-media",
		RemoteRef: []byte("opaque"),
		Filename:  "photo.png",
		MIME:      "image/png",
		SizeBytes: &size,
	})
	outgoing := sqlite.Message{
		MessageID:       "message-outgoing",
		ConversationID:  "conversation-google",
		AccountID:       "google-account",
		RemoteMessageID: "remote-outgoing",
		Direction:       sqlite.MessageDirectionOutgoing,
		Body:            "sent by me",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    300,
	}
	importSourceMessage(t, messages, outgoing)
	reactions, err := sqlite.NewReactionRepository(store, func() time.Time { return time.UnixMilli(sourceTestTimeMS) })
	if err != nil {
		t.Fatalf("NewReactionRepository(): %v", err)
	}
	for _, reaction := range []sqlite.ReactionApply{
		{
			AccountID: "google-account", ConversationID: "conversation-google", MessageID: incoming.MessageID,
			ReactorKey: identity.IdentityID, ReactorIdentityID: stringPointer(identity.IdentityID),
			ReactorLabel: identity.RawValue, Emoji: "👍", Action: bridge.ReactionAdd, OccurredAtMS: 201,
		},
		{
			AccountID: "google-account", ConversationID: "conversation-google", MessageID: incoming.MessageID,
			ReactorKey: "self", ReactorIsSelf: true, ReactorLabel: "me",
			Emoji: "❤️", Action: bridge.ReactionAdd, OccurredAtMS: 202,
		},
		{
			AccountID: "google-account", ConversationID: "conversation-google", MessageID: incoming.MessageID,
			ReactorKey: "removed", ReactorLabel: "gone", Emoji: "😂", Action: bridge.ReactionAdd, OccurredAtMS: 203,
		},
		{
			AccountID: "google-account", ConversationID: "conversation-google", MessageID: incoming.MessageID,
			ReactorKey: "removed", ReactorLabel: "gone", Action: bridge.ReactionRemove, OccurredAtMS: 204,
		},
	} {
		if _, err := reactions.ApplyReaction(context.Background(), reaction); err != nil {
			t.Fatalf("ApplyReaction(%q): %v", reaction.ReactorKey, err)
		}
	}

	conversation, err := source.GetConversation("conversation-google")
	if err != nil {
		t.Fatalf("GetConversation(): %v", err)
	}
	if conversation.ConversationID != "conversation-google" ||
		conversation.Name != "Byte-exact group" ||
		!conversation.IsGroup ||
		conversation.LastMessageTS != 300 ||
		conversation.SourcePlatform != "sms" ||
		!conversation.IsFavorite ||
		conversation.NotificationMode != "muted" ||
		conversation.UnreadCount != 0 ||
		conversation.Tab != "" ||
		conversation.RiverID != "messages-default" {
		t.Fatalf("mapped conversation = %+v", conversation)
	}
	if _, err := source.GetConversation("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetConversation(missing) error = %v, want sql.ErrNoRows", err)
	}
	var participants []struct {
		Name   string `json:"name"`
		Number string `json:"number"`
	}
	if err := json.Unmarshal([]byte(conversation.Participants), &participants); err != nil {
		t.Fatalf("decode Participants %q: %v", conversation.Participants, err)
	}
	if got, want := participants, []struct {
		Name   string `json:"name"`
		Number string `json:"number"`
	}{{Name: "Alice in group", Number: "+15550000001"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("participants = %+v, want %+v", got, want)
	}

	gotMessages, err := source.GetMessagesByConversation("conversation-google", 10)
	if err != nil {
		t.Fatalf("GetMessagesByConversation(): %v", err)
	}
	if len(gotMessages) != 2 {
		t.Fatalf("mapped messages = %d, want 2: %+v", len(gotMessages), gotMessages)
	}
	gotOutgoing := gotMessages[0]
	if gotOutgoing.MessageID != outgoing.MessageID ||
		!gotOutgoing.IsFromMe ||
		gotOutgoing.SenderName != "" ||
		gotOutgoing.SenderNumber != "" ||
		gotOutgoing.SourcePlatform != "sms" ||
		gotOutgoing.SourceID != outgoing.RemoteMessageID {
		t.Fatalf("mapped outgoing = %+v", gotOutgoing)
	}
	gotIncoming := gotMessages[1]
	if gotIncoming.MessageID != incoming.MessageID ||
		gotIncoming.ConversationID != incoming.ConversationID ||
		gotIncoming.Body != incoming.Body ||
		gotIncoming.TimestampMS != incoming.OccurredAtMS ||
		gotIncoming.IsFromMe ||
		gotIncoming.SenderName != identity.DisplayName ||
		gotIncoming.SenderNumber != identity.CanonicalValue ||
		gotIncoming.SourcePlatform != "sms" ||
		gotIncoming.SourceID != incoming.RemoteMessageID ||
		gotIncoming.ReplyToID != v2keys.MessageID(incoming.AccountID, "remote-google", replyRemoteID) ||
		gotIncoming.MediaID != "v2msg:message-incoming:0" ||
		gotIncoming.MimeType != "image/png" ||
		gotIncoming.Status != "" ||
		gotIncoming.Reactions != `[{"emoji":"👍","count":1,"actors":["+15550000001"]},{"emoji":"❤️","count":1,"actors":["me"]}]` {
		t.Fatalf("mapped incoming = %+v", gotIncoming)
	}
}

func TestSourceCrossAccountRecencyPaginationAndLIKEFilters(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-account", "google_messages")
	seedSourceAccount(t, store, "whatsapp-account", "whatsmeow")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-google",
		AccountID:            "google-account",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Google",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      500,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-whatsapp",
		AccountID:            "whatsapp-account",
		RemoteConversationID: "remote-whatsapp",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "WhatsApp",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      500,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-old",
		AccountID:            "google-account",
		RemoteConversationID: "remote-old",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Old",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      100,
	})
	for _, identity := range []sqlite.Identity{
		{
			IdentityID:     "alice-google",
			AccountID:      "google-account",
			Kind:           sqlite.IdentityKind("e164"),
			CanonicalValue: "+15550000001",
			RawValue:       "+15550000001",
			DisplayName:    "Alice",
			MetadataJSON:   `{}`,
			CreatedAtMS:    sourceTestTimeMS,
			UpdatedAtMS:    sourceTestTimeMS,
		},
		{
			IdentityID:     "bob-google",
			AccountID:      "google-account",
			Kind:           sqlite.IdentityKind("e164"),
			CanonicalValue: "+15550000002",
			RawValue:       "+15550000002",
			DisplayName:    "Bob",
			MetadataJSON:   `{}`,
			CreatedAtMS:    sourceTestTimeMS,
			UpdatedAtMS:    sourceTestTimeMS,
		},
	} {
		if err := store.UpsertIdentity(identity); err != nil {
			t.Fatalf("UpsertIdentity(%q): %v", identity.IdentityID, err)
		}
	}

	for _, item := range []struct {
		id       string
		ts       int64
		body     string
		senderID string
	}{
		{id: "message-old", ts: 100, body: "Needle oldest", senderID: "alice-google"},
		{id: "message-tie-a", ts: 200, body: "needle from Bob", senderID: "bob-google"},
		{id: "message-tie-b", ts: 200, body: "NEEDLE from Alice", senderID: "alice-google"},
		{id: "message-new", ts: 300, body: "needle newest", senderID: "alice-google"},
	} {
		importSourceMessage(t, messages, sqlite.Message{
			MessageID:        item.id,
			ConversationID:   "conversation-google",
			AccountID:        "google-account",
			RemoteMessageID:  "remote-" + item.id,
			SenderIdentityID: stringPointer(item.senderID),
			Direction:        sqlite.MessageDirectionIncoming,
			Body:             item.body,
			State:            sqlite.MessageStateActive,
			OccurredAtMS:     item.ts,
		})
	}

	conversations, err := source.ListConversations(3)
	if err != nil {
		t.Fatalf("ListConversations(): %v", err)
	}
	assertConversationIDs(
		t,
		conversations,
		"conversation-google",
		"conversation-whatsapp",
		"conversation-old",
	)

	latest, err := source.GetMessagesByConversation("conversation-google", 3)
	if err != nil {
		t.Fatalf("GetMessagesByConversation(): %v", err)
	}
	assertMessageIDs(t, latest, "message-new", "message-tie-b", "message-tie-a")
	before, err := source.GetMessagesByConversationBefore(
		"conversation-google", 200, "message-tie-b", 10,
	)
	if err != nil {
		t.Fatalf("GetMessagesByConversationBefore(): %v", err)
	}
	assertMessageIDs(t, before, "message-tie-a", "message-old")
	after, err := source.GetMessagesByConversationAfter(
		"conversation-google", 200, "message-tie-a", 10,
	)
	if err != nil {
		t.Fatalf("GetMessagesByConversationAfter(): %v", err)
	}
	assertMessageIDs(t, after, "message-tie-b", "message-new")
	around, err := source.GetMessagesAroundMessage(
		"conversation-google", "message-tie-b", 2, 1,
	)
	if err != nil {
		t.Fatalf("GetMessagesAroundMessage(): %v", err)
	}
	assertMessageIDs(t, around, "message-old", "message-tie-a", "message-tie-b", "message-new")
	if _, err := source.GetMessagesAroundMessage(
		"conversation-whatsapp", "message-tie-b", 1, 1,
	); !errors.Is(err, db.ErrMessageNotFound) {
		t.Fatalf("wrong-conversation around error = %v, want db.ErrMessageNotFound", err)
	}
	got, err := source.GetMessageByID("message-new")
	if err != nil {
		t.Fatalf("GetMessageByID(): %v", err)
	}
	if got == nil || got.MessageID != "message-new" || got.Body != "needle newest" {
		t.Fatalf("GetMessageByID() = %+v", got)
	}
	missing, err := source.GetMessageByID("message-does-not-exist")
	if err != nil {
		t.Fatalf("GetMessageByID(missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("GetMessageByID(missing) = %+v, want nil", missing)
	}

	searchResults, err := source.SearchMessagesFiltered("needle", db.SearchFilter{
		Phone:          "+15550000001",
		ConversationID: "conversation-google",
		SinceMS:        150,
		UntilMS:        350,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("SearchMessagesFiltered(): %v", err)
	}
	assertMessageIDs(t, searchResults, "message-new", "message-tie-b")
}

func TestSourceGetMessagesByConversationsMergesOldestFirst(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-account", "google_messages")
	seedSourceAccount(t, store, "whatsapp-account", "whatsmeow")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-google",
		AccountID:            "google-account",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Alice SMS",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      400,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-whatsapp",
		AccountID:            "whatsapp-account",
		RemoteConversationID: "remote-whatsapp",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Alice WA",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      200,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "google-old",
		ConversationID:  "conversation-google",
		AccountID:       "google-account",
		RemoteMessageID: "remote-google-old",
		Direction:       sqlite.MessageDirectionIncoming,
		Body:            "sms old",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    100,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "whatsapp-mid",
		ConversationID:  "conversation-whatsapp",
		AccountID:       "whatsapp-account",
		RemoteMessageID: "remote-whatsapp-mid",
		Direction:       sqlite.MessageDirectionIncoming,
		Body:            "wa mid",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    200,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "google-new",
		ConversationID:  "conversation-google",
		AccountID:       "google-account",
		RemoteMessageID: "remote-google-new",
		Direction:       sqlite.MessageDirectionOutgoing,
		Body:            "sms new",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    400,
	})

	got, err := source.GetMessagesByConversations(
		[]string{"conversation-google", "conversation-whatsapp"},
		2,
	)
	if err != nil {
		t.Fatalf("GetMessagesByConversations(): %v", err)
	}
	assertMessageIDs(t, got, "whatsapp-mid", "google-new")

	ranged, err := source.GetMessagesByConversationsRange(
		[]string{"conversation-google", "conversation-whatsapp"},
		150, 250, 10,
	)
	if err != nil {
		t.Fatalf("GetMessagesByConversationsRange(): %v", err)
	}
	assertMessageIDs(t, ranged, "whatsapp-mid")
}

func TestSourceStatsCountsLatestAndPreviewsPageAllMessages(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-account", "google_messages")
	seedSourceAccount(t, store, "whatsapp-account", "whatsmeow")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-google",
		AccountID:            "google-account",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Google",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      1_000,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-google-empty",
		AccountID:            "google-account",
		RemoteConversationID: "remote-google-empty",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Empty",
		NotificationMode:     sqlite.NotificationModeAll,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-whatsapp",
		AccountID:            "whatsapp-account",
		RemoteConversationID: "remote-whatsapp",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "WhatsApp",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      2_000,
	})

	for index := 0; index < sourceMessagePageSize+1; index++ {
		direction := sqlite.MessageDirectionIncoming
		if index == sourceMessagePageSize {
			direction = sqlite.MessageDirectionOutgoing
		}
		body := fmt.Sprintf("message %03d", index)
		if index == sourceMessagePageSize {
			body = "  Latest\n  outgoing   preview  "
		}
		importSourceMessage(t, messages, sqlite.Message{
			MessageID:       fmt.Sprintf("google-message-%03d", index),
			ConversationID:  "conversation-google",
			AccountID:       "google-account",
			RemoteMessageID: fmt.Sprintf("remote-google-%03d", index),
			Direction:       direction,
			Body:            body,
			State:           sqlite.MessageStateActive,
			OccurredAtMS:    int64(index + 1),
		})
	}
	whatsapp := sqlite.Message{
		MessageID:       "whatsapp-message",
		ConversationID:  "conversation-whatsapp",
		AccountID:       "whatsapp-account",
		RemoteMessageID: "remote-whatsapp-message",
		Direction:       sqlite.MessageDirectionIncoming,
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    2_000,
	}
	importSourceMessage(t, messages, whatsapp, sqlite.MessageAttachment{
		Ordinal:   0,
		RemoteID:  "remote-video",
		RemoteRef: []byte("video-ref"),
		Filename:  "clip.mp4",
		MIME:      "video/mp4",
	})

	wantSMSMessages := sourceMessagePageSize + 1
	for _, test := range []struct {
		platform string
		want     int
	}{
		{platform: "", want: wantSMSMessages + 1},
		{platform: "sms", want: wantSMSMessages},
		{platform: "whatsapp", want: 1},
		{platform: "signal", want: 0},
	} {
		got, err := source.MessageCount(test.platform)
		if err != nil {
			t.Fatalf("MessageCount(%q): %v", test.platform, err)
		}
		if got != test.want {
			t.Fatalf("MessageCount(%q) = %d, want %d", test.platform, got, test.want)
		}
	}
	for _, test := range []struct {
		platform string
		want     int
	}{
		{platform: "", want: 3},
		{platform: "sms", want: 2},
		{platform: "whatsapp", want: 1},
		{platform: "signal", want: 0},
	} {
		got, err := source.ConversationCount(test.platform)
		if err != nil {
			t.Fatalf("ConversationCount(%q): %v", test.platform, err)
		}
		if got != test.want {
			t.Fatalf("ConversationCount(%q) = %d, want %d", test.platform, got, test.want)
		}
	}
	latestSMS, err := source.LatestTimestamp("sms")
	if err != nil {
		t.Fatalf("LatestTimestamp(sms): %v", err)
	}
	if latestSMS != int64(sourceMessagePageSize+1) {
		t.Fatalf("LatestTimestamp(sms) = %d, want %d", latestSMS, sourceMessagePageSize+1)
	}
	latestMissing, err := source.LatestTimestamp("signal")
	if err != nil {
		t.Fatalf("LatestTimestamp(signal): %v", err)
	}
	if latestMissing != 0 {
		t.Fatalf("LatestTimestamp(signal) = %d, want 0", latestMissing)
	}

	stats, err := source.PlatformStats()
	if err != nil {
		t.Fatalf("PlatformStats(): %v", err)
	}
	wantStats := []db.PlatformStat{
		{Platform: "whatsapp", Count: 1, LatestMS: 2_000, LatestRecvMS: 2_000},
		{
			Platform:     "sms",
			Count:        wantSMSMessages,
			LatestMS:     int64(sourceMessagePageSize + 1),
			LatestRecvMS: int64(sourceMessagePageSize),
		},
	}
	if !reflect.DeepEqual(stats, wantStats) {
		t.Fatalf("PlatformStats() = %+v, want %+v", stats, wantStats)
	}

	previews, err := source.LatestConversationPreviews([]string{
		" conversation-google ",
		"conversation-whatsapp",
		"conversation-google-empty",
		"conversation-google",
		"",
	})
	if err != nil {
		t.Fatalf("LatestConversationPreviews(): %v", err)
	}
	wantPreviews := map[string]string{
		"conversation-google":   "You: Latest outgoing preview",
		"conversation-whatsapp": "Video",
	}
	if !reflect.DeepEqual(previews, wantPreviews) {
		t.Fatalf("LatestConversationPreviews() = %#v, want %#v", previews, wantPreviews)
	}
}

func TestPlatformForBridgeKey(t *testing.T) {
	for _, test := range []struct {
		bridgeKey string
		want      string
	}{
		{bridgeKey: "google_messages", want: "sms"},
		{bridgeKey: "whatsmeow", want: "whatsapp"},
		{bridgeKey: "signal_cli", want: "signal"},
		{bridgeKey: "slack_web", want: "slack"},
		{bridgeKey: "gchat", want: "gchat"},
		{bridgeKey: "imessage", want: "imessage"},
		{bridgeKey: "custom_bridge", want: "custom_bridge"},
	} {
		t.Run(test.bridgeKey, func(t *testing.T) {
			if got := platformForBridgeKey(test.bridgeKey); got != test.want {
				t.Fatalf("platformForBridgeKey(%q) = %q, want %q", test.bridgeKey, got, test.want)
			}
		})
	}
}

func TestListConversationsByRiverUsesAccountMapping(t *testing.T) {
	store, _, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-primary", "google_messages")
	seedSourceAccount(t, store, "slack-T1", "slack_web")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "google-thread",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "SMS",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      200,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "slack-thread",
		AccountID:            "slack-T1",
		RemoteConversationID: "C1",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#general",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      300,
	})

	slack, err := source.ListConversationsByRiver("slack-T1", 10)
	if err != nil {
		t.Fatalf("ListConversationsByRiver(slack): %v", err)
	}
	if len(slack) != 1 || slack[0].ConversationID != "slack-thread" || slack[0].RiverID != "slack-T1" {
		t.Fatalf("slack river = %+v", slack)
	}

	google, err := source.ListConversationsByRiver("messages-default", 10)
	if err != nil {
		t.Fatalf("ListConversationsByRiver(messages-default): %v", err)
	}
	if len(google) != 1 || google[0].ConversationID != "google-thread" || google[0].RiverID != "messages-default" {
		t.Fatalf("messages river = %+v", google)
	}

	empty, err := source.ListConversationsByRiver("slack-missing", 10)
	if err != nil {
		t.Fatalf("ListConversationsByRiver(missing): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("missing river = %+v", empty)
	}
}

func TestListConversationsByPlatformMergesMatchingAccounts(t *testing.T) {
	store, _, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-primary", "google_messages")
	seedSourceAccount(t, store, "slack-T1", "slack_web")
	seedSourceAccount(t, store, "slack-T2", "slack_web")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "google-thread",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "SMS",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      400,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "slack-older",
		AccountID:            "slack-T1",
		RemoteConversationID: "C1",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#general",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      200,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "slack-newer",
		AccountID:            "slack-T2",
		RemoteConversationID: "C2",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#eng",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      300,
	})

	slack, err := source.ListConversationsByPlatform("slack", 10)
	if err != nil {
		t.Fatalf("ListConversationsByPlatform(slack): %v", err)
	}
	if len(slack) != 2 || slack[0].ConversationID != "slack-newer" || slack[1].ConversationID != "slack-older" {
		t.Fatalf("slack platform = %+v", slack)
	}

	limited, err := source.ListConversationsByPlatform("SLACK", 1)
	if err != nil {
		t.Fatalf("ListConversationsByPlatform(SLACK, 1): %v", err)
	}
	if len(limited) != 1 || limited[0].ConversationID != "slack-newer" {
		t.Fatalf("limited slack = %+v", limited)
	}

	sms, err := source.ListConversationsByPlatform("", 10)
	if err != nil {
		t.Fatalf("ListConversationsByPlatform(empty): %v", err)
	}
	if len(sms) != 1 || sms[0].ConversationID != "google-thread" || sms[0].SourcePlatform != "sms" {
		t.Fatalf("sms platform = %+v", sms)
	}

	none, err := source.ListConversationsByPlatform("whatsapp", 10)
	if err != nil {
		t.Fatalf("ListConversationsByPlatform(whatsapp): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("whatsapp platform = %+v", none)
	}
}

func TestSearchConversationsByNameMatchesTitleAndRiver(t *testing.T) {
	store, _, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-primary", "google_messages")
	seedSourceAccount(t, store, "slack-T1", "slack_web")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "google-thread",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Alice",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      200,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "slack-thread",
		AccountID:            "slack-T1",
		RemoteConversationID: "C1",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#nathan-alerts",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      300,
	})

	hits, err := source.SearchConversationsByName("nathan", "", 10)
	if err != nil {
		t.Fatalf("SearchConversationsByName: %v", err)
	}
	if len(hits) != 1 || hits[0].ConversationID != "slack-thread" {
		t.Fatalf("unscoped nathan hits = %+v", hits)
	}

	google, err := source.SearchConversationsByName("alice", "messages-default", 10)
	if err != nil {
		t.Fatalf("SearchConversationsByName(messages-default): %v", err)
	}
	if len(google) != 1 || google[0].ConversationID != "google-thread" {
		t.Fatalf("google river hits = %+v", google)
	}

	none, err := source.SearchConversationsByName("alice", "slack-T1", 10)
	if err != nil {
		t.Fatalf("SearchConversationsByName(slack-T1): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("slack river alice hits = %+v", none)
	}
}

func TestSearchMessagesFilteredScopesRiver(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-primary", "google_messages")
	seedSourceAccount(t, store, "slack-T1", "slack_web")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "google-thread",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "SMS",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      200,
	})
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "slack-thread",
		AccountID:            "slack-T1",
		RemoteConversationID: "C1",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "#general",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      300,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "google-hit",
		ConversationID:  "google-thread",
		AccountID:       "google-primary",
		RemoteMessageID: "remote-google-hit",
		Direction:       sqlite.MessageDirectionIncoming,
		Body:            "shared needle",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    200,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "slack-hit",
		ConversationID:  "slack-thread",
		AccountID:       "slack-T1",
		RemoteMessageID: "remote-slack-hit",
		Direction:       sqlite.MessageDirectionIncoming,
		Body:            "shared needle",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    300,
	})

	all, err := source.SearchMessagesFiltered("shared needle", db.SearchFilter{Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessagesFiltered(): %v", err)
	}
	assertMessageIDs(t, all, "slack-hit", "google-hit")

	slack, err := source.SearchMessagesFiltered("shared needle", db.SearchFilter{
		RiverID: "slack-T1",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchMessagesFiltered(slack): %v", err)
	}
	assertMessageIDs(t, slack, "slack-hit")

	google, err := source.SearchMessagesFiltered("shared needle", db.SearchFilter{
		RiverID: "messages-default",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("SearchMessagesFiltered(messages-default): %v", err)
	}
	assertMessageIDs(t, google, "google-hit")
}

func TestUnreadCountsFollowIncomingAndReadCursor(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "google-primary", "google_messages")
	seedSourceDevice(t, store, "google-primary")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-google",
		AccountID:            "google-primary",
		RemoteConversationID: "remote-google",
		Kind:                 sqlite.ConversationKindDirect,
		Title:                "Alice",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      300,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "message-incoming",
		ConversationID:  "conversation-google",
		AccountID:       "google-primary",
		RemoteMessageID: "remote-incoming",
		Direction:       sqlite.MessageDirectionIncoming,
		Body:            "hello",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    200,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "message-outgoing",
		ConversationID:  "conversation-google",
		AccountID:       "google-primary",
		RemoteMessageID: "remote-outgoing",
		Direction:       sqlite.MessageDirectionOutgoing,
		Body:            "reply",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    300,
	})

	conversation, err := source.GetConversation("conversation-google")
	if err != nil {
		t.Fatalf("GetConversation(): %v", err)
	}
	if conversation.UnreadCount != 1 {
		t.Fatalf("unread before cursor = %d, want 1", conversation.UnreadCount)
	}
	listed, err := source.ListConversations(10)
	if err != nil {
		t.Fatalf("ListConversations(): %v", err)
	}
	if len(listed) != 1 || listed[0].UnreadCount != 1 {
		t.Fatalf("list unread = %+v, want 1", listed)
	}

	if err := store.UpsertReadCursor(sqlite.ReadCursor{
		AccountID:      "google-primary",
		DeviceID:       "local-primary:google-primary",
		ConversationID: "conversation-google",
		LastReadAtMS:   300,
		UpdatedAtMS:    sourceTestTimeMS,
	}); err != nil {
		t.Fatalf("UpsertReadCursor(): %v", err)
	}
	conversation, err = source.GetConversation("conversation-google")
	if err != nil {
		t.Fatalf("GetConversation(after cursor): %v", err)
	}
	if conversation.UnreadCount != 0 {
		t.Fatalf("unread after cursor = %d, want 0", conversation.UnreadCount)
	}
}

func TestSourceRendersBlockKitAndTranscriptExtras(t *testing.T) {
	store, messages, source := openSourceTestStore(t)
	seedSourceAccount(t, store, "slack-T1", "slack_web")
	seedSourceConversation(t, store, sqlite.Conversation{
		ConversationID:       "conversation-slack",
		AccountID:            "slack-T1",
		RemoteConversationID: "C1",
		Kind:                 sqlite.ConversationKindGroup,
		Title:                "general",
		NotificationMode:     sqlite.NotificationModeAll,
		LastMessageAtMS:      200,
	})
	importSourceMessage(t, messages, sqlite.Message{
		MessageID:       "message-blocks",
		ConversationID:  "conversation-slack",
		AccountID:       "slack-T1",
		RemoteMessageID: "1700000001.000001",
		Direction:       sqlite.MessageDirectionIncoming,
		Body:            "short fallback",
		State:           sqlite.MessageStateActive,
		OccurredAtMS:    200,
	})
	if err := store.MergeMessagePayload(context.Background(), "message-blocks", sqlite.MessagePayload{
		Blocks:          json.RawMessage(`{"blocks":[{"type":"header","text":{"type":"plain_text","text":"PR opened"}},{"type":"actions","elements":[{"type":"button","text":{"type":"plain_text","text":"View"},"url":"https://example.com/pr","action_id":"view"},{"type":"button","text":{"type":"plain_text","text":"Approve"},"value":"ok","action_id":"approve","style":"primary"}]}]}`),
		Transcript:      "voice note",
		TranscriptModel: "whisper",
		TranscribedAtMS: 200,
	}); err != nil {
		t.Fatalf("MergeMessagePayload(): %v", err)
	}
	got, err := source.GetMessageByID("message-blocks")
	if err != nil {
		t.Fatalf("GetMessageByID(): %v", err)
	}
	if got == nil || !strings.Contains(got.Body, "PR opened") || strings.Contains(got.Body, "short fallback") {
		t.Fatalf("body = %+v", got)
	}
	if !strings.Contains(got.Body, "[View]") || !strings.Contains(got.Body, "[Approve]") {
		t.Fatalf("body missing button labels: %+v", got)
	}
	if len(got.BlockActions) != 2 {
		t.Fatalf("block_actions = %#v", got.BlockActions)
	}
	if got.BlockActions[0].Kind != "url" || got.BlockActions[0].URL != "https://example.com/pr" || got.BlockActions[0].Label != "View" {
		t.Fatalf("url action = %#v", got.BlockActions[0])
	}
	wantLink := "slack://channel?team=T1&id=C1&message=1700000001.000001"
	if got.BlockActions[1].Kind != "app" || got.BlockActions[1].URL != wantLink || got.BlockActions[1].Label != "Approve" {
		t.Fatalf("app action = %#v", got.BlockActions[1])
	}
	if got.Transcript != "voice note" || got.TranscriptModel != "whisper" || got.TranscribedAtMS != 200 {
		t.Fatalf("transcript = %+v", got)
	}
}

func openSourceTestStore(t *testing.T) (*sqlite.Store, *sqlite.MessageRepository, *Source) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "store.sqlite3"))
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close(): %v", err)
		}
	})
	messages, err := sqlite.NewMessageRepository(
		store,
		func() time.Time { return time.UnixMilli(sourceTestTimeMS) },
	)
	if err != nil {
		t.Fatalf("NewMessageRepository(): %v", err)
	}
	return store, messages, New(store)
}

func seedSourceDevice(t *testing.T, store *sqlite.Store, accountID string) {
	t.Helper()
	if err := store.UpsertDevice(sqlite.Device{
		DeviceID:    "local-primary:" + accountID,
		AccountID:   accountID,
		Kind:        sqlite.DeviceKindLocalInstallation,
		DisplayName: "OpenMessage",
		State:       sqlite.DeviceStateActive,
		IsCurrent:   true,
		CreatedAtMS: sourceTestTimeMS,
		UpdatedAtMS: sourceTestTimeMS,
	}); err != nil {
		t.Fatalf("UpsertDevice(%q): %v", accountID, err)
	}
}

func seedSourceAccount(t *testing.T, store *sqlite.Store, accountID, bridgeKey string) {
	t.Helper()
	if err := store.UpsertAccount(sqlite.Account{
		AccountID:   accountID,
		BridgeKey:   bridgeKey,
		DisplayName: accountID,
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  `{}`,
		CreatedAtMS: sourceTestTimeMS,
		UpdatedAtMS: sourceTestTimeMS,
	}); err != nil {
		t.Fatalf("UpsertAccount(%q): %v", accountID, err)
	}
}

func seedSourceConversation(t *testing.T, store *sqlite.Store, conversation sqlite.Conversation) {
	t.Helper()
	if conversation.MetadataJSON == "" {
		conversation.MetadataJSON = `{}`
	}
	if conversation.CreatedAtMS == 0 {
		conversation.CreatedAtMS = sourceTestTimeMS
	}
	if conversation.UpdatedAtMS == 0 {
		conversation.UpdatedAtMS = sourceTestTimeMS
	}
	if err := store.UpsertConversation(conversation); err != nil {
		t.Fatalf("UpsertConversation(%q): %v", conversation.ConversationID, err)
	}
}

func importSourceMessage(
	t *testing.T,
	repository *sqlite.MessageRepository,
	message sqlite.Message,
	attachments ...sqlite.MessageAttachment,
) {
	t.Helper()
	if err := repository.ImportMessage(context.Background(), sqlite.MessageProjection{
		Message:     message,
		Attachments: attachments,
	}); err != nil {
		t.Fatalf("ImportMessage(%q): %v", message.MessageID, err)
	}
}

func assertConversationIDs(t *testing.T, conversations []*db.Conversation, want ...string) {
	t.Helper()
	if len(conversations) != len(want) {
		t.Fatalf("conversation IDs = %d rows %+v, want %d %v", len(conversations), conversations, len(want), want)
	}
	for i, wantID := range want {
		if conversations[i].ConversationID != wantID {
			t.Fatalf("conversation ID %d = %q, want %q", i, conversations[i].ConversationID, wantID)
		}
	}
}

func assertMessageIDs(t *testing.T, messages []*db.Message, want ...string) {
	t.Helper()
	if len(messages) != len(want) {
		t.Fatalf("message IDs = %d rows %+v, want %d %v", len(messages), messages, len(want), want)
	}
	for i, wantID := range want {
		if messages[i].MessageID != wantID {
			t.Fatalf("message ID %d = %q, want %q", i, messages[i].MessageID, wantID)
		}
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestFormatLastMessagePreviewMatchesLegacyRules(t *testing.T) {
	long := strings.Repeat("界", 130)
	got := formatLastMessagePreview(long, "", "", false)
	if runes := []rune(got); len(runes) != 120 || runes[len(runes)-1] != '…' {
		t.Fatalf("long preview = %q (%d runes), want 120 runes ending ellipsis", got, len([]rune(got)))
	}
	if got := formatLastMessagePreview("", "v2msg:m:0", "audio/ogg", true); got != "You: Audio" {
		t.Fatalf("audio preview = %q, want %q", got, "You: Audio")
	}
}
