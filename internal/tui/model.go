package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/maxghenis/openmessage/internal/localapi"
)

type focusPane int

const (
	focusList focusPane = iota
	focusThread
	focusCompose
	focusSearch
)

type convItem struct {
	conv     localapi.Conversation
	selected bool
}

func (i convItem) Title() string {
	name := strings.TrimSpace(i.conv.Name)
	if name == "" {
		name = i.conv.ConversationID
	}
	if i.conv.UnreadCount > 0 {
		name = fmt.Sprintf("%s (%d)", name, i.conv.UnreadCount)
	}
	if i.selected {
		return "* " + name
	}
	return "  " + name
}

func (i convItem) Description() string {
	preview := strings.TrimSpace(i.conv.LastMessagePreview)
	if preview == "" {
		return i.conv.ConversationID
	}
	return truncate(preview, 60)
}

func (i convItem) FilterValue() string {
	return i.conv.Name + " " + i.conv.ConversationID
}

type searchItem struct {
	hit localapi.SearchHit
}

func (i searchItem) Title() string {
	name := strings.TrimSpace(i.hit.Name)
	if name == "" {
		name = i.hit.ConversationID
	}
	return name
}

func (i searchItem) Description() string {
	return truncate(strings.TrimSpace(i.hit.Preview), 80)
}

func (i searchItem) FilterValue() string {
	return i.hit.Name + " " + i.hit.Preview
}

type (
	statusMsg        localapi.DaemonStatus
	conversationsMsg []localapi.Conversation
	messagesMsg      struct {
		conversationID string
		generation     uint64
		messages       []localapi.Message
	}
	searchMsg      []localapi.SearchHit
	errMsg         struct{ err error }
	streamEventMsg localapi.StreamEvent
	sentMsg        struct{ conversationID string }
	markedReadMsg  string
	reconnectMsg   localapi.DaemonStatus
	mediaDoneMsg   struct {
		action string // "open" or "save"
		path   string
	}
	reactDoneMsg struct {
		conversationID string
		emoji          string
		action         string
	}
)

// Model is the Bubble Tea TUI root.
type Model struct {
	session *Session
	width   int
	height  int
	focus   focusPane

	status localapi.DaemonStatus
	err    string
	info   string

	list     list.Model
	search   list.Model
	viewport viewport.Model
	compose  textarea.Model
	query    textinput.Model

	activeID           string
	activeName         string
	activeParticipants string
	messages           []localapi.Message
	msgGeneration      uint64
	viewportWidth      int
	composeHeight      int
	drafts             map[string]string
	broadcastIDs       map[string]string // conversationID -> display name
	selectedMsg        int               // index into messages; clamped to latest when out of range
	reactPalette       bool              // thread-focus emoji picker open

	sseCancel context.CancelFunc
	events    <-chan localapi.StreamEvent
	ready     bool
}

