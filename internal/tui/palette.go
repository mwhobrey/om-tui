package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/maxghenis/openmessage/internal/localapi"
)

const (
	paletteMaxWidth     = 60
	paletteMaxRows      = 12
	paletteConvsTTL     = 30 * time.Second
	paletteConvFetchLim = 500
	paletteKindConv     = "conv"
	paletteKindAction   = "action"
	paletteKindCustom   = "custom"
)

type paletteItem struct {
	Key      string
	Kind     string
	Label    string
	Sub      string
	Score    float64
	Conv     *localapi.Conversation
	ActionID string
	CustomID string
	Args     string
}

type commandPalette struct {
	open      bool
	filter    textinput.Model
	matches   []paletteItem
	cursor    int
	prevFocus focusPane
	mode      string // jump | commands
}

type paletteConvsMsg struct {
	conversations []localapi.Conversation
	err           error
}

func newCommandPalette() commandPalette {
	ti := textinput.New()
	ti.Placeholder = "jump…  or > commands"
	ti.CharLimit = 200
	ti.Prompt = ""
	ti.Width = paletteMaxWidth - 4
	return commandPalette{filter: ti, mode: "jump"}
}

func parsePaletteQuery(raw string) (mode, query string) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, ">") {
		return "commands", strings.TrimSpace(strings.TrimPrefix(s, ">"))
	}
	return "jump", s
}

func (m Model) openPalette() (tea.Model, tea.Cmd) {
	if m.palette.open {
		return m, nil
	}
	m.palette.prevFocus = m.focus
	m.palette.open = true
	m.palette.cursor = 0
	m.palette.filter.SetValue("")
	m.compose.Blur()
	m.query.Blur()
	m.reactPalette = false
	focusCmd := m.palette.filter.Focus()
	m.reloadCustomCommandsIfChanged()
	m.refreshPaletteMatches()
	m.info = ""
	m.err = ""
	var fetch tea.Cmd
	if m.paletteConvsNeedRefresh() {
		m.paletteConvsLoading = true
		fetch = m.fetchPaletteConvsCmd()
	}
	return m, tea.Batch(focusCmd, fetch)
}

func (m *Model) closePalette() tea.Cmd {
	if !m.palette.open {
		return nil
	}
	m.palette.open = false
	m.palette.filter.Blur()
	m.palette.filter.SetValue("")
	m.palette.matches = nil
	m.palette.cursor = 0
	m.palette.mode = "jump"
	var saveCmd tea.Cmd
	if m.frecency != nil {
		saveCmd = m.frecency.saveCmd()
	}
	m.focus = m.palette.prevFocus
	switch m.focus {
	case focusCompose:
		m.compose.Focus()
		m.query.Blur()
	case focusSearch:
		m.query.Focus()
		m.compose.Blur()
	default:
		m.compose.Blur()
		m.query.Blur()
	}
	return saveCmd
}

// reloadCustomCommandsIfChanged re-reads commands.json only when the file's
// mtime has moved since the last load, instead of re-parsing on every
// palette open. A parse error is surfaced via m.err rather than silently
// dropping the previously loaded commands.
func (m *Model) reloadCustomCommandsIfChanged() {
	dir := ""
	if m.session != nil {
		dir = m.session.DataDir
	}
	if strings.TrimSpace(dir) == "" {
		return
	}
	path := filepath.Join(dir, customCommandsFileName)
	info, statErr := os.Stat(path)
	if statErr != nil {
		if m.customCmds != nil || !m.customCmdsModTime.IsZero() {
			m.customCmds = nil
			m.customCmdsModTime = time.Time{}
		}
		return
	}
	if !m.customCmdsModTime.IsZero() && !info.ModTime().After(m.customCmdsModTime) {
		return
	}
	cmds, modTime, err := loadCustomCommands(dir)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.customCmds = cmds
	m.customCmdsModTime = modTime
}

