package googlecookies

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type cdpCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

func snapshotUserDataForCDP(profile string) (userDataDir, profileDir string, cleanup func(), err error) {
	if profile == "" {
		return "", "", nil, fmt.Errorf("no Chrome profile directory")
	}
	srcUserData := filepath.Dir(profile)
	profileDir = filepath.Base(profile)
	if _, err := os.Stat(filepath.Join(srcUserData, "Local State")); err != nil {
		return "", "", nil, err
	}

	tmpDir, err := os.MkdirTemp("", "om-chrome-cdp-")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	dstUserData := filepath.Join(tmpDir, "User Data")
	dstProfile := filepath.Join(dstUserData, profileDir, "Network")
	if err := os.MkdirAll(dstProfile, 0o700); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := copyFile(filepath.Join(srcUserData, "Local State"), filepath.Join(dstUserData, "Local State")); err != nil {
		cleanup()
		return "", "", nil, err
	}
	_ = os.WriteFile(filepath.Join(dstUserData, "First Run"), nil, 0o600)

	srcNetwork := filepath.Join(profile, "Network")
	for _, name := range []string{"Cookies", "Cookies-wal", "Cookies-shm", "Cookies-journal", "Preferences"} {
		src := filepath.Join(srcNetwork, name)
		if name == "Preferences" {
			src = filepath.Join(profile, name)
		}
		if _, statErr := os.Stat(src); statErr != nil {
			continue
		}
		dst := filepath.Join(dstProfile, name)
		if name == "Preferences" {
			dst = filepath.Join(dstUserData, profileDir, name)
		}
		if err := copyFile(src, dst); err != nil {
			cleanup()
			return "", "", nil, err
		}
	}
	return dstUserData, profileDir, cleanup, nil
}

const (
	cdpPullTimeout    = 12 * time.Second
	cdpDevToolsWait   = 8 * time.Second
	cdpCookiePollWait = 2 * time.Second
)

func pullCookiesFromUserDataDir(ctx context.Context, userDataDir, profileDir string, headless bool) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, cdpPullTimeout)
	defer cancel()

	chrome, err := lookPathChrome()
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(userDataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		return nil, err
	}
	if err := guardChromeUserDataDir(absDir); err != nil {
		return nil, err
	}
	if isEphemeralChromeDir(absDir) {
		markChromeExitClean(absDir, profileDir)
	}
	_ = os.Remove(filepath.Join(absDir, "DevToolsActivePort"))

	port, err := freeLocalPort()
	if err != nil {
		return nil, err
	}

	args := chromeCookieReadArgs(absDir, profileDir, port, headless)

	cmd := exec.Command(chrome, args...)
	configureChromeProc(cmd)
	nullIn := detachChildStdin(cmd)
	var stderr strings.Builder
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		if nullIn != nil {
			_ = nullIn.Close()
		}
		return nil, fmt.Errorf("start Chrome: %w", err)
	}
	if nullIn != nil {
		_ = nullIn.Close()
	}
	defer stopChrome(cmd, port, absDir, profileDir)

	startCtx, startCancel := context.WithTimeout(ctx, cdpDevToolsWait)
	err = waitDevToolsHTTP(startCtx, port)
	startCancel()
	if err != nil {
		return nil, fmt.Errorf("%w%s", err, chromeStderrSuffix(stderr.String()))
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	return waitForGoogleCookies(ctx, endpoint, profileDir, true)
}

func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func chromeCookieReadArgs(userDataDir, profileDir string, port int, headless bool) []string {
	args := []string{
		"--user-data-dir=" + userDataDir,
		"--profile-directory=" + profileDir,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-allow-origins=*",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-session-crashed-bubble",
		"--hide-crash-restore-bubble",
		"--disable-extensions",
	}
	if headless {
		args = append(args, "--headless=new", "--disable-gpu")
	}
	return args
}

