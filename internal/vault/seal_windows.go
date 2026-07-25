//go:build windows

package vault

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func seal(plaintext []byte) ([]byte, error) {
	var in, out windows.DataBlob
	if len(plaintext) > 0 {
		in.Size = uint32(len(plaintext))
		in.Data = &plaintext[0]
	}
	name, err := windows.UTF16PtrFromString("OpenMessage river credentials")
	if err != nil {
		return nil, err
	}
	if err := windows.CryptProtectData(&in, name, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("vault: DPAPI protect: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	buf := make([]byte, out.Size)
	copy(buf, unsafe.Slice(out.Data, out.Size))
	return buf, nil
}

func open(ciphertext []byte) ([]byte, error) {
	var in, out windows.DataBlob
	if len(ciphertext) > 0 {
		in.Size = uint32(len(ciphertext))
		in.Data = &ciphertext[0]
	}
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("vault: DPAPI unprotect: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	buf := make([]byte, out.Size)
	copy(buf, unsafe.Slice(out.Data, out.Size))
	return buf, nil
}
