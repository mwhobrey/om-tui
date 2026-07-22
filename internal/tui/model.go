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
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	conv localapi.Conversation
}

func (i convItem) Title() string {
	name := strings.TrimSpace(i.conv.Name)
	if name == "" {
		name = i.conv.ConversationID
	}
	if i.conv.UnreadCount > 0 {
		return fmt.Sprintf("%s (%d)", name, i.conv.UnreadCount)
	}
	return name
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
	compose  textinput.Model
	query    textinput.Model

	activeID      string
	activeName    string
	messages      []localapi.Message
	msgGeneration uint64
	viewportWidth int

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

	compose := textinput.New()
	compose.Placeholder = "Write a message…"
	compose.CharLimit = 4000
	compose.Prompt = "> "

	query := textinput.New()
	query.Placeholder = "Search messages…"
	query.CharLimit = 200
	query.Prompt = "/ "

	vp := viewport.New(40, 10)
	vp.SetContent("Select a conversation.")

	return Model{
		session:  session,
		focus:    focusList,
		list:     convList,
		search:   searchList,
		viewport: vp,
		compose:  compose,
		query:    query,
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
			m.setThreadContent(renderMessages(m.messages, m.viewportWidth))
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
			items = append(items, convItem{conv: c})
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
		return m, nil

	case messagesMsg:
		if msg.conversationID != m.activeID || msg.generation != m.msgGeneration {
			return m, nil
		}
		m.messages = msg.messages
		m.setThreadContent(renderMessages(msg.messages, m.viewportWidth))
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
			cmds = append(cmds, m.refreshConversationsCmd())
			if m.activeID != "" && (msg.ConversationID == "" || msg.ConversationID == m.activeID) {
				cmds = append(cmds, m.refreshMessagesCmd(m.activeID, m.msgGeneration))
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
		case "tab":
			m.cycleFocus(1)
			return m, nil
		case "shift+tab":
			m.cycleFocus(-1)
			return m, nil
		case "enter":
			if m.focus == focusList {
				return m.openSelectedConversation()
			}
		case "esc":
			m.focus = focusList
			m.err = ""
			m.info = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case focusList:
		m.list, cmd = m.list.Update(msg)
	case focusThread:
		m.viewport, cmd = m.viewport.Update(msg)
	}
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
	case "o":
		if strings.TrimSpace(m.compose.Value()) == "" && m.activeID != "" {
			m.info = "Opening media…"
			return m, m.mediaActionCmd(false)
		}
	case "s":
		if strings.TrimSpace(m.compose.Value()) == "" && m.activeID != "" {
			m.info = "Saving media…"
			return m, m.mediaActionCmd(true)
		}
	case "enter":
		body := strings.TrimSpace(m.compose.Value())
		if body == "" || m.activeID == "" {
			return m, nil
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press r to reconnect or run openmessage pair"
			return m, nil
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
			return m.openConversation(item.hit.ConversationID, item.hit.Name)
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

func (m *Model) cycleFocus(dir int) {
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
	m.compose.Blur()
	if m.focus == focusCompose {
		m.compose.Focus()
	}
}

func (m Model) openSelectedConversation() (tea.Model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(convItem)
	if !ok {
		return m, nil
	}
	return m.openConversation(item.conv.ConversationID, item.conv.Name)
}

func (m Model) openConversation(id, name string) (tea.Model, tea.Cmd) {
	m.msgGeneration++
	gen := m.msgGeneration
	m.activeID = id
	m.activeName = name
	m.messages = nil
	m.focus = focusCompose
	m.compose.Focus()
	m.setThreadContent(mutedStyle.Width(max(1, m.viewportWidth)).Render("Loading…"))
	return m, tea.Batch(
		m.refreshMessagesCmd(id, gen),
		m.markReadCmd(id),
	)
}

func (m *Model) setThreadContent(content string) {
	m.viewport.SetYOffset(0)
	m.viewport.SetContent(content)
}

func (m Model) View() string {
	if !m.ready {
		return "Starting OpenMessage TUI…"
	}
	status := renderStatus(m.status, m.err, m.info)
	help := mutedStyle.Render("q quit  / search  o open media  s save media  r reconnect  tab focus  enter open/send  esc back")

	mainH := m.height - 2
	if mainH < 5 {
		mainH = 5
	}
	leftW := leftWidth(m.width)
	rightW := m.width - leftW
	if rightW < 20 {
		rightW = 20
	}

	left := borderStyle.Width(leftW).Height(mainH).Render(m.list.View())
	threadTitle := m.activeName
	if threadTitle == "" {
		threadTitle = "Thread"
	}
	threadBody := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Width(rightW-2).Render(truncate(threadTitle, rightW-4)),
		m.viewport.View(),
		m.compose.View(),
	)
	right := borderStyle.Width(rightW).Height(mainH).Render(threadBody)

	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	if m.focus == focusSearch {
		searchPane := lipgloss.JoinVertical(lipgloss.Left, m.query.View(), m.search.View())
		main = borderStyle.Width(m.width).Height(mainH).Render(searchPane)
	}

	frame := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(status),
		main,
		lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(help),
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
	// Title (1) + compose (1) + padding inside border.
	m.viewport.Height = listH - 3
	if m.viewport.Height < 3 {
		m.viewport.Height = 3
	}
	m.compose.Width = threadW - 2
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

func renderMessages(msgs []localapi.Message, width int) string {
	if width < 16 {
		width = 16
	}
	if len(msgs) == 0 {
		return mutedStyle.Width(width).Render("No messages yet.")
	}
	var b strings.Builder
	for _, msg := range msgs {
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
		line := fmt.Sprintf("%s  %s: %s", ts, who, body)
		style := themStyle
		if msg.IsFromMe {
			style = meStyle
		}
		if hasMedia {
			style = style.Bold(true)
		}
		// Width-constrain before paint so ANSI styles don't wrap mid-sequence
		// (a common Windows Terminal ghosting source).
		b.WriteString(style.Width(width).MaxWidth(width).Render(line))
		b.WriteByte('\n')
	}
	return b.String()
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
	themStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)
