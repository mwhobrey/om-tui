package tui

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

// maxTUIMediaBytes mirrors the HTTP/MCP upload cap (128 MiB).
const maxTUIMediaBytes = 128 << 20

func looksLikeExistingFile(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	s = strings.Trim(s, `"'`)
	s = expandUserPath(s)
	if s == "" {
		return "", false
	}
	// Paths with newlines are never files; treat as plain text.
	if strings.ContainsAny(s, "\r\n") {
		return "", false
	}
	info, err := os.Stat(s)
	if err != nil || info.IsDir() {
		return "", false
	}
	return s, true
}

func expandUserPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, s[2:])
		}
	}
	if strings.HasPrefix(strings.ToUpper(s), "%USERPROFILE%") {
		home, err := os.UserHomeDir()
		if err == nil {
			rest := s[len("%USERPROFILE%"):]
			rest = strings.TrimPrefix(rest, `\`)
			rest = strings.TrimPrefix(rest, `/`)
			return filepath.Join(home, rest)
		}
	}
	return s
}

func detectSendMIME(filename string, sniff []byte) string {
	if ext := strings.TrimSpace(filepath.Ext(filename)); ext != "" {
		if typed := mime.TypeByExtension(ext); typed != "" {
			if idx := strings.Index(typed, ";"); idx >= 0 {
				typed = typed[:idx]
			}
			if typed != "" {
				return typed
			}
		}
	}
	if len(sniff) > 0 {
		return http.DetectContentType(sniff)
	}
	return "application/octet-stream"
}

func captionForAttach(composeValue string) string {
	if _, ok := looksLikeExistingFile(composeValue); ok {
		return ""
	}
	return strings.TrimSpace(composeValue)
}

func (m Model) sendMediaCmd(conversationID, path, caption string) tea.Cmd {
	client := m.session.Client
	fail := func(err error) tea.Msg {
		return sendFailedMsg{conversationID: conversationID, body: path, err: err}
	}
	return func() tea.Msg {
		info, err := os.Stat(path)
		if err != nil {
			return fail(fmt.Errorf("stat media: %w", err))
		}
		if info.IsDir() {
			return fail(fmt.Errorf("path is a directory, not a file"))
		}
		if info.Size() > maxTUIMediaBytes {
			return fail(fmt.Errorf("file too large (%d bytes; limit %d MB)", info.Size(), maxTUIMediaBytes>>20))
		}

		file, err := os.Open(path)
		if err != nil {
			return fail(fmt.Errorf("open media: %w", err))
		}
		defer file.Close()

		sniff := make([]byte, 512)
		n, _ := file.Read(sniff)
		if n < 0 {
			n = 0
		}
		sniff = sniff[:n]
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return fail(fmt.Errorf("rewind media: %w", err))
		}

		filename := filepath.Base(path)
		if filename == "." || filename == string(filepath.Separator) || filename == "" {
			return fail(fmt.Errorf("invalid media filename"))
		}
		mimeType := detectSendMIME(filename, sniff)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		status, _, err := client.Status(ctx)
		if err != nil {
			return fail(err)
		}
		key, err := newIdempotencyKey()
		if err != nil {
			return fail(err)
		}
		submission := localapi.MediaSubmission{
			ConversationID: conversationID,
			Filename:       filename,
			MIME:           mimeType,
			Caption:        caption,
			IdempotencyKey: key,
			Content:        file,
		}
		if status.V2Send || status.V2Primary {
			if _, err := client.SubmitMedia(ctx, submission); err != nil {
				return fail(err)
			}
		} else {
			if _, err := client.LegacySendMedia(ctx, submission); err != nil {
				return fail(err)
			}
		}
		return sentMsg{conversationID: conversationID}
	}
}

func (m Model) attachPickerCmd(conversationID, caption string) tea.Cmd {
	return func() tea.Msg {
		path, err := pickMediaFile()
		if err != nil {
			return errMsg{err: err}
		}
		if strings.TrimSpace(path) == "" {
			return attachCancelledMsg{}
		}
		return attachResolvedMsg{
			conversationID: conversationID,
			path:           path,
			caption:        caption,
		}
	}
}

func (m Model) attachClipboardCmd(conversationID, caption string) tea.Cmd {
	return func() tea.Msg {
		path, ok, err := clipboardMediaPath()
		if err != nil {
			return errMsg{err: err}
		}
		if !ok || strings.TrimSpace(path) == "" {
			return clipboardTextFallbackMsg{}
		}
		return attachResolvedMsg{
			conversationID: conversationID,
			path:           path,
			caption:        caption,
		}
	}
}

type attachResolvedMsg struct {
	conversationID string
	path           string
	caption        string
}

type clipboardTextFallbackMsg struct{}

type attachCancelledMsg struct{}