// NewModel builds the initial TUI model bound to a daemon session.
func NewModel(session *Session) Model {
	delegate := list.NewDefaultDelegate()
	convList := list.New(nil, delegate, 20, 20)
	convList.Title = "Conversations"
	convList.SetShowHelp(false)
	convList.SetFilteringEnabled(false)
	convList.DisableQuitKeybindings()

	searchList := list.New(nil, delegate, 20, 10)
	searchList.Title = "Search"
	searchList.SetShowHelp(false)
	searchList.SetFilteringEnabled(false)
	searchList.DisableQuitKeybindings()

	compose := textarea.New()
	compose.Placeholder = "Write a message…"
	compose.CharLimit = 4000
	compose.Prompt = "> "
	compose.ShowLineNumbers = false
	compose.SetHeight(3)
	compose.MaxHeight = 6
	compose.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	compose.BlurredStyle.Prompt = mutedStyle

	query := textinput.New()
	query.Placeholder = "Search messages…"
	query.CharLimit = 200
	query.Prompt = "/ "

	vp := viewport.New(40, 10)
	vp.SetContent("Select a conversation.")

	return Model{
		session:       session,
		focus:         focusList,
		list:          convList,
		search:        searchList,
		viewport:      vp,
		compose:       compose,
		query:         query,
		drafts:        make(map[string]string),
		composeHeight: 3,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshStatusCmd(),
		m.refreshConversationsCmd(),
		m.ensureSSECmd(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.ready = true
		if m.activeID != "" && len(m.messages) > 0 {
			m.setThreadContent(m.renderActiveThread())
		}
		return m, nil

	case statusMsg:
		m.status = localapi.DaemonStatus(msg)
		return m, nil

	case conversationsMsg:
		selectedID := ""
		if item, ok := m.list.SelectedItem().(convItem); ok {
			selectedID = item.conv.ConversationID
		}
		items := make([]list.Item, 0, len(msg))
		for _, c := range msg {
			_, selected := m.broadcastIDs[c.ConversationID]
			items = append(items, convItem{conv: c, selected: selected})
			if m.activeID != "" && c.ConversationID == m.activeID {
				m.activeName = c.Name
				m.activeParticipants = c.Participants
			}
		}
		m.list.SetItems(items)
		if selectedID != "" {
			for i, it := range items {
				if it.(convItem).conv.ConversationID == selectedID {
					m.list.Select(i)
					break
				}
			}
		}
		m.syncComposePlaceholder()
		return m, nil

	case broadcastSentMsg:
		m.compose.SetValue("")
		if msg.failed == 0 {
			m.info = fmt.Sprintf("Sent to %d chats", msg.ok)
			m.err = ""
			m.clearBroadcast()
			m.restampConversationList()
			m.syncComposePlaceholder()
		} else {
			m.info = fmt.Sprintf("Sent to %d/%d chats", msg.ok, msg.ok+msg.failed)
			if msg.lastErr != "" {
				m.err = msg.lastErr
			}
		}
		return m, m.refreshConversationsCmd()

	case messagesMsg:
		if msg.conversationID != m.activeID || msg.generation != m.msgGeneration {
			return m, nil
		}
		m.messages = msg.messages
		m.selectedMsg = clampMessageIndex(len(m.messages), m.selectedMsg)
		m.setThreadContent(m.renderActiveThread())
		m.viewport.GotoBottom()
		return m, nil

	case searchMsg:
		items := make([]list.Item, 0, len(msg))
		for _, hit := range msg {
			platform := strings.ToLower(hit.SourcePlatform)
			if platform != "" && platform != "sms" && platform != "rcs" {
				continue
			}
			items = append(items, searchItem{hit: hit})
		}
		m.search.SetItems(items)
		return m, nil

	case sentMsg:
		m.info = "Sent"
		m.compose.SetValue("")
		delete(m.drafts, msg.conversationID)
		return m, tea.Batch(m.refreshMessagesCmd(msg.conversationID, m.msgGeneration), m.refreshConversationsCmd())

	case markedReadMsg:
		return m, m.refreshConversationsCmd()

	case reconnectMsg:
		m.status = localapi.DaemonStatus(msg)
		m.info = "Reconnect requested"
		return m, m.refreshStatusCmd()

	case mediaDoneMsg:
		m.err = ""
		if msg.action == "save" {
			m.info = "Saved " + msg.path
		} else {
			m.info = "Opened " + filepath.Base(msg.path)
		}
		return m, nil

	case reactDoneMsg:
		m.err = ""
		if msg.action == "remove" {
			m.info = "Removed " + msg.emoji
		} else {
			m.info = "Reacted " + msg.emoji
		}
		return m, m.refreshMessagesCmd(msg.conversationID, m.msgGeneration)

	case attachResolvedMsg:
		if msg.conversationID == "" || strings.TrimSpace(msg.path) == "" {
			return m, nil
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
			return m, nil
		}
		m.err = ""
		m.info = "Sending media…"
		return m, m.sendMediaCmd(msg.conversationID, msg.path, msg.caption)

	case attachCancelledMsg:
		m.info = "Attach cancelled"
		return m, nil

	case clipboardTextFallbackMsg:
		text, err := clipboardText()
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		if text == "" {
			m.info = "Clipboard has no media"
			return m, nil
		}
		m.info = ""
		m.compose.SetValue(m.compose.Value() + text)
		return m, nil

	case sseReadyMsg:
		if m.sseCancel != nil {
			m.sseCancel()
		}
		m.sseCancel = msg.cancel
		m.events = msg.events
		m.session.cancelSSE = msg.cancel
		return m, m.waitSSECmd()

	case streamEventMsg:
		cmds := []tea.Cmd{m.waitSSECmd()}
		switch msg.Type {
		case "status", "heartbeat":
			if msg.Type == "status" {
				cmds = append(cmds, m.refreshStatusCmd())
			}
		case "conversations":
			cmds = append(cmds, m.refreshConversationsCmd())
		case "messages":
			if m.activeID != "" && (msg.ConversationID == "" || msg.ConversationID == m.activeID) {
				cmds = append(cmds,
					m.refreshMessagesCmd(m.activeID, m.msgGeneration),
					tea.Sequence(m.markReadCmd(m.activeID), m.refreshConversationsCmd()),
				)
			} else {
				cmds = append(cmds, m.refreshConversationsCmd())
			}
		case "sse_restart":
			cmds = []tea.Cmd{m.ensureSSECmd()}
		}
		return m, tea.Batch(cmds...)

	case errMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		if m.focus == focusSearch {
			return m.updateSearchKeys(msg)
		}
		if m.focus == focusCompose {
			return m.updateComposeKeys(msg)
		}
		if m.focus == focusThread {
			return m.updateThreadKeys(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "/":
			m.focus = focusSearch
			m.query.SetValue("")
			m.query.Focus()
			m.compose.Blur()
			return m, nil
		case "r":
			m.info = "Reconnecting…"
			return m, m.reconnectCmd()
		case "o":
			if m.activeID != "" {
				m.info = "Opening media…"
				return m, m.mediaActionCmd(false)
			}
		case "s":
			if m.activeID != "" {
				m.info = "Saving media…"
				return m, m.mediaActionCmd(true)
			}
		case "a":
			if m.activeID != "" {
				if !m.canSend() {
					m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
					return m, nil
				}
				m.info = "Attach file…"
				return m, m.attachPickerCmd(m.activeID, captionForAttach(m.compose.Value()))
			}
		case "ctrl+v":
			if m.activeID != "" {
				if !m.canSend() {
					m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
					return m, nil
				}
				m.info = "Checking clipboard…"
				return m, m.attachClipboardCmd(m.activeID, captionForAttach(m.compose.Value()))
			}
		case "tab":
			return m, m.cycleFocus(1)
		case "shift+tab":
			return m, m.cycleFocus(-1)
		case " ":
			if m.focus == focusList {
				if item, ok := m.list.SelectedItem().(convItem); ok {
					m.toggleBroadcast(item.conv.ConversationID, item.conv.Name)
					m.restampConversationList()
					m.syncComposePlaceholder()
					n := m.broadcastCount()
					if n == 0 {
						m.info = "Broadcast cleared"
					} else {
						m.info = fmt.Sprintf("%d chats selected — type message, Enter sends to all", n)
					}
					return m, nil
				}
			}
		case "c":
			if m.focus == focusList && m.broadcastCount() > 0 {
				m.clearBroadcast()
				m.restampConversationList()
				m.syncComposePlaceholder()
				m.info = "Broadcast cleared"
				return m, nil
			}
		case "m":
			if m.focus == focusList && m.broadcastCount() > 0 {
				m.focus = focusCompose
				m.syncComposePlaceholder()
				return m, m.compose.Focus()
			}
		case "enter":
			if m.focus == focusList {
				return m.openSelectedConversation()
			}
		case "esc":
			if m.broadcastCount() > 0 {
				m.clearBroadcast()
				m.restampConversationList()
				m.syncComposePlaceholder()
				m.info = "Broadcast cleared"
				m.err = ""
				return m, nil
			}
			m.focus = focusList
			m.reactPalette = false
			m.err = ""
			m.info = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focus == focusList {
		m.list, cmd = m.list.Update(msg)
	}
	return m, cmd
}

func (m Model) updateThreadKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.reactPalette {
		switch key {
		case "esc":
			m.reactPalette = false
			m.info = ""
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			idx := int(key[0] - '1')
			if idx < 0 || idx >= len(reactPaletteEmojis) {
				return m, nil
			}
			target, ok := selectedMessage(m.messages, m.selectedMsg)
			if !ok || strings.TrimSpace(target.MessageID) == "" || m.activeID == "" {
				m.err = "no message selected"
				m.reactPalette = false
				return m, nil
			}
			if !m.canSend() {
				m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
				return m, nil
			}
			emoji := reactPaletteEmojis[idx]
			action := reactionAction(target.Reactions, emoji)
			m.reactPalette = false
			m.info = "Sending reaction…"
			return m, m.reactCmd(m.activeID, target.MessageID, emoji, action)
		}
		return m, nil
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.focus = focusList
		m.err = ""
		m.info = ""
		return m, nil
	case "tab":
		return m, m.cycleFocus(1)
	case "shift+tab":
		return m, m.cycleFocus(-1)
	case "j", "down":
		if len(m.messages) == 0 {
			return m, nil
		}
		m.selectedMsg = clampMessageIndex(len(m.messages), m.selectedMsg)
		if m.selectedMsg < len(m.messages)-1 {
			m.selectedMsg++
		}
		m.setThreadContent(m.renderActiveThread())
		return m, nil
	case "k", "up":
		if len(m.messages) == 0 {
			return m, nil
		}
		m.selectedMsg = clampMessageIndex(len(m.messages), m.selectedMsg)
		if m.selectedMsg > 0 {
			m.selectedMsg--
		}
		m.setThreadContent(m.renderActiveThread())
		return m, nil
	case "e":
		if _, ok := selectedMessage(m.messages, m.selectedMsg); !ok {
			m.err = "no message to react to"
			return m, nil
		}
		m.reactPalette = true
		m.err = ""
		m.info = "React: " + reactPaletteHelp()
		return m, nil
	case "o":
		if m.activeID != "" {
			m.info = "Opening media…"
			return m, m.mediaActionCmd(false)
		}
	case "s":
		if m.activeID != "" {
			m.info = "Saving media…"
			return m, m.mediaActionCmd(true)
		}
	case "a":
		if m.activeID != "" {
			if !m.canSend() {
				m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
				return m, nil
			}
			m.info = "Attach file…"
			return m, m.attachPickerCmd(m.activeID, captionForAttach(m.compose.Value()))
		}
	case "ctrl+v":
		if m.activeID != "" {
			if !m.canSend() {
				m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
				return m, nil
			}
			m.info = "Checking clipboard…"
			return m, m.attachClipboardCmd(m.activeID, captionForAttach(m.compose.Value()))
		}
	case "r":
		m.info = "Reconnecting…"
		return m, m.reconnectCmd()
	case "/":
		m.focus = focusSearch
		m.query.SetValue("")
		m.query.Focus()
		m.compose.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) updateComposeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.compose.Blur()
		m.err = ""
		m.info = ""
		return m, nil
	case "ctrl+a":
		// Bare "a" must remain typable in the composer; Ctrl+A attaches.
		if m.activeID == "" {
			return m, nil
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
			return m, nil
		}
		m.info = "Attach file…"
		return m, m.attachPickerCmd(m.activeID, captionForAttach(m.compose.Value()))
	case "ctrl+v":
		if m.activeID == "" {
			break
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
			return m, nil
		}
		m.info = "Checking clipboard…"
		return m, m.attachClipboardCmd(m.activeID, captionForAttach(m.compose.Value()))
	case "enter":
		body := strings.TrimSpace(m.compose.Value())
		if body == "" {
			return m, nil
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
			return m, nil
		}
		if ids := m.broadcastIDList(); len(ids) > 0 {
			if _, isPath := looksLikeExistingFile(body); isPath {
				m.err = "broadcast is text-only — clear multi-select (c) to send media"
				return m, nil
			}
			m.info = fmt.Sprintf("Sending to %d chats…", len(ids))
			return m, m.sendBroadcastCmd(ids, body)
		}
		if m.activeID == "" {
			return m, nil
		}
		if path, ok := looksLikeExistingFile(body); ok {
			m.info = "Sending media…"
			return m, m.sendMediaCmd(m.activeID, path, "")
		}
		return m, m.sendCmd(m.activeID, body)
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.compose, cmd = m.compose.Update(msg)
	return m, cmd
}

func (m Model) updateSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.query.Blur()
		m.search.SetItems(nil)
		return m, nil
	case "enter":
		if item, ok := m.search.SelectedItem().(searchItem); ok {
			m.focus = focusThread
			m.query.Blur()
			return m.openConversation(item.hit.ConversationID, item.hit.Name, "")
		}
		q := strings.TrimSpace(m.query.Value())
		if q == "" {
			return m, nil
		}
		return m, m.searchCmd(q)
	case "ctrl+c":
		return m, tea.Quit
	case "down", "j", "up", "k", "pgdown", "pgup":
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.query, cmd = m.query.Update(msg)
	return m, cmd
}

func (m *Model) cycleFocus(dir int) tea.Cmd {
	order := []focusPane{focusList, focusThread, focusCompose}
	idx := 0
	for i, p := range order {
		if p == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(order)) % len(order)
	m.focus = order[idx]
	m.reactPalette = false
	m.compose.Blur()
	if m.focus == focusCompose {
		return m.compose.Focus()
	}
	return nil
}

func (m Model) openSelectedConversation() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(convItem)
	if !ok {
		return m, nil
	}
	return m.openConversation(item.conv.ConversationID, item.conv.Name, item.conv.Participants)
}

func (m Model) openConversation(id, name, participants string) (tea.Model, tea.Cmd) {
	m.saveComposeDraft()
	m.msgGeneration++
	gen := m.msgGeneration
	m.activeID = id
	m.activeName = name
	m.activeParticipants = participants
	m.messages = nil
	m.selectedMsg = -1
	m.reactPalette = false
	m.focus = focusCompose
	m.compose.SetValue(m.drafts[id])
	focusCmd := m.compose.Focus()
	m.setThreadContent(mutedStyle.Render("Loading…"))
	return m, tea.Batch(
		focusCmd,
		tea.ClearScreen,
		m.refreshMessagesCmd(id, gen),
		m.markReadCmd(id),
	)
}

func (m *Model) saveComposeDraft() {
	if m.drafts == nil {
		m.drafts = make(map[string]string)
	}
	id := strings.TrimSpace(m.activeID)
	if id == "" {
		return
	}
	text := m.compose.Value()
	if strings.TrimSpace(text) == "" {
		delete(m.drafts, id)
		return
	}
	m.drafts[id] = text
}

func (m *Model) setThreadContent(content string) {
	content = padLines(content, m.viewport.Height, max(1, m.viewportWidth))
	m.viewport.SetYOffset(0)
	m.viewport.SetContent(content)
}

func (m Model) View() string {
	if !m.ready {
		return "Starting OpenMessage TUI…"
	}
	status := renderStatus(m.status, m.err, m.info)
	help := "q quit  space multi-select  m compose  c clear  e react  enter send  esc back"

	mainH := m.height - 2
	if mainH < 5 {
		mainH = 5
	}
	leftW := leftWidth(m.width)
	rightW := m.width - leftW
	if rightW < 20 {
		rightW = 20
	}

	left := borderStyle.Width(leftW).Height(mainH).MaxHeight(mainH).Render(m.list.View())
	threadTitle := m.activeName
	if threadTitle == "" {
		threadTitle = "Thread"
	}
	innerW := max(1, rightW-2)
	composeH := m.composeHeight
	if composeH < 1 {
		composeH = 3
	}
	threadBody := lipgloss.JoinVertical(lipgloss.Left,
		paintLine(titleStyle, truncate(threadTitle, innerW), innerW),
		m.viewport.View(),
		lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).Height(composeH).MaxHeight(composeH).Render(m.compose.View()),
	)
	right := borderStyle.Width(rightW).Height(mainH).MaxHeight(mainH).Render(threadBody)

	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	if m.focus == focusSearch {
		searchPane := lipgloss.JoinVertical(lipgloss.Left, m.query.View(), m.search.View())
		main = borderStyle.Width(m.width).Height(mainH).MaxHeight(mainH).Render(searchPane)
	}

	frame := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Height(1).MaxHeight(1).Render(status),
		main,
		paintLine(mutedStyle, help, m.width),
	)
	// Fill the whole terminal so Windows Terminal doesn't leave ghost cells
	// from the previous conversation's taller/wrapped content.
	return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, frame,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func (m *Model) layout() {
	listW := leftWidth(m.width)
	mainH := m.height - 2
	if mainH < 5 {
		mainH = 5
	}
	listH := mainH - 2
	if listH < 5 {
		listH = 5
	}
	m.list.SetSize(listW-2, listH)
	m.search.SetSize(m.width-4, listH-2)
	threadW := m.width - listW - 4
	if threadW < 20 {
		threadW = 20
	}
	m.viewportWidth = threadW
	m.viewport.Width = threadW
	composeH := m.composeHeight
	if composeH < 1 {
		composeH = 3
	}
	// Title (1) + compose (composeH) inside border.
	m.viewport.Height = listH - 1 - composeH
	if m.viewport.Height < 3 {
		m.viewport.Height = 3
	}
	m.compose.SetWidth(max(1, threadW-2))
	m.compose.SetHeight(composeH)
}

