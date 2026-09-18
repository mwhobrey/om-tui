package slack

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/slacklive"
)

type fakePoster struct {
	conversationID string
	body           string
	replyToID      string
	msg            *db.Message
	err            error
}

func (f *fakePoster) SendText(_ context.Context, conversationID, body, replyToID string) (*db.Message, error) {
	f.conversationID = conversationID
	f.body = body
	f.replyToID = replyToID
	return f.msg, f.err
}

func TestSendTextReconstructsLegacyConversationAndReturnsTS(t *testing.T) {
	poster := &fakePoster{msg: &db.Message{
		MessageID:   "slack:C123:1700000000.000100",
		SourceID:    "C123:1700000000.000100",
		TimestampMS: 1_700_000_000_100,
	}}
	adapter := &Adapter{accountID: "slack-T1TEAM", poster: poster}

	got, err := adapter.SendText(context.Background(), bridge.TextRequest{
		AccountID: "slack-T1TEAM",
		Conversation: bridge.ConversationRef{
			RemoteID: "C123",
		},
		Body: "hello",
		ReplyTo: &bridge.MessageRef{
			RemoteID: "1700000000.000001",
		},
	})
	if err != nil {
		t.Fatalf("SendText(): %v", err)
	}
	if poster.conversationID != "slack:T1TEAM:C123" {
		t.Fatalf("legacy conversation = %q, want slack:T1TEAM:C123", poster.conversationID)
	}
	if poster.replyToID != "slack:C123:1700000000.000001" {
		t.Fatalf("reply = %q, want slack:C123:1700000000.000001", poster.replyToID)
	}
	if got.RemoteMessageID != "1700000000.000100" || got.EchoExpected {
		t.Fatalf("result = %+v", got)
	}
}

func TestSendTextAcceptsLegacyRemoteConversationID(t *testing.T) {
	poster := &fakePoster{msg: &db.Message{SourceID: "C9:1.2"}}
	adapter := &Adapter{accountID: "slack-T9", poster: poster}

	got, err := adapter.SendText(context.Background(), bridge.TextRequest{
		Conversation: bridge.ConversationRef{RemoteID: "slack:T9:C9"},
		Body:         "hi",
	})
	if err != nil {
		t.Fatalf("SendText(): %v", err)
	}
	if poster.conversationID != "slack:T9:C9" {
		t.Fatalf("legacy conversation = %q", poster.conversationID)
	}
	if got.RemoteMessageID != "1.2" {
		t.Fatalf("remote = %q", got.RemoteMessageID)
	}
}

func TestSendTextPreCallFailuresAreNotDispatched(t *testing.T) {
	adapter := &Adapter{accountID: "slack-T1"}
	_, err := adapter.SendText(context.Background(), bridge.TextRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C1"},
		Body:         "nope",
	})
	var op bridge.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error = %v, want OpError", err)
	}
	if op.Dispatch != bridge.DispatchNotCalled || op.Fingerprint != "slack_not_connected" {
		t.Fatalf("failure = %+v", op)
	}
}

func TestSendTextTransportErrorStaysTransient(t *testing.T) {
	poster := &fakePoster{err: errors.New("chat.postMessage: ratelimited")}
	adapter := &Adapter{accountID: "slack-T1", poster: poster}
	_, err := adapter.SendText(context.Background(), bridge.TextRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C1"},
		Body:         "hello",
	})
	var op bridge.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error = %v, want OpError", err)
	}
	if op.Class != bridge.FailureTransient || op.Fingerprint != "slack_send_text_failed" {
		t.Fatalf("failure = %+v", op)
	}
}

type fakeReactor struct {
	addConv    string
	addTarget  string
	addEmoji   string
	removeConv string
	removeTgt  string
	removeEmo  string
	err        error
}

func (f *fakeReactor) AddReaction(_ context.Context, conversationID, messageID, emoji string) error {
	f.addConv = conversationID
	f.addTarget = messageID
	f.addEmoji = emoji
	return f.err
}

func (f *fakeReactor) RemoveReaction(_ context.Context, conversationID, messageID, emoji string) error {
	f.removeConv = conversationID
	f.removeTgt = messageID
	f.removeEmo = emoji
	return f.err
}

func TestSendReactionReconstructsLegacyIDsAndConfirmsEmpty(t *testing.T) {
	reactor := &fakeReactor{}
	adapter := &Adapter{accountID: "slack-T1TEAM", reactions: reactor}

	got, err := adapter.SendReaction(context.Background(), bridge.ReactionRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C123"},
		Target:       bridge.MessageRef{RemoteID: "1700000000.000001"},
		Emoji:        "👍",
		Action:       bridge.ReactionAdd,
	})
	if err != nil {
		t.Fatalf("SendReaction(): %v", err)
	}
	if got != (bridge.SendResult{}) {
		t.Fatalf("result = %+v, want empty confirm", got)
	}
	if reactor.addConv != "slack:T1TEAM:C123" || reactor.addTarget != "slack:C123:1700000000.000001" || reactor.addEmoji != "👍" {
		t.Fatalf("add = conv=%q target=%q emoji=%q", reactor.addConv, reactor.addTarget, reactor.addEmoji)
	}

	if _, err := adapter.SendReaction(context.Background(), bridge.ReactionRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C123"},
		Target:       bridge.MessageRef{RemoteID: "1700000000.000001"},
		Emoji:        "❤️",
		Action:       bridge.ReactionRemove,
	}); err != nil {
		t.Fatalf("SendReaction(remove): %v", err)
	}
	if reactor.removeConv != "slack:T1TEAM:C123" || reactor.removeTgt != "slack:C123:1700000000.000001" {
		t.Fatalf("remove = conv=%q target=%q", reactor.removeConv, reactor.removeTgt)
	}
}

