package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"serial-assistant/pkg/jlink"   // 引入刚才创建的包
	"serial-assistant/pkg/updater" // 引入更新模块

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.bug.st/serial"
)

// ConnectionType 定义连接类型
type ConnectionType string

const (
	TypeSerial    ConnectionType = "SERIAL"
	TypeTcpClient ConnectionType = "TCP_CLIENT"
	TypeTcpServer ConnectionType = "TCP_SERVER"
	TypeUdp       ConnectionType = "UDP"
	TypeJLink     ConnectionType = "JLINK" // 新增 JLink 类型
)

// App struct
type App struct {
	ctx          context.Context
	mutex        sync.Mutex
	connType     ConnectionType
	isConnected  bool
	readStopChan chan struct{}

	// 串口资源
	serialPort serial.Port

	// 网络资源
	netConn     net.Conn       // 用于 TCP Client, active TCP Server conn
	netListener net.Listener   // 用于 TCP Server
	udpConn     net.PacketConn // 用于 UDP
	udpRemote   net.Addr       // UDP 远程地址 (用于发送)

	// RTT 资源
	jlinkConn *jlink.JLinkWrapper
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

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

// --- 连接逻辑封装 ---

// OpenSerial 打开串口
func (a *App) OpenSerial(portName string, baudRate int, dataBits int, stopBits int, parityName string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.isConnected {
		return "Already connected"
	}

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

	port.SetMode(mode)
	port.SetDTR(true)
	port.SetRTS(true)

	a.serialPort = port
	a.connType = TypeSerial
	a.startReadLoop(port) // 启动通用读取循环

	return "Success"
}

// OpenJLink 连接 RTT
func (a *App) OpenJLink(chip string, speed int, iface string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.isConnected {
		return "Already connected"
	}

	// 定义日志回调函数，将日志发送到前端 RX Monitor
	logCallback := func(message string) {
		// 将日志消息作为字符串发送到前端
		logData := []byte(message + "\n")
		runtime.EventsEmit(a.ctx, "serial-data", logData)
	}

	// 1. 加载驱动
	jl, err := jlink.NewJLinkWrapper(logCallback)
	if err != nil {
		return err.Error()
	}

	// 2. 连接芯片
	err = jl.Connect(chip, speed, iface)
	if err != nil {
		// 连接失败需要释放资源
		jl.Close()
		return err.Error()
	}

	a.jlinkConn = jl
	a.connType = TypeJLink
	a.isConnected = true
	a.readStopChan = make(chan struct{})

	// 3. 启动 RTT 专用读取循环 (因为它的 API 不是 io.Reader 风格，而是轮询)
	go a.jlinkReadLoop()

	return "Success"
}

// jlinkReadLoop 专用的 RTT 轮询循环
func (a *App) jlinkReadLoop() {
	ticker := time.NewTicker(10 * time.Millisecond) // 10ms 轮询一次
	defer ticker.Stop()

	for {
		select {
		case <-a.readStopChan:
			return
		case <-ticker.C:
			// 检查连接是否还在 (需要加锁读取 jlinkConn，或者假设 stopChan 会处理)
			// 注意：这里为了性能，简单处理，如果 closed 会置为 nil，所以要小心
			a.mutex.Lock()
			jl := a.jlinkConn
			a.mutex.Unlock()

			if jl == nil {
				return
			}

			data, err := jl.ReadRTT()
			if err != nil {
				// 读取错误通常意味着掉线
				runtime.EventsEmit(a.ctx, "serial-error", fmt.Sprintf("RTT Error: %v", err))
				a.Close()
				return
			}

			if len(data) > 0 {
				runtime.EventsEmit(a.ctx, "serial-data", data)
			}
		}
	}
}

// OpenTcpClient 连接 TCP 服务端
func (a *App) OpenTcpClient(ip string, port string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.isConnected {
		return "Already connected"
	}

	address := net.JoinHostPort(ip, port)
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return fmt.Sprintf("Connect error: %v", err)
	}

	a.netConn = conn
	a.connType = TypeTcpClient
	a.startReadLoop(conn)

	return "Success"
}

// OpenTcpServer 开启 TCP 服务端
func (a *App) OpenTcpServer(port string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.isConnected {
		return "Already connected"
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Sprintf("Listen error: %v", err)
	}

	a.netListener = listener
	a.connType = TypeTcpServer
	a.isConnected = true
	a.readStopChan = make(chan struct{})

	go func() {
		for {
			select {
			case <-a.readStopChan:
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					return
				}

				a.mutex.Lock()
				if a.netConn != nil {
					a.netConn.Close()
				}
				a.netConn = conn
				a.mutex.Unlock()

				runtime.EventsEmit(a.ctx, "sys-msg", fmt.Sprintf("Client connected: %s", conn.RemoteAddr().String()))
				go a.handleTcpConnection(conn)
			}
		}
	}()

	return "Success"
}

func (a *App) handleTcpConnection(conn net.Conn) {
	buff := make([]byte, 4096)
	for {
		n, err := conn.Read(buff)
		if err != nil {
			a.mutex.Lock()
			if a.netConn == conn {
				a.netConn = nil
			}
			a.mutex.Unlock()
			return
		}
		if n > 0 {
			dataToSend := make([]byte, n)
			copy(dataToSend, buff[:n])
			runtime.EventsEmit(a.ctx, "serial-data", dataToSend)
		}
	}
}

