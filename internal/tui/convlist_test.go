package tui

import (
	"strings"
	"testing"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestConvListViewExactHeight(t *testing.T) {
	var l convList
	l.title = "Messages"
	l.setSize(24, 10)
	items := make([]convItem, 20)
	for i := range items {
		items[i] = convItem{conv: localapi.Conversation{
			ConversationID:     "id-" + string(rune('a'+i%26)),
			Name:               "Contact " + string(rune('A'+i%26)),
			LastMessagePreview: "preview text",
		}}
	}
	l.setItems(items)

	for step := 0; step < 15; step++ {
		l.move(1)
		view := l.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 10 {
			t.Fatalf("step %d: lines = %d, want 10", step, len(lines))
		}
		for i, line := range lines {
			if w := cellWidth(stripForWidthTest(line)); w != 24 {
				t.Fatalf("step %d line %d width = %d (%q)", step, i, w, stripForWidthTest(line))
			}
		}
	}
}

func TestConvListFilterJump(t *testing.T) {
	var l convList
	l.setSize(20, 8)
	l.setItems([]convItem{
		{conv: localapi.Conversation{ConversationID: "1", Name: "Alice"}},
		{conv: localapi.Conversation{ConversationID: "2", Name: "Bob"}},
		{conv: localapi.Conversation{ConversationID: "3", Name: "Alice Smith"}},
	})
	l.startFilter()
	l.setFilter("ali")
	if l.visibleCount() != 2 {
		t.Fatalf("visible = %d, want 2", l.visibleCount())
	}
	cur, ok := l.selected()
	if !ok || cur.conv.Name != "Alice" {
		t.Fatalf("selected = %#v", cur)
	}
}
