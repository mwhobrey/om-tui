package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/river"
)

// tuiAction is a discoverable TUI command shared by the context help footer
// and the Ctrl+K command palette.
type tuiAction struct {
	ID       string
	Label    string
	Keys     []string // display chords; Keys[0] is the primary help chord
	Keywords []string
	Group    string
	InHelp   bool // curated footer; palette always includes available actions
	When     func(Model) bool
	Run      func(Model) (tea.Model, tea.Cmd)
}

func allActions() []tuiAction {
	return []tuiAction{
		{
			ID:     "quit",
			Label:  "Quit",
			Keys:   []string{"q"},
			Group:  "global",
			InHelp: true,
			When:   func(m Model) bool { return !m.reactPalette },
			Run:    func(m Model) (tea.Model, tea.Cmd) { return m, tea.Quit },
		},
		{
			ID:       "reconnect",
			Label:    "Reconnect",
			Keys:     []string{"ctrl+r"},
			Keywords: []string{"repair", "google", "pair", "whatsapp", "signal"},
			Group:    "global",
			When:     func(m Model) bool { return !m.reactPalette },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.info = "Reconnecting…"
				return m, m.reconnectCmd()
			},
		},
		{
			ID:       "pair",
			Label:    "Pair",
			Keys:     []string{"p"},
			Keywords: []string{"google", "pair", "cookie", "gaia", "account", "whatsapp", "signal", "qr"},
			Group:    "global",
			InHelp:   true,
			When:     func(m Model) bool { return !m.reactPalette && m.riverNeedsPair() },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openPairOverlay()
			},
		},
		{
			ID:       "quick-msg",
			Label:    "Quick message",
			Keys:     []string{">msg"},
			Keywords: []string{"msg", "dm", "quick", "send"},
			Group:    "global",
			When:     func(m Model) bool { return !m.reactPalette },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.info = "usage: >msg contact::message"
				return m, nil
			},
		},
		{
			ID:       "jump",
			Label:    "Jump filter",
			Keys:     []string{"/"},
			Keywords: []string{"filter", "find conversation"},
			Group:    "list",
			InHelp:   true,
			When: func(m Model) bool {
				return !m.reactPalette && (m.focus == focusList || m.focus == focusThread)
			},
			Run: func(m Model) (tea.Model, tea.Cmd) { return m.startJumpFilter() },
		},
		{
			ID:       "new-chat",
			Label:    "New chat",
			Keys:     []string{"n"},
			Keywords: []string{"new", "dm", "compose", "phone", "contact", "sms"},
			Group:    "list",
			InHelp:   true,
			When: func(m Model) bool {
				return m.canStartNewChat() && (m.focus == focusList || m.palette.open)
			},
			Run: func(m Model) (tea.Model, tea.Cmd) { return m.openNewChatOverlay() },
		},
		{
			ID:       "search",
			Label:    "Search messages",
			Keys:     []string{"ctrl+f"},
			Keywords: []string{"find", "msgs", "message search"},
			Group:    "global",
			InHelp:   true,
			When:     func(m Model) bool { return !m.reactPalette && m.focus != focusSearch },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.focus = focusSearch
				m.query.SetValue("")
				m.query.Focus()
				m.compose.Blur()
				return m, nil
			},
		},
		{
			ID:       "river-prev",
			Label:    "Previous river",
			Keys:     []string{"["},
			Keywords: []string{"workspace", "slack", "messages"},
			Group:    "list",
			When:     func(m Model) bool { return !m.reactPalette && m.focus == focusList },
			Run:      func(m Model) (tea.Model, tea.Cmd) { return m.cycleRiver(-1) },
		},
		{
			ID:       "river-next",
			Label:    "Next river",
			Keys:     []string{"]"},
			Keywords: []string{"workspace", "slack", "messages"},
			Group:    "list",
			When:     func(m Model) bool { return !m.reactPalette && m.focus == focusList },
			Run:      func(m Model) (tea.Model, tea.Cmd) { return m.cycleRiver(1) },
		},
		{
			ID:       "add-whatsapp",
			Label:    "Add WhatsApp account",
			Keys:     []string{">add whatsapp"},
			Keywords: []string{"river", "pair", "account", "whatsapp"},
			Group:    "global",
			When:     func(m Model) bool { return !m.reactPalette },
			Run:      func(m Model) (tea.Model, tea.Cmd) { return m.addLiveRiver(river.ProviderWhatsApp) },
		},
		{
			ID:       "add-signal",
			Label:    "Add Signal account",
			Keys:     []string{">add signal"},
			Keywords: []string{"river", "pair", "account", "signal"},
			Group:    "global",
			When:     func(m Model) bool { return !m.reactPalette },
			Run:      func(m Model) (tea.Model, tea.Cmd) { return m.addLiveRiver(river.ProviderSignal) },
		},
		{
			ID:     "river",
			Label:  "river",
			Keys:   []string{"[ ]"},
			Group:  "list",
			InHelp: true,
			When:   func(m Model) bool { return !m.reactPalette && m.focus == focusList },
			Run:    func(m Model) (tea.Model, tea.Cmd) { return m.cycleRiver(1) },
		},
		{
			ID:       "open-conversation",
			Label:    "Open conversation",
			Keys:     []string{"enter"},
			Keywords: []string{"open", "thread", "chat"},
			Group:    "list",
			InHelp:   true,
			When: func(m Model) bool {
				if m.reactPalette || m.focus != focusList {
					return false
				}
				_, ok := m.list.selected()
				return ok
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				item, ok := m.list.selected()
				if !ok {
					return m, nil
				}
				return m.openConversation(item.conv.ConversationID, item.conv.Name, item.conv.Participants)
			},
		},
		{
			ID:       "broadcast-toggle",
			Label:    "Toggle broadcast select",
			Keys:     []string{"space"},
			Keywords: []string{"multi", "select", "broadcast"},
			Group:    "list",
			InHelp:   true,
			When:     func(m Model) bool { return !m.reactPalette && m.focus == focusList },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				item, ok := m.list.selected()
				if !ok {
					return m, nil
				}
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
			},
		},
		{
			ID:       "broadcast-clear",
			Label:    "Clear broadcast select",
			Keys:     []string{"c"},
			Keywords: []string{"multi", "deselect"},
			Group:    "list",
			When:     func(m Model) bool { return !m.reactPalette && m.focus == focusList && m.broadcastCount() > 0 },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.clearBroadcast()
				m.restampConversationList()
				m.syncComposePlaceholder()
				m.info = "Broadcast cleared"
				return m, nil
			},
		},
		{
			ID:       "broadcast-compose",
			Label:    "Compose to selected chats",
			Keys:     []string{"m"},
			Keywords: []string{"broadcast", "multi", "message all"},
			Group:    "list",
			When:     func(m Model) bool { return !m.reactPalette && m.focus == focusList && m.broadcastCount() > 0 },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.focus = focusCompose
				return m, m.compose.Focus()
			},
		},
		{
			ID:       "focus-composer",
			Label:    "Focus composer",
			Keys:     []string{"tab"},
			Keywords: []string{"compose", "write", "draft"},
			Group:    "compose",
			When: func(m Model) bool {
				return !m.reactPalette && m.focus != focusCompose && (m.activeID != "" || m.broadcastCount() > 0)
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.focus = focusCompose
				m.query.Blur()
				return m, m.compose.Focus()
			},
		},
		{
			ID:     "send",
			Label:  "send",
			Keys:   []string{"enter"},
			Group:  "compose",
			InHelp: true,
			When: func(m Model) bool {
				return !m.reactPalette && m.focus == focusCompose && (m.activeID != "" || m.broadcastCount() > 0)
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				// Discovery only for Enter-send; keep focus and let the user press Enter.
				m.info = "Type a message and press Enter to send"
				return m, nil
			},
		},
		{
			ID:       "back",
			Label:    "back",
			Keys:     []string{"esc"},
			Keywords: []string{"list", "channel", "leave", "close"},
			Group:    "global",
			InHelp:   true,
			When: func(m Model) bool {
				if m.reactPalette {
					return true
				}
				switch m.focus {
				case focusCompose, focusThread, focusSearch:
					return true
				default:
					return m.list.filtering
				}
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				if m.reactPalette {
					m.reactPalette = false
					m.info = ""
					return m, nil
				}
				if m.threadRootID != "" {
					return m.leaveSlackThread()
				}
				if m.focus == focusSearch {
					m.focus = focusList
					m.query.Blur()
					m.search.SetItems(nil)
					return m, nil
				}
				if m.focus == focusCompose || m.focus == focusThread {
					m.focus = focusList
					m.compose.Blur()
					m.err = ""
					m.info = ""
					return m, nil
				}
				return m, nil
			},
		},
		{
			ID:       "slack-thread",
			Label:    "Open Slack thread",
			Keys:     []string{"ctrl+t"},
			Keywords: []string{"reply", "thread", "slack", "replies"},
			Group:    "slack",
			InHelp:   true,
			When: func(m Model) bool {
				if m.reactPalette || m.activeRiverProvider() != "slack" || m.threadRootID != "" || m.activeID == "" {
					return false
				}
				if m.focus != focusCompose && m.focus != focusThread {
					return false
				}
				_, ok := selectedMessage(m.messages, m.selectedMsg)
				return ok
			},
			Run: func(m Model) (tea.Model, tea.Cmd) { return m.openSelectedSlackThread() },
		},
		{
			ID:       "slack-older",
			Label:    "Load older Slack history",
			Keys:     []string{"pgup"},
			Keywords: []string{"history", "older", "page up", "ctrl+u"},
			Group:    "slack",
			InHelp:   true,
			When: func(m Model) bool {
				return !m.reactPalette &&
					m.activeRiverProvider() == "slack" &&
					m.threadRootID == "" &&
					m.activeID != "" &&
					(m.focus == focusCompose || m.focus == focusThread)
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.info = "Loading older Slack history…"
				return m, m.fetchOlderSlackHistoryCmd(m.activeID)
			},
		},
		{
			ID:       "leave-slack-thread",
			Label:    "Leave Slack thread",
			Keys:     []string{"esc"},
			Keywords: []string{"channel", "back", "close thread"},
			Group:    "slack",
			When: func(m Model) bool {
				return !m.reactPalette && m.threadRootID != "" && (m.focus == focusCompose || m.focus == focusThread)
			},
			Run: func(m Model) (tea.Model, tea.Cmd) { return m.leaveSlackThread() },
		},
		{
			ID:       "react",
			Label:    "React to message",
			Keys:     []string{"ctrl+e"},
			Keywords: []string{"emoji", "reaction", "react"},
			Group:    "thread",
			InHelp:   true,
			When: func(m Model) bool {
				if m.reactPalette || m.activeID == "" {
					return false
				}
				if m.activeRiverProvider() == "slack" && !m.status.V2Primary {
					return false
				}
				if m.focus != focusCompose && m.focus != focusThread {
					return false
				}
				_, ok := selectedMessage(m.messages, m.selectedMsg)
				return ok
			},
			Run: func(m Model) (tea.Model, tea.Cmd) { return m.openReactPalette() },
		},
		{
			ID:       "block-kit",
			Label:    "Block Kit actions",
			Keys:     []string{"ctrl+b"},
			Keywords: []string{"button", "slack", "blocks", "interactive"},
			Group:    "thread",
			InHelp:   true,
			When: func(m Model) bool {
				if m.reactPalette || m.blockPalette || m.activeID == "" {
					return false
				}
				if m.activeRiverProvider() != "slack" {
					return false
				}
				if m.focus != focusCompose && m.focus != focusThread {
					return false
				}
				target, ok := selectedMessage(m.messages, m.selectedMsg)
				return ok && len(selectedBlockActions(target)) > 0
			},
			Run: func(m Model) (tea.Model, tea.Cmd) { return m.openBlockPalette() },
		},
		{
			ID:       "open-media",
			Label:    "Open media",
			Keys:     []string{"ctrl+o"},
			Keywords: []string{"attachment", "image", "download", "view"},
			Group:    "media",
			InHelp:   true,
			When:     func(m Model) bool { return !m.reactPalette && m.activeID != "" && m.focus != focusSearch },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.info = "Opening media…"
				return m, m.mediaActionCmd(false)
			},
		},
		{
			ID:       "save-media",
			Label:    "Save media",
			Keys:     []string{"ctrl+s"},
			Keywords: []string{"attachment", "export", "download"},
			Group:    "media",
			When:     func(m Model) bool { return !m.reactPalette && m.activeID != "" && m.focus != focusSearch },
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m.info = "Saving media…"
				return m, m.mediaActionCmd(true)
			},
		},
		{
			ID:       "attach",
			Label:    "Attach file",
			Keys:     []string{"ctrl+a"},
			Keywords: []string{"upload", "send file", "picker"},
			Group:    "media",
			When: func(m Model) bool {
				return !m.reactPalette && m.activeID != "" && m.focus != focusSearch && m.focus != focusList && !m.slackFileSendBlocked()
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.beginAttach(false)
			},
		},
		{
			ID:       "clipboard-attach",
			Label:    "Attach from clipboard",
			Keys:     []string{"ctrl+v"},
			Keywords: []string{"paste", "screenshot", "clipboard", "image"},
			Group:    "media",
			When: func(m Model) bool {
				return !m.reactPalette && m.activeID != "" && (m.focus == focusCompose || m.focus == focusThread) && !m.slackFileSendBlocked()
			},
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.beginAttach(true)
			},
		},
	}
}

