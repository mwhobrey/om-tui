//go:build windows

package cmd

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func registerChromeNativeHostWindows(hostName, manifestPath string) error {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		return err
	}
	keyPath := `Software\Google\Chrome\NativeMessagingHosts\` + hostName
	key, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create Chrome native messaging registry key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue("", abs); err != nil {
		return fmt.Errorf("set Chrome native messaging registry value: %w", err)
	}
	return nil
}