func (m Model) paletteConvsNeedRefresh() bool {
	if len(m.paletteConvs) == 0 {
		return true
	}
	return time.Since(m.paletteConvsAt) > paletteConvsTTL
}

func (m Model) fetchPaletteConvsCmd() tea.Cmd {
	if m.session == nil || m.session.Client == nil {
		return func() tea.Msg {
			return paletteConvsMsg{conversations: nil}
		}
	}
	client := m.session.Client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		convs, err := client.ListConversations(ctx, paletteConvFetchLim)
		if err != nil {
			return paletteConvsMsg{err: err}
		}
		return paletteConvsMsg{conversations: convs}
	}
}

func (m *Model) refreshPaletteMatches() {
	mode, query := parsePaletteQuery(m.palette.filter.Value())
	m.palette.mode = mode
	var items []paletteItem
	if mode == "commands" {
		items = m.commandPaletteItems(query)
	} else {
		items = m.jumpPaletteItems(query)
	}
	m.palette.matches = items
	if m.palette.cursor >= len(m.palette.matches) {
		m.palette.cursor = len(m.palette.matches) - 1
	}
	if m.palette.cursor < 0 {
		m.palette.cursor = 0
	}
}

func (m Model) jumpPaletteItems(query string) []paletteItem {
	if m.paletteConvsLoading && len(m.paletteConvs) == 0 {
		return nil
	}
	convs := m.paletteConvs
	badges := m.riverBadgeMap()
	var items []paletteItem
	if query == "" {
		items = make([]paletteItem, 0, len(convs))
		for i := range convs {
			items = append(items, m.convPaletteItem(&convs[i], 0, badgeFor(badges, convs[i].RiverID)))
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Score != items[j].Score {
				return items[i].Score > items[j].Score
			}
			ci, cj := items[i].Conv, items[j].Conv
			if ci != nil && cj != nil {
				if ci.UnreadCount != cj.UnreadCount {
					return ci.UnreadCount > cj.UnreadCount
				}
				return ci.LastMessageTS > cj.LastMessageTS
			}
			return items[i].Label < items[j].Label
		})
		return truncatePaletteItems(items, 40)
	}

	if paletteQueryHasFilterTerms(query) || strings.Contains(query, " ") {
		for i := range convs {
			if conversationMatchesFilter(convs[i], query) {
				items = append(items, m.convPaletteItem(&convs[i], 0, badgeFor(badges, convs[i].RiverID)))
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Score > items[j].Score
		})
		return truncatePaletteItems(items, 40)
	}

	data := make([]string, len(convs))
	for i := range convs {
		data[i] = paletteConvHaystack(convs[i], badgeFor(badges, convs[i].RiverID))
	}
	found := fuzzy.Find(query, data)
	items = make([]paletteItem, 0, len(found))
	for rank, match := range found {
		items = append(items, m.convPaletteItem(&convs[match.Index], float64(len(found)-rank), badgeFor(badges, convs[match.Index].RiverID)))
	}
	sort.SliceStable(items, func(i, j int) bool {
		// Prefer fuzzy order already; frecency breaks ties via Score field mix
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Label < items[j].Label
	})
	return truncatePaletteItems(items, 40)
}

func paletteQueryHasFilterTerms(query string) bool {
	for _, t := range strings.Fields(strings.ToLower(query)) {
		if t == "is:unread" || strings.HasPrefix(t, "type:") {
			return true
		}
	}
	return false
}

func paletteConvHaystack(c localapi.Conversation, riverBadge string) string {
	return strings.Join([]string{
		conversationFilterValue(c),
		c.RiverID,
		riverBadge,
		c.StreamKind,
		c.SourcePlatform,
	}, " ")
}

func (m Model) convPaletteItem(c *localapi.Conversation, fuzzyBoost float64, badge string) paletteItem {
	cp := *c
	label := strings.TrimSpace(c.Name)
	if label == "" {
		label = c.ConversationID
	}
	key := "conv:" + c.ConversationID
	score := m.frecency.score(key) + fuzzyBoost
	return paletteItem{
		Key:   key,
		Kind:  paletteKindConv,
		Label: label,
		Sub:   badge,
		Score: score,
		Conv:  &cp,
	}
}

