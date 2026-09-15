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

type pairQRMsg struct {
	kind      string
	payload   string
	updatedAt int64
	err       error
	missing   bool
}

func (m Model) startLivePair() (tea.Model, tea.Cmd) {
	m.pair.submitting = true
	m.pair.pasteErr = ""
	m.pair.started = time.Now()
	m.pair.ticks = 0
	m.pair.ticking = false
	m, tick := m.armPairTick()
	return m, tea.Batch(m.livePairConnectCmd(), m.livePairQRCmd(), tick)
}

func (m Model) livePairConnectCmd() tea.Cmd {
	kind := m.pair.kind
	return func() tea.Msg {
		if m.session == nil || m.session.Client == nil {
			return pairStartedMsg{err: fmt.Errorf("not attached to the local API daemon")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var err error
		switch kind {
		case pairKindWhatsApp:
			err = m.session.Client.ConnectWhatsAppRiver(ctx, m.activeRiverID)
		case pairKindSignal:
			err = m.session.Client.ConnectSignalRiver(ctx, m.activeRiverID)
		default:
			err = fmt.Errorf("unsupported pair provider %q", kind)
		}
		return pairStartedMsg{err: err}
	}
}

func (m Model) livePairQRCmd() tea.Cmd {
	kind := m.pair.kind
	if kind != pairKindWhatsApp && kind != pairKindSignal {
		return nil
	}
	return func() tea.Msg {
		if m.session == nil || m.session.Client == nil {
			return pairQRMsg{kind: kind, missing: true}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		var qr localapi.QRCode
		var err error
		if kind == pairKindWhatsApp {
			qr, err = m.session.Client.WhatsAppQRRiver(ctx, m.activeRiverID)
		} else {
			qr, err = m.session.Client.SignalQRRiver(ctx, m.activeRiverID)
		}
		if localapi.IsQRUnavailable(err) {
			return pairQRMsg{kind: kind, missing: true}
		}
		if err != nil {
			return pairQRMsg{kind: kind, err: err}
		}
		return pairQRMsg{kind: kind, payload: qr.Payload(), updatedAt: qr.UpdatedAt}
	}
}

func (m Model) applyPairQR(msg pairQRMsg) (Model, tea.Cmd) {
	if !m.pair.open || msg.kind != m.pair.kind {
		return m, nil
	}
	if msg.err != nil {
		m.pair.pasteErr = msg.err.Error()
		return m.armPairTick()
	}
	if msg.missing {
		return m.armPairTick()
	}
	payload := strings.TrimSpace(msg.payload)
	if payload == "" || (payload == m.pair.qrPayload && m.pair.qrCells != "") {
		return m.armPairTick()
	}
	cells, graphic, cellW, cellH, err := compilePairQR(payload, m.pair.gfx, 0)
	if err != nil {
		m.pair.pasteErr = err.Error()
		return m.armPairTick()
	}
	hadGraphic := m.pair.qrGraphic != ""
	m.pair.qrPayload = payload
	m.pair.qrUpdatedAt = msg.updatedAt
	m.pair.qrCells = cells
	m.pair.qrGraphic = graphic
	m.pair.qrCellW = cellW
	m.pair.qrCellH = cellH
	m.pair.pasteErr = ""
	if !hadGraphic && graphic != "" {
		m.pair.ticking = false
	}
	return m.armPairTick()
}

func (m Model) syncLivePairFromStatus() (Model, tea.Cmd) {
	paired, connected := false, false
	lastErr := ""
	switch m.pair.kind {
	case pairKindWhatsApp:
		w := m.whatsappStatusForActive()
		paired, connected = w.Paired, w.Connected
		lastErr = w.LastError
	case pairKindSignal:
		s := m.signalStatusForActive()
		paired, connected = s.Paired, s.Connected
		lastErr = s.LastError
	}
	if paired && connected {
		m.pair.submitting = false
		m.pair.pasteErr = ""
		if m.pair.successUntil.IsZero() {
			m.pair.successUntil = time.Now().Add(1500 * time.Millisecond)
		}
		return m.armPairTick()
	}
	if lastErr != "" && strings.TrimSpace(m.pair.qrPayload) == "" {
		m.pair.pasteErr = lastErr
		if !paired {
			m.pair.submitting = false
		}
	}
	return m.armPairTick()
}

func (m Model) renderLivePairOverlay() string {
	kind := m.pairKind()
	title := "Pair WhatsApp"
	scanHint := "WhatsApp → Linked devices → Link a device"
	if kind == pairKindSignal {
		title = "Pair Signal"
		scanHint = "Signal → Linked devices → Link new device"
	}

	width := pairOverlayWidth
	if m.pair.qrCellW+4 > width {
		width = m.pair.qrCellW + 4
	}
	if m.width > 0 && width > m.width-4 {
		width = m.width - 4
	}
	if width < 40 {
		width = 40
	}
	innerW := width - 2
	if m.pair.qrGraphic != "" {
		innerW = width
		if m.pair.qrCellW > innerW {
			innerW = m.pair.qrCellW
		}
	}

	var rows []string
	rows = append(rows, paintLine(accentBoldStyle, title, innerW))

	switch {
	case !m.pair.successUntil.IsZero() && time.Now().Before(m.pair.successUntil):
		connectedName := "WhatsApp"
		if kind == pairKindSignal {
			connectedName = "Signal"
		}
		rows = append(rows,
			"",
			paintCenteredLine(okStyle, "Paired", innerW),
			paintCenteredLine(mutedStyle, connectedName+" is connected.", innerW),
		)
	default:
		rows = append(rows, "", paintLine(mutedStyle, scanHint, innerW), "")
		if qrRows := m.pairQRRows(innerW); len(qrRows) > 0 {
			rows = append(rows, qrRows...)
		} else {
			spin := m.pair.spinner()
			rows = append(rows, paintCenteredLine(mutedStyle, spin+" Waiting for a QR code…", innerW))
			if !m.pair.started.IsZero() {
				rows = append(rows, paintCenteredLine(mutedStyle, m.pair.elapsedLabel(), innerW))
			}
		}
		if m.pair.pasteErr != "" {
			rows = append(rows, "")
			for _, line := range wrapLines(m.pair.pasteErr, innerW) {
				rows = append(rows, paintLine(warnStyle, line, innerW))
			}
		}
		rows = append(rows, "", paintLine(mutedStyle, "esc close    enter retry", innerW))
	}

	body := strings.Join(rows, "\n")
	if m.pair.qrGraphic != "" {
		return body
	}
	box := focusBorderStyle.Width(innerW).Render(body)
	return padViewBox(box, lipgloss.Width(box), lipgloss.Height(box))
}

func (m Model) pairQRRows(innerW int) []string {
	if m.pair.qrGraphic != "" {
		rows := []string{m.pair.qrGraphic}
		skip := m.pair.qrCellH - 1
		if skip < 0 {
			skip = 0
		}
		for i := 0; i < skip; i++ {
			rows = append(rows, qrSkipLine)
		}
		return rows
	}
	if m.pair.qrCells == "" {
		return nil
	}
	var rows []string
	for _, line := range strings.Split(m.pair.qrCells, "\n") {
		rows = append(rows, paintCenteredLine(lipgloss.NewStyle(), line, innerW))
	}
	return rows
}
