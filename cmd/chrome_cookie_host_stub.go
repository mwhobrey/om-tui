//go:build !windows

package cmd

import "fmt"

func registerChromeNativeHostWindows(hostName, manifestPath string) error {
	return fmt.Errorf("Windows registry install unavailable on this OS (host %s manifest at %s)", hostName, manifestPath)
}
