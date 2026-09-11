// Package googlecookies refreshes the Google account cookies inside
// OpenMessage's session.json from a local Chrome profile.
//
// Google-account libgm sessions authenticate with five .google.com account
// cookies (SID, HSID, SSID, APISID, and SAPISID) plus SAPISIDHASH. The cookies
// rotate roughly every 30 minutes. When the backend has been offline long
// enough (laptop asleep, travel), the stored cookies expire and every token
// refresh returns HTTP 401 — the session looks dead, but the phone-side device
// link is usually still intact. Rewriting auth_data.cookies with fresh values
// from the user's signed-in Chrome profile and reconnecting revives it with no
// re-pairing. A messages.google.com OSID cookie exists only when the user has
// used Messages for web in that Chrome profile; it is carried when present but
// is never required.
//
// This is the native (in-process) equivalent of
// scripts/refresh-google-session-cookies-{linux,macos}.py, so app installs
// self-heal without any external script configured.
package googlecookies

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/pbkdf2"
	_ "modernc.org/sqlite"
)

type requiredCookie struct {
	host string
	name string
}

var requiredCookies = []requiredCookie{
	{".google.com", "SID"},
	{".google.com", "HSID"},
	{".google.com", "SSID"},
	{".google.com", "APISID"},
	{".google.com", "SAPISID"},
}

var hostPriority = map[string]int{
	"messages.google.com": 0,
	".google.com":         1,
	"accounts.google.com": 2,
}

// NativeSupported reports whether a Chrome cookie DB exists for the configured
// profile. Silent refresh still has to open that file (Chrome on Windows may
// hold it exclusively) or fall back to a previously used pair-browser profile.
func NativeSupported() bool {
	return cookieDBExists(DefaultChromeProfile())
}

func cookieDBPaths(profile string) []string {
	return []string{
		filepath.Join(profile, "Network", "Cookies"),
		filepath.Join(profile, "Cookies"),
	}
}

