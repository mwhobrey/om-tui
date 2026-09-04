//go:build linux

package vault

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

const (
	secretServiceAttr  = "om-tui-river-vault-key"
	secretServiceLabel = "om-tui river vault key"
)

// seal/open use a 32-byte master key stored in the Secret Service (GNOME
// Keyring, KWallet, etc. via D-Bus) through the `secret-tool` CLI —
// consistent with shelling out to `security` on macOS and `signal-cli`
// elsewhere in this codebase, rather than linking libsecret via cgo.
func seal(plaintext []byte) ([]byte, error) {
	if insecureRequested() {
		return sealInsecure(plaintext)
	}
	key, err := secretServiceKey()
	if err != nil {
		return nil, err
	}
	return sealWithKey(key, plaintext)
}

func open(ciphertext []byte) ([]byte, error) {
	if insecureRequested() {
		return openInsecure(ciphertext)
	}
	key, err := secretServiceKey()
	if err != nil {
		return nil, err
	}
	return openWithKey(key, ciphertext)
}

func secretServiceKey() ([]byte, error) {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil, fmt.Errorf("%w: `secret-tool` not found — install libsecret-tools (Debian/Ubuntu: "+
			"apt install libsecret-tools; needs a running Secret Service provider such as gnome-keyring) "+
			"or set OPENMESSAGES_VAULT_INSECURE=1 for local-only testing", ErrUnsupported)
	}
	out, err := exec.Command("secret-tool", "lookup", "attribute", secretServiceAttr).Output()
	if err == nil {
		key, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
		if decErr == nil && len(key) == 32 {
			return key, nil
		}
	}
	return createSecretServiceKey()
}

func createSecretServiceKey() ([]byte, error) {
	raw, err := randomKey()
	if err != nil {
		return nil, fmt.Errorf("vault: generate secret-service key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	store := exec.Command("secret-tool", "store",
		"--label="+secretServiceLabel, "attribute", secretServiceAttr)
	store.Stdin = strings.NewReader(encoded)
	if out, err := store.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%w: store Secret Service key via `secret-tool store`: %v (%s)",
			ErrUnsupported, err, strings.TrimSpace(string(out)))
	}
	return raw, nil
}
