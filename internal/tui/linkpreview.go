package tui

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/maxghenis/openmessage/internal/localapi"
)

var (
	rawURLPattern    = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
	linkPreviewStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	linkDescStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
)

// extractFirstURL finds and returns the first HTTP/HTTPS URL in text, stripped of trailing punctuation.
func extractFirstURL(text string) string {
	loc := rawURLPattern.FindString(text)
	if loc == "" {
		return ""
	}
	trimmed := strings.TrimRight(loc, ".,;:!?)>]}")
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return trimmed
}

// sanitizePreviewText strips ASCII and C0/C1 control characters (e.g. \x1b ESC, \x00-\x1f, \x7f, \x80-\x9f)
// except tab/newlines (which become spaces) to prevent escape injection or terminal title manipulation.
func sanitizePreviewText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(' ')
			continue
		}
		if r < 32 || r == 127 || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// formatLinkPreview formats a link preview as a clean 1-2 line summary box.
func formatLinkPreview(preview *localapi.LinkPreview, width int) []string {
	if preview == nil || width < 12 {
		return nil
	}
	title := sanitizePreviewText(preview.Title)
	desc := sanitizePreviewText(preview.Description)
	site := sanitizePreviewText(preview.SiteName)
	if site == "" {
		site = sanitizePreviewText(preview.Domain)
	}
	if site == "" && preview.URL != "" {
		if parsed, err := url.Parse(preview.URL); err == nil {
			site = sanitizePreviewText(parsed.Host)
		}
	}
	if title == "" && desc == "" {
		return nil
	}

	var lines []string
	if title != "" {
		header := title
		if site != "" {
			header = fmt.Sprintf("[%s] %s", site, title)
		}
		prefix := "┌─ 🔗 "
		maxLen := width - cellWidth(prefix)
		if maxLen > 4 {
			header = truncateCells(header, maxLen)
		}
		lines = append(lines, linkPreviewStyle.Render(prefix+header))
	}

	if desc != "" {
		prefix := "└─ "
		if len(lines) == 0 {
			prefix = "┌─ 🔗 "
		}
		maxLen := width - cellWidth(prefix)
		if maxLen > 4 {
			desc = truncateCells(desc, maxLen)
		}
		lines = append(lines, linkDescStyle.Render(prefix+desc))
	}

	return lines
}
