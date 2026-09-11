package googlecookies

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type dbCookie struct {
	host      string
	name      string
	encrypted []byte
}

// writeCookieDB builds a minimal Chrome-shaped cookies SQLite file.
func writeCookieDB(t *testing.T, path string, cookies []dbCookie) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open cookie db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`create table cookies (host_key text, name text, encrypted_value blob, value text)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, c := range cookies {
		if _, err := db.Exec(
			`insert into cookies (host_key, name, encrypted_value, value) values (?, ?, ?, '')`,
			c.host, c.name, c.encrypted,
		); err != nil {
			t.Fatalf("insert cookie: %v", err)
		}
	}
}

// encryptCookie mirrors Chrome's v10 scheme so DecryptCookie can be tested
// without a live Chrome profile: AES-128-CBC, IV of 16 spaces, PKCS7 padding,
// optional SHA256(host) plaintext prefix (Chrome 130+).
func encryptCookie(t *testing.T, value, host string, key []byte, withHostHash bool) []byte {
	t.Helper()
	plain := []byte(value)
	if withHostHash {
		h := sha256.Sum256([]byte(host))
		plain = append(h[:], plain...)
	}
	padLen := aes.BlockSize - len(plain)%aes.BlockSize
	for i := 0; i < padLen; i++ {
		plain = append(plain, byte(padLen))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte("                ")).CryptBlocks(out, plain)
	return append([]byte("v10"), out...)
}

func TestDeriveKeyChromePBKDF2Vectors(t *testing.T) {
	// Fixed Chrome vectors guard key derivation before Wave 1 changes credential storage.
	tests := []struct {
		iterations int
		want       string
	}{
		{iterations: 1, want: "d7d4df19d842591632e8dfb427ab3474"},
		{iterations: 1003, want: "01ab06dc67d036480129f3e40d53ca5f"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := hex.EncodeToString(DeriveKey([]byte("test-secret"), tt.iterations))
			if got != tt.want {
				t.Fatalf("DeriveKey(iterations=%d) = %s, want %s", tt.iterations, got, tt.want)
			}
		})
	}
}

func TestDecryptCookieRoundTrip(t *testing.T) {
	key := DeriveKey([]byte("test-secret"), 1003)
	for _, withHash := range []bool{false, true} {
		enc := encryptCookie(t, "cookie-value-123", ".google.com", key, withHash)
		got, err := DecryptCookie(enc, ".google.com", key)
		if err != nil {
			t.Fatalf("DecryptCookie(withHash=%v): %v", withHash, err)
		}
		if got != "cookie-value-123" {
			t.Fatalf("DecryptCookie(withHash=%v) = %q, want %q", withHash, got, "cookie-value-123")
		}
	}
}

func TestDecryptCookieV11(t *testing.T) {
	// Cookie refresh must keep accepting Chrome's v11 envelope through Wave 1.
	key := DeriveKey([]byte("test-secret"), 1003)
	encrypted := encryptCookie(t, "cookie-value-123", ".google.com", key, true)
	copy(encrypted[:3], "v11")

	got, err := DecryptCookie(encrypted, ".google.com", key)
	if err != nil {
		t.Fatalf("DecryptCookie(v11): %v", err)
	}
	if got != "cookie-value-123" {
		t.Fatalf("DecryptCookie(v11) = %q, want %q", got, "cookie-value-123")
	}
}

func TestDecryptCookieIgnoresUnencrypted(t *testing.T) {
	key := DeriveKey([]byte("test-secret"), 1003)
	got, err := DecryptCookie([]byte("plain"), ".google.com", key)
	if err != nil {
		t.Fatalf("DecryptCookie() unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("DecryptCookie() = %q, want empty for non-v10 value", got)
	}
}

func TestUpdateSessionCookiesPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")
	original := map[string]any{
		"phone_id": "abc123",
		"auth_data": map[string]any{
			"tachyon_token": "keep-me",
			"cookies":       map[string]any{"OLD": "stale"},
		},
	}
	raw, _ := json.Marshal(original)
	if err := os.WriteFile(sessionPath, raw, 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	newCookies := map[string]string{"SID": "fresh", "OSID": "fresh2"}
	if err := UpdateSessionCookies(sessionPath, newCookies); err != nil {
		t.Fatalf("UpdateSessionCookies(): %v", err)
	}

	updatedRaw, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read updated session: %v", err)
	}
	var updated map[string]any
	if err := json.Unmarshal(updatedRaw, &updated); err != nil {
		t.Fatalf("parse updated session: %v", err)
	}
	if updated["phone_id"] != "abc123" {
		t.Fatalf("phone_id lost: %v", updated["phone_id"])
	}
	authData := updated["auth_data"].(map[string]any)
	if authData["tachyon_token"] != "keep-me" {
		t.Fatalf("tachyon_token lost: %v", authData["tachyon_token"])
	}
	cookies := authData["cookies"].(map[string]any)
	if cookies["SID"] != "fresh" || cookies["OSID"] != "fresh2" {
		t.Fatalf("cookies not replaced: %v", cookies)
	}
	if _, exists := cookies["OLD"]; exists {
		t.Fatalf("stale cookie survived replacement")
	}
}

func TestLoadChromeCookiesRequiresAllAccountCookies(t *testing.T) {
	// Exact host/name requirements guard Wave 1 from persisting partial credentials.
	secret := []byte("secret")
	key := DeriveKey(secret, 1003)

	for _, missing := range requiredCookies {
		missing := missing
		t.Run("missing "+missing.host+":"+missing.name, func(t *testing.T) {
			profile := filepath.Join(t.TempDir(), "Default")
			if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
				t.Fatalf("mkdir profile: %v", err)
			}
			rows := make([]dbCookie, 0, len(requiredCookies)-1)
			for _, req := range requiredCookies {
				if req == missing {
					continue
				}
				rows = append(rows, dbCookie{req.host, req.name, encryptCookie(t, "val-"+req.name, req.host, key, true)})
			}
			writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

			_, err := LoadChromeCookies(profile, secret)
			if err == nil || !strings.Contains(err.Error(), missing.name) || !strings.Contains(err.Error(), "Chrome profile Default") {
				t.Fatalf("LoadChromeCookies() error = %v, want missing %s in Default", err, missing.name)
			}
		})
	}

	t.Run("required name on wrong host", func(t *testing.T) {
		profile := filepath.Join(t.TempDir(), "Default")
		if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
			t.Fatalf("mkdir profile: %v", err)
		}
		rows := make([]dbCookie, 0, len(requiredCookies))
		for _, req := range requiredCookies {
			if req.name == "SID" {
				continue
			}
			rows = append(rows, dbCookie{req.host, req.name, encryptCookie(t, "val-"+req.name, req.host, key, true)})
		}
		rows = append(rows, dbCookie{
			host:      "accounts.google.com",
			name:      "SID",
			encrypted: encryptCookie(t, "wrong-host", "accounts.google.com", key, true),
		})
		writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

		_, err := LoadChromeCookies(profile, secret)
		if err == nil || !strings.Contains(err.Error(), "SID") {
			t.Fatalf("LoadChromeCookies() error = %v, want missing SID", err)
		}
	})
}

func TestLoadChromeCookiesAppBoundV20(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	rows := make([]dbCookie, 0, len(requiredCookies))
	for _, req := range requiredCookies {
		enc := append([]byte("v20"), bytes.Repeat([]byte{0xab}, 40)...)
		rows = append(rows, dbCookie{req.host, req.name, enc})
	}
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)
	_, err := LoadChromeCookies(profile, bytes.Repeat([]byte("s"), 32))
	if err == nil || !strings.Contains(err.Error(), "cannot read them") {
		t.Fatalf("LoadChromeCookies() error = %v, want app-bound paste error", err)
	}
	if strings.Contains(err.Error(), "missing SID") {
		t.Fatalf("v20 cookies reported as missing: %v", err)
	}
}

func TestLoadChromeCookiesBareGoogleComHost(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := []byte("secret")
	key := DeriveKey(secret, 1003)
	rows := make([]dbCookie, 0, len(requiredCookies))
	for _, req := range requiredCookies {
		rows = append(rows, dbCookie{"google.com", req.name, encryptCookie(t, "val-"+req.name, "google.com", key, true)})
	}
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)
	got, err := LoadChromeCookies(profile, secret)
	if err != nil {
		t.Fatalf("LoadChromeCookies(): %v", err)
	}
	if got["SID"] != "val-SID" {
		t.Fatalf("SID = %q, want val-SID from google.com host", got["SID"])
	}
}

func TestCookiesAppBoundOnBareGoogleComHost(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	rows := make([]dbCookie, 0, len(requiredCookies))
	for _, req := range requiredCookies {
		enc := append([]byte("v20"), bytes.Repeat([]byte{0xab}, 40)...)
		rows = append(rows, dbCookie{"google.com", req.name, enc})
	}
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)
	if !cookiesAppBound(profile) {
		t.Fatal("v20 SID cookies on google.com should count as app-bound")
	}
}

func TestLoadChromeCookiesDoesNotRequireMessagesOSID(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	secret := []byte("secret")
	key := DeriveKey(secret, 1003)
	rows := make([]dbCookie, 0, len(requiredCookies))
	for _, req := range requiredCookies {
		rows = append(rows, dbCookie{req.host, req.name, encryptCookie(t, "val-"+req.name, req.host, key, true)})
	}
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

	got, err := LoadChromeCookies(profile, secret)
	if err != nil {
		t.Fatalf("LoadChromeCookies(): %v", err)
	}
	if _, exists := got["OSID"]; exists {
		t.Fatalf("OSID = %q, want absent when no Chrome host provides it", got["OSID"])
	}
}

func TestLoadChromeCookiesHappyPath(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	secret := []byte("secret")
	key := DeriveKey(secret, 1003)
	rows := make([]dbCookie, 0, len(requiredCookies)+3)
	for _, req := range requiredCookies {
		rows = append(rows, dbCookie{req.host, req.name, encryptCookie(t, "val-"+req.name, req.host, key, true)})
	}
	// A duplicate SID on a lower-priority host must not shadow the .google.com one.
	rows = append(rows, dbCookie{"accounts.google.com", "SID", encryptCookie(t, "wrong", "accounts.google.com", key, true)})
	// A Messages OSID is preferred over a same-name account cookie when present.
	rows = append(rows,
		dbCookie{".google.com", "OSID", encryptCookie(t, "account-osid", ".google.com", key, true)},
		dbCookie{"messages.google.com", "OSID", encryptCookie(t, "messages-osid", "messages.google.com", key, true)},
	)
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

	got, err := LoadChromeCookies(profile, secret)
	if err != nil {
		t.Fatalf("LoadChromeCookies(): %v", err)
	}
	if got["SID"] != "val-SID" {
		t.Fatalf("SID = %q, want %q (host priority not applied)", got["SID"], "val-SID")
	}
	if got["OSID"] != "messages-osid" {
		t.Fatalf("OSID = %q, want %q", got["OSID"], "messages-osid")
	}
}

func TestLoadChromeCookiesEqualPriorityHostsUseLexicographicOrder(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	secret := []byte("secret")
	key := DeriveKey(secret, 1003)
	rows := make([]dbCookie, 0, len(requiredCookies)+2)
	for _, req := range requiredCookies {
		rows = append(rows, dbCookie{req.host, req.name, encryptCookie(t, "val-"+req.name, req.host, key, true)})
	}
	// Insert mail first so SQLite's explicit ordering, not insertion order,
	// makes docs.google.com the stable winner at equal host priority.
	rows = append(rows,
		dbCookie{"mail.google.com", "OSID", encryptCookie(t, "mail-osid", "mail.google.com", key, true)},
		dbCookie{"docs.google.com", "OSID", encryptCookie(t, "docs-osid", "docs.google.com", key, true)},
	)
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

	for i := 0; i < 5; i++ {
		got, err := LoadChromeCookies(profile, secret)
		if err != nil {
			t.Fatalf("LoadChromeCookies() run %d: %v", i, err)
		}
		if got["OSID"] != "docs-osid" {
			t.Fatalf("LoadChromeCookies() run %d OSID = %q, want %q", i, got["OSID"], "docs-osid")
		}
	}
}

func TestLoadChromeCookiesSelectsLinuxPBKDF2Iterations(t *testing.T) {
	// Wave 1 credential handling must preserve selection of Chrome's Linux key.
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	secret := []byte("secret")
	key := DeriveKey(secret, 1)
	rows := make([]dbCookie, 0, len(requiredCookies))
	for _, req := range requiredCookies {
		rows = append(rows, dbCookie{req.host, req.name, encryptCookie(t, "linux-"+req.name, req.host, key, true)})
	}
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

	got, err := LoadChromeCookies(profile, secret)
	if err != nil {
		t.Fatalf("LoadChromeCookies(): %v", err)
	}
	for _, req := range requiredCookies {
		want := "linux-" + req.name
		if got[req.name] != want {
			t.Fatalf("%s = %q, want %q", req.name, got[req.name], want)
		}
	}
}

func encryptCookieGCM(t *testing.T, value, host string, key []byte, withHostHash bool) []byte {
	t.Helper()
	plain := []byte(value)
	if withHostHash {
		h := sha256.Sum256([]byte(host))
		plain = append(h[:], plain...)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm: %v", err)
	}
	nonce := make([]byte, gcmNonceSize)
	sealed := aead.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, 3+len(nonce)+len(sealed))
	out = append(out, []byte("v20")...)
	out = append(out, nonce...)
	return append(out, sealed...)
}

func TestDecryptCookieGCMRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	for _, withHash := range []bool{false, true} {
		enc := encryptCookieGCM(t, "cookie-value-123", ".google.com", key, withHash)
		got, err := DecryptCookieGCM(enc, ".google.com", key)
		if err != nil {
			t.Fatalf("DecryptCookieGCM(withHash=%v): %v", withHash, err)
		}
		if got != "cookie-value-123" {
			t.Fatalf("DecryptCookieGCM(withHash=%v) = %q", withHash, got)
		}
	}
}

func TestLoadChromeCookiesWindowsGCM(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	rows := make([]dbCookie, 0, len(requiredCookies))
	for _, req := range requiredCookies {
		rows = append(rows, dbCookie{req.host, req.name, encryptCookieGCM(t, "win-"+req.name, req.host, key, true)})
	}
	writeCookieDB(t, filepath.Join(profile, "Network", "Cookies"), rows)

	got, err := LoadChromeCookies(profile, key)
	if err != nil {
		t.Fatalf("LoadChromeCookies(): %v", err)
	}
	if got["SID"] != "win-SID" {
		t.Fatalf("SID = %q, want win-SID", got["SID"])
	}
}

func fakeChromeProfile(t *testing.T) string {
	t.Helper()
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	if err := os.MkdirAll(filepath.Join(profile, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(`{"profile":{"last_used":"Default"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile
}

func requiredCookieMap() map[string]string {
	out := map[string]string{}
	for _, req := range requiredCookies {
		out[req.name] = "val-" + req.name
	}
	return out
}

func TestSelectGoogleCookiesNormalizesDomain(t *testing.T) {
	got, err := selectGoogleCookies([]cdpCookie{
		{Name: "SID", Value: "sid", Domain: "google.com"},
		{Name: "HSID", Value: "hsid", Domain: ".google.com"},
		{Name: "SSID", Value: "ssid", Domain: ".google.com"},
		{Name: "APISID", Value: "apisid", Domain: ".google.com"},
		{Name: "SAPISID", Value: "sapisid", Domain: ".google.com"},
		{Name: "OSID", Value: "osid", Domain: "messages.google.com"},
	}, "Default")
	if err != nil {
		t.Fatalf("selectGoogleCookies(): %v", err)
	}
	if got["SID"] != "sid" {
		t.Fatalf("SID = %q", got["SID"])
	}
	if got["OSID"] != "osid" {
		t.Fatalf("OSID = %q", got["OSID"])
	}
}

func TestReadGoogleAccountCookiesUsesHeadlessRealProfile(t *testing.T) {
	origN, origC := nativeLoader, cdpPuller
	t.Cleanup(func() {
		nativeLoader, cdpPuller = origN, origC
	})

	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("locked")
	}
	cdpPuller = func(_ context.Context, _ string, _ string, headless bool) (map[string]string, error) {
		return requiredCookieMap(), nil
	}

	got, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    fakeChromeProfile(t),
		AllowInteractive: true,
		OnNeedBrowser:    func() { t.Fatal("must not ask the user to sign in") },
	})
	if err != nil {
		t.Fatalf("ReadGoogleAccountCookies(): %v", err)
	}
	if got["SID"] != "val-SID" {
		t.Fatalf("SID = %q", got["SID"])
	}
}

