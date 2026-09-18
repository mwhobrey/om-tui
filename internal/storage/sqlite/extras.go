package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MessagePayload is the JSON object stored in message_extras.payload_json.
type MessagePayload struct {
	Blocks          json.RawMessage `json:"blocks,omitempty"`
	Transcript      string          `json:"transcript,omitempty"`
	TranscriptModel string          `json:"transcript_model,omitempty"`
	TranscribedAtMS int64           `json:"transcribed_at_ms,omitempty"`
}

// MessageExtras is one extras row plus the decoded payload.
type MessageExtras struct {
	MessageID   string
	Payload     MessagePayload
	UpdatedAtMS int64
}

// MessageExtrasFor returns extras keyed by message ID. Missing rows are omitted.
func (s *Store) MessageExtrasFor(ctx context.Context, messageIDs []string) (map[string]MessageExtras, error) {
	out := make(map[string]MessageExtras, len(messageIDs))
	if s == nil || s.db == nil || len(messageIDs) == 0 {
		return out, nil
	}
	const batch = 400
	for start := 0; start < len(messageIDs); start += batch {
		end := start + batch
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		chunk := messageIDs[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT message_id, payload_json, updated_at_ms
			FROM message_extras
			WHERE message_id IN (`+strings.Join(placeholders, ",")+`)
		`, args...)
		if err != nil {
			return nil, fmt.Errorf("list message extras: %w", err)
		}
		for rows.Next() {
			extra, err := scanMessageExtras(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out[extra.MessageID] = extra
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list message extras: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("list message extras: %w", err)
		}
	}
	return out, nil
}

// MergeMessagePayload inserts or merges payload fields for messageID.
func (s *Store) MergeMessagePayload(ctx context.Context, messageID string, patch MessagePayload) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return fmt.Errorf("merge message extras: message_id is empty")
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("merge message extras: store is nil")
	}
	nowMS := time.Now().UnixMilli()
	if nowMS <= 0 {
		return fmt.Errorf("merge message extras: current Unix time is not positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("merge message extras: begin: %w", err)
	}
	defer tx.Rollback()

	existing, err := getMessageExtrasTx(ctx, tx, messageID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	payload := MessagePayload{}
	if err == nil {
		payload = existing.Payload
	}
	mergeMessagePayload(&payload, patch)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("merge message extras: encode: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO message_extras (message_id, payload_json, updated_at_ms)
		VALUES (?, ?, ?)
		ON CONFLICT(message_id) DO UPDATE SET
			payload_json = excluded.payload_json,
			updated_at_ms = excluded.updated_at_ms
	`, messageID, string(encoded), nowMS); err != nil {
		return fmt.Errorf("merge message extras %q: %w", messageID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("merge message extras: commit: %w", err)
	}
	return nil
}

// SetMessageTranscript stores or clears a transcript on the extras row.
func (s *Store) SetMessageTranscript(ctx context.Context, messageID, transcript string, model *string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return fmt.Errorf("set message transcript: message_id is empty")
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("set message transcript: store is nil")
	}
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT message_id FROM messages WHERE message_id = ?`, messageID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("set message transcript: %w", err)
	}
	existing, err := s.MessageExtrasFor(ctx, []string{messageID})
	if err != nil {
		return err
	}
	current := existing[messageID].Payload
	modelToSave := current.TranscriptModel
	if model != nil {
		modelToSave = *model
	}
	nowMS := current.TranscribedAtMS
	if transcript == "" {
		if current.Transcript == "" && current.TranscribedAtMS == 0 && current.TranscriptModel == "" {
			return nil
		}
		return s.MergeMessagePayload(ctx, messageID, MessagePayload{})
	}
	if current.Transcript == transcript && current.TranscriptModel == modelToSave && current.TranscribedAtMS != 0 {
		return nil
	}
	nowMS = time.Now().UnixMilli()
	if nowMS <= current.TranscribedAtMS {
		nowMS = current.TranscribedAtMS + 1
	}
	return s.MergeMessagePayload(ctx, messageID, MessagePayload{
		Transcript:      transcript,
		TranscriptModel: modelToSave,
		TranscribedAtMS: nowMS,
	})
}

func getMessageExtrasTx(ctx context.Context, tx *sql.Tx, messageID string) (MessageExtras, error) {
	extra, err := scanMessageExtras(tx.QueryRowContext(ctx, `
		SELECT message_id, payload_json, updated_at_ms
		FROM message_extras
		WHERE message_id = ?
	`, messageID))
	if errors.Is(err, sql.ErrNoRows) {
		return MessageExtras{}, notFound("message extras", messageID)
	}
	if err != nil {
		return MessageExtras{}, fmt.Errorf("get message extras %q: %w", messageID, err)
	}
	return extra, nil
}

func scanMessageExtras(row rowScanner) (MessageExtras, error) {
	var extra MessageExtras
	var raw string
	if err := row.Scan(&extra.MessageID, &raw, &extra.UpdatedAtMS); err != nil {
		return MessageExtras{}, err
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &extra.Payload); err != nil {
			return MessageExtras{}, fmt.Errorf("decode extras %q: %w", extra.MessageID, err)
		}
	}
	return extra, nil
}

func mergeMessagePayload(dst *MessagePayload, patch MessagePayload) {
	if dst == nil {
		return
	}
	if len(patch.Blocks) > 0 {
		dst.Blocks = append(json.RawMessage(nil), patch.Blocks...)
	}
	if patch.Transcript != "" || patch.TranscribedAtMS != 0 || patch.TranscriptModel != "" {
		dst.Transcript = patch.Transcript
		dst.TranscriptModel = patch.TranscriptModel
		dst.TranscribedAtMS = patch.TranscribedAtMS
	}
	if patch.Transcript == "" && patch.TranscribedAtMS == 0 && patch.TranscriptModel == "" && len(patch.Blocks) == 0 {
		*dst = MessagePayload{Blocks: dst.Blocks}
	}
}
