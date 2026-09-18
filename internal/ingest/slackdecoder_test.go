package ingest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/slacklive"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

func TestSlackDecoderRegistration(t *testing.T) {
	t.Parallel()
	reg := NewSlackDecoderRegistration()
	if reg.Codec != SlackCodec || reg.Platform != bridge.PlatformSlack {
		t.Fatalf("registration = %+v", reg)
	}
	if _, ok := reg.Decoder.(*SlackDecoder); !ok {
		t.Fatalf("decoder type = %T", reg.Decoder)
	}
}

func TestBuildSlackIngressAndDecodeMessage(t *testing.T) {
	t.Parallel()
	receivedAt := time.UnixMilli(1_700_000_000_000)
	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		Kind:        "message",
		ChannelID:   "C1",
		ChannelName: "general",
		ChannelKind: "group",
		TS:          "1700000001.000001",
		ThreadTS:    "1700000000.000001",
		UserID:      "U1",
		UserName:    "Alice",
		Body:        "hello",
		TimestampMS: 1_700_000_001_000,
	}, receivedAt)
	if err != nil {
		t.Fatalf("BuildSlackIngress(): %v", err)
	}
	if record.Codec != SlackCodec || record.DedupeKey != "C1:1700000001.000001" || record.AccountID != "slack-T1" {
		t.Fatalf("record = %+v", record)
	}
	events, err := NewSlackDecoder().Decode(context.Background(), *record)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %+v", len(events), events)
	}
	if events[0].Kind != bridge.EventConversation || events[0].Conversation.Kind != "group" ||
		events[0].Conversation.RemoteConversationID != "C1" || events[0].Conversation.Title != "general" {
		t.Fatalf("conversation = %+v", events[0].Conversation)
	}
	msg := events[1].Message
	if events[1].Kind != bridge.EventMessage || msg == nil {
		t.Fatalf("message event = %+v", events[1])
	}
	if msg.RemoteMessageID != "1700000001.000001" || msg.Body != "hello" || msg.ReplyToRemoteID != "1700000000.000001" ||
		msg.Sender.Raw != "U1" || msg.Direction != "incoming" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestSlackDecoderCopiesLayoutJSON(t *testing.T) {
	t.Parallel()
	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		Kind:        "message",
		ChannelID:   "C1",
		TS:          "1700000001.000001",
		UserID:      "U1",
		UserName:    "Alice",
		Body:        "fallback",
		TimestampMS: 1_700_000_001_000,
		BlocksJSON:  []byte(`{"blocks":[{"type":"header","text":{"type":"plain_text","text":"Title"}}]}`),
	}, time.UnixMilli(1_700_000_000_000))
	if err != nil {
		t.Fatalf("BuildSlackIngress(): %v", err)
	}
	events, err := NewSlackDecoder().Decode(context.Background(), *record)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(events) != 2 || events[1].Message == nil {
		t.Fatalf("events = %+v", events)
	}
	if !strings.Contains(events[1].Message.LayoutJSON, `"type":"header"`) {
		t.Fatalf("LayoutJSON = %q", events[1].Message.LayoutJSON)
	}
}

func TestBuildSlackIngressDecodesEmbeddedReactions(t *testing.T) {
	t.Parallel()
	receivedAt := time.UnixMilli(1_700_000_000_000)
	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		Kind:        "message",
		ChannelID:   "C1",
		TS:          "1700000001.000001",
		UserID:      "U1",
		UserName:    "Alice",
		Body:        "hello",
		TimestampMS: 1_700_000_001_000,
		Reactions: []slacklive.IngressReaction{{
			Name:     "+1",
			UserID:   "U2",
			UserName: "Bob",
		}, {
			Name:   "heart",
			UserID: "U0",
			IsSelf: true,
		}},
	}, receivedAt)
	if err != nil {
		t.Fatalf("BuildSlackIngress(): %v", err)
	}
	if record.DedupeKey != "C1:1700000001.000001:+1=U2;heart=U0" {
		t.Fatalf("dedupe = %q", record.DedupeKey)
	}
	events, err := NewSlackDecoder().Decode(context.Background(), *record)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4: %+v", len(events), events)
	}
	if events[2].Kind != bridge.EventReaction || events[2].Reaction.Emoji != "👍" ||
		events[2].Reaction.Actor.Raw != "U2" || events[2].Reaction.Actor.IsSelf {
		t.Fatalf("first reaction = %+v", events[2].Reaction)
	}
	if events[3].Kind != bridge.EventReaction || events[3].Reaction.Emoji != "❤️" ||
		!events[3].Reaction.Actor.IsSelf {
		t.Fatalf("self reaction = %+v", events[3].Reaction)
	}
}