// OpenUdp 开启 UDP
func (a *App) OpenUdp(localPort string, remoteIp string, remotePort string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.isConnected {
		return "Already connected"
	}

	lAddrStr := ":" + localPort
	conn, err := net.ListenPacket("udp", lAddrStr)
	if err != nil {
		return fmt.Sprintf("UDP Listen error: %v", err)
	}

	var rAddr net.Addr
	if remoteIp != "" && remotePort != "" {
		rAddr, err = net.ResolveUDPAddr("udp", net.JoinHostPort(remoteIp, remotePort))
		if err != nil {
			conn.Close()
			return fmt.Sprintf("Remote Addr error: %v", err)
		}
	}

	a.udpConn = conn
	a.udpRemote = rAddr
	a.connType = TypeUdp
	a.isConnected = true
	a.readStopChan = make(chan struct{})

	go func() {
		buff := make([]byte, 4096)
		for {
			select {
			case <-a.readStopChan:
				return
			default:
				conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				n, addr, err := conn.ReadFrom(buff)
				if err != nil {
					if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
						continue
					}
					if a.isConnected {
						runtime.EventsEmit(a.ctx, "serial-error", err.Error())
					}
					return
				}

				a.mutex.Lock()
				if a.udpRemote == nil {
					a.udpRemote = addr
					runtime.EventsEmit(a.ctx, "sys-msg", fmt.Sprintf("Remote set to: %s", addr.String()))
				}
				a.mutex.Unlock()

				if n > 0 {
					dataToSend := make([]byte, n)
					copy(dataToSend, buff[:n])
					runtime.EventsEmit(a.ctx, "serial-data", dataToSend)
				}
			}
		}
	}()

	return "Success"
}

// --- 通用方法 ---

func (a *App) startReadLoop(reader io.Reader) {
	a.isConnected = true
	a.readStopChan = make(chan struct{})

	go func() {
		buff := make([]byte, 4096)
		for {
			select {
			case <-a.readStopChan:
				return
			default:
				n, err := reader.Read(buff)
				if err != nil {
					if a.isConnected {
						fmt.Printf("Read Error: %v\n", err)
						runtime.EventsEmit(a.ctx, "serial-error", err.Error())
						a.Close()
					}
					return
				}
				if n == 0 {
					continue
				}

				fmt.Printf("[DEBUG] Recv %d bytes\n", n)
				dataToSend := make([]byte, n)
				copy(dataToSend, buff[:n])
				runtime.EventsEmit(a.ctx, "serial-data", dataToSend)
			}
		}
	}()
}

// Close 关闭连接
func (a *App) Close() string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.isConnected {
		return "Not connected"
	}

	a.isConnected = false
	if a.readStopChan != nil {
		close(a.readStopChan)
	}

	var err error

	switch a.connType {
	case TypeSerial:
		if a.serialPort != nil {
			err = a.serialPort.Close()
			a.serialPort = nil
		}
	case TypeJLink:
		if a.jlinkConn != nil {
			a.jlinkConn.Close()
			a.jlinkConn = nil
		}
	case TypeTcpClient:
		if a.netConn != nil {
			err = a.netConn.Close()
			a.netConn = nil
		}
	case TypeTcpServer:
		if a.netListener != nil {
			err = a.netListener.Close()
			a.netListener = nil
		}
		if a.netConn != nil {
			a.netConn.Close()
			a.netConn = nil
		}
	case TypeUdp:
		if a.udpConn != nil {
			err = a.udpConn.Close()
			a.udpConn = nil
			a.udpRemote = nil
		}
	}

	if err != nil {
		return fmt.Sprintf("Error closing: %v", err)
	}
	return "Success"
}

// SendData 发送数据
func (a *App) SendData(data string) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.isConnected {
		return "Error: Not connected"
	}

	payload := []byte(data)
	var err error

	switch a.connType {
	case TypeSerial:
		if a.serialPort != nil {
			_, err = a.serialPort.Write(payload)
		}
	case TypeJLink:
		if a.jlinkConn != nil {
			_, err = a.jlinkConn.WriteRTT(payload)
		}
	case TypeTcpClient, TypeTcpServer:
		if a.netConn != nil {
			_, err = a.netConn.Write(payload)
		} else if a.connType == TypeTcpServer {
			return "Error: No client connected"
		}
	case TypeUdp:
		if a.udpConn != nil && a.udpRemote != nil {
			_, err = a.udpConn.WriteTo(payload, a.udpRemote)
		} else {
			return "Error: No remote address set"
		}
	}

	if err != nil {
		return fmt.Sprintf("Send error: %v", err)
	}
	return "Sent"
}

// --- Update Methods ---

// GetVersion returns the current application version
func (a *App) GetVersion() string {
	return Version
}

// CheckForUpdates checks if a new version is available
func (a *App) CheckForUpdates() (updater.UpdateInfo, error) {
	info, err := updater.CheckForUpdates(Version)
	if err != nil {
		return updater.UpdateInfo{}, err
	}
	return *info, nil
}

// DownloadAndInstallUpdate downloads and installs the update
func (a *App) DownloadAndInstallUpdate(downloadURL string) error {
	// Download with progress reporting
	tempFile, err := updater.DownloadUpdate(downloadURL, func(downloaded, total int64) {
		// Emit progress event to frontend
		progress := float64(downloaded) / float64(total) * 100
		runtime.EventsEmit(a.ctx, "update-progress", map[string]interface{}{
			"downloaded": downloaded,
			"total":      total,
			"progress":   progress,
		})
	})
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Install the update
	if err := updater.InstallUpdate(tempFile); err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	// Clean up temp file
	os.Remove(tempFile)

	return nil
}

// QuitApp quits the application (user can manually restart it)
func (a *App) QuitApp() {
	// Close all connections first
	a.Close()
	
	// Quit the application
	runtime.Quit(a.ctx)
}
