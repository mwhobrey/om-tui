package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/client"
	"github.com/maxghenis/openmessage/internal/googlecookies"
)

const (
	googlePairPhaseStarting       = "starting"
	googlePairPhaseReadingChrome  = "reading_chrome"
	googlePairPhaseWaitingBrowser = "waiting_browser"
	googlePairPhaseRefreshing     = "refreshing_cookies"
	googlePairPhaseWaitingConfirm = "waiting_confirm"
	googlePairPhaseFinishing      = "finishing"
	googlePairPhaseFailed         = "failed"
)

type googleGaiaAttempt struct {
	Emoji      string
	Finish     func(context.Context) (*client.SessionData, error)
	Disconnect func()
}

type googlePairRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
	phase  string
	emoji  string
	err    string
}

func (c *googleSupervisorControl) PairingSnapshot() *app.GooglePairingSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pairing == nil {
		return nil
	}
	return &app.GooglePairingSnapshot{
		Phase: c.pairing.phase,
		Emoji: c.pairing.emoji,
		Error: c.pairing.err,
	}
}

func (c *googleSupervisorControl) StartGoogleAccountPair(cookies map[string]string) error {
	if len(cookies) == 0 {
		return errors.New("paste Google cookies (curl or Cookie header from messages.google.com)")
	}

	c.mu.Lock()
	if c.closed || c.unpairing {
		c.mu.Unlock()
		return bridge.ErrSupervisorStopped
	}
	if c.pairing != nil && c.pairing.phase != googlePairPhaseFailed {
		c.mu.Unlock()
		return app.ErrGooglePairingInProgress
	}
	ctx, cancel := context.WithTimeout(context.Background(), googlePairPhoneTimeout)
	done := make(chan struct{})
	c.pairing = &googlePairRuntime{cancel: cancel, done: done, phase: googlePairPhaseStarting}
	startGaia := c.startGaia
	loadCookies := c.loadCookies
	c.mu.Unlock()
	c.notifyPairingChange()

	go func() {
		defer close(done)
		c.runGoogleAccountPair(ctx, cancel, cookies, startGaia, loadCookies)
	}()
	return nil
}

func (c *googleSupervisorControl) CancelGoogleAccountPair() {
	c.mu.Lock()
	runtime := c.pairing
	c.mu.Unlock()
	if runtime != nil && runtime.cancel != nil {
		runtime.cancel()
	}
}

func (c *googleSupervisorControl) waitPairingDone() {
	c.mu.Lock()
	runtime := c.pairing
	c.mu.Unlock()
	if runtime != nil && runtime.done != nil {
		<-runtime.done
	}
}

