package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
	"github.com/maxghenis/openmessage/internal/river"
)

const (
	historyPageSize = 100
	// historyMaxLoaded bounds the in-memory thread. The messages endpoint serves
	// at most 1000 per request and live refreshes re-fetch the loaded window, so
	// going past it would make every refresh drop the oldest rows. Use
	// ">export" for anything longer.
	historyMaxLoaded = 1000
)

// History is read in two tiers. Scrolling up first pages the local store; when
// that runs dry on an SMS/RCS thread, the daemon is asked to pull older
// messages from the phone into the store, and the store is read again. Slack
// has its own live fetch and bypasses all of this.

// olderMessagesMsg carries one page of older history from the store, oldest-first.
type olderMessagesMsg struct {
	conversationID string
	generation     uint64
	messages       []localapi.Message
}

// olderErrMsg is a failed older-page fetch (store or phone). It is separate
// from errMsg so the loading flag is cleared and a later scroll can retry.
type olderErrMsg struct {
	conversationID string
	generation     uint64
	err            error
}

// phoneOlderMsg is the result of a phone-side older-history fetch.
type phoneOlderMsg struct {
	conversationID string
	generation     uint64
	result         localapi.OlderPhoneResult
}

// loadedWindow is how many messages a live refresh should re-fetch so scrolling
// back through history isn't undone by the next incoming message.
func (m Model) loadedWindow() int {
	n := len(m.messages)
	if n < historyPageSize {
		return historyPageSize
	}
	if n > historyMaxLoaded {
		return historyMaxLoaded
	}
	return n
}

// phoneCapable reports whether the open conversation's history can be pulled
// from the phone: Google Messages threads only.
func (m Model) phoneCapable() bool {
	if m.activeRiverProvider() != river.ProviderMessages {
		return false
	}
	for _, c := range m.paletteConvs {
		if c.ConversationID == m.activeID {
			return c.SourcePlatform == "" || c.SourcePlatform == "sms"
		}
	}
	return true
}

// historyDone reports that neither the store nor the phone has anything older.
func (m Model) historyDone() bool {
	return m.localExhausted && (m.phoneExhausted || !m.phoneCapable())
}

// loadOlderHistory brings in the next page of older messages, from the store if
// it has any, otherwise from the phone.
func (m Model) loadOlderHistory() (tea.Model, tea.Cmd) {
	if m.activeID == "" || m.threadRootID != "" || m.activeRiverProvider() == river.ProviderSlack {
		return m, nil
	}
	if m.historyDone() {
		m.info = "Start of history"
		return m, nil
	}
	if m.olderLoading || len(m.messages) == 0 {
		return m, nil
	}
	if len(m.messages) >= historyMaxLoaded {
		m.info = fmt.Sprintf("%d messages loaded — use >export for the rest", historyMaxLoaded)
		return m, nil
	}
	oldest := m.messages[0]
	if oldest.TimestampMS <= 0 {
		return m, nil
	}
	m.olderLoading = true
	if m.localExhausted {
		m.info = "Fetching older messages from your phone…"
		return m, m.fetchPhoneOlderCmd(m.activeID, m.msgGeneration, oldest)
	}
	m.info = "Loading older messages…"
	return m, m.fetchOlderMessagesCmd(m.activeID, m.msgGeneration, oldest)
}

func (m Model) fetchOlderMessagesCmd(conversationID string, generation uint64, oldest localapi.Message) tea.Cmd {
	client := m.session.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		msgs, err := client.ConversationMessagesBefore(ctx, conversationID, oldest.TimestampMS, oldest.MessageID, historyPageSize)
		if err != nil {
			return olderErrMsg{conversationID: conversationID, generation: generation, err: err}
		}
		// API returns newest-first; threads render oldest→newest.
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		return olderMessagesMsg{conversationID: conversationID, generation: generation, messages: msgs}
	}
}

func (m Model) fetchPhoneOlderCmd(conversationID string, generation uint64, oldest localapi.Message) tea.Cmd {
	client := m.session.Client
	return func() tea.Msg {
		// The daemon may walk many pages on the phone before it reaches the cutoff.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := client.FetchOlderPhoneHistory(ctx, conversationID, oldest.TimestampMS)
		if err != nil {
			return olderErrMsg{conversationID: conversationID, generation: generation, err: err}
		}
		return phoneOlderMsg{conversationID: conversationID, generation: generation, result: res}
	}
}

// mergeOlder prepends page (oldest-first) to msgs, skipping ids already loaded.
func mergeOlder(msgs, page []localapi.Message) []localapi.Message {
	have := make(map[string]struct{}, len(msgs))
	for _, m := range msgs {
		if m.MessageID != "" {
			have[m.MessageID] = struct{}{}
		}
	}
	out := make([]localapi.Message, 0, len(page)+len(msgs))
	for _, p := range page {
		if _, dup := have[p.MessageID]; dup && p.MessageID != "" {
			continue
		}
		out = append(out, p)
	}
	return append(out, msgs...)
}

func (m Model) handleOlderMessages(msg olderMessagesMsg) (tea.Model, tea.Cmd) {
	if msg.conversationID != m.activeID || msg.generation != m.msgGeneration {
		return m, nil
	}
	m.olderLoading = false
	merged := mergeOlder(m.messages, msg.messages)
	added := len(merged) - len(m.messages)
	if len(msg.messages) < historyPageSize || added == 0 {
		m.localExhausted = true
	}
	if added == 0 {
		// The store is dry. Go straight to the phone if it can help.
		if m.historyDone() {
			m.info = "Start of history"
			return m, nil
		}
		return m.loadOlderHistory()
	}
	selectedID := ""
	if sel, ok := selectedMessage(m.messages, m.selectedMsg); ok {
		selectedID = sel.MessageID
	}
	m.messages = merged
	m.selectedMsg = indexMessageByID(m.messages, selectedID)
	if m.selectedMsg < 0 {
		m.selectedMsg = clampMessageIndex(len(m.messages), added)
	}
	m.setThreadContent(m.renderActiveThread())
	m.info = fmt.Sprintf("Loaded %d older message(s)", added)
	return m, nil
}

func (m Model) handlePhoneOlder(msg phoneOlderMsg) (tea.Model, tea.Cmd) {
	if msg.conversationID != m.activeID || msg.generation != m.msgGeneration {
		return m, nil
	}
	m.olderLoading = false
	if msg.result.Exhausted {
		m.phoneExhausted = true
	}
	if msg.result.Fetched > 0 {
		// The phone wrote to the store: read it back.
		m.localExhausted = false
		return m.loadOlderHistory()
	}
	if m.historyDone() {
		m.info = "Start of history"
	} else {
		m.info = "Nothing new from your phone yet — scroll up again to keep looking"
	}
	return m, nil
}

func (m Model) handleOlderErr(msg olderErrMsg) (tea.Model, tea.Cmd) {
	if msg.conversationID != m.activeID || msg.generation != m.msgGeneration {
		return m, nil
	}
	m.olderLoading = false
	m.err = "older messages: " + msg.err.Error()
	return m, nil
}
