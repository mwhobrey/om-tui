package v2read

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
	"github.com/maxghenis/openmessage/internal/slacklive"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2keys"
)

type participantDTO struct {
	Name   string `json:"name"`
	Number string `json:"number"`
}

type reactionDTO struct {
	Emoji  string   `json:"emoji"`
	Count  int      `json:"count"`
	Actors []string `json:"actors,omitempty"`
}

func slackTeamID(accountID string) string {
	const prefix = "slack-"
	if strings.HasPrefix(accountID, prefix) && len(accountID) > len(prefix) {
		return accountID[len(prefix):]
	}
	return ""
}

func platformForBridgeKey(bridgeKey string) string {
	bridgeKey = strings.TrimSpace(bridgeKey)
	switch bridgeKey {
	case "google_messages":
		return "sms"
	case "whatsmeow":
		return "whatsapp"
	case "signal_cli":
		return "signal"
	case "slack_web":
		return "slack"
	case "gchat", "imessage":
		return bridgeKey
	default:
		return bridgeKey
	}
}

func (s *Source) accountIndex() (map[string]sqlite.Account, error) {
	accounts, err := s.store.ListAccounts()
	if err != nil {
		return nil, err
	}
	index := make(map[string]sqlite.Account, len(accounts))
	for _, account := range accounts {
		index[account.AccountID] = account
	}
	return index, nil
}

func (s *Source) mapConversation(
	conversation sqlite.Conversation,
	accounts map[string]sqlite.Account,
) (*db.Conversation, error) {
	account, ok := accounts[conversation.AccountID]
	if !ok {
		return nil, fmt.Errorf(
			"map conversation %q: account %q is missing",
			conversation.ConversationID,
			conversation.AccountID,
		)
	}
	participants, err := s.participantsJSON(conversation.ConversationID)
	if err != nil {
		return nil, err
	}
	return &db.Conversation{
		ConversationID:   conversation.ConversationID,
		Name:             conversation.Title,
		IsGroup:          conversation.Kind == sqlite.ConversationKindGroup,
		Participants:     participants,
		LastMessageTS:    conversation.LastMessageAtMS,
		UnreadCount:      0,
		SourcePlatform:   platformForBridgeKey(account.BridgeKey),
		IsFavorite:       conversation.IsFavorite,
		NotificationMode: string(conversation.NotificationMode),
		Tab:              sqlite.ConversationTab(conversation),
		RiverID:          riverIDForAccount(account),
	}, nil
}

func riverIDForAccount(account sqlite.Account) string {
	switch platformForBridgeKey(account.BridgeKey) {
	case "sms":
		return river.DefaultMessagesRiverID
	case "whatsapp":
		if account.AccountID != "" && account.AccountID != "whatsapp-primary" {
			return account.AccountID
		}
		return river.DefaultWhatsAppRiverID
	case "signal":
		if account.AccountID != "" && account.AccountID != "signal-primary" {
			return account.AccountID
		}
		return river.DefaultSignalRiverID
	case "slack":
		return account.AccountID
	default:
		return ""
	}
}

func accountIDForRiver(riverID string, accounts map[string]sqlite.Account) string {
	riverID = strings.TrimSpace(riverID)
	if riverID == "" {
		return ""
	}
	if _, ok := accounts[riverID]; ok {
		return riverID
	}
	for accountID, account := range accounts {
		if riverIDForAccount(account) == riverID {
			return accountID
		}
	}
	return ""
}

func (s *Source) participantsJSON(conversationID string) (string, error) {
	participants, err := s.store.ListParticipants(conversationID)
	if err != nil {
		return "", fmt.Errorf("map conversation %q participants: %w", conversationID, err)
	}
	dtos := make([]participantDTO, 0, len(participants))
	for _, participant := range participants {
		identity, err := s.store.GetIdentity(participant.IdentityID)
		if err != nil {
			return "", fmt.Errorf(
				"map conversation %q participant %q: %w",
				conversationID,
				participant.IdentityID,
				err,
			)
		}
		name := strings.TrimSpace(participant.DisplayName)
		if name == "" {
			name = identity.DisplayName
		}
		dtos = append(dtos, participantDTO{
			Name:   name,
			Number: identity.CanonicalValue,
		})
	}
	encoded, err := json.Marshal(dtos)
	if err != nil {
		return "", fmt.Errorf("map conversation %q participants: %w", conversationID, err)
	}
	return string(encoded), nil
}

