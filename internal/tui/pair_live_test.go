package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
	"github.com/maxghenis/openmessage/internal/river"
)

func TestCanSendPerRiver(t *testing.T) {
	m := Model{activeRiverID: river.DefaultMessagesRiverID}
	if m.canSend() {
		t.Fatal("unpaired Google should not send")
	}
	m.status.Google.Paired = true
	m.status.Google.Connected = true
	if !m.canSend() {
		t.Fatal("paired Google should send")
	}

	m.activeRiverID = river.DefaultWhatsAppRiverID
	m.status.WhatsApp = localapi.WhatsAppStatus{Paired: true, Connected: true}
	if !m.canSend() {
		t.Fatal("connected WhatsApp should send even if we ignore Google")
	}
	m.status.WhatsApp.Connected = false
	if m.canSend() {
		t.Fatal("disconnected WhatsApp should not send")
	}

	m.activeRiverID = river.DefaultSignalRiverID
	m.status.Signal = localapi.SignalStatus{Paired: true, Connected: true}
	if !m.canSend() {
		t.Fatal("connected Signal should send")
	}
	m.status.Signal.NeedsReauth = true
	if m.canSend() {
		t.Fatal("Signal needs_reauth should not send")
	}
}

func TestRiverNeedsPairUsesActiveRiver(t *testing.T) {
	m := Model{
		activeRiverID: river.DefaultWhatsAppRiverID,
		status: localapi.DaemonStatus{
			Google:   localapi.GoogleStatus{Paired: true, Connected: true},
			WhatsApp: localapi.WhatsAppStatus{Paired: false},
		},
	}
	if !m.riverNeedsPair() {
		t.Fatal("unpaired WhatsApp river should need pair")
	}
	if m.googleNeedsPair() {
		t.Fatal("Google is paired; googleNeedsPair should be false")
	}

	m.activeRiverID = river.DefaultMessagesRiverID
	if m.riverNeedsPair() {
		t.Fatal("Messages river should not need pair when Google is paired")
	}
}

func TestWhatsAppPOpensQROverlayNotGooglePaste(t *testing.T) {
	m := Model{
		width:         80,
		height:        24,
		focus:         focusList,
		activeRiverID: river.DefaultWhatsAppRiverID,
		rivers:        []localapi.River{{ID: river.DefaultWhatsAppRiverID, Provider: river.ProviderWhatsApp, DisplayName: "WhatsApp"}},
		status: localapi.DaemonStatus{
			Google:   localapi.GoogleStatus{Paired: true, Connected: true},
			WhatsApp: localapi.WhatsAppStatus{Paired: false},
		},
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if !got.pair.open {
		t.Fatal("p should open the WhatsApp pair overlay")
	}
	if got.pair.kind != pairKindWhatsApp {
		t.Fatalf("kind = %q", got.pair.kind)
	}
	view := got.renderPairOverlay()
	if !strings.Contains(view, "Pair WhatsApp") {
		t.Fatalf("missing WhatsApp title:\n%s", view)
	}
	if strings.Contains(view, "Copy as cURL") || strings.Contains(view, "messages.google.com") {
		t.Fatalf("WhatsApp overlay showed Google paste steps:\n%s", view)
	}
}

func TestEmptyComposePPairsActiveWhatsAppRiver(t *testing.T) {
	m := Model{
		width:         80,
		height:        24,
		focus:         focusCompose,
		compose:       textarea.New(),
		activeRiverID: river.DefaultWhatsAppRiverID,
		rivers:        []localapi.River{{ID: river.DefaultWhatsAppRiverID, Provider: river.ProviderWhatsApp}},
		status:        localapi.DaemonStatus{WhatsApp: localapi.WhatsAppStatus{Paired: false}},
	}
	next, _ := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got := next.(Model)
	if !got.pair.open || got.pair.kind != pairKindWhatsApp {
		t.Fatalf("open=%v kind=%q", got.pair.open, got.pair.kind)
	}
}

func TestRenderLivePairOverlayShowsQR(t *testing.T) {
	cells, _, _, _, err := compilePairQR("whatsapp-pair-test", pairGfxCells, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cells == "" {
		t.Fatal("compilePairQR returned empty cells")
	}
	m := Model{
		width:  80,
		height: 40,
		pair: pairOverlay{
			open:      true,
			kind:      pairKindWhatsApp,
			qrCells:   cells,
			qrPayload: "whatsapp-pair-test",
		},
	}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "Pair WhatsApp") || !strings.Contains(got, "Linked devices") {
		t.Fatalf("overlay missing WhatsApp copy:\n%s", got)
	}
	if !strings.Contains(got, strings.Split(cells, "\n")[0]) {
		t.Fatalf("overlay missing QR art:\n%s", got)
	}
}

func TestSyncLivePairSurfacesSignalErrorWhileSubmitting(t *testing.T) {
	m := Model{
		pair: pairOverlay{open: true, kind: pairKindSignal, submitting: true},
		status: localapi.DaemonStatus{
			Signal: localapi.SignalStatus{
				LastError: `exec: "script": executable file not found in %PATH%`,
			},
		},
	}
	next, _ := m.syncLivePairFromStatus()
	if !strings.Contains(next.pair.pasteErr, "script") {
		t.Fatalf("pasteErr = %q", next.pair.pasteErr)
	}
	if next.pair.submitting {
		t.Fatal("failed Signal pair should drop the spinner")
	}
}

func TestSyncLivePairKeepsQRWhenSignalHasLastError(t *testing.T) {
	m := Model{
		pair: pairOverlay{
			open:       true,
			kind:       pairKindSignal,
			submitting: true,
			qrPayload:  "sgnl://linkdevice?uuid=test",
		},
		status: localapi.DaemonStatus{
			Signal: localapi.SignalStatus{
				LastError: `list Signal accounts: exit status 1: INFO AccountHelper - The Signal protocol expects that incoming messages are regularly received.: [{"number":"+16015758787"}]`,
			},
		},
	}
	next, _ := m.syncLivePairFromStatus()
	if next.pair.pasteErr != "" {
		t.Fatalf("in-flight QR should not surface listAccounts noise: %q", next.pair.pasteErr)
	}
	if !next.pair.submitting {
		t.Fatal("in-flight QR should keep waiting for scan")
	}
}

func TestRenderSignalPairOverlayTitle(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true, kind: pairKindSignal}}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "Pair Signal") {
		t.Fatalf("missing Signal title:\n%s", got)
	}
}

