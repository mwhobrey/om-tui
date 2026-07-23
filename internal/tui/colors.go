package tui

import (
	"hash/fnv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/maxghenis/openmessage/internal/localapi"
)

// participantPalette is a fixed set of 256-color ANSI codes for inbound
// senders. Avoids reserved UI colors: 81 (me), 245 (muted), 42/214/196 (status).
var participantPalette = []lipgloss.Color{
	lipgloss.Color("177"), // purple
	lipgloss.Color("114"), // green
	lipgloss.Color("208"), // orange
	lipgloss.Color("75"),  // blue
	lipgloss.Color("218"), // pink
	lipgloss.Color("185"), // yellow-green
	lipgloss.Color("110"), // steel blue
	lipgloss.Color("203"), // coral
	lipgloss.Color("151"), // mint
	lipgloss.Color("141"), // violet
	lipgloss.Color("180"), // tan
	lipgloss.Color("116"), // teal
}

func participantKey(msg localapi.Message) string {
	if n := strings.TrimSpace(msg.SenderNumber); n != "" {
		return strings.ToLower(n)
	}
	if n := strings.TrimSpace(msg.SenderName); n != "" {
		return strings.ToLower(n)
	}
	return "them"
}

func participantColorIndex(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(len(participantPalette)))
}

func participantStyle(msg localapi.Message) lipgloss.Style {
	if msg.IsFromMe {
		return meStyle
	}
	idx := participantColorIndex(participantKey(msg))
	return lipgloss.NewStyle().Foreground(participantPalette[idx])
}
