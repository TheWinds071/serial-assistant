package jlink

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// JLinkWrapper 封装 J-Link API
type JLinkWrapper struct {
	libHandle uintptr

	// 基础 API (Linux/Win 通用)
	apiOpen        func() int
	apiClose       func()
	apiConnect     func() int
	apiTIFSelect   func(int) int
	apiExecCommand func(string, int, int) int
	apiIsConnected func() bool
	apiReadMem     func(uint32, uint32, uintptr) int
	apiWriteMem    func(uint32, uint32, uintptr) int

	// 原生 RTT API (可能不存在)
	apiRTTStart func() int
	apiRTTRead  func(uint32, uintptr, uint32) int
	apiRTTWrite func(uint32, uintptr, uint32) int

	// 软 RTT 状态
	useSoftRTT    bool
	rttControlBlk uint32 // 控制块地址
	rttUpBuffer   RTTBufferDesc
	// rttDownBuffer RTTBufferDesc // 暂时未用到下行
}

// RTTBufferDesc RTT 缓冲区描述符 (对应 C 结构体)
type RTTBufferDesc struct {
	NamePtr   uint32
	BufferPtr uint32
	Size      uint32
	WrOff     uint32
	RdOff     uint32
	Flags     uint32
}

// NewJLinkWrapper 加载驱动并初始化
func NewJLinkWrapper() (*JLinkWrapper, error) {
	libPath, err := getLibraryPath()
	if err != nil {
		return nil, err
	}

	fmt.Printf("[JLink] 尝试加载驱动: %s\n", libPath)

	// 加载动态库
	lib, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		// 尝试加载备用路径（如果是 Linux，可能在系统路径）
		if runtime.GOOS == "linux" && libPath == "./libjlinkarm.so" {
			fmt.Println("[JLink] 本地加载失败，尝试系统默认路径 /opt/SEGGER/JLink/libjlinkarm.so")
			lib, err = purego.Dlopen("/opt/SEGGER/JLink/libjlinkarm.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		}
		if err != nil {
			return nil, fmt.Errorf("加载 J-Link 驱动失败: %v", err)
		}
	}

	jl := &JLinkWrapper{libHandle: lib}

	// 辅助注册函数：忽略返回值，使用 recover 防止 panic
	register := func(dest interface{}, name string) {
		defer func() { recover() }() // 忽略符号未找到的 panic
		purego.RegisterLibFunc(dest, lib, name)
	}

	// 1. 绑定基础函数 (这些必须有)
	register(&jl.apiOpen, "JLINK_Open")
	register(&jl.apiClose, "JLINK_Close")
	register(&jl.apiConnect, "JLINK_Connect")
	register(&jl.apiTIFSelect, "JLINK_TIF_Select")
	register(&jl.apiExecCommand, "JLINK_ExecCommand")
	register(&jl.apiIsConnected, "JLINK_IsConnected")
	register(&jl.apiReadMem, "JLINK_ReadMem")
	register(&jl.apiWriteMem, "JLINK_WriteMem")

	// 2. 尝试绑定原生 RTT 函数 (Linux 上可能没有，或者版本较旧)
	register(&jl.apiRTTStart, "JLINK_RTT_Start")
	register(&jl.apiRTTRead, "JLINK_RTT_Read")
	register(&jl.apiRTTWrite, "JLINK_RTT_Write")

	// 验证核心 API 是否加载成功
	if jl.apiOpen == nil || jl.apiReadMem == nil {
		return nil, fmt.Errorf("J-Link 驱动加载不完整，缺少核心函数")
	}

	return jl, nil
}

// Connect 连接芯片
func (jl *JLinkWrapper) Connect(chipName string, speed int, iface string) error {
	if jl.apiOpen == nil {
		return fmt.Errorf("J-Link API 未初始化")
	}
	jl.apiOpen()

	// 设置接口
	if iface == "JTAG" {
		if jl.apiTIFSelect != nil {
			jl.apiTIFSelect(0)
		}
	} else {
		if jl.apiTIFSelect != nil {
			jl.apiTIFSelect(1)
		} // SWD
	}

	// 设置参数
	if jl.apiExecCommand != nil {
		jl.apiExecCommand(fmt.Sprintf("Speed = %d", speed), 0, 0)
		jl.apiExecCommand(fmt.Sprintf("Device = %s", chipName), 0, 0)
	}

	// 连接
	if jl.apiConnect != nil {
		if ret := jl.apiConnect(); ret < 0 {
			return fmt.Errorf("J-Link 连接失败 (Ret: %d)", ret)
		}
	}

	// --- [关键修复] ---
	// Build 模式下程序运行极快，JLink 复位芯片后，芯片内部固件可能还没运行到初始化 RTT 控制块的代码。
	// 如果立即扫描内存，读到的全是 0，导致“未找到控制块”错误。
	// 这里强制等待 500ms (根据芯片启动速度可适当调整)
	fmt.Println("[JLink] 连接成功，等待芯片稳定...")
	time.Sleep(500 * time.Millisecond)

	// 策略选择：如果原生 RTT API 存在则使用，否则使用软 RTT
	// 注意：在 Linux 下，即使有 apiRTTStart 符号，有时也可能调用失败，建议优先尝试 SoftRTT 或者加更严格的判断
	if jl.apiRTTStart != nil && jl.apiRTTRead != nil {
		fmt.Println("[JLink] 尝试启动原生驱动 RTT...")
		// 尝试启动，如果返回值 < 0 则回退到 SoftRTT
		if ret := jl.apiRTTStart(); ret >= 0 {
			fmt.Println("[JLink] 原生 RTT 启动成功")
			jl.useSoftRTT = false
			return nil
		}
		fmt.Println("[JLink] 原生 RTT 启动返回错误，回退到 Soft RTT")
	}

	fmt.Println("[JLink] 原生 RTT 不可用或失败，切换到内存轮询模式 (Soft RTT)")

	// 给 Soft RTT 初始化增加几次重试，防止芯片启动慢
	var err error
	for i := 0; i < 3; i++ {
		if err = jl.initSoftRTT(); err == nil {
			jl.useSoftRTT = true
			return nil
		}
		fmt.Printf("[JLink] Soft RTT 初始化尝试 %d/3 失败，重试中...\n", i+1)
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("软 RTT 初始化最终失败: %v", err)
}

// ReadRTT 统一读取接口
func (jl *JLinkWrapper) ReadRTT() ([]byte, error) {
	if !jl.useSoftRTT {
		// 原生模式
		if jl.apiRTTRead == nil {
			return nil, nil
		}
		buf := make([]byte, 4096)
		// 注意：某些版本的 .so 文件调用约定可能导致 panic，需注意
		n := jl.apiRTTRead(0, uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)))
		if n <= 0 {
			return nil, nil
		}
		return buf[:n], nil
	} else {
		// 内存轮询模式
		return jl.readSoftRTT()
	}
}