func TestCanSendExtraWhatsAppUsesRiverStatus(t *testing.T) {
	m := Model{
		activeRiverID: "whatsapp-2",
		rivers:        []localapi.River{{ID: "whatsapp-2", Provider: river.ProviderWhatsApp}},
		status: localapi.DaemonStatus{
			WhatsApp: localapi.WhatsAppStatus{Paired: true, Connected: true},
			WhatsAppRivers: []localapi.WhatsAppStatus{
				{RiverID: "whatsapp-2", Paired: true, Connected: true},
			},
		},
	}
	if !m.canSend() {
		t.Fatal("extra WhatsApp should send from its own status")
	}
	m.status.WhatsAppRivers[0].Connected = false
	if m.canSend() {
		t.Fatal("disconnected extra WhatsApp should not send even if default is up")
	}
}

func TestLiveRiverCreatedOpensWhatsAppPair(t *testing.T) {
	m := Model{width: 80, height: 24, activeRiverID: river.DefaultMessagesRiverID}
	next, _ := m.Update(liveRiverCreatedMsg{
		ID:          "whatsapp-2",
		Provider:    river.ProviderWhatsApp,
		DisplayName: "WhatsApp 2",
	})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if got.activeRiverID != "whatsapp-2" || !got.pair.open || got.pair.kind != pairKindWhatsApp {
		t.Fatalf("active=%q open=%v kind=%q", got.activeRiverID, got.pair.open, got.pair.kind)
	}
}

func TestSendBlockedReasonFollowsRiver(t *testing.T) {
	m := Model{activeRiverID: river.DefaultWhatsAppRiverID}
	if !strings.Contains(m.sendBlockedReason(), "WhatsApp") {
		t.Fatalf("got %q", m.sendBlockedReason())
	}
	m.activeRiverID = river.DefaultSignalRiverID
	if !strings.Contains(m.sendBlockedReason(), "Signal") {
		t.Fatalf("got %q", m.sendBlockedReason())
	}
}

func TestWhatsAppStatusBarWhenUnpaired(t *testing.T) {
	m := Model{
		width:         80,
		activeRiverID: river.DefaultWhatsAppRiverID,
		rivers:        []localapi.River{{ID: river.DefaultWhatsAppRiverID, Provider: river.ProviderWhatsApp, DisplayName: "WhatsApp"}},
	}
	got := m.renderStatusBar()
	if !strings.Contains(got, "WhatsApp") || !strings.Contains(got, "press p to pair") {
		t.Fatalf("status = %q", got)
	}
	if strings.Contains(got, "Google Messages") {
		t.Fatalf("WhatsApp river still advertised Google:\n%s", got)
	}
}
