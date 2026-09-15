package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
	"github.com/maxghenis/openmessage/internal/signallive"
	"github.com/maxghenis/openmessage/internal/whatsapplive"
)

func (a *App) whatsAppSessionPathFor(riverID string) string {
	if river.IsDefaultRiverID(riverID) || strings.TrimSpace(riverID) == "" {
		return a.WhatsAppSessionPath
	}
	dir, err := confinedRiverSessionDir(a.DataDir, riverID)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "whatsapp-session.db")
}

func (a *App) signalConfigPathFor(riverID string) string {
	if river.IsDefaultRiverID(riverID) || strings.TrimSpace(riverID) == "" {
		return a.SignalConfigPath
	}
	dir, err := confinedRiverSessionDir(a.DataDir, riverID)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "signal-cli")
}

func confinedRiverSessionDir(dataDir, riverID string) (string, error) {
	if !river.SafeLiveRiverID(riverID) {
		return "", fmt.Errorf("invalid river id")
	}
	root := filepath.Join(dataDir, "rivers")
	dir := filepath.Clean(river.SessionDir(dataDir, riverID))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid river session path")
	}
	return dir, nil
}

func (a *App) requireExtraLiveRiver(riverID, provider string) error {
	if !river.SafeLiveRiverID(riverID) {
		return fmt.Errorf("invalid river id")
	}
	if a.Store == nil {
		return fmt.Errorf("store unavailable")
	}
	row, err := a.Store.GetRiver(riverID)
	if err != nil {
		return err
	}
	if row == nil || river.NormalizeProvider(row.Provider) != provider {
		return fmt.Errorf("unknown %s river", provider)
	}
	return nil
}

// CreateLiveRiver registers an extra WhatsApp or Signal river.
// Slack still uses PairSlackRiver (team id is known at auth time).
// Google Messages stays a single live client (`messages-default`).
func (a *App) CreateLiveRiver(provider, displayName string) (river.River, error) {
	provider = river.NormalizeProvider(provider)
	switch provider {
	case river.ProviderWhatsApp, river.ProviderSignal:
	case river.ProviderMessages:
		return river.River{}, fmt.Errorf("Google Messages supports one live river (messages-default)")
	default:
		return river.River{}, fmt.Errorf("cannot add extra %s river from this command; use pair slack for workspaces", provider)
	}
	if _, err := a.EnsureRivers(); err != nil {
		return river.River{}, err
	}
	rows, err := a.Store.ListRivers()
	if err != nil {
		return river.River{}, err
	}
	used := make([]string, 0, len(rows))
	same := 1
	for _, r := range rows {
		if r == nil {
			continue
		}
		used = append(used, r.ID)
		if r.Provider == provider {
			same++
		}
	}
	id := river.NextExtraRiverID(provider, used)
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = river.ExtraRiverDisplayName(provider, same)
	}
	meta := river.NewExtraRiver(provider, id, name)
	row := &db.River{
		ID:          meta.ID,
		Provider:    meta.Provider,
		DisplayName: meta.DisplayName,
		AccountKey:  meta.AccountKey,
		Status:      river.StatusActive,
		CreatedAtMS: meta.CreatedAtMS,
		LastActive:  time.Now().UnixMilli(),
	}
	if err := a.Store.UpsertRiver(row); err != nil {
		return river.River{}, err
	}
	if err := os.MkdirAll(river.SessionDir(a.DataDir, row.ID), 0o700); err != nil {
		_ = a.Store.DeleteRiver(row.ID)
		return river.River{}, fmt.Errorf("create river session dir: %w", err)
	}
	return river.River{
		ID:          row.ID,
		Provider:    row.Provider,
		DisplayName: row.DisplayName,
		AccountKey:  row.AccountKey,
		Status:      row.Status,
		CreatedAtMS: row.CreatedAtMS,
		LastActive:  row.LastActive,
	}, nil
}

