package v2read

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

// ListConversations returns v2 conversations in cross-account recency order.
func (s *Source) ListConversations(limit int) ([]*db.Conversation, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	conversations, err := s.store.ListConversationsByRecencyAllAccounts(limit)
	if err != nil {
		return nil, err
	}
	accounts, err := s.accountIndex()
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	mapped := make([]*db.Conversation, 0, len(conversations))
	for _, conversation := range conversations {
		dto, err := s.mapConversation(conversation, accounts)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, dto)
	}
	if err := s.applyUnread(mapped); err != nil {
		return nil, err
	}
	return mapped, nil
}

// ListConversationsByRiver returns v2 conversations for one river, mapped to
// the canonical legacy DTO. Slack rivers are v2 account IDs (`slack-<team>`).
func (s *Source) ListConversationsByRiver(riverID string, limit int) ([]*db.Conversation, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	accounts, err := s.accountIndex()
	if err != nil {
		return nil, fmt.Errorf("list conversations by river: %w", err)
	}
	accountID := accountIDForRiver(riverID, accounts)
	if accountID == "" {
		return []*db.Conversation{}, nil
	}
	conversations, err := s.store.ListConversationsByRecency(accountID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(conversations) > limit {
		conversations = conversations[:limit]
	}
	mapped := make([]*db.Conversation, 0, len(conversations))
	for _, conversation := range conversations {
		dto, err := s.mapConversation(conversation, accounts)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, dto)
	}
	if err := s.applyUnread(mapped); err != nil {
		return nil, err
	}
	return mapped, nil
}

// ListConversationsByPlatform returns v2 conversations for one source platform
// (sms, whatsapp, signal, slack, …), newest first across every matching account.
func (s *Source) ListConversationsByPlatform(platform string, limit int) ([]*db.Conversation, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		platform = "sms"
	}
	accounts, err := s.accountIndex()
	if err != nil {
		return nil, fmt.Errorf("list conversations by platform: %w", err)
	}
	collected := make([]sqlite.Conversation, 0)
	for _, account := range accounts {
		if platformForBridgeKey(account.BridgeKey) != platform {
			continue
		}
		rows, err := s.store.ListConversationsByRecency(account.AccountID)
		if err != nil {
			return nil, fmt.Errorf("list conversations by platform: %w", err)
		}
		collected = append(collected, rows...)
	}
	sort.Slice(collected, func(i, j int) bool {
		if collected[i].LastMessageAtMS != collected[j].LastMessageAtMS {
			return collected[i].LastMessageAtMS > collected[j].LastMessageAtMS
		}
		return collected[i].ConversationID < collected[j].ConversationID
	})
	if limit > 0 && len(collected) > limit {
		collected = collected[:limit]
	}
	mapped := make([]*db.Conversation, 0, len(collected))
	for _, conversation := range collected {
		dto, err := s.mapConversation(conversation, accounts)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, dto)
	}
	if err := s.applyUnread(mapped); err != nil {
		return nil, err
	}
	return mapped, nil
}

// SearchConversationsByName returns conversations whose title or participant
// identity matches query, newest first. riverID scopes the search to one
// account the same way SearchMessagesFiltered does.
func (s *Source) SearchConversationsByName(query, riverID string, limit int) ([]*db.Conversation, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return []*db.Conversation{}, nil
	}
	accounts, err := s.accountIndex()
	if err != nil {
		return nil, fmt.Errorf("search conversations by name: %w", err)
	}
	rows, err := s.store.SearchConversationsByName(query, s.accountIDForSearchRiver(riverID), limit)
	if err != nil {
		return nil, err
	}
	mapped := make([]*db.Conversation, 0, len(rows))
	for _, conversation := range rows {
		dto, err := s.mapConversation(conversation, accounts)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, dto)
	}
	if err := s.applyUnread(mapped); err != nil {
		return nil, err
	}
	return mapped, nil
}

// GetConversation returns one v2 conversation as the canonical legacy DTO.
func (s *Source) GetConversation(id string) (*db.Conversation, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	conversation, err := s.store.GetConversation(id)
	if errors.Is(err, sqlite.ErrNotFound) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	accounts, err := s.accountIndex()
	if err != nil {
		return nil, fmt.Errorf("get conversation %q: %w", id, err)
	}
	dto, err := s.mapConversation(conversation, accounts)
	if err != nil {
		return nil, err
	}
	if err := s.applyUnread([]*db.Conversation{dto}); err != nil {
		return nil, err
	}
	return dto, nil
}

func (s *Source) applyUnread(conversations []*db.Conversation) error {
	if len(conversations) == 0 {
		return nil
	}
	ids := make([]string, 0, len(conversations))
	for _, conversation := range conversations {
		if conversation != nil && conversation.ConversationID != "" {
			ids = append(ids, conversation.ConversationID)
		}
	}
	counts, err := s.store.IncomingUnreadCounts(ids)
	if err != nil {
		return fmt.Errorf("conversation unread counts: %w", err)
	}
	for _, conversation := range conversations {
		if conversation == nil {
			continue
		}
		conversation.UnreadCount = counts[conversation.ConversationID]
	}
	return nil
}
