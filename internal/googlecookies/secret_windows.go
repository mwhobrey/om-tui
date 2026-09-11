//go:build windows

package googlecookies

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func defaultChromeProfileDir(home string) string {
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return resolveChromeProfile(filepath.Join(local, "Google", "Chrome", "User Data"))
	}
	return resolveChromeProfile(filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data"))
}

// chromeSafeStorageSecret unwraps Chrome's AES-256 cookie key from Local State
// via DPAPI. This decrypts v10 cookies. Current Chrome also stores an
// app-bound v20 key that DPAPI alone cannot unwrap; ReadGoogleAccountCookies
// then lets Chrome decrypt those via CDP. The key is never logged.
func chromeSafeStorageSecret(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile := DefaultChromeProfile()
	if profile == "" {
		return nil, fmt.Errorf("no Chrome profile directory")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(profile), "Local State"))
	if err != nil {
		return nil, fmt.Errorf("read Chrome Local State: %w", err)
	}
	var state struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("parse Chrome Local State: %w", err)
	}
	blob, err := base64.StdEncoding.DecodeString(state.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("decode Chrome encrypted_key: %w", err)
	}
	const dpapiPrefix = "DPAPI"
	if len(blob) <= len(dpapiPrefix) || string(blob[:len(dpapiPrefix)]) != dpapiPrefix {
		return nil, fmt.Errorf("Chrome encrypted_key is not DPAPI-wrapped")
	}
	key, err := dpapiUnprotect(blob[len(dpapiPrefix):])
	if err != nil {
		return nil, fmt.Errorf("unprotect Chrome cookie key: %w", err)
	}
	if len(key) != 16 && len(key) != 32 {
		return nil, fmt.Errorf("Chrome cookie key has unexpected length %d", len(key))
	}
	return key, nil
}

func dpapiUnprotect(ciphertext []byte) ([]byte, error) {
	var in, out windows.DataBlob
	if len(ciphertext) > 0 {
		in.Size = uint32(len(ciphertext))
		in.Data = &ciphertext[0]
	}
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	buf := make([]byte, out.Size)
	copy(buf, unsafe.Slice(out.Data, out.Size))
	return buf, nil
}
