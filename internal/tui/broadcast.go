package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

type broadcastSentMsg struct {
	ok      int
	failed  int
	lastErr string
}

func (m *Model) broadcastCount() int {
	return len(m.broadcastIDs)
}

func (m *Model) broadcastIDList() []string {
	if len(m.broadcastIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(m.broadcastIDs))
	for id := range m.broadcastIDs {
		ids = append(ids, id)
	}
	return ids
}

func (m *Model) toggleBroadcast(id, name string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if m.broadcastIDs == nil {
		m.broadcastIDs = make(map[string]string)
	}
	if _, ok := m.broadcastIDs[id]; ok {
		delete(m.broadcastIDs, id)
		if len(m.broadcastIDs) == 0 {
			m.broadcastIDs = nil
		}
		return
	}
	if name == "" {
		name = id
	}
	m.broadcastIDs[id] = name
}

func (m *Model) clearBroadcast() {
	m.broadcastIDs = nil
}

func (m *Model) syncComposePlaceholder() {
	n := m.broadcastCount()
	if n > 0 {
		m.compose.Placeholder = fmt.Sprintf("Message to %d chats…", n)
		return
	}
	m.compose.Placeholder = "Write a message…"
}

func (m *Model) restampConversationList() {
	items := m.list.Items()
	if len(items) == 0 {
		return
	}
	next := make([]list.Item, 0, len(items))
	selectedID := ""
	if cur, ok := m.list.SelectedItem().(convItem); ok {
		selectedID = cur.conv.ConversationID
	}
	for _, it := range items {
		ci, ok := it.(convItem)
		if !ok {
			continue
		}
		_, ci.selected = m.broadcastIDs[ci.conv.ConversationID]
		next = append(next, ci)
	}
	m.list.SetItems(next)
	if selectedID != "" {
		for i, it := range next {
			if it.(convItem).conv.ConversationID == selectedID {
				m.list.Select(i)
				break
			}
		}
	}
}

func (m Model) sendBroadcastCmd(ids []string, body string) tea.Cmd {
	client := m.session.Client
	return func() tea.Msg {
		if len(ids) == 0 {
			return errMsg{err: fmt.Errorf("no conversations selected")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(15*len(ids))*time.Second+30*time.Second)
		defer cancel()
		status, _, err := client.Status(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		ok, failed := 0, 0
		lastErr := ""
		for _, id := range ids {
			key, err := newIdempotencyKey()
			if err != nil {
				failed++
				lastErr = err.Error()
				continue
			}
			var sendErr error
			if status.V2Send || status.V2Primary {
				_, sendErr = client.SubmitText(ctx, localapi.TextSubmission{
					ConversationID: id,
					Body:           body,
					IdempotencyKey: key,
				})
			} else {
				_, sendErr = client.LegacySendText(ctx, id, body, "", key)
			}
			if sendErr != nil {
				failed++
				lastErr = sendErr.Error()
				continue
			}
			ok++
		}
		return broadcastSentMsg{ok: ok, failed: failed, lastErr: lastErr}
	}
}
