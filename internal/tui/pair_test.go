package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/maxghenis/openmessage/internal/localapi"
)

func TestGoogleNeedsPair(t *testing.T) {
	var m Model
	if !m.googleNeedsPair() {
		t.Fatal("zero status should need pairing")
	}
	m.status.Google.Paired = true
	m.status.Google.Connected = true
	if m.googleNeedsPair() {
		t.Fatal("paired+connected should not need pairing")
	}
	m.status.Google.NeedsRepair = true
	if !m.googleNeedsPair() {
		t.Fatal("needs_repair should offer re-pair")
	}
}

func TestEmptyComposePOpensPairOverlay(t *testing.T) {
	m := Model{width: 80, height: 24, focus: focusCompose, compose: textarea.New()}
	next, cmd := m.updateComposeKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if !got.pair.open {
		t.Fatal("empty composer p should open pair overlay when unpaired")
	}
	if got.pair.submitting {
		t.Fatal("opening the overlay must not auto-read Chrome")
	}
	if cmd != nil {
		t.Fatal("opening the overlay must wait for a paste")
	}
	view := got.renderPairOverlay()
	if !strings.Contains(view, "Copy as cURL") {
		t.Fatalf("overlay missing paste steps:\n%s", view)
	}
	if strings.Contains(view, "pair from Chrome") || strings.Contains(view, "Reading Chrome cookies") {
		t.Fatalf("overlay still tries Chrome auto-read:\n%s", view)
	}
}

func TestRenderPairOverlayShowsEmoji(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{
		Phase: "waiting_confirm",
		Emoji: "🦊",
	}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "🦊") || !strings.Contains(got, "Tap this emoji") {
		t.Fatalf("overlay missing emoji prompt:\n%s", got)
	}
}

func TestRenderPairOverlayAsksToQuitChrome(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_browser"}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "Quit Google Chrome") {
		t.Fatalf("overlay missing quit-Chrome prompt:\n%s", got)
	}
	if strings.Contains(got, "Chrome window") || strings.Contains(got, "Sign in to Google") {
		t.Fatalf("overlay still asks for a debug Chrome login:\n%s", got)
	}
}

func TestRenderPairOverlayIdleShowsPasteSteps(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "Copy as cURL") || !strings.Contains(got, "messages.google.com") {
		t.Fatalf("idle overlay missing paste steps:\n%s", got)
	}
	if strings.Contains(got, "pair from Chrome") {
		t.Fatalf("idle overlay still offers Chrome auto-read:\n%s", got)
	}
}

func TestEscDismissesPairOverlayWhileDaemonPairing(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_browser"}
	next, _ := m.updatePairKeys(tea.KeyMsg{Type: tea.KeyEsc})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if got.pair.open {
		t.Fatal("esc should close the overlay")
	}
	if !got.pair.dismissed {
		t.Fatal("esc should mark the overlay dismissed")
	}
	got.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_browser"}
	got, _ = got.syncPairOverlayFromStatus()
	if got.pair.open {
		t.Fatal("status sync must not reopen a dismissed overlay")
	}
}

func TestPairStatusFailedShowsError(t *testing.T) {
	m := Model{
		width:  80,
		height: 24,
		pair:   pairOverlay{open: true},
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "failed", Error: "Chrome is still open"}
	got, _ := m.syncPairOverlayFromStatus()
	if got.pair.submitting {
		t.Fatal("idle failed pairing should not spin")
	}
	if got.pair.pasteErr != "Chrome is still open" {
		t.Fatalf("pasteErr = %q", got.pair.pasteErr)
	}
	view := got.renderPairOverlay()
	if !strings.Contains(view, "Chrome is still open") {
		t.Fatalf("failed overlay missing error:\n%s", view)
	}
}

func TestStaleFailedStatusDoesNotAbortRetry(t *testing.T) {
	m := Model{
		width:  80,
		height: 24,
		pair: pairOverlay{
			open:              true,
			submitting:        true,
			started:           time.Now(),
			ignoreFailedUntil: time.Now().Add(time.Second),
		},
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "failed", Error: "Chrome profile Default is missing SID"}
	got, cmd := m.syncPairOverlayFromStatus()
	if !got.pair.submitting {
		t.Fatal("in-flight retry must keep the spinner")
	}
	if cmd == nil {
		t.Fatal("in-flight retry must keep ticking")
	}
	view := got.renderPairOverlay()
	if !strings.Contains(view, pairSpinnerFrames[0]) {
		t.Fatalf("retry overlay should show progress, not the stale error:\n%s", view)
	}
	if !strings.Contains(view, "Talking to Google") {
		t.Fatalf("retry should stay on Google, not Chrome:\n%s", view)
	}
	if strings.Contains(view, "Reading Chrome cookies") {
		t.Fatalf("paste retry showed Chrome read:\n%s", view)
	}
}