func (m Model) riverBadge(riverID string) string {
	riverID = strings.TrimSpace(riverID)
	if riverID == "" {
		return ""
	}
	for _, r := range m.rivers {
		if r.ID == riverID {
			return r.DisplayName
		}
	}
	if riverID == "messages-default" {
		return "Messages"
	}
	return riverID
}

// riverBadgeMap precomputes riverID -> display-name badges once so callers
// filtering/scoring many conversations don't re-scan m.rivers per item.
func (m Model) riverBadgeMap() map[string]string {
	badges := make(map[string]string, len(m.rivers)+1)
	for _, r := range m.rivers {
		badges[r.ID] = r.DisplayName
	}
	if _, ok := badges["messages-default"]; !ok {
		badges["messages-default"] = "Messages"
	}
	return badges
}

// badgeFor mirrors riverBadge's fallback (unknown non-empty river IDs display
// as themselves) but reads from a precomputed map instead of scanning rivers.
func badgeFor(badges map[string]string, riverID string) string {
	riverID = strings.TrimSpace(riverID)
	if riverID == "" {
		return ""
	}
	if b, ok := badges[riverID]; ok {
		return b
	}
	return riverID
}

func (m Model) commandPaletteItems(query string) []paletteItem {
	actions := availableActions(m)
	customs := m.customCmds
	type cand struct {
		item paletteItem
		hay  string
	}
	cands := make([]cand, 0, len(actions)+len(customs)+1)

	for _, a := range actions {
		if a.ID == "river" || a.ID == "send" {
			continue
		}
		args := extractCommandArgs(query, a.ID, a.Label, a.Keywords)
		chord := ""
		if len(a.Keys) > 0 {
			chord = a.Keys[0]
		}
		key := "action:" + a.ID
		cands = append(cands, cand{
			item: paletteItem{
				Key:      key,
				Kind:     paletteKindAction,
				Label:    a.Label,
				Sub:      chord,
				Score:    m.frecency.score(key),
				ActionID: a.ID,
				Args:     args,
			},
			hay: paletteHaystack(a),
		})
	}
	for _, c := range customs {
		args := extractCommandArgs(query, c.ID, c.Label, c.Keywords)
		key := "custom:" + c.ID
		cands = append(cands, cand{
			item: paletteItem{
				Key:      key,
				Kind:     paletteKindCustom,
				Label:    c.Label,
				Sub:      "custom",
				Score:    m.frecency.score(key),
				CustomID: c.ID,
				Args:     args,
			},
			hay: c.haystack(),
		})
	}

	if query == "" {
		items := make([]paletteItem, 0, len(cands))
		for _, c := range cands {
			items = append(items, c.item)
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Score != items[j].Score {
				return items[i].Score > items[j].Score
			}
			return items[i].Label < items[j].Label
		})
		return truncatePaletteItems(items, 40)
	}

	// Fuzzy on command stem (strip trailing args for matching).
	stem := commandMatchStem(query)
	data := make([]string, len(cands))
	for i, c := range cands {
		data[i] = c.hay
	}
	found := fuzzy.Find(stem, data)
	items := make([]paletteItem, 0, len(found))
	for rank, match := range found {
		c := cands[match.Index]
		it := c.item
		it.Score += float64(len(found) - rank)
		it.Args = extractCommandArgs(query, it.ActionID, it.Label, nil)
		if it.Kind == paletteKindCustom {
			for _, cc := range customs {
				if cc.ID == it.CustomID {
					it.Args = extractCommandArgs(query, cc.ID, cc.Label, cc.Keywords)
					break
				}
			}
		}
		if it.ActionID == "quick-msg" {
			it.Args = extractQuickMsgArgs(query)
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Label < items[j].Label
	})
	return truncatePaletteItems(items, 40)
}

