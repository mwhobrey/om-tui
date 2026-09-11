package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/maxghenis/openmessage/internal/localapi"
)

const (
	pairOverlayWidth     = 64
	pairTickInterval     = 100 * time.Millisecond
	pairStatusEveryTicks = 2
	pairFailedGrace      = 2 * time.Second
)

var pairSpinnerFrames = []string{"|", "/", "-", "\\"}

var pairStepNames = []string{"Chrome", "Google", "Phone", "Connect"}

type pairOverlay struct {
	open              bool
	submitting        bool
	dismissed         bool
	successUntil      time.Time
	pasteErr          string
	started           time.Time
	ignoreFailedUntil time.Time
	ticks             int
}

func (p pairOverlay) ignoringStaleFailed() bool {
	return p.submitting && !p.ignoreFailedUntil.IsZero() && time.Now().Before(p.ignoreFailedUntil)
}

type pairStartedMsg struct {
	err    error
	status localapi.DaemonStatus
}

type pairTickMsg struct{}

func (m Model) googleNeedsPair() bool {
	g := m.status.Google
	return g.NeedsPairing || !g.Paired || g.NeedsRepair
}

func (m Model) pairingFromDaemon() *localapi.GooglePairingStatus {
	return m.status.Google.Pairing
}

func (m Model) pairPhase() string {
	if p := m.status.Google.Pairing; p != nil {
		switch p.Phase {
		case "failed":
			if m.pair.ignoringStaleFailed() {
				return "starting"
			}
			return p.Phase
		case "starting", "reading_chrome", "waiting_browser", "waiting_confirm", "finishing":
			return p.Phase
		}
	}
	if m.pair.submitting {
		return "starting"
	}
	return ""
}

func (m Model) openPairOverlay() (tea.Model, tea.Cmd) {
	m.pair.open = true
	m.pair.dismissed = false
	m.pair.pasteErr = ""
	m.pair.submitting = false
	m.pair.started = time.Time{}
	m.pair.ticks = 0
	m.info = ""
	m.err = ""
	if pairing := m.pairingFromDaemon(); pairing != nil && pairing.Phase != "" && pairing.Phase != "failed" {
		return m, m.pairTickCmd()
	}
	return m, nil
}

func (m Model) closePairOverlay() (tea.Model, tea.Cmd) {
	m.pair.open = false
	m.pair.submitting = false
	m.pair.dismissed = true
	m.pair.pasteErr = ""
	m.pair.successUntil = time.Time{}
	m.pair.started = time.Time{}
	m.pair.ignoreFailedUntil = time.Time{}
	m.pair.ticks = 0
	return m, nil
}

func (m Model) pairBusy() bool {
	if !m.pair.successUntil.IsZero() {
		return true
	}
	if m.pair.submitting {
		return true
	}
	pairing := m.pairingFromDaemon()
	if pairing == nil {
		return false
	}
	switch pairing.Phase {
	case "starting", "reading_chrome", "waiting_browser", "waiting_confirm", "finishing":
		return true
	default:
		return false
	}
}

func (m Model) syncPairOverlayFromStatus() (Model, tea.Cmd) {
	if m.pair.dismissed {
		return m, nil
	}
	pairing := m.pairingFromDaemon()
	if pairing != nil && pairing.Phase != "" && pairing.Phase != "failed" {
		m.pair.open = true
		m.pair.submitting = pairing.Phase == "starting" || pairing.Phase == "reading_chrome"
		if pairing.Phase == "waiting_confirm" || pairing.Phase == "finishing" || pairing.Phase == "waiting_browser" {
			m.pair.submitting = false
			m.pair.pasteErr = ""
		}
		return m, m.pairTickCmd()
	}
	if pairing != nil && pairing.Phase == "failed" {
		if m.pair.ignoringStaleFailed() {
			return m, m.pairTickCmd()
		}
		m.pair.open = true
		m.pair.submitting = false
		m.pair.pasteErr = pairing.Error
		return m, nil
	}
	if m.pair.open && m.pair.submitting && m.status.Google.Paired && m.status.Google.Connected {
		m.pair.submitting = false
		m.pair.successUntil = time.Now().Add(1500 * time.Millisecond)
		return m, m.pairTickCmd()
	}
	if m.pair.open && !m.pair.submitting && m.status.Google.Paired && m.status.Google.Connected && m.pair.successUntil.IsZero() {
		m.pair.successUntil = time.Now().Add(1500 * time.Millisecond)
		return m, m.pairTickCmd()
	}
	return m, nil
}

func (m Model) pairTickCmd() tea.Cmd {
	if !m.pair.open || !m.pairBusy() {
		return nil
	}
	return tea.Tick(pairTickInterval, func(time.Time) tea.Msg { return pairTickMsg{} })
}

