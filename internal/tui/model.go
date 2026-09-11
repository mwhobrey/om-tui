package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
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
	conv     localapi.Conversation
	selected bool
}

func (i convItem) Description() string {
	preview := strings.TrimSpace(i.conv.LastMessagePreview)
	if preview == "" {
		return i.conv.ConversationID
	}
	return truncate(preview, 60)
}

func (i convItem) FilterValue() string {
	return conversationFilterValue(i.conv)
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
	riversMsg        []localapi.River
	conversationsMsg []localapi.Conversation
	messagesMsg      struct {
		conversationID string
		generation     uint64
		messages       []localapi.Message
	}
	slackThreadMsg struct {
		conversationID string
		rootMessageID  string
		messages       []localapi.Message
	}
	olderSlackHistoryMsg struct {
		conversationID string
		messages       []localapi.Message
	}
	searchMsg      []localapi.SearchHit
	errMsg         struct{ err error }
	streamEventMsg localapi.StreamEvent
	sentMsg        struct{ conversationID string }
	sendFailedMsg  struct {
		conversationID string // "" for a broadcast, which has no single conversation
		body           string // the draft that was optimistically cleared, to restore
		err            error
	}
	markedReadMsg string
	reconnectMsg  localapi.DaemonStatus
	mediaDoneMsg  struct {
		action string // "open" or "save"
		path   string
	}
	reactDoneMsg struct {
		conversationID string
		emoji          string
		action         string
	}
	debouncedConvRefreshMsg uint64
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

	list     convList
	search   list.Model
	viewport viewport.Model
	compose  textarea.Model
	query    textinput.Model

	activeID            string
	activeName          string
	activeParticipants  string
	activeRiverID       string
	rivers              []localapi.River
	messages            []localapi.Message
	msgGeneration       uint64
	viewportWidth       int
	composeHeight       int
	drafts              map[string]string
	broadcastIDs        map[string]string // conversationID -> display name
	sending             bool              // true while a sendCmd/sendMediaCmd/sendBroadcastCmd is in flight
	selectedMsg         int               // index into messages; clamped to latest when out of range
	threadRootID        string            // non-empty while a dedicated Slack thread is open
	channelMessages     []localapi.Message
	reactPalette        bool   // thread-focus emoji picker open
	convRefreshGen      uint64 // debounce token for conversation list reloads
	pendingConvs        []localapi.Conversation
	pendingConvsSet     bool // hold list Apply while jump-filter is open
	palette             commandPalette
	paletteConvs        []localapi.Conversation
	paletteConvsAt      time.Time
	paletteConvsLoading bool
	frecency            *frecencyStore
	customCmds          []customCommand
	customCmdsModTime   time.Time
	pair                pairOverlay

	sseCancel context.CancelFunc
	events    <-chan localapi.StreamEvent
	ready     bool
}

