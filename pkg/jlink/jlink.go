package jlink

import (
	"bytes"
	"encoding/binary"
	"fmt"
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
	// 调用平台特定的加载函数 (定义在 jlink_windows.go 或 jlink_unix.go 中)
	lib, err := loadNativeLibrary()
	if err != nil {
		return nil, err
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

	// 2. 尝试绑定原生 RTT 函数 (Linux 上可能没有)
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

	// 强制延时，等待芯片固件初始化 RTT 控制块
	fmt.Println("[JLink] 连接成功，等待芯片稳定...")
	time.Sleep(500 * time.Millisecond)

	// 策略选择
	if jl.apiRTTStart != nil && jl.apiRTTRead != nil {
		fmt.Println("[JLink] 尝试启动原生驱动 RTT...")
		if ret := jl.apiRTTStart(); ret >= 0 {
			fmt.Println("[JLink] 原生 RTT 启动成功")
			jl.useSoftRTT = false
			return nil
		}
		fmt.Println("[JLink] 原生 RTT 启动返回错误，回退到 Soft RTT")
	}

	fmt.Println("[JLink] 原生 RTT 不可用或失败，切换到内存轮询模式 (Soft RTT)")

	// 给 Soft RTT 初始化增加几次重试
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
		if jl.apiRTTRead == nil {
			return nil, nil
		}
		buf := make([]byte, 4096)
		n := jl.apiRTTRead(0, uintptr(unsafe.Pointer(&buf[0])), uint32(len(buf)))
		if n <= 0 {
			return nil, nil
		}
		return buf[:n], nil
	} else {
		return jl.readSoftRTT()
	}
}

// WriteRTT 统一写入接口
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
	} else {
		return 0, nil
	}
}

// Close 关闭
func (jl *JLinkWrapper) Close() {
	if jl.apiClose != nil {
		jl.apiClose()
	}
}

// --- Soft RTT 实现 ---

func (jl *JLinkWrapper) initSoftRTT() error {
	// 搜索范围：0x20000000 开始的 64KB
	searchStart := uint32(0x20000000)
	searchSize := uint32(0x10000)

	chunkSize := uint32(0x800)
	memBuf := make([]byte, chunkSize)
	signature := []byte("SEGGER RTT")

	for offset := uint32(0); offset < searchSize; offset += chunkSize {
		addr := searchStart + offset
		if jl.apiReadMem(addr, chunkSize, uintptr(unsafe.Pointer(&memBuf[0]))) < 0 {
			continue
		}

		idx := bytes.Index(memBuf, signature)
		if idx >= 0 {
			jl.rttControlBlk = addr + uint32(idx)
			fmt.Printf("[JLink] RTT 控制块找到: 0x%08X\n", jl.rttControlBlk)

			descAddr := jl.rttControlBlk + 16 + 4 + 4
			descData := make([]byte, 24)
			if jl.apiReadMem(descAddr, 24, uintptr(unsafe.Pointer(&descData[0]))) < 0 {
				return fmt.Errorf("读取 RTT 描述符失败")
			}

			jl.rttUpBuffer = parseBufferDesc(descData)
			fmt.Printf("[JLink] UpBuffer0: Addr=0x%X Size=%d\n", jl.rttUpBuffer.BufferPtr, jl.rttUpBuffer.Size)

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

	wrOffAddr := jl.rttControlBlk + 24 + 12
	var wrOff uint32
	if jl.apiReadMem(wrOffAddr, 4, uintptr(unsafe.Pointer(&wrOff))) < 0 {
		return nil, nil
	}

	rdOffAddr := jl.rttControlBlk + 24 + 16
	var rdOff uint32
	if jl.apiReadMem(rdOffAddr, 4, uintptr(unsafe.Pointer(&rdOff))) < 0 {
		return nil, nil
	}

	if wrOff == rdOff {
		return nil, nil
	}

	bufSize := jl.rttUpBuffer.Size
	bufBase := jl.rttUpBuffer.BufferPtr

	var data []byte

	if wrOff > rdOff {
		readLen := wrOff - rdOff
		if readLen > 0 {
			chunk := make([]byte, readLen)
			jl.apiReadMem(bufBase+rdOff, readLen, uintptr(unsafe.Pointer(&chunk[0])))
			data = chunk
		}
		rdOff += readLen
	} else {
		len1 := bufSize - rdOff
		if len1 > 0 {
			chunk1 := make([]byte, len1)
			jl.apiReadMem(bufBase+rdOff, len1, uintptr(unsafe.Pointer(&chunk1[0])))
			data = append(data, chunk1...)
		}

		len2 := wrOff
		if len2 > 0 {
			chunk2 := make([]byte, len2)
			jl.apiReadMem(bufBase, len2, uintptr(unsafe.Pointer(&chunk2[0])))
			data = append(data, chunk2...)
		}

		rdOff = wrOff
	}

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
