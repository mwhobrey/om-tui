package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/maxghenis/openmessage/internal/localapi"
	"github.com/maxghenis/openmessage/internal/river"
)

const (
	newChatOverlayWidth = 64
	maxNewChatMatches   = 8
)

type newChatOverlay struct {
	open       bool
	submitting bool
	input      textinput.Model
	contacts   []localapi.Contact
	matches    []localapi.Contact
	cursor     int
	err        string
}

type newChatContactsMsg struct {
	contacts []localapi.Contact
	query    string
	err      error
}

type newChatCreatedMsg struct {
	conv localapi.CreatedConversation
	err  error
}

func (m Model) canStartNewChat() bool {
	if m.reactPalette || m.blockPalette {
		return false
	}
	_, ok := m.newChatPlatform()
	return ok
}

func (m Model) newChatPlatform() (string, bool) {
	switch m.activeRiverProvider() {
	case river.ProviderWhatsApp:
		return "whatsapp", true
	case river.ProviderSignal:
		return "signal", true
	case river.ProviderSlack:
		return "", false
	default:
		return "sms", true
	}
}

func looksLikePhoneNumber(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return false
		}
	}
	digits := digitsOnly(s)
	return len(digits) >= 7 && len(digits) <= 15
}

func (m Model) openNewChatOverlay() (tea.Model, tea.Cmd) {
	if !m.canStartNewChat() {
		m.err = "new chat is not available on this river"
		return m, nil
	}
	ti := textinput.New()
	ti.Placeholder = "Name or phone number"
	ti.CharLimit = 64
	ti.Prompt = "> "
	ti.Width = 48
	m.newChat = newChatOverlay{open: true, input: ti}
	m.newChat.syncMatches()
	m.compose.Blur()
	m.err = ""
	m.info = ""
	return m, tea.Batch(m.newChat.input.Focus(), m.loadNewChatContactsCmd())
}

func (m Model) closeNewChatOverlay() Model {
	m.newChat = newChatOverlay{}
	return m
}

func (p *newChatOverlay) syncMatches() {
	q := strings.TrimSpace(p.input.Value())
	matches := make([]localapi.Contact, 0, maxNewChatMatches)
	if looksLikePhoneNumber(q) {
		matches = append(matches, localapi.Contact{Name: "New chat", Number: q})
	}
	lq := strings.ToLower(q)
	for _, c := range p.contacts {
		if len(matches) >= maxNewChatMatches {
			break
		}
		if q != "" && !contactMatchesQuery(c, lq) {
			continue
		}
		if c.Number == "" && c.Name == "" {
			continue
		}
		matches = append(matches, c)
	}
	p.matches = matches
	if p.cursor >= len(p.matches) {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func contactMatchesQuery(c localapi.Contact, lq string) bool {
	if lq == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.Name), lq) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Number), lq) {
		return true
	}
	qd := digitsOnly(lq)
	return qd != "" && strings.Contains(digitsOnly(c.Number), qd)
}

func (m Model) loadNewChatContactsCmd() tea.Cmd {
	query := strings.TrimSpace(m.newChat.input.Value())
	return func() tea.Msg {
		if m.session == nil || m.session.Client == nil {
			return newChatContactsMsg{query: query}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		contacts, err := m.session.Client.ListContacts(ctx, query, 40)
		return newChatContactsMsg{contacts: contacts, query: query, err: err}
	}
}

func (m Model) applyNewChatContacts(msg newChatContactsMsg) (Model, tea.Cmd) {
	if !m.newChat.open {
		return m, nil
	}
	if msg.query != strings.TrimSpace(m.newChat.input.Value()) {
		return m, nil
	}
	if msg.err != nil {
		m.newChat.err = msg.err.Error()
		return m, nil
	}
	m.newChat.contacts = msg.contacts
	m.newChat.syncMatches()
	return m, nil
}

func (m Model) updateNewChatKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.newChat.submitting {
		if msg.String() == "esc" {
			return m.closeNewChatOverlay(), nil
		}
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.closeNewChatOverlay(), nil
	case "enter":
		return m.submitNewChat()
	case "down", "ctrl+n":
		if n := len(m.newChat.matches); n > 0 {
			m.newChat.cursor = (m.newChat.cursor + 1) % n
		}
		return m, nil
	case "up", "ctrl+p":
		if n := len(m.newChat.matches); n > 0 {
			m.newChat.cursor--
			if m.newChat.cursor < 0 {
				m.newChat.cursor = n - 1
			}
		}
		return m, nil
	}
	prev := strings.TrimSpace(m.newChat.input.Value())
	var cmd tea.Cmd
	m.newChat.input, cmd = m.newChat.input.Update(msg)
	m.newChat.syncMatches()
	next := strings.TrimSpace(m.newChat.input.Value())
	if next != prev {
		return m, tea.Batch(cmd, m.loadNewChatContactsCmd())
	}
	return m, cmd
}

