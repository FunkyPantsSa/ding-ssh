package sshx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"

	"github.com/pkg/sftp"
)

// ErrSessionNotFound 会话不存在。
var ErrSessionNotFound = errors.New("会话不存在")

// ErrTransferCancelled SFTP 传输被用户取消。
var ErrTransferCancelled = errors.New("传输已取消")

// Notifier 事件通知回调，由上层（App）注入，实现与 Wails runtime 解耦。
type Notifier func(eventName string, payload interface{})

// activeTransfer 一条进行中的 SFTP 传输，cancel 用于通知传输协程停止。
type activeTransfer struct {
	sessionID string
	direction string
	name      string
	cancel    context.CancelFunc
}

// Manager 会话管理器：维护全部活动的 SSH 会话与隧道。
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	tunnels  map[string]*Tunnel
	notify   Notifier

	transfersMu sync.Mutex
	transfers   map[string][]*activeTransfer // key: sessionID

	cache *SFTPCacheManager // SWR 目录缓存
}

// NewManager 创建会话管理器。
func NewManager(notify Notifier) *Manager {
	return &Manager{
		sessions:  make(map[string]*Session),
		tunnels:   make(map[string]*Tunnel),
		transfers: make(map[string][]*activeTransfer),
		notify:    notify,
		cache:     NewSFTPCacheManager(),
	}
}

// Connect 建立新会话。
func (m *Manager) Connect(sessionID string, node models.ServerNode, cols, rows int) error {
	logx.Debugf("发起 SSH 连接: session=%s server=%s host=%s:%d user=%s",
		sessionID, node.Name, node.Host, node.Port, node.User)
	s, err := newSession(sessionID, node, cols, rows,
		func(id string, data []byte) {
			// 事件按会话粒度命名，便于前端独立注册/注销监听。
			m.notify("ssh:output:"+id, models.OutputEvent{
				SessionID: id,
				Data:      encodeBase64(data),
			})
		},
		func(id string, status models.SessionStatus, message string) {
			m.notify("ssh:status:"+id, models.StatusEvent{
				SessionID: id,
				Status:    status,
				Message:   message,
			})
		},
		func(id string) {
			m.remove(id)
		},
		func(id string, step string) {
			m.notify("ssh:progress:"+id, models.ProgressEvent{
				SessionID: id,
				Step:      step,
			})
		},
		func(id string, path string) {
			m.notify("sftp:sync-path:"+id, models.DirSyncEvent{
				SessionID:   id,
				CurrentPath: path,
				Source:      "terminal",
			})
		},
	)
	if err != nil {
		logx.Errorf("SSH 连接失败: session=%s server=%s err=%v", sessionID, node.Name, err)
		return err
	}
	logx.Debugf("SSH 连接成功: session=%s server=%s", sessionID, node.Name)
	m.mu.Lock()
	m.sessions[sessionID] = s
	m.mu.Unlock()
	return nil
}

// Write 向会话写入输入数据（base64 解码后）。
func (m *Manager) Write(sessionID string, dataBase64 string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	data, err := decodeBase64(dataBase64)
	if err != nil {
		return err
	}
	return s.Write(data)
}

// Resize 调整会话终端尺寸。
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	return s.Resize(cols, rows)
}

// Disconnect 主动断开会话。
func (m *Manager) Disconnect(sessionID string) error {
	s, err := m.get(sessionID)
	if err != nil {
		logx.Errorf("断开会话失败: session=%s err=%v", sessionID, err)
		return err
	}
	logx.Debugf("用户断开会话: session=%s", sessionID)
	s.close(models.StatusClosed, "用户主动断开")
	return nil
}

// Reconnect 重新建立已断开的 SSH 会话，复用原会话配置与回调。
func (m *Manager) Reconnect(sessionID string, cols, rows int) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	logx.Debugf("重新连接会话: session=%s", sessionID)
	return s.Reconnect(cols, rows)
}

// List 返回全部会话摘要。
func (m *Manager) List() []models.SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]models.SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, s.Info())
	}
	return infos
}