func (s *Source) mapMessages(
	messages []sqlite.Message,
) ([]*db.Message, error) {
	accounts, err := s.accountIndex()
	if err != nil {
		return nil, fmt.Errorf("map messages: %w", err)
	}
	messageIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.MessageID)
	}
	reactions, err := s.reactions.ReactionsForMessages(context.Background(), messageIDs)
	if err != nil {
		return nil, fmt.Errorf("map messages: load reactions: %w", err)
	}
	extras, err := s.store.MessageExtrasFor(context.Background(), messageIDs)
	if err != nil {
		return nil, fmt.Errorf("map messages: load extras: %w", err)
	}
	remoteByConversation := make(map[string]string)
	mapped := make([]*db.Message, 0, len(messages))
	for _, message := range messages {
		remoteConversationID, ok := remoteByConversation[message.ConversationID]
		if !ok {
			conversation, err := s.store.GetConversation(message.ConversationID)
			if err != nil {
				return nil, fmt.Errorf("map messages: load conversation %q: %w", message.ConversationID, err)
			}
			remoteConversationID = conversation.RemoteConversationID
			remoteByConversation[message.ConversationID] = remoteConversationID
		}
		dto, err := s.mapMessage(message, accounts, reactions[message.MessageID], extras[message.MessageID], remoteConversationID)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, dto)
	}
	return mapped, nil
}

func (s *Source) mapMessage(
	message sqlite.Message,
	accounts map[string]sqlite.Account,
	reactionRows []sqlite.ReactionRow,
	extra sqlite.MessageExtras,
	remoteConversationID string,
) (*db.Message, error) {
	account, ok := accounts[message.AccountID]
	if !ok {
		return nil, fmt.Errorf(
			"map message %q: account %q is missing",
			message.MessageID,
			message.AccountID,
		)
	}
	dto := &db.Message{
		MessageID:      message.MessageID,
		ConversationID: message.ConversationID,
		Body:           message.Body,
		TimestampMS:    message.OccurredAtMS,
		IsFromMe:       message.Direction == sqlite.MessageDirectionOutgoing,
		SourcePlatform: platformForBridgeKey(account.BridgeKey),
		SourceID:       message.RemoteMessageID,
	}
	reactionJSON, err := mapReactions(reactionRows)
	if err != nil {
		return nil, fmt.Errorf("map message %q reactions: %w", message.MessageID, err)
	}
	dto.Reactions = reactionJSON
	if message.SenderIdentityID != nil {
		identity, err := s.store.GetIdentity(*message.SenderIdentityID)
		if err != nil {
			return nil, fmt.Errorf(
				"map message %q sender %q: %w",
				message.MessageID,
				*message.SenderIdentityID,
				err,
			)
		}
		dto.SenderNumber = identity.CanonicalValue
		dto.SenderName = identity.DisplayName
	}
	if message.ReplyToRemoteID != nil {
		if reply := strings.TrimSpace(*message.ReplyToRemoteID); reply != "" {
			dto.ReplyToID = v2keys.MessageID(message.AccountID, remoteConversationID, reply)
		}
	}
	if dto.IsFromMe {
		status, err := s.sendStatusForMessage(message)
		if err != nil {
			return nil, err
		}
		dto.Status = status
	}
	attachment, ok, err := s.messageAttachment(context.Background(), message.MessageID)
	if err != nil {
		return nil, err
	}
	if ok {
		dto.MediaID = fmt.Sprintf("v2msg:%s:%d", message.MessageID, attachment.Ordinal)
		dto.MimeType = attachment.MIME
	}
	if rendered := slacklive.RenderBlocksJSON(extra.Payload.Blocks); rendered != "" {
		dto.Body = rendered
	}
	if actions := slacklive.InteractiveActions(extra.Payload.Blocks); len(actions) > 0 {
		link := slacklive.MessageDeepLink(slackTeamID(account.AccountID), remoteConversationID, message.RemoteMessageID)
		dto.BlockActions = make([]db.MessageAction, 0, len(actions))
		for _, action := range actions {
			item := db.MessageAction{
				Label: action.Label,
				Kind:  action.Kind,
				URL:   action.URL,
				Style: action.Style,
			}
			if item.URL == "" {
				item.URL = link
			}
			dto.BlockActions = append(dto.BlockActions, item)
		}
	}
	dto.Transcript = extra.Payload.Transcript
	dto.TranscriptModel = extra.Payload.TranscriptModel
	dto.TranscribedAtMS = extra.Payload.TranscribedAtMS
	return dto, nil
}