// NewModel builds the initial TUI model bound to a daemon session.
func NewModel(session *Session) Model {
	delegate := list.NewDefaultDelegate()
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
	compose.SetHeight(composeMinHeight)
	compose.MaxHeight = composeMaxHeight
	compose.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	compose.BlurredStyle.Prompt = mutedStyle
	// Default focused CursorLine uses a near-black background that can swallow
	// the placeholder on Windows Terminal; keep the line unstyled.
	compose.FocusedStyle.CursorLine = lipgloss.NewStyle()
	compose.BlurredStyle.CursorLine = lipgloss.NewStyle()
	compose.FocusedStyle.Placeholder = mutedStyle
	compose.BlurredStyle.Placeholder = mutedStyle

	query := textinput.New()
	query.Placeholder = "Search message text…"
	query.CharLimit = 200
	query.Prompt = "ctrl+f "

	vp := viewport.New(40, 10)
	vp.SetContent("Select a conversation.")

	dataDir := ""
	if session != nil {
		dataDir = session.DataDir
	}

	customCmds, customCmdsModTime, customCmdsErr := loadCustomCommands(dataDir)

	m := Model{
		session:           session,
		focus:             focusList,
		list:              convList{title: "Conversations"},
		search:            searchList,
		viewport:          vp,
		compose:           compose,
		query:             query,
		drafts:            make(map[string]string),
		composeHeight:     composeMinHeight,
		activeRiverID:     "messages-default",
		palette:           newCommandPalette(),
		frecency:          loadFrecency(dataDir),
		customCmds:        customCmds,
		customCmdsModTime: customCmdsModTime,
	}
	if customCmdsErr != nil {
		m.err = customCmdsErr.Error()
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshStatusCmd(),
		m.refreshRiversCmd(),
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

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case statusMsg:
		m.status = localapi.DaemonStatus(msg)
		return m.syncPairOverlayFromStatus()

	case pairTickMsg:
		m.pair.ticking = false
		m.pair.ticks++
		if !m.pair.successUntil.IsZero() && time.Now().After(m.pair.successUntil) {
			return m.closePairOverlay()
		}
		var cmds []tea.Cmd
		if m.pair.ticks%pairStatusEveryTicks == 0 {
			cmds = append(cmds, m.refreshStatusCmd())
		}
		var tick tea.Cmd
		m, tick = m.armPairTick()
		if tick != nil {
			cmds = append(cmds, tick)
		}
		return m, tea.Batch(cmds...)

	case pairStartedMsg:
		if msg.err != nil {
			m.pair.submitting = false
			m.pair.pasteErr = msg.err.Error()
			return m, nil
		}
		if msg.status.Google.Pairing != nil {
			m.status = msg.status
			return m.syncPairOverlayFromStatus()
		}
		m, tick := m.armPairTick()
		return m, tea.Batch(m.refreshStatusCmd(), tick)

	case riversMsg:
		m.rivers = msg
		if m.activeRiverID == "" && len(msg) > 0 {
			m.activeRiverID = msg[0].ID
		}
		found := false
		for _, r := range msg {
			if r.ID == m.activeRiverID {
				found = true
				m.list.title = r.DisplayName
				break
			}
		}
		if !found && len(msg) > 0 {
			m.activeRiverID = msg[0].ID
			m.list.title = msg[0].DisplayName
		}
		return m, m.refreshConversationsCmd()

	case conversationsMsg:
		if m.list.filtering {
			m.pendingConvs = append([]localapi.Conversation(nil), msg...)
			m.pendingConvsSet = true
			return m, nil
		}
		m.applyConversations(msg)
		return m, nil

	case debouncedConvRefreshMsg:
		if uint64(msg) != m.convRefreshGen {
			return m, nil
		}
		return m, m.refreshConversationsCmd()

	case broadcastSentMsg:
		m.sending = false
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

	case paletteConvsMsg:
		m.paletteConvsLoading = false
		if msg.err != nil {
			if m.palette.open && m.palette.mode == "jump" {
				m.info = "Failed to load conversations for jump"
			}
			return m, nil
		}
		m.paletteConvs = msg.conversations
		m.paletteConvsAt = time.Now()
		if m.palette.open {
			m.refreshPaletteMatches()
		}
		return m, nil

	case messagesMsg:
		if msg.conversationID != m.activeID || msg.generation != m.msgGeneration {
			return m, nil
		}
		m.messages = msg.messages
		m.selectedMsg = clampMessageIndex(len(m.messages), m.selectedMsg)
		m.setThreadContentFollow(m.renderActiveThread())
		return m, nil

	case slackThreadMsg:
		if msg.conversationID != m.activeID || msg.rootMessageID != m.threadRootID {
			return m, nil
		}
		m.messages = msg.messages
		m.selectedMsg = clampMessageIndex(len(m.messages), len(m.messages)-1)
		m.setThreadContentFollow(m.renderActiveThread())
		m.info = "Slack thread"
		return m, nil

	case olderSlackHistoryMsg:
		if msg.conversationID != m.activeID || m.threadRootID != "" {
			return m, nil
		}
		selectedID := ""
		if selected, ok := selectedMessage(m.messages, m.selectedMsg); ok {
			selectedID = selected.MessageID
		}
		m.messages = msg.messages
		m.selectedMsg = indexMessageByID(m.messages, selectedID)
		if m.selectedMsg < 0 {
			m.selectedMsg = clampMessageIndex(len(m.messages), len(m.messages)-1)
		}
		m.setThreadContent(m.renderActiveThread())
		m.info = "Loaded older Slack history"
		return m, nil

	case searchMsg:
		items := make([]list.Item, 0, len(msg))
		provider := m.activeRiverProvider()
		for _, hit := range msg {
			platform := strings.ToLower(hit.SourcePlatform)
			switch provider {
			case "slack":
				if platform != "slack" {
					continue
				}
			default:
				if platform != "" && platform != "sms" && platform != "rcs" {
					continue
				}
			}
			items = append(items, searchItem{hit: hit})
		}
		m.search.SetItems(items)
		return m, nil

	case sentMsg:
		m.sending = false
		m.info = "Sent"
		delete(m.drafts, msg.conversationID)
		refresh := m.refreshMessagesCmd(msg.conversationID, m.msgGeneration)
		if m.threadRootID != "" {
			refresh = m.fetchSlackThreadCmd(msg.conversationID, m.threadRootID)
		}
		return m, tea.Batch(refresh, m.refreshConversationsCmd())

	case markedReadMsg:
		return m, m.scheduleConversationsRefresh()

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
			m.err = "Google Messages is not connected — press p to pair or r to reconnect"
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
			if m.pair.open {
				m.pair.pasteErr = err.Error()
				return m, nil
			}
			m.err = err.Error()
			return m, nil
		}
		if m.pair.open {
			return m.submitPairCookies(text)
		}
		if text == "" {
			m.info = "Clipboard has no media"
			return m, nil
		}
		m.info = ""
		m.compose.SetValue(m.compose.Value() + text)
		m.syncComposeHeight()
		m.syncComposeViewport()
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
			cmds = append(cmds, m.scheduleConversationsRefresh())
		case "messages":
			if m.activeID != "" && (msg.ConversationID == "" || msg.ConversationID == m.activeID) {
				// Refresh the open thread immediately; debounce the list so
				// unread badges don't thrash the left pane mid-navigation.
				refresh := m.refreshMessagesCmd(m.activeID, m.msgGeneration)
				if m.threadRootID != "" {
					refresh = m.fetchSlackThreadCmd(m.activeID, m.threadRootID)
				}
				cmds = append(cmds,
					refresh,
					m.markReadCmd(m.activeID),
					m.scheduleConversationsRefresh(),
				)
			} else {
				cmds = append(cmds, m.scheduleConversationsRefresh())
			}
		case "sse_restart":
			cmds = []tea.Cmd{m.ensureSSECmd()}
		}
		return m, tea.Batch(cmds...)

	case errMsg:
		m.sending = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.info = ""
		}
		return m, nil

	case sendFailedMsg:
		m.sending = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.info = ""
		}
		// Restore what failed to send — but only if the composer is still
		// empty. If the user already typed something new during the
		// round-trip, that takes priority; don't clobber it.
		if msg.body != "" && strings.TrimSpace(m.compose.Value()) == "" {
			m.compose.SetValue(msg.body)
			m.syncComposeHeight()
			m.syncComposeViewport()
		}
		return m, nil

	case tea.KeyMsg:
		if m.pair.open {
			return m.updatePairKeys(msg)
		}
		if m.palette.open {
			return m.updatePaletteKeys(msg)
		}
		if msg.String() == "ctrl+k" {
			return m.openPalette()
		}
		if m.focus == focusSearch {
			return m.updateSearchKeys(msg)
		}
		if m.focus == focusCompose {
			return m.updateComposeKeys(msg)
		}
		if m.focus == focusThread {
			return m.updateThreadKeys(msg)
		}
		// Jump-to filter: keep typing in the left-column list filter.
		if m.focus == focusList && m.list.filtering {
			return m.updateListFilterKeys(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "/":
			return m.startJumpFilter()
		case "ctrl+f":
			m.focus = focusSearch
			m.query.SetValue("")
			m.query.Focus()
			m.compose.Blur()
			return m, nil
		case "r", "ctrl+r":
			m.info = "Reconnecting…"
			return m, m.reconnectCmd()
		case "p":
			if m.googleNeedsPair() {
				return m.openPairOverlay()
			}
		case "o", "ctrl+o":
			if m.activeID != "" {
				m.info = "Opening media…"
				return m, m.mediaActionCmd(false)
			}
		case "s", "ctrl+s":
			if m.activeID != "" {
				m.info = "Saving media…"
				return m, m.mediaActionCmd(true)
			}
		case "a", "ctrl+a":
			if m.activeID != "" {
				if !m.canSend() {
					m.err = "Google Messages is not connected — press p to pair or r to reconnect"
					return m, nil
				}
				m.info = "Attach file…"
				return m, m.attachPickerCmd(m.activeID, captionForAttach(m.compose.Value()))
			}
		case "ctrl+t":
			return m.openSelectedSlackThread()
		case "ctrl+e":
			return m.openReactPalette()
		case "ctrl+v":
			if m.activeID != "" {
				if !m.canSend() {
					m.err = "Google Messages is not connected — press p to pair or r to reconnect"
					return m, nil
				}
				m.info = "Checking clipboard…"
				return m, m.attachClipboardCmd(m.activeID, captionForAttach(m.compose.Value()))
			}
		case "tab":
			return m, m.cycleFocus(1)
		case "shift+tab":
			return m, m.cycleFocus(-1)
		case "[":
			if m.focus == focusList {
				return m.cycleRiver(-1)
			}
		case "]":
			if m.focus == focusList {
				return m.cycleRiver(1)
			}
		case "j", "down":
			if m.focus == focusList {
				m.list.move(1)
				return m, nil
			}
		case "k", "up":
			if m.focus == focusList {
				m.list.move(-1)
				return m, nil
			}
		case "pgdown":
			if m.focus == focusList {
				m.list.move(m.list.pageSize())
				return m, nil
			}
		case "pgup":
			if m.focus == focusList {
				m.list.move(-m.list.pageSize())
				return m, nil
			}
		case " ":
			if m.focus == focusList {
				if item, ok := m.list.selected(); ok {
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
			if m.list.filtering || m.list.filter != "" {
				m.list.clearFilter()
				m.flushPendingConversations()
				m.info = ""
				return m, nil
			}
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
			m.compose.Blur()
			m.err = ""
			m.info = ""
			return m, nil
		}
	}

	// Forward non-key messages (cursor blink, etc.) to the focused pane.
	var cmd tea.Cmd
	switch m.focus {
	case focusCompose:
		m.compose, cmd = m.compose.Update(msg)
		return m, cmd
	case focusThread:
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
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
				m.err = "Google Messages is not connected — press p to pair or r to reconnect"
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
		if m.threadRootID != "" {
			return m.leaveSlackThread()
		}
		m.focus = focusList
		m.compose.Blur()
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
	case "ctrl+t":
		// Bare letters are reserved for typing (and Shift is unusable as a
		// modifier on Windows Terminal message panes). Match Ctrl+O / Ctrl+A.
		return m.openSelectedSlackThread()
	case "pgup", "ctrl+u":
		if m.activeRiverProvider() == "slack" && m.threadRootID == "" {
			m.info = "Loading older Slack history…"
			return m, m.fetchOlderSlackHistoryCmd(m.activeID)
		}
	case "ctrl+e", "e":
		// Ctrl+E works from compose too; bare e is only reachable in thread focus.
		return m.openReactPalette()
	case "o", "ctrl+o":
		if m.activeID != "" {
			m.info = "Opening media…"
			return m, m.mediaActionCmd(false)
		}
	case "s", "ctrl+s":
		if m.activeID != "" {
			m.info = "Saving media…"
			return m, m.mediaActionCmd(true)
		}
	case "a", "ctrl+a":
		if m.activeID != "" {
			if !m.canSend() {
				m.err = "Google Messages is not connected — press p to pair or r to reconnect"
				return m, nil
			}
			m.info = "Attach file…"
			return m, m.attachPickerCmd(m.activeID, captionForAttach(m.compose.Value()))
		}
	case "ctrl+v":
		if m.activeID != "" {
			if !m.canSend() {
				m.err = "Google Messages is not connected — press p to pair or r to reconnect"
				return m, nil
			}
			m.info = "Checking clipboard…"
			return m, m.attachClipboardCmd(m.activeID, captionForAttach(m.compose.Value()))
		}
	case "r", "ctrl+r":
		m.info = "Reconnecting…"
		return m, m.reconnectCmd()
	case "p":
		if m.googleNeedsPair() {
			return m.openPairOverlay()
		}
	case "/":
		return m.startJumpFilter()
	case "ctrl+f":
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
	case "p":
		// Status bar advertises "press p to pair". Honor it from an empty
		// composer; a non-empty draft still types the letter.
		if m.googleNeedsPair() && strings.TrimSpace(m.compose.Value()) == "" {
			return m.openPairOverlay()
		}
	case "ctrl+o":
		// Bare letters always type in the composer — the old empty-draft special
		// case ate the first "o"/"s" of a new message as a media action.
		if m.activeID == "" {
			return m, nil
		}
		m.info = "Opening media…"
		return m, m.mediaActionCmd(false)
	case "ctrl+s":
		if m.activeID == "" {
			return m, nil
		}
		m.info = "Saving media…"
		return m, m.mediaActionCmd(true)
	case "ctrl+a":
		// Bare "a" must remain typable in the composer; Ctrl+A attaches.
		if m.activeID == "" {
			return m, nil
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press p to pair or r to reconnect"
			return m, nil
		}
		m.info = "Attach file…"
		return m, m.attachPickerCmd(m.activeID, captionForAttach(m.compose.Value()))
	case "ctrl+t":
		return m.openSelectedSlackThread()
	case "ctrl+e":
		return m.openReactPalette()
	case "ctrl+f":
		m.focus = focusSearch
		m.query.SetValue("")
		m.query.Focus()
		m.compose.Blur()
		return m, nil
	case "pgup", "ctrl+u":
		if m.activeRiverProvider() == "slack" && m.threadRootID == "" && m.activeID != "" {
			m.info = "Loading older Slack history…"
			return m, m.fetchOlderSlackHistoryCmd(m.activeID)
		}
	case "ctrl+r":
		m.info = "Reconnecting…"
		return m, m.reconnectCmd()
	case "ctrl+v":
		if m.activeID == "" {
			break
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press p to pair or r to reconnect"
			return m, nil
		}
		m.info = "Checking clipboard…"
		return m, m.attachClipboardCmd(m.activeID, captionForAttach(m.compose.Value()))
	case "enter":
		if m.sending {
			return m, nil
		}
		body := strings.TrimSpace(m.compose.Value())
		if body == "" {
			return m, nil
		}
		if !m.canSend() {
			m.err = "Google Messages is not connected — press p to pair or r to reconnect"
			return m, nil
		}
		if ids := m.broadcastIDList(); len(ids) > 0 {
			if _, isPath := looksLikeExistingFile(body); isPath {
				m.err = "broadcast is text-only — clear multi-select (c) to send media"
				return m, nil
			}
			m.info = fmt.Sprintf("Sending to %d chats…", len(ids))
			m.sending = true
			cmd := m.sendBroadcastCmd(ids, body)
			m.clearComposeOptimistically()
			return m, cmd
		}
		if m.activeID == "" {
			return m, nil
		}
		if path, ok := looksLikeExistingFile(body); ok {
			m.info = "Sending media…"
			m.sending = true
			cmd := m.sendMediaCmd(m.activeID, path, "")
			m.clearComposeOptimistically()
			return m, cmd
		}
		m.sending = true
		cmd := m.sendCmd(m.activeID, body, m.threadRootID)
		m.clearComposeOptimistically()
		return m, cmd
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.compose, cmd = m.compose.Update(msg)
	prevH := m.composeHeight
	m.syncComposeHeight()
	if m.composeHeight != prevH {
		m.syncComposeViewport()
	}
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

func (m Model) startJumpFilter() (tea.Model, tea.Cmd) {
	m.focus = focusList
	m.compose.Blur()
	m.query.Blur()
	m.reactPalette = false
	m.err = ""
	m.info = "Type to jump — Enter opens, Esc clears"
	m.list.startFilter()
	return m, nil
}

func (m Model) updateListFilterKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		item, ok := m.list.selected()
		if !ok {
			return m, nil
		}
		m.list.clearFilter()
		m.flushPendingConversations()
		m.info = ""
		return m.openConversation(item.conv.ConversationID, item.conv.Name, item.conv.Participants)
	case "esc":
		m.list.clearFilter()
		m.flushPendingConversations()
		m.info = ""
		return m, nil
	case "backspace":
		if m.list.filter == "" {
			m.list.clearFilter()
			m.flushPendingConversations()
			return m, nil
		}
		r := []rune(m.list.filter)
		m.list.setFilter(string(r[:len(r)-1]))
		return m, nil
	case "ctrl+c", "q":
		return m, tea.Quit
	case "down", "j":
		m.list.move(1)
		return m, nil
	case "up", "k":
		m.list.move(-1)
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.list.setFilter(m.list.filter + string(msg.Runes))
		return m, nil
	}
	return m, nil
}

func (m *Model) applyConversations(msg []localapi.Conversation) {
	selectedID := ""
	if item, ok := m.list.selected(); ok {
		selectedID = item.conv.ConversationID
	}
	items := make([]convItem, 0, len(msg))
	for _, c := range msg {
		_, selected := m.broadcastIDs[c.ConversationID]
		items = append(items, convItem{conv: c, selected: selected})
		if m.activeID != "" && c.ConversationID == m.activeID {
			m.activeName = c.Name
			m.activeParticipants = c.Participants
		}
	}
	m.list.setItems(items)
	m.list.selectID(selectedID)
	m.syncComposePlaceholder()
}

func (m *Model) flushPendingConversations() {
	if !m.pendingConvsSet {
		return
	}
	m.pendingConvsSet = false
	pending := m.pendingConvs
	m.pendingConvs = nil
	m.applyConversations(pending)
}

func (m *Model) scheduleConversationsRefresh() tea.Cmd {
	m.convRefreshGen++
	gen := m.convRefreshGen
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return debouncedConvRefreshMsg(gen)
	})
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

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	ev := tea.MouseEvent(msg)
	if !ev.IsWheel() {
		return m, nil
	}
	delta := 1
	if ev.Button == tea.MouseButtonWheelUp {
		delta = -1
	}

	// Prefer the pane under the cursor so wheel-over-contacts works even while
	// the composer is focused (opening a thread focuses compose).
	d := m.paneDims()
	listRightEdge := d.leftW + borderStyle.GetHorizontalBorderSize()
	overList := ev.X < listRightEdge && m.focus != focusSearch

	switch {
	case m.focus == focusSearch:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	case overList:
		if m.list.filtering {
			return m, nil
		}
		m.list.move(delta)
		return m, nil
	default:
		if delta < 0 {
			m.viewport.LineUp(1)
		} else {
			m.viewport.LineDown(1)
		}
		return m, nil
	}
}