func (a *App) ensureWhatsAppRiver(riverID string) (*whatsapplive.Bridge, error) {
	if strings.TrimSpace(riverID) == "" {
		riverID = river.DefaultWhatsAppRiverID
	}
	if !river.IsDefaultRiverID(riverID) {
		if err := a.requireExtraLiveRiver(riverID, river.ProviderWhatsApp); err != nil {
			return nil, err
		}
	}
	a.whatsAppMu.Lock()
	defer a.whatsAppMu.Unlock()
	if a.WhatsAppRivers == nil {
		a.WhatsAppRivers = map[string]*whatsapplive.Bridge{}
	}
	if b := a.WhatsAppRivers[riverID]; b != nil {
		if riverID == river.DefaultWhatsAppRiverID {
			a.WhatsApp = b
		}
		return b, nil
	}
	if riverID == river.DefaultWhatsAppRiverID && a.WhatsApp != nil {
		a.WhatsAppRivers[riverID] = a.WhatsApp
		return a.WhatsApp, nil
	}
	sessionPath := a.whatsAppSessionPathFor(riverID)
	if sessionPath == "" {
		return nil, fmt.Errorf("invalid WhatsApp river session path")
	}
	bridge, err := whatsapplive.NewForRiver(riverID, sessionPath, a.Store, a.Logger, whatsapplive.Callbacks{
		OnConversationsChange: a.emitConversationsChange,
		OnIncomingMessage:     a.OnIncomingMessage,
		OnMessagesChange:      a.emitMessagesChange,
		OnConnectionError:     a.reportWhatsAppLifecycleError,
		OnStatusChange: func() {
			if a.OnWhatsAppStatusChange != nil {
				a.OnWhatsAppStatusChange()
			}
		},
		OnTypingChange: a.OnTypingChange,
	})
	if err != nil {
		return nil, err
	}
	a.WhatsAppRivers[riverID] = bridge
	if riverID == river.DefaultWhatsAppRiverID {
		a.WhatsApp = bridge
	}
	return bridge, nil
}

func (a *App) ensureSignalRiver(riverID string) (*signallive.Bridge, error) {
	if strings.TrimSpace(riverID) == "" {
		riverID = river.DefaultSignalRiverID
	}
	if !river.IsDefaultRiverID(riverID) {
		if err := a.requireExtraLiveRiver(riverID, river.ProviderSignal); err != nil {
			return nil, err
		}
	}
	a.signalMu.Lock()
	defer a.signalMu.Unlock()
	if a.SignalRivers == nil {
		a.SignalRivers = map[string]*signallive.Bridge{}
	}
	if b := a.SignalRivers[riverID]; b != nil {
		if riverID == river.DefaultSignalRiverID {
			a.Signal = b
		}
		return b, nil
	}
	if riverID == river.DefaultSignalRiverID && a.Signal != nil {
		a.SignalRivers[riverID] = a.Signal
		return a.Signal, nil
	}
	configDir := a.signalConfigPathFor(riverID)
	if configDir == "" {
		return nil, fmt.Errorf("invalid Signal river session path")
	}
	bridge, err := signallive.NewForRiver(riverID, configDir, a.Store, a.Logger, signallive.Callbacks{
		OnConversationsChange: a.emitConversationsChange,
		OnIncomingMessage:     a.OnIncomingMessage,
		OnMessagesChange:      a.emitMessagesChange,
		OnStatusChange: func() {
			if a.OnSignalStatusChange != nil {
				a.OnSignalStatusChange()
			}
		},
		OnTypingChange: a.OnTypingChange,
	})
	if err != nil {
		return nil, err
	}
	a.SignalRivers[riverID] = bridge
	if riverID == river.DefaultSignalRiverID {
		a.Signal = bridge
	}
	return bridge, nil
}

func (a *App) StartWhatsAppConnectRiver(riverID string) error {
	bridge, err := a.ensureWhatsAppRiver(riverID)
	if err != nil {
		return fmt.Errorf("init WhatsApp bridge: %w", err)
	}
	if err := bridge.Connect(); err != nil {
		return fmt.Errorf("connect WhatsApp bridge: %w", err)
	}
	return nil
}

func (a *App) StartSignalConnectRiver(riverID string) error {
	bridge, err := a.ensureSignalRiver(riverID)
	if err != nil {
		return fmt.Errorf("init Signal bridge: %w", err)
	}
	if err := bridge.Connect(); err != nil {
		return fmt.Errorf("connect Signal bridge: %w", err)
	}
	return nil
}

func (a *App) WhatsAppQRCodeRiver(riverID string) (whatsapplive.QRSnapshot, error) {
	bridge, err := a.ensureWhatsAppRiver(riverID)
	if err != nil {
		return whatsapplive.QRSnapshot{}, fmt.Errorf("init WhatsApp bridge: %w", err)
	}
	return bridge.QRCode()
}

func (a *App) SignalQRCodeRiver(riverID string) (signallive.QRSnapshot, error) {
	bridge, err := a.ensureSignalRiver(riverID)
	if err != nil {
		return signallive.QRSnapshot{}, fmt.Errorf("init Signal bridge: %w", err)
	}
	return bridge.QRCode()
}

func (a *App) WhatsAppStatusRiver(riverID string) whatsapplive.StatusSnapshot {
	bridge, err := a.ensureWhatsAppRiver(riverID)
	if err != nil {
		return whatsapplive.StatusSnapshot{LastError: err.Error()}
	}
	return bridge.Status()
}

func (a *App) SignalStatusRiver(riverID string) signallive.StatusSnapshot {
	bridge, err := a.ensureSignalRiver(riverID)
	if err != nil {
		return signallive.StatusSnapshot{LastError: err.Error()}
	}
	return bridge.Status()
}

