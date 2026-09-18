package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/db"
)

// Draft is one unsent composer body stored against a v2 conversation.
type Draft struct {
	DraftID        string
	ConversationID string
	Body           string
	CreatedAtMS    int64
	UpdatedAtMS    int64
}

// Tab is a user-created conversation folder. Inbox and archive are implicit.
type Tab struct {
	TabID       string
	Name        string
	Position    int
	CreatedAtMS int64
}

// ContactMeta is per-person CRM metadata keyed the same way as legacy contact_meta.
type ContactMeta struct {
	PersonKey    string
	DisplayName  string
	Tags         []string
	ReachOutDays int
	Summary      string
	SummaryAtMS  int64
	UpdatedAtMS  int64
}

type conversationMetadata struct {
	Tab string `json:"tab,omitempty"`
}

const (
	tabInbox   = ""
	tabArchive = "archive"
)

// UpsertDraft inserts or replaces a draft. Empty draftID mints one.
func (s *Store) UpsertDraft(ctx context.Context, draft Draft) (Draft, error) {
	if s == nil || s.db == nil {
		return Draft{}, fmt.Errorf("upsert draft: store is nil")
	}
	draft.ConversationID = strings.TrimSpace(draft.ConversationID)
	if draft.ConversationID == "" {
		return Draft{}, fmt.Errorf("upsert draft: conversation_id is empty")
	}
	nowMS := time.Now().UnixMilli()
	if draft.DraftID = strings.TrimSpace(draft.DraftID); draft.DraftID == "" {
		id, err := newRandomID("draft")
		if err != nil {
			return Draft{}, err
		}
		draft.DraftID = id
	}
	if draft.CreatedAtMS <= 0 {
		draft.CreatedAtMS = nowMS
	}
	draft.UpdatedAtMS = nowMS
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO drafts (draft_id, conversation_id, body, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(draft_id) DO UPDATE SET
			conversation_id = excluded.conversation_id,
			body = excluded.body,
			updated_at_ms = excluded.updated_at_ms
	`, draft.DraftID, draft.ConversationID, draft.Body, draft.CreatedAtMS, draft.UpdatedAtMS)
	if err != nil {
		return Draft{}, fmt.Errorf("upsert draft %q: %w", draft.DraftID, mapConstraintError(err))
	}
	return draft, nil
}

// ListDrafts returns drafts for a conversation, newest first.
func (s *Store) ListDrafts(ctx context.Context, conversationID string) ([]Draft, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("list drafts: store is nil")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT draft_id, conversation_id, body, created_at_ms, updated_at_ms
		FROM drafts
		WHERE conversation_id = ?
		ORDER BY created_at_ms DESC, draft_id DESC
	`, strings.TrimSpace(conversationID))
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}
	defer rows.Close()
	var drafts []Draft
	for rows.Next() {
		var draft Draft
		if err := rows.Scan(&draft.DraftID, &draft.ConversationID, &draft.Body, &draft.CreatedAtMS, &draft.UpdatedAtMS); err != nil {
			return nil, fmt.Errorf("list drafts: %w", err)
		}
		drafts = append(drafts, draft)
	}
	return drafts, rows.Err()
}

// GetDraft returns one draft or ErrNotFound.
func (s *Store) GetDraft(ctx context.Context, draftID string) (Draft, error) {
	var draft Draft
	err := s.db.QueryRowContext(ctx, `
		SELECT draft_id, conversation_id, body, created_at_ms, updated_at_ms
		FROM drafts WHERE draft_id = ?
	`, strings.TrimSpace(draftID)).Scan(
		&draft.DraftID, &draft.ConversationID, &draft.Body, &draft.CreatedAtMS, &draft.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, notFound("draft", draftID)
	}
	if err != nil {
		return Draft{}, fmt.Errorf("get draft %q: %w", draftID, err)
	}
	return draft, nil
}

