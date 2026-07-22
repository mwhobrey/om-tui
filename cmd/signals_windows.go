//go:build windows

package cmd

import "os"

func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
