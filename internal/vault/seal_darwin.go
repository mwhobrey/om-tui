//go:build darwin

package vault

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

const (
	keychainService = "om-tui"
	keychainAccount = "river-vault-key"
)

// seal/open use a 32-byte master key stored in the macOS login Keychain
// (via the `security` CLI, so no cgo/Security.framework linking is needed —
// this keeps CGO_ENABLED=0 cross-compilation for release builds unaffected;
// Keychain access only happens at runtime on an actual Mac) to wrap
// per-river credential blobs with AES-GCM.
func seal(plaintext []byte) ([]byte, error) {
	if insecureRequested() {
		return sealInsecure(plaintext)
	}
	key, err := keychainKey()
	if err != nil {
		return nil, err
	}
	return sealWithKey(key, plaintext)
}

func open(ciphertext []byte) ([]byte, error) {
	if insecureRequested() {
		return openInsecure(ciphertext)
	}
	key, err := keychainKey()
	if err != nil {
		return nil, err
	}
	return openWithKey(key, ciphertext)
}

func keychainKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w").Output()
	if err == nil {
		key, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
		if decErr == nil && len(key) == 32 {
			return key, nil
		}
	}
	return createKeychainKey()
}

func createKeychainKey() ([]byte, error) {
	raw, err := randomKey()
	if err != nil {
		return nil, fmt.Errorf("vault: generate keychain key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	add := exec.Command("security", "add-generic-password",
		"-s", keychainService, "-a", keychainAccount, "-w", encoded, "-U")
	if out, err := add.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%w: store Keychain key via `security add-generic-password`: %v (%s)",
			ErrUnsupported, err, strings.TrimSpace(string(out)))
	}
	return raw, nil
}