func (m Model) openSelectedConversation() (tea.Model, tea.Cmd) {
	item, ok := m.list.selected()
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
	m.threadRootID = ""
	m.channelMessages = nil
	m.selectedMsg = -1
	m.reactPalette = false
	m.focus = focusCompose
	m.compose.SetValue(m.drafts[id])
	if m.viewportWidth > 0 {
		m.compose.SetWidth(m.viewportWidth)
	}
	if m.composeHeight > 0 {
		m.compose.SetHeight(m.composeHeight)
	}
	focusCmd := m.compose.Focus()
	m.syncComposeHeight()
	m.syncComposeViewport()
	m.setThreadContentFollow(paintCenteredLine(mutedStyle, "Loading…", m.viewportWidth))
	return m, tea.Batch(
		focusCmd,
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
	y := m.viewport.YOffset
	content = padLines(content, m.viewport.Height, max(1, m.viewportWidth))
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(y)
}

func (m *Model) setThreadContentFollow(content string) {
	content = padLines(content, m.viewport.Height, max(1, m.viewportWidth))
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m Model) View() string {
	if !m.ready {
		return "Starting OM-TUI…"
	}
	status := renderStatus(m.status, m.activeRiverName(), m.err, m.info, m.activeSlackBadge(), m.list.totalUnread(), m.width)
	help := renderContextHelpStyled(m)

	d := m.paneDims()
	// lipgloss Height is content-box (borders add outside). MaxHeight caps the
	// final rendered block including borders — using mainH for both clipped the
	// bottom border and two list rows, which left Windows Terminal ghosts of the
	// first contact's preview above/below the name while scrolling.
	paneMaxH := d.mainH + borderStyle.GetVerticalBorderSize()
	leftBorder, rightBorder := dimBorderStyle, dimBorderStyle
	switch m.focus {
	case focusList:
		leftBorder = focusBorderStyle
	case focusThread, focusCompose:
		rightBorder = focusBorderStyle
	}
	leftInner := padViewBox(m.list.View(), d.listInnerW, d.mainH)
	left := leftBorder.Width(d.leftW).Height(d.mainH).MaxHeight(paneMaxH).Render(leftInner)
	threadTitle := m.activeName
	if threadTitle == "" {
		threadTitle = "Thread"
	}
	if m.threadRootID != "" {
		threadTitle += " › thread"
	}
	innerW := d.threadInnerW
	// Pin both panes to exact cell heights. An over-tall viewport.View() used to
	// push the empty composer (placeholder) past MaxHeight and clip it away until
	// typing switched textarea off the placeholder path.
	threadVP := padViewBox(m.viewport.View(), innerW, d.viewportH)
	composer := padViewBox(m.compose.View(), innerW, d.composeH)
	threadBody := lipgloss.JoinVertical(lipgloss.Left,
		m.renderThreadTitleLine(threadTitle, innerW),
		threadVP,
		composer,
	)
	right := rightBorder.Width(d.rightW).Height(d.mainH).MaxHeight(paneMaxH).Render(threadBody)

	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	if m.focus == focusSearch {
		searchPane := lipgloss.JoinVertical(lipgloss.Left, m.query.View(), m.search.View())
		// Single pane: Width is content-only; border adds GetHorizontalBorderSize outside.
		searchW := m.width - borderStyle.GetHorizontalBorderSize()
		if searchW < 10 {
			searchW = 10
		}
		main = focusBorderStyle.Width(searchW).Height(d.mainH).MaxHeight(paneMaxH).Render(
			padViewBox(searchPane, searchW-borderStyle.GetHorizontalPadding(), d.mainH),
		)
	}

	frame := lipgloss.JoinVertical(lipgloss.Left,
		paintLine(lipgloss.NewStyle(), status, m.width),
		main,
		paintLine(lipgloss.NewStyle(), help, m.width),
	)
	// Exact terminal fill — short lines from a prior frame are the usual WT ghost source.
	placed := lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, frame,
		lipgloss.WithWhitespaceChars(" "),
	)
	if m.palette.open {
		placed = overlayCenter(placed, m.renderPaletteOverlay(), m.width, m.height)
	}
	if m.pair.open {
		placed = overlayCenter(placed, m.renderPairOverlay(), m.width, m.height)
	}
	// Disable autowrap (DECAWM) for the whole frame. A single contact name or SMS
	// preview containing a grapheme Windows Terminal renders wider than we measure
	// (ZWJ/flag/skin-tone emoji) would otherwise wrap one physical line, push the
	// frame a row past the viewport, scroll the alt screen, and leave the ghosts
	// we keep chasing. With autowrap off the extra cells clip at the right margin
	// instead. Run() restores it (?7h) on exit so the user's shell still wraps.
	return decawmOff + placed
}

