package jlink

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"
)

// LogCallback 日志回调函数类型
type LogCallback func(message string)

// JLinkWrapper 封装 RTT API
type JLinkWrapper struct {
	libHandle uintptr

	// 基础 API
	apiOpen        func() int
	apiClose       func()
	apiConnect     func() int
	apiTIFSelect   func(int) int
	apiExecCommand func(string, int, int) int
	apiIsConnected func() bool
	apiReadMem     func(uint32, uint32, uintptr) int
	apiWriteMem    func(uint32, uint32, uintptr) int

	// RTT API
	apiRTTStart func() int
	apiRTTRead  func(uint32, uintptr, uint32) int
	apiRTTWrite func(uint32, uintptr, uint32) int

	// 软 RTT 状态
	useSoftRTT    bool
	rttControlBlk uint32
	rttUpBuffer   RTTBufferDesc

	// 日志回调
	logCallback LogCallback

	// 读取缓冲区重用（避免频繁分配）
	readBuffer []byte
}

// RTTBufferDesc RTT 缓冲区描述符
type RTTBufferDesc struct {
	NamePtr   uint32
	BufferPtr uint32
	Size      uint32
	WrOff     uint32
	RdOff     uint32
	Flags     uint32
}

// RTT 读取限制常量
const (
	// maxRTTReadSize 限制单次 RTT 读取的最大字节数，防止在连接中断或
	// 状态损坏时分配过大的内存缓冲区（例如当偏移量被损坏为极大值时）
	maxRTTReadSize = 64 * 1024 // 64KB
)

// NewJLinkWrapper 加载驱动并初始化
func NewJLinkWrapper(logCallback LogCallback) (*JLinkWrapper, error) {
	libPath, err := getLibraryPath()
	if err != nil {
		return nil, err
	}

	if logCallback != nil {
		logCallback(fmt.Sprintf("[RTT] 正在加载库: %s", libPath))
	}

	// [修复关键点]
	// 这里直接调用我们自己在 loader_*.go 中定义的 openLibrary
	// 不再直接调用 purego.Dlopen，从而避免了 Windows 下的 undefined 错误
	lib, err := openLibrary(libPath)
	if err != nil {
		// Linux 备用路径逻辑也改用 openLibrary
		if runtime.GOOS == "linux" && libPath == "./libjlinkarm.so" {
			if logCallback != nil {
				logCallback("[RTT] 本地加载失败，尝试 /opt/SEGGER/JLink/libjlinkarm.so")
			}
			lib, err = openLibrary("/opt/SEGGER/JLink/libjlinkarm.so")
		}
		if err != nil {
			return nil, err
		}
	}

	jl := &JLinkWrapper{
		libHandle:   lib,
		logCallback: logCallback,
		readBuffer:  make([]byte, 4096), // 预分配读取缓冲区
	}

	// 注册函数 - registerLibFunc 是跨平台的，可以在这里安全使用
	register := func(dest interface{}, name string) {
		defer func() { recover() }()
		registerLibFunc(dest, lib, name)
	}

	register(&jl.apiOpen, "JLINK_Open")
	register(&jl.apiClose, "JLINK_Close")
	register(&jl.apiConnect, "JLINK_Connect")
	register(&jl.apiTIFSelect, "JLINK_TIF_Select")
	register(&jl.apiExecCommand, "JLINK_ExecCommand")
	register(&jl.apiIsConnected, "JLINK_IsConnected")
	register(&jl.apiReadMem, "JLINK_ReadMem")
	register(&jl.apiWriteMem, "JLINK_WriteMem")
	register(&jl.apiRTTStart, "JLINK_RTT_Start")
	register(&jl.apiRTTRead, "JLINK_RTT_Read")
	register(&jl.apiRTTWrite, "JLINK_RTT_Write")

	if jl.apiOpen == nil || jl.apiReadMem == nil {
		return nil, fmt.Errorf("RTT 库已加载但缺少核心函数")
	}

	return jl, nil
}

// log 内部日志辅助函数
func (jl *JLinkWrapper) log(message string) {
	if jl.logCallback != nil {
		jl.logCallback(message)
	}
}

