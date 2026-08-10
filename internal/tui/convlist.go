package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/maxghenis/openmessage/internal/localapi"
)

const convRowHeight = 2 // title + preview

// convList is a fixed-geometry conversation scroller. It avoids bubbles/list
// pagination, which overflows and ghosts on Windows Terminal.
type convList struct {
	title     string
	items     []convItem
	filtered  []int // indices into items; nil means unfiltered
	cursor    int   // index into the visible set
	offset    int   // scroll offset into the visible set
	width     int
	height    int
	filter    string
	filtering bool
}

func (l *convList) setSize(width, height int) {
	l.width = max(1, width)
	l.height = max(3, height)
	l.ensureVisible()
}

func (l *convList) setItems(items []convItem) {
	selectedID := ""
	if cur, ok := l.selected(); ok {
		selectedID = cur.conv.ConversationID
	}
	l.items = items
	l.applyFilter()
	l.selectID(selectedID)
}

func (l *convList) visibleCount() int {
	if l.filtered == nil {
		return len(l.items)
	}
	return len(l.filtered)
}

func (l *convList) visibleAt(i int) (convItem, bool) {
	if i < 0 || i >= l.visibleCount() {
		return convItem{}, false
	}
	if l.filtered == nil {
		return l.items[i], true
	}
	return l.items[l.filtered[i]], true
}

func (l *convList) selected() (convItem, bool) {
	return l.visibleAt(l.cursor)
}

func (l *convList) selectID(conversationID string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		l.clampCursor()
		l.ensureVisible()
		return
	}
	for i := 0; i < l.visibleCount(); i++ {
		it, ok := l.visibleAt(i)
		if ok && it.conv.ConversationID == conversationID {
			l.cursor = i
			l.ensureVisible()
			return
		}
	}
	l.clampCursor()
	l.ensureVisible()
}

func (l *convList) bodyRows() int {
	return max(1, l.height-1) // reserve 1 for title / filter
}

func (l *convList) pageSize() int {
	return max(1, l.bodyRows()/convRowHeight)
}