// paneDims sizes bordered panes so content Width/Height plus outside borders
// fit exactly in the terminal. lipgloss Width/Height are content-box only;
// NormalBorder adds 2 cols / 2 rows outside that box.
type paneDims struct {
	leftW, rightW int
	mainH         int
	listInnerW    int
	threadInnerW  int
	composeH      int
	viewportH     int
}

func (m Model) paneDims() paneDims {
	borderX := borderStyle.GetHorizontalBorderSize()
	borderY := borderStyle.GetVerticalBorderSize()
	padX := borderStyle.GetHorizontalPadding()

	availW := m.width - 2*borderX
	if availW < 16 {
		availW = 16
	}
	leftW := leftWidth(availW)
	rightW := availW - leftW
	if rightW < 20 {
		rightW = min(20, availW/2)
		if rightW < 10 {
			rightW = max(1, availW/2)
		}
		leftW = availW - rightW
	}

	mainH := m.height - 2 - borderY // status + help + vertical pane borders
	if mainH < 5 {
		mainH = 5
	}
	composeH := m.composeHeight
	if composeH < 1 {
		composeH = 3
	}
	viewportH := mainH - 1 - composeH // thread title + compose
	if viewportH < 3 {
		viewportH = 3
	}
	return paneDims{
		leftW:        leftW,
		rightW:       rightW,
		mainH:        mainH,
		listInnerW:   max(1, leftW-padX),
		threadInnerW: max(1, rightW-padX),
		composeH:     composeH,
		viewportH:    viewportH,
	}
}