func leftWidth(total int) int {
	w := total / 3
	if w < 24 {
		w = 24
	}
	if w > 42 {
		w = 42
	}
	if w > total-30 {
		w = total - 30
	}
	if w < 16 {
		w = 16
	}
	return w
}

func (m Model) canSend() bool {
	g := m.status.Google
	if g.NeedsPairing || !g.Paired {
		return false
	}
	if g.NeedsRepair || g.AuthExpired {
		return false
	}
	return g.Connected || m.status.Connected
}

func (m Model) refreshStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, _, err := m.session.Client.Status(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return statusMsg(status)
	}
}

func (m Model) refreshConversationsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		convs, err := m.session.Client.ListSMSConversations(ctx, 200)
		if err != nil {
			return errMsg{err: err}
		}
		return conversationsMsg(convs)
	}
}

func (m Model) refreshMessagesCmd(conversationID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		msgs, err := m.session.Client.ConversationMessages(ctx, conversationID, 100)
		if err != nil {
			return errMsg{err: err}
		}
		// API returns newest-first; render oldest→newest.
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		return messagesMsg{conversationID: conversationID, generation: generation, messages: msgs}
	}
}

func (m Model) markReadCmd(conversationID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.session.Client.MarkRead(ctx, conversationID); err != nil {
			return errMsg{err: err}
		}
		return markedReadMsg(conversationID)
	}
}

