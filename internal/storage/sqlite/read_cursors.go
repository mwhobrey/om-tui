package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ReadCursor is the latest device-scoped read position for one conversation.
// LastReadMessageID may be nil for imported cursors that only approximate a
// position; M3 read-receipt submissions always provide it.
type ReadCursor struct {
	AccountID         string
	DeviceID          string
	ConversationID    string
	LastReadMessageID *string
	LastReadAtMS      int64
	SourceUpdatedAtMS *int64
	UpdatedAtMS       int64
}

// UpsertReadCursor advances a cursor monotonically by read time. An older
// write succeeds without changing the stored cursor; an equal time replaces
// the message position and update timestamp.
func (s *Store) UpsertReadCursor(cursor ReadCursor) error {
	if err := upsertReadCursor(
		context.Background(),
		s.db,
		cursor,
	); err != nil {
		return fmt.Errorf(
			"upsert read cursor for device %q and conversation %q: %w",
			cursor.DeviceID,
			cursor.ConversationID,
			err,
		)
	}
	return nil
}

func (s *Store) upsertReadCursorTx(
	ctx context.Context,
	tx *sql.Tx,
	cursor ReadCursor,
) error {
	if err := upsertReadCursor(ctx, tx, cursor); err != nil {
		return fmt.Errorf(
			"upsert read cursor for device %q and conversation %q: %w",
			cursor.DeviceID,
			cursor.ConversationID,
			err,
		)
	}
	return nil
}

type readCursorExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func upsertReadCursor(
	ctx context.Context,
	execer readCursorExecer,
	cursor ReadCursor,
) error {
	_, err := execer.ExecContext(ctx, `
		INSERT INTO read_cursors (
			account_id,
			device_id,
			conversation_id,
			last_read_message_id,
			last_read_at_ms,
			source_updated_at_ms,
			updated_at_ms
		) VALUES (?, ?, ?, ?, ?, NULL, ?)
		ON CONFLICT(device_id, conversation_id) DO UPDATE SET
			last_read_message_id = excluded.last_read_message_id,
			last_read_at_ms      = excluded.last_read_at_ms,
			updated_at_ms        = excluded.updated_at_ms
		WHERE excluded.last_read_at_ms >= read_cursors.last_read_at_ms
	`,
		cursor.AccountID,
		cursor.DeviceID,
		cursor.ConversationID,
		cursor.LastReadMessageID,
		cursor.LastReadAtMS,
		cursor.UpdatedAtMS,
	)
	if err != nil {
		return mapConstraintError(err)
	}
	return nil
}

// GetReadCursor returns the cursor for one device and conversation.
func (s *Store) GetReadCursor(deviceID, conversationID string) (ReadCursor, error) {
	var cursor ReadCursor
	err := s.db.QueryRowContext(context.Background(), `
		SELECT
			account_id,
			device_id,
			conversation_id,
			last_read_message_id,
			last_read_at_ms,
			source_updated_at_ms,
			updated_at_ms
		FROM read_cursors
		WHERE device_id = ? AND conversation_id = ?
	`, deviceID, conversationID).Scan(
		&cursor.AccountID,
		&cursor.DeviceID,
		&cursor.ConversationID,
		&cursor.LastReadMessageID,
		&cursor.LastReadAtMS,
		&cursor.SourceUpdatedAtMS,
		&cursor.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadCursor{}, notFound("read cursor", deviceID+"/"+conversationID)
	}
	if err != nil {
		return ReadCursor{}, fmt.Errorf(
			"get read cursor for device %q and conversation %q: %w",
			deviceID,
			conversationID,
			err,
		)
	}
	return cursor, nil
}

// IncomingUnreadCounts returns incoming messages newer than each conversation's
// local-installation read cursor. Conversations with no unread incoming rows
// are omitted (count 0).
func (s *Store) IncomingUnreadCounts(conversationIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(conversationIDs))
	if len(conversationIDs) == 0 {
		return counts, nil
	}
	placeholders := strings.Repeat("?,", len(conversationIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT m.conversation_id, COUNT(*)
		FROM messages AS m
		JOIN conversations AS c ON c.conversation_id = m.conversation_id
		JOIN devices AS d ON d.device_id = (
			SELECT d2.device_id
			FROM devices AS d2
			WHERE d2.account_id = c.account_id
			  AND d2.kind = 'local_installation'
			ORDER BY d2.is_current DESC, d2.device_id
			LIMIT 1
		)
		LEFT JOIN read_cursors AS rc
		  ON rc.device_id = d.device_id
		 AND rc.conversation_id = m.conversation_id
		WHERE m.direction = 'incoming'
		  AND m.occurred_at_ms > COALESCE(rc.last_read_at_ms, 0)
		  AND m.conversation_id IN (`+placeholders+`)
		GROUP BY m.conversation_id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("incoming unread counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var conversationID string
		var count int
		if err := rows.Scan(&conversationID, &count); err != nil {
			return nil, fmt.Errorf("incoming unread counts: %w", err)
		}
		counts[conversationID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("incoming unread counts: %w", err)
	}
	return counts, nil
}