// DeleteDraft removes a draft. Missing IDs succeed.
func (s *Store) DeleteDraft(ctx context.Context, draftID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM drafts WHERE draft_id = ?`, strings.TrimSpace(draftID))
	if err != nil {
		return fmt.Errorf("delete draft %q: %w", draftID, err)
	}
	return nil
}

// ListTabs returns custom tabs by position.
func (s *Store) ListTabs(ctx context.Context) ([]Tab, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tab_id, name, position, created_at_ms FROM tabs
		ORDER BY position ASC, created_at_ms ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list tabs: %w", err)
	}
	defer rows.Close()
	var tabs []Tab
	for rows.Next() {
		var tab Tab
		if err := rows.Scan(&tab.TabID, &tab.Name, &tab.Position, &tab.CreatedAtMS); err != nil {
			return nil, fmt.Errorf("list tabs: %w", err)
		}
		tabs = append(tabs, tab)
	}
	return tabs, rows.Err()
}

// CreateTab appends a custom tab.
func (s *Store) CreateTab(ctx context.Context, name string) (Tab, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Tab{}, fmt.Errorf("tab name is required")
	}
	id, err := newRandomID("tab")
	if err != nil {
		return Tab{}, err
	}
	var maxPos sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(position) FROM tabs`).Scan(&maxPos); err != nil {
		return Tab{}, fmt.Errorf("create tab: %w", err)
	}
	pos := 0
	if maxPos.Valid {
		pos = int(maxPos.Int64) + 1
	}
	nowMS := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO tabs (tab_id, name, position, created_at_ms) VALUES (?, ?, ?, ?)
	`, id, name, pos, nowMS); err != nil {
		return Tab{}, fmt.Errorf("create tab: %w", mapConstraintError(err))
	}
	return Tab{TabID: id, Name: name, Position: pos, CreatedAtMS: nowMS}, nil
}

// RenameTab updates a custom tab's name.
func (s *Store) RenameTab(ctx context.Context, tabID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tab name is required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tabs SET name = ? WHERE tab_id = ?`, name, strings.TrimSpace(tabID))
	if err != nil {
		return fmt.Errorf("rename tab %q: %w", tabID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rename tab %q: %w", tabID, err)
	}
	if rows == 0 {
		return notFound("tab", tabID)
	}
	return nil
}

// DeleteTab drops a custom tab and files its conversations back to inbox.
func (s *Store) DeleteTab(ctx context.Context, tabID string) error {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return fmt.Errorf("tab id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete tab: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := clearConversationTabTx(ctx, tx, tabID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tabs WHERE tab_id = ?`, tabID); err != nil {
		return fmt.Errorf("delete tab %q: %w", tabID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete tab %q: %w", tabID, err)
	}
	return nil
}

// SetConversationTab files a conversation under inbox, archive, or a custom tab.
func (s *Store) SetConversationTab(ctx context.Context, conversationID, tab string) error {
	tab, err := s.validateConversationTab(ctx, tab)
	if err != nil {
		return err
	}
	conversation, err := s.GetConversation(conversationID)
	if err != nil {
		return err
	}
	nowMS := conversationTouchMS(conversation)
	meta := decodeConversationMetadata(conversation.MetadataJSON)
	switch tab {
	case tabArchive:
		conversation.ArchivedAtMS = &nowMS
		meta.Tab = ""
	case tabInbox:
		conversation.ArchivedAtMS = nil
		meta.Tab = ""
	default:
		conversation.ArchivedAtMS = nil
		meta.Tab = tab
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode conversation metadata: %w", err)
	}
	conversation.MetadataJSON = string(encoded)
	conversation.UpdatedAtMS = nowMS
	return s.UpsertConversation(conversation)
}

func conversationTouchMS(conversation Conversation) int64 {
	nowMS := time.Now().UnixMilli()
	if conversation.CreatedAtMS > nowMS {
		nowMS = conversation.CreatedAtMS
	}
	if conversation.UpdatedAtMS > nowMS {
		nowMS = conversation.UpdatedAtMS
	}
	return nowMS
}

func (s *Store) validateConversationTab(ctx context.Context, tab string) (string, error) {
	tab = strings.TrimSpace(tab)
	switch tab {
	case tabInbox, tabArchive:
		return tab, nil
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM tabs WHERE tab_id = ?`, tab).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("unknown conversation tab %q", tab)
	}
	if err != nil {
		return "", err
	}
	return tab, nil
}

func clearConversationTabTx(ctx context.Context, tx *sql.Tx, tabID string) error {
	nowMS := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET metadata_json = json_remove(metadata_json, '$.tab'),
		    updated_at_ms = max(updated_at_ms, created_at_ms, ?)
		WHERE json_extract(metadata_json, '$.tab') = ?
	`, nowMS, tabID)
	if err != nil {
		return fmt.Errorf("clear tab %q: %w", tabID, err)
	}
	return nil
}

// ConversationTab returns the v1-compatible tab id for a v2 conversation.
func ConversationTab(conversation Conversation) string {
	if conversation.ArchivedAtMS != nil {
		return tabArchive
	}
	return decodeConversationMetadata(conversation.MetadataJSON).Tab
}

func decodeConversationMetadata(raw string) conversationMetadata {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return conversationMetadata{}
	}
	var meta conversationMetadata
	_ = json.Unmarshal([]byte(raw), &meta)
	return meta
}