func TestReadGoogleAccountCookiesWaitsForChromeQuit(t *testing.T) {
	origN, origC, origU := nativeLoader, cdpPuller, profileInUse
	t.Cleanup(func() {
		nativeLoader, cdpPuller, profileInUse = origN, origC, origU
	})

	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("locked")
	}
	checks := 0
	profileInUse = func(string) bool {
		checks++
		return checks < 3
	}
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		return requiredCookieMap(), nil
	}

	var needQuit bool
	got, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    fakeChromeProfile(t),
		AllowInteractive: true,
		OnNeedBrowser:    func() { needQuit = true },
	})
	if err != nil {
		t.Fatalf("ReadGoogleAccountCookies(): %v", err)
	}
	if got["SID"] != "val-SID" {
		t.Fatalf("SID = %q", got["SID"])
	}
	if !needQuit {
		t.Fatal("expected OnNeedBrowser while Chrome held the profile")
	}
}

func TestReadGoogleAccountCookiesSilentSkipsCDPWhenChromeOpen(t *testing.T) {
	origN, origC, origU := nativeLoader, cdpPuller, profileInUse
	t.Cleanup(func() {
		nativeLoader, cdpPuller, profileInUse = origN, origC, origU
	})
	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("locked")
	}
	profileInUse = func(string) bool { return true }
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		t.Fatal("cdp must not run while Chrome holds the profile")
		return nil, nil
	}
	_, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    t.TempDir(),
		AllowInteractive: false,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadGoogleAccountCookiesSilentDoesNotOpenBrowser(t *testing.T) {
	origN, origC := nativeLoader, cdpPuller
	t.Cleanup(func() {
		nativeLoader, cdpPuller = origN, origC
	})
	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("locked")
	}
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		t.Fatal("cdp should not run when pair-browser is empty and interactive is false")
		return nil, nil
	}
	_, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    t.TempDir(),
		AllowInteractive: false,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadGoogleAccountCookiesDropsStaleLockErrorAfterWait(t *testing.T) {
	origN, origC, origU := nativeLoader, cdpPuller, profileInUse
	t.Cleanup(func() {
		nativeLoader, cdpPuller, profileInUse = origN, origC, origU
	})
	calls := 0
	nativeLoader = func(context.Context, string) (map[string]string, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("open Cookies: The process cannot access the file because it is being used by another process")
		}
		return nil, fmt.Errorf("Chrome profile Default is missing SID, HSID, SSID, APISID, SAPISID")
	}
	checks := 0
	profileInUse = func(string) bool {
		checks++
		return checks < 4
	}
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		return nil, fmt.Errorf("Chrome DevTools did not start: context deadline exceeded")
	}

	var needQuit bool
	_, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    fakeChromeProfile(t),
		AllowInteractive: true,
		OnNeedBrowser:    func() { needQuit = true },
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !needQuit {
		t.Fatal("expected OnNeedBrowser")
	}
	msg := err.Error()
	if strings.Contains(msg, "used by another process") || strings.Contains(msg, "missing SID") {
		t.Fatalf("stale Chrome-open errors survived wait: %v", err)
	}
	if strings.Contains(msg, "still locked") {
		t.Fatalf("DevTools timeout should not be reported as a file lock: %v", err)
	}
	if !strings.Contains(msg, "cookie read") {
		t.Fatalf("want cookie-read failure, got %v", err)
	}
}

