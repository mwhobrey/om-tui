//go:build !windows

package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const insecureMagic = "OMV1DEV\n"

// insecureRequested reports whether the caller explicitly opted into the
// weak, machine-local-file-derived key instead of a real OS-backed secret
// store. Local testing / CI only — never set in production.
func insecureRequested() bool {
	return os.Getenv("OPENMESSAGES_VAULT_INSECURE") == "1"
}

// sealInsecure and openInsecure implement the pre-existing (pre-Keychain/
// Secret-Service) fallback: AES-GCM with a key derived from a machine-local
// random file. Not a substitute for DPAPI/Keychain/Secret Service.
func sealInsecure(plaintext []byte) ([]byte, error) {
	key, err := insecureKey()
	if err != nil {
		return nil, err
	}
	sealed, err := sealWithKey(key, plaintext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(insecureMagic)+len(sealed))
	out = append(out, insecureMagic...)
	out = append(out, sealed...)
	return out, nil
}

func openInsecure(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < len(insecureMagic) || string(ciphertext[:len(insecureMagic)]) != insecureMagic {
		return nil, fmt.Errorf("vault: invalid insecure blob")
	}
	key, err := insecureKey()
	if err != nil {
		return nil, err
	}
	return openWithKey(key, ciphertext[len(insecureMagic):])
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
