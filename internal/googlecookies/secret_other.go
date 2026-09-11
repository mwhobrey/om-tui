//go:build !darwin && !windows

package googlecookies

import (
	"context"
	"fmt"
	"path/filepath"
)

func defaultChromeProfileDir(home string) string {
	return resolveChromeProfile(filepath.Join(home, ".config", "google-chrome"))
}

func chromeSafeStorageSecret(ctx context.Context) ([]byte, error) {
	return nil, fmt.Errorf("native Chrome cookie refresh is not supported on this platform")
}