func commandMatchStem(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return q
	}
	// Prefer text before :: for msg matching; else first token.
	if i := strings.Index(q, "::"); i >= 0 {
		before := strings.TrimSpace(q[:i])
		fields := strings.Fields(before)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return q
	}
	return fields[0]
}

func extractCommandArgs(query, id, label string, keywords []string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	lower := strings.ToLower(q)
	prefixes := []string{strings.ToLower(id)}
	if label != "" {
		if f := strings.Fields(label); len(f) > 0 {
			prefixes = append(prefixes, strings.ToLower(f[0]))
		}
	}
	for _, kw := range keywords {
		prefixes = append(prefixes, strings.ToLower(strings.TrimSpace(kw)))
	}
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if lower == p {
			return ""
		}
		if strings.HasPrefix(lower, p+" ") || strings.HasPrefix(lower, p+"\t") {
			return strings.TrimSpace(q[len(p):])
		}
	}
	return ""
}

func extractQuickMsgArgs(query string) string {
	q := strings.TrimSpace(query)
	lower := strings.ToLower(q)
	for _, p := range []string{"msg", "dm", "quick", "quick-msg"} {
		if lower == p {
			return ""
		}
		if strings.HasPrefix(lower, p+" ") || strings.HasPrefix(lower, p+"\t") {
			return strings.TrimSpace(q[len(p):])
		}
	}
	// Whole query may be contact::body without verb when already filtered to quick-msg
	if strings.Contains(q, "::") {
		return q
	}
	return ""
}

func parseQuickMsgArgs(args string) (contact, body string, ok bool) {
	args = strings.TrimSpace(args)
	idx := strings.LastIndex(args, "::")
	if idx < 0 {
		return "", "", false
	}
	contact = strings.TrimSpace(args[:idx])
	body = strings.TrimSpace(args[idx+2:])
	if contact == "" || body == "" {
		return "", "", false
	}
	return contact, body, true
}

func truncatePaletteItems(items []paletteItem, n int) []paletteItem {
	if n <= 0 || len(items) <= n {
		return items
	}
	return items[:n]
}

func paletteHaystack(a tuiAction) string {
	parts := []string{a.Label, a.ID, a.Group}
	parts = append(parts, a.Keys...)
	parts = append(parts, a.Keywords...)
	return strings.Join(parts, " ")
}

func (m Model) updatePaletteKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "ctrl+k":
		saveCmd := m.closePalette()
		return m, saveCmd
	case "enter":
		return m.runPaletteSelection()
	case "down", "j", "ctrl+n":
		if len(m.palette.matches) == 0 {
			return m, nil
		}
		m.palette.cursor = (m.palette.cursor + 1) % len(m.palette.matches)
		return m, nil
	case "up", "k", "ctrl+p":
		if len(m.palette.matches) == 0 {
			return m, nil
		}
		m.palette.cursor--
		if m.palette.cursor < 0 {
			m.palette.cursor = len(m.palette.matches) - 1
		}
		return m, nil
	case "pgdown":
		m.palette.cursor += 5
		if m.palette.cursor >= len(m.palette.matches) {
			m.palette.cursor = len(m.palette.matches) - 1
		}
		if m.palette.cursor < 0 {
			m.palette.cursor = 0
		}
		return m, nil
	case "pgup":
		m.palette.cursor -= 5
		if m.palette.cursor < 0 {
			m.palette.cursor = 0
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.palette.filter, cmd = m.palette.filter.Update(msg)
	m.refreshPaletteMatches()
	return m, cmd
}

func (m Model) runPaletteSelection() (tea.Model, tea.Cmd) {
	if len(m.palette.matches) == 0 || m.palette.cursor < 0 || m.palette.cursor >= len(m.palette.matches) {
		saveCmd := m.closePalette()
		return m, saveCmd
	}
	item := m.palette.matches[m.palette.cursor]
	if m.frecency != nil {
		m.frecency.bump(item.Key)
	}
	saveCmd := m.closePalette()

	next, cmd := m.runPaletteAction(item)
	if saveCmd == nil {
		return next, cmd
	}
	return next, tea.Batch(saveCmd, cmd)
}