func (c *googleSupervisorControl) runGoogleAccountPair(
	ctx context.Context,
	cancel context.CancelFunc,
	cookies map[string]string,
	startGaia func(context.Context, map[string]string) (googleGaiaAttempt, error),
	loadCookies func(context.Context, func()) (map[string]string, error),
) {
	defer cancel()

	fail := func(err error) {
		c.setPairing(googlePairPhaseFailed, "", err.Error())
	}

	if len(cookies) == 0 {
		if loadCookies == nil {
			fail(errors.New("Google cookies are required"))
			return
		}
		loaded, err := loadCookies(ctx, func() {
			c.setPairing(googlePairPhaseWaitingBrowser, "", "")
		})
		if err != nil {
			fail(fmt.Errorf("read Chrome cookies: %w", err))
			return
		}
		cookies = loaded
		c.setPairing(googlePairPhaseStarting, "", "")
	}

	if err := c.parkSupervisor(); err != nil {
		fail(fmt.Errorf("park Google Messages: %w", err))
		return
	}

	if sessionHasPairedAuth(c.sessionPath) {
		c.setPairing(googlePairPhaseRefreshing, "", "")
		if err := googlecookies.UpdateSessionCookies(c.sessionPath, cookies); err != nil {
			c.logger.Warn().Err(err).Msg("Google cookie paste could not rewrite the existing session")
		} else if err := c.reconnectAfterPair(); err != nil {
			c.logger.Warn().Err(err).Msg("Google cookie paste reconnect failed; falling back to account pairing")
		} else {
			c.mu.Lock()
			c.pairing = nil
			c.mu.Unlock()
			c.notifyPairingChange()
			return
		}
	}

	createdBackup, err := backupAndRemoveSession(c.sessionPath)
	if err != nil {
		fail(err)
		return
	}
	sessionSaved := false
	defer func() {
		if !sessionSaved && createdBackup {
			restoreSessionBackup(c.sessionPath)
		}
	}()

	if startGaia == nil {
		fail(errors.New("Google account pairing is not configured"))
		return
	}
	attempt, err := startGaia(ctx, cookies)
	if err != nil {
		fail(fmt.Errorf("start Google account pairing: %w", err))
		return
	}
	if attempt.Disconnect != nil {
		defer attempt.Disconnect()
	}

	c.setPairing(googlePairPhaseWaitingConfirm, attempt.Emoji, "")

	sessionData, err := attempt.Finish(ctx)
	if err != nil {
		fail(fmt.Errorf("finish Google account pairing: %w", err))
		return
	}
	c.setPairing(googlePairPhaseFinishing, attempt.Emoji, "")
	if err := client.SaveSession(c.sessionPath, sessionData); err != nil {
		fail(fmt.Errorf("save Google session: %w", err))
		return
	}
	sessionSaved = true
	removeSessionBackup(c.sessionPath)
	if attempt.Disconnect != nil {
		attempt.Disconnect()
		attempt.Disconnect = nil
	}
	if err := c.reconnectAfterPair(); err != nil {
		fail(fmt.Errorf("connect after pairing: %w", err))
		return
	}

	c.mu.Lock()
	c.pairing = nil
	c.mu.Unlock()
	c.notifyPairingChange()
}

func (c *googleSupervisorControl) reconnectAfterPair() error {
	if err := c.startSupervisorFromSession(); err != nil {
		return err
	}
	c.mu.Lock()
	supervisor := c.supervisor
	c.mu.Unlock()
	if supervisor == nil {
		return errors.New("Google Messages supervisor missing after restart")
	}
	ctx, cancel := context.WithTimeout(context.Background(), googleSupervisorPolicy().ConnectTimeout)
	defer cancel()
	return waitForGoogleOnline(ctx, supervisor)
}

// startSupervisorFromSession rebuilds the supervisor from session.json and
// admits Start without waiting for Online. Cookie-bridge reconnects use this
// so a wedged Google connect cannot pin a goroutine for the full ConnectTimeout.
func (c *googleSupervisorControl) startSupervisorFromSession() error {
	fingerprint, err := googleSessionFingerprint(c.sessionPath)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return bridge.ErrSupervisorStopped
	}
	if c.newSupervisor == nil {
		c.mu.Unlock()
		return errors.New("Google Messages supervisor cannot be restarted")
	}
	supervisor, err := c.newSupervisor()
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("rebuild Google Messages supervisor: %w", err)
	}
	c.supervisor = supervisor
	c.inputFingerprint = fingerprint
	c.supervisorStopped = false
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := supervisor.Start(ctx, bridge.StartRequest{}); err != nil && !errors.Is(err, bridge.ErrSupervisorBusy) {
		return err
	}
	return nil
}

// ApplySessionCookies rewrites Gaia cookies in session.json and starts a fresh
// supervisor generation from disk. It must not call InputsChanged: for a
// credentials-expired Blocked state that re-enters paced credential repair
// (90s floor) instead of loading the cookies we just wrote.
//
// Reconnect runs asynchronously so the Chrome native host is not held inside
// /api/google/cookie-bridge/push (that deadlocks RequestCookies).
func (c *googleSupervisorControl) ApplySessionCookies(cookies map[string]string) error {
	if err := googlecookies.UpdateSessionCookies(c.sessionPath, cookies); err != nil {
		return err
	}
	c.startCookieReconnectAsync()
	return nil
}