func (m Model) searchCmd(query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hits, err := m.session.Client.SearchMessages(ctx, query, 50)
		if err != nil {
			return errMsg{err: err}
		}
		return searchMsg(hits)
	}
}

func (m Model) reconnectCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		status, err := m.session.Client.ReconnectGoogle(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return reconnectMsg(status)
	}
}

func (m Model) sendCmd(conversationID, body string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		status, _, err := m.session.Client.Status(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		key, err := newIdempotencyKey()
		if err != nil {
			return errMsg{err: err}
		}
		if status.V2Send || status.V2Primary {
			if _, err := m.session.Client.SubmitText(ctx, localapi.TextSubmission{
				ConversationID: conversationID,
				Body:           body,
				IdempotencyKey: key,
			}); err != nil {
				return errMsg{err: err}
			}
		} else {
			if _, err := m.session.Client.LegacySendText(ctx, conversationID, body, "", key); err != nil {
				return errMsg{err: err}
			}
		}
		return sentMsg{conversationID: conversationID}
	}
}

func (m Model) reactCmd(conversationID, messageID, emoji, action string) tea.Cmd {
	client := m.session.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := client.React(ctx, conversationID, messageID, emoji, action); err != nil {
			return errMsg{err: err}
		}
		return reactDoneMsg{conversationID: conversationID, emoji: emoji, action: action}
	}
}

