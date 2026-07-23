package notify

import (
	"regexp"
	"strings"
	"sync"

	"github.com/maxghenis/openmessage/internal/db"
)

const notificationHistoryCap = 256

type idHistory struct {
	mu    sync.Mutex
	seen  map[string]struct{}
	order []string
}

func newIDHistory() idHistory {
	return idHistory{seen: make(map[string]struct{})}
}

func (h *idHistory) remember(messageID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.seen[messageID]; exists {
		return false
	}
	h.seen[messageID] = struct{}{}
	h.order = append(h.order, messageID)
	if len(h.order) > notificationHistoryCap {
		evicted := h.order[0]
		h.order = h.order[1:]
		delete(h.seen, evicted)
	}
	return true
}

func notificationAllowed(store *db.Store, mentionNames []string, message *db.Message) bool {
	if message == nil || store == nil {
		return true
	}
	conversation, err := store.GetConversation(message.ConversationID)
	if err != nil || conversation == nil {
		return true
	}
	switch conversation.NotificationMode {
	case db.NotificationModeMuted:
		return false
	case db.NotificationModeMentions:
		return message.MentionsMe || bodyMentionsAnyName(message.Body, mentionNames)
	default:
		return true
	}
}

func notificationTitle(message *db.Message) string {
	title := strings.TrimSpace(message.SenderName)
	if title == "" {
		title = strings.TrimSpace(message.SenderNumber)
	}
	if title == "" {
		title = "OpenMessage"
	}
	return title
}

func notificationBody(message *db.Message) string {
	body := strings.TrimSpace(message.Body)
	if body != "" {
		return body
	}
	if message.MediaID != "" || message.MimeType != "" {
		return "Sent an attachment"
	}
	return "New message"
}

func mentionNamesForIdentity(identity string) []string {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var names []string
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if len(candidate) < 3 {
			return
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		names = append(names, candidate)
	}

	add(identity)
	fields := strings.Fields(identity)
	if len(fields) > 0 {
		add(fields[0])
	}
	return names
}

func bodyMentionsAnyName(body string, names []string) bool {
	body = strings.TrimSpace(body)
	if body == "" || len(names) == 0 {
		return false
	}
	for _, name := range names {
		pattern := `(?i)(^|[^a-z0-9])` + regexp.QuoteMeta(name) + `([^a-z0-9]|$)`
		if matched, _ := regexp.MatchString(pattern, body); matched {
			return true
		}
	}
	return false
}
