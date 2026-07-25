package tui

import (
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

// Regression: opening a thread then scrolling the contact list used to leave
// Windows Terminal ghosts when any frame line wrapped past the terminal width
// (emoji/reactions under-measured) or when the joined panes exceeded height.
func TestViewStableAfterOpenAndListScroll(t *testing.T) {
	m := NewModel(nil)
	m.width = 120
	m.height = 36
	m.ready = true
	m.layout()

	items := make([]convItem, 40)
	for i := range items {
		items[i] = convItem{conv: localapi.Conversation{
			ConversationID:     "id-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Name:               "Contact " + string(rune('A'+i%26)),
			LastMessagePreview: "preview with emoji 💙🙏 and text",
			UnreadCount:        i % 3,
		}}
	}
	m.list.setItems(items)
	m.activeID = items[0].conv.ConversationID
	m.activeName = items[0].conv.Name
	m.focus = focusList
	m.messages = []localapi.Message{
		{TimestampMS: 1721815600000, IsFromMe: false, SenderName: "Keebo W Corley", Body: "Replied to a message: I think that was when he was having the issues with the LP results 💙"},
		{TimestampMS: 1721815700000, IsFromMe: true, Body: "mhm thats about half of it… 🙏"},
		{TimestampMS: 1721815800000, IsFromMe: false, SenderName: "Chelle Rowland", Body: "hospital update with [image] caption and ❤️💛"},
		{TimestampMS: 1721815900000, IsFromMe: true, Body: strings.Repeat("word ", 40) + "💙"},
	}
	m.setThreadContentFollow(m.renderActiveThread())

	assertExactFrame := func(t *testing.T, label string) {
		t.Helper()
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) < m.height-1 || len(lines) > m.height {
			t.Fatalf("%s: view lines = %d, want ~%d", label, len(lines), m.height)
		}
		if !strings.Contains(view, "└") {
			t.Fatalf("%s: missing bottom borders (MaxHeight clipping?)", label)
		}
		for i, line := range lines {
			if i >= m.height {
				break
			}
			w := cellWidth(stripForWidthTest(line))
			if w != m.width {
				t.Fatalf("%s: line %d width = %d, want %d (%q)", label, i, w, m.width, stripForWidthTest(line))
			}
		}
	}

	assertExactFrame(t, "after open")
	for step := 0; step < 25; step++ {
		m.list.move(1)
		assertExactFrame(t, "scroll "+string(rune('0'+step%10)))
	}
}