// SyncSftpToTerminal 在 SFTP 面板双击文件夹时，向终端发送 cd 命令实现目录同步。
func (m *Manager) SyncSftpToTerminal(sessionID, path string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	cmd := "cd " + escapeShellPath(path) + "\n"
	return s.Write([]byte(cmd))
}

// SetSftpPathFromTerminal 当 Shell 侧检测到目录变更时，向前端推送同步事件。
func (m *Manager) SetSftpPathFromTerminal(sessionID, path string) error {
	_, err := m.get(sessionID)
	if err != nil {
		return err
	}
	m.notify("sftp:sync-path:"+sessionID, models.DirSyncEvent{
		SessionID:   sessionID,
		CurrentPath: path,
		Source:      "terminal",
	})
	return nil
}

// escapeShellPath 对路径中的特殊字符进行转义，确保 cd 命令安全执行。
func escapeShellPath(path string) string {
	escaped := strings.ReplaceAll(path, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// SftpList 列出指定会话的远程目录条目。
func (m *Manager) SftpList(sessionID, path string) ([]models.SFTPEntry, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	client, err := s.SftpClient()
	if err != nil {
		return nil, fmt.Errorf("建立 SFTP 连接失败: %w", err)
	}

	readDir := func(p string) ([]models.SFTPEntry, error) {
		infos, err := client.ReadDir(p)
		if err != nil {
			return nil, err
		}
		base := p
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		entries := make([]models.SFTPEntry, 0, len(infos))
		for _, fi := range infos {
			entries = append(entries, models.SFTPEntry{
				Name:    fi.Name(),
				Path:    base + fi.Name(),
				IsDir:   fi.IsDir(),
				Size:    fi.Size(),
				ModTime: fi.ModTime().UnixMilli(),
			})
		}
		return entries, nil
	}

	// SWR 模式：优先缓存，后台异步校验
	var result []models.SFTPEntry
	m.cache.SWRReadDir(context.Background(), path, readDir,
		func(entries []models.SFTPEntry) {
			result = entries
		},
		func(diff *entryDiff, entries []models.SFTPEntry) {
			m.notify("sftp:dir-updated:"+sessionID, map[string]interface{}{
				"sessionId": sessionID,
				"path":      path,
				"entries":   entries,
			})
		},
	)
	if result == nil {
		// 缓存未命中且异步尚未完成，降级为同步读取
		entries, err := readDir(path)
		if err != nil {
			return nil, err
		}
		m.cache.Set(path, entries)
		result = entries
	}
	return result, nil
}

// SftpRename 重命名远程文件或目录。
func (m *Manager) SftpRename(sessionID, oldPath, newPath string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf("建立 SFTP 连接失败: %w", err)
	}
	if err := client.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("重命名失败: %w", err)
	}
	m.cache.Invalidate(parentDir(oldPath))
	m.cache.Invalidate(parentDir(newPath))
	return nil
}

// SftpMkdir 在远程路径新建目录。
func (m *Manager) SftpMkdir(sessionID, path string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf("建立 SFTP 连接失败: %w", err)
	}
	if err := client.Mkdir(path); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	m.cache.Invalidate(parentDir(path))
	return nil
}

// SftpRemove 删除远程文件或目录（目录递归删除）。
func (m *Manager) SftpRemove(sessionID, path string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf("建立 SFTP 连接失败: %w", err)
	}
	if err := removeRemote(client, path); err != nil {
		return fmt.Errorf("删除失败: %w", err)
	}
	m.cache.Invalidate(parentDir(path))
	return nil
}

// removeRemote 递归删除远程路径：目录先删子项再删自身。
func removeRemote(client *sftp.Client, path string) error {
	fi, err := client.Stat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return client.Remove(path)
	}
	children, err := client.ReadDir(path)
	if err != nil {
		return err
	}
	for _, c := range children {
		child := path
		if !strings.HasSuffix(child, "/") {
			child += "/"
		}
		child += c.Name()
		if err := removeRemote(client, child); err != nil {
			return err
		}
	}
	return client.RemoveDirectory(path)
}

// transferNotifyInterval 传输进度事件的最小发送间隔。
const transferNotifyInterval = 100 * time.Millisecond

