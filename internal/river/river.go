package river

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderMessages = "messages"
	ProviderSlack    = "slack"

	// DefaultMessagesRiverID is the built-in Google Messages river.
	DefaultMessagesRiverID = "messages-default"

	StatusActive = "active"
	StatusError  = "error"
)

// River is one connected account/workspace instance.
type River struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	AccountKey  string `json:"account_key"`
	Status      string `json:"status"`
	CreatedAtMS int64  `json:"created_at_ms"`
	LastActive  int64  `json:"last_active_ms"`
	UnreadCount int    `json:"unread_count,omitempty"`
}

func NormalizeProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case ProviderMessages, "sms", "rcs", "google", "gmessages":
		return ProviderMessages
	case ProviderSlack:
		return ProviderSlack
	default:
		return strings.ToLower(strings.TrimSpace(p))
	}
}

func SlackRiverID(teamID string) string {
	teamID = strings.TrimSpace(teamID)
	return "slack-" + teamID
}

func SlackConversationID(teamID, channelID string) string {
	return fmt.Sprintf("slack:%s:%s", strings.TrimSpace(teamID), strings.TrimSpace(channelID))
}

func ParseSlackConversationID(conversationID string) (teamID, channelID string, ok bool) {
	parts := strings.Split(conversationID, ":")
	if len(parts) != 3 || parts[0] != "slack" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func NewMessagesRiver() River {
	now := time.Now().UnixMilli()
	return River{
		ID:          DefaultMessagesRiverID,
		Provider:    ProviderMessages,
		DisplayName: "Messages",
		AccountKey:  "default",
		Status:      StatusActive,
		CreatedAtMS: now,
		LastActive:  now,
	}
}

func NewSlackRiver(teamID, teamName string) River {
	now := time.Now().UnixMilli()
	name := strings.TrimSpace(teamName)
	if name == "" {
		name = "Slack"
	}
	return River{
		ID:          SlackRiverID(teamID),
		Provider:    ProviderSlack,
		DisplayName: name,
		AccountKey:  strings.TrimSpace(teamID),
		Status:      StatusActive,
		CreatedAtMS: now,
		LastActive:  now,
	}
}
