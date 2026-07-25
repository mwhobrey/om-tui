package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/river"
)

// River is persisted metadata for a connected account/workspace.
type River struct {
	ID          string
	Provider    string
	DisplayName string
	AccountKey  string
	Status      string
	CreatedAtMS int64
	LastActive  int64
}

func (s *Store) ensureRiversTable() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS rivers (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			account_key TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at_ms INTEGER NOT NULL DEFAULT 0,
			last_active_ms INTEGER NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_rivers_provider_account
			ON rivers(provider, account_key);
	`)
	return err
}

func (s *Store) UpsertRiver(r *River) error {
	if r == nil || strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("river id required")
	}
	if r.Provider == "" {
		r.Provider = river.ProviderMessages
	}
	if r.Status == "" {
		r.Status = river.StatusActive
	}
	if r.CreatedAtMS == 0 {
		r.CreatedAtMS = time.Now().UnixMilli()
	}
	if r.LastActive == 0 {
		r.LastActive = r.CreatedAtMS
	}
	_, err := s.db.Exec(`
		INSERT INTO rivers (id, provider, display_name, account_key, status, created_at_ms, last_active_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider=excluded.provider,
			display_name=excluded.display_name,
			account_key=excluded.account_key,
			status=excluded.status,
			last_active_ms=excluded.last_active_ms
	`, r.ID, r.Provider, r.DisplayName, r.AccountKey, r.Status, r.CreatedAtMS, r.LastActive)
	return err
}

func (s *Store) GetRiver(id string) (*River, error) {
	r := &River{}
	err := s.db.QueryRow(`
		SELECT id, provider, display_name, account_key, status, created_at_ms, last_active_ms
		FROM rivers WHERE id = ?
	`, id).Scan(&r.ID, &r.Provider, &r.DisplayName, &r.AccountKey, &r.Status, &r.CreatedAtMS, &r.LastActive)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) ListRivers() ([]*River, error) {
	rows, err := s.db.Query(`
		SELECT id, provider, display_name, account_key, status, created_at_ms, last_active_ms
		FROM rivers
		ORDER BY CASE provider WHEN 'messages' THEN 0 ELSE 1 END, display_name COLLATE NOCASE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*River
	for rows.Next() {
		r := &River{}
		if err := rows.Scan(&r.ID, &r.Provider, &r.DisplayName, &r.AccountKey, &r.Status, &r.CreatedAtMS, &r.LastActive); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRiver(id string) error {
	_, err := s.db.Exec(`DELETE FROM rivers WHERE id = ?`, id)
	return err
}

// EnsureMessagesRiver creates the built-in Messages river and backfills river_id.
func (s *Store) EnsureMessagesRiver() (*River, error) {
	existing, err := s.GetRiver(river.DefaultMessagesRiverID)
	if err != nil {
		return nil, err
	}
	r := river.NewMessagesRiver()
	row := &River{
		ID:          r.ID,
		Provider:    r.Provider,
		DisplayName: r.DisplayName,
		AccountKey:  r.AccountKey,
		Status:      r.Status,
		CreatedAtMS: r.CreatedAtMS,
		LastActive:  r.LastActive,
	}
	if existing != nil {
		row.CreatedAtMS = existing.CreatedAtMS
		row.DisplayName = existing.DisplayName
	}
	if err := s.UpsertRiver(row); err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`
		UPDATE conversations
		SET river_id = ?
		WHERE IFNULL(river_id, '') = ''
		  AND LOWER(IFNULL(source_platform, 'sms')) IN ('', 'sms', 'rcs')
	`, river.DefaultMessagesRiverID); err != nil {
		return nil, fmt.Errorf("backfill messages river_id: %w", err)
	}
	return row, nil
}

// UnreadCountsByRiver returns sum of conversation unread_count per river_id.
func (s *Store) UnreadCountsByRiver() (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT river_id, COALESCE(SUM(unread_count), 0)
		FROM conversations
		WHERE IFNULL(river_id, '') != ''
		GROUP BY river_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