func TestFailedStatusStopsSpinnerAfterGrace(t *testing.T) {
	m := Model{
		width:  80,
		height: 24,
		pair: pairOverlay{
			open:              true,
			submitting:        true,
			started:           time.Now().Add(-11 * time.Minute),
			ignoreFailedUntil: time.Now().Add(-time.Second),
		},
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{
		Phase: "failed",
		Error: "Chrome did not start for a cookie read. Press enter to retry, or paste with ctrl+v",
	}
	got, _ := m.syncPairOverlayFromStatus()
	if got.pair.submitting {
		t.Fatal("real failure must stop the spinner")
	}
	if got.pair.pasteErr == "" {
		t.Fatal("real failure must surface the daemon error")
	}
	view := got.renderPairOverlay()
	if strings.Contains(view, "Reading Chrome cookies") {
		t.Fatalf("failed overlay still spinning:\n%s", view)
	}
	if !strings.Contains(view, "did not start for a cookie read") {
		t.Fatalf("failed overlay missing error:\n%s", view)
	}
}

func TestEnterOnFailedAsksToPaste(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{
		Phase: "failed",
		Error: "read Chrome cookies: missing required cookies: .google.com:SID, .google.com:HSID",
	}
	next, cmd := m.updatePairKeys(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if got.pair.submitting {
		t.Fatal("enter must not start a Chrome retry")
	}
	if cmd != nil {
		t.Fatal("enter must not start pairing")
	}
	if !strings.Contains(got.pair.pasteErr, "ctrl+v") {
		t.Fatalf("pasteErr = %q", got.pair.pasteErr)
	}
	view := got.renderPairOverlay()
	if !strings.Contains(view, "Copy as cURL") {
		t.Fatalf("failed overlay missing paste steps:\n%s", view)
	}
	if strings.Contains(view, "Reading Chrome cookies") {
		t.Fatalf("enter started a Chrome read:\n%s", view)
	}
}

func TestFailedOverlayWrapsCookieNames(t *testing.T) {
	m := Model{
		width:  80,
		height: 24,
		pair: pairOverlay{
			open:     true,
			pasteErr: "Chrome profile Profile 1 is missing SID, HSID, SSID, APISID, SAPISID. Sign into Google in that profile, quit Chrome fully, then retry",
		},
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "failed", Error: m.pair.pasteErr}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "SID") || !strings.Contains(got, "SAPISID") {
		t.Fatalf("wrapped error dropped cookie names:\n%s", got)
	}
	if strings.Contains(got, "SI…") {
		t.Fatalf("error still truncated mid-cookie:\n%s", got)
	}
}

func TestPairOverlayCtrlCQuits(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true, submitting: true}}
	_, cmd := m.updatePairKeys(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should quit")
	}
}

func TestRenderPairOverlayBusyShowsProgress(t *testing.T) {
	m := Model{
		width:  80,
		height: 24,
		pair: pairOverlay{
			open:       true,
			submitting: true,
			started:    time.Now().Add(-5 * time.Second),
		},
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_browser"}
	got := m.renderPairOverlay()
	if !strings.Contains(got, "Quit Google Chrome") {
		t.Fatalf("missing quit Chrome copy:\n%s", got)
	}
	if !strings.Contains(got, "Waiting for Chrome's files to unlock") {
		t.Fatalf("missing live wait copy:\n%s", got)
	}
	if !strings.Contains(got, "elapsed") {
		t.Fatalf("missing elapsed timer:\n%s", got)
	}
	if !strings.Contains(got, pairSpinnerFrames[0]) {
		t.Fatalf("missing spinner:\n%s", got)
	}
	for _, step := range pairStepNames {
		if !strings.Contains(got, step) {
			t.Fatalf("missing step %q:\n%s", step, got)
		}
	}
}

func TestPairSpinnerAdvances(t *testing.T) {
	a := pairOverlay{ticks: 0}.spinner()
	b := pairOverlay{ticks: 1}.spinner()
	if a == b {
		t.Fatal("spinner should change between ticks")
	}
	if a != pairSpinnerFrames[0] || b != pairSpinnerFrames[1] {
		t.Fatalf("spinner frames = %q %q", a, b)
	}
}

func TestPairElapsedLabel(t *testing.T) {
	got := pairOverlay{started: time.Now().Add(-65 * time.Second)}.elapsedLabel()
	if got != "1:05 elapsed" && got != "1:06 elapsed" {
		t.Fatalf("elapsed = %q", got)
	}
}

func TestIdlePairOverlayHasNoSpinnerTimer(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	got := m.renderPairOverlay()
	if strings.Contains(got, "elapsed") {
		t.Fatalf("idle overlay should not tick a timer:\n%s", got)
	}
}

func TestPairBusyPhases(t *testing.T) {
	var m Model
	m.pair.submitting = true
	if !m.pairBusy() {
		t.Fatal("submitting should be busy")
	}
	m.pair.submitting = false
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_confirm"}
	if !m.pairBusy() {
		t.Fatal("waiting_confirm should be busy")
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "failed"}
	if m.pairBusy() {
		t.Fatal("failed should not spin")
	}
}

func TestQClosesPairOverlayWithoutCancel(t *testing.T) {
	m := Model{width: 80, height: 24, pair: pairOverlay{open: true}}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_confirm", Emoji: "🦊"}
	next, cmd := m.updatePairKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if got.pair.open {
		t.Fatal("q should close the overlay")
	}
	if cmd != nil {
		t.Fatal("q must not cancel pairing")
	}
}

func TestSyncPairOverlayDoesNotStartSecondTick(t *testing.T) {
	m := Model{
		width:  80,
		height: 24,
		pair: pairOverlay{
			open:    true,
			ticking: true,
		},
	}
	m.status.Google.Pairing = &localapi.GooglePairingStatus{Phase: "waiting_confirm"}
	_, cmd := m.syncPairOverlayFromStatus()
	if cmd != nil {
		t.Fatal("status sync must not start a second tick chain")
	}
}

func TestPairTickAdvancesFrame(t *testing.T) {
	m := Model{pair: pairOverlay{open: true, submitting: true, started: time.Now()}}
	next, cmd := m.Update(pairTickMsg{})
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("next type %T", next)
	}
	if got.pair.ticks != 1 {
		t.Fatalf("ticks = %d", got.pair.ticks)
	}
	if cmd == nil {
		t.Fatal("busy overlay should keep ticking")
	}
}