func (m Model) runPaletteAction(item paletteItem) (tea.Model, tea.Cmd) {
	switch item.Kind {
	case paletteKindConv:
		if item.Conv == nil {
			return m, nil
		}
		return m.openConversationAcrossRivers(*item.Conv)
	case paletteKindCustom:
		for _, c := range m.customCmds {
			if c.ID == item.CustomID {
				return m.runCustomCommand(c, item.Args)
			}
		}
		m.err = "custom command not found"
		return m, nil
	case paletteKindAction:
		if item.ActionID == "quick-msg" {
			return m.runQuickMsg(item.Args)
		}
		for _, a := range allActions() {
			if a.ID == item.ActionID && a.Run != nil {
				return a.Run(m)
			}
		}
	}
	return m, nil
}

func (m Model) runQuickMsg(args string) (tea.Model, tea.Cmd) {
	contact, body, ok := parseQuickMsgArgs(args)
	if !ok {
		m.err = "usage: >msg contact::message"
		return m, nil
	}
	conv, ok := m.resolvePaletteContact(contact)
	if !ok {
		m.err = fmt.Sprintf("no conversation matching %q", contact)
		return m, nil
	}
	if m.frecency != nil {
		m.frecency.bump("conv:" + conv.ConversationID)
	}
	m2, openCmd := m.openConversationAcrossRivers(conv)
	opened := m2.(Model)
	opened.info = "Sending…"
	send := opened.sendCmd(conv.ConversationID, body, "")
	if openCmd != nil {
		return opened, tea.Batch(openCmd, send)
	}
	return opened, send
}

func (m Model) resolvePaletteContact(query string) (localapi.Conversation, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return localapi.Conversation{}, false
	}
	convs := m.paletteConvs
	if len(convs) == 0 {
		return localapi.Conversation{}, false
	}

	var filtered []localapi.Conversation
	if paletteQueryHasFilterTerms(query) || strings.Contains(query, " ") {
		for _, c := range convs {
			if conversationMatchesFilter(c, query) {
				filtered = append(filtered, c)
			}
		}
	} else {
		data := make([]string, len(convs))
		for i, c := range convs {
			data[i] = paletteConvHaystack(c, m.riverBadge(c.RiverID))
		}
		found := fuzzy.Find(query, data)
		for _, match := range found {
			filtered = append(filtered, convs[match.Index])
		}
		// Also allow substring filter fallback
		if len(filtered) == 0 {
			lq := strings.ToLower(query)
			for _, c := range convs {
				if strings.Contains(strings.ToLower(conversationFilterValue(c)), lq) {
					filtered = append(filtered, c)
				}
			}
		}
	}
	if len(filtered) == 0 {
		return localapi.Conversation{}, false
	}
	best := filtered[0]
	bestScore := m.frecency.score("conv:" + best.ConversationID)
	for _, c := range filtered[1:] {
		sc := m.frecency.score("conv:" + c.ConversationID)
		if sc > bestScore || (sc == bestScore && c.LastMessageTS > best.LastMessageTS) {
			best = c
			bestScore = sc
		}
	}
	return best, true
}

func (m Model) openConversationAcrossRivers(conv localapi.Conversation) (tea.Model, tea.Cmd) {
	riverID := strings.TrimSpace(conv.RiverID)
	if riverID != "" && riverID != m.activeRiverID {
		m.activeRiverID = riverID
		for _, r := range m.rivers {
			if r.ID == riverID {
				m.list.title = r.DisplayName
				break
			}
		}
		if m.list.title == "" {
			m.list.title = m.riverBadge(riverID)
		}
		m.list.clearFilter()
		m.clearBroadcast()
		m.restampConversationList()
		m.syncComposePlaceholder()
		m.threadRootID = ""
		m.channelMessages = nil
	}
	next, openCmd := m.openConversation(conv.ConversationID, conv.Name, conv.Participants)
	opened := next.(Model)
	refresh := opened.refreshConversationsCmd()
	if openCmd != nil {
		return opened, tea.Batch(openCmd, refresh)
	}
	return opened, refresh
}

