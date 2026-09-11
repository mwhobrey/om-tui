//go:build windows

package googlecookies

import (
	"strings"

	"golang.org/x/sys/windows"
)

func resolveFinalPath(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		p,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	var buf [4096]uint16
	n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || int(n) > len(buf) {
		return "", windows.ERROR_INSUFFICIENT_BUFFER
	}
	out := windows.UTF16ToString(buf[:n])
	out = strings.TrimPrefix(out, `\\?\`)
	out = strings.TrimPrefix(out, `\??\`)
	return out, nil
}