// WriteRTT 统一写入接口
func (jl *JLinkWrapper) WriteRTT(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	if !jl.useSoftRTT {
		// 原生模式
		if jl.apiRTTWrite == nil {
			return 0, nil
		}
		n := jl.apiRTTWrite(0, uintptr(unsafe.Pointer(&data[0])), uint32(len(data)))
		return int(n), nil
	} else {
		// 内存轮询模式 (暂未实现写入，仅实现读取用于日志)
		// TODO: 实现 Soft RTT 写入逻辑 (需要读取 DownBuffer 描述符并写入内存)
		return 0, nil
	}
}

// Close 关闭
func (jl *JLinkWrapper) Close() {
	if jl.apiClose != nil {
		jl.apiClose()
	}
}

// --- Soft RTT 实现 (内存轮询) ---

func (jl *JLinkWrapper) initSoftRTT() error {
	// 1. 搜索 _SEGGER_RTT 控制块
	// 搜索范围：0x20000000 开始的 64KB (通常够用了，范围太大会很慢)
	// 如果你的芯片 RAM 起始地址不同（如 H7 系列），需要修改这里
	searchStart := uint32(0x20000000)
	searchSize := uint32(0x10000) // 64KB

	// 分块搜索，防止一次读取过大内存导致 JLink 报错或超时
	chunkSize := uint32(0x800) // 2KB 一块
	memBuf := make([]byte, chunkSize)
	signature := []byte("SEGGER RTT")

	for offset := uint32(0); offset < searchSize; offset += chunkSize {
		addr := searchStart + offset
		// 读取内存
		if jl.apiReadMem(addr, chunkSize, uintptr(unsafe.Pointer(&memBuf[0]))) < 0 {
			// 读取失败可能是越界或保护，跳过
			continue
		}

		idx := bytes.Index(memBuf, signature)
		if idx >= 0 {
			// 找到了！
			jl.rttControlBlk = addr + uint32(idx)
			fmt.Printf("[JLink] RTT 控制块找到: 0x%08X\n", jl.rttControlBlk)

			// 读取 Up Buffer 0 描述符 (Target -> Host)
			// 结构体偏移: ID(16 bytes) + MaxUpBuffers(4) + MaxDownBuffers(4)
			// UpBuffer[0] 从偏移 24 开始
			descAddr := jl.rttControlBlk + 16 + 4 + 4

			descData := make([]byte, 24) // sizeof(SEGGER_RTT_BUFFER_UP)
			if jl.apiReadMem(descAddr, 24, uintptr(unsafe.Pointer(&descData[0]))) < 0 {
				return fmt.Errorf("读取 RTT 描述符失败")
			}

			jl.rttUpBuffer = parseBufferDesc(descData)
			fmt.Printf("[JLink] UpBuffer0: Addr=0x%X Size=%d\n", jl.rttUpBuffer.BufferPtr, jl.rttUpBuffer.Size)

			// 简单校验一下 Buffer 地址是否合法 (防止误识别)
			if jl.rttUpBuffer.BufferPtr < 0x20000000 || jl.rttUpBuffer.Size > 0x100000 {
				fmt.Println("[JLink] 警告: RTT Buffer 地址看起来不正常，可能识别错误")
			}

			return nil
		}
	}
	return fmt.Errorf("未在 RAM (0x%X - 0x%X) 中找到 SEGGER RTT 控制块", searchStart, searchStart+searchSize)
}

