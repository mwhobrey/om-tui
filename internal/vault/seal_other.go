//go:build !windows

package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const insecureMagic = "OMV1DEV\n"

// Non-Windows builds refuse real secrets unless OPENMESSAGES_VAULT_INSECURE=1
// (tests / explicit local-only). Uses AES-GCM with a key derived from a
// machine-local random file — not a substitute for DPAPI/Keychain.
func seal(plaintext []byte) ([]byte, error) {
	if os.Getenv("OPENMESSAGES_VAULT_INSECURE") != "1" {
		return nil, fmt.Errorf("%w: set OPENMESSAGES_VAULT_INSECURE=1 for non-Windows local testing only", ErrUnsupported)
	}
	key, err := insecureKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	out := make([]byte, 0, len(insecureMagic)+len(sealed))
	out = append(out, insecureMagic...)
	out = append(out, sealed...)
	return out, nil
}

func open(ciphertext []byte) ([]byte, error) {
	if os.Getenv("OPENMESSAGES_VAULT_INSECURE") != "1" {
		return nil, fmt.Errorf("%w: set OPENMESSAGES_VAULT_INSECURE=1 for non-Windows local testing only", ErrUnsupported)
	}
	if len(ciphertext) < len(insecureMagic) || string(ciphertext[:len(insecureMagic)]) != insecureMagic {
		return nil, fmt.Errorf("vault: invalid insecure blob")
	}
	raw := ciphertext[len(insecureMagic):]
	key, err := insecureKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("vault: ciphertext too short")
	}
	nonce, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt: %w", err)
	}
	return plain, nil
}

func insecureKey() ([]byte, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	path := dir + string(os.PathSeparator) + "openmessage-vault-dev.key"
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		sum := sha256.Sum256(b)
		return sum[:], nil
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, err
	}
	var hdr [8]byte
	binary.LittleEndian.PutUint64(hdr[:], 1)
	payload := append(hdr[:], raw...)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}
