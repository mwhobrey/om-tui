package tui

import (
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
		if strings.Contains(strings.ToLower(conversationFilterValue(it.conv)), q) {
			idxs = append(idxs, i)
		}
	}
	l.filtered = idxs
	l.clampCursor()
	l.ensureVisible()
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
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		descStyle := mutedStyle
		if i == l.cursor {
			nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
			descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		}
		b.WriteString(paintLine(nameStyle, it.Title(), width))
		b.WriteByte('\n')
		linesUsed++
		if linesUsed >= bodyRows {
			break
		}
		b.WriteString(paintLine(descStyle, it.Description(), width))
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
