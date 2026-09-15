package river

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	ProviderMessages = "messages"
	ProviderWhatsApp = "whatsapp"
	ProviderSignal   = "signal"
	ProviderSlack    = "slack"

	// DefaultMessagesRiverID is the built-in Google Messages river.
	DefaultMessagesRiverID = "messages-default"
	DefaultWhatsAppRiverID = "whatsapp-default"
	DefaultSignalRiverID   = "signal-default"

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
	case ProviderWhatsApp, "wa":
		return ProviderWhatsApp
	case ProviderSignal:
		return ProviderSignal
	case ProviderSlack:
		return ProviderSlack
	default:
		return strings.ToLower(strings.TrimSpace(p))
	}
}

// DefaultRiverID returns the built-in river for a source_platform, or "" when
// the platform is import-only / already river-keyed (Slack, gchat, iMessage).
func DefaultRiverID(sourcePlatform string) string {
	if strings.TrimSpace(sourcePlatform) == "" {
		return DefaultMessagesRiverID
	}
	switch NormalizeProvider(sourcePlatform) {
	case ProviderMessages:
		return DefaultMessagesRiverID
	case ProviderWhatsApp:
		return DefaultWhatsAppRiverID
	case ProviderSignal:
		return DefaultSignalRiverID
	default:
		return ""
	}
}

func IsDefaultRiverID(id string) bool {
	switch strings.TrimSpace(id) {
	case DefaultMessagesRiverID, DefaultWhatsAppRiverID, DefaultSignalRiverID:
		return true
	default:
		return false
	}
}

// SafeLiveRiverID reports whether id is a non-default extra WhatsApp/Signal
// river that cannot escape dataDir/rivers when joined as a path segment.
func SafeLiveRiverID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || IsDefaultRiverID(id) {
		return false
	}
	if filepath.Base(id) != id || strings.Contains(id, "..") {
		return false
	}
	switch {
	case strings.HasPrefix(id, "whatsapp-") && len(id) > len("whatsapp-"):
		return true
	case strings.HasPrefix(id, "signal-") && len(id) > len("signal-"):
		return true
	default:
		return false
	}
}

func ProviderPrefix(provider string) string {
	switch NormalizeProvider(provider) {
	case ProviderMessages:
		return "messages"
	case ProviderWhatsApp:
		return "whatsapp"
	case ProviderSignal:
		return "signal"
	case ProviderSlack:
		return "slack"
	default:
		return NormalizeProvider(provider)
	}
}

// NextExtraRiverID returns whatsapp-2, signal-2, … skipping IDs already in
// used. The built-in *-default rivers stay the first account.
func NextExtraRiverID(provider string, used []string) string {
	prefix := ProviderPrefix(provider) + "-"
	seen := make(map[string]struct{}, len(used))
	for _, id := range used {
		seen[strings.TrimSpace(id)] = struct{}{}
	}
	for n := 2; n < 10000; n++ {
		id := fmt.Sprintf("%s%d", prefix, n)
		if _, ok := seen[id]; !ok {
			return id
		}
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixMilli())
}

func ExtraRiverDisplayName(provider string, ordinal int) string {
	base := "Messages"
	switch NormalizeProvider(provider) {
	case ProviderWhatsApp:
		base = "WhatsApp"
	case ProviderSignal:
		base = "Signal"
	case ProviderSlack:
		base = "Slack"
	}
	if ordinal <= 1 {
		return base
	}
	return fmt.Sprintf("%s %d", base, ordinal)
}

func NewExtraRiver(provider, id, displayName string) River {
	now := time.Now().UnixMilli()
	provider = NormalizeProvider(provider)
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = ExtraRiverDisplayName(provider, 2)
	}
	return River{
		ID:          strings.TrimSpace(id),
		Provider:    provider,
		DisplayName: name,
		AccountKey:  strings.TrimSpace(id),
		Status:      StatusActive,
		CreatedAtMS: now,
		LastActive:  now,
	}
}

// SessionDir is <dataDir>/rivers/<id> for extra accounts. Built-in rivers keep
// the legacy files at the data-dir root (session.json, whatsapp-session.db,
// signal-cli/) so existing installs do not move.
func SessionDir(dataDir, riverID string) string {
	return filepath.Join(dataDir, "rivers", strings.TrimSpace(riverID))
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

func NewWhatsAppRiver() River {
	now := time.Now().UnixMilli()
	return River{
		ID:          DefaultWhatsAppRiverID,
		Provider:    ProviderWhatsApp,
		DisplayName: "WhatsApp",
		AccountKey:  "default",
		Status:      StatusActive,
		CreatedAtMS: now,
		LastActive:  now,
	}
}

func NewSignalRiver() River {
	now := time.Now().UnixMilli()
	return River{
		ID:          DefaultSignalRiverID,
		Provider:    ProviderSignal,
		DisplayName: "Signal",
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