// actionContext returns a model view for When/help evaluation while the
// palette is open (focus was blurred for exclusive key routing).
func (m Model) actionContext() Model {
	if m.palette.open {
		m.focus = m.palette.prevFocus
	}
	return m
}

func availableActions(m Model) []tuiAction {
	ctx := m.actionContext()
	all := allActions()
	out := make([]tuiAction, 0, len(all))
	seen := make(map[string]bool, len(all))
	for _, a := range all {
		if a.When != nil && !a.When(ctx) {
			continue
		}
		// "river" is help-only shorthand; palette prefers prev/next entries.
		if a.ID == "river" {
			continue
		}
		if a.ID == "send" {
			continue // Enter-send is documented in help only
		}
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out
}

func (m Model) leaveSlackThread() (tea.Model, tea.Cmd) {
	if m.threadRootID == "" {
		return m, nil
	}
	m.threadRootID = ""
	m.messages = m.channelMessages
	m.channelMessages = nil
	m.selectedMsg = clampMessageIndex(len(m.messages), len(m.messages)-1)
	m.setThreadContent(m.renderActiveThread())
	m.info = ""
	return m, m.refreshMessagesCmd(m.activeID, m.msgGeneration)
}

// helpPart is one chord+label pair in the context help footer. chord is
// empty when an entry has no dedicated key (rendered as a bare label).
type helpPart struct {
	chord string
	label string
}

// contextHelpParts computes the current footer content as structured
// chord/label pairs. renderContextHelp joins these as plain text (kept
// stable for tests and any plain-text consumer); renderContextHelpStyled
// renders the same parts with the accent/muted hierarchy used in View().
func contextHelpParts(m Model) []helpPart {
	ctx := m.actionContext()
	if ctx.blockPalette {
		return []helpPart{{chord: "1-9", label: "block"}, {chord: "esc", label: "cancel"}}
	}
	if ctx.reactPalette {
		return []helpPart{{chord: "1-9", label: "react"}, {chord: "esc", label: "cancel"}}
	}

	var parts []helpPart
	add := func(chord, label string) {
		if label == "" {
			return
		}
		parts = append(parts, helpPart{chord: chord, label: label})
	}

	if ctx.focus == focusSearch {
		add("esc", "list")
		add("enter", "open")
		add("ctrl+k", "commands")
		return parts
	}

	if ctx.threadRootID != "" && (ctx.focus == focusCompose || ctx.focus == focusThread) {
		add("esc", "channel")
		add("enter", "send")
		add("ctrl+k", "commands")
		return parts
	}

	helpActions := make([]tuiAction, 0, 12)
	for _, a := range allActions() {
		if !a.InHelp || a.When == nil || !a.When(ctx) {
			continue
		}
		helpActions = append(helpActions, a)
	}

	// Stable order for footer readability (not registry order alone).
	order := []string{
		"quit", "pair", "back", "river", "jump", "new-chat", "broadcast-toggle", "open-conversation",
		"send", "slack-thread", "react", "block-kit", "slack-older", "open-media", "search",
	}
	byID := make(map[string]tuiAction, len(helpActions))
	for _, a := range helpActions {
		byID[a.ID] = a
	}
	for _, id := range order {
		a, ok := byID[id]
		if !ok {
			continue
		}
		label := a.Label
		switch a.ID {
		case "quit":
			label = "quit"
		case "back":
			if ctx.focus == focusCompose || ctx.focus == focusThread {
				label = "list"
			} else {
				label = "back"
			}
		case "broadcast-toggle":
			label = "multi"
		case "open-conversation":
			label = "open"
		case "slack-thread":
			label = "thread"
		case "react":
			label = "react"
		case "block-kit":
			label = "blocks"
		case "slack-older":
			label = "older"
		case "open-media":
			label = "media"
		case "jump":
			label = "jump"
		case "new-chat":
			label = "new"
		case "search":
			label = "msgs"
		case "pair":
			label = "pair"
		}
		chord := ""
		if len(a.Keys) > 0 {
			chord = a.Keys[0]
		}
		add(chord, label)
	}
	add("ctrl+k", "commands")
	return parts
}

func renderContextHelp(m Model) string {
	parts := contextHelpParts(m)
	joined := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.chord == "" {
			joined = append(joined, p.label)
		} else {
			joined = append(joined, p.chord+" "+p.label)
		}
	}
	return strings.Join(joined, "  ")
}

// renderContextHelpStyled renders the same content as renderContextHelp but
// with chords bold in the accent color and labels muted, separated by a
// dim middot instead of raw double-spaces — so the chord and its action
// read as a scannable pair instead of one undifferentiated blob.
func renderContextHelpStyled(m Model) string {
	parts := contextHelpParts(m)
	rendered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.chord == "" {
			rendered = append(rendered, mutedStyle.Render(p.label))
			continue
		}
		rendered = append(rendered, accentBoldStyle.Render(p.chord)+" "+mutedStyle.Render(p.label))
	}
	return strings.Join(rendered, dimStyle.Render(" · "))
}
