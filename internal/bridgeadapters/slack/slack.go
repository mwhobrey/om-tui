// Package slack adapts slacklive clients to the bridge registry surface.
package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
	"github.com/maxghenis/openmessage/internal/slacklive"
)

type textPoster interface {
	SendText(ctx context.Context, conversationID, body, replyToID string) (*db.Message, error)
}

type reactionMutator interface {
	AddReaction(ctx context.Context, conversationID, messageID, emoji string) error
	RemoveReaction(ctx context.Context, conversationID, messageID, emoji string) error
}

type fileDownloader interface {
	DownloadFile(ctx context.Context, fileID string) (slacklive.DownloadedFile, error)
}

type fileUploader interface {
	SendFile(ctx context.Context, conversationID, filename, mime, caption, replyToID string, data []byte) (*db.Message, error)
}

// Adapter is a thin registry entry for one Slack river.
type Adapter struct {
	accountID string
	client    *slacklive.Client
	poster    textPoster
	reactions reactionMutator
	files     fileDownloader
	uploads   fileUploader
}

func New(accountID string, client *slacklive.Client) *Adapter {
	adapter := &Adapter{accountID: accountID, client: client}
	if client != nil {
		adapter.poster = client
		adapter.reactions = client
		adapter.files = client
		adapter.uploads = client
	}
	return adapter
}

func (a *Adapter) AccountID() string         { return a.accountID }
func (a *Adapter) Platform() bridge.Platform { return bridge.PlatformSlack }
func (a *Adapter) AccountDisplayName() string {
	if a == nil || a.client == nil {
		return "Slack"
	}
	if name := a.client.TeamName(); name != "" {
		return name
	}
	return "Slack"
}
func (a *Adapter) DeclaredCapabilities() bridge.CapabilitySet {
	return bridge.CapabilitySet{TextSend: true, Reactions: true}
}

func (a *Adapter) Start(ctx context.Context, req bridge.StartRequest, sink bridge.ConnectionSink) (bridge.Run, error) {
	if a.client == nil {
		return nil, fmt.Errorf("slack adapter: client required")
	}
	if err := a.client.Start(ctx); err != nil {
		return nil, err
	}
	return &run{client: a.client, started: time.Now()}, nil
}

func (a *Adapter) SendText(ctx context.Context, req bridge.TextRequest) (bridge.SendResult, error) {
	if ctx == nil {
		return bridge.SendResult{}, slackTextPreCallFailure(
			"slack_text_context_missing",
			errors.New("Slack text send context is nil"),
		)
	}
	if err := ctx.Err(); err != nil {
		return bridge.SendResult{}, slackTextPreCallFailure("slack_text_context_done", err)
	}
	if a == nil || a.poster == nil {
		return bridge.SendResult{}, slackTextPreCallFailure(
			"slack_not_connected",
			errors.New("Slack client is not connected"),
		)
	}
	conversationID, err := slackLegacyConversationID(a.accountID, req.Conversation.RemoteID)
	if err != nil {
		return bridge.SendResult{}, slackTextPreCallFailure("slack_conversation_unusable", err)
	}
	replyToID := ""
	if req.ReplyTo != nil {
		replyToID = slackReplyToLegacyID(req.Conversation.RemoteID, req.ReplyTo.RemoteID)
	}
	msg, err := a.poster.SendText(ctx, conversationID, req.Body, replyToID)
	if err != nil {
		return bridge.SendResult{}, bridge.OpError{
			Class:       bridge.FailureTransient,
			Operation:   "send_text",
			Fingerprint: "slack_send_text_failed",
			Cause:       err,
		}
	}
	ts := slackResultTS(msg)
	if ts == "" {
		return bridge.SendResult{}, bridge.OpError{
			Class:       bridge.FailureTransient,
			Operation:   "send_text",
			Fingerprint: "slack_send_text_missing_ts",
			Cause:       errors.New("Slack send returned no timestamp"),
		}
	}
	acceptedAt := time.Now()
	if msg != nil && msg.TimestampMS > 0 {
		acceptedAt = time.UnixMilli(msg.TimestampMS)
	}
	return bridge.SendResult{
		RemoteMessageID: ts,
		AcceptedAt:      acceptedAt,
		EchoExpected:    false,
	}, nil
}