// SftpUpload 上传本地文件到远程路径，过程中通过 sftp:transfer:{sessionID} 上报进度。
// 传输注册到取消表，用户可通过 SftpCancelTransfer 取消。
func (m *Manager) SftpUpload(sessionID, localPath, remotePath string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf("建立 SFTP 连接失败: %w", err)
	}
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %w", err)
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return fmt.Errorf("读取本地文件信息失败: %w", err)
	}
	dst, err := client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("创建远程文件失败: %w", err)
	}
	defer dst.Close()
	name := filepath.Base(localPath)
	ctx, cancel := context.WithCancel(context.Background())
	tr := m.registerTransfer(sessionID, "upload", name, cancel)
	defer m.unregisterTransfer(tr)
	err = m.streamTransfer(ctx, sessionID, "upload", name, fi.Size(), src.Read, dst.Write)
	if err != nil {
		if errors.Is(err, ErrTransferCancelled) {
			return nil // 已取消，进度事件已上报
		}
		_ = client.Remove(remotePath) // 清理不完整的上传文件
		return fmt.Errorf("上传失败: %w", err)
	}
	m.cache.Invalidate(parentDir(remotePath))
	return nil
}

// SftpDownload 下载远程文件到本地路径，过程中通过 sftp:transfer:{sessionID} 上报进度。
// 传输注册到取消表，用户可通过 SftpCancelTransfer 取消。
func (m *Manager) SftpDownload(sessionID, remotePath, localPath string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf("建立 SFTP 连接失败: %w", err)
	}
	src, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("打开远程文件失败: %w", err)
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return fmt.Errorf("读取远程文件信息失败: %w", err)
	}
	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %w", err)
	}
	defer dst.Close()
	name := filepath.Base(remotePath)
	ctx, cancel := context.WithCancel(context.Background())
	tr := m.registerTransfer(sessionID, "download", name, cancel)
	defer m.unregisterTransfer(tr)
	err = m.streamTransfer(ctx, sessionID, "download", name, fi.Size(), src.Read, dst.Write)
	if err != nil {
		if errors.Is(err, ErrTransferCancelled) {
			_ = os.Remove(localPath) // 清理不完整的下载文件
			return nil               // 已取消，进度事件已上报
		}
		_ = os.Remove(localPath) // 清理不完整的下载文件
		return fmt.Errorf("下载失败: %w", err)
	}
	m.cache.Invalidate(parentDir(remotePath))
	return nil
}

// streamTransfer 通用流式拷贝并上报进度（限频 100ms）。
// 每轮拷贝前检查 ctx，取消时上报「已取消」事件并返回 ErrTransferCancelled。
func (m *Manager) streamTransfer(
	ctx context.Context,
	sessionID, direction, name string,
	total int64,
	read func([]byte) (int, error),
	write func([]byte) (int, error),
) error {
	transferred := int64(0)
	last := time.Now()
	notify := func(done bool, errMsg string) {
		if !done && time.Since(last) < transferNotifyInterval {
			return
		}
		last = time.Now()
		m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
			SessionID:   sessionID,
			Direction:   direction,
			Name:        name,
			Transferred: transferred,
			Total:       total,
			Done:        done,
			Error:       errMsg,
		})
	}
	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			notify(true, "已取消")
			return ErrTransferCancelled
		}
		n, rerr := read(buf)
		if n > 0 {
			// Phase 2: 令牌桶限速
			if err := DefaultTransferPool.Limiter().Wait(ctx, n); err != nil {
				if err == context.Canceled {
					notify(true, "已取消")
					return ErrTransferCancelled
				}
				notify(true, err.Error())
				return err
			}
			if _, werr := write(buf[:n]); werr != nil {
				notify(true, werr.Error())
				return werr
			}
			transferred += int64(n)
			notify(false, "")
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			notify(true, rerr.Error())
			return rerr
		}
	}
	notify(true, "")
	return nil
}

// registerTransfer 将一条传输登记到取消表。
func (m *Manager) registerTransfer(sessionID, direction, name string, cancel context.CancelFunc) *activeTransfer {
	tr := &activeTransfer{sessionID: sessionID, direction: direction, name: name, cancel: cancel}
	m.transfersMu.Lock()
	m.transfers[sessionID] = append(m.transfers[sessionID], tr)
	m.transfersMu.Unlock()
	return tr
}

