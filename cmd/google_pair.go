package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/client"
)

const (
	googlePairPhaseStarting       = "starting"
	googlePairPhaseReadingChrome  = "reading_chrome"
	googlePairPhaseWaitingBrowser = "waiting_browser"
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
	if c.closed {
		c.mu.Unlock()
		return bridge.ErrSupervisorStopped
	}
	if c.pairing != nil && c.pairing.phase != googlePairPhaseFailed {
		c.mu.Unlock()
		return app.ErrGooglePairingInProgress
	}
	ctx, cancel := context.WithTimeout(context.Background(), googlePairPhoneTimeout)
	phase := googlePairPhaseStarting
	c.pairing = &googlePairRuntime{cancel: cancel, phase: phase}
	startGaia := c.startGaia
	loadCookies := c.loadCookies
	c.mu.Unlock()
	c.notifyPairingChange()

	go c.runGoogleAccountPair(ctx, cancel, cookies, startGaia, loadCookies)
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
	if err := backupAndRemoveSession(c.sessionPath); err != nil {
		fail(err)
		return
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), googleSupervisorPolicy().ConnectTimeout)
	defer cancel()
	if err := supervisor.Start(ctx, bridge.StartRequest{}); err != nil && !errors.Is(err, bridge.ErrSupervisorBusy) {
		return err
	}
	return waitForGoogleOnline(ctx, supervisor)
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

func backupAndRemoveSession(sessionPath string) error {
	if _, err := os.Stat(sessionPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat Google session: %w", err)
	}
	backupPath := sessionPath + ".bak"
	if err := os.Rename(sessionPath, backupPath); err != nil {
		return fmt.Errorf("backup Google session: %w", err)
	}
	return nil
}