func (m Model) renderPaletteOverlay() string {
	width := paletteMaxWidth
	if m.width > 0 && width > m.width-4 {
		width = m.width - 4
	}
	if width < 24 {
		width = 24
	}
	innerW := width - 2

	titleText := "Jump"
	if m.palette.mode == "commands" {
		titleText = "Commands"
	}
	// Accent-colored title (not the plain titleStyle every other pane uses)
	// so the palette reads as its own floating surface, not another pane.
	title := paintLine(accentBoldStyle, titleText, innerW)
	filterLine := paintLine(lipgloss.NewStyle(), m.palette.filter.View(), innerW)

	maxList := paletteMaxRows - 3
	if maxList < 3 {
		maxList = 3
	}
	start := 0
	if m.palette.cursor >= maxList {
		start = m.palette.cursor - maxList + 1
	}
	end := start + maxList
	if end > len(m.palette.matches) {
		end = len(m.palette.matches)
	}

	var rows []string
	rows = append(rows, title, filterLine)
	if len(m.palette.matches) == 0 {
		msg := "No matches"
		if m.palette.mode == "jump" && m.paletteConvsLoading {
			msg = "Loading conversations…"
		}
		rows = append(rows, paintCenteredLine(mutedStyle, msg, innerW))
	} else {
		for i := start; i < end; i++ {
			it := m.palette.matches[i]
			line := it.Label
			if it.Sub != "" {
				line = it.Label + "  " + it.Sub
			}
			if it.Args != "" && it.Kind != paletteKindConv {
				line += "  …"
			}
			style := lipgloss.NewStyle()
			if i == m.palette.cursor {
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63"))
			}
			rows = append(rows, paintLine(style, truncateCells(line, innerW), innerW))
		}
	}
	hint := "enter open  > cmds  esc close"
	if m.palette.mode == "commands" {
		hint = "enter run  esc close"
	}
	rows = append(rows, paintLine(mutedStyle, hint, innerW))

	body := strings.Join(rows, "\n")
	// Accent border, same idiom as the focused pane elsewhere — the
	// palette owns all input while it's open, so it's always "focused".
	box := focusBorderStyle.Width(innerW).Render(body)
	return padViewBox(box, lipgloss.Width(box), lipgloss.Height(box))
}

func overlayCenter(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, strings.Repeat(" ", max(0, width)))
	}
	if len(baseLines) > height {
		baseLines = baseLines[:height]
	}

	ovLines := strings.Split(overlay, "\n")
	ovH := len(ovLines)
	ovW := 0
	for _, line := range ovLines {
		if w := ansi.StringWidth(line); w > ovW {
			ovW = w
		}
	}
	startY := (height - ovH) / 2
	startX := (width - ovW) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	for i, ov := range ovLines {
		row := startY + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		lineW := ansi.StringWidth(ov)
		padded := ov
		if lineW < ovW {
			padded = ov + strings.Repeat(" ", ovW-lineW)
			lineW = ovW
		}
		bg := baseLines[row]
		left := ansi.Cut(bg, 0, startX)
		rightStart := startX + lineW
		bgW := ansi.StringWidth(bg)
		right := ""
		if rightStart < bgW {
			right = ansi.Cut(bg, rightStart, bgW)
		}
		combined := left + padded + right
		if ansi.StringWidth(combined) < width {
			combined += strings.Repeat(" ", width-ansi.StringWidth(combined))
		} else if ansi.StringWidth(combined) > width {
			combined = ansi.Truncate(combined, width, "")
		}
		baseLines[row] = combined
	}
	return strings.Join(baseLines, "\n")
}