func (l *convList) clampCursor() {
	n := l.visibleCount()
	if n == 0 {
		l.cursor = 0
		l.offset = 0
		return
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= n {
		l.cursor = n - 1
	}
}

func (l *convList) ensureVisible() {
	l.clampCursor()
	page := l.pageSize()
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+page {
		l.offset = l.cursor - page + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

func (l *convList) move(delta int) {
	if l.visibleCount() == 0 {
		return
	}
	l.cursor += delta
	l.ensureVisible()
}

func (l *convList) clearFilter() {
	l.filter = ""
	l.filtering = false
	l.filtered = nil
	l.clampCursor()
	l.ensureVisible()
}

func (l *convList) startFilter() {
	l.filtering = true
	l.filter = ""
	l.filtered = nil
	l.cursor = 0
	l.offset = 0
}

func (l *convList) setFilter(s string) {
	l.filter = s
	l.filtering = true
	l.applyFilter()
}

func (l *convList) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(l.filter))
	if q == "" {
		l.filtered = nil
		l.clampCursor()
		l.ensureVisible()
		return
	}
	idxs := make([]int, 0, len(l.items))
	for i, it := range l.items {
		if conversationMatchesFilter(it.conv, q) {
			idxs = append(idxs, i)
		}
	}
	l.filtered = idxs
	l.clampCursor()
	l.ensureVisible()
}

func conversationMatchesFilter(c localapi.Conversation, query string) bool {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	haystack := strings.ToLower(conversationFilterValue(c))
	for _, term := range terms {
		switch term {
		case "is:unread":
			if c.UnreadCount <= 0 {
				return false
			}
		case "type:dm":
			if c.StreamKind != "im" && c.StreamKind != "mpim" {
				return false
			}
		case "type:channel":
			if c.StreamKind != "public_channel" && c.StreamKind != "private_channel" {
				return false
			}
		case "type:im", "type:mpim", "type:public_channel", "type:private_channel":
			if c.StreamKind != strings.TrimPrefix(term, "type:") {
				return false
			}
		default:
			if !strings.Contains(haystack, term) {
				return false
			}
		}
	}
	return true
}

// conversationTypeGlyph distinguishes Slack channels from Slack DMs in a
// mixed river list. Plain ASCII only — this codebase has a history of
// Windows Terminal ghosting from under-measured wide glyphs (see wrap.go),
// so no emoji here. SMS/RCS/etc. rivers are already homogeneous per list,
// so they get a blank glyph rather than a redundant per-row marker.
func conversationTypeGlyph(c localapi.Conversation) string {
	switch c.StreamKind {
	case "public_channel", "private_channel":
		return "#"
	case "im", "mpim":
		return "@"
	default:
		return " "
	}
}

// renderConvRow builds one styled conversation-list row: a left-edge cursor
// marker (the same selection idiom the thread view uses), the broadcast
// multi-select marker, a conversation-type glyph, the name, and a
// right-aligned unread-count badge pill instead of "(N)" baked into the
// name's own color.
func renderConvRow(it convItem, isCursor bool, width int) string {
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	if isCursor {
		nameStyle = accentBoldStyle
	}

	cursorMark := " "
	if isCursor {
		cursorMark = accentStyle.Render("▍")
	}
	broadcastMark := " "
	if it.selected {
		// Warn-orange, not the row's own color: a pending multi-select
		// broadcast is a "you're about to message N people" state and
		// should be harder to miss than the name's usual styling.
		broadcastMark = warnStyle.Render("*")
	}
	const prefixW = 5 // cursor(1) + broadcast(1) + space(1) + glyph(1) + space(1)
	prefix := cursorMark + broadcastMark + " " + dimStyle.Render(conversationTypeGlyph(it.conv)) + " "

	name := strings.TrimSpace(it.conv.Name)
	if name == "" {
		name = it.conv.ConversationID
	}

	badge := ""
	badgeW := 0
	if it.conv.UnreadCount > 0 {
		badgeText := fmt.Sprintf(" %d ", it.conv.UnreadCount)
		badge = badgeStyle.Render(badgeText)
		badgeW = cellWidth(badgeText)
	}

	nameW := width - prefixW - badgeW
	if nameW < 1 {
		nameW = 1
	}
	nameTrunc := truncateCells(name, nameW)
	pad := max(0, nameW-cellWidth(nameTrunc))
	return prefix + nameStyle.Render(nameTrunc) + strings.Repeat(" ", pad) + badge
}

func (l convList) View() string {
	width := max(1, l.width)
	height := max(3, l.height)

	var b strings.Builder
	title := strings.TrimSpace(l.title)
	if title == "" {
		title = "Conversations"
	}
	if l.filtering {
		title = "/ " + l.filter
		if title == "/ " {
			title = "/ "
		}
		b.WriteString(paintLine(lipgloss.NewStyle().Foreground(lipgloss.Color("81")), title, width))
	} else {
		b.WriteString(paintLine(titleStyle, title, width))
	}
	b.WriteByte('\n')

	bodyRows := height - 1
	page := max(1, bodyRows/convRowHeight)
	linesUsed := 0
	end := l.offset + page
	if end > l.visibleCount() {
		end = l.visibleCount()
	}
	for i := l.offset; i < end; i++ {
		it, ok := l.visibleAt(i)
		if !ok {
			break
		}
		b.WriteString(paintLine(lipgloss.NewStyle(), renderConvRow(it, i == l.cursor, width), width))
		b.WriteByte('\n')
		linesUsed++
		if linesUsed >= bodyRows {
			break
		}
		b.WriteString(paintLine(mutedStyle, "  "+it.Description(), width))
		b.WriteByte('\n')
		linesUsed++
		if linesUsed >= bodyRows {
			break
		}
	}
	blank := strings.Repeat(" ", width)
	for linesUsed < bodyRows {
		b.WriteString(blank)
		b.WriteByte('\n')
		linesUsed++
	}
	// Trim trailing newline so JoinVertical/borders count exact height lines.
	return strings.TrimRight(b.String(), "\n")
}

// restampBroadcast marks broadcast selection flags from ids.
// totalUnread sums UnreadCount across all loaded conversations (not just the
// filtered/visible subset) for the status bar's right-aligned badge.
func (l convList) totalUnread() int {
	total := 0
	for _, it := range l.items {
		if it.conv.UnreadCount > 0 {
			total += it.conv.UnreadCount
		}
	}
	return total
}

func (l *convList) restampBroadcast(ids map[string]string) {
	selectedID := ""
	if cur, ok := l.selected(); ok {
		selectedID = cur.conv.ConversationID
	}
	for i := range l.items {
		_, l.items[i].selected = ids[l.items[i].conv.ConversationID]
	}
	l.selectID(selectedID)
}

func conversationFilterValue(c localapi.Conversation) string {
	parts := []string{
		strings.TrimSpace(c.Name),
		strings.TrimSpace(c.ConversationID),
		strings.TrimSpace(c.LastMessagePreview),
	}
	if p := strings.TrimSpace(c.Participants); p != "" {
		p = strings.NewReplacer(`"`, "", `[`, "", `]`, "", `,`, " ").Replace(p)
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}
