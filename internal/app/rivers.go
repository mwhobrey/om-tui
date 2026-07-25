package app

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/river"
	"github.com/maxghenis/openmessage/internal/vault"
)

// EnsureVault opens the credential vault for this data dir.
func (a *App) EnsureVault() (*vault.Store, error) {
	if a.Vault != nil {
		return a.Vault, nil
	}
	v, err := vault.New(a.DataDir)
	if err != nil {
		return nil, err
	}
	a.Vault = v
	return v, nil
}

// EnsureRivers creates the Messages river, migrates session.json into the vault
// when present, and returns the river list.
func (a *App) EnsureRivers() ([]*db.River, error) {
	if _, err := a.EnsureVault(); err != nil {
		return nil, err
	}
	if _, err := a.Store.EnsureMessagesRiver(); err != nil {
		return nil, err
	}
	if err := a.migrateMessagesSessionToVault(); err != nil {
		a.Logger.Warn().Err(err).Msg("Could not migrate Messages session into vault")
	}
	return a.Store.ListRivers()
}

func (a *App) migrateMessagesSessionToVault() error {
	if a.Vault == nil {
		return nil
	}
	if a.Vault.Exists(river.DefaultMessagesRiverID) {
		return nil
	}
	raw, err := os.ReadFile(a.SessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	blob, err := json.Marshal(map[string]any{
		"kind":       "google_session",
		"migrated":   time.Now().UnixMilli(),
		"session":    json.RawMessage(raw),
		"path_hint":  "session.json",
	})
	if err != nil {
		return err
	}
	if err := a.Vault.Put(river.DefaultMessagesRiverID, blob); err != nil {
		return fmt.Errorf("vault put messages session: %w", err)
	}
	a.Logger.Info().Msg("Migrated Google Messages session into river vault")
	return nil
}

// ListRiversWithUnread returns rivers annotated with unread stream counts.
func (a *App) ListRiversWithUnread() ([]river.River, error) {
	rows, err := a.EnsureRivers()
	if err != nil {
		return nil, err
	}
	unread, err := a.Store.UnreadCountsByRiver()
	if err != nil {
		return nil, err
	}
	out := make([]river.River, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		out = append(out, river.River{
			ID:          r.ID,
			Provider:    r.Provider,
			DisplayName: r.DisplayName,
			AccountKey:  r.AccountKey,
			Status:      r.Status,
			CreatedAtMS: r.CreatedAtMS,
			LastActive:  r.LastActive,
			UnreadCount: unread[r.ID],
		})
	}
	return out, nil
}
