package sshx

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"

	"golang.org/x/crypto/ssh"
)

// TunnelStatus 隧道运行状态。
type TunnelStatus string

const (
	TunnelRunning TunnelStatus = "running"
	TunnelStopped TunnelStatus = "stopped"
	TunnelError   TunnelStatus = "error"
)

// TunnelMode 隧道转发模式。
type TunnelMode string

const (
	TunnelLocal    TunnelMode = "local"    // -L 本地转发
	TunnelRemote   TunnelMode = "remote"   // -R 远程转发
	TunnelDynamic  TunnelMode = "dynamic"  // -D SOCKS5
)

// Tunnel 一条 SSH 隧道（支持 Local / Remote / Dynamic）。
type Tunnel struct {
	id         string
	name       string
	mode       TunnelMode
	node       models.ServerNode
	localPort  int
	remoteHost string
	remotePort int
	startedAt  int64

	mu       sync.Mutex
	status   TunnelStatus
	message  string
	client   *ssh.Client
	listener net.Listener
	stopCtx  context.Context
	stopFn   context.CancelFunc
	wg       sync.WaitGroup

	onStatus func(id string, status TunnelStatus, message string)
}

// newTunnel 创建隧道实例，初始为 stopped 状态。
func newTunnel(
	id, name string,
	mode TunnelMode,
	node models.ServerNode,
	localPort int,
	remoteHost string,
	remotePort int,
	onStatus func(string, TunnelStatus, string),
) *Tunnel {
	ctx, cancel := context.WithCancel(context.Background())
	if mode == "" {
		mode = TunnelLocal
	}
	return &Tunnel{
		id:         id,
		name:       name,
		mode:       mode,
		node:       node,
		localPort:  localPort,
		remoteHost: remoteHost,
		remotePort: remotePort,
		startedAt:  time.Now().UnixMilli(),
		status:     TunnelStopped,
		stopCtx:    ctx,
		stopFn:     cancel,
		onStatus:   onStatus,
	}
}

// Info 返回对外展示的隧道信息。
func (t *Tunnel) Info() models.TunnelInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return models.TunnelInfo{
		ID:         t.id,
		Name:       t.name,
		ServerID:   t.node.ID,
		ServerName: t.node.Name,
		Mode:       string(t.mode),
		LocalPort:  t.localPort,
		RemoteHost: t.remoteHost,
		RemotePort: t.remotePort,
		Status:     string(t.status),
		Message:    t.message,
		StartedAt:  t.startedAt,
	}
}

// Status 返回当前运行状态。
func (t *Tunnel) Status() TunnelStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// Start 建立 SSH 连接并按模式启动转发。
func (t *Tunnel) Start() error {
	t.mu.Lock()
	if t.status == TunnelRunning {
		t.mu.Unlock()
		return fmt.Errorf("隧道已在运行")
	}
	if t.listener != nil {
		_ = t.listener.Close()
	}
	if t.client != nil {
		_ = t.client.Close()
	}
	// 重置 stop context，支持 restart
	t.stopFn()
	ctx, cancel := context.WithCancel(context.Background())
	t.stopCtx = ctx
	t.stopFn = cancel
	t.mu.Unlock()

	config, err := buildClientConfig(t.node, noopProgress)
	if err != nil {
		return err
	}
	client, err := dialSSH(t.node.Host, t.node.Port, config, noopProgress)
	if err != nil {
		return err
	}

	var ln net.Listener
	switch t.mode {
	case TunnelRemote:
		addr := net.JoinHostPort(t.remoteHost, strconv.Itoa(t.remotePort))
		ln, err = client.Listen("tcp", addr)
		if err != nil {
			client.Close()
			return fmt.Errorf("远程监听 %s 失败: %w", addr, err)
		}
	case TunnelDynamic:
		ln, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.localPort)))
		if err != nil {
			client.Close()
			return fmt.Errorf("监听本地 SOCKS5 端口 %d 失败: %w", t.localPort, err)
		}
	default: // local
		ln, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.localPort)))
		if err != nil {
			client.Close()
			return fmt.Errorf("监听本地端口 %d 失败: %w", t.localPort, err)
		}
	}

	t.mu.Lock()
	t.client = client
	t.listener = ln
	t.status = TunnelRunning
	t.message = ""
	t.startedAt = time.Now().UnixMilli()
	t.mu.Unlock()

	logx.Infof("SSH 隧道已启动: id=%s mode=%s name=%s local=%d remote=%s:%d",
		t.id, t.mode, t.name, t.localPort, t.remoteHost, t.remotePort)
	t.notify(TunnelRunning, "")
	t.wg.Add(1)
	go t.acceptLoop()
	return nil
}

