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

// JLinkWrapper 封装 J-Link API
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

// NewJLinkWrapper 加载驱动并初始化
func NewJLinkWrapper() (*JLinkWrapper, error) {
	libPath, err := getLibraryPath()
	if err != nil {
		return nil, err
	}

	fmt.Printf("[JLink] Loading library: %s\n", libPath)

	// [修复关键点]
	// 这里直接调用我们自己在 loader_*.go 中定义的 openLibrary
	// 不再直接调用 purego.Dlopen，从而避免了 Windows 下的 undefined 错误
	lib, err := openLibrary(libPath)
	if err != nil {
		// Linux 备用路径逻辑也改用 openLibrary
		if runtime.GOOS == "linux" && libPath == "./libjlinkarm.so" {
			fmt.Println("[JLink] Local load failed, trying /opt/SEGGER/JLink/libjlinkarm.so")
			lib, err = openLibrary("/opt/SEGGER/JLink/libjlinkarm.so")
		}
		if err != nil {
			return nil, err
		}
	}

	jl := &JLinkWrapper{libHandle: lib}

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
		return nil, fmt.Errorf("J-Link library loaded but missing core functions")
	}

	return jl, nil
}

// Connect 连接芯片
func (jl *JLinkWrapper) Connect(chipName string, speed int, iface string) error {
	if jl.apiOpen == nil {
		return fmt.Errorf("J-Link API not initialized")
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
			return fmt.Errorf("J-Link Connect failed (Ret: %d)", ret)
		}
	}

	fmt.Println("[JLink] Connected, waiting for chip stabilization...")
	time.Sleep(500 * time.Millisecond)

	if jl.apiRTTStart != nil && jl.apiRTTRead != nil {
		fmt.Println("[JLink] Trying native RTT...")
		if ret := jl.apiRTTStart(); ret >= 0 {
			fmt.Println("[JLink] Native RTT started")
			jl.useSoftRTT = false
			return nil
		}
	}

	fmt.Println("[JLink] Native RTT unavailable, switching to Soft RTT")
	var err error
	for i := 0; i < 3; i++ {
		if err = jl.initSoftRTT(); err == nil {
			jl.useSoftRTT = true
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Soft RTT init failed: %v", err)
}

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

	for offset := uint32(0); offset < searchSize; offset += chunkSize {
		addr := searchStart + offset
		if jl.apiReadMem(addr, chunkSize, uintptr(unsafe.Pointer(&memBuf[0]))) < 0 {
			continue
		}
		idx := bytes.Index(memBuf, signature)
		if idx >= 0 {
			jl.rttControlBlk = addr + uint32(idx)
			descAddr := jl.rttControlBlk + 16 + 4 + 4
			descData := make([]byte, 24)
			if jl.apiReadMem(descAddr, 24, uintptr(unsafe.Pointer(&descData[0]))) < 0 {
				return fmt.Errorf("Read RTT descriptor failed")
			}
			jl.rttUpBuffer = parseBufferDesc(descData)
			return nil
		}
	}
	return fmt.Errorf("SEGGER RTT Control Block not found")
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

	bufBase := jl.rttUpBuffer.BufferPtr
	bufSize := jl.rttUpBuffer.Size
	var data []byte

	if wrOff > rdOff {
		readLen := wrOff - rdOff
		chunk := make([]byte, readLen)
		jl.apiReadMem(bufBase+rdOff, readLen, uintptr(unsafe.Pointer(&chunk[0])))
		data = chunk
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