func (m *Model) layout() {
	d := m.paneDims()
	m.list.setSize(d.listInnerW, d.mainH)
	searchW := m.width - borderStyle.GetHorizontalBorderSize() - borderStyle.GetHorizontalPadding()
	if searchW < 10 {
		searchW = 10
	}
	m.search.SetSize(searchW, max(5, d.mainH-2))
	m.viewportWidth = d.threadInnerW
	m.viewport.Width = d.threadInnerW
	m.viewport.Height = d.viewportH
	m.compose.SetWidth(d.threadInnerW)
	m.compose.SetHeight(d.composeH)
}

const (
	composeMinHeight = 1
	composeMaxHeight = 6
)

// syncComposeHeight grows/shrinks the composer between composeMinHeight and
// MaxHeight so wrapped drafts stay readable instead of scrolling out of the
// box into the void. Idle/empty stays at composeMinHeight (1 row) so the
// thread pane keeps the rest of the vertical space.
func (m *Model) syncComposeHeight() {
	maxH := m.compose.MaxHeight
	if maxH < composeMinHeight {
		maxH = composeMaxHeight
	}
	w := m.compose.Width()
	if w < 1 {
		w = max(1, m.viewportWidth-2)
	}
	lines := len(wrapText(m.compose.Value(), w))
	if lines < 1 {
		lines = 1
	}
	h := lines
	if h < composeMinHeight {
		h = composeMinHeight
	}
	if h > maxH {
		h = maxH
	}
	if h == m.composeHeight && m.compose.Height() == h {
		return
	}
	m.composeHeight = h
	if m.width > 0 && m.height > 0 {
		m.layout()
		if m.activeID != "" && len(m.messages) > 0 {
			m.setThreadContent(m.renderActiveThread())
		}
	} else {
		m.compose.SetHeight(h)
	}
}

