package googlecookies

import (
	"os"
	"os/exec"
)

func detachChildStdin(cmd *exec.Cmd) *os.File {
	null, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return nil
	}
	cmd.Stdin = null
	return null
}
