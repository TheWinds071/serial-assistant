package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.bug.st/serial"
)

// App struct
type App struct {
	ctx          context.Context
	port         serial.Port
	isConnected  bool
	mutex        sync.Mutex
	readStopChan chan struct{}
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// 1. 获取串口列表
func (a *App) GetSerialPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		return []string{}, nil
	}
	return ports, nil
}

// OpenSerial 打开串口 (支持完整参数)
func (a *App) OpenSerial(portName string, baudRate int, dataBits int, stopBits int, parityName string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.isConnected {
		return "Port already open"
	}

	// 1. 映射校验位
	var parity serial.Parity
	switch parityName {
		case "None":
			parity = serial.NoParity
		case "Odd":
			parity = serial.OddParity
		case "Even":
			parity = serial.EvenParity
		case "Mark":
			parity = serial.MarkParity
		case "Space":
			parity = serial.SpaceParity
		default:
			parity = serial.NoParity
	}

	// 2. 映射停止位 (前端传 1, 15(代表1.5), 2)
	var stop serial.StopBits
	switch stopBits {
		case 1:
			stop = serial.OneStopBit
		case 15:
			stop = serial.OnePointFiveStopBits
		case 2:
			stop = serial.TwoStopBits
		default:
			stop = serial.OneStopBit
	}

	// 3. 配置 Mode
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: dataBits,
		Parity:   parity,
		StopBits: stop,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	a.port = port
	a.isConnected = true
	a.readStopChan = make(chan struct{})

	go a.readLoop()

	return "Success"
}

// 3. 读取循环 (将数据推送给前端)
func (a *App) readLoop() {
	buff := make([]byte, 100)
	for {
		select {
			case <-a.readStopChan:
				return
			default:
				n, err := a.port.Read(buff)
				if err != nil {
					// 处理错误或断开连接
					if a.isConnected {
						runtime.EventsEmit(a.ctx, "serial-error", err.Error())
						a.CloseSerial()
					}
					return
				}
				if n == 0 {
					continue
				}
				// 发送原始字节数据到前端 (前端处理 Hex/ASCII 显示)
				// 注意：为了传输方便，这里转为 byte slice
				runtime.EventsEmit(a.ctx, "serial-data", buff[:n])
		}
	}
}

// 4. 关闭串口
func (a *App) CloseSerial() string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.isConnected {
		return "Port not open"
	}

	close(a.readStopChan) // 停止读取协程
	err := a.port.Close()
	a.isConnected = false
	a.port = nil

	if err != nil {
		return fmt.Sprintf("Error closing: %v", err)
	}
	return "Closed"
}

// 5. 发送数据
func (a *App) SendData(data string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.isConnected {
		return "Error: Port not connected"
	}

	// 这里简化处理，直接发送字符串。如果是Hex发送，前端需先解析为字节数组传过来，
	// 或者在这里将 HexString 转为 []byte
	_, err := a.port.Write([]byte(data))
	if err != nil {
		return fmt.Sprintf("Send error: %v", err)
	}
	return "Sent"
}
