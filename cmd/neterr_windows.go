//go:build windows

package cmd

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows"
)

func isConnectionRefused(err error) bool {
	if errors.Is(err, windows.WSAECONNREFUSED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused")
}
