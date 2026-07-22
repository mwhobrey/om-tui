//go:build windows

package cmd

func setRestrictiveUmask() func() {
	return func() {}
}
