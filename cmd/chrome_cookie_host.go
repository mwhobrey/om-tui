package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/maxghenis/openmessage/internal/app"
	"github.com/maxghenis/openmessage/internal/cookiebridge"
	"github.com/maxghenis/openmessage/internal/localapi"
)

// RunChromeCookieHost is the Chrome native-messaging host that relays
// getGoogleCookies requests between the MV3 extension and the local daemon.
//
//	om-tui chrome-cookie-host           # stdio host (spawned by Chrome)
//	om-tui chrome-cookie-host --install # write Windows manifest + registry
func RunChromeCookieHost(logger zerolog.Logger, args ...string) error {
	opts, err := parseChromeCookieHostArgs(args)
	if err != nil {
		return err
	}
	if opts.help {
		fmt.Fprintln(os.Stderr, "Usage: om-tui chrome-cookie-host [--install] [--extension-id ID]")
		return nil
	}
	if opts.install {
		return installChromeCookieHost(opts.extensionID)
	}
	return runChromeCookieHost(logger)
}

type chromeCookieHostOpts struct {
	install     bool
	help        bool
	extensionID string
}

func parseChromeCookieHostArgs(args []string) (chromeCookieHostOpts, error) {
	opts := chromeCookieHostOpts{extensionID: cookiebridge.ExtensionID}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--install":
			opts.install = true
		case arg == "--extension-id":
			if i+1 >= len(args) {
				return opts, errors.New("--extension-id requires a value")
			}
			i++
			opts.extensionID = strings.TrimSpace(args[i])
			if opts.extensionID == "" {
				return opts, errors.New("--extension-id requires a value")
			}
		case arg == "--help" || arg == "-h":
			opts.help = true
		case strings.HasPrefix(arg, "chrome-extension://"):
			// Chrome passes the calling extension origin.
			continue
		case arg == "--parent-window" || strings.HasPrefix(arg, "--parent-window="):
			// Windows: Chrome appends --parent-window=<HWND> (0 from a service worker).
			if arg == "--parent-window" && i+1 < len(args) {
				i++
			}
			continue
		default:
			return opts, fmt.Errorf("unknown chrome-cookie-host option %s", arg)
		}
	}
	return opts, nil
}

func installChromeCookieHost(extensionID string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve om-tui executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	dataDir := app.DefaultDataDir()
	dir := filepath.Join(dataDir, "chrome-native")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create chrome-native dir: %w", err)
	}

	// Windows: point the manifest at a .cmd wrapper so Chrome always invokes
	// `om-tui chrome-cookie-host …` (origin + --parent-window are then args to
	// the subcommand). Direct exe launch also works after the arg parser
	// ignores Chrome's switches, but the wrapper is the durable install shape.
	hostPath := exe
	if runtime.GOOS == "windows" {
		cmdPath := filepath.Join(dir, cookiebridge.HostName+".cmd")
		script := "@echo off\r\n" +
			"rem om-tui Chrome native messaging host wrapper\r\n" +
			"\"" + exe + "\" chrome-cookie-host %*\r\n"
		if err := os.WriteFile(cmdPath, []byte(script), 0o700); err != nil {
			return fmt.Errorf("write native host wrapper: %w", err)
		}
		hostPath = cmdPath
	}

	manifestPath := filepath.Join(dir, cookiebridge.HostName+".json")
	origin := "chrome-extension://" + extensionID + "/"
	manifest := map[string]any{
		"name":            cookiebridge.HostName,
		"description":     "om-tui Google cookie bridge",
		"path":            hostPath,
		"type":            "stdio",
		"allowed_origins": []string{origin},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write native host manifest: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		if err := registerChromeNativeHostWindows(cookiebridge.HostName, manifestPath); err != nil {
			return err
		}
	default:
		return fmt.Errorf("chrome-cookie-host --install is Windows-only in v1 (manifest written to %s); register it manually for Chrome on %s", manifestPath, runtime.GOOS)
	}
	fmt.Printf("Installed native host %s\n", cookiebridge.HostName)
	fmt.Printf("  manifest: %s\n", manifestPath)
	fmt.Printf("  host: %s\n", hostPath)
	fmt.Printf("  executable: %s\n", exe)
	fmt.Printf("  allowed origin: %s\n", origin)
	fmt.Println("Load the unpacked extension from extensions/google-cookies/ in chrome://extensions")
	return nil
}

type chromeHostMsg struct {
	Op      string            `json:"op"`
	ID      string            `json:"id,omitempty"`
	Cookies map[string]string `json:"cookies,omitempty"`
	Error   string            `json:"error,omitempty"`
}

