//go:build !windows

package googlecookies

import "path/filepath"

func resolveFinalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