func (m Model) mediaActionCmd(export bool) tea.Cmd {
	msgs := m.messages
	client := m.session.Client
	return func() tea.Msg {
		msg, ok := latestMediaMessage(msgs)
		if !ok {
			return errMsg{err: fmt.Errorf("no media in this thread")}
		}
		if strings.TrimSpace(msg.MessageID) == "" {
			return errMsg{err: fmt.Errorf("media message missing id")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		data, contentType, err := client.DownloadMedia(ctx, msg.MessageID)
		if err != nil {
			return errMsg{err: err}
		}
		path, err := materializeMedia(msg, data, contentType, export)
		if err != nil {
			return errMsg{err: err}
		}
		action := "save"
		if !export {
			action = "open"
			if err := openFile(path); err != nil {
				return errMsg{err: fmt.Errorf("open %s: %w", path, err)}
			}
		}
		return mediaDoneMsg{action: action, path: path}
	}
}

func (m Model) ensureSSECmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		events, err := m.session.Client.Events(ctx)
		if err != nil {
			cancel()
			time.Sleep(2 * time.Second)
			return streamEventMsg{Type: "sse_restart"}
		}
		return sseReadyMsg{events: events, cancel: cancel}
	}
}

type sseReadyMsg struct {
	events <-chan localapi.StreamEvent
	cancel context.CancelFunc
}