func waitDevToolsHTTP(ctx context.Context, port int) error {
	for {
		if devToolsPortReady(ctx, port) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Chrome DevTools did not start: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func chromeStderrSuffix(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 240 {
		msg = msg[:240] + "…"
	}
	return " (" + msg + ")"
}

func isEphemeralChromeDir(dir string) bool {
	return strings.Contains(dir, "om-chrome-cdp-") ||
		strings.Contains(dir, "google-pair-browser") ||
		strings.Contains(dir, "om-google-pair-browser")
}

var errLiveChromeUserData = errors.New("refusing to launch Chrome against the live profile directory")

func liveChromeUserDataDir() string {
	if p := DefaultChromeProfile(); p != "" {
		return filepath.Dir(p)
	}
	return ""
}

func normalizeUserDataDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if resolved, err := resolveFinalPath(abs); err == nil && resolved != "" {
		abs = resolved
	} else if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func isSameUserDataDir(a, b string) bool {
	na, err := normalizeUserDataDir(a)
	if err != nil {
		return false
	}
	nb, err := normalizeUserDataDir(b)
	if err != nil {
		return false
	}
	return strings.EqualFold(na, nb)
}

func guardChromeUserDataDir(dir string) error {
	live := liveChromeUserDataDir()
	if live == "" {
		return nil
	}
	if isSameUserDataDir(dir, live) {
		return errLiveChromeUserData
	}
	return nil
}

func stopChrome(cmd *exec.Cmd, port int, userDataDir, profileDir string) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	_ = browserClose(ctx, fmt.Sprintf("http://127.0.0.1:%d", port))
	cancel()
	if isEphemeralChromeDir(userDataDir) {
		markChromeExitClean(userDataDir, profileDir)
	}
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		killProcessTree(cmd.Process.Pid)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

func browserClose(ctx context.Context, baseURL string) error {
	wsURL, err := fetchWebSocketDebuggerURL(ctx, baseURL)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://127.0.0.1"}},
	})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	return (&cdpClient{conn: conn}).call(ctx, "Browser.close", nil, nil)
}

