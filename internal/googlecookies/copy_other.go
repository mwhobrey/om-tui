//go:build !windows

package googlecookies

import "os"

func readFileShared(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func fileHeld(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return !os.IsNotExist(err)
	}
	_ = f.Close()
	return false
}