func runChromeCookieHost(logger zerolog.Logger) error {
	dataDir := app.DefaultDataDir()
	token := localapi.LoadControlToken(dataDir)
	client := localapi.NewClient(localapi.DefaultBaseURL(), token)
	client.HTTP.Timeout = 0 // long-poll waits are owned per-request

	stdoutMu := sync.Mutex{}
	writeExt := func(msg chromeHostMsg) error {
		stdoutMu.Lock()
		defer stdoutMu.Unlock()
		return cookiebridge.WriteNativeMessage(os.Stdout, msg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	extMessages := make(chan chromeHostMsg, 8)
	readErr := make(chan error, 1)
	go func() {
		for {
			var msg chromeHostMsg
			if err := cookiebridge.ReadNativeMessage(os.Stdin, &msg); err != nil {
				readErr <- err
				return
			}
			select {
			case extMessages <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	pending := make(map[string]chan chromeHostMsg)
	var pendingMu sync.Mutex

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-extMessages:
				switch msg.Op {
				case "hello":
					logger.Debug().Msg("Chrome cookie extension connected")
				case "getGoogleCookiesResult":
					pendingMu.Lock()
					ch := pending[msg.ID]
					delete(pending, msg.ID)
					pendingMu.Unlock()
					if ch != nil {
						ch <- msg
					}
				case "forceCookies":
					reply := chromeHostMsg{Op: "forceCookiesResult", ID: msg.ID}
					if err := pushCookiesToDaemon(ctx, client, msg.Cookies); err != nil {
						logger.Warn().Err(err).Msg("Force cookie push failed")
						reply.Error = err.Error()
					} else {
						logger.Info().Msg("Force-pushed Google cookies to daemon")
					}
					if err := writeExt(reply); err != nil {
						logger.Warn().Err(err).Msg("Failed to ack force cookie push to extension")
					}
				default:
					logger.Debug().Str("op", msg.Op).Msg("Ignoring extension message")
				}
			}
		}
	}()

	for {
		select {
		case err := <-readErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		default:
		}

		req, err := waitDaemonCookieRequest(ctx, client)
		if err != nil {
			if errors.Is(err, errDaemonWaitIdle) || errors.Is(err, context.Canceled) {
				continue
			}
			// Daemon may be down briefly; back off without killing the Chrome port.
			logger.Debug().Err(err).Msg("Cookie bridge wait failed")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}

		replyCh := make(chan chromeHostMsg, 1)
		pendingMu.Lock()
		pending[req.ID] = replyCh
		pendingMu.Unlock()

		if err := writeExt(chromeHostMsg{Op: "getGoogleCookies", ID: req.ID}); err != nil {
			pendingMu.Lock()
			delete(pending, req.ID)
			pendingMu.Unlock()
			_ = replyDaemonCookieRequest(ctx, client, req.ID, nil, err.Error())
			return err
		}

		var reply chromeHostMsg
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			_ = replyDaemonCookieRequest(ctx, client, req.ID, nil, "extension disconnected")
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case <-time.After(cookiebridge.DefaultRequestTimeout):
			pendingMu.Lock()
			delete(pending, req.ID)
			pendingMu.Unlock()
			_ = replyDaemonCookieRequest(ctx, client, req.ID, nil, "extension timed out")
			continue
		case reply = <-replyCh:
		}

		if reply.Error != "" {
			_ = replyDaemonCookieRequest(ctx, client, req.ID, nil, reply.Error)
			continue
		}
		_ = replyDaemonCookieRequest(ctx, client, req.ID, reply.Cookies, "")
	}
}

var errDaemonWaitIdle = errors.New("daemon wait idle")

type daemonWaitResponse struct {
	Op    string `json:"op"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

func waitDaemonCookieRequest(ctx context.Context, client *localapi.Client) (cookiebridge.Request, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, client.BaseURL+"/api/google/cookie-bridge/wait?timeout_ms=25000", nil)
	if err != nil {
		return cookiebridge.Request{}, err
	}
	if client.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+client.Token)
	}
	resp, err := client.HTTP.Do(httpReq)
	if err != nil {
		return cookiebridge.Request{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusNoContent {
		return cookiebridge.Request{}, errDaemonWaitIdle
	}
	if resp.StatusCode != http.StatusOK {
		return cookiebridge.Request{}, fmt.Errorf("cookie-bridge wait: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed daemonWaitResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return cookiebridge.Request{}, err
	}
	if parsed.Op == "idle" || parsed.ID == "" {
		return cookiebridge.Request{}, errDaemonWaitIdle
	}
	return cookiebridge.Request{ID: parsed.ID, Op: parsed.Op}, nil
}

func replyDaemonCookieRequest(ctx context.Context, client *localapi.Client, id string, cookies map[string]string, errText string) error {
	payload := map[string]any{"id": id}
	if errText != "" {
		payload["error"] = errText
	} else {
		payload["cookies"] = cookies
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, client.BaseURL+"/api/google/cookie-bridge/reply", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if client.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+client.Token)
	}
	resp, err := client.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("cookie-bridge reply: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func pushCookiesToDaemon(ctx context.Context, client *localapi.Client, cookies map[string]string) error {
	if err := cookiebridge.ValidateCookies(cookies); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]any{"cookies": cookies})
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, client.BaseURL+"/api/google/cookie-bridge/push", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if client.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+client.Token)
	}
	resp, err := client.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("cookie-bridge push: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
