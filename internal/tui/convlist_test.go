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

func TestConversationTypeGlyph(t *testing.T) {
	cases := []struct {
		streamKind string
		want       string
	}{
		{"public_channel", "#"},
		{"private_channel", "#"},
		{"im", "@"},
		{"mpim", "@"},
		{"", " "},
		{"unknown", " "},
	}
	for _, c := range cases {
		got := conversationTypeGlyph(localapi.Conversation{StreamKind: c.streamKind})
		if got != c.want {
			t.Fatalf("streamKind=%q: got %q, want %q", c.streamKind, got, c.want)
		}
	}
}

func TestRenderConvRowExactWidthWithGlyphAndBadge(t *testing.T) {
	it := convItem{conv: localapi.Conversation{
		Name:        "Design Team",
		StreamKind:  "public_channel",
		UnreadCount: 12,
	}, selected: true}
	for _, width := range []int{20, 30, 60} {
		row := renderConvRow(it, true, width)
		if w := cellWidth(stripForWidthTest(row)); w != width {
			t.Fatalf("width=%d: rendered row width = %d (%q)", width, w, stripForWidthTest(row))
		}
	}
	plain := stripForWidthTest(renderConvRow(it, true, 60))
	if !strings.Contains(plain, "#") {
		t.Fatalf("expected public_channel glyph in row: %q", plain)
	}
	if !strings.Contains(plain, "*") {
		t.Fatalf("expected broadcast marker in row: %q", plain)
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
