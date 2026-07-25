package db

import (
	"database/sql"
	"strings"
)

type SlackUser struct {
	RiverID     string
	UserID      string
	DisplayName string
	RealName    string
	IsDeleted   bool
	IsBot       bool
	UpdatedAtMS int64
}

func (u SlackUser) Name() string {
	if name := strings.TrimSpace(u.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(u.RealName); name != "" {
		return name
	}
	return strings.TrimSpace(u.UserID)
}

type SlackSyncCursor struct {
	RiverID   string
	ChannelID string
	OldestTS  string
	NewestTS  string
}

func (s *Store) ensureSlackTables() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS slack_users (
			river_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			real_name TEXT NOT NULL DEFAULT '',
			is_deleted INTEGER NOT NULL DEFAULT 0,
			is_bot INTEGER NOT NULL DEFAULT 0,
			updated_at_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (river_id, user_id)
		);
		CREATE TABLE IF NOT EXISTS slack_sync_cursors (
			river_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			oldest_ts TEXT NOT NULL DEFAULT '',
			newest_ts TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (river_id, channel_id)
		);
	`)
	return err
}

func (s *Store) UpsertSlackUser(u SlackUser) error {
	_, err := s.db.Exec(`
		INSERT INTO slack_users (river_id, user_id, display_name, real_name, is_deleted, is_bot, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(river_id, user_id) DO UPDATE SET
			display_name=excluded.display_name,
			real_name=excluded.real_name,
			is_deleted=excluded.is_deleted,
			is_bot=excluded.is_bot,
			updated_at_ms=excluded.updated_at_ms
	`, u.RiverID, u.UserID, u.DisplayName, u.RealName, u.IsDeleted, u.IsBot, u.UpdatedAtMS)
	return err
}

func (s *Store) GetSlackUser(riverID, userID string) (*SlackUser, error) {
	var u SlackUser
	err := s.db.QueryRow(`
		SELECT river_id, user_id, display_name, real_name, is_deleted, is_bot, updated_at_ms
		FROM slack_users WHERE river_id = ? AND user_id = ?
	`, riverID, userID).Scan(&u.RiverID, &u.UserID, &u.DisplayName, &u.RealName, &u.IsDeleted, &u.IsBot, &u.UpdatedAtMS)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) ListSlackUsers(riverID string) ([]SlackUser, error) {
	rows, err := s.db.Query(`
		SELECT river_id, user_id, display_name, real_name, is_deleted, is_bot, updated_at_ms
		FROM slack_users WHERE river_id = ?
	`, riverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []SlackUser
	for rows.Next() {
		var u SlackUser
		if err := rows.Scan(&u.RiverID, &u.UserID, &u.DisplayName, &u.RealName, &u.IsDeleted, &u.IsBot, &u.UpdatedAtMS); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpsertSlackConversation updates Slack-owned metadata without clobbering
// unread_count, favorites, notification mode, or tab state.
func (s *Store) UpsertSlackConversation(c *Conversation) error {
	_, err := s.db.Exec(`
		INSERT INTO conversations
			(conversation_id, name, is_group, participants, last_message_ts, unread_count, source_platform, river_id)
		VALUES (?, ?, ?, ?, ?, 0, 'slack', ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			name=excluded.name,
			is_group=excluded.is_group,
			participants=excluded.participants,
			last_message_ts=CASE WHEN excluded.last_message_ts > conversations.last_message_ts
				THEN excluded.last_message_ts ELSE conversations.last_message_ts END,
			source_platform='slack',
			river_id=excluded.river_id
	`, c.ConversationID, c.Name, c.IsGroup, c.Participants, c.LastMessageTS, c.RiverID)
	return err
}

func (s *Store) GetSlackSyncCursor(riverID, channelID string) (SlackSyncCursor, error) {
	var c SlackSyncCursor
	err := s.db.QueryRow(`
		SELECT river_id, channel_id, oldest_ts, newest_ts
		FROM slack_sync_cursors WHERE river_id = ? AND channel_id = ?
	`, riverID, channelID).Scan(&c.RiverID, &c.ChannelID, &c.OldestTS, &c.NewestTS)
	if err == sql.ErrNoRows {
		return SlackSyncCursor{RiverID: riverID, ChannelID: channelID}, nil
	}
	return c, err
}

func (s *Store) UpsertSlackSyncCursor(c SlackSyncCursor) error {
	_, err := s.db.Exec(`
		INSERT INTO slack_sync_cursors (river_id, channel_id, oldest_ts, newest_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(river_id, channel_id) DO UPDATE SET
			oldest_ts=CASE WHEN excluded.oldest_ts != '' THEN excluded.oldest_ts ELSE slack_sync_cursors.oldest_ts END,
			newest_ts=CASE WHEN excluded.newest_ts != '' THEN excluded.newest_ts ELSE slack_sync_cursors.newest_ts END
	`, c.RiverID, c.ChannelID, c.OldestTS, c.NewestTS)
	return err
}

func (s *Store) ListThreadMessages(conversationID, rootMessageID string) ([]*Message, error) {
	rows, err := s.db.Query(`
		SELECT `+messageColumns+`
		FROM messages
		WHERE conversation_id = ? AND (message_id = ? OR reply_to_id = ?)
		ORDER BY timestamp_ms ASC, message_id ASC
	`, conversationID, rootMessageID, rootMessageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}
