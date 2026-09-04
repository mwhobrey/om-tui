//go:build !windows && !darwin && !linux

package vault

import "fmt"

// No OS-backed secret store is wired up for this platform (BSD, etc.).
// Real secrets require an explicit opt-in to the weak, machine-local-file
// fallback — see insecure.go.
func seal(plaintext []byte) ([]byte, error) {
	if !insecureRequested() {
		return nil, fmt.Errorf("%w: set OPENMESSAGES_VAULT_INSECURE=1 for local-only testing", ErrUnsupported)
	}
	return sealInsecure(plaintext)
}

func open(ciphertext []byte) ([]byte, error) {
	if !insecureRequested() {
		return nil, fmt.Errorf("%w: set OPENMESSAGES_VAULT_INSECURE=1 for local-only testing", ErrUnsupported)
	}
	return openInsecure(ciphertext)
}