// StartExtraLiveRivers connects non-default WhatsApp/Signal rivers that already
// have a session. The built-in *-default rivers stay on the serve supervisors.
func (a *App) StartExtraLiveRivers(ctx context.Context) {
	_ = ctx
	rows, err := a.Store.ListRivers()
	if err != nil {
		a.Logger.Warn().Err(err).Msg("List rivers for extra live start")
		return
	}
	for _, r := range rows {
		if r == nil || river.IsDefaultRiverID(r.ID) {
			continue
		}
		switch r.Provider {
		case river.ProviderWhatsApp:
			bridge, err := a.ensureWhatsAppRiver(r.ID)
			if err != nil {
				a.Logger.Warn().Err(err).Str("river_id", r.ID).Msg("Failed to init extra WhatsApp river")
				continue
			}
			if err := bridge.ConnectIfPaired(); err != nil {
				a.Logger.Warn().Err(err).Str("river_id", r.ID).Msg("Failed to connect extra WhatsApp river")
			}
		case river.ProviderSignal:
			bridge, err := a.ensureSignalRiver(r.ID)
			if err != nil {
				a.Logger.Warn().Err(err).Str("river_id", r.ID).Msg("Failed to init extra Signal river")
				continue
			}
			if err := bridge.ConnectIfPaired(); err != nil {
				a.Logger.Warn().Err(err).Str("river_id", r.ID).Msg("Failed to connect extra Signal river")
			}
		}
	}
}

func (a *App) closeLiveBridges() {
	a.whatsAppMu.Lock()
	waDefault := a.WhatsApp
	var waExtras []*whatsapplive.Bridge
	for id, b := range a.WhatsAppRivers {
		if b == nil || river.IsDefaultRiverID(id) || b == waDefault {
			continue
		}
		waExtras = append(waExtras, b)
	}
	a.whatsAppMu.Unlock()
	for _, b := range waExtras {
		if err := b.Close(); err != nil {
			a.Logger.Warn().Err(err).Msg("Failed to close extra WhatsApp bridge")
		}
	}
	if waDefault != nil {
		if err := waDefault.Close(); err != nil {
			a.Logger.Warn().Err(err).Msg("Failed to close WhatsApp bridge")
		}
	}

	a.signalMu.Lock()
	sigDefault := a.Signal
	var sigExtras []*signallive.Bridge
	for id, b := range a.SignalRivers {
		if b == nil || river.IsDefaultRiverID(id) || b == sigDefault {
			continue
		}
		sigExtras = append(sigExtras, b)
	}
	a.signalMu.Unlock()
	for _, b := range sigExtras {
		if err := b.Close(); err != nil {
			a.Logger.Warn().Err(err).Msg("Failed to close extra Signal bridge")
		}
	}
	if sigDefault != nil {
		if err := sigDefault.Close(); err != nil {
			a.Logger.Warn().Err(err).Msg("Failed to close Signal bridge")
		}
	}
}

func (a *App) ExtraWhatsAppStatuses() []whatsapplive.StatusSnapshot {
	a.whatsAppMu.Lock()
	defer a.whatsAppMu.Unlock()
	out := make([]whatsapplive.StatusSnapshot, 0, len(a.WhatsAppRivers))
	for id, b := range a.WhatsAppRivers {
		if b == nil || river.IsDefaultRiverID(id) {
			continue
		}
		snap := b.Status()
		snap.RiverID = id
		out = append(out, snap)
	}
	return out
}

func (a *App) ExtraSignalStatuses() []signallive.StatusSnapshot {
	a.signalMu.Lock()
	defer a.signalMu.Unlock()
	out := make([]signallive.StatusSnapshot, 0, len(a.SignalRivers))
	for id, b := range a.SignalRivers {
		if b == nil || river.IsDefaultRiverID(id) {
			continue
		}
		snap := b.Status()
		snap.RiverID = id
		out = append(out, snap)
	}
	return out
}

func (a *App) liveRiverIDForMessage(msg *db.Message, fallback string) string {
	if msg == nil || strings.TrimSpace(msg.ConversationID) == "" {
		return fallback
	}
	return a.liveRiverIDForConversation(msg.ConversationID)
}

func (a *App) liveRiverIDForConversation(conversationID string) string {
	if id := river.RiverIDFromScoped(conversationID); id != "" {
		return id
	}
	if a.Store != nil {
		if conv, err := a.Store.GetConversation(conversationID); err == nil && conv != nil && strings.TrimSpace(conv.RiverID) != "" {
			return conv.RiverID
		}
	}
	switch {
	case strings.HasPrefix(conversationID, "whatsapp"):
		return river.DefaultWhatsAppRiverID
	case strings.HasPrefix(conversationID, "signal"):
		return river.DefaultSignalRiverID
	default:
		return river.DefaultMessagesRiverID
	}
}
