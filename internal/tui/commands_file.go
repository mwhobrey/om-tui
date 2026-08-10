package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

const customCommandsFileName = "commands.json"

type customCommand struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Keywords       []string `json:"keywords"`
	Action         string   `json:"action"` // send | open
	RiverID        string   `json:"river_id"`
	ConversationID string   `json:"conversation_id"`
	Body           string   `json:"body"`
	AcceptsArgs    bool     `json:"accepts_args"`
}

type customCommandsFile struct {
	Commands []customCommand `json:"commands"`
}

// loadCustomCommands reads and parses commands.json, returning the file's
// mtime alongside the parsed commands so callers can skip re-reading when
// unchanged. A missing file is not an error (empty result, zero mtime); a
// malformed file returns an error so it can be surfaced instead of silently
// dropping the user's custom commands.
func loadCustomCommands(dataDir string) ([]customCommand, time.Time, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, time.Time{}, nil
	}
	path := filepath.Join(dataDir, customCommandsFileName)
	info, statErr := os.Stat(path)
	if statErr != nil {
		return nil, time.Time{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read %s: %w", customCommandsFileName, err)
	}
	var file customCommandsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, info.ModTime(), fmt.Errorf("parse %s: %w", customCommandsFileName, err)
	}
	out := make([]customCommand, 0, len(file.Commands))
	for _, c := range file.Commands {
		c.ID = strings.TrimSpace(c.ID)
		c.Label = strings.TrimSpace(c.Label)
		c.Action = strings.ToLower(strings.TrimSpace(c.Action))
		c.RiverID = strings.TrimSpace(c.RiverID)
		c.ConversationID = strings.TrimSpace(c.ConversationID)
		if c.ID == "" || c.Label == "" || c.ConversationID == "" {
			continue
		}
		if c.Action != "send" && c.Action != "open" {
			continue
		}
		out = append(out, c)
	}
	return out, info.ModTime(), nil
}

func (c customCommand) haystack() string {
	parts := []string{c.Label, c.ID, "custom"}
	parts = append(parts, c.Keywords...)
	return strings.Join(parts, " ")
}

func (c customCommand) resolveBody(args string) string {
	body := c.Body
	if strings.Contains(body, "{{args}}") {
		body = strings.ReplaceAll(body, "{{args}}", args)
	} else if c.AcceptsArgs && strings.TrimSpace(args) != "" {
		body = args
	}
	return body
}

func (m Model) runCustomCommand(c customCommand, args string) (tea.Model, tea.Cmd) {
	body := c.resolveBody(args)
	switch c.Action {
	case "open":
		conv := localapiConversationStub(c.ConversationID, c.RiverID, c.Label)
		next, cmd := m.openConversationAcrossRivers(conv)
		opened := next.(Model)
		if strings.TrimSpace(body) != "" {
			opened.compose.SetValue(body)
			opened.syncComposeHeight()
			opened.syncComposeViewport()
		}
		return opened, cmd
	case "send":
		if strings.TrimSpace(body) == "" {
			conv := localapiConversationStub(c.ConversationID, c.RiverID, c.Label)
			return m.openConversationAcrossRivers(conv)
		}
		m2, openCmd := m.openConversationAcrossRivers(localapiConversationStub(c.ConversationID, c.RiverID, c.Label))
		opened := m2.(Model)
		opened.info = "Sending…"
		send := opened.sendCmd(c.ConversationID, body, "")
		if openCmd != nil {
			return opened, tea.Batch(openCmd, send)
		}
		return opened, send
	default:
		m.err = "unknown custom command action"
		return m, nil
	}
}

func localapiConversationStub(id, riverID, name string) localapi.Conversation {
	return localapi.Conversation{
		ConversationID: id,
		RiverID:        riverID,
		Name:           name,
	}
}
