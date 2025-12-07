//go:build !windows

package jlink

import (
	"fmt"
	"os"
	"runtime"

	"github.com/ebitengine/purego"
)

// Linux/macOS 下使用 Dlopen
func loadNativeLibrary() (uintptr, error) {
	var libPath string

	if runtime.GOOS == "darwin" {
		libPath = "libjlinkarm.dylib"
		if _, err := os.Stat(libPath); err != nil {
			// Mac 默认路径
			libPath = "/Applications/SEGGER/JLink/libjlinkarm.dylib"
		}
	} else {
		// Linux
		libPath = "./libjlinkarm.so"
	}

	fmt.Printf("[JLink] 尝试加载驱动: %s\n", libPath)

	// Unix 使用 Dlopen
	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		// Linux 备用路径重试
		if runtime.GOOS == "linux" {
			fmt.Println("[JLink] 本地加载失败，尝试系统默认路径 /opt/SEGGER/JLink/libjlinkarm.so")
			lib, err = purego.Dlopen("/opt/SEGGER/JLink/libjlinkarm.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		}
		if err != nil {
			return 0, fmt.Errorf("加载 J-Link 驱动失败: %v", err)
		}
	}
	return lib, nil
}
