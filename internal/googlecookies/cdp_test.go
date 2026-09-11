package googlecookies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	_ "modernc.org/sqlite"
)

func TestCookiesFromDevTools(t *testing.T) {
	cookies := []cdpCookie{
		{Name: "SID", Value: "sid", Domain: ".google.com"},
		{Name: "HSID", Value: "hsid", Domain: ".google.com"},
		{Name: "SSID", Value: "ssid", Domain: ".google.com"},
		{Name: "APISID", Value: "apisid", Domain: ".google.com"},
		{Name: "SAPISID", Value: "sapisid", Domain: ".google.com"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools",
			})
		case "/devtools":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				OriginPatterns: []string{"*"},
			})
			if err != nil {
				t.Errorf("accept: %v", err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			ctx := r.Context()
			for {
				var msg struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := wsjson.Read(ctx, conn, &msg); err != nil {
					return
				}
				switch msg.Method {
				case "Storage.getCookies":
					_ = wsjson.Write(ctx, conn, map[string]any{
						"id":     msg.ID,
						"result": map[string]any{"cookies": cookies},
					})
				default:
					_ = wsjson.Write(ctx, conn, map[string]any{
						"id":    msg.ID,
						"error": map[string]any{"code": -32601, "message": msg.Method},
					})
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := cookiesFromDevTools(context.Background(), srv.URL, "Default")
	if err != nil {
		t.Fatalf("cookiesFromDevTools(): %v", err)
	}
	if got["SID"] != "sid" || got["SAPISID"] != "sapisid" {
		t.Fatalf("cookies = %#v", got)
	}
}

func TestCookiesFromDevToolsDoesNotCreateTargets(t *testing.T) {
	var created int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools",
			})
		case "/devtools":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				OriginPatterns: []string{"*"},
			})
			if err != nil {
				t.Errorf("accept: %v", err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			ctx := r.Context()
			for {
				var msg struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := wsjson.Read(ctx, conn, &msg); err != nil {
					return
				}
				if msg.Method == "Target.createTarget" {
					created++
				}
				switch msg.Method {
				case "Storage.getCookies":
					_ = wsjson.Write(ctx, conn, map[string]any{
						"id":     msg.ID,
						"result": map[string]any{"cookies": []cdpCookie{}},
					})
				default:
					_ = wsjson.Write(ctx, conn, map[string]any{
						"id":    msg.ID,
						"error": map[string]any{"code": -32601, "message": msg.Method},
					})
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := cookiesFromDevTools(context.Background(), srv.URL, "Default")
	if err == nil {
		t.Fatal("expected missing cookies")
	}
	if created != 0 {
		t.Fatalf("Target.createTarget called %d times, want 0", created)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _ = waitForGoogleCookies(ctx, srv.URL, "Default", true)
	if created != 0 {
		t.Fatalf("poll opened %d tabs via Target.createTarget, want 0", created)
	}
}

func TestBrowserCloseSendsMethod(t *testing.T) {
	closed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools",
			})
		case "/devtools":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				OriginPatterns: []string{"*"},
			})
			if err != nil {
				t.Errorf("accept: %v", err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			ctx := r.Context()
			for {
				var msg struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := wsjson.Read(ctx, conn, &msg); err != nil {
					return
				}
				if msg.Method == "Browser.close" {
					closed++
				}
				_ = wsjson.Write(ctx, conn, map[string]any{"id": msg.ID, "result": map[string]any{}})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	if err := browserClose(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("Browser.close called %d times, want 1", closed)
	}
}

func TestChromeCookieReadArgsOmitAccountStateFlags(t *testing.T) {
	args := chromeCookieReadArgs(`C:\temp\om-chrome-cdp-1\User Data`, "Default", 9222, true)
	joined := strings.Join(args, " ")
	for _, banned := range []string{"--disable-sync", "--disable-background-networking"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("cookie-read Chrome must not pass %s: %v", banned, args)
		}
	}
}

func TestGuardChromeUserDataDirAllowsTempCopy(t *testing.T) {
	if err := guardChromeUserDataDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestGuardChromeUserDataDirRejectsLiveDir(t *testing.T) {
	live := liveChromeUserDataDir()
	if live == "" {
		t.Skip("no Chrome user data dir")
	}
	if err := guardChromeUserDataDir(live); !errors.Is(err, errLiveChromeUserData) {
		t.Fatalf("got %v, want errLiveChromeUserData", err)
	}
}

func TestMarkChromeExitCleanClearsSessionRestore(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	prefs := []byte(`{"profile":{"exit_type":"Crashed","exited_cleanly":false}}`)
	if err := os.WriteFile(filepath.Join(profile, "Preferences"), prefs, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Current Session"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Sessions", "tabs"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	markChromeExitClean(dir, "Default")

	raw, err := os.ReadFile(filepath.Join(profile, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"exit_type":"Normal"`) {
		t.Fatalf("preferences = %s", raw)
	}
	if _, err := os.Stat(filepath.Join(profile, "Current Session")); !os.IsNotExist(err) {
		t.Fatalf("Current Session still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "Sessions")); !os.IsNotExist(err) {
		t.Fatalf("Sessions still present: %v", err)
	}
}

func TestWaitDevToolsPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1/devtools"}`))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	want, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte(fmt.Sprintf("%d\n/devtools/browser/abc\n", want)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	port, err := waitDevToolsPort(ctx, dir)
	if err != nil {
		t.Fatalf("waitDevToolsPort(): %v", err)
	}
	if port != want {
		t.Fatalf("port = %d, want %d", port, want)
	}
}

func TestWaitDevToolsHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1/devtools"}`))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitDevToolsHTTP(ctx, port); err != nil {
		t.Fatal(err)
	}
}

func TestWaitDevToolsPortIgnoresDeadPort(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if _, err := waitDevToolsPort(ctx, dir); err == nil {
		t.Fatal("expected timeout on a stale DevTools port")
	}
}

func TestFetchWebSocketDebuggerURLTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	start := time.Now()
	_, err = fetchWebSocketDebuggerURL(context.Background(), "http://"+ln.Addr().String())
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed < 2*time.Second || elapsed > 8*time.Second {
		t.Fatalf("elapsed %v, want ~3s HTTP timeout, not a hang", elapsed)
	}
}

func TestSnapshotUserDataCopiesCookieJournal(t *testing.T) {
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	network := filepath.Join(profile, "Network")
	if err := os.MkdirAll(network, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(network, "Cookies"), []byte("cookies"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(network, "Cookies-journal"), []byte("j"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst, dir, cleanup, err := snapshotUserDataForCDP(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if dir != "Default" {
		t.Fatalf("profileDir=%s", dir)
	}
	if _, err := os.Stat(filepath.Join(dst, "Default", "Network", "Cookies-journal")); err != nil {
		t.Fatal(err)
	}
}

func TestLookPathChromePrefersChromeOverEdge(t *testing.T) {
	path, err := lookPathChrome()
	if err != nil {
		t.Skip(err)
	}
	if !isEdgeBinary(path) {
		return
	}
	for _, candidate := range chromeWellKnownPaths() {
		if isEdgeBinary(candidate) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			t.Fatalf("lookPathChrome()=%s but Chrome exists at %s", path, candidate)
		}
	}
}

func TestListLiveRequiredCookieRows(t *testing.T) {
	if os.Getenv("OM_TEST_LIVE_CHROME") == "" {
		t.Skip("set OM_TEST_LIVE_CHROME=1")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	userData := filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "User Data")
	if os.Getenv("LOCALAPPDATA") == "" {
		userData = filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data")
	}
	entries, err := os.ReadDir(userData)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("DefaultChromeProfile=%s", DefaultChromeProfile())
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		profile := filepath.Join(userData, ent.Name())
		if !cookieDBExists(profile) {
			continue
		}
		dbCopy, cleanup, err := snapshotCookieDB(profile)
		if err != nil {
			t.Logf("%s: snapshot %v", ent.Name(), err)
			continue
		}
		db, err := sql.Open("sqlite", dbCopy)
		if err != nil {
			cleanup()
			t.Fatal(err)
		}
		var nAll, nAuth int
		_ = db.QueryRow(`select count(*) from cookies`).Scan(&nAll)
		_ = db.QueryRow(`select count(*) from cookies where name in ('SID','HSID','SSID','APISID','SAPISID')`).Scan(&nAuth)
		t.Logf("profile %s total=%d auth=%d", ent.Name(), nAll, nAuth)
		names, qerr := db.Query(`select distinct name from cookies where instr(host_key, 'google.com') > 0 order by name`)
		if qerr == nil {
			var listed []string
			for names.Next() {
				var name string
				if names.Scan(&name) == nil && name != "" {
					listed = append(listed, name)
				}
			}
			_ = names.Close()
			t.Logf("profile %s google cookie names=%v", ent.Name(), listed)
		}
		var prefixes []string
		pre, perr := db.Query(`select distinct substr(encrypted_value, 1, 3) from cookies where name in ('SID','HSID','SSID','APISID','SAPISID')`)
		if perr == nil {
			for pre.Next() {
				var p string
				if pre.Scan(&p) == nil && p != "" {
					prefixes = append(prefixes, p)
				}
			}
			_ = pre.Close()
			t.Logf("profile %s auth prefixes=%v", ent.Name(), prefixes)
		}
		_ = db.Close()
		cleanup()
	}
}

func TestLiveCopiedProfileCDPCookieNames(t *testing.T) {
	if os.Getenv("OM_TEST_LIVE_CHROME") == "" {
		t.Skip("set OM_TEST_LIVE_CHROME=1")
	}
	profile := DefaultChromeProfile()
	userData, profileDir, cleanup, err := snapshotUserDataForCDP(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	t.Logf("profile=%s copy=%s", profile, userData)

	chrome, err := lookPathChrome()
	if err != nil {
		t.Fatal(err)
	}
	port, err := freeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	args := chromeCookieReadArgs(userData, profileDir, port, true)
	cmd := exec.Command(chrome, args...)
	configureChromeProc(cmd)
	nullIn := detachChildStdin(cmd)
	var stderr strings.Builder
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			killProcessTree(cmd.Process.Pid)
			_, _ = cmd.Process.Wait()
		}
		if nullIn != nil {
			_ = nullIn.Close()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := waitDevToolsHTTP(ctx, port); err != nil {
		t.Fatalf("DevTools: %v stderr=%q", err, strings.TrimSpace(stderr.String()))
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	wsURL, err := fetchWebSocketDebuggerURL(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"http://127.0.0.1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	client := &cdpClient{conn: conn}
	var result struct {
		Cookies []cdpCookie `json:"cookies"`
	}
	if err := client.call(ctx, "Storage.getCookies", nil, &result); err != nil {
		t.Fatalf("Storage.getCookies: %v", err)
	}
	var names []string
	for _, c := range result.Cookies {
		if strings.Contains(strings.ToLower(c.Domain), "google") {
			names = append(names, c.Name+"@"+c.Domain)
		}
	}
	sort.Strings(names)
	t.Logf("google cdp names=%v total=%d stderr=%q", names, len(result.Cookies), strings.TrimSpace(stderr.String()))
}

func TestLiveSnapshotCDPGetsAccountCookieNames(t *testing.T) {
	if os.Getenv("OM_TEST_LIVE_CHROME") == "" {
		t.Skip("set OM_TEST_LIVE_CHROME=1 to run against the local Chrome profile")
	}
	profile := DefaultChromeProfile()
	if profile == "" {
		t.Fatal("no Chrome profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := cdpCookiesFromProfile(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"SID", "HSID", "SSID", "APISID", "SAPISID"} {
		if strings.TrimSpace(got[name]) == "" {
			t.Errorf("missing %s", name)
		}
	}
}
