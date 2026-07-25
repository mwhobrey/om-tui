package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
	"github.com/maxghenis/openmessage/internal/slacklive"
)

// PairSlackRiver validates a user token, stores it in the vault, and registers the river.
func (a *App) PairSlackRiver(token, appToken, displayName string) (*db.River, error) {
	if _, err := a.EnsureVault(); err != nil {
		return nil, err
	}
	creds, err := slacklive.AuthTest(token)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(displayName); name != "" {
		creds.TeamName = name
	}
	appToken = strings.TrimSpace(appToken)
	if appToken != "" && !strings.HasPrefix(appToken, "xapp-") {
		return nil, fmt.Errorf("Slack app token must start with xapp-")
	}
	creds.AppToken = appToken
	rMeta := river.NewSlackRiver(creds.TeamID, creds.TeamName)
	blob, err := slacklive.MarshalCredentials(creds)
	if err != nil {
		return nil, err
	}
	if err := a.Vault.Put(rMeta.ID, blob); err != nil {
		return nil, fmt.Errorf("store slack credentials: %w", err)
	}
	row := &db.River{
		ID:          rMeta.ID,
		Provider:    rMeta.Provider,
		DisplayName: rMeta.DisplayName,
		AccountKey:  rMeta.AccountKey,
		Status:      river.StatusActive,
		CreatedAtMS: rMeta.CreatedAtMS,
		LastActive:  time.Now().UnixMilli(),
	}
	if existing, _ := a.Store.GetRiver(row.ID); existing != nil {
		row.CreatedAtMS = existing.CreatedAtMS
	}
	if err := a.Store.UpsertRiver(row); err != nil {
		return nil, err
	}
	if _, err := a.StartSlackRiver(context.Background(), row.ID); err != nil {
		a.Logger.Warn().Err(err).Str("river_id", row.ID).Msg("Slack river paired but live start failed")
	}
	return row, nil
}

// StartSlackRiver loads vault credentials and starts the live client.
func (a *App) StartSlackRiver(ctx context.Context, riverID string) (*slacklive.Client, error) {
	if _, err := a.EnsureVault(); err != nil {
		return nil, err
	}
	raw, err := a.Vault.Get(riverID)
	if err != nil {
		return nil, err
	}
	creds, err := slacklive.UnmarshalCredentials(raw)
	if err != nil {
		return nil, err
	}
	client, err := slacklive.New(a.Store, riverID, creds)
	if err != nil {
		return nil, err
	}
	client.SetOnChange(func(conversationID string) {
		a.emitMessagesChange(conversationID)
		a.emitConversationsChange()
	})
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	_ = client.SyncRecentMessages(ctx, 30)

	a.slackMu.Lock()
	if a.SlackRivers == nil {
		a.SlackRivers = map[string]*slacklive.Client{}
	}
	if prev := a.SlackRivers[riverID]; prev != nil {
		prev.Stop()
	}
	a.SlackRivers[riverID] = client
	a.slackMu.Unlock()

	if a.OnSlackStatusChange != nil {
		a.OnSlackStatusChange()
	}
	if a.OnConversationsChange != nil {
		a.OnConversationsChange()
	}
	return client, nil
}

// StartAllSlackRivers starts every registered Slack river.
func (a *App) StartAllSlackRivers(ctx context.Context) {
	rivers, err := a.Store.ListRivers()
	if err != nil {
		a.Logger.Warn().Err(err).Msg("List rivers for Slack start")
		return
	}
	for _, r := range rivers {
		if r == nil || r.Provider != river.ProviderSlack {
			continue
		}
		if _, err := a.StartSlackRiver(ctx, r.ID); err != nil {
			a.Logger.Warn().Err(err).Str("river_id", r.ID).Msg("Failed to start Slack river")
		} else {
			a.Logger.Info().Str("river_id", r.ID).Str("name", r.DisplayName).Msg("Slack river started")
		}
	}
}

// StopAllSlackRivers stops live Slack clients.
func (a *App) StopAllSlackRivers() {
	a.slackMu.Lock()
	defer a.slackMu.Unlock()
	for id, c := range a.SlackRivers {
		if c != nil {
			c.Stop()
		}
		delete(a.SlackRivers, id)
	}
}

// SendSlackText routes a text send through the river that owns the conversation.
func (a *App) SendSlackText(conversationID, body, replyToID string) (*db.Message, error) {
	conv, err := a.Store.GetConversation(conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, fmt.Errorf("conversation not found")
	}
	riverID := conv.RiverID
	if riverID == "" {
		if teamID, _, ok := river.ParseSlackConversationID(conversationID); ok {
			riverID = river.SlackRiverID(teamID)
		}
	}
	a.slackMu.Lock()
	client := a.SlackRivers[riverID]
	a.slackMu.Unlock()
	if client == nil {
		started, err := a.StartSlackRiver(context.Background(), riverID)
		if err != nil {
			return nil, fmt.Errorf("slack river not connected: %w", err)
		}
		client = started
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.SendText(ctx, conversationID, body, replyToID)
}

func (a *App) FetchSlackThread(conversationID, rootMessageID string) ([]*db.Message, error) {
	client, err := a.slackClientForConversation(conversationID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.FetchThread(ctx, conversationID, rootMessageID)
}

func (a *App) FetchOlderSlackHistory(conversationID string, limit int) ([]*db.Message, error) {
	client, err := a.slackClientForConversation(conversationID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.SyncOlderHistory(ctx, conversationID, limit)
}

func (a *App) slackClientForConversation(conversationID string) (*slacklive.Client, error) {
	conv, err := a.Store.GetConversation(conversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil || conv.SourcePlatform != "slack" {
		return nil, fmt.Errorf("not a Slack conversation")
	}
	riverID := conv.RiverID
	a.slackMu.Lock()
	client := a.SlackRivers[riverID]
	a.slackMu.Unlock()
	if client != nil {
		return client, nil
	}
	return a.StartSlackRiver(context.Background(), riverID)
}

// SlackStatusSnapshot summarizes Slack river connectivity for /api/status.
func (a *App) SlackStatusSnapshot() []map[string]any {
	a.slackMu.Lock()
	defer a.slackMu.Unlock()
	out := make([]map[string]any, 0, len(a.SlackRivers))
	for id, c := range a.SlackRivers {
		connected, lastErr := false, ""
		socketConfigured, socketConnected := false, false
		if c != nil {
			connected, lastErr = c.Status()
			socketConfigured, socketConnected = c.SocketStatus()
		}
		out = append(out, map[string]any{
			"river_id":          id,
			"connected":         connected,
			"last_error":        lastErr,
			"socket_configured": socketConfigured,
			"socket_connected":  socketConnected,
		})
	}
	return out
}