func TestReadGoogleAccountCookiesDoesNotCDPWhileLocked(t *testing.T) {
	origN, origC, origU := nativeLoader, cdpPuller, profileInUse
	t.Cleanup(func() {
		nativeLoader, cdpPuller, profileInUse = origN, origC, origU
	})
	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("open Cookies: The process cannot access the file because it is being used by another process")
	}
	profileInUse = func(string) bool { return true }
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		t.Fatal("cdp must not launch against a locked profile")
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_, err := ReadGoogleAccountCookies(ctx, ReadConfig{
		ChromeProfile:    t.TempDir(),
		AllowInteractive: true,
		OnNeedBrowser:    func() {},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "still locked after the process exited") {
		t.Fatalf("got %v", err)
	}
}

func TestReadGoogleAccountCookiesSkipsCDPWhenSqliteHasNoAccountCookies(t *testing.T) {
	origN, origC, origU := nativeLoader, cdpPuller, profileInUse
	t.Cleanup(func() {
		nativeLoader, cdpPuller, profileInUse = origN, origC, origU
	})
	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("native decrypt failed")
	}
	profileInUse = func(string) bool { return false }
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		t.Fatal("cdp must not launch when sqlite has no Gaia cookies")
		return nil, nil
	}
	userData := t.TempDir()
	seedChromeCookieNames(t, userData, "Default", []string{"NID"})
	_, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    filepath.Join(userData, "Default"),
		AllowInteractive: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing SID") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "no Chrome profile has Google account cookies") {
		t.Fatalf("got %v", err)
	}
}

