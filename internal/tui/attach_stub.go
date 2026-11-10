//go:build !windows && !darwin && !linux

package tui

import "fmt"

func pickMediaFile() (string, error) {
	return "", fmt.Errorf("file picker is not available on this platform")
}

func clipboardMediaPath() (string, bool, error) {
	return "", false, nil
}

func clipboardText() (string, error) {
	return "", nil
}
