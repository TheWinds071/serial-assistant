//go:build windows

package jlink

import (
	"fmt"
	"syscall"

	"github.com/ebitengine/purego"
)

func openLibrary(name string) (uintptr, error) {
	// Windows 下使用 LoadLibrary
	handle, err := syscall.LoadLibrary(name)
	if err != nil {
		return 0, fmt.Errorf("failed to load library %s: %w", name, err)
	}
	return uintptr(handle), nil
}

func closeLibrary(handle uintptr) {
	syscall.FreeLibrary(syscall.Handle(handle))
}

// registerLibFunc wraps purego.RegisterLibFunc for cross-platform compatibility
func registerLibFunc(fptr interface{}, handle uintptr, name string) {
	purego.RegisterLibFunc(fptr, handle, name)
}