func (m Model) waitSSECmd() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		if ch == nil {
			time.Sleep(time.Second)
			return streamEventMsg{Type: "sse_restart"}
		}
		evt, ok := <-ch
		if !ok {
			time.Sleep(time.Second)
			return streamEventMsg{Type: "sse_restart"}
		}
		return streamEventMsg(evt)
	}
}

func (m Model) renderActiveThread() string {
	resolve := reactionResolver(m.activeParticipants, m.activeName, m.messages)
	peer := reactionPeerFallback(m.activeParticipants, m.activeName)
	return renderMessages(m.messages, m.viewportWidth, m.selectedMsg, resolve, peer)
}

func renderMessages(msgs []localapi.Message, width, selected int, resolve func(string) string, peerName string) string {
	if width < 16 {
		width = 16
	}
	if len(msgs) == 0 {
		return paintLine(mutedStyle, "No messages yet.", width)
	}
	selected = clampMessageIndex(len(msgs), selected)
	var b strings.Builder
	for i, msg := range msgs {
		ts := time.UnixMilli(msg.TimestampMS).Local().Format("Jan 2 15:04")
		who := "them"
		if msg.IsFromMe {
			who = "you"
		} else if name := strings.TrimSpace(msg.SenderName); name != "" {
			who = name
		}
		body := strings.TrimSpace(msg.Body)
		hasMedia := strings.TrimSpace(msg.MediaID) != "" || strings.TrimSpace(msg.MimeType) != ""
		switch {
		case hasMedia:
			body = mediaLabel(msg)
		case body == "":
			body = "[empty]"
		}
		if rx := formatReactions(msg.Reactions, resolve, peerName); rx != "" {
			body = body + "  " + rx
		}
		marker := " "
		if i == selected {
			marker = ">"
		}
		prefix := fmt.Sprintf("%s %s  %s: ", marker, ts, who)
		prefixW := runewidth.StringWidth(prefix)
		bodyWidth := width - prefixW
		if bodyWidth < 8 {
			bodyWidth = width
			prefix = ""
			prefixW = 0
		}
		chunks := wrapText(body, bodyWidth)
		style := participantStyle(msg)
		if hasMedia {
			style = style.Bold(true)
		}
		if i == selected {
			// Avoid Reverse(): it ghosts badly on Windows Terminal with emoji
			// (reactions) and variable-width glyphs.
			style = style.Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("238"))
		}
		for li, chunk := range chunks {
			var line string
			if li == 0 && prefix != "" {
				line = prefix + chunk
			} else if prefixW > 0 {
				line = strings.Repeat(" ", prefixW) + chunk
			} else {
				line = chunk
			}
			b.WriteString(paintLine(style, line, width))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// paintLine truncates and space-pads to an exact terminal cell width before
// applying styles, so Windows Terminal differential redraws don't leave tails.
func paintLine(style lipgloss.Style, text string, width int) string {
	if width < 1 {
		width = 1
	}
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", "")
	text = runewidth.Truncate(text, width, "…")
	if pad := width - runewidth.StringWidth(text); pad > 0 {
		text += strings.Repeat(" ", pad)
	}
	return style.Render(text)
}

func padLines(content string, height, width int) string {
	if height < 1 {
		return content
	}
	content = strings.TrimRight(content, "\n")
	var lines []string
	if content == "" {
		lines = nil
	} else {
		lines = strings.Split(content, "\n")
	}
	blank := strings.Repeat(" ", max(0, width))
	for len(lines) < height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func renderStatus(status localapi.DaemonStatus, errText, info string) string {
	g := status.Google
	state := "disconnected"
	style := warnStyle
	switch {
	case g.NeedsPairing || (!g.Paired && !status.Connected):
		state = "unpaired — run openmessage pair"
		style = warnStyle
	case g.NeedsRepair:
		state = "needs repair — re-pair Google Messages"
		style = warnStyle
	case g.AuthExpired:
		state = "auth expired"
		style = warnStyle
	case g.Connected || status.Connected:
		state = "connected"
		style = okStyle
		if !g.PhoneResponding {
			state = "connected (phone not responding)"
			style = warnStyle
		}
	}
	line := style.Render("Google Messages: "+state) + "  " + mutedStyle.Render("local API")
	if info != "" {
		line += "  " + mutedStyle.Render(info)
	}
	if errText != "" {
		line += "  " + errStyle.Render(errText)
	}
	return line
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func newIdempotencyKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Run launches the Bubble Tea program for session.
func Run(session *Session) error {
	model := NewModel(session)
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	borderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	meStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
)