func (a *Adapter) SendReaction(ctx context.Context, req bridge.ReactionRequest) (bridge.SendResult, error) {
	if ctx == nil {
		return bridge.SendResult{}, slackReactionPreCallFailure(
			"slack_reaction_context_missing",
			errors.New("Slack reaction send context is nil"),
		)
	}
	if err := ctx.Err(); err != nil {
		return bridge.SendResult{}, slackReactionPreCallFailure("slack_reaction_context_done", err)
	}
	if a == nil || a.reactions == nil {
		return bridge.SendResult{}, slackReactionPreCallFailure(
			"slack_not_connected",
			errors.New("Slack client is not connected"),
		)
	}
	conversationID, err := slackLegacyConversationID(a.accountID, req.Conversation.RemoteID)
	if err != nil {
		return bridge.SendResult{}, slackReactionPreCallFailure("slack_conversation_unusable", err)
	}
	targetID := slackReplyToLegacyID(req.Conversation.RemoteID, req.Target.RemoteID)
	if strings.TrimSpace(req.Target.RemoteID) != "" && targetID == "" {
		targetID = strings.TrimSpace(req.Target.RemoteID)
	}
	var mutateErr error
	if req.Action == bridge.ReactionRemove {
		mutateErr = a.reactions.RemoveReaction(ctx, conversationID, targetID, req.Emoji)
	} else {
		mutateErr = a.reactions.AddReaction(ctx, conversationID, targetID, req.Emoji)
	}
	if mutateErr != nil {
		return bridge.SendResult{}, bridge.OpError{
			Class:       bridge.FailureTransient,
			Operation:   "send_reaction",
			Fingerprint: "slack_send_reaction_failed",
			Cause:       mutateErr,
		}
	}
	return bridge.SendResult{}, nil
}

func (a *Adapter) DownloadMedia(ctx context.Context, _ string, ref bridge.MediaRef) (bridge.MediaStream, error) {
	if ctx == nil {
		return bridge.MediaStream{}, slackDownloadPreCallFailure(
			"slack_download_context_missing",
			errors.New("Slack media download context is nil"),
		)
	}
	if err := ctx.Err(); err != nil {
		return bridge.MediaStream{}, slackDownloadPreCallFailure("slack_download_context_done", err)
	}
	if a == nil || a.files == nil {
		return bridge.MediaStream{}, slackDownloadPreCallFailure(
			"slack_not_connected",
			errors.New("Slack client is not connected"),
		)
	}
	opaque, err := slacklive.UnmarshalDownloadOpaque(ref.Opaque)
	if err != nil {
		return bridge.MediaStream{}, bridge.OpError{
			Class:       bridge.FailureUnsupported,
			Operation:   "download_media",
			Fingerprint: "slack_opaque_malformed",
			Cause:       err,
		}
	}
	file, err := a.files.DownloadFile(ctx, opaque.FileID)
	if err != nil {
		return bridge.MediaStream{}, bridge.OpError{
			Class:       bridge.FailureTransient,
			Operation:   "download_media",
			Fingerprint: "slack_download_media_failed",
			Cause:       err,
		}
	}
	return bridge.MediaStream{
		ReadCloser: io.NopCloser(bytes.NewReader(file.Data)),
		Size:       int64(len(file.Data)),
		Filename:   firstNonEmpty(file.Filename, ref.Filename),
		MIME:       firstNonEmpty(file.MIME, ref.MIME),
	}, nil
}

func (a *Adapter) SendMedia(ctx context.Context, req bridge.MediaRequest) (bridge.SendResult, error) {
	if ctx == nil {
		return bridge.SendResult{}, slackMediaPreCallFailure(
			"slack_media_context_missing",
			errors.New("Slack media send context is nil"),
		)
	}
	if err := ctx.Err(); err != nil {
		return bridge.SendResult{}, slackMediaPreCallFailure("slack_media_context_done", err)
	}
	if a == nil || a.uploads == nil {
		return bridge.SendResult{}, slackMediaPreCallFailure(
			"slack_not_connected",
			errors.New("Slack client is not connected"),
		)
	}
	if req.Reader == nil {
		return bridge.SendResult{}, slackMediaPreCallFailure(
			"slack_media_read_failed",
			errors.New("Slack media reader is nil"),
		)
	}
	limit := req.Size + 1
	if req.Size < 0 || limit <= 0 {
		return bridge.SendResult{}, slackMediaPreCallFailure(
			"slack_media_size_mismatch",
			fmt.Errorf("invalid Slack media size %d", req.Size),
		)
	}
	data, err := io.ReadAll(io.LimitReader(req.Reader, limit))
	if err != nil {
		return bridge.SendResult{}, slackMediaPreCallFailure(
			"slack_media_read_failed",
			fmt.Errorf("read Slack media: %w", err),
		)
	}
	if int64(len(data)) != req.Size {
		return bridge.SendResult{}, slackMediaPreCallFailure(
			"slack_media_size_mismatch",
			fmt.Errorf("Slack media reader returned %d bytes, want %d", len(data), req.Size),
		)
	}
	if err := ctx.Err(); err != nil {
		return bridge.SendResult{}, slackMediaPreCallFailure("slack_media_context_done", err)
	}
	conversationID, err := slackLegacyConversationID(a.accountID, req.Conversation.RemoteID)
	if err != nil {
		return bridge.SendResult{}, slackMediaPreCallFailure("slack_conversation_unusable", err)
	}
	replyToID := ""
	if req.ReplyTo != nil {
		replyToID = slackReplyToLegacyID(req.Conversation.RemoteID, req.ReplyTo.RemoteID)
	}
	msg, err := a.uploads.SendFile(ctx, conversationID, req.Filename, req.MIME, req.Caption, replyToID, data)
	if err != nil {
		return bridge.SendResult{}, bridge.OpError{
			Class:       bridge.FailureTransient,
			Operation:   "send_media",
			Fingerprint: "slack_send_media_failed",
			Cause:       err,
		}
	}
	ts := slackResultTS(msg)
	if ts == "" {
		return bridge.SendResult{}, bridge.OpError{
			Class:       bridge.FailureTransient,
			Operation:   "send_media",
			Fingerprint: "slack_send_media_missing_ts",
			Cause:       errors.New("Slack media send returned no timestamp"),
		}
	}
	acceptedAt := time.Now()
	if msg != nil && msg.TimestampMS > 0 {
		acceptedAt = time.UnixMilli(msg.TimestampMS)
	}
	return bridge.SendResult{
		RemoteMessageID: ts,
		AcceptedAt:      acceptedAt,
		EchoExpected:    false,
	}, nil
}

