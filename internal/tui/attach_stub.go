//go:build !windows

package tui

import "fmt"

func pickMediaFile() (string, error) {
	return "", fmt.Errorf("file picker is only available on Windows")
}

func clipboardMediaPath() (string, bool, error) {
	return "", false, nil
}

func clipboardText() (string, error) {
	return "", nil
}