func TestReadGoogleAccountCookiesSkipsCDPWhenAccountCookiesPresent(t *testing.T) {
	origN, origC, origU := nativeLoader, cdpPuller, profileInUse
	t.Cleanup(func() {
		nativeLoader, cdpPuller, profileInUse = origN, origC, origU
	})
	nativeLoader = func(context.Context, string) (map[string]string, error) {
		return nil, fmt.Errorf("Chrome profile Personal is signed into Chrome but is missing SID, HSID, SSID, APISID, SAPISID. Open google.com in that profile, quit Chrome fully, then retry")
	}
	profileInUse = func(string) bool { return false }
	cdpPuller = func(context.Context, string, string, bool) (map[string]string, error) {
		t.Fatal("cdp must not launch when sqlite already has Gaia cookies")
		return nil, nil
	}
	userData := t.TempDir()
	seedChromeCookieNames(t, userData, "Default", []string{"SID", "HSID", "SSID", "APISID", "SAPISID"})
	_, err := ReadGoogleAccountCookies(context.Background(), ReadConfig{
		ChromeProfile:    filepath.Join(userData, "Default"),
		AllowInteractive: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot read them") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "Open google.com") {
		t.Fatalf("still told user cookies were missing: %v", err)
	}
}

func TestSummarizeCookieReadErrorCollapsesDuplicates(t *testing.T) {
	err := summarizeCookieReadError([]error{
		fmt.Errorf("open Cookies: The process cannot access the file because it is being used by another process"),
		fmt.Errorf("Chrome profile Default is missing SID, HSID, SSID, APISID, SAPISID. Sign into Google in that profile, quit Chrome fully, then retry"),
		fmt.Errorf("Chrome profile Default is missing SID, HSID, SSID, APISID, SAPISID. Sign into Google in that profile, quit Chrome fully, then retry"),
		fmt.Errorf("Chrome DevTools did not start: context deadline exceeded"),
	})
	msg := err.Error()
	if strings.Count(msg, "missing SID") != 0 || strings.Count(msg, "used by another process") != 0 {
		t.Fatalf("dumped raw join: %s", msg)
	}
	if !strings.Contains(msg, "still locked after the process exited") {
		t.Fatalf("got %s", msg)
	}
}

func TestSummarizeCookieReadErrorPrefersAppBoundOverMissing(t *testing.T) {
	err := summarizeCookieReadError([]error{
		fmt.Errorf("Chrome profile Personal is signed into Chrome but is missing SID, HSID, SSID, APISID, SAPISID. Open google.com in that profile, quit Chrome fully, then retry"),
		fmt.Errorf("Chrome profile Personal has Google account cookies, but current Chrome encrypts them so om-tui cannot read them. Paste with ctrl+v"),
	})
	msg := err.Error()
	if strings.Contains(msg, "Open google.com") || strings.Contains(msg, "missing SID") {
		t.Fatalf("app-bound lost to missing: %s", msg)
	}
	if !strings.Contains(msg, "cannot read them") {
		t.Fatalf("got %s", msg)
	}
}

func TestSummarizeDevToolsTimeoutIsNotLockLag(t *testing.T) {
	err := summarizeCookieReadError([]error{
		fmt.Errorf("Chrome DevTools did not start: context deadline exceeded"),
	})
	msg := err.Error()
	if strings.Contains(msg, "still locked") {
		t.Fatalf("DevTools timeout reported as a lock: %s", msg)
	}
	if !strings.Contains(msg, "cookie read") {
		t.Fatalf("got %s", msg)
	}
}

func TestChromeProfileInUseUnixLockfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses exclusive lockfile handles, not mere existence")
	}
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userData, "lockfile"), []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !chromeProfileInUse(profile) {
		t.Fatal("lockfile should mean Chrome is open")
	}
}

