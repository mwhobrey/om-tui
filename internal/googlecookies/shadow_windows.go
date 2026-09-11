//go:build windows

package googlecookies

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// shadowUserDataDir makes a directory junction to src so Chrome sees a
// non-default --user-data-dir path. Remote debugging is refused on the real
// default User Data folder. Cleanup removes only the junction.
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
	out, err := exec.Command("cmd", "/c", "mklink", "/J", dst, abs).CombinedOutput()
	if err != nil {
		_ = os.Remove(parent)
		return "", nil, fmt.Errorf("link Chrome profile: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	cleanup := func() {
		if err := os.Remove(dst); err != nil {
			return
		}
		_ = os.Remove(parent)
	}
	return dst, cleanup, nil
}