// RebuildAfterSessionChange starts a fresh generation after session.json was
// rewritten out-of-band (e.g. refreshGoogleSessionCookies).
func (c *googleSupervisorControl) RebuildAfterSessionChange() error {
	c.startCookieReconnectAsync()
	return nil
}

func (c *googleSupervisorControl) startCookieReconnectAsync() {
	c.cookieReconnectMu.Lock()
	if c.cookieReconnectQueued {
		c.cookieReconnectMu.Unlock()
		return
	}
	// Coalesce bursts (extension push + repair refresh) but never drop a
	// cookie write for 30s — that left session.json updated while the
	// supervisor kept running on the previous generation.
	if time.Since(c.lastCookieReconnect) < 2*time.Second {
		c.cookieReconnectMu.Unlock()
		return
	}
	c.cookieReconnectQueued = true
	c.lastCookieReconnect = time.Now()
	c.cookieReconnectMu.Unlock()

	go func() {
		defer func() {
			c.cookieReconnectMu.Lock()
			c.cookieReconnectQueued = false
			c.cookieReconnectMu.Unlock()
		}()
		if err := c.parkSupervisor(); err != nil {
			c.logger.Warn().Err(err).Msg("park Google supervisor before cookie reconnect failed")
		}
		// Admit Start only — never waitForGoogleOnline here. A hung Google
		// connect used to pin this goroutine for minutes and stack parks from
		// repeated bridge pushes until the daemon looked wedged.
		if err := c.startSupervisorFromSession(); err != nil {
			c.logger.Warn().Err(err).Msg("Google cookie reconnect failed")
		}
	}()
}

func (c *googleSupervisorControl) startLiveGaia(
	ctx context.Context,
	cookies map[string]string,
) (googleGaiaAttempt, error) {
	cli := client.NewForPairing(c.logger)
	cli.GM.AuthData.SetCookies(cookies)
	emoji, session, err := cli.GM.StartGaiaPairing(ctx)
	if err != nil {
		cli.GM.Disconnect()
		return googleGaiaAttempt{}, err
	}
	return googleGaiaAttempt{
		Emoji: emoji,
		Finish: func(ctx context.Context) (*client.SessionData, error) {
			if _, err := cli.GM.FinishGaiaPairing(ctx, session); err != nil {
				return nil, err
			}
			return cli.SessionData()
		},
		Disconnect: func() { cli.GM.Disconnect() },
	}, nil
}

func (c *googleSupervisorControl) setPairing(phase, emoji, errText string) {
	c.mu.Lock()
	if c.pairing == nil {
		c.pairing = &googlePairRuntime{}
	}
	c.pairing.phase = phase
	if emoji != "" {
		c.pairing.emoji = emoji
	}
	c.pairing.err = errText
	c.mu.Unlock()
	c.notifyPairingChange()
}

func (c *googleSupervisorControl) notifyPairingChange() {
	if c.onChange != nil {
		c.onChange()
	}
}

func sessionHasPairedAuth(sessionPath string) bool {
	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		return false
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil {
		return false
	}
	auth, ok := data["auth_data"].(map[string]any)
	if !ok || len(auth) == 0 {
		return false
	}
	cookies, _ := auth["cookies"].(map[string]any)
	return len(cookies) > 0
}

func backupAndRemoveSession(sessionPath string) (created bool, err error) {
	if _, err := os.Stat(sessionPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat Google session: %w", err)
	}
	backupPath := sessionPath + ".bak"
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Remove(backupPath); err != nil {
			return false, fmt.Errorf("rotate Google session backup: %w", err)
		}
	}
	if err := os.Rename(sessionPath, backupPath); err != nil {
		return false, fmt.Errorf("backup Google session: %w", err)
	}
	return true, nil
}

func restoreSessionBackup(sessionPath string) {
	backupPath := sessionPath + ".bak"
	if _, err := os.Stat(backupPath); err != nil {
		return
	}
	if _, err := os.Stat(sessionPath); err == nil {
		return
	}
	_ = os.Rename(backupPath, sessionPath)
}

func removeSessionBackup(sessionPath string) {
	_ = os.Remove(sessionPath + ".bak")
}