func cookieDBExists(profile string) bool {
	if profile == "" {
		return false
	}
	for _, c := range cookieDBPaths(profile) {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

// DefaultChromeProfile returns the Chrome profile directory to read cookies
// from, honouring the same OPENMESSAGE_CHROME_PROFILE override as the
// standalone refresh scripts.
func DefaultChromeProfile() string {
	if p := strings.TrimSpace(os.Getenv("OPENMESSAGE_CHROME_PROFILE")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return defaultChromeProfileDir(home)
}

func resolveChromeProfile(userData string) string {
	if strings.TrimSpace(userData) == "" {
		return ""
	}
	state := readChromeProfileState(userData)
	lastUsed := state.lastUsed
	if lastUsed == "" {
		lastUsed = "Default"
	}
	fallback := filepath.Join(userData, lastUsed)

	var names []string
	seen := map[string]bool{}
	addName := func(name string) {
		name = sanitizeChromeProfileName(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	addName(lastUsed)
	for _, name := range state.active {
		addName(name)
	}
	addName("Default")
	if entries, err := os.ReadDir(userData); err == nil {
		for _, ent := range entries {
			if ent.IsDir() {
				addName(ent.Name())
			}
		}
	}

	var signedIn []string
	for _, name := range names {
		profile := filepath.Join(userData, name)
		if !cookieDBExists(profile) {
			continue
		}
		if requiredCookieNamesPresent(profile) {
			signedIn = append(signedIn, profile)
		}
	}
	if len(signedIn) == 0 {
		return fallback
	}
	lastUsedPath := filepath.Join(userData, lastUsed)
	for _, profile := range signedIn {
		if profile == lastUsedPath {
			return profile
		}
	}
	for _, name := range state.active {
		want := filepath.Join(userData, sanitizeChromeProfileName(name))
		for _, profile := range signedIn {
			if profile == want {
				return profile
			}
		}
	}
	best := signedIn[0]
	bestTime := cookieDBModTime(best)
	for _, profile := range signedIn[1:] {
		if t := cookieDBModTime(profile); t.After(bestTime) {
			best = profile
			bestTime = t
		}
	}
	return best
}

type chromeProfileState struct {
	lastUsed     string
	active       []string
	displayNames map[string]string
	signedInGaia map[string]bool
}

func readChromeProfileState(userData string) chromeProfileState {
	raw, err := os.ReadFile(filepath.Join(userData, "Local State"))
	if err != nil {
		return chromeProfileState{}
	}
	var state struct {
		Profile struct {
			LastUsed           string   `json:"last_used"`
			LastActiveProfiles []string `json:"last_active_profiles"`
			InfoCache          map[string]struct {
				Name   string `json:"name"`
				GaiaID string `json:"gaia_id"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return chromeProfileState{}
	}
	out := chromeProfileState{
		lastUsed:     sanitizeChromeProfileName(state.Profile.LastUsed),
		displayNames: map[string]string{},
		signedInGaia: map[string]bool{},
	}
	for _, name := range state.Profile.LastActiveProfiles {
		if n := sanitizeChromeProfileName(name); n != "" {
			out.active = append(out.active, n)
		}
	}
	for dir, info := range state.Profile.InfoCache {
		key := sanitizeChromeProfileName(dir)
		if key == "" {
			continue
		}
		if n := strings.TrimSpace(info.Name); n != "" {
			out.displayNames[key] = n
		}
		if strings.TrimSpace(info.GaiaID) != "" {
			out.signedInGaia[key] = true
		}
	}
	return out
}

func sanitizeChromeProfileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

func cookieDBModTime(profile string) time.Time {
	var best time.Time
	for _, path := range cookieDBPaths(profile) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(best) {
			best = info.ModTime()
		}
	}
	return best
}

func requiredCookieNamesPresent(profile string) bool {
	dbCopy, cleanup, err := snapshotCookieDB(profile)
	if err != nil {
		return false
	}
	defer cleanup()
	db, err := sql.Open("sqlite", dbCopy)
	if err != nil {
		return false
	}
	defer db.Close()
	rows, err := db.Query(`select distinct name from cookies where name in ('SID','HSID','SSID','APISID','SAPISID')`)
	if err != nil {
		return false
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false
		}
		seen[name] = true
	}
	if rows.Err() != nil {
		return false
	}
	for _, req := range requiredCookies {
		if !seen[req.name] {
			return false
		}
	}
	return true
}

func requiredNamesPresentInRows(rows []cookieRow) bool {
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.name] = true
	}
	for _, req := range requiredCookies {
		if !seen[req.name] {
			return false
		}
	}
	return true
}

func requiredCookieValuesLookAppBound(profile string) bool {
	dbCopy, cleanup, err := snapshotCookieDB(profile)
	if err != nil {
		return false
	}
	defer cleanup()
	db, err := sql.Open("sqlite", dbCopy)
	if err != nil {
		return false
	}
	defer db.Close()
	_, _ = db.Exec(`PRAGMA busy_timeout=2000`)
	rows, err := db.Query(`select name, encrypted_value from cookies where name in ('SID','HSID','SSID','APISID','SAPISID')`)
	if err != nil {
		return false
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name string
		var enc []byte
		if err := rows.Scan(&name, &enc); err != nil {
			return false
		}
		if len(enc) >= 3 && string(enc[:3]) == "v20" {
			seen[name] = true
		}
	}
	if rows.Err() != nil {
		return false
	}
	for _, req := range requiredCookies {
		if !seen[req.name] {
			return false
		}
	}
	return true
}

func appBoundCookiesError(profile string) error {
	return fmt.Errorf("%s has Google account cookies, but current Chrome encrypts them so om-tui cannot read them. Paste with ctrl+v", chromeProfileLabel(profile))
}

func cookiesAppBound(profile string) bool {
	return requiredCookieNamesPresent(profile) && requiredCookieValuesLookAppBound(profile)
}

func chromeProfileLabel(profile string) string {
	base := filepath.Base(strings.TrimSpace(profile))
	if base == "" || base == "." || strings.EqualFold(base, "User Data") {
		return "Chrome"
	}
	if name := readChromeProfileState(filepath.Dir(profile)).displayNames[base]; name != "" {
		return "Chrome profile " + name
	}
	return "Chrome profile " + base
}

func chromeProfileHasGaia(profile string) bool {
	base := filepath.Base(strings.TrimSpace(profile))
	return readChromeProfileState(filepath.Dir(profile)).signedInGaia[base]
}

func missingAllRequiredCookies(string) error {
	return fmt.Errorf("no Chrome profile has Google account cookies (missing SID, HSID, SSID, APISID, SAPISID). Sign into Google in Chrome, quit Chrome fully, then retry")
}

func missingRequiredCookiesError(profile string, missing []string) error {
	names := make([]string, 0, len(missing))
	for _, item := range missing {
		name := item
		if _, n, ok := strings.Cut(item, ":"); ok {
			name = n
		}
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		names = []string{"Google cookies"}
	}
	if chromeProfileHasGaia(profile) {
		return fmt.Errorf("%s is signed into Chrome but is missing %s. Open google.com in that profile, quit Chrome fully, then retry", chromeProfileLabel(profile), strings.Join(names, ", "))
	}
	return fmt.Errorf("%s is missing %s. Sign into Google in that profile, quit Chrome fully, then retry", chromeProfileLabel(profile), strings.Join(names, ", "))
}

// Refresh reads Google cookies from the Chrome profile and rewrites
// auth_data.cookies in sessionPath. It never logs or returns cookie values.
// Interactive Chrome windows are never opened from this path.
func Refresh(ctx context.Context, profile, sessionPath string) error {
	if profile == "" {
		return fmt.Errorf("no Chrome profile directory")
	}
	cookies, err := ReadGoogleAccountCookies(ctx, ReadConfig{
		ChromeProfile:    profile,
		PairBrowserDir:   filepath.Join(filepath.Dir(sessionPath), "google-pair-browser"),
		AllowInteractive: false,
	})
	if err != nil {
		return err
	}
	return UpdateSessionCookies(sessionPath, cookies)
}

// DeriveKey derives the AES-128 key Chrome uses for cookie encryption.
func DeriveKey(secret []byte, iterations int) []byte {
	return pbkdf2.Key(secret, []byte("saltysalt"), iterations, 16, sha1.New)
}

// DecryptCookie decrypts one Chrome encrypted_value. It returns ("", nil) for
// values without a v10/v11 prefix (unencrypted or unknown scheme).
func DecryptCookie(encrypted []byte, host string, key []byte) (string, error) {
	if len(encrypted) < 3 {
		return "", nil
	}
	prefix := string(encrypted[:3])
	if prefix != "v10" && prefix != "v11" {
		return "", nil
	}
	ciphertext := encrypted[3:]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext length %d not a block multiple", len(ciphertext))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := []byte("                ") // 16 spaces
	padded := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, ciphertext)

	padLen := int(padded[len(padded)-1])
	if padLen < 1 || padLen > aes.BlockSize || padLen > len(padded) {
		return "", fmt.Errorf("invalid cookie padding")
	}
	plain := padded[:len(padded)-padLen]

	// Chrome 130+ prepends SHA256(host_key) to the plaintext.
	hostHash := sha256.Sum256([]byte(host))
	if len(plain) >= 32 && strings.HasPrefix(string(plain), string(hostHash[:])) {
		plain = plain[32:]
	}
	if !utf8.Valid(plain) {
		return "", fmt.Errorf("decrypted cookie is not valid UTF-8")
	}
	return string(plain), nil
}

const (
	gcmNonceSize = 12
	gcmTagSize   = 16
)

// DecryptCookieGCM decrypts a Windows Chrome v10/v20 cookie (AES-GCM,
// 12-byte nonce, 16-byte tag). It returns ("", nil) for other prefixes.
func DecryptCookieGCM(encrypted []byte, host string, key []byte) (string, error) {
	if len(encrypted) < 3 {
		return "", nil
	}
	prefix := string(encrypted[:3])
	if prefix != "v10" && prefix != "v11" && prefix != "v20" {
		return "", nil
	}
	payload := encrypted[3:]
	if len(payload) < gcmNonceSize+gcmTagSize {
		return "", fmt.Errorf("gcm cookie too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := payload[:gcmNonceSize]
	ciphertext := payload[gcmNonceSize:]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	hostHash := sha256.Sum256([]byte(host))
	if len(plain) >= 32 && strings.HasPrefix(string(plain), string(hostHash[:])) {
		plain = plain[32:]
	}
	if !utf8.Valid(plain) {
		return "", fmt.Errorf("decrypted cookie is not valid UTF-8")
	}
	return string(plain), nil
}

func decryptCookieWithKey(encrypted []byte, host string, key []byte, gcm bool) (string, error) {
	if gcm {
		return DecryptCookieGCM(encrypted, host, key)
	}
	return DecryptCookie(encrypted, host, key)
}

func scoreCookieRows(rows []cookieRow, key []byte, gcm bool) *attempt {
	att := &attempt{values: map[string]hostValue{}}
	for _, row := range rows {
		value := row.plainValue
		if value == "" && len(row.encrypted) > 0 {
			decrypted, err := decryptCookieWithKey(row.encrypted, row.host, key, gcm)
			if err != nil {
				att.failed++
				continue
			}
			value = decrypted
		}
		if value == "" {
			continue
		}
		att.ok++
		prev, exists := att.values[row.name]
		if !exists || priorityOf(row.host) < priorityOf(prev.host) {
			att.values[row.name] = hostValue{host: row.host, value: value}
		}
	}
	for _, req := range requiredCookies {
		if hv, ok := att.values[req.name]; ok && hv.host == req.host {
			att.present++
		}
	}
	return att
}

// LoadChromeCookies reads the profile's cookie DB and returns the decrypted
// Google cookies, trying both known PBKDF2 iteration counts and keeping the
// best-scoring result. The five .google.com account cookies are required;
// messages.google.com cookies such as OSID are carried only when present.
func LoadChromeCookies(profile string, secret []byte) (map[string]string, error) {
	dbCopy, cleanup, err := snapshotCookieDB(profile)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	rows, err := readCookieRows(dbCopy)
	if err != nil {
		return nil, err
	}

	var best *attempt
	// 1003 is macOS, 1 is Linux/basic; trying both keeps the loader portable.
	for _, iterations := range []int{1003, 1} {
		att := scoreCookieRows(rows, DeriveKey(secret, iterations), false)
		if best == nil || betterAttempt(att, best) {
			best = att
		}
	}
	// Windows Chrome uses AES-GCM with the DPAPI-unwrapped 32-byte key as-is.
	if len(secret) == 16 || len(secret) == 32 {
		att := scoreCookieRows(rows, secret, true)
		if best == nil || betterAttempt(att, best) {
			best = att
		}
	}

	var missing, undecrypted []string
	for _, req := range requiredCookies {
		hv, ok := best.values[req.name]
		if !ok {
			missing = append(missing, req.host+":"+req.name)
			undecrypted = append(undecrypted, req.name)
			continue
		}
		if normalizeCookieHost(hv.host) != req.host {
			missing = append(missing, req.host+":"+req.name)
		}
	}
	if len(undecrypted) > 0 && (requiredNamesPresentInRows(rows) || requiredCookieNamesPresent(profile)) {
		return nil, appBoundCookiesError(profile)
	}
	if len(missing) > 0 {
		return nil, missingRequiredCookiesError(profile, missing)
	}

	out := make(map[string]string, len(best.values))
	for name, hv := range best.values {
		out[name] = hv.value
	}
	return out, nil
}

type hostValue struct {
	host  string
	value string
}

type attempt struct {
	values  map[string]hostValue
	present int
	ok      int
	failed  int
}

type cookieRow struct {
	host       string
	name       string
	encrypted  []byte
	plainValue string
}

func priorityOf(host string) int {
	if p, ok := hostPriority[host]; ok {
		return p
	}
	return 10
}

func betterAttempt(a, b *attempt) bool {
	if a.present != b.present {
		return a.present > b.present
	}
	if a.ok != b.ok {
		return a.ok > b.ok
	}
	return a.failed < b.failed
}

// snapshotCookieDB copies the profile's cookie DB (with WAL/SHM sidecars) to
// a temp dir so we read the freshest values even while Chrome holds the live
// file — Google session cookies rotate ~30 minutes, so staleness matters.
func snapshotCookieDB(profile string) (string, func(), error) {
	candidates := []string{
		filepath.Join(profile, "Network", "Cookies"),
		filepath.Join(profile, "Cookies"),
	}
	var src string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			src = c
			break
		}
	}
	if src == "" {
		return "", nil, fmt.Errorf("chrome cookie DB not found under %s", profile)
	}

	tmpDir, err := os.MkdirTemp("", "om-cookie-refresh-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmpDir) }
	dst := filepath.Join(tmpDir, "Cookies")
	if err := copyFile(src, dst); err != nil {
		cleanup()
		return "", nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		side := src + suffix
		if _, err := os.Stat(side); err == nil {
			if err := copyFile(side, dst+suffix); err != nil {
				cleanup()
				return "", nil, err
			}
		}
	}
	return dst, cleanup, nil
}

func copyFile(src, dst string) error {
	data, err := readFileShared(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// readCookieRows opens the snapshot read-write so SQLite replays the WAL,
// making recently rotated cookies visible.
func readCookieRows(dbPath string) ([]cookieRow, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	_, _ = db.Exec(`PRAGMA busy_timeout=2000`)

	rows, err := db.Query(`
		select host_key, name, encrypted_value, value
		from cookies
		where host_key in ('.google.com','google.com','messages.google.com','accounts.google.com')
		   or host_key like '%.google.com'
		order by host_key, name`)
	if err != nil {
		return nil, fmt.Errorf("query cookies: %w", err)
	}
	defer rows.Close()

	var out []cookieRow
	for rows.Next() {
		var row cookieRow
		if err := rows.Scan(&row.host, &row.name, &row.encrypted, &row.plainValue); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateSessionCookies rewrites auth_data.cookies in session.json atomically,
// preserving every other field.
func UpdateSessionCookies(sessionPath string, cookies map[string]string) error {
	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		return fmt.Errorf("read session: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse session: %w", err)
	}
	authData, ok := data["auth_data"].(map[string]any)
	if !ok {
		authData = map[string]any{}
		data["auth_data"] = authData
	}
	authData["cookies"] = cookies

	updated, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	tmp := sessionPath + ".tmp"
	if err := os.WriteFile(tmp, append(updated, '\n'), 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("secure session: %w", err)
	}
	return os.Rename(tmp, sessionPath)
}
