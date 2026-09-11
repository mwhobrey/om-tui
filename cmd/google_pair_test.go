package cmd

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/bridge"
	"github.com/maxghenis/openmessage/internal/client"
)

func TestGoogleSupervisorControlAccountPairSavesSessionAndReconnects(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	lifecycle := &googleRepairTestLifecycle{}
	var supervisorCount atomic.Int32
	newSupervisor := func() (*bridge.Supervisor, error) {
		supervisorCount.Add(1)
		return bridge.NewSupervisor(
			googleAccountID,
			bridge.PlatformGoogle,
			lifecycle,
			googleSupervisorPolicy(),
			googleWallClock{},
			googleRandom{},
		)
	}
	first, err := newSupervisor()
	if err != nil {
		t.Fatalf("NewSupervisor(): %v", err)
	}
	control := newGoogleSupervisorControl(first, sessionPath, newSupervisor, zerolog.Nop(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Stop(ctx)
	})

	finishCh := make(chan struct{})
	control.startGaia = func(ctx context.Context, cookies map[string]string) (googleGaiaAttempt, error) {
		if cookies["SID"] != "sid-value" {
			t.Fatalf("cookies = %#v", cookies)
		}
		return googleGaiaAttempt{
			Emoji: "🦊",
			Finish: func(context.Context) (*client.SessionData, error) {
				<-finishCh
				return &client.SessionData{AuthDataJSON: []byte(`{"cookies":{"SID":"sid-value"}}`)}, nil
			},
			Disconnect: func() {},
		}, nil
	}

	if err := control.StartGoogleAccountPair(map[string]string{"SID": "sid-value"}); err != nil {
		t.Fatalf("StartGoogleAccountPair(): %v", err)
	}
	if err := control.StartGoogleAccountPair(map[string]string{"SID": "other"}); err != app.ErrGooglePairingInProgress {
		t.Fatalf("second start error = %v, want in progress", err)
	}

	waitFor := func(wantPhase, wantEmoji string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			snap := control.PairingSnapshot()
			if snap != nil && snap.Phase == wantPhase && snap.Emoji == wantEmoji {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("pairing = %#v, want phase %q emoji %q", control.PairingSnapshot(), wantPhase, wantEmoji)
	}
	waitFor(googlePairPhaseWaitingConfirm, "🦊")
	close(finishCh)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if control.PairingSnapshot() == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pairing still active: %#v", control.PairingSnapshot())
}

func TestGoogleSupervisorControlAccountPairRequiresCookies(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	lifecycle := &googleRepairTestLifecycle{}
	newSupervisor := func() (*bridge.Supervisor, error) {
		return bridge.NewSupervisor(
			googleAccountID,
			bridge.PlatformGoogle,
			lifecycle,
			googleSupervisorPolicy(),
			googleWallClock{},
			googleRandom{},
		)
	}
	first, err := newSupervisor()
	if err != nil {
		t.Fatalf("NewSupervisor(): %v", err)
	}
	control := newGoogleSupervisorControl(first, sessionPath, newSupervisor, zerolog.Nop(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Stop(ctx)
	})

	control.loadCookies = func(ctx context.Context, onNeedBrowser func()) (map[string]string, error) {
		t.Fatal("Chrome auto-read must not run")
		return nil, nil
	}
	if err := control.StartGoogleAccountPair(nil); err == nil || !strings.Contains(err.Error(), "paste Google cookies") {
		t.Fatalf("StartGoogleAccountPair() error = %v, want paste required", err)
	}
}

func TestBackupAndRemoveSessionRestoresWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.json"
	if err := os.WriteFile(path, []byte("old-session"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := backupAndRemoveSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a backup for an existing session")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("session should be moved aside")
	}
	restoreSessionBackup(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "old-session" {
		t.Fatalf("restored %q", raw)
	}
}

func TestRestoreSessionBackupDoesNotClobberNewSession(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/session.json"
	if err := os.WriteFile(path+".bak", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoreSessionBackup(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new" {
		t.Fatalf("clobbered new session: %q", raw)
	}
}

func TestStopAndUnpairCancelsInFlightPairing(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	if err := os.WriteFile(sessionPath, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := &googleRepairTestLifecycle{}
	newSupervisor := func() (*bridge.Supervisor, error) {
		return bridge.NewSupervisor(
			googleAccountID,
			bridge.PlatformGoogle,
			lifecycle,
			googleSupervisorPolicy(),
			googleWallClock{},
			googleRandom{},
		)
	}
	first, err := newSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	control := newGoogleSupervisorControl(first, sessionPath, newSupervisor, zerolog.Nop(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Stop(ctx)
	})

	blocked := make(chan struct{})
	control.startGaia = func(ctx context.Context, cookies map[string]string) (googleGaiaAttempt, error) {
		return googleGaiaAttempt{
			Emoji: "🦊",
			Finish: func(ctx context.Context) (*client.SessionData, error) {
				close(blocked)
				<-ctx.Done()
				return nil, ctx.Err()
			},
			Disconnect: func() {},
		}, nil
	}
	if err := control.StartGoogleAccountPair(map[string]string{"SID": "sid-value"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("pairing did not reach phone confirm")
	}
	if err := control.StopAndUnpair(func() error { return os.Remove(sessionPath) }); err != nil {
		t.Fatalf("StopAndUnpair(): %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatal("unpair should remove the session")
	}
}

func TestFailedPairDoesNotRestoreStaleBackup(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	if err := os.WriteFile(sessionPath+".bak", []byte(`{"unpaired":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := &googleRepairTestLifecycle{}
	newSupervisor := func() (*bridge.Supervisor, error) {
		return bridge.NewSupervisor(
			googleAccountID,
			bridge.PlatformGoogle,
			lifecycle,
			googleSupervisorPolicy(),
			googleWallClock{},
			googleRandom{},
		)
	}
	first, err := newSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	control := newGoogleSupervisorControl(first, sessionPath, newSupervisor, zerolog.Nop(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Stop(ctx)
	})
	control.startGaia = func(ctx context.Context, cookies map[string]string) (googleGaiaAttempt, error) {
		return googleGaiaAttempt{}, errors.New("gaia failed")
	}
	if err := control.StartGoogleAccountPair(map[string]string{"SID": "sid-value"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := control.PairingSnapshot()
		if snap != nil && snap.Phase == googlePairPhaseFailed {
			if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
				t.Fatal("stale backup must not be restored as session.json")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pairing = %#v, want failed", control.PairingSnapshot())
}

func TestStopAndUnpairRemovesSessionBackup(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	if err := os.WriteFile(sessionPath, []byte(`{"new":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath+".bak", []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := &googleRepairTestLifecycle{}
	newSupervisor := func() (*bridge.Supervisor, error) {
		return bridge.NewSupervisor(
			googleAccountID,
			bridge.PlatformGoogle,
			lifecycle,
			googleSupervisorPolicy(),
			googleWallClock{},
			googleRandom{},
		)
	}
	first, err := newSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	control := newGoogleSupervisorControl(first, sessionPath, newSupervisor, zerolog.Nop(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Stop(ctx)
	})
	if err := control.StopAndUnpair(func() error { return os.Remove(sessionPath) }); err != nil {
		t.Fatalf("StopAndUnpair(): %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatal("unpair should remove the session")
	}
	if _, err := os.Stat(sessionPath + ".bak"); !os.IsNotExist(err) {
		t.Fatal("unpair should remove the session backup")
	}
}

func TestStartGoogleAccountPairRejectedDuringUnpair(t *testing.T) {
	sessionPath := t.TempDir() + "/session.json"
	if err := os.WriteFile(sessionPath, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := &googleRepairTestLifecycle{}
	newSupervisor := func() (*bridge.Supervisor, error) {
		return bridge.NewSupervisor(
			googleAccountID,
			bridge.PlatformGoogle,
			lifecycle,
			googleSupervisorPolicy(),
			googleWallClock{},
			googleRandom{},
		)
	}
	first, err := newSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	control := newGoogleSupervisorControl(first, sessionPath, newSupervisor, zerolog.Nop(), nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = control.Stop(ctx)
	})

	unpairStarted := make(chan struct{})
	unpairRelease := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- control.StopAndUnpair(func() error {
			close(unpairStarted)
			<-unpairRelease
			return os.Remove(sessionPath)
		})
	}()
	select {
	case <-unpairStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("unpair did not start")
	}
	if err := control.StartGoogleAccountPair(map[string]string{"SID": "sid-value"}); !errors.Is(err, bridge.ErrSupervisorStopped) {
		t.Fatalf("StartGoogleAccountPair() during unpair = %v, want ErrSupervisorStopped", err)
	}
	close(unpairRelease)
	if err := <-errCh; err != nil {
		t.Fatalf("StopAndUnpair(): %v", err)
	}
}
