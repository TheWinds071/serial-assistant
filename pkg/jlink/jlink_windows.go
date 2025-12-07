//go:build windows

package jlink

import (
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

// Windows 下使用 LoadLibrary
func loadNativeLibrary() (uintptr, error) {
	libPath := "JLink_x64.dll"

	// 尝试优先加载当前目录下的 DLL (解决开发/生产环境路径问题)
	if _, err := os.Stat(libPath); err == nil {
		fmt.Printf("[JLink] 加载本地 DLL: %s\n", libPath)
	} else {
		// 如果本地没有，让系统去 PATH 环境变量里找
		fmt.Println("[JLink] 本地未找到 DLL，尝试从系统 PATH 加载...")
	}

	// Windows 使用 LoadLibrary
	lib, err := purego.LoadLibrary(libPath)
	if err != nil {
		return 0, fmt.Errorf("加载 JLink_x64.dll 失败: %v", err)
	}
	return lib, nil
}
