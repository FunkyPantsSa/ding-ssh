package sshx

import (
	"context"
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

// Tunnel 一条 SSH 隧道（本地端口转发）。
type Tunnel struct {
	id         string
	name       string
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
	node models.ServerNode,
	localPort int,
	remoteHost string,
	remotePort int,
	onStatus func(string, TunnelStatus, string),
) *Tunnel {
	ctx, cancel := context.WithCancel(context.Background())
	return &Tunnel{
		id:         id,
		name:       name,
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
		LocalPort:  t.localPort,
		RemoteHost: t.remoteHost,
		RemotePort: t.remotePort,
		Status:     string(t.status),
		Message:    t.message,
		StartedAt:  t.startedAt,
	}
}

// Start 建立 SSH 连接并开始监听本地端口；支持从 stopped/error 状态重新启动。
func (t *Tunnel) Start() error {
	t.mu.Lock()
	if t.status == TunnelRunning {
		t.mu.Unlock()
		return fmt.Errorf("隧道已在运行")
	}
	// 清理上次残留资源（error 状态重新启动时）。
	if t.listener != nil {
		_ = t.listener.Close()
	}
	if t.client != nil {
		_ = t.client.Close()
	}
	t.mu.Unlock()

	config, err := buildClientConfig(t.node)
	if err != nil {
		return err
	}
	client, err := dialSSH(t.node.Host, t.node.Port, config)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.localPort)))
	if err != nil {
		client.Close()
		return fmt.Errorf("监听本地端口 %d 失败: %w", t.localPort, err)
	}

	t.mu.Lock()
	t.client = client
	t.listener = ln
	t.status = TunnelRunning
	t.message = ""
	t.mu.Unlock()

	logx.Infof("SSH 隧道已启动: id=%s name=%s local=127.0.0.1:%d remote=%s:%d",
		t.id, t.name, t.localPort, t.remoteHost, t.remotePort)
	t.notify(TunnelRunning, "")
	t.wg.Add(1)
	go t.acceptLoop()
	return nil
}

// Stop 停止隧道：关闭监听与 SSH 连接，状态置为 stopped（保留条目可重新启动）。
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

// acceptLoop 接受本地连接并转发到远程目标。
func (t *Tunnel) acceptLoop() {
	defer t.wg.Done()
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			if t.stopCtx.Err() != nil {
				return
			}
			t.fail(fmt.Sprintf("监听本地端口异常: %v", err))
			return
		}
		go t.handleConn(conn)
	}
}

// handleConn 双向转发一条 TCP 连接。
func (t *Tunnel) handleConn(local net.Conn) {
	defer local.Close()
	remote, err := t.client.Dial("tcp", net.JoinHostPort(t.remoteHost, strconv.Itoa(t.remotePort)))
	if err != nil {
		logx.Debugf("SSH 隧道转发失败: id=%s remote=%s:%d err=%v", t.id, t.remoteHost, t.remotePort, err)
		return
	}
	defer remote.Close()
	go func() {
		_, _ = io.Copy(remote, local)
		remote.Close()
	}()
	_, _ = io.Copy(local, remote)
}

// fail 将隧道置为 error 状态并通知前端。
func (t *Tunnel) fail(message string) {
	t.mu.Lock()
	t.status = TunnelError
	t.message = message
	t.mu.Unlock()
	logx.Errorf("SSH 隧道异常: id=%s err=%s", t.id, message)
	t.notify(TunnelError, message)
}

// notify 通过回调上报状态事件。
func (t *Tunnel) notify(status TunnelStatus, message string) {
	if t.onStatus != nil {
		t.onStatus(t.id, status, message)
	}
}