// clearComposeOptimistically clears the composer immediately on send
// dispatch, matching every other chat app, instead of waiting for the
// send's own round-trip. Waiting was the source of a visible "duplicate"
// bug: the SSE-driven message refresh and the send's direct HTTP response
// are two independent round-trips with no ordering guarantee, so the sent
// message could already be showing in the thread while the composer still
// displayed the same text it was mid-flight on. sendFailedMsg restores the
// text if the send actually fails.
func (m *Model) clearComposeOptimistically() {
	m.compose.SetValue("")
	m.syncComposeHeight()
	m.syncComposeViewport()
}

// syncComposeViewport refreshes the textarea line cache and scrolls so the
// cursor stays visible. bubbles/textarea only repositions against lines
// populated by View(); SetValue resets YOffset to 0 without scrolling back.
func (m *Model) syncComposeViewport() {
	_ = m.compose.View()
	if !m.compose.Focused() {
		return
	}
	var cmd tea.Cmd
	m.compose, cmd = m.compose.Update(tea.KeyMsg{Type: tea.KeyEnd})
	_ = cmd
}

func leftWidth(total int) int {
	w := total * 2 / 5
	if w < 24 {
		w = 24
	}
	if w > 56 {
		w = 56
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
	switch m.activeRiverProvider() {
	case "slack":
		return true
	default:
		g := m.status.Google
		if g.NeedsPairing || !g.Paired {
			return false
		}
		if g.NeedsRepair || g.AuthExpired {
			return false
		}
		return g.Connected || m.status.Connected
	}
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
	riverID := m.activeRiverID
	if riverID == "" {
		riverID = "messages-default"
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		convs, err := m.session.Client.ListConversationsByRiver(ctx, riverID, 200)
		if err != nil {
			return errMsg{err: err}
		}
		return conversationsMsg(convs)
	}
}

func (m Model) refreshRiversCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rivers, err := m.session.Client.ListRivers(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return riversMsg(rivers)
	}
}