func (jl *JLinkWrapper) readSoftRTT() ([]byte, error) {
	if jl.rttControlBlk == 0 {
		return nil, nil
	}

	// 1. 读取 WrOff (Target 写指针)
	// 结构体: [sNamePtr(4), sBufferPtr(4), Size(4), WrOff(4), RdOff(4), Flags(4)]
	// WrOff 在结构体中的偏移是 12
	// UpBuffer0 描述符在控制块中的偏移是 24 (Header 16 + MaxNum 4*2)
	// 所以 WrOff 的绝对地址 = ControlBlock + 24 + 12 = ControlBlock + 36
	wrOffAddr := jl.rttControlBlk + 24 + 12
	var wrOff uint32
	if jl.apiReadMem(wrOffAddr, 4, uintptr(unsafe.Pointer(&wrOff))) < 0 {
		return nil, nil // 读取失败忽略
	}

	// 读取 RdOff (Host 读指针)
	// RdOff 偏移 = 16
	// 绝对地址 = ControlBlock + 24 + 16 = ControlBlock + 40
	rdOffAddr := jl.rttControlBlk + 24 + 16
	var rdOff uint32
	if jl.apiReadMem(rdOffAddr, 4, uintptr(unsafe.Pointer(&rdOff))) < 0 {
		return nil, nil
	}

	if wrOff == rdOff {
		return nil, nil // 无数据
	}

	bufSize := jl.rttUpBuffer.Size
	bufBase := jl.rttUpBuffer.BufferPtr

	var data []byte

	if wrOff > rdOff {
		// 连续数据: [RdOff ... WrOff]
		readLen := wrOff - rdOff
		if readLen > 0 {
			chunk := make([]byte, readLen)
			jl.apiReadMem(bufBase+rdOff, readLen, uintptr(unsafe.Pointer(&chunk[0])))
			data = chunk
		}
		rdOff += readLen
	} else {
		// 环绕数据: [RdOff ... End] + [Start ... WrOff]
		// 1. 读取 RdOff -> End
		len1 := bufSize - rdOff
		if len1 > 0 {
			chunk1 := make([]byte, len1)
			jl.apiReadMem(bufBase+rdOff, len1, uintptr(unsafe.Pointer(&chunk1[0])))
			data = append(data, chunk1...)
		}

		// 2. 读取 Start -> WrOff
		len2 := wrOff
		if len2 > 0 {
			chunk2 := make([]byte, len2)
			jl.apiReadMem(bufBase, len2, uintptr(unsafe.Pointer(&chunk2[0])))
			data = append(data, chunk2...)
		}

		rdOff = wrOff
	}

	// 更新 RdOff 到目标内存，通知 Target 我们读完了
	jl.apiWriteMem(rdOffAddr, 4, uintptr(unsafe.Pointer(&rdOff)))

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

// getLibraryPath 跨平台路径查找
func getLibraryPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		// 尝试当前目录
		if _, err := os.Stat("JLink_x64.dll"); err == nil {
			return "JLink_x64.dll", nil
		}
		return "JLink_x64.dll", nil // 让系统去搜
	case "linux":
		// [修改] 优先查找当前目录下的 libjlinkarm.so (解决 Build 后找不到库的问题)
		if _, err := os.Stat("./libjlinkarm.so"); err == nil {
			return "./libjlinkarm.so", nil
		}
		// Arch Linux / Ubuntu 默认安装路径
		return "/opt/SEGGER/JLink/libjlinkarm.so", nil
	case "darwin":
		if _, err := os.Stat("libjlinkarm.dylib"); err == nil {
			return "libjlinkarm.dylib", nil
		}
		return "/Applications/SEGGER/JLink/libjlinkarm.dylib", nil
	default:
		return "", fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
}
