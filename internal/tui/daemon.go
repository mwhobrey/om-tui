package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/localapi"
)

const (
	ownedDaemonPIDFile = "tui-owned-daemon.pid"
	daemonLogFile      = "tui-daemon.log"
	daemonReadyTimeout = 20 * time.Second
)

// Session is a live attachment to a local OpenMessage API daemon.
type Session struct {
	Client    *localapi.Client
	DataDir   string
	BaseURL   string
	Owned     bool
	ownedCmd  *exec.Cmd
	ownedPID  int
	cancelSSE context.CancelFunc
}

// EnsureDaemon attaches to a reachable daemon for dataDir, or spawns
// `openmessage serve --api --no-web` and waits until /api/status succeeds.
func EnsureDaemon(ctx context.Context, dataDir string) (*Session, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = app.DefaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	baseURL := localapi.DefaultBaseURL()
	client := localapi.NewClient(baseURL, localapi.LoadControlToken(dataDir))

	status, reachable, err := client.Status(ctx)
	if reachable && err == nil && dataDirsMatch(status.Auth.DataDir, dataDir) {
		client.Token = preferToken(status, dataDir, client.Token)
		return &Session{Client: client, DataDir: dataDir, BaseURL: baseURL, Owned: false}, nil
	}
	if reachable && err == nil && !dataDirsMatch(status.Auth.DataDir, dataDir) {
		return nil, fmt.Errorf("daemon at %s is using data dir %q, want %q", baseURL, status.Auth.DataDir, dataDir)
	}

	cmd, pid, err := spawnAPIDaemon(dataDir)
	if err != nil {
		return nil, err
	}
	session := &Session{
		Client:   localapi.NewClient(baseURL, ""),
		DataDir:  dataDir,
		BaseURL:  baseURL,
		Owned:    true,
		ownedCmd: cmd,
		ownedPID: pid,
	}
	if err := writeOwnedPID(dataDir, pid); err != nil {
		_ = session.Close()
		return nil, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, daemonReadyTimeout)
	defer cancel()
	if err := waitForDaemon(waitCtx, session); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

func waitForDaemon(ctx context.Context, session *Session) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		session.Client.Token = localapi.LoadControlToken(session.DataDir)
		status, reachable, err := session.Client.Status(ctx)
		if reachable && err == nil && dataDirsMatch(status.Auth.DataDir, session.DataDir) {
			session.Client.Token = preferToken(status, session.DataDir, session.Client.Token)
			return nil
		}
		if err != nil {
			lastErr = err
		} else if !reachable {
			lastErr = fmt.Errorf("daemon not reachable at %s", session.BaseURL)
		} else {
			lastErr = fmt.Errorf("daemon data dir mismatch")
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for local API daemon: %w", lastErr)
			}
			return fmt.Errorf("timed out waiting for local API daemon: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func spawnAPIDaemon(dataDir string) (*exec.Cmd, int, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, 0, fmt.Errorf("resolve executable: %w", err)
	}
	logPath := filepath.Join(dataDir, daemonLogFile)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("open daemon log: %w", err)
	}

	cmd := exec.Command(exe, "serve", "--api", "--no-web")
	cmd.Env = append(os.Environ(), "OPENMESSAGES_DATA_DIR="+dataDir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, 0, fmt.Errorf("start serve --api: %w", err)
	}
	// Detach log file from our process; child keeps the FD.
	_ = logFile.Close()
	return cmd, cmd.Process.Pid, nil
}

func writeOwnedPID(dataDir string, pid int) error {
	path := filepath.Join(dataDir, ownedDaemonPIDFile)
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func clearOwnedPID(dataDir string) {
	_ = os.Remove(filepath.Join(dataDir, ownedDaemonPIDFile))
}

// Close stops an owned daemon child. Pre-existing daemons are left running.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	if s.cancelSSE != nil {
		s.cancelSSE()
		s.cancelSSE = nil
	}
	if !s.Owned {
		return nil
	}
	var err error
	if s.ownedCmd != nil && s.ownedCmd.Process != nil {
		err = s.ownedCmd.Process.Kill()
		_, _ = s.ownedCmd.Process.Wait()
	} else if s.ownedPID > 0 {
		proc, findErr := os.FindProcess(s.ownedPID)
		if findErr == nil {
			err = proc.Kill()
		}
	}
	clearOwnedPID(s.DataDir)
	s.Owned = false
	return err
}

func dataDirsMatch(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

func preferToken(status localapi.DaemonStatus, dataDir, fallback string) string {
	if token := localapi.LoadControlToken(dataDir); token != "" {
		return token
	}
	return fallback
}