func (m Model) submitNewChat() (tea.Model, tea.Cmd) {
	phone, err := m.selectedNewChatNumber()
	if err != nil {
		m.newChat.err = err.Error()
		return m, nil
	}
	platform, ok := m.newChatPlatform()
	if !ok {
		m.newChat.err = "new chat is not available on this river"
		return m, nil
	}
	m.newChat.submitting = true
	m.newChat.err = ""
	return m, m.createNewChatCmd(phone, platform)
}

func (m Model) selectedNewChatNumber() (string, error) {
	if len(m.newChat.matches) > 0 && m.newChat.cursor >= 0 && m.newChat.cursor < len(m.newChat.matches) {
		c := m.newChat.matches[m.newChat.cursor]
		if n := strings.TrimSpace(c.Number); n != "" {
			return n, nil
		}
	}
	q := strings.TrimSpace(m.newChat.input.Value())
	if looksLikePhoneNumber(q) {
		return q, nil
	}
	if q == "" {
		return "", fmt.Errorf("type a name or phone number")
	}
	return "", fmt.Errorf("no number for %q — pick a match or type a phone number", q)
}

func (m Model) createNewChatCmd(phone, platform string) tea.Cmd {
	return func() tea.Msg {
		if m.session == nil || m.session.Client == nil {
			return newChatCreatedMsg{err: fmt.Errorf("not attached to the local API daemon")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		created, err := m.session.Client.CreateConversation(ctx, phone, platform)
		return newChatCreatedMsg{conv: created, err: err}
	}
}

func (m Model) applyNewChatCreated(msg newChatCreatedMsg) (tea.Model, tea.Cmd) {
	if !m.newChat.open {
		return m, nil
	}
	if msg.err != nil {
		m.newChat.submitting = false
		m.newChat.err = msg.err.Error()
		return m, nil
	}
	id := strings.TrimSpace(msg.conv.ConversationID)
	if id == "" {
		m.newChat.submitting = false
		m.newChat.err = "daemon did not return a conversation"
		return m, nil
	}
	name := strings.TrimSpace(msg.conv.Name)
	if name == "" {
		name = id
	}
	m = m.closeNewChatOverlay()
	m.info = "Opened new chat"
	next, cmd := m.openConversation(id, name, "")
	opened := next.(Model)
	refresh := opened.refreshConversationsCmd()
	if cmd != nil {
		return opened, tea.Batch(cmd, refresh)
	}
	return opened, refresh
}

func (m Model) renderNewChatOverlay() string {
	width := newChatOverlayWidth
	if m.width > 0 && width > m.width-4 {
		width = m.width - 4
	}
	if width < 40 {
		width = 40
	}
	innerW := width - 2
	title := paintLine(accentBoldStyle, "New chat", innerW)
	rows := []string{title, ""}
	hint := "Google Messages · SMS/RCS"
	switch m.activeRiverProvider() {
	case river.ProviderWhatsApp:
		hint = "WhatsApp"
	case river.ProviderSignal:
		hint = "Signal"
	}
	rows = append(rows, paintLine(mutedStyle, hint, innerW))
	rows = append(rows, paintLine(lipgloss.NewStyle(), m.newChat.input.View(), innerW))
	rows = append(rows, "")
	if m.newChat.submitting {
		rows = append(rows, paintCenteredLine(mutedStyle, "Creating conversation…", innerW))
	} else if len(m.newChat.matches) == 0 {
		msg := "Type a phone number to start a chat."
		if strings.TrimSpace(m.newChat.input.Value()) != "" {
			msg = "No matches. Type a full phone number."
		}
		rows = append(rows, paintCenteredLine(mutedStyle, msg, innerW))
	} else {
		for i, c := range m.newChat.matches {
			label := strings.TrimSpace(c.Name)
			if label == "" {
				label = c.Number
			} else if c.Number != "" && c.Name != "New chat" {
				label = label + "  " + c.Number
			}
			style := lipgloss.NewStyle()
			if i == m.newChat.cursor {
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63"))
			}
			rows = append(rows, paintLine(style, truncateCells(label, innerW), innerW))
		}
	}
	if m.newChat.err != "" {
		rows = append(rows, "", paintLine(errStyle, truncateCells(m.newChat.err, innerW), innerW))
	}
	rows = append(rows, paintLine(mutedStyle, "enter open    esc close", innerW))
	body := strings.Join(rows, "\n")
	box := focusBorderStyle.Width(innerW).Render(body)
	return padViewBox(box, lipgloss.Width(box), lipgloss.Height(box))
}