// GetContactMeta returns saved CRM metadata, or a zero-value row.
func (s *Store) GetContactMeta(ctx context.Context, personKey string) (ContactMeta, error) {
	meta := ContactMeta{PersonKey: personKey, Tags: []string{}}
	var tagsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT display_name, tags_json, reach_out_days, summary, summary_at_ms, updated_at_ms
		FROM contact_meta WHERE person_key = ?
	`, personKey).Scan(&meta.DisplayName, &tagsJSON, &meta.ReachOutDays, &meta.Summary, &meta.SummaryAtMS, &meta.UpdatedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return meta, nil
	}
	if err != nil {
		return ContactMeta{}, fmt.Errorf("get contact meta %q: %w", personKey, err)
	}
	meta.Tags = parseContactTags(tagsJSON)
	return meta, nil
}

// GetContactMetaMap loads all CRM rows keyed by person_key.
func (s *Store) GetContactMetaMap(ctx context.Context) (map[string]ContactMeta, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT person_key, display_name, tags_json, reach_out_days, summary, summary_at_ms, updated_at_ms
		FROM contact_meta
	`)
	if err != nil {
		return nil, fmt.Errorf("list contact meta: %w", err)
	}
	defer rows.Close()
	out := map[string]ContactMeta{}
	for rows.Next() {
		var meta ContactMeta
		var tagsJSON string
		if err := rows.Scan(&meta.PersonKey, &meta.DisplayName, &tagsJSON, &meta.ReachOutDays, &meta.Summary, &meta.SummaryAtMS, &meta.UpdatedAtMS); err != nil {
			return nil, fmt.Errorf("list contact meta: %w", err)
		}
		meta.Tags = parseContactTags(tagsJSON)
		out[meta.PersonKey] = meta
	}
	return out, rows.Err()
}

// SetContactTags replaces the tag list for a person.
func (s *Store) SetContactTags(ctx context.Context, personKey, displayName string, tags []string) error {
	if err := s.ensureContactMeta(ctx, personKey, displayName); err != nil {
		return err
	}
	encoded, _ := json.Marshal(cleanContactTags(tags))
	_, err := s.db.ExecContext(ctx, `
		UPDATE contact_meta SET tags_json = ?, updated_at_ms = ? WHERE person_key = ?
	`, string(encoded), time.Now().UnixMilli(), personKey)
	return err
}

// SetContactReachOut sets the reach-out cadence in days. 0 disables it.
func (s *Store) SetContactReachOut(ctx context.Context, personKey, displayName string, days int) error {
	if days < 0 {
		days = 0
	}
	if err := s.ensureContactMeta(ctx, personKey, displayName); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE contact_meta SET reach_out_days = ?, updated_at_ms = ? WHERE person_key = ?
	`, days, time.Now().UnixMilli(), personKey)
	return err
}

// SetContactSummary caches a generated relationship summary.
func (s *Store) SetContactSummary(ctx context.Context, personKey, displayName, summary string, atMS int64) error {
	if err := s.ensureContactMeta(ctx, personKey, displayName); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE contact_meta SET summary = ?, summary_at_ms = ?, updated_at_ms = ? WHERE person_key = ?
	`, summary, atMS, time.Now().UnixMilli(), personKey)
	return err
}

func (s *Store) ensureContactMeta(ctx context.Context, personKey, displayName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO contact_meta (person_key, display_name, updated_at_ms)
		VALUES (?, ?, ?)
		ON CONFLICT(person_key) DO UPDATE SET
			display_name = CASE WHEN excluded.display_name != '' THEN excluded.display_name ELSE contact_meta.display_name END
	`, personKey, displayName, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("ensure contact meta %q: %w", personKey, err)
	}
	return nil
}

func parseContactTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}

func cleanContactTags(tags []string) []string {
	clean := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		clean = append(clean, tag)
	}
	return clean
}

func newRandomID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}

// DraftDTO maps a v2 draft onto the legacy API shape.
func DraftDTO(draft Draft) *db.Draft {
	return &db.Draft{
		DraftID:        draft.DraftID,
		ConversationID: draft.ConversationID,
		Body:           draft.Body,
		CreatedAt:      draft.CreatedAtMS,
	}
}

// TabDTO maps a v2 tab onto the legacy API shape.
func TabDTO(tab Tab) *db.Tab {
	return &db.Tab{
		TabID:     tab.TabID,
		Name:      tab.Name,
		Position:  tab.Position,
		CreatedAt: tab.CreatedAtMS,
	}
}

// ContactMetaDTO maps v2 CRM onto the legacy API shape.
func ContactMetaDTO(meta ContactMeta) *db.ContactMeta {
	tags := meta.Tags
	if tags == nil {
		tags = []string{}
	}
	return &db.ContactMeta{
		PersonKey:    meta.PersonKey,
		DisplayName:  meta.DisplayName,
		Tags:         tags,
		ReachOutDays: meta.ReachOutDays,
		Summary:      meta.Summary,
		SummaryAt:    meta.SummaryAtMS,
		UpdatedAt:    meta.UpdatedAtMS,
	}
}