// Stop 停止隧道。
func (t *Tunnel) Stop() {
	t.mu.Lock()
	t.stopFn()
	if t.listener != nil {
		_ = t.listener.Close()
	}
	if t.client != nil {
		_ = t.client.Close()
	}
	t.status = TunnelStopped
	t.message = ""
	t.mu.Unlock()
	t.wg.Wait()
	logx.Infof("SSH 隧道已停止: id=%s name=%s", t.id, t.name)
	t.notify(TunnelStopped, "")
}

func (t *Tunnel) acceptLoop() {
	defer t.wg.Done()
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			if t.stopCtx.Err() != nil {
				return
			}
			t.fail(fmt.Sprintf("监听异常: %v", err))
			return
		}
		switch t.mode {
		case TunnelRemote:
			go t.handleRemoteConn(conn)
		case TunnelDynamic:
			go t.handleSocksConn(conn)
		default:
			go t.handleLocalConn(conn)
		}
	}
}

func (t *Tunnel) handleLocalConn(local net.Conn) {
	defer local.Close()
	remote, err := t.client.Dial("tcp", net.JoinHostPort(t.remoteHost, strconv.Itoa(t.remotePort)))
	if err != nil {
		logx.Debugf("SSH 本地转发失败: id=%s remote=%s:%d err=%v", t.id, t.remoteHost, t.remotePort, err)
		return
	}
	defer remote.Close()
	pipe(local, remote)
}

func (t *Tunnel) handleRemoteConn(remote net.Conn) {
	defer remote.Close()
	// Remote Forward：远程连入后转发到本地目标
	local, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.localPort)), 10*time.Second)
	if err != nil {
		logx.Debugf("SSH 远程转发连接本地失败: id=%s localPort=%d err=%v", t.id, t.localPort, err)
		return
	}
	defer local.Close()
	pipe(remote, local)
}

func (t *Tunnel) handleSocksConn(conn net.Conn) {
	defer conn.Close()
	target, err := socks5Handshake(conn)
	if err != nil {
		logx.Debugf("SOCKS5 握手失败: id=%s err=%v", t.id, err)
		return
	}
	remote, err := t.client.Dial("tcp", target)
	if err != nil {
		// 回复连接失败
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	// 成功响应
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	pipe(conn, remote)
}

func pipe(a, b net.Conn) {
	go func() {
		_, _ = io.Copy(b, a)
		b.Close()
	}()
	_, _ = io.Copy(a, b)
}

func (t *Tunnel) fail(message string) {
	t.mu.Lock()
	t.status = TunnelError
	t.message = message
	t.mu.Unlock()
	logx.Errorf("SSH 隧道异常: id=%s err=%s", t.id, message)
	t.notify(TunnelError, message)
}

func (t *Tunnel) notify(status TunnelStatus, message string) {
	if t.onStatus != nil {
		t.onStatus(t.id, status, message)
	}
}

// socks5Handshake 处理 SOCKS5 无认证握手，返回目标地址 host:port。
func socks5Handshake(conn net.Conn) (string, error) {
	buf := make([]byte, 258)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return "", err
	}
	if buf[0] != 0x05 {
		return "", fmt.Errorf("非 SOCKS5 协议")
	}
	nMethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		return "", err
	}
	// 无认证
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return "", err
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return "", err
	}
	if buf[0] != 0x05 || buf[1] != 0x01 {
		return "", fmt.Errorf("仅支持 CONNECT")
	}
	var host string
	switch buf[3] {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return "", err
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // Domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return "", err
		}
		l := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:l]); err != nil {
			return "", err
		}
		host = string(buf[:l])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return "", err
		}
		host = net.IP(buf[:16]).String()
	default:
		return "", fmt.Errorf("不支持的地址类型")
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(buf[:2])
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}