// unregisterTransfer 从取消表移除已结束的传输。
func (m *Manager) unregisterTransfer(tr *activeTransfer) {
	m.transfersMu.Lock()
	defer m.transfersMu.Unlock()
	list := m.transfers[tr.sessionID]
	for i, t := range list {
		if t == tr {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(m.transfers, tr.sessionID)
	} else {
		m.transfers[tr.sessionID] = list
	}
}

// SftpCancelTransfer 取消指定会话中正在进行的 SFTP 传输（按方向 + 文件名定位）。
func (m *Manager) SftpCancelTransfer(sessionID, direction, name string) error {
	m.transfersMu.Lock()
	defer m.transfersMu.Unlock()
	list := m.transfers[sessionID]
	cancelled := 0
	for _, tr := range list {
		if tr.direction == direction && tr.name == name {
			tr.cancel()
			cancelled++
		}
	}
	if cancelled == 0 {
		return fmt.Errorf("没有进行中的传输: direction=%s name=%s", direction, name)
	}
	return nil
}

// CloseAll 关闭全部会话与隧道（应用退出时调用）。
func (m *Manager) CloseAll() {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	m.mu.RUnlock()
	for _, s := range sessions {
		s.close(models.StatusClosed, "应用退出")
	}
	for _, t := range tunnels {
		t.Stop()
	}
}

// ---- SSH 隧道 ----

// StartTunnel 创建并启动一条 SSH 隧道（本地端口转发），
// 状态变更通过 tunnel:status 事件上报。
func (m *Manager) StartTunnel(node models.ServerNode, name string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	if localPort < 1 || localPort > 65535 {
		return models.TunnelInfo{}, fmt.Errorf("本地端口无效: %d", localPort)
	}
	if remotePort < 1 || remotePort > 65535 {
		return models.TunnelInfo{}, fmt.Errorf("远程端口无效: %d", remotePort)
	}
	if name == "" {
		name = fmt.Sprintf("%s:%d", node.Name, localPort)
	}
	id := fmt.Sprintf("tunnel-%d", time.Now().UnixMilli())
	t := newTunnel(id, name, node, localPort, remoteHost, remotePort, func(id string, status TunnelStatus, message string) {
		m.notify("tunnel:status", models.TunnelStatusEvent{ID: id, Status: string(status), Message: message})
	})
	if err := t.Start(); err != nil {
		return models.TunnelInfo{}, err
	}
	m.mu.Lock()
	m.tunnels[id] = t
	m.mu.Unlock()
	return t.Info(), nil
}

// StopTunnel 停止指定隧道（保留条目，可重新启动）。
func (m *Manager) StopTunnel(id string) error {
	t, err := m.getTunnel(id)
	if err != nil {
		return err
	}
	t.Stop()
	return nil
}

// RestartTunnel 重新启动已停止/异常的隧道。
func (m *Manager) RestartTunnel(id string) error {
	t, err := m.getTunnel(id)
	if err != nil {
		return err
	}
	return t.Start()
}

// RemoveTunnel 停止并移除指定隧道。
func (m *Manager) RemoveTunnel(id string) error {
	m.mu.Lock()
	t, ok := m.tunnels[id]
	delete(m.tunnels, id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("隧道不存在: %s", id)
	}
	t.Stop()
	return nil
}

// ListTunnels 返回全部隧道摘要。
func (m *Manager) ListTunnels() []models.TunnelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	infos := make([]models.TunnelInfo, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		infos = append(infos, t.Info())
	}
	return infos
}

func (m *Manager) getTunnel(id string) (*Tunnel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tunnels[id]
	if !ok {
		return nil, fmt.Errorf("隧道不存在: %s", id)
	}
	return t, nil
}

func (m *Manager) get(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func (m *Manager) remove(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// parentDir 返回给定路径的父目录路径。
func parentDir(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	// 去掉末尾的 /
	for len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "/"
	}
	if idx == 0 {
		return "/"
	}
	return path[:idx]
}