func mapReactions(rows []sqlite.ReactionRow) (string, error) {
	if len(rows) == 0 {
		return "", nil
	}
	type group struct {
		dto   reactionDTO
		first int64
		seen  map[string]struct{}
	}
	groups := map[string]*group{}
	for _, row := range rows {
		item := groups[row.Emoji]
		if item == nil {
			item = &group{dto: reactionDTO{Emoji: row.Emoji}, first: row.OccurredAtMS, seen: map[string]struct{}{}}
			groups[row.Emoji] = item
		}
		actor := row.ReactorLabel
		if row.ReactorIsSelf {
			actor = "me"
		} else if row.ReactorCanonical != "" {
			actor = row.ReactorCanonical
		}
		if actor != "" {
			if _, ok := item.seen[actor]; !ok {
				item.seen[actor] = struct{}{}
				item.dto.Actors = append(item.dto.Actors, actor)
			}
		}
		item.dto.Count++
	}
	ordered := make([]*group, 0, len(groups))
	for _, item := range groups {
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].first != ordered[j].first {
			return ordered[i].first < ordered[j].first
		}
		return ordered[i].dto.Emoji < ordered[j].dto.Emoji
	})
	dtos := make([]reactionDTO, 0, len(ordered))
	for _, item := range ordered {
		dtos = append(dtos, item.dto)
	}
	encoded, err := json.Marshal(dtos)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (s *Source) messageAttachment(
	ctx context.Context,
	messageID string,
) (sqlite.MessageAttachment, bool, error) {
	// R2 historical migration and every current decoder number attachments from
	// zero, so ordinal zero is the canonical legacy MediaID representative.
	attachment, err := s.attachments.GetForDownload(ctx, messageID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return sqlite.MessageAttachment{}, false, nil
	}
	if err != nil {
		return sqlite.MessageAttachment{}, false, fmt.Errorf(
			"map message %q attachment: %w",
			messageID,
			err,
		)
	}
	return attachment, true, nil
}

// sendStatusForMessage maps an outgoing message's most recent outbox delivery
// state onto the legacy status vocabulary the web UI already renders
// (sending/sent/failed). It is deliberately honest per the durable-send
// contract: queued/dispatching/not_dispatched/uncertain all read as "sending"
// — never "failed" — because none of them is a settled failure and an
// ambiguous send must not be shown as failed. store_failed reads "sent"
// (the transport delivered; only the local record needs repair). Terminal
// rejected/canceled read "failed". A message with no outbox row (received, or
// pre-outbox) carries no status.
func (s *Source) sendStatusForMessage(message sqlite.Message) (string, error) {
	if strings.TrimSpace(message.MessageID) == "" {
		return "", nil
	}
	state, ok, err := s.outbox.LatestStateForLocalMessage(
		context.Background(),
		message.AccountID,
		message.MessageID,
	)
	if err != nil {
		return "", fmt.Errorf("map message %q send status: %w", message.MessageID, err)
	}
	if !ok {
		return "", nil
	}
	switch state {
	case sqlite.OutboxQueued, sqlite.OutboxDispatching, sqlite.OutboxNotDispatched, sqlite.OutboxUncertain:
		return db.OutgoingSendStatusSending, nil
	case sqlite.OutboxConfirmed, sqlite.OutboxStoreFailed:
		return db.OutgoingSendStatusSent, nil
	case sqlite.OutboxRejected, sqlite.OutboxCanceled:
		return db.OutgoingSendStatusFailed, nil
	default:
		return "", nil
	}
}
