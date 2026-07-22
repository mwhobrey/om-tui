//go:build !windows

package cmd

import "syscall"

func setRestrictiveUmask() func() {
	previous := syscall.Umask(0o077)
	return func() { syscall.Umask(previous) }
}
