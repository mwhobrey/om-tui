package v2read

import (
	"context"
	"errors"

	"github.com/maxghenis/openmessage/internal/db"
	"github.com/maxghenis/openmessage/internal/storage/sqlite"
)

// GetMessagesByConversation returns the latest v2 messages newest-first.
func (s *Source) GetMessagesByConversation(
	conversationID string,
	limit int,
) ([]*db.Message, error) {
	return s.getMessagesBefore(conversationID, 0, "", limit)
}

// GetMessagesByConversationBefore returns a newest-first cursor page.
func (s *Source) GetMessagesByConversationBefore(
	conversationID string,
	beforeMS int64,
	beforeID string,
	limit int,
) ([]*db.Message, error) {
	return s.getMessagesBefore(conversationID, beforeMS, beforeID, limit)
}

func (s *Source) getMessagesBefore(
	conversationID string,
	beforeMS int64,
	beforeID string,
	limit int,
) ([]*db.Message, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	messages, err := s.messages.ListMessagesByConversation(
		context.Background(), conversationID, beforeMS, beforeID, limit,
	)
	if err != nil {
		return nil, err
	}
	return s.mapMessages(messages)
}

// GetMessagesByConversationAfter returns an oldest-first cursor page.
func (s *Source) GetMessagesByConversationAfter(
	conversationID string,
	afterMS int64,
	afterID string,
	limit int,
) ([]*db.Message, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	messages, err := s.messages.ListMessagesByConversationAfter(
		context.Background(), conversationID, afterMS, afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	return s.mapMessages(messages)
}

// GetMessagesByConversations returns a newest-limited cross-conversation page,
// remapped oldest-first like the legacy person-history query.
func (s *Source) GetMessagesByConversations(conversationIDs []string, limit int) ([]*db.Message, error) {
	return s.messagesByConversations(conversationIDs, 0, 0, limit)
}

// GetMessagesByConversationsRange is the date-bounded person-history query.
func (s *Source) GetMessagesByConversationsRange(conversationIDs []string, afterMS, beforeMS int64, limit int) ([]*db.Message, error) {
	return s.messagesByConversations(conversationIDs, afterMS, beforeMS, limit)
}

func (s *Source) messagesByConversations(conversationIDs []string, afterMS, beforeMS int64, limit int) ([]*db.Message, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	messages, err := s.messages.ListMessagesByConversations(
		context.Background(), conversationIDs, afterMS, beforeMS, limit,
	)
	if err != nil {
		return nil, err
	}
	return s.mapMessages(messages)
}

// GetMessagesAroundMessage returns a chronological window including anchor.
func (s *Source) GetMessagesAroundMessage(
	conversationID string,
	messageID string,
	before int,
	after int,
) ([]*db.Message, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	messages, err := s.messages.ListMessagesAroundMessage(
		context.Background(), conversationID, messageID, before, after,
	)
	if errors.Is(err, sqlite.ErrNotFound) {
		return nil, db.ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.mapMessages(messages)
}

// GetMessageByID returns one mapped message, or nil if it is missing.
func (s *Source) GetMessageByID(messageID string) (*db.Message, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	message, err := s.messages.GetMessage(context.Background(), messageID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	mapped, err := s.mapMessages([]sqlite.Message{message})
	if err != nil {
		return nil, err
	}
	if len(mapped) == 0 {
		return nil, nil
	}
	return mapped[0], nil
}

func (s *Source) walkConversationMessages(
	conversationID string,
	visit func(sqlite.Message),
) error {
	var beforeMS int64
	var beforeID string
	for {
		page, err := s.messages.ListMessagesByConversation(
			context.Background(),
			conversationID,
			beforeMS,
			beforeID,
			sourceMessagePageSize,
		)
		if err != nil {
			return err
		}
		for _, message := range page {
			visit(message)
		}
		if len(page) < sourceMessagePageSize {
			return nil
		}
		last := page[len(page)-1]
		if last.OccurredAtMS == beforeMS && last.MessageID == beforeID {
			return errors.New("v2 read message pagination did not advance")
		}
		beforeMS = last.OccurredAtMS
		beforeID = last.MessageID
	}
}
