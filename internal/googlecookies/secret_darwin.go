//go:build darwin

package googlecookies

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
)

func defaultChromeProfileDir(home string) string {
	return resolveChromeProfile(filepath.Join(home, "Library", "Application Support", "Google", "Chrome"))
}

// chromeSafeStorageSecret reads Chrome's cookie-encryption password from the
// login keychain. The value itself is never logged.
func chromeSafeStorageSecret(ctx context.Context) ([]byte, error) {
	out, err := exec.CommandContext(ctx,
		"/usr/bin/security", "find-generic-password", "-w", "-s", "Chrome Safe Storage",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("read Chrome Safe Storage from keychain: %w", err)
	}
	secret := bytes.TrimRight(out, "\n")
	if len(secret) == 0 {
		return nil, fmt.Errorf("Chrome Safe Storage key was empty")
	}
	return secret, nil
}