func (m Model) cycleRiver(dir int) (tea.Model, tea.Cmd) {
	if len(m.rivers) == 0 {
		return m, m.refreshRiversCmd()
	}
	idx := 0
	for i, r := range m.rivers {
		if r.ID == m.activeRiverID {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(m.rivers)) % len(m.rivers)
	m.activeRiverID = m.rivers[idx].ID
	m.list.title = m.rivers[idx].DisplayName
	m.list.clearFilter()
	m.clearBroadcast()
	m.restampConversationList()
	m.syncComposePlaceholder()
	m.info = fmt.Sprintf("River: %s", m.rivers[idx].DisplayName)
	m.activeID = ""
	m.activeName = ""
	m.messages = nil
	m.threadRootID = ""
	m.channelMessages = nil
	m.setThreadContentFollow(paintCenteredLine(mutedStyle, "Select a stream.", m.viewportWidth))
	return m, m.refreshConversationsCmd()
}

func (m Model) activeRiverName() string {
	for _, r := range m.rivers {
		if r.ID == m.activeRiverID {
			return r.DisplayName
		}
	}
	if m.activeRiverID == "messages-default" {
		return "Messages"
	}
	return m.activeRiverID
}

func (m Model) activeRiverProvider() string {
	for _, r := range m.rivers {
		if r.ID == m.activeRiverID {
			return r.Provider
		}
	}
	return "messages"
}

// activeSlackBadge renders a small live/poll indicator for the active Slack
// river, or "" when the active river isn't Slack. socket_connected means
// Socket Mode is actually delivering events right now; socket_configured
// but not connected means the reconnect loop (internal/slacklive/socket.go)
// is between attempts; no app token at all means it's on 45s polling only.
func (m Model) activeSlackBadge() string {
	if m.activeRiverProvider() != "slack" {
		return ""
	}
	for _, s := range m.status.Slack {
		if s.RiverID != m.activeRiverID {
			continue
		}
		switch {
		case s.SocketConnected:
			return okStyle.Render("● live")
		case s.SocketConfigured:
			return warnStyle.Render("○ reconnecting")
		default:
			return mutedStyle.Render("○ poll")
		}
	}
	return ""
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

func (m Model) openReactPalette() (tea.Model, tea.Cmd) {
	if _, ok := selectedMessage(m.messages, m.selectedMsg); !ok {
		m.err = "no message to react to"
		return m, nil
	}
	m.focus = focusThread
	m.compose.Blur()
	m.reactPalette = true
	m.err = ""
	m.info = "React: " + reactPaletteHelp()
	return m, nil
}

func (m Model) openSelectedSlackThread() (tea.Model, tea.Cmd) {
	if m.activeRiverProvider() != "slack" || m.threadRootID != "" || m.activeID == "" {
		return m, nil
	}
	target, ok := selectedMessage(m.messages, m.selectedMsg)
	if !ok || target.MessageID == "" {
		m.err = "no Slack message selected"
		return m, nil
	}
	rootID := target.MessageID
	if target.ReplyToID != "" {
		rootID = target.ReplyToID
	}
	m.channelMessages = append([]localapi.Message(nil), m.messages...)
	m.threadRootID = rootID
	m.messages = nil
	m.selectedMsg = -1
	m.setThreadContentFollow(paintCenteredLine(mutedStyle, "Loading Slack thread…", m.viewportWidth))
	m.info = "Loading Slack thread…"
	return m, m.fetchSlackThreadCmd(m.activeID, rootID)
}

func (m Model) fetchSlackThreadCmd(conversationID, rootMessageID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msgs, err := m.session.Client.SlackThread(ctx, conversationID, rootMessageID)
		if err != nil {
			return errMsg{err: err}
		}
		return slackThreadMsg{conversationID: conversationID, rootMessageID: rootMessageID, messages: msgs}
	}
}

func (m Model) fetchOlderSlackHistoryCmd(conversationID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msgs, err := m.session.Client.FetchOlderSlackHistory(ctx, conversationID, 100)
		if err != nil {
			return errMsg{err: err}
		}
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		return olderSlackHistoryMsg{conversationID: conversationID, messages: msgs}
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
	riverID := m.activeRiverID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hits, err := m.session.Client.SearchMessagesByRiver(ctx, query, riverID, 50)
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

func (m Model) sendCmd(conversationID, body, replyToID string) tea.Cmd {
	slackRiver := m.activeRiverProvider() == "slack"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		status, _, err := m.session.Client.Status(ctx)
		if err != nil {
			return sendFailedMsg{conversationID: conversationID, body: body, err: err}
		}
		key, err := newIdempotencyKey()
		if err != nil {
			return sendFailedMsg{conversationID: conversationID, body: body, err: err}
		}
		if (status.V2Send || status.V2Primary) && !slackRiver {
			if _, err := m.session.Client.SubmitText(ctx, localapi.TextSubmission{
				ConversationID: conversationID,
				Body:           body,
				ReplyToID:      replyToID,
				IdempotencyKey: key,
			}); err != nil {
				return sendFailedMsg{conversationID: conversationID, body: body, err: err}
			}
		} else {
			if _, err := m.session.Client.LegacySendText(ctx, conversationID, body, replyToID, key); err != nil {
				return sendFailedMsg{conversationID: conversationID, body: body, err: err}
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
	selected := m.selectedMsg
	preferSelected := m.focus == focusThread
	client := m.session.Client
	return func() tea.Msg {
		msg, ok, err := resolveMediaMessage(msgs, selected, preferSelected)
		if err != nil {
			return errMsg{err: err}
		}
		if !ok {
			return errMsg{err: fmt.Errorf("no downloadable media in this thread")}
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

// resolveMediaMessage picks the attachment to open/save.
// Thread focus with an explicit selection never silently falls back to another
// message — that produced confusing API 404s on text-only / empty-body rows
// when an older MimeType-only stub was still in the thread.
func resolveMediaMessage(msgs []localapi.Message, selected int, preferSelected bool) (localapi.Message, bool, error) {
	if preferSelected {
		sel, sok := selectedMessage(msgs, selected)
		if !sok {
			return localapi.Message{}, false, fmt.Errorf("no message selected")
		}
		if !sel.HasDownloadableMedia() {
			if sel.HasMedia() {
				return localapi.Message{}, false, fmt.Errorf("selected message has no downloadable attachment")
			}
			return localapi.Message{}, false, fmt.Errorf("selected message has no media")
		}
		return sel, true, nil
	}
	msg, ok := latestDownloadableMediaMessage(msgs)
	return msg, ok, nil
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

// sameThreadTurn reports whether msg continues the same visual "turn" as
// prev: same sender (already resolved to who) and close enough in time that
// collapsing the repeated timestamp/name still reads as one block instead
// of hiding a real gap in the conversation.
func sameThreadTurn(prev, msg localapi.Message, who string) bool {
	if prev.ReplyToID != "" {
		return false
	}
	prevWho := "them"
	if prev.IsFromMe {
		prevWho = "you"
	} else if name := strings.TrimSpace(prev.SenderName); name != "" {
		prevWho = name
	}
	if prevWho != who {
		return false
	}
	const groupWindowMS = 5 * 60 * 1000
	gap := msg.TimestampMS - prev.TimestampMS
	return gap >= 0 && gap < groupWindowMS
}

// dayDividerText centers a "── Mon, Jan 2 ──" rule for a calendar-day
// boundary. Per-message prefixes show time-only (see renderMessages) — the
// divider is the sole place the date appears, instead of every line
// repeating it.
func dayDividerText(t time.Time, width int) string {
	label := t.Format("Mon, Jan 2")
	if t.Year() != time.Now().Year() {
		label = t.Format("Mon, Jan 2, 2006")
	}
	label = " " + label + " "
	labelW := cellWidth(label)
	if labelW >= width {
		return truncateCells(strings.TrimSpace(label), width)
	}
	side := (width - labelW) / 2
	return strings.Repeat("─", side) + label + strings.Repeat("─", width-labelW-side)
}

func renderMessages(msgs []localapi.Message, width, selected int, resolve func(string) string, peerName string) string {
	if width < 16 {
		width = 16
	}
	if len(msgs) == 0 {
		return paintCenteredLine(mutedStyle, "No messages yet.", width)
	}
	selected = clampMessageIndex(len(msgs), selected)
	var b strings.Builder
	var lastDay string
	for i, msg := range msgs {
		local := time.UnixMilli(msg.TimestampMS).Local()
		if day := local.Format("2006-01-02"); day != lastDay {
			b.WriteString(paintLine(dimStyle, dayDividerText(local, width), width))
			b.WriteByte('\n')
			lastDay = day
		}
		ts := local.Format("15:04")
		who := "them"
		if msg.IsFromMe {
			who = "you"
		} else if name := strings.TrimSpace(msg.SenderName); name != "" {
			who = name
		}
		grouped := msg.ReplyToID == "" && i > 0 && sameThreadTurn(msgs[i-1], msg, who)
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
		if msg.ReplyCount > 0 {
			body += fmt.Sprintf("  [%d replies]", msg.ReplyCount)
		}
		marker := " "
		if i == selected {
			marker = ">"
		}
		prefix := fmt.Sprintf("%s %s  %s: ", marker, ts, who)
		if msg.ReplyToID != "" {
			prefix = fmt.Sprintf("%s ↳ %s  %s: ", marker, ts, who)
		}
		if grouped {
			// Same run as the previous message (same sender, close in time,
			// not a reply): keep the marker column live but drop the
			// repeated timestamp/name — Slack/iMessage-style turn grouping.
			blankW := cellWidth(prefix) - cellWidth(marker) - 1
			if blankW < 0 {
				blankW = 0
			}
			prefix = marker + " " + strings.Repeat(" ", blankW)
		}
		prefixW := cellWidth(prefix)
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
	text = truncateCells(text, width)
	if pad := width - cellWidth(text); pad > 0 {
		text += strings.Repeat(" ", pad)
	}
	return style.Render(text)
}

// paintCenteredLine is paintLine's centered sibling for empty/loading
// placeholder text ("No messages yet.", "Loading…") — space-padded evenly
// on both sides instead of left-aligned. Falls back to plain unpadded
// styling when width is unknown (<1), e.g. callers running before the
// first layout pass has sized anything.
func paintCenteredLine(style lipgloss.Style, text string, width int) string {
	if width < 1 {
		return style.Render(text)
	}
	text = truncateCells(text, width)
	textW := cellWidth(text)
	if textW >= width {
		return style.Render(text)
	}
	left := (width - textW) / 2
	right := width - textW - left
	return style.Render(strings.Repeat(" ", left) + text + strings.Repeat(" ", right))
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

// padViewBox pads/truncates each line to width and fills to height so WT
// differential redraws don't leave ghosts when list titles change length
// (e.g. unread badges appearing on inbound SSE).
func padViewBox(content string, width, height int) string {
	if width < 1 {
		width = 1
	}
	content = strings.TrimRight(content, "\n")
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}
	for i, line := range lines {
		lines[i] = padANSILine(line, width)
	}
	blank := strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, blank)
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func padANSILine(line string, width int) string {
	if width < 1 {
		return ""
	}
	w := lipgloss.Width(line)
	if w == width {
		return line
	}
	if w < width {
		return line + strings.Repeat(" ", width-w)
	}
	// Visible truncate without trying to preserve broken ANSI mid-sequence:
	// lipgloss Width+cut via rune walk on stripped content is lossy for styles,
	// but list lines rarely exceed the pane; prefer hard cut via MaxWidth.
	return lipgloss.NewStyle().MaxWidth(width).Width(width).Render(line)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderStatus builds the segmented status line: a connection dot, plain
// label, colored state, "│"-separated segments, and a right-aligned
// accent unread badge when there's anything unread. width is the target
// terminal width used only to right-align the badge — the final paintLine
// pass in View() still truncates/pads the whole line to the real width.
// composeCharLimitThreshold is how close to compose.CharLimit a draft has
// to get before the counter appears — quiet until it's actually relevant,
// rather than a permanent "0/4000" fixture.
const composeCharLimitThreshold = 500

// composeCharLimitBadge renders "N/limit" once a draft is within
// composeCharLimitThreshold characters of compose.CharLimit, in warn color
// once it's actually at the cap. Returns "" otherwise.
func (m Model) composeCharLimitBadge() string {
	limit := m.compose.CharLimit
	if limit <= 0 {
		return ""
	}
	used := len(m.compose.Value())
	if limit-used > composeCharLimitThreshold {
		return ""
	}
	style := mutedStyle
	if used >= limit {
		style = warnStyle
	}
	return style.Render(fmt.Sprintf("%d/%d", used, limit))
}

// renderThreadTitleLine is the thread pane's title row: the conversation
// name, plus a right-aligned char-limit counter once the draft is close to
// compose.CharLimit.
func (m Model) renderThreadTitleLine(title string, width int) string {
	titleText := truncate(title, width)
	line := titleStyle.Render(titleText)
	if badge := m.composeCharLimitBadge(); badge != "" {
		pad := width - cellWidth(titleText) - cellWidth(badge)
		if pad < 1 {
			pad = 1
		}
		line += strings.Repeat(" ", pad) + badge
	}
	return paintLine(lipgloss.NewStyle(), line, width)
}

func renderStatus(status localapi.DaemonStatus, riverName, errText, info, slackBadge string, unread, width int) string {
	g := status.Google
	state := "disconnected"
	style := warnStyle
	dot := "○"
	switch {
	case g.NeedsPairing || (!g.Paired && !status.Connected):
		state = "unpaired — press p to pair"
		style = warnStyle
	case g.NeedsRepair:
		state = "needs repair — press p to re-pair"
		style = warnStyle
	case g.AuthExpired:
		state = "auth expired"
		style = warnStyle
	case g.Connected || status.Connected:
		state = "connected"
		style = okStyle
		dot = "●"
		if !g.PhoneResponding {
			state = "connected (phone not responding)"
			style = warnStyle
			dot = "○"
		}
	}
	riverLabel := strings.TrimSpace(riverName)
	if riverLabel == "" {
		riverLabel = "Messages"
	}
	sep := dimStyle.Render(" │ ")
	left := style.Render(dot) + " Google Messages " + style.Render(state) +
		sep + mutedStyle.Render("river ") + riverLabel
	if slackBadge != "" {
		left += sep + slackBadge
	}
	if info != "" {
		left += sep + mutedStyle.Render(info)
	}
	if errText != "" {
		left += sep + errStyle.Render(errText)
	}
	if unread <= 0 {
		return left
	}
	badge := badgeStyle.Render(fmt.Sprintf("%d unread", unread))
	pad := width - cellWidth(left) - cellWidth(badge)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + badge
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return truncateCells(s, n)
}

func newIdempotencyKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// decawmOff disables terminal autowrap so an under-measured wide glyph clips at
// the right margin instead of wrapping the frame and scrolling the alt screen.
// decawmOn restores it. See View() for the ghost this prevents.
const (
	decawmOff = "\x1b[?7l"
	decawmOn  = "\x1b[?7h"
)

// Run launches the Bubble Tea program for session.
func Run(session *Session) error {
	model := NewModel(session)
	// Mouse cell motion keeps Windows Terminal from scrolling the alt screen
	// buffer on wheel (that scroll is what smears panes into each other).
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	// Frames render with autowrap disabled (see decawmOff); restore it no matter
	// how we exit so the user's shell keeps wrapping long lines.
	defer fmt.Fprint(os.Stdout, decawmOn)
	_, err := program.Run()
	return err
}

// colorAccent is the TUI's single accent hue (cyan) — already used for "me"
// messages and now reused consistently for focus/selection/badge chrome
// instead of introducing new colors per element.
const colorAccent = "81"

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	borderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	meStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))

	accentStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent))
	accentBoldStyle = accentStyle.Bold(true)
	dimStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	focusBorderStyle = borderStyle.BorderForeground(lipgloss.Color(colorAccent))
	dimBorderStyle   = borderStyle.BorderForeground(lipgloss.Color("240"))

	// badgeStyle renders the unread-count pill: accent text on a dim chip
	// background, distinct from both the name's own style and status colors.
	badgeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAccent)).Bold(true)
)
