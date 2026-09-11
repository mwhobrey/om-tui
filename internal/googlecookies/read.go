package googlecookies

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var errChromeNotFound = errors.New("Chrome is not installed")

// ReadConfig controls how Google account cookies are loaded from a local browser.
type ReadConfig struct {
	ChromeProfile    string
	PairBrowserDir   string
	AllowInteractive bool
	OnNeedBrowser    func()
}

func (c ReadConfig) profile() string {
	if p := strings.TrimSpace(c.ChromeProfile); p != "" {
		return p
	}
	return DefaultChromeProfile()
}

// ReadGoogleAccountCookies returns the five Google account cookies needed for
// Gaia pairing. Cookie values are never logged.
//
// Google blocks sign-in in any Chrome we launch with remote debugging ("This
// browser or app may not be secure"). So we never open a Google login window.
// We decrypt the user's real Chrome profile, or briefly run headless Chrome
// against a *copy* of that profile's cookie files after the user quits Chrome.
// We never launch Chrome against the live User Data directory: remote debugging
// is refused there, and a debug session on the real profile can empty the
// cookie DB. Chrome must already be signed into the Google account.
func ReadGoogleAccountCookies(ctx context.Context, cfg ReadConfig) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	profile := cfg.profile()
	cookies, errs := loadCookiesSilent(ctx, profile)
	if cookies != nil {
		return cookies, nil
	}

	if !cfg.AllowInteractive {
		return nil, summarizeCookieReadError(errs)
	}

	if profileInUse(profile) || cookieFilesBusy(errs) || cookieDBLocked(profile) {
		if cfg.OnNeedBrowser != nil {
			cfg.OnNeedBrowser()
		}
		if err := waitChromeProfileFree(ctx, profile); err != nil {
			return nil, summarizeCookieReadError(append(errs, err))
		}
		profile = cfg.profile()
		cookies, errs = loadCookiesSilent(ctx, profile)
		if cookies != nil {
			return cookies, nil
		}
	}

	if profileInUse(profile) || cookieFilesBusy(errs) || cookieDBLocked(profile) {
		return nil, summarizeCookieReadError(append(errs, errCookieFilesLocked))
	}

	if profile == "" {
		return nil, summarizeCookieReadError(append(errs, fmt.Errorf("no Chrome profile directory")))
	}
	if cookieDBExists(profile) && requiredCookieNamesPresent(profile) {
		return nil, summarizeCookieReadError(append(errs, appBoundCookiesError(profile)))
	}
	if cookieDBExists(profile) && !requiredCookieNamesPresent(profile) {
		if cookieDBLocked(profile) || profileInUse(profile) {
			return nil, summarizeCookieReadError(append(errs, errCookieFilesLocked))
		}
		return nil, summarizeCookieReadError(append(errs, missingAllRequiredCookies(profile)))
	}
	cookies, err := cdpCookiesFromProfile(ctx, profile)
	if err == nil {
		return cookies, nil
	}
	return nil, summarizeCookieReadError(append(errs, err))
}

func cdpCookiesFromProfile(ctx context.Context, profile string) (map[string]string, error) {
	copyDir, profileDir, cleanup, err := snapshotUserDataForCDP(profile)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return cdpPuller(ctx, copyDir, profileDir, true)
}

func loadCookiesSilent(ctx context.Context, profile string) (map[string]string, []error) {
	if err := ctx.Err(); err != nil {
		return nil, []error{err}
	}
	if cookies, err := nativeLoader(ctx, profile); err == nil {
		return cookies, nil
	} else if err != nil {
		return nil, []error{err}
	}
	return nil, nil
}

var (
	nativeLoader = loadNativeChromeCookies
	cdpPuller    = pullCookiesFromUserDataDir
	profileInUse = chromeProfileInUse
)

func loadNativeChromeCookies(ctx context.Context, profile string) (map[string]string, error) {
	if profile == "" {
		return nil, fmt.Errorf("no Chrome profile directory")
	}
	secret, err := chromeSafeStorageSecret(ctx)
	if err != nil {
		return nil, fmt.Errorf("chrome cookie key: %w", err)
	}
	return LoadChromeCookies(profile, secret)
}

func chromeProfileInUse(profile string) bool {
	if profile == "" {
		return false
	}
	userData := filepath.Dir(profile)
	if runtime.GOOS == "windows" {
		// Existence of lockfile is not enough: Windows leaves it behind after
		// a clean quit. Exclusive open fails only while a handle is still live,
		// including the few seconds after chrome.exe has already vanished.
		return fileHeld(filepath.Join(userData, "lockfile")) || cookieDBLocked(profile)
	}
	for _, name := range []string{"lockfile", "SingletonLock", "SingletonCookie", "SingletonSocket"} {
		if _, err := os.Stat(filepath.Join(userData, name)); err == nil {
			return true
		}
	}
	return cookieDBLocked(profile)
}

func cookieDBLocked(profile string) bool {
	for _, cookies := range []string{
		filepath.Join(profile, "Network", "Cookies"),
		filepath.Join(profile, "Cookies"),
	} {
		if _, err := os.Stat(cookies); err != nil {
			continue
		}
		return fileHeld(cookies)
	}
	return false
}

const (
	chromeUnlockPoll   = 400 * time.Millisecond
	chromeUnlockStable = 3
)

func waitChromeProfileFree(ctx context.Context, profile string) error {
	ticker := time.NewTicker(chromeUnlockPoll)
	defer ticker.Stop()
	free := 0
	for {
		if !profileInUse(profile) {
			free++
			if free >= chromeUnlockStable {
				return nil
			}
		} else {
			free = 0
		}
		select {
		case <-ctx.Done():
			return errCookieFilesLocked
		case <-ticker.C:
		}
	}
}

var errCookieFilesLocked = errors.New("Chrome's cookie file is still locked after Chrome exited")

func cookieFilesBusy(errs []error) bool {
	for _, err := range errs {
		if isSharingBusy(err) {
			return true
		}
	}
	return false
}

func isSharingBusy(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "used by another process") ||
		strings.Contains(low, "sharing violation") ||
		strings.Contains(low, "text file busy")
}

func summarizeCookieReadError(errs []error) error {
	var sharing, appbound, devtools, missing, other []error
	seen := map[string]bool{}
	for _, err := range errs {
		if err == nil {
			continue
		}
		msg := err.Error()
		if seen[msg] {
			continue
		}
		seen[msg] = true
		low := strings.ToLower(msg)
		switch {
		case isSharingBusy(err) || errors.Is(err, errCookieFilesLocked) || strings.Contains(low, "still locked"):
			sharing = append(sharing, err)
		case strings.Contains(low, "encrypts them") || strings.Contains(low, "cannot read them"):
			appbound = append(appbound, err)
		case strings.Contains(msg, "DevTools did not start"):
			devtools = append(devtools, err)
		case strings.Contains(msg, "missing") && strings.Contains(msg, "SID"):
			missing = append(missing, err)
		default:
			other = append(other, err)
		}
	}
	if len(sharing) > 0 {
		return fmt.Errorf("Chrome's files were still locked after the process exited. Wait a couple seconds, then press enter. Paste with ctrl+v if it keeps failing")
	}
	if len(appbound) > 0 {
		return appbound[0]
	}
	if len(devtools) > 0 {
		return fmt.Errorf("Chrome did not start for a cookie read. Press enter to retry, or paste with ctrl+v")
	}
	if len(missing) > 0 {
		return missing[0]
	}
	if len(other) == 1 {
		return other[0]
	}
	if len(other) > 1 {
		return errors.Join(other...)
	}
	return fmt.Errorf("could not read Google cookies from Chrome")
}
