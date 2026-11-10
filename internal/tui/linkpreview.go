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

// formatLinkPreview formats a link preview as a clean 1-2 line summary box.
func formatLinkPreview(preview *localapi.LinkPreview, width int) []string {
	if preview == nil || width < 12 {
		return nil
	}
	title := strings.TrimSpace(preview.Title)
	desc := strings.TrimSpace(preview.Description)
	site := strings.TrimSpace(preview.SiteName)
	if site == "" {
		site = strings.TrimSpace(preview.Domain)
	}
	if site == "" && preview.URL != "" {
		if parsed, err := url.Parse(preview.URL); err == nil {
			site = parsed.Host
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