func slackLegacyConversationID(accountID, remoteID string) (string, error) {
	remoteID = strings.TrimSpace(remoteID)
	if _, _, ok := river.ParseSlackConversationID(remoteID); ok {
		return remoteID, nil
	}
	if remoteID == "" {
		return "", errors.New("Slack conversation remote id is empty")
	}
	teamID := strings.TrimPrefix(strings.TrimSpace(accountID), "slack-")
	if teamID == "" || teamID == accountID {
		return "", fmt.Errorf("Slack account %q is not a slack river id", accountID)
	}
	return river.SlackConversationID(teamID, remoteID), nil
}

func slackReplyToLegacyID(remoteConversationID, replyRemoteID string) string {
	replyRemoteID = strings.TrimSpace(replyRemoteID)
	if replyRemoteID == "" {
		return ""
	}
	if strings.HasPrefix(replyRemoteID, "slack:") {
		return replyRemoteID
	}
	_, channelID, ok := river.ParseSlackConversationID(remoteConversationID)
	if !ok {
		channelID = strings.TrimSpace(remoteConversationID)
	}
	if channelID == "" {
		return replyRemoteID
	}
	return fmt.Sprintf("slack:%s:%s", channelID, replyRemoteID)
}

func slackResultTS(msg *db.Message) string {
	if msg == nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(msg.MessageID), ":")
	if len(parts) == 3 && parts[0] == "slack" && parts[2] != "" {
		return parts[2]
	}
	source := strings.TrimSpace(msg.SourceID)
	if _, ts, ok := strings.Cut(source, ":"); ok && ts != "" {
		return ts
	}
	return source
}

func slackTextPreCallFailure(fingerprint string, cause error) bridge.OpError {
	return bridge.OpError{
		Class:       bridge.FailureTransient,
		Operation:   "send_text",
		Fingerprint: fingerprint,
		Dispatch:    bridge.DispatchNotCalled,
		Cause:       cause,
	}
}

func slackReactionPreCallFailure(fingerprint string, cause error) bridge.OpError {
	return bridge.OpError{
		Class:       bridge.FailureTransient,
		Operation:   "send_reaction",
		Fingerprint: fingerprint,
		Dispatch:    bridge.DispatchNotCalled,
		Cause:       cause,
	}
}

func slackDownloadPreCallFailure(fingerprint string, cause error) bridge.OpError {
	return bridge.OpError{
		Class:       bridge.FailureTransient,
		Operation:   "download_media",
		Fingerprint: fingerprint,
		Cause:       cause,
	}
}

func slackMediaPreCallFailure(fingerprint string, cause error) bridge.OpError {
	return bridge.OpError{
		Class:       bridge.FailureTransient,
		Operation:   "send_media",
		Fingerprint: fingerprint,
		Dispatch:    bridge.DispatchNotCalled,
		Cause:       cause,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var _ bridge.TextSender = (*Adapter)(nil)
var _ bridge.ReactionSender = (*Adapter)(nil)
var _ bridge.MediaDownloader = (*Adapter)(nil)
var _ bridge.MediaSender = (*Adapter)(nil)

type run struct {
	client  *slacklive.Client
	started time.Time
	ready   chan struct{}
	done    chan error
}

func (r *run) Ready() <-chan struct{} {
	if r.ready == nil {
		r.ready = make(chan struct{})
		close(r.ready)
	}
	return r.ready
}

func (r *run) Done() <-chan error {
	if r.done == nil {
		r.done = make(chan error)
	}
	return r.done
}

func (r *run) Probe(ctx context.Context) (bridge.Liveness, error) {
	ok, detail := r.client.Status()
	if !ok {
		return bridge.Liveness{AliveAt: time.Now(), Detail: detail}, fmt.Errorf("slack disconnected: %s", detail)
	}
	return bridge.Liveness{AliveAt: time.Now(), Detail: "ok"}, nil
}

func (r *run) Stop(ctx context.Context) error {
	r.client.Stop()
	if r.done != nil {
		select {
		case <-r.done:
		default:
			close(r.done)
		}
	}
	return nil
}
