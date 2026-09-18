package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/slacklive"
)

const (
	SlackCodec        = slacklive.IngressCodec
	SlackCodecVersion = slacklive.IngressCodecVersion
	slackFrameMessage = "message"
)

// SlackDecoder maps capture-enriched Slack frames to normalized events.
type SlackDecoder struct{}

var _ bridge.Decoder = (*SlackDecoder)(nil)

func NewSlackDecoder() *SlackDecoder { return &SlackDecoder{} }

func NewSlackDecoderRegistration() DecoderRegistration {
	return DecoderRegistration{
		Codec:    SlackCodec,
		Platform: bridge.PlatformSlack,
		Decoder:  NewSlackDecoder(),
	}
}

func BuildSlackIngress(
	accountID string,
	generation bridge.Generation,
	frame slacklive.IngressFrame,
	receivedAt time.Time,
) (*bridge.RawIngressRecord, error) {
	frame.Kind = strings.TrimSpace(frame.Kind)
	if frame.Kind == "" {
		frame.Kind = slackFrameMessage
	}
	frame.ChannelID = strings.TrimSpace(frame.ChannelID)
	frame.TS = strings.TrimSpace(frame.TS)
	if frame.ChannelID == "" {
		return nil, fmt.Errorf("encode Slack ingress: channel_id is empty")
	}
	if frame.TS == "" {
		return nil, fmt.Errorf("encode Slack ingress: ts is empty")
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("encode Slack ingress: %w", err)
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	return &bridge.RawIngressRecord{
		AccountID:    accountID,
		Generation:   generation,
		DedupeKey:    slackIngressDedupeKey(frame),
		Codec:        SlackCodec,
		CodecVersion: SlackCodecVersion,
		ReceivedAt:   receivedAt,
		Payload:      payload,
	}, nil
}

func (d *SlackDecoder) Decode(ctx context.Context, record bridge.RawIngressRecord) ([]bridge.Event, error) {
	if ctx == nil {
		return nil, fmt.Errorf("decode Slack ingress: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if record.Codec != SlackCodec {
		return nil, fmt.Errorf("decode Slack ingress: codec %q is not %q", record.Codec, SlackCodec)
	}
	if record.CodecVersion != SlackCodecVersion {
		return nil, fmt.Errorf("decode Slack ingress: codec version %d is unsupported", record.CodecVersion)
	}
	var frame slacklive.IngressFrame
	if err := json.Unmarshal(record.Payload, &frame); err != nil {
		return nil, fmt.Errorf("decode Slack ingress envelope: %w", err)
	}
	if strings.TrimSpace(frame.Kind) != slackFrameMessage {
		return nil, fmt.Errorf("decode Slack ingress envelope: kind %q is unsupported", frame.Kind)
	}
	channelID := strings.TrimSpace(frame.ChannelID)
	ts := strings.TrimSpace(frame.TS)
	if channelID == "" {
		return nil, fmt.Errorf("decode Slack message: channel_id is empty")
	}
	if ts == "" {
		return nil, fmt.Errorf("decode Slack message: ts is empty")
	}
	if strings.TrimSpace(frame.UserID) == "" {
		return nil, nil
	}
	if strings.TrimSpace(frame.Body) == "" && len(frame.Reactions) == 0 && len(frame.Files) == 0 && len(frame.BlocksJSON) == 0 {
		return nil, nil
	}
	occurredAt := time.UnixMilli(frame.TimestampMS)
	if frame.TimestampMS <= 0 {
		occurredAt = record.ReceivedAt
	}
	kind := strings.TrimSpace(frame.ChannelKind)
	if kind == "" {
		kind = slackChannelKind(channelID)
	}
	sender := slackIdentity(frame.UserID, frame.UserName, frame.IsFromMe)
	direction := "incoming"
	if frame.IsFromMe {
		direction = "outgoing"
	}
	replyTo := ""
	threadTS := strings.TrimSpace(frame.ThreadTS)
	if threadTS != "" && threadTS != ts {
		replyTo = threadTS
	}
	attachments, err := slackAttachments(frame.Files)
	if err != nil {
		return nil, err
	}
	events := []bridge.Event{
		{
			Kind: bridge.EventConversation,
			Conversation: &bridge.ConversationEvent{
				RemoteConversationID: channelID,
				Kind:                 kind,
				Title:                strings.TrimSpace(frame.ChannelName),
				Participants: []bridge.Participant{{
					Identity: sender,
					Role:     "member",
					Active:   true,
				}},
			},
		},
		{
			Kind: bridge.EventMessage,
			Message: &bridge.MessageEvent{
				RemoteConversationID: channelID,
				RemoteMessageID:      ts,
				Sender:               sender,
				Direction:            direction,
				Body:                 frame.Body,
				Attachments:          attachments,
				ReplyToRemoteID:      replyTo,
				OccurredAt:           occurredAt,
				LayoutJSON:           string(frame.BlocksJSON),
			},
		},
	}
	for _, reaction := range frame.Reactions {
		name := strings.TrimSpace(reaction.Name)
		userID := strings.TrimSpace(reaction.UserID)
		if name == "" || userID == "" {
			continue
		}
		events = append(events, bridge.Event{
			Kind: bridge.EventReaction,
			Reaction: &bridge.ReactionEvent{
				RemoteConversationID:  channelID,
				TargetRemoteMessageID: ts,
				Actor:                 slackIdentity(userID, reaction.UserName, reaction.IsSelf),
				Emoji:                 slacklive.ReactionEmoji(name),
				Action:                bridge.ReactionAdd,
				OccurredAt:            occurredAt,
			},
		})
	}
	return events, nil
}

func slackIngressDedupeKey(frame slacklive.IngressFrame) string {
	key := frame.ChannelID + ":" + frame.TS
	var extra []string
	if digest := slackReactionDedupe(frame.Reactions); digest != "" {
		extra = append(extra, digest)
	}
	if digest := slackFileDedupe(frame.Files); digest != "" {
		extra = append(extra, digest)
	}
	if len(extra) == 0 {
		return key
	}
	return key + ":" + strings.Join(extra, ":")
}

func slackFileDedupe(files []slacklive.IngressFile) string {
	if len(files) == 0 {
		return ""
	}
	parts := make([]string, 0, len(files))
	for _, file := range files {
		id := strings.TrimSpace(file.ID)
		if id == "" {
			continue
		}
		parts = append(parts, id)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return "files=" + strings.Join(parts, ",")
}

func slackAttachments(files []slacklive.IngressFile) ([]bridge.Attachment, error) {
	if len(files) == 0 {
		return nil, nil
	}
	out := make([]bridge.Attachment, 0, len(files))
	for _, file := range files {
		id := strings.TrimSpace(file.ID)
		if id == "" {
			continue
		}
		remoteRef, err := slacklive.MarshalDownloadOpaque(id)
		if err != nil {
			return nil, fmt.Errorf("encode Slack attachment %q: %w", id, err)
		}
		out = append(out, bridge.Attachment{
			RemoteID:  id,
			RemoteRef: remoteRef,
			Filename:  strings.TrimSpace(file.Name),
			MIME:      strings.TrimSpace(file.MIME),
			Size:      file.Size,
		})
	}
	return out, nil
}

func slackReactionDedupe(reactions []slacklive.IngressReaction) string {
	if len(reactions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(reactions))
	for _, reaction := range reactions {
		name := strings.TrimSpace(reaction.Name)
		userID := strings.TrimSpace(reaction.UserID)
		if name == "" || userID == "" {
			continue
		}
		parts = append(parts, name+"="+userID)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func slackChannelKind(channelID string) string {
	if strings.HasPrefix(channelID, "D") {
		return "direct"
	}
	return "group"
}

func slackIdentity(userID, name string, self bool) bridge.IdentityRef {
	return bridge.IdentityRef{
		Raw:    strings.TrimSpace(userID),
		Name:   strings.TrimSpace(name),
		IsSelf: self,
	}
}