func TestBuildSlackIngressDecodesFileAttachments(t *testing.T) {
	t.Parallel()
	receivedAt := time.UnixMilli(1_700_000_000_000)
	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		ChannelID:   "C1",
		TS:          "1700000001.000001",
		UserID:      "U1",
		Body:        "[file]",
		TimestampMS: 1_700_000_001_000,
		Files: []slacklive.IngressFile{{
			ID:   "F123",
			Name: "photo.png",
			MIME: "image/png",
			Size: 12,
		}},
	}, receivedAt)
	if err != nil {
		t.Fatalf("BuildSlackIngress(): %v", err)
	}
	if record.DedupeKey != "C1:1700000001.000001:files=F123" {
		t.Fatalf("dedupe = %q", record.DedupeKey)
	}
	events, err := NewSlackDecoder().Decode(context.Background(), *record)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(events) != 2 || events[1].Message == nil || len(events[1].Message.Attachments) != 1 {
		t.Fatalf("events = %+v", events)
	}
	att := events[1].Message.Attachments[0]
	if att.RemoteID != "F123" || att.Filename != "photo.png" || att.MIME != "image/png" || att.Size != 12 {
		t.Fatalf("attachment = %+v", att)
	}
	opaque, err := slacklive.UnmarshalDownloadOpaque(att.RemoteRef)
	if err != nil || opaque.FileID != "F123" {
		t.Fatalf("opaque = %+v err=%v", opaque, err)
	}
}

func TestSlackDecoderDirectChannelAndEmptyBody(t *testing.T) {
	t.Parallel()
	receivedAt := time.UnixMilli(1_700_000_000_000)
	direct, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		ChannelID:   "D99",
		TS:          "1.0",
		UserID:      "U2",
		Body:        "dm",
		IsFromMe:    true,
		TimestampMS: 1_700_000_002_000,
	}, receivedAt)
	if err != nil {
		t.Fatal(err)
	}
	events, err := NewSlackDecoder().Decode(context.Background(), *direct)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Conversation.Kind != "direct" || events[1].Message.Direction != "outgoing" {
		t.Fatalf("direct events = %+v", events)
	}

	empty, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		ChannelID: "C1",
		TS:        "2.0",
		UserID:    "U1",
		Body:      "  ",
	}, receivedAt)
	if err != nil {
		t.Fatal(err)
	}
	events, err = NewSlackDecoder().Decode(context.Background(), *empty)
	if err != nil {
		t.Fatal(err)
	}
	if events != nil {
		t.Fatalf("empty body events = %+v, want nil", events)
	}
}

func TestSlackWorkerProjectsChannelMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-ingest.sqlite3")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.UnixMilli(1_700_000_000_000)
	if err := store.UpsertAccount(sqlite.Account{
		AccountID:   "slack-T1",
		BridgeKey:   "slack_web",
		DisplayName: "Acme",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: now.UnixMilli(),
		UpdatedAtMS: now.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return now }
	messages, err := sqlite.NewMessageRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	reactions, err := sqlite.NewReactionRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	counters := &Counters{}
	worker, err := NewWorker(WorkerConfig{
		Store:     store,
		Messages:  messages,
		Reactions: reactions,
		Counters:  counters,
		Logger:    zerolog.Nop(),
		Now:       clock,
		Decoders:  []DecoderRegistration{NewSlackDecoderRegistration()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := i01NewSink(t, messages, worker, counters, "slack-inbox")
	i01StartWorker(t, worker)

	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		ChannelID:   "C1",
		ChannelName: "general",
		TS:          "1700000001.000001",
		UserID:      "U01ABCDEF",
		UserName:    "Alice",
		Body:        "hello v2",
		TimestampMS: now.UnixMilli(),
		BlocksJSON:  []byte(`{"blocks":[{"type":"header","text":{"type":"plain_text","text":"Title"}}]}`),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.AppendIngress(context.Background(), *record); err != nil {
		t.Fatalf("AppendIngress(): %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending, err := messages.Unprocessed(context.Background())
		if err == nil && len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("inbox not processed: pending=%d err=%v", len(pending), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	convo, err := store.GetConversationByRemote("slack-T1", "C1")
	if err != nil {
		t.Fatalf("GetConversationByRemote(): %v", err)
	}
	if convo.Kind != sqlite.ConversationKindGroup || convo.Title != "general" {
		t.Fatalf("conversation = %+v", convo)
	}
	got, err := messages.GetMessageByRemote(context.Background(), "slack-T1", convo.ConversationID, "1700000001.000001")
	if err != nil {
		t.Fatalf("GetMessageByRemote(): %v", err)
	}
	if got.Body != "hello v2" {
		t.Fatalf("projected body = %q", got.Body)
	}
	extras, err := store.MessageExtrasFor(context.Background(), []string{got.MessageID})
	if err != nil {
		t.Fatalf("MessageExtrasFor(): %v", err)
	}
	if extra, ok := extras[got.MessageID]; !ok || !strings.Contains(string(extra.Payload.Blocks), `"type":"header"`) {
		t.Fatalf("extras = %+v", extras)
	}
}

func TestSlackWorkerAppliesEmbeddedReactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-reactions.sqlite3")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.UnixMilli(1_700_000_000_000)
	if err := store.UpsertAccount(sqlite.Account{
		AccountID:   "slack-T1",
		BridgeKey:   "slack_web",
		DisplayName: "Acme",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: now.UnixMilli(),
		UpdatedAtMS: now.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return now }
	messages, err := sqlite.NewMessageRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	reactions, err := sqlite.NewReactionRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	counters := &Counters{}
	worker, err := NewWorker(WorkerConfig{
		Store:     store,
		Messages:  messages,
		Reactions: reactions,
		Counters:  counters,
		Logger:    zerolog.Nop(),
		Now:       clock,
		Decoders:  []DecoderRegistration{NewSlackDecoderRegistration()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := i01NewSink(t, messages, worker, counters, "slack-inbox")
	i01StartWorker(t, worker)

	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		ChannelID:   "C1",
		ChannelName: "general",
		TS:          "1700000001.000001",
		UserID:      "U01ABCDEF",
		UserName:    "Alice",
		Body:        "hello v2",
		TimestampMS: now.UnixMilli(),
		Reactions: []slacklive.IngressReaction{{
			Name:     "+1",
			UserID:   "U01ABCDEF",
			UserName: "Alice",
		}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.AppendIngress(context.Background(), *record); err != nil {
		t.Fatalf("AppendIngress(): %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var messageID string
	for {
		pending, err := messages.Unprocessed(context.Background())
		if err == nil && len(pending) == 0 {
			convo, convErr := store.GetConversationByRemote("slack-T1", "C1")
			if convErr == nil {
				got, msgErr := messages.GetMessageByRemote(context.Background(), "slack-T1", convo.ConversationID, "1700000001.000001")
				if msgErr == nil {
					messageID = got.MessageID
					if len(activeReactionRows(t, reactions, messageID)) == 1 {
						break
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("reactions not applied: pending=%d err=%v", len(pending), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	assertSingleActiveReaction(t, reactions, messageID, "👍", "U01ABCDEF", false)
	if snapshot := counters.Snapshot("slack-T1"); snapshot.ReactionsApplied != 1 {
		t.Fatalf("counters = %+v", snapshot)
	}
}

func TestSlackWorkerProjectsFileAttachment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-files.sqlite3")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.UnixMilli(1_700_000_000_000)
	if err := store.UpsertAccount(sqlite.Account{
		AccountID:   "slack-T1",
		BridgeKey:   "slack_web",
		DisplayName: "Acme",
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: now.UnixMilli(),
		UpdatedAtMS: now.UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return now }
	messages, err := sqlite.NewMessageRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	reactions, err := sqlite.NewReactionRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := sqlite.NewMessageAttachmentRepository(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	counters := &Counters{}
	worker, err := NewWorker(WorkerConfig{
		Store:     store,
		Messages:  messages,
		Reactions: reactions,
		Counters:  counters,
		Logger:    zerolog.Nop(),
		Now:       clock,
		Decoders:  []DecoderRegistration{NewSlackDecoderRegistration()},
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := i01NewSink(t, messages, worker, counters, "slack-inbox")
	i01StartWorker(t, worker)

	record, err := BuildSlackIngress("slack-T1", 0, slacklive.IngressFrame{
		ChannelID:   "C1",
		ChannelName: "general",
		TS:          "1700000001.000001",
		UserID:      "U01ABCDEF",
		UserName:    "Alice",
		Body:        "[file]",
		TimestampMS: now.UnixMilli(),
		Files: []slacklive.IngressFile{{
			ID:   "F99",
			Name: "shot.png",
			MIME: "image/png",
			Size: 4,
		}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.AppendIngress(context.Background(), *record); err != nil {
		t.Fatalf("AppendIngress(): %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var messageID string
	for {
		pending, err := messages.Unprocessed(context.Background())
		if err == nil && len(pending) == 0 {
			convo, convErr := store.GetConversationByRemote("slack-T1", "C1")
			if convErr == nil {
				got, msgErr := messages.GetMessageByRemote(context.Background(), "slack-T1", convo.ConversationID, "1700000001.000001")
				if msgErr == nil {
					messageID = got.MessageID
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("inbox not processed: pending=%d err=%v", len(pending), err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	att, err := attachments.GetForDownload(context.Background(), messageID, 0)
	if err != nil {
		t.Fatalf("GetForDownload(): %v", err)
	}
	if att.RemoteID != "F99" || att.Filename != "shot.png" || att.MIME != "image/png" || att.State != "pending" {
		t.Fatalf("attachment = %+v", att)
	}
	opaque, err := slacklive.UnmarshalDownloadOpaque(att.RemoteRef)
	if err != nil || opaque.FileID != "F99" {
		t.Fatalf("opaque = %+v err=%v", opaque, err)
	}
}