// Connect 连接芯片
func (jl *JLinkWrapper) Connect(chipName string, speed int, iface string) error {
	if jl.apiOpen == nil {
		return fmt.Errorf("RTT API 未初始化")
	}
	jl.apiOpen()

	if iface == "JTAG" {
		if jl.apiTIFSelect != nil {
			jl.apiTIFSelect(0)
		}
	} else {
		if jl.apiTIFSelect != nil {
			jl.apiTIFSelect(1)
		}
	}

	if jl.apiExecCommand != nil {
		jl.apiExecCommand(fmt.Sprintf("Speed = %d", speed), 0, 0)
		jl.apiExecCommand(fmt.Sprintf("Device = %s", chipName), 0, 0)
	}

	if jl.apiConnect != nil {
		if ret := jl.apiConnect(); ret < 0 {
			return fmt.Errorf("RTT 连接失败 (返回值: %d)", ret)
		}
	}

	jl.log("[RTT] 已连接，等待芯片稳定...")
	time.Sleep(500 * time.Millisecond)

	if jl.apiRTTStart != nil && jl.apiRTTRead != nil {
		jl.log("[RTT] 尝试启动原生 RTT...")
		if ret := jl.apiRTTStart(); ret >= 0 {
			jl.log("[RTT] 原生 RTT 已启动")
			jl.useSoftRTT = false
			return nil
		}
	}

	jl.log("[RTT] 原生 RTT 不可用，切换到软件 RTT")
	var err error
	for i := 0; i < 3; i++ {
		if err = jl.initSoftRTT(); err == nil {
			jl.useSoftRTT = true
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("软件 RTT 初始化失败: %v", err)
}

func (jl *JLinkWrapper) ReadRTT() ([]byte, error) {
	if !jl.useSoftRTT {
		if jl.apiRTTRead == nil {
			return nil, nil
		}
		// 重用预分配的缓冲区，避免每次调用都分配内存
		n := jl.apiRTTRead(0, uintptr(unsafe.Pointer(&jl.readBuffer[0])), uint32(len(jl.readBuffer)))
		if n <= 0 {
			return nil, nil
		}
		// 返回数据的副本，保护内部缓冲区
		result := make([]byte, n)
		copy(result, jl.readBuffer[:n])
		return result, nil
	}
	return jl.readSoftRTT()
}

func (jl *JLinkWrapper) WriteRTT(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if !jl.useSoftRTT {
		if jl.apiRTTWrite == nil {
			return 0, nil
		}
		n := jl.apiRTTWrite(0, uintptr(unsafe.Pointer(&data[0])), uint32(len(data)))
		return int(n), nil
	}
	// Soft RTT Write not implemented yet
	return 0, nil
}

func (jl *JLinkWrapper) Close() {
	if jl.apiClose != nil {
		jl.apiClose()
	}
	// 使用我们定义的 closeLibrary
	closeLibrary(jl.libHandle)
}

// --- Soft RTT Logic ---

func (jl *JLinkWrapper) initSoftRTT() error {
	searchStart := uint32(0x20000000)
	searchSize := uint32(0x10000)
	chunkSize := uint32(0x800)
	memBuf := make([]byte, chunkSize)
	signature := []byte("SEGGER RTT")

	jl.log("[RTT] 搜索 RTT 控制块...")
	for offset := uint32(0); offset < searchSize; offset += chunkSize {
		addr := searchStart + offset
		if jl.apiReadMem(addr, chunkSize, uintptr(unsafe.Pointer(&memBuf[0]))) < 0 {
			continue
		}
		idx := bytes.Index(memBuf, signature)
		if idx >= 0 {
			jl.rttControlBlk = addr + uint32(idx)
			jl.log(fmt.Sprintf("[RTT] 找到 RTT 控制块 @ 0x%08X", jl.rttControlBlk))
			descAddr := jl.rttControlBlk + 16 + 4 + 4
			descData := make([]byte, 24)
			if jl.apiReadMem(descAddr, 24, uintptr(unsafe.Pointer(&descData[0]))) < 0 {
				return fmt.Errorf("读取 RTT 描述符失败")
			}
			jl.rttUpBuffer = parseBufferDesc(descData)
			jl.log("[RTT] 软件 RTT 初始化成功")
			return nil
		}
	}
	return fmt.Errorf("未找到 SEGGER RTT 控制块")
}

func (jl *JLinkWrapper) readSoftRTT() ([]byte, error) {
	if jl.rttControlBlk == 0 {
		return nil, nil
	}
	wrOffAddr := jl.rttControlBlk + 24 + 12
	var wrOff uint32
	if jl.apiReadMem(wrOffAddr, 4, uintptr(unsafe.Pointer(&wrOff))) < 0 {
		return nil, fmt.Errorf("failed to read write offset")
	}
	rdOffAddr := jl.rttControlBlk + 24 + 16
	var rdOff uint32
	if jl.apiReadMem(rdOffAddr, 4, uintptr(unsafe.Pointer(&rdOff))) < 0 {
		return nil, fmt.Errorf("failed to read read offset")
	}

	bufBase := jl.rttUpBuffer.BufferPtr
	bufSize := jl.rttUpBuffer.Size

	// 关键修复：验证偏移量是否在有效范围内
	// 如果连接中断或状态损坏，偏移量可能变得异常大
	if wrOff >= bufSize || rdOff >= bufSize {
		jl.log(fmt.Sprintf("[RTT] 错误：偏移量超出范围 (wrOff=%d, rdOff=%d, bufSize=%d)", wrOff, rdOff, bufSize))
		return nil, fmt.Errorf("RTT offset out of bounds: wrOff=%d, rdOff=%d, bufSize=%d", wrOff, rdOff, bufSize)
	}

	if wrOff == rdOff {
		return nil, nil
	}

	var data []byte

	if wrOff > rdOff {
		readLen := wrOff - rdOff
		// 关键修复：限制读取长度，防止分配过大内存
		if readLen > maxRTTReadSize {
			jl.log(fmt.Sprintf("[RTT] 警告：读取长度过大 (%d bytes)，限制为 %d bytes", readLen, maxRTTReadSize))
			readLen = maxRTTReadSize
		}
		chunk := make([]byte, readLen)
		if jl.apiReadMem(bufBase+rdOff, readLen, uintptr(unsafe.Pointer(&chunk[0]))) < 0 {
			return nil, fmt.Errorf("failed to read RTT data")
		}
		data = chunk
		rdOff += readLen
	} else {
		// 环形缓冲区回绕情况
		len1 := bufSize - rdOff
		len2 := wrOff
		totalLen := len1 + len2

		// 关键修复：检查总读取长度
		if totalLen > maxRTTReadSize {
			jl.log(fmt.Sprintf("[RTT] 警告：总读取长度过大 (%d bytes)，限制为 %d bytes", totalLen, maxRTTReadSize))
			// 优先读取缓冲区末尾的数据
			if len1 > maxRTTReadSize {
				len1 = maxRTTReadSize
				len2 = 0
			} else {
				len2 = maxRTTReadSize - len1
			}
		}

		if len1 > 0 {
			chunk1 := make([]byte, len1)
			if jl.apiReadMem(bufBase+rdOff, len1, uintptr(unsafe.Pointer(&chunk1[0]))) < 0 {
				return nil, fmt.Errorf("failed to read RTT data (segment 1)")
			}
			data = append(data, chunk1...)
		}
		if len2 > 0 {
			chunk2 := make([]byte, len2)
			if jl.apiReadMem(bufBase, len2, uintptr(unsafe.Pointer(&chunk2[0]))) < 0 {
				return nil, fmt.Errorf("failed to read RTT data (segment 2)")
			}
			data = append(data, chunk2...)
		}
		// 更新读偏移量：len1 和 len2 是实际读取的长度（已考虑截断）
		// 使用模运算确保在环形缓冲区中正确回绕
		rdOff = (rdOff + len1 + len2) % bufSize
	}

	// 写回更新的读偏移量
	if jl.apiWriteMem(rdOffAddr, 4, uintptr(unsafe.Pointer(&rdOff))) < 0 {
		jl.log("[RTT] 警告：无法更新读偏移量")
	}
	return data, nil
}

func parseBufferDesc(data []byte) RTTBufferDesc {
	return RTTBufferDesc{
		NamePtr:   binary.LittleEndian.Uint32(data[0:4]),
		BufferPtr: binary.LittleEndian.Uint32(data[4:8]),
		Size:      binary.LittleEndian.Uint32(data[8:12]),
		WrOff:     binary.LittleEndian.Uint32(data[12:16]),
		RdOff:     binary.LittleEndian.Uint32(data[16:20]),
		Flags:     binary.LittleEndian.Uint32(data[20:24]),
	}
}

// ReinitSoftRTT attempts to reinitialize software RTT (used to recover connection after STM32 reset)
func (jl *JLinkWrapper) ReinitSoftRTT() error {
	if !jl.useSoftRTT {
		return fmt.Errorf("not using soft RTT")
	}
	jl.log("[RTT] 检测到偏移量异常，尝试重新初始化 RTT...")
	return jl.initSoftRTT()
}

// getLibraryPath 跨平台路径选择
func getLibraryPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if _, err := os.Stat("JLink_x64.dll"); err == nil {
			return "JLink_x64.dll", nil
		}
		return "JLink_x64.dll", nil
	case "linux":
		if _, err := os.Stat("./libjlinkarm.so"); err == nil {
			return "./libjlinkarm.so", nil
		}
		return "/opt/SEGGER/JLink/libjlinkarm.so", nil
	case "darwin":
		if _, err := os.Stat("libjlinkarm.dylib"); err == nil {
			return "libjlinkarm.dylib", nil
		}
		return "/Applications/SEGGER/JLink/libjlinkarm.dylib", nil
	default:
		return "", fmt.Errorf("Unsupported OS: %s", runtime.GOOS)
	}
}
