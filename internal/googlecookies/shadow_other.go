//go:build !windows

package googlecookies

import (
	"os"
	"path/filepath"
)

func shadowUserDataDir(src string) (string, func(), error) {
	abs, err := filepath.Abs(src)
	if err != nil {
		return "", nil, err
	}
	parent, err := os.MkdirTemp("", "om-chrome-live-")
	if err != nil {
		return "", nil, err
	}
	dst := filepath.Join(parent, "User Data")
	if err := os.Symlink(abs, dst); err != nil {
		_ = os.RemoveAll(parent)
		return "", nil, err
	}
	cleanup := func() {
		if err := os.Remove(dst); err != nil {
			return
		}
		_ = os.Remove(parent)
	}
	return dst, cleanup, nil
}