func TestSendReactionPreCallFailuresAreNotDispatched(t *testing.T) {
	adapter := &Adapter{accountID: "slack-T1"}
	_, err := adapter.SendReaction(context.Background(), bridge.ReactionRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C1"},
		Emoji:        "👍",
	})
	var op bridge.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error = %v, want OpError", err)
	}
	if op.Dispatch != bridge.DispatchNotCalled || op.Fingerprint != "slack_not_connected" {
		t.Fatalf("failure = %+v", op)
	}
}

type fakeFiles struct {
	fileID string
	file   slacklive.DownloadedFile
	err    error
}

func (f *fakeFiles) DownloadFile(_ context.Context, fileID string) (slacklive.DownloadedFile, error) {
	f.fileID = fileID
	return f.file, f.err
}

func TestDownloadMediaUnpacksOpaqueAndWrapsBytes(t *testing.T) {
	opaque, err := slacklive.MarshalDownloadOpaque("F123")
	if err != nil {
		t.Fatal(err)
	}
	downloader := &fakeFiles{file: slacklive.DownloadedFile{
		Data:     []byte("png-bytes"),
		Filename: "photo.png",
		MIME:     "image/png",
	}}
	adapter := &Adapter{accountID: "slack-T1", files: downloader}
	stream, err := adapter.DownloadMedia(context.Background(), "slack-T1", bridge.MediaRef{
		Opaque:   opaque,
		Filename: "fallback.bin",
		MIME:     "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("DownloadMedia(): %v", err)
	}
	defer stream.Close()
	if downloader.fileID != "F123" {
		t.Fatalf("file id = %q", downloader.fileID)
	}
	if stream.Filename != "photo.png" || stream.MIME != "image/png" || stream.Size != 9 {
		t.Fatalf("stream = %+v", stream)
	}
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "png-bytes" {
		t.Fatalf("bytes = %q", got)
	}
}

type fakeUploader struct {
	conversationID string
	filename       string
	mime           string
	caption        string
	replyToID      string
	data           []byte
	msg            *db.Message
	err            error
}

func (f *fakeUploader) SendFile(_ context.Context, conversationID, filename, mime, caption, replyToID string, data []byte) (*db.Message, error) {
	f.conversationID = conversationID
	f.filename = filename
	f.mime = mime
	f.caption = caption
	f.replyToID = replyToID
	f.data = append([]byte(nil), data...)
	return f.msg, f.err
}

func TestSendMediaReconstructsLegacyConversationAndReturnsTS(t *testing.T) {
	uploader := &fakeUploader{msg: &db.Message{
		MessageID:   "slack:C123:1700000000.000100",
		SourceID:    "C123:1700000000.000100",
		TimestampMS: 1_700_000_000_100,
	}}
	adapter := &Adapter{accountID: "slack-T1TEAM", uploads: uploader}

	got, err := adapter.SendMedia(context.Background(), bridge.MediaRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C123"},
		Reader:       strings.NewReader("png-bytes"),
		Size:         9,
		Filename:     "photo.png",
		MIME:         "image/png",
		Caption:      "look",
		ReplyTo:      &bridge.MessageRef{RemoteID: "1700000000.000001"},
	})
	if err != nil {
		t.Fatalf("SendMedia(): %v", err)
	}
	if uploader.conversationID != "slack:T1TEAM:C123" {
		t.Fatalf("legacy conversation = %q", uploader.conversationID)
	}
	if uploader.replyToID != "slack:C123:1700000000.000001" {
		t.Fatalf("reply = %q", uploader.replyToID)
	}
	if uploader.filename != "photo.png" || uploader.caption != "look" || string(uploader.data) != "png-bytes" {
		t.Fatalf("upload = %+v data=%q", uploader, uploader.data)
	}
	if got.RemoteMessageID != "1700000000.000100" || got.EchoExpected {
		t.Fatalf("result = %+v", got)
	}
}

func TestSendMediaPreCallFailuresAreNotDispatched(t *testing.T) {
	adapter := &Adapter{accountID: "slack-T1"}
	_, err := adapter.SendMedia(context.Background(), bridge.MediaRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C1"},
		Reader:       strings.NewReader("x"),
		Size:         1,
		Filename:     "a.png",
	})
	var op bridge.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error = %v, want OpError", err)
	}
	if op.Dispatch != bridge.DispatchNotCalled || op.Fingerprint != "slack_not_connected" {
		t.Fatalf("failure = %+v", op)
	}
}

func TestSendMediaTransportErrorStaysTransient(t *testing.T) {
	uploader := &fakeUploader{err: errors.New("files.completeUploadExternal: ratelimited")}
	adapter := &Adapter{accountID: "slack-T1", uploads: uploader}
	_, err := adapter.SendMedia(context.Background(), bridge.MediaRequest{
		Conversation: bridge.ConversationRef{RemoteID: "C1"},
		Reader:       strings.NewReader("x"),
		Size:         1,
		Filename:     "a.png",
	})
	var op bridge.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error = %v, want OpError", err)
	}
	if op.Class != bridge.FailureTransient || op.Fingerprint != "slack_send_media_failed" {
		t.Fatalf("failure = %+v", op)
	}
}

func TestDownloadMediaPreCallFailuresAreNotDispatched(t *testing.T) {
	adapter := &Adapter{accountID: "slack-T1"}
	_, err := adapter.DownloadMedia(context.Background(), "slack-T1", bridge.MediaRef{})
	var op bridge.OpError
	if !errors.As(err, &op) {
		t.Fatalf("error = %v, want OpError", err)
	}
	if op.Fingerprint != "slack_not_connected" {
		t.Fatalf("failure = %+v", op)
	}
}