func waitDevToolsPort(ctx context.Context, userDataDir string) (int, error) {
	path := filepath.Join(userDataDir, "DevToolsActivePort")
	for {
		if raw, err := os.ReadFile(path); err == nil {
			line, _, _ := strings.Cut(string(raw), "\n")
			port, err := strconv.Atoi(strings.TrimSpace(line))
			if err == nil && port > 0 && devToolsPortReady(ctx, port) {
				return port, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("Chrome DevTools did not start: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func devToolsPortReady(ctx context.Context, port int) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return false
	}
	req.Header.Set("Origin", "http://127.0.0.1")
	resp, err := (&http.Client{Timeout: 400 * time.Millisecond}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

func waitForGoogleCookies(ctx context.Context, baseURL, profile string, poll bool) (map[string]string, error) {
	cookies, err := cookiesFromDevTools(ctx, baseURL, profile)
	if err == nil || !poll {
		return cookies, err
	}

	pollCtx, cancel := context.WithTimeout(ctx, cdpCookiePollWait)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastErr := err
	for {
		select {
		case <-pollCtx.Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, pollCtx.Err()
		case <-ticker.C:
		}
		cookies, err := cookiesFromDevTools(pollCtx, baseURL, profile)
		if err == nil {
			return cookies, nil
		}
		lastErr = err
	}
}

func cookiesFromDevTools(ctx context.Context, baseURL, profile string) (map[string]string, error) {
	wsURL, err := fetchWebSocketDebuggerURL(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://127.0.0.1"}},
	})
	if err != nil {
		return nil, fmt.Errorf("connect Chrome DevTools: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	client := &cdpClient{conn: conn}
	cookies, err := readCDPCookies(ctx, client, profile)
	if err == nil {
		return cookies, nil
	}
	if netCookies, netErr := readCDPNetworkCookies(ctx, client, profile); netErr == nil {
		return netCookies, nil
	}
	return nil, err
}

func readCDPCookies(ctx context.Context, client *cdpClient, profile string) (map[string]string, error) {
	var result struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if err := client.call(ctx, "Storage.getCookies", nil, &result); err != nil {
		return nil, err
	}
	return selectGoogleCookies(result.Cookies, profile)
}

func readCDPNetworkCookies(ctx context.Context, client *cdpClient, profile string) (map[string]string, error) {
	var result struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if err := client.call(ctx, "Network.getAllCookies", nil, &result); err != nil {
		return nil, err
	}
	return selectGoogleCookies(result.Cookies, profile)
}

func markChromeExitClean(userDataDir, profileDir string) {
	prefPath := filepath.Join(userDataDir, profileDir, "Preferences")
	raw, err := os.ReadFile(prefPath)
	if err != nil {
		return
	}
	var prefs map[string]any
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return
	}
	profile, _ := prefs["profile"].(map[string]any)
	if profile == nil {
		profile = map[string]any{}
		prefs["profile"] = profile
	}
	profile["exit_type"] = "Normal"
	profile["exited_cleanly"] = true
	updated, err := json.Marshal(prefs)
	if err != nil {
		return
	}
	_ = os.WriteFile(prefPath, updated, 0o600)

	sessions := filepath.Join(userDataDir, profileDir, "Sessions")
	_ = os.RemoveAll(sessions)
	for _, name := range []string{"Current Session", "Current Tabs", "Last Session", "Last Tabs"} {
		_ = os.Remove(filepath.Join(userDataDir, profileDir, name))
	}
}

func fetchWebSocketDebuggerURL(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/json/version", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Origin", "http://127.0.0.1")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("chrome DevTools version: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var payload struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse chrome DevTools version: %w", err)
	}
	if payload.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("chrome DevTools version missing websocket URL")
	}
	return payload.WebSocketDebuggerURL, nil
}

type cdpClient struct {
	conn *websocket.Conn
	next int
}

func (c *cdpClient) call(ctx context.Context, method string, params any, out any) error {
	return c.callSession(ctx, "", method, params, out)
}

func (c *cdpClient) callSession(ctx context.Context, sessionID, method string, params any, out any) error {
	c.next++
	id := c.next
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	if err := wsjson.Write(ctx, c.conn, msg); err != nil {
		return err
	}
	for {
		var frame struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result json.RawMessage `json:"result"`
			Method string          `json:"method"`
		}
		if err := wsjson.Read(ctx, c.conn, &frame); err != nil {
			return err
		}
		if frame.ID != id {
			continue
		}
		if len(frame.Error) > 0 && string(frame.Error) != "null" {
			return fmt.Errorf("chrome DevTools %s: %s", method, string(frame.Error))
		}
		if out == nil || len(frame.Result) == 0 || string(frame.Result) == "null" {
			return nil
		}
		return json.Unmarshal(frame.Result, out)
	}
}

func selectGoogleCookies(cookies []cdpCookie, profile string) (map[string]string, error) {
	type scored struct {
		host  string
		value string
	}
	best := map[string]scored{}
	for _, cookie := range cookies {
		host := normalizeCookieHost(cookie.Domain)
		if !strings.Contains(host, "google.com") {
			continue
		}
		name := strings.TrimSpace(cookie.Name)
		if name == "" || cookie.Value == "" {
			continue
		}
		prev, exists := best[name]
		if !exists || priorityOf(host) < priorityOf(prev.host) {
			best[name] = scored{host: host, value: cookie.Value}
		}
	}

	var missing []string
	for _, req := range requiredCookies {
		if hv, ok := best[req.name]; !ok || hv.host != req.host {
			missing = append(missing, req.host+":"+req.name)
		}
	}
	if len(missing) > 0 {
		var found []string
		for name := range best {
			found = append(found, name)
		}
		sort.Strings(found)
		err := missingRequiredCookiesError(profile, missing)
		if len(found) > 0 {
			return nil, fmt.Errorf("%w (found %s)", err, strings.Join(found, ", "))
		}
		return nil, fmt.Errorf("%w (cdp cookies=%d)", err, len(cookies))
	}

	out := make(map[string]string, len(best))
	for name, hv := range best {
		out[name] = hv.value
	}
	return out, nil
}

func normalizeCookieHost(domain string) string {
	host := strings.TrimSpace(strings.ToLower(domain))
	if host == "google.com" {
		return ".google.com"
	}
	if strings.HasPrefix(host, ".") {
		return host
	}
	if host == "messages.google.com" || host == "accounts.google.com" {
		return host
	}
	if strings.HasSuffix(host, ".google.com") {
		return host
	}
	return host
}

func pairBrowserLooksReady(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "Local State"))
	return err == nil
}
