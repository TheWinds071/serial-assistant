//go:build !windows

package jlink

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// openLibrary 是我们自己定义的跨平台接口
// 在 Unix (Linux/macOS) 下，它调用 purego.Dlopen
func openLibrary(name string) (uintptr, error) {
	// RTLD_NOW 和 RTLD_GLOBAL 是 purego 在 Unix 平台下导出的常量
	// 在 Windows 编译环境下，purego 包里没有这些常量，所以必须放在这个带 build tag 的文件中
	handle, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return 0, fmt.Errorf("failed to load library %s: %w", name, err)
	}
	return handle, nil
}

func closeLibrary(handle uintptr) {
	purego.Dlclose(handle)
}

// registerLibFunc wraps purego.RegisterLibFunc for cross-platform compatibility
func registerLibFunc(fptr interface{}, handle uintptr, name string) {
	purego.RegisterLibFunc(fptr, handle, name)
}