func (m Model) submitPairCookies(raw string) (tea.Model, tea.Cmd) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		m.pair.pasteErr = "clipboard was empty. Copy as cURL from messages.google.com, then ctrl+v"
		return m, nil
	}
	m.pair.submitting = true
	m.pair.pasteErr = ""
	m.pair.started = time.Now()
	m.pair.ignoreFailedUntil = time.Now().Add(pairFailedGrace)
	m.pair.ticks = 0
	return m, tea.Batch(m.pairGoogleCmd(raw), m.pairTickCmd())
}

func (m Model) pairGoogleCmd(raw string) tea.Cmd {
	return func() tea.Msg {
		if m.session == nil || m.session.Client == nil {
			return pairStartedMsg{err: fmt.Errorf("not attached to the local API daemon")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		status, err := m.session.Client.PairGoogle(ctx, raw)
		return pairStartedMsg{err: err, status: status}
	}
}

func (m Model) cancelPairCmd() tea.Cmd {
	return func() tea.Msg {
		if m.session == nil || m.session.Client == nil {
			return pairTickMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, _ = m.session.Client.CancelGooglePair(ctx)
		return pairTickMsg{}
	}
}

func (m Model) pastePairCookiesCmd() tea.Cmd {
	return func() tea.Msg {
		return clipboardTextFallbackMsg{}
	}
}

func (m Model) updatePairKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	busy := m.pairBusy()
	switch msg.String() {
	case "ctrl+c":
		cmds := []tea.Cmd{m.cancelPairCmd()}
		next, cmd := m.closePairOverlay()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return next, tea.Batch(append(cmds, tea.Quit)...)
	case "esc", "q":
		cmds := []tea.Cmd{m.cancelPairCmd()}
		next, cmd := m.closePairOverlay()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return next, tea.Batch(cmds...)
	case "enter":
		if busy {
			return m, nil
		}
		m.pair.pasteErr = "paste a messages.google.com curl with ctrl+v"
		return m, nil
	case "ctrl+v":
		if busy {
			return m, nil
		}
		return m, m.pastePairCookiesCmd()
	}
	return m, nil
}

func (p pairOverlay) spinner() string {
	if len(pairSpinnerFrames) == 0 {
		return "..."
	}
	return pairSpinnerFrames[p.ticks%len(pairSpinnerFrames)]
}

func (p pairOverlay) elapsedLabel() string {
	d := time.Duration(0)
	if !p.started.IsZero() {
		d = time.Since(p.started).Truncate(time.Second)
		if d < 0 {
			d = 0
		}
	}
	return fmt.Sprintf("%d:%02d elapsed", int(d.Minutes()), int(d.Seconds())%60)
}

func pairStepIndex(phase string, submitting bool) int {
	switch phase {
	case "reading_chrome", "waiting_browser":
		return 0
	case "starting":
		return 1
	case "waiting_confirm":
		return 2
	case "finishing":
		return 3
	default:
		if submitting {
			return 0
		}
		return -1
	}
}

func renderPairStepLine(innerW, current int, spin string) string {
	var b strings.Builder
	for i, name := range pairStepNames {
		if i > 0 {
			b.WriteString(mutedStyle.Render("  ·  "))
		}
		switch {
		case current < 0:
			b.WriteString(mutedStyle.Render(name))
		case i < current:
			b.WriteString(okStyle.Render("✓ " + name))
		case i == current:
			b.WriteString(accentBoldStyle.Render(spin + " " + name))
		default:
			b.WriteString(mutedStyle.Render(name))
		}
	}
	line := b.String()
	if pad := innerW - cellWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

func pairPasteInstructionLines(innerW int) []string {
	intro := "Chrome encrypts Google cookies on this PC, so paste them instead."
	steps := []string{
		"1. Open messages.google.com signed in",
		"2. F12, Network, reload the page",
		"3. Click a messages.google.com request",
		"4. Copy as cURL, then ctrl+v here",
		"5. Tap the emoji on your phone",
	}
	var rows []string
	for _, line := range wrapLines(intro, innerW) {
		rows = append(rows, paintLine(mutedStyle, line, innerW))
	}
	rows = append(rows, "")
	for _, step := range steps {
		for _, line := range wrapLines(step, innerW) {
			rows = append(rows, paintLine(lipgloss.NewStyle(), line, innerW))
		}
	}
	return rows
}

func (m Model) pairProgressHeader(innerW, current int) []string {
	return []string{
		"",
		renderPairStepLine(innerW, current, m.pair.spinner()),
		paintCenteredLine(mutedStyle, m.pair.elapsedLabel(), innerW),
	}
}

func (m Model) renderPairOverlay() string {
	width := pairOverlayWidth
	if m.width > 0 && width > m.width-4 {
		width = m.width - 4
	}
	if width < 40 {
		width = 40
	}
	innerW := width - 2

	pairing := m.pairingFromDaemon()
	phase := m.pairPhase()
	step := pairStepIndex(phase, m.pair.submitting)
	title := paintLine(accentBoldStyle, "Pair Google Messages", innerW)
	var rows []string
	rows = append(rows, title)

	chromeHint := "Stay signed into Chrome on this PC."
	if !m.pair.started.IsZero() && time.Since(m.pair.started) >= 8*time.Second {
		chromeHint = "If this stalls, fully quit Chrome — tray icon too."
	}

	switch {
	case !m.pair.successUntil.IsZero() && time.Now().Before(m.pair.successUntil):
		rows = append(rows, renderPairStepLine(innerW, len(pairStepNames), m.pair.spinner()))
		rows = append(rows,
			"",
			paintCenteredLine(okStyle, "Paired", innerW),
			paintCenteredLine(mutedStyle, "Google Messages is connected.", innerW),
		)
	case phase == "waiting_confirm":
		emoji := ""
		if pairing != nil {
			emoji = pairing.Emoji
		}
		rows = append(rows, m.pairProgressHeader(innerW, step)...)
		rows = append(rows,
			"",
			paintCenteredLine(lipgloss.NewStyle().Bold(true), emoji, innerW),
			"",
			paintCenteredLine(lipgloss.NewStyle(), "Tap this emoji in Google Messages", innerW),
			paintCenteredLine(mutedStyle, "on your phone (notification or Device pairing).", innerW),
			paintCenteredLine(mutedStyle, "Waiting on your phone…", innerW),
			"",
			paintLine(mutedStyle, "esc cancel    q close", innerW),
		)
	case phase == "finishing":
		rows = append(rows, m.pairProgressHeader(innerW, step)...)
		rows = append(rows,
			"",
			paintCenteredLine(okStyle, "Phone confirmed", innerW),
			paintCenteredLine(mutedStyle, m.pair.spinner()+" Connecting to Google Messages…", innerW),
			"",
			paintLine(mutedStyle, "esc cancel    q close", innerW),
		)
	case phase == "waiting_browser":
		rows = append(rows, m.pairProgressHeader(innerW, step)...)
		rows = append(rows,
			"",
			paintCenteredLine(lipgloss.NewStyle().Bold(true), "Quit Google Chrome", innerW),
			"",
			paintCenteredLine(lipgloss.NewStyle(), "Leave it signed into this Google account.", innerW),
			paintCenteredLine(mutedStyle, "Task Manager can be empty while Windows still holds the files.", innerW),
			paintCenteredLine(mutedStyle, m.pair.spinner()+" Waiting for Chrome's files to unlock…", innerW),
			"",
			paintLine(mutedStyle, "esc cancel    q close", innerW),
		)
	case phase == "reading_chrome":
		rows = append(rows, m.pairProgressHeader(innerW, step)...)
		rows = append(rows,
			"",
			paintCenteredLine(mutedStyle, m.pair.spinner()+" Reading Chrome cookies…", innerW),
			paintCenteredLine(mutedStyle, chromeHint, innerW),
			"",
			paintLine(mutedStyle, "esc cancel    q close", innerW),
		)
	case phase == "starting":
		rows = append(rows, m.pairProgressHeader(innerW, step)...)
		rows = append(rows,
			"",
			paintCenteredLine(mutedStyle, m.pair.spinner()+" Talking to Google…", innerW),
			paintCenteredLine(mutedStyle, "Stay in this window — pairing is still running.", innerW),
			"",
			paintLine(mutedStyle, "esc cancel    q close", innerW),
		)
	case phase == "failed":
		rows = append(rows, "")
		if m.pair.pasteErr != "" {
			for _, line := range wrapLines(m.pair.pasteErr, innerW) {
				rows = append(rows, paintLine(warnStyle, line, innerW))
			}
			rows = append(rows, "")
		} else {
			rows = append(rows, paintCenteredLine(warnStyle, "Pairing failed", innerW), "")
		}
		rows = append(rows, pairPasteInstructionLines(innerW)...)
		rows = append(rows, "", paintLine(mutedStyle, "ctrl+v paste    esc close", innerW))
	default:
		rows = append(rows, pairPasteInstructionLines(innerW)...)
		if m.pair.pasteErr != "" {
			rows = append(rows, "")
			for _, line := range wrapLines(m.pair.pasteErr, innerW) {
				rows = append(rows, paintLine(warnStyle, line, innerW))
			}
		}
		rows = append(rows, "", paintLine(mutedStyle, "ctrl+v paste    esc close", innerW))
	}

	body := strings.Join(rows, "\n")
	box := focusBorderStyle.Width(innerW).Render(body)
	return padViewBox(box, lipgloss.Width(box), lipgloss.Height(box))
}