func TestResolveChromeProfileUsesLastUsed(t *testing.T) {
	userData := t.TempDir()
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(`{"profile":{"last_used":"Profile 1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := resolveChromeProfile(userData)
	want := filepath.Join(userData, "Profile 1")
	if got != want {
		t.Fatalf("resolveChromeProfile() = %q, want %q", got, want)
	}
}

func TestResolveChromeProfileFallsBackToDefault(t *testing.T) {
	got := resolveChromeProfile(t.TempDir())
	if filepath.Base(got) != "Default" {
		t.Fatalf("resolveChromeProfile() = %q, want Default", got)
	}
}

func TestChromeProfileLabelUsesDisplayName(t *testing.T) {
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	state := `{"profile":{"last_used":"Default","info_cache":{"Default":{"name":"Personal","gaia_id":"123"}}}}`
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	got := chromeProfileLabel(profile)
	if got != "Chrome profile Personal" {
		t.Fatalf("chromeProfileLabel() = %q, want Chrome profile Personal", got)
	}
	if !chromeProfileHasGaia(profile) {
		t.Fatal("expected gaia sign-in")
	}
	err := missingRequiredCookiesError(profile, []string{".google.com:SID"})
	if !strings.Contains(err.Error(), "Personal") || !strings.Contains(err.Error(), "signed into Chrome") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "Chrome profile Default") {
		t.Fatalf("folder name leaked into error: %v", err)
	}
}

func seedChromeCookieNames(t *testing.T, userData, profile string, names []string) {
	t.Helper()
	dir := filepath.Join(userData, profile, "Network")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rows := make([]dbCookie, 0, len(names))
	for _, name := range names {
		rows = append(rows, dbCookie{host: ".google.com", name: name, encrypted: []byte("v10x")})
	}
	writeCookieDB(t, filepath.Join(dir, "Cookies"), rows)
}

func TestResolveChromeProfileSkipsLastUsedWithoutAccountCookies(t *testing.T) {
	userData := t.TempDir()
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(`{"profile":{"last_used":"Default"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	seedChromeCookieNames(t, userData, "Default", []string{"NID"})
	seedChromeCookieNames(t, userData, "Profile 1", []string{"SID", "HSID", "SSID", "APISID", "SAPISID"})
	got := resolveChromeProfile(userData)
	if filepath.Base(got) != "Profile 1" {
		t.Fatalf("resolveChromeProfile() = %q, want Profile 1", got)
	}
}

func TestResolveChromeProfilePrefersLastUsedWhenSignedIn(t *testing.T) {
	userData := t.TempDir()
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(`{"profile":{"last_used":"Profile 1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	seedChromeCookieNames(t, userData, "Profile 1", []string{"SID", "HSID", "SSID", "APISID", "SAPISID"})
	seedChromeCookieNames(t, userData, "Profile 2", []string{"SID", "HSID", "SSID", "APISID", "SAPISID"})
	got := resolveChromeProfile(userData)
	if filepath.Base(got) != "Profile 1" {
		t.Fatalf("resolveChromeProfile() = %q, want Profile 1", got)
	}
}

func TestResolveChromeProfilePicksNewestSignedInProfile(t *testing.T) {
	userData := t.TempDir()
	if err := os.WriteFile(filepath.Join(userData, "Local State"), []byte(`{"profile":{"last_used":"Default"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	seedChromeCookieNames(t, userData, "Default", []string{"NID"})
	seedChromeCookieNames(t, userData, "Profile 1", []string{"SID", "HSID", "SSID", "APISID", "SAPISID"})
	seedChromeCookieNames(t, userData, "Profile 2", []string{"SID", "HSID", "SSID", "APISID", "SAPISID"})
	old := time.Now().Add(-48 * time.Hour)
	newer := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(userData, "Profile 1", "Network", "Cookies"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(userData, "Profile 2", "Network", "Cookies"), newer, newer); err != nil {
		t.Fatal(err)
	}
	got := resolveChromeProfile(userData)
	if filepath.Base(got) != "Profile 2" {
		t.Fatalf("resolveChromeProfile() = %q, want Profile 2", got)
	}
}
