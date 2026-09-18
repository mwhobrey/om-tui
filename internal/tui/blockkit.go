package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

const maxBlockActions = 9

func selectedBlockActions(msg localapi.Message) []localapi.MessageAction {
	if len(msg.BlockActions) == 0 {
		return nil
	}
	out := make([]localapi.MessageAction, 0, len(msg.BlockActions))
	for _, action := range msg.BlockActions {
		if strings.TrimSpace(action.Label) == "" {
			continue
		}
		out = append(out, action)
		if len(out) == maxBlockActions {
			break
		}
	}
	return out
}

func blockPaletteHelp(actions []localapi.MessageAction) string {
	parts := make([]string, 0, len(actions))
	for i, action := range actions {
		parts = append(parts, fmt.Sprintf("%d %s", i+1, action.Label))
	}
	return strings.Join(parts, "  ")
}

func (m Model) openBlockPalette() (tea.Model, tea.Cmd) {
	target, ok := selectedMessage(m.messages, m.selectedMsg)
	if !ok {
		m.err = "no message selected"
		return m, nil
	}
	actions := selectedBlockActions(target)
	if len(actions) == 0 {
		m.err = "no Block Kit actions on this message"
		return m, nil
	}
	m.focus = focusThread
	m.compose.Blur()
	m.reactPalette = false
	m.err = ""
	if len(actions) == 1 {
		return m.activateBlockAction(actions[0])
	}
	m.blockPalette = true
	m.info = "Block Kit: " + blockPaletteHelp(actions)
	return m, nil
}

func (m Model) activateBlockAction(action localapi.MessageAction) (tea.Model, tea.Cmd) {
	m.blockPalette = false
	url := strings.TrimSpace(action.URL)
	if url == "" {
		m.err = "that control has no link om-tui can open"
		return m, nil
	}
	if action.Kind == "app" {
		m.info = "Opening in Slack — this control belongs to the app that posted it"
	} else {
		m.info = "Opening " + action.Label + "…"
	}
	return m, func() tea.Msg {
		if err := openFile(url); err != nil {
			return errMsg{err: fmt.Errorf("open %s: %w", action.Label, err)}
		}
		return nil
	}
}
