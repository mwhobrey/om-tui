package v2wire

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/storage/sqlite"
	"github.com/maxghenis/openmessage/internal/v2keys"
)

// ConversationSpec is the minimum identity needed to mint or reuse a v2 thread.
type ConversationSpec struct {
	AccountID            string
	Platform             string
	RemoteConversationID string
	Title                string
	Group                bool
}

// EnsureConversation returns the v2 conversation for a remote thread, creating
// the account/device/conversation with ingest's DeriveID when missing.
func EnsureConversation(v2 *sqlite.Store, spec ConversationSpec) (sqlite.Conversation, error) {
	if v2 == nil {
		return sqlite.Conversation{}, errors.New("ensure conversation: v2 store is nil")
	}
	accountID := strings.TrimSpace(spec.AccountID)
	if accountID == "" {
		return sqlite.Conversation{}, errors.New("ensure conversation: account id is empty")
	}
	remoteID := v2keys.NormalizeRemoteConversationID(spec.Platform, spec.RemoteConversationID)
	if strings.TrimSpace(remoteID) == "" {
		return sqlite.Conversation{}, errors.New("ensure conversation: remote id is empty")
	}
	existing, err := v2.GetConversationByRemote(accountID, remoteID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sqlite.ErrNotFound) {
		return sqlite.Conversation{}, err
	}

	nowMS := time.Now().UnixMilli()
	if err := ensureLiveAccount(v2, accountID, nowMS); err != nil {
		return sqlite.Conversation{}, err
	}
	if err := ensureLocalDevice(v2, accountID, nowMS); err != nil {
		return sqlite.Conversation{}, err
	}

	kind := sqlite.ConversationKindDirect
	if spec.Group {
		kind = sqlite.ConversationKindGroup
	}
	conversationID := v2keys.DeriveID("conversation", accountID, remoteID)
	if err := v2.UpsertConversation(sqlite.Conversation{
		ConversationID:       conversationID,
		AccountID:            accountID,
		RemoteConversationID: remoteID,
		Kind:                 kind,
		Title:                strings.TrimSpace(spec.Title),
		NotificationMode:     sqlite.NotificationModeAll,
		MetadataJSON:         "{}",
		CreatedAtMS:          nowMS,
		UpdatedAtMS:          nowMS,
	}); err != nil {
		return sqlite.Conversation{}, fmt.Errorf("ensure conversation %q: %w", remoteID, err)
	}
	return v2.GetConversationByRemote(accountID, remoteID)
}

func ensureLiveAccount(v2 *sqlite.Store, accountID string, nowMS int64) error {
	if _, err := v2.GetAccount(accountID); err == nil {
		return nil
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return err
	}
	return v2.UpsertAccount(sqlite.Account{
		AccountID:   accountID,
		BridgeKey:   liveBridgeKey(accountID),
		DisplayName: accountID,
		Mode:        sqlite.AccountModeLive,
		Enabled:     true,
		ConfigJSON:  "{}",
		CreatedAtMS: nowMS,
		UpdatedAtMS: nowMS,
	})
}

func liveBridgeKey(accountID string) string {
	switch {
	case accountID == googleAccountID:
		return "google_messages"
	case accountID == whatsappAccountID || strings.HasPrefix(accountID, "whatsapp-"):
		return "whatsmeow"
	case accountID == signalAccountID || strings.HasPrefix(accountID, "signal-"):
		return "signal_cli"
	case strings.HasPrefix(accountID, "slack-"):
		return "slack_web"
	default:
		return "google_messages"
	}
}
