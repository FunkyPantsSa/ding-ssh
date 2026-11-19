package sshx

import (
	"context"
	"ding-ssh/internal/i18n"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
)

// 传输进度/分块是高频路径：只允许采样 DEBUG，且只记累计字节与百分比。
var transferChunkLog = logx.NewSampler(0, 2*time.Second)

// ErrSessionNotFound 会话不存在。
var ErrSessionNotFound = errors.New(i18n.T("会话不存在"))

// ErrTransferCancelled SFTP 传输被用户取消。
var ErrTransferCancelled = errors.New(i18n.T("传输已取消"))

// Notifier 事件通知回调，由上层（App）注入，实现与 Wails runtime 解耦。
type Notifier func(eventName string, payload interface{})

// activeTransfer 一条进行中的 SFTP 传输，cancel 用于通知传输协程停止。
type transferContextKey struct{}

type activeTransfer struct {
	id        string
	sessionID string
	direction string
	name      string
	cancel    context.CancelFunc
}

// SettingsGetter 由 App 注入，用于在建立会话时读取当前应用设置。
type SettingsGetter func() models.Settings

// Manager 会话管理器：维护全部活动的 SSH 会话与隧道。
type Manager struct {
	mu              sync.RWMutex
	sessions        map[string]*Session
	pending         map[string]context.CancelFunc
	tunnels         map[string]*Tunnel
	sysinfo         map[string]*SysInfoCollector
	sysInfoRealtime map[string]bool // 会话级实时刷新覆盖；关闭会话即清除，不写全局设置
	notify          Notifier

	transfersMu sync.Mutex
	transfers   map[string][]*activeTransfer // key: sessionID

	cache       *SFTPCacheManager // SWR 目录缓存
	getSettings SettingsGetter    // 读取应用设置（可为 nil，使用默认值）
}

// NewManager 创建会话管理器。
func NewManager(notify Notifier) *Manager {
	return &Manager{
		sessions:        make(map[string]*Session),
		pending:         make(map[string]context.CancelFunc),
		tunnels:         make(map[string]*Tunnel),
		sysinfo:         make(map[string]*SysInfoCollector),
		sysInfoRealtime: make(map[string]bool),
		transfers:       make(map[string][]*activeTransfer),
		notify:          notify,
		cache:           NewSFTPCacheManager(),
	}
}

// SetSettingsGetter 注入应用设置读取函数，用于建立会话时获取 keepAliveEnabled 等配置。
func (m *Manager) SetSettingsGetter(fn SettingsGetter) {
	m.getSettings = fn
}

// currentSettings 返回当前应用设置，若未注入则返回默认值。
func (m *Manager) currentSettings() models.Settings {
	if m.getSettings != nil {
		return m.getSettings()
	}
	return models.Settings{KeepAliveEnabled: true, AutoReconnect: true, SysInfoCollectEnabled: true}
}

// Connect 建立新会话。
func (m *Manager) Connect(sessionID string, node models.ServerNode, cols, rows int) error {
	start := time.Now()
	logx.Debugf("[sshx] 发起 SSH 连接: session=%s server=%s host=%s:%d user=%s auth=%s jumps=%d size=%dx%d",
		sessionID, node.Name, node.Host, node.Port, node.User, node.AuthType, len(node.JumpServerIDs), cols, rows)
	m.mu.Lock()
	if _, ok := m.pending[sessionID]; ok {
		m.mu.Unlock()
		logx.Warnf("[sshx] 会话正在连接中，拒绝重复发起: session=%s server=%s", sessionID, node.Name)
		return fmt.Errorf("%s", i18n.T("会话正在连接"))
	}
	if _, ok := m.sessions[sessionID]; ok {
		m.mu.Unlock()
		logx.Warnf("[sshx] 会话已存在，拒绝重复发起: session=%s server=%s", sessionID, node.Name)
		return fmt.Errorf("%s", i18n.T("会话已存在"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.pending[sessionID] = cancel
	m.mu.Unlock()
	defer func() { cancel(); m.mu.Lock(); delete(m.pending, sessionID); m.mu.Unlock() }()
	cfg := m.currentSettings()
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
		func(id string, path string) {
			if !strings.HasPrefix(path, "/") {
				logx.Debugf("[sshx] 忽略非绝对路径的目录同步: session=%s path=%q", id, path)
				return
			}
			logx.Debugf("[sshx] 推送 sftp:sync-path: session=%s path=%s", id, path)
			m.notify("sftp:sync-path:"+id, models.DirSyncEvent{
				SessionID:   id,
				CurrentPath: path,
				Source:      "terminal",
			})
		},
		func(id string, step, status, log, message string) {
			m.notify("ssh:progress:"+id, models.ProgressEvent{
				SessionID: id,
				Step:      step,
				Status:    status,
				Log:       log,
				Message:   message,
			})
		},
		cfg.KeepAliveEnabled,
		ctx,
	)
	if err != nil {
		logx.Errorf("[sshx] SSH 连接失败: session=%s server=%s host=%s:%d user=%s elapsed=%s err=%v",
			sessionID, node.Name, node.Host, node.Port, node.User, time.Since(start), err)
		return err
	}
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		logx.Debugf("[sshx] 连接建立后已被取消: session=%s elapsed=%s", sessionID, time.Since(start))
		s.close(models.StatusClosed, i18n.T("连接已取消"))
		return err
	}
	delete(m.pending, sessionID)
	m.sessions[sessionID] = s
	sessions := len(m.sessions)
	m.mu.Unlock()
	logx.Infof("[sshx] SSH 连接成功: session=%s server=%s host=%s:%d user=%s elapsed=%s activeSessions=%d",
		sessionID, node.Name, node.Host, node.Port, node.User, time.Since(start), sessions)
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
		logx.Debugf("[sshx] 终端输入解码失败: session=%s length=%d err=%v", sessionID, len(dataBase64), err)
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
	m.mu.Lock()
	cancel := m.pending[sessionID]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		return nil
	}

	s, err := m.get(sessionID)
	if err != nil {
		logx.Errorf("[sshx] 断开会话失败: session=%s err=%v", sessionID, err)
		return err
	}
	logx.Infof("[sshx] 用户断开会话: session=%s server=%s", sessionID, s.server.Name)
	s.close(models.StatusClosed, i18n.T("用户主动断开"))
	return nil
}

// Reconnect 重新建立已断开的 SSH 会话，复用原会话配置与回调。
func (m *Manager) Reconnect(sessionID string, cols, rows int) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	logx.Debugf("[sshx] 重新连接会话: session=%s server=%s size=%dx%d", sessionID, s.server.Name, cols, rows)
	cfg := m.currentSettings()
	start := time.Now()
	if err := s.Reconnect(cols, rows, cfg.KeepAliveEnabled); err != nil {
		logx.Errorf("[sshx] 会话重连失败: session=%s server=%s elapsed=%s err=%v", sessionID, s.server.Name, time.Since(start), err)
		return err
	}
	logx.Infof("[sshx] 会话重连成功: session=%s server=%s elapsed=%s", sessionID, s.server.Name, time.Since(start))
	return nil
}

// SetKeepAliveEnabled 动态更新所有会话的心跳开关。
func (m *Manager) SetKeepAliveEnabled(enabled bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	logx.Infof("[sshx] 心跳检测开关已更新: enabled=%v sessions=%d", enabled, len(m.sessions))
	for _, s := range m.sessions {
		s.SetKeepAliveEnabled(enabled)
	}
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
	if s.server.JumpServerMode {
		logx.Warnf("[sshx] JumpServer 模式不支持目录同步到终端: session=%s path=%s", sessionID, path)
		return fmt.Errorf("JumpServer 模式不支持终端与 SFTP 目录同步")
	}
	cmd := "cd " + escapeShellPath(path) + "\n"
	logx.Infof("[sshx] 目录同步(SFTP→终端): session=%s path=%s", sessionID, path)
	return s.Write([]byte(cmd))
}

// SetSftpPathFromTerminal 当 Shell 侧检测到目录变更时，向前端推送同步事件。
func (m *Manager) SetSftpPathFromTerminal(sessionID, path string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	if s.server.JumpServerMode {
		logx.Debugf("[sshx] JumpServer 模式忽略终端目录同步推送: session=%s path=%s", sessionID, path)
		return nil
	}
	logx.Debugf("[sshx] 终端目录同步已推送: session=%s path=%s", sessionID, path)
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

// SftpClient 返回指定会话的 SFTP 客户端（M8 调试控制面用）。
//
// 复用会话上**已经建立的那一份**客户端（Session.SftpClient 内部 lazy + once），
// 不新建 SSH 连接、不重复握手；会话不存在时返回 ErrSessionNotFound。
// 存在的意义：debugsrv 的 sftp.stat / sftp.read / sftp.write 需要 stat / seek / OpenFile
// 这些 Manager 现有方法没有覆盖的能力，而 Manager.get 是非导出的。
func (m *Manager) SftpClient(sessionID string) (*sftp.Client, error) {
	s, err := m.get(sessionID)
	if err != nil {
		return nil, err
	}
	client, err := s.SftpClient()
	if err != nil {
		logx.Warnf("[sshx] 获取会话 SFTP 客户端失败: session=%s err=%v", sessionID, err)
		return nil, fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	return client, nil
}

// InvalidateSftpCache 让指定目录的 SWR 缓存失效（M8：AI 写入 / 删除后让面板立刻看到新内容）。
func (m *Manager) InvalidateSftpCache(path string) {
	logx.Debugf("[sshx] 手动失效 SFTP 缓存: path=%s", path)
	m.cache.Invalidate(path)
}

// SftpList 列出指定会话的远程目录条目。
func (m *Manager) SftpList(sessionID, path string) ([]models.SFTPEntry, error) {
	start := time.Now()
	logx.Debugf("[sshx] 列远程目录: session=%s path=%s", sessionID, path)
	s, err := m.get(sessionID)
	if err != nil {
		logx.Debugf("[sshx] 列出目录失败(会话不存在): session=%s path=%s err=%v", sessionID, path, err)
		return nil, err
	}
	client, err := s.SftpClient()
	if err != nil {
		logx.Warnf("[sshx] 列出目录失败(SFTP 不可用): session=%s path=%s err=%v", sessionID, path, err)
		return nil, fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
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
		logx.Debugf("[sshx] 目录缓存未命中，降级为同步读取: session=%s path=%s", sessionID, path)
		entries, err := readDir(path)
		if err != nil {
			logx.Errorf("[sshx] 列出远程目录失败: session=%s path=%s elapsed=%s err=%v", sessionID, path, time.Since(start), err)
			return nil, err
		}
		m.cache.Set(path, entries)
		result = entries
	}
	logx.Debugf("[sshx] 远程目录已列出: session=%s path=%s entries=%d elapsed=%s", sessionID, path, len(result), time.Since(start))
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
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	start := time.Now()
	if err := client.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf(i18n.T("重命名失败: %w"), err)
	}
	m.cache.Invalidate(parentDir(oldPath))
	m.cache.Invalidate(parentDir(newPath))
	logx.Infof("[sshx] 远程重命名完成: session=%s old=%s new=%s elapsed=%s", sessionID, oldPath, newPath, time.Since(start))
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
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	start := time.Now()
	if err := client.Mkdir(path); err != nil {
		return fmt.Errorf(i18n.T("创建目录失败: %w"), err)
	}
	m.cache.Invalidate(parentDir(path))
	logx.Infof("[sshx] 远程目录已创建: session=%s path=%s elapsed=%s", sessionID, path, time.Since(start))
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
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	start := time.Now()
	if err := removeRemote(client, path); err != nil {
		return fmt.Errorf(i18n.T("删除失败: %w"), err)
	}
	m.cache.Invalidate(parentDir(path))
	logx.Infof("[sshx] 远程路径已删除: session=%s path=%s elapsed=%s", sessionID, path, time.Since(start))
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
	start := time.Now()
	logx.Debugf("[sshx] 开始上传文件: session=%s local=%s remote=%s", sessionID, localPath, remotePath)
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf(i18n.T("打开本地文件失败: %w"), err)
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return fmt.Errorf(i18n.T("读取本地文件信息失败: %w"), err)
	}
	dst, err := client.Create(remotePath)
	if err != nil {
		return fmt.Errorf(i18n.T("创建远程文件失败: %w"), err)
	}
	defer dst.Close()
	name := filepath.Base(localPath)
	ctx, cancel := context.WithCancel(context.Background())
	tr := m.registerTransfer(sessionID, "upload", name, cancel)
	defer m.unregisterTransfer(tr)
	ctx = context.WithValue(ctx, transferContextKey{}, tr.id)
	err = m.streamTransfer(ctx, sessionID, "upload", name, fi.Size(), src.Read, dst.Write)
	if err != nil {
		if errors.Is(err, ErrTransferCancelled) {
			logx.Warnf("[sshx] 上传已取消: session=%s name=%s elapsed=%s", sessionID, name, time.Since(start))
			return ErrTransferCancelled // 调用方也必须收到取消结果，避免晚到事件被记作成功
		}
		logx.Debugf("[sshx] 上传失败，清理不完整文件: session=%s name=%s remote=%s elapsed=%s err=%v",
			sessionID, name, remotePath, time.Since(start), err)
		_ = client.Remove(remotePath) // 清理不完整的上传文件
		return fmt.Errorf(i18n.T("上传失败: %w"), err)
	}
	m.cache.Invalidate(parentDir(remotePath))
	logx.Infof("[sshx] 上传完成: session=%s name=%s bytes=%d elapsed=%s", sessionID, name, fi.Size(), time.Since(start))
	return nil
}

// SftpDownload 下载远程文件到本地路径，过程中通过 sftp:transfer:{sessionID} 上报进度。
// 传输注册到取消表，用户可通过 SftpCancelTransfer 取消。
func (m *Manager) SftpDownload(sessionID, remotePath, localPath string) error {
	start := time.Now()
	logx.Debugf("[sshx] 开始下载文件: session=%s remote=%s local=%s", sessionID, remotePath, localPath)
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	src, err := client.Open(remotePath)
	if err != nil {
		return fmt.Errorf(i18n.T("打开远程文件失败: %w"), err)
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return fmt.Errorf(i18n.T("读取远程文件信息失败: %w"), err)
	}
	dst, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf(i18n.T("创建本地文件失败: %w"), err)
	}
	defer dst.Close()
	name := filepath.Base(remotePath)
	ctx, cancel := context.WithCancel(context.Background())
	tr := m.registerTransfer(sessionID, "download", name, cancel)
	defer m.unregisterTransfer(tr)
	ctx = context.WithValue(ctx, transferContextKey{}, tr.id)
	err = m.streamTransfer(ctx, sessionID, "download", name, fi.Size(), src.Read, dst.Write)
	if err != nil {
		if errors.Is(err, ErrTransferCancelled) {
			logx.Warnf("[sshx] 下载已取消: session=%s name=%s elapsed=%s", sessionID, name, time.Since(start))
			_ = os.Remove(localPath)    // 清理不完整的下载文件
			return ErrTransferCancelled // 调用方也必须收到取消结果
		}
		logx.Debugf("[sshx] 下载失败，清理不完整文件: session=%s name=%s local=%s elapsed=%s err=%v",
			sessionID, name, localPath, time.Since(start), err)
		_ = os.Remove(localPath) // 清理不完整的下载文件
		return fmt.Errorf(i18n.T("下载失败: %w"), err)
	}
	m.cache.Invalidate(parentDir(remotePath))
	logx.Infof("[sshx] 下载完成: session=%s name=%s bytes=%d elapsed=%s", sessionID, name, fi.Size(), time.Since(start))
	return nil
}

// SftpUploadFolder 递归上传本地文件夹到远端目录，保留本地文件夹名与内部目录结构。
//
// 远端目标为 remoteDir/<本地文件夹名>/...：用户选择「上传文件夹」后，本地文件夹
// 整体成为当前远端目录下的一个子目录（目录结构与本地一致）。
//
// 整个文件夹作为**一条**传输记录（name=本地文件夹名）：进度按全部文件的累计字节上报，
// 取消即取消整个文件夹（与 SftpUpload / SftpDownload 共用取消表与限速器）。
func (m *Manager) SftpUploadFolder(sessionID, localDir, remoteDir string) error {
	start := time.Now()
	logx.Debugf("[sshx] 开始上传文件夹: session=%s local=%s remote=%s", sessionID, localDir, remoteDir)
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	localDir = filepath.Clean(localDir)
	fi, err := os.Stat(localDir)
	if err != nil {
		return fmt.Errorf(i18n.T("读取本地文件夹失败: %w"), err)
	}
	if !fi.IsDir() {
		return fmt.Errorf(i18n.T("本地路径不是文件夹: %s"), localDir)
	}
	// 第一遍统计总字节数，让进度条一开始就有确定的总数。
	total, err := dirBytes(localDir)
	if err != nil {
		return fmt.Errorf(i18n.T("统计文件夹字节数失败: %w"), err)
	}

	name := filepath.Base(localDir)
	root := joinRemote(remoteDir, name)
	// 远端目标目录整体创建（已存在时 MkdirAll 不报错）。
	if err := client.MkdirAll(root); err != nil {
		return fmt.Errorf(i18n.T("创建远端目录失败: %w"), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tr := m.registerTransfer(sessionID, "upload", name, cancel)
	defer m.unregisterTransfer(tr)
	ctx = context.WithValue(ctx, transferContextKey{}, tr.id)

	var transferred atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		emit := func() {
			m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
				TransferID:  tr.id,
				SessionID:   sessionID,
				Direction:   "upload",
				Name:        name,
				Transferred: transferred.Load(),
				Total:       total,
				Done:        false,
			})
		}
		emit()
		ticker := time.NewTicker(transferNotifyInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				emit()
			case <-stop:
				return
			}
		}
	}()

	finish := func(errMsg string) {
		close(stop)
		wg.Wait()
		m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
			TransferID:  tr.id,
			SessionID:   sessionID,
			Direction:   "upload",
			Name:        name,
			Transferred: transferred.Load(),
			Total:       total,
			Done:        true,
			Error:       errMsg,
		})
	}

	uploadErr := m.uploadFolderTree(ctx, client, localDir, root, &transferred)
	if uploadErr != nil {
		if errors.Is(uploadErr, ErrTransferCancelled) {
			logx.Warnf("[sshx] 上传文件夹已取消: session=%s name=%s transferred=%d total=%d elapsed=%s",
				sessionID, name, transferred.Load(), total, time.Since(start))
			finish(i18n.T("已取消"))
			return ErrTransferCancelled // 调用方也必须收到取消结果，避免晚到事件被记作成功
		}
		logx.Debugf("[sshx] 上传文件夹失败: session=%s name=%s transferred=%d total=%d elapsed=%s err=%v",
			sessionID, name, transferred.Load(), total, time.Since(start), uploadErr)
		finish(uploadErr.Error())
		return fmt.Errorf(i18n.T("上传文件夹失败: %w"), uploadErr)
	}
	finish("")
	m.cache.Invalidate(parentDir(root))
	logx.Infof("[sshx] 上传文件夹完成: session=%s name=%s bytes=%d elapsed=%s", sessionID, name, total, time.Since(start))
	return nil
}

// SftpDownloadFolder 递归下载远端文件夹到本地目录，保留远端文件夹名与内部目录结构。
//
// 本地目标为 localDir/<远端文件夹名>/...：与「上传文件夹」对称，远端文件夹整体成为
// 用户所选本地目录下的一个子目录（目录结构与远端一致）。
//
// 整个文件夹作为**一条**传输记录（name=远端文件夹名）：进度按全部文件的累计字节上报，
// 取消即取消整个文件夹（与 SftpUpload / SftpDownload 共用取消表与限速器）。
func (m *Manager) SftpDownloadFolder(sessionID, remoteDir, localDir string) error {
	start := time.Now()
	logx.Debugf("[sshx] 开始下载文件夹: session=%s remote=%s local=%s", sessionID, remoteDir, localDir)
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	client, err := s.SftpClient()
	if err != nil {
		return fmt.Errorf(i18n.T("建立 SFTP 连接失败: %w"), err)
	}
	remoteDir = strings.TrimRight(remoteDir, "/")
	fi, err := client.Stat(remoteDir)
	if err != nil {
		return fmt.Errorf(i18n.T("读取远端文件夹失败: %w"), err)
	}
	if !fi.IsDir() {
		return fmt.Errorf(i18n.T("远端路径不是文件夹: %s"), remoteDir)
	}
	name := filepath.Base(remoteDir)
	if name == "" || name == "." || name == "/" {
		return fmt.Errorf(i18n.T("不支持下载根目录: %s"), remoteDir)
	}
	localRoot := filepath.Join(localDir, name)
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return fmt.Errorf(i18n.T("创建本地目录失败: %w"), err)
	}
	// 第一遍统计总字节数，让进度条一开始就有确定的总数。
	total, err := remoteDirBytes(client, remoteDir)
	if err != nil {
		return fmt.Errorf(i18n.T("统计远端文件夹字节数失败: %w"), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tr := m.registerTransfer(sessionID, "download", name, cancel)
	defer m.unregisterTransfer(tr)
	ctx = context.WithValue(ctx, transferContextKey{}, tr.id)

	var transferred atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		emit := func() {
			m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
				TransferID:  tr.id,
				SessionID:   sessionID,
				Direction:   "download",
				Name:        name,
				Transferred: transferred.Load(),
				Total:       total,
				Done:        false,
			})
		}
		emit()
		ticker := time.NewTicker(transferNotifyInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				emit()
			case <-stop:
				return
			}
		}
	}()

	finish := func(errMsg string) {
		close(stop)
		wg.Wait()
		m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
			TransferID:  tr.id,
			SessionID:   sessionID,
			Direction:   "download",
			Name:        name,
			Transferred: transferred.Load(),
			Total:       total,
			Done:        true,
			Error:       errMsg,
		})
	}

	downloadErr := m.downloadFolderTree(ctx, client, remoteDir, localRoot, &transferred)
	if downloadErr != nil {
		if errors.Is(downloadErr, ErrTransferCancelled) {
			logx.Warnf("[sshx] 下载文件夹已取消: session=%s name=%s transferred=%d total=%d elapsed=%s",
				sessionID, name, transferred.Load(), total, time.Since(start))
			finish(i18n.T("已取消"))
			return ErrTransferCancelled // 调用方也必须收到取消结果，避免晚到事件被记作成功
		}
		logx.Debugf("[sshx] 下载文件夹失败: session=%s name=%s transferred=%d total=%d elapsed=%s err=%v",
			sessionID, name, transferred.Load(), total, time.Since(start), downloadErr)
		finish(downloadErr.Error())
		return fmt.Errorf(i18n.T("下载文件夹失败: %w"), downloadErr)
	}
	finish("")
	logx.Infof("[sshx] 下载文件夹完成: session=%s name=%s bytes=%d elapsed=%s", sessionID, name, total, time.Since(start))
	return nil
}

// remoteDirBytes 统计远端目录树内全部常规文件的总字节数（用于文件夹下载的进度总数）。
// 符号链接不跟随、不计入（与下载时的跳过规则一致）。
func remoteDirBytes(client *sftp.Client, root string) (int64, error) {
	var total int64
	err := walkRemote(client, root, func(_ string, fi os.FileInfo) error {
		if fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	if err != nil {
		return total, err
	}
	return total, err
}

// walkRemote 深度优先遍历远端目录树（目录本身与其子项都访问，目录先于子项），
// 对每个条目调用 fn(path, fi)。符号链接一律跳过，避免跟随链接造成循环或意外访问。
func walkRemote(client *sftp.Client, root string, fn func(path string, fi os.FileInfo) error) error {
	fi, err := client.Stat(root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fn(root, fi)
	}
	if err := fn(root, fi); err != nil {
		return err
	}
	children, err := client.ReadDir(root)
	if err != nil {
		return err
	}
	for _, c := range children {
		if c.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		child := joinRemote(root, c.Name())
		if c.IsDir() {
			if err := walkRemote(client, child, fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(child, c); err != nil {
			return err
		}
	}
	return nil
}

// downloadFolderTree 深度优先下载远端目录树到本地：
//   - 目录：在本地对应路径 MkdirAll；
//   - 常规文件：逐一流式下载，字节数累计进 transferred（整条文件夹传输共用一个计数器）。
//
// 符号链接等非常规条目一律跳过（与上传对称，避免链接循环）。
func (m *Manager) downloadFolderTree(ctx context.Context, client *sftp.Client, remoteRoot, localRoot string, transferred *atomic.Int64) error {
	return walkRemote(client, remoteRoot, func(path string, fi os.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return ErrTransferCancelled
		}
		rel, err := filepath.Rel(remoteRoot, path)
		if err != nil {
			return err
		}
		localPath := filepath.Join(localRoot, rel)
		if fi.IsDir() {
			if rel == "." {
				return nil // 本地根目录已由 SftpDownloadFolder 创建
			}
			return os.MkdirAll(localPath, 0o755)
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		logx.Debugf("[sshx] 下载文件夹: 下载文件 remote=%s local=%s size=%d", path, localPath, fi.Size())
		src, err := client.Open(path)
		if err != nil {
			return fmt.Errorf(i18n.T("打开远端文件失败: %w"), err)
		}
		dst, err := os.Create(localPath)
		if err != nil {
			src.Close()
			return fmt.Errorf(i18n.T("创建本地文件失败: %w"), err)
		}
		copyErr := copyFolderChunk(ctx, src, dst, transferred)
		closeSrcErr := src.Close()
		closeDstErr := dst.Close()
		if copyErr != nil {
			_ = os.Remove(localPath) // 清理不完整的下载文件
			if errors.Is(copyErr, ErrTransferCancelled) {
				return ErrTransferCancelled
			}
			return copyErr
		}
		if closeSrcErr != nil {
			_ = os.Remove(localPath)
			return fmt.Errorf(i18n.T("关闭远端文件失败: %w"), closeSrcErr)
		}
		if closeDstErr != nil {
			_ = os.Remove(localPath)
			return fmt.Errorf(i18n.T("关闭本地文件失败: %w"), closeDstErr)
		}
		return nil
	})
}

// dirBytes 统计本地文件夹内全部常规文件的总字节数（用于文件夹上传的进度总数）。
// 符号链接 / 设备等非常规条目不计入（与上传时的跳过规则一致）。
func dirBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		total += fi.Size()
		return nil
	})
	return total, err
}

// uploadFolderTree 深度优先遍历本地文件夹：
//   - 目录：在远端对应路径 MkdirAll（父目录先于子项被访问，天然满足自底向上的依赖）；
//   - 常规文件：逐一流式上传，字节数累计进 transferred（整条文件夹传输共用一个计数器）。
//
// 符号链接 / 设备等非常规条目一律跳过，避免链接循环或意外传输。
func (m *Manager) uploadFolderTree(ctx context.Context, client *sftp.Client, localRoot, remoteRoot string, transferred *atomic.Int64) error {
	return filepath.WalkDir(localRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return ErrTransferCancelled
		}
		rel, err := filepath.Rel(localRoot, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel == "." {
				return nil // 远端根目录已由 SftpUploadFolder 创建
			}
			dir := joinRemote(remoteRoot, rel)
			logx.Debugf("[sshx] 上传文件夹: 创建远端目录 remote=%s", dir)
			return client.MkdirAll(dir)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		remotePath := joinRemote(remoteRoot, rel)
		if info, ierr := d.Info(); ierr == nil {
			logx.Debugf("[sshx] 上传文件夹: 上传文件 local=%s remote=%s size=%d", p, remotePath, info.Size())
		} else {
			logx.Debugf("[sshx] 上传文件夹: 上传文件 local=%s remote=%s", p, remotePath)
		}
		src, err := os.Open(p)
		if err != nil {
			return fmt.Errorf(i18n.T("打开本地文件失败: %w"), err)
		}
		dst, err := client.Create(remotePath)
		if err != nil {
			src.Close()
			return fmt.Errorf(i18n.T("创建远程文件失败: %w"), err)
		}
		copyErr := copyFolderChunk(ctx, src, dst, transferred)
		closeSrcErr := src.Close()
		closeDstErr := dst.Close()
		if copyErr != nil {
			_ = client.Remove(remotePath) // 清理不完整的上传文件
			if errors.Is(copyErr, ErrTransferCancelled) {
				return ErrTransferCancelled
			}
			return copyErr
		}
		if closeSrcErr != nil {
			_ = client.Remove(remotePath)
			return fmt.Errorf(i18n.T("关闭本地文件失败: %w"), closeSrcErr)
		}
		if closeDstErr != nil {
			_ = client.Remove(remotePath)
			return fmt.Errorf(i18n.T("关闭远程文件失败: %w"), closeDstErr)
		}
		return nil
	})
}

// copyFolderChunk 带取消与全局限速的逐块拷贝（无单文件进度事件 —— 进度由调用方按累计字节上报）。
// 上传 / 下载文件夹共用：src / dst 分别是本地或远端的 Reader / Writer。
func copyFolderChunk(ctx context.Context, src io.Reader, dst io.Writer, transferred *atomic.Int64) error {
	buf := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			logx.Debugf("[sshx] 文件夹传输已取消: transferred=%d", transferred.Load())
			return ErrTransferCancelled
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if err := DefaultTransferPool.Limiter().Wait(ctx, n); err != nil {
				if err == context.Canceled {
					return ErrTransferCancelled
				}
				return err
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			transferred.Add(int64(n))
			// 高频分块路径：只按采样记录累计字节，绝不记录内容。
			logx.DebugSampled(transferChunkLog, "[sshx] 文件夹传输进度: transferred=%d", transferred.Load())
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// joinRemote 拼接远端路径（一律用 / 分隔，容忍 base 以 / 结尾、name 带 \ 或首尾 /）。
func joinRemote(base, name string) string {
	name = strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/")
	if base == "" || base == "/" {
		return "/" + name
	}
	return strings.TrimRight(base, "/") + "/" + name
}

// streamTransfer 通用流式拷贝并上报进度（限频 100ms）。
// 进度由独立 goroutine 推送：Wails 绑定方法执行期间，同协程 EventsEmit 会被排到调用返回之后，
// 前端 await SftpUpload 时就看不到中间进度。
func (m *Manager) streamTransfer(
	ctx context.Context,
	sessionID, direction, name string,
	total int64,
	read func([]byte) (int, error),
	write func([]byte) (int, error),
) error {
	id, _ := ctx.Value(transferContextKey{}).(string)
	if id == "" {
		id = uuid.NewString()
	}
	var transferred atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		emit := func() {
			m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
				TransferID:  id,
				SessionID:   sessionID,
				Direction:   direction,
				Name:        name,
				Transferred: transferred.Load(),
				Total:       total,
				Done:        false,
			})
		}
		emit()
		ticker := time.NewTicker(transferNotifyInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				emit()
			case <-stop:
				return
			}
		}
	}()

	finish := func(errMsg string) {
		close(stop)
		wg.Wait()
		m.notify("sftp:transfer:"+sessionID, models.SFTPTransferEvent{
			TransferID:  id,
			SessionID:   sessionID,
			Direction:   direction,
			Name:        name,
			Transferred: transferred.Load(),
			Total:       total,
			Done:        true,
			Error:       errMsg,
		})
	}

	buf := make([]byte, 64*1024)
	chunkStart := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			logx.Debugf("[sshx] 流式传输已取消: session=%s direction=%s name=%s transferred=%d total=%d elapsed=%s",
				sessionID, direction, name, transferred.Load(), total, time.Since(chunkStart))
			finish(i18n.T("已取消"))
			return ErrTransferCancelled
		}
		n, rerr := read(buf)
		if n > 0 {
			if err := DefaultTransferPool.Limiter().Wait(ctx, n); err != nil {
				if err == context.Canceled {
					finish(i18n.T("已取消"))
					return ErrTransferCancelled
				}
				finish(err.Error())
				return err
			}
			if _, werr := write(buf[:n]); werr != nil {
				logx.Debugf("[sshx] 流式传输写入失败: session=%s direction=%s name=%s transferred=%d err=%v",
					sessionID, direction, name, transferred.Load(), werr)
				finish(werr.Error())
				return werr
			}
			transferred.Add(int64(n))
			if total > 0 {
				// 高频分块路径：采样记录百分比，绝不记录内容。
				logx.DebugSampled(transferChunkLog, "[sshx] 传输进度: session=%s direction=%s name=%s transferred=%d total=%d pct=%d%%",
					sessionID, direction, name, transferred.Load(), total, transferred.Load()*100/total)
			} else {
				logx.DebugSampled(transferChunkLog, "[sshx] 传输进度: session=%s direction=%s name=%s transferred=%d",
					sessionID, direction, name, transferred.Load())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			logx.Debugf("[sshx] 流式传输读取失败: session=%s direction=%s name=%s transferred=%d elapsed=%s err=%v",
				sessionID, direction, name, transferred.Load(), time.Since(chunkStart), rerr)
			finish(rerr.Error())
			return rerr
		}
	}
	finish("")
	return nil
}

// registerTransfer 将一条传输登记到取消表。
func (m *Manager) registerTransfer(sessionID, direction, name string, cancel context.CancelFunc) *activeTransfer {
	tr := &activeTransfer{id: uuid.NewString(), sessionID: sessionID, direction: direction, name: name, cancel: cancel}
	m.transfersMu.Lock()
	m.transfers[sessionID] = append(m.transfers[sessionID], tr)
	active := len(m.transfers[sessionID])
	m.transfersMu.Unlock()
	logx.Debugf("[sshx] 传输已登记: id=%s session=%s direction=%s name=%s active=%d", tr.id, sessionID, direction, name, active)
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
	var matches []*activeTransfer
	for _, tr := range list {
		if tr.direction != direction {
			continue
		}
		if strings.HasPrefix(name, "@id:") {
			if tr.id == strings.TrimPrefix(name, "@id:") {
				matches = append(matches, tr)
			}
		} else if tr.name == name {
			matches = append(matches, tr)
		}
	}
	if len(matches) == 0 {
		logx.Debugf("[sshx] 取消传输未命中: session=%s direction=%s name=%s", sessionID, direction, name)
		return fmt.Errorf(i18n.T("没有进行中的传输: direction=%s name=%s"), direction, name)
	}
	if len(matches) > 1 {
		logx.Warnf("[sshx] 取消传输存在同名并发: session=%s direction=%s name=%s matches=%d", sessionID, direction, name, len(matches))
		return fmt.Errorf("%s", i18n.T("存在同名并发传输，请按任务 ID 精确取消"))
	}
	logx.Infof("[sshx] 用户取消传输: session=%s direction=%s name=%s id=%s", sessionID, direction, matches[0].name, matches[0].id)
	matches[0].cancel()
	return nil
}

// CloseAll 关闭全部会话与隧道（应用退出时调用）。
func (m *Manager) CloseAll() {
	m.mu.RLock()
	for _, cancel := range m.pending {
		cancel()
	}
	sessions := make([]*Session, 0, len(m.sessions))
	tunnels := make([]*Tunnel, 0, len(m.tunnels))
	collectors := make([]*SysInfoCollector, 0, len(m.sysinfo))
	pending := len(m.pending)
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	for _, t := range m.tunnels {
		tunnels = append(tunnels, t)
	}
	for _, c := range m.sysinfo {
		collectors = append(collectors, c)
	}
	m.mu.RUnlock()
	logx.Infof("[sshx] 关闭全部会话/隧道/采集器: sessions=%d tunnels=%d collectors=%d pending=%d",
		len(sessions), len(tunnels), len(collectors), pending)
	for _, c := range collectors {
		c.Stop()
	}
	for _, s := range sessions {
		s.close(models.StatusClosed, i18n.T("应用退出"))
	}
	for _, t := range tunnels {
		t.Stop()
	}
}

// ---- SSH 隧道 ----

// normalizeTunnelConfig 校验隧道参数，并补齐默认的远端地址与名称。
func normalizeTunnelConfig(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (TunnelMode, string, string, error) {
	tm := TunnelMode(mode)
	if tm == "" {
		tm = TunnelLocal
	}
	switch tm {
	case TunnelLocal, TunnelRemote, TunnelDynamic, TunnelReverseDynamic:
	default:
		logx.Warnf("[sshx] 隧道模式非法: mode=%q", mode)
		return "", "", "", fmt.Errorf(i18n.T("不支持的隧道模式: %s"), mode)
	}
	if tm != TunnelReverseDynamic && (localPort < 1 || localPort > 65535) {
		logx.Warnf("[sshx] 隧道本地端口非法: mode=%s localPort=%d", tm, localPort)
		return "", "", "", fmt.Errorf(i18n.T("本地端口无效: %d"), localPort)
	}
	if tm != TunnelDynamic {
		if remotePort < 1 || remotePort > 65535 {
			logx.Warnf("[sshx] 隧道远程端口非法: mode=%s remotePort=%d", tm, remotePort)
			return "", "", "", fmt.Errorf(i18n.T("远程端口无效: %d"), remotePort)
		}
		if tm == TunnelReverseDynamic || remoteHost == "" {
			remoteHost = "127.0.0.1"
		}
	}
	if name == "" {
		switch tm {
		case TunnelDynamic:
			name = fmt.Sprintf("SOCKS5:%d", localPort)
		case TunnelReverseDynamic:
			name = fmt.Sprintf(i18n.T("反向 SOCKS5:%d"), remotePort)
		case TunnelRemote:
			name = fmt.Sprintf("R:%d→%d", remotePort, localPort)
		default:
			name = fmt.Sprintf("%s:%d", node.Name, localPort)
		}
	}
	return tm, remoteHost, name, nil
}

func tunnelListenHost(mode TunnelMode, hosts []string) (string, error) {
	if mode != TunnelLocal || len(hosts) == 0 || strings.TrimSpace(hosts[0]) == "" {
		return "127.0.0.1", nil
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(hosts[0]))
	if err != nil || ip.IsMulticast() || (ip.Is6() && ip.Unmap().IsUnspecified()) {
		logx.Warnf("[sshx] 隧道监听地址非法: host=%q mode=%s", hosts[0], mode)
		return "", fmt.Errorf("%s", i18n.T("请输入本机网卡的 IP 地址，或选择所有 IPv4 网卡"))
	}
	return ip.Unmap().String(), nil
}

// StartTunnel 创建并启动一条 SSH 隧道，
// 状态变更通过 tunnel:status 事件上报。
func (m *Manager) StartTunnel(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHosts ...string) (models.TunnelInfo, error) {
	tm, remoteHost, name, err := normalizeTunnelConfig(node, name, mode, localPort, remoteHost, remotePort)
	if err != nil {
		return models.TunnelInfo{}, err
	}
	listenHost, err := tunnelListenHost(tm, listenHosts)
	if err != nil {
		return models.TunnelInfo{}, err
	}
	id := fmt.Sprintf("tunnel-%d", time.Now().UnixMilli())
	t := newTunnel(id, name, tm, node, localPort, remoteHost, remotePort, m.tunnelStatusNotifier())
	t.listenHost = listenHost
	if err := t.Start(); err != nil {
		logx.Debugf("[sshx] 隧道启动失败: id=%s name=%s err=%v", id, name, err)
		return models.TunnelInfo{}, err
	}
	m.mu.Lock()
	m.tunnels[id] = t
	m.mu.Unlock()
	return t.Info(), nil
}

// UpdateTunnel 以新配置替换已有隧道；原本处于运行状态的隧道会按新配置重新启动。
// 隧道 ID 保持不变，列表中的位置与既有引用因此得以保留。
func (m *Manager) UpdateTunnel(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHosts ...string) (models.TunnelInfo, error) {
	tm, remoteHost, name, err := normalizeTunnelConfig(node, name, mode, localPort, remoteHost, remotePort)
	if err != nil {
		return models.TunnelInfo{}, err
	}
	old, err := m.getTunnel(id)
	if err != nil {
		return models.TunnelInfo{}, err
	}
	if tm == TunnelLocal && old.mode == TunnelLocal && (len(listenHosts) == 0 || strings.TrimSpace(listenHosts[0]) == "") {
		listenHosts = []string{old.listenHost}
	}
	listenHost, err := tunnelListenHost(tm, listenHosts)
	if err != nil {
		return models.TunnelInfo{}, err
	}
	wasRunning := old.Status() == TunnelRunning
	logx.Debugf("[sshx] 更新隧道配置: id=%s wasRunning=%v newMode=%s name=%s local=%d remote=%s:%d",
		id, wasRunning, tm, name, localPort, remoteHost, remotePort)
	old.Stop()

	t := newTunnel(id, name, tm, node, localPort, remoteHost, remotePort, m.tunnelStatusNotifier())
	t.listenHost = listenHost
	m.mu.Lock()
	m.tunnels[id] = t
	m.mu.Unlock()
	// 启动失败时保留新配置的停止态条目，避免隧道从列表中消失。
	if wasRunning {
		if err := t.Start(); err != nil {
			logx.Debugf("[sshx] 隧道按新配置重启失败: id=%s err=%v", id, err)
			return t.Info(), err
		}
	}
	logx.Infof("[sshx] 隧道配置已更新: id=%s name=%s mode=%s running=%v", id, name, tm, wasRunning)
	return t.Info(), nil
}

func (m *Manager) tunnelStatusNotifier() func(string, TunnelStatus, string) {
	return func(id string, status TunnelStatus, message string) {
		m.notify("tunnel:status", models.TunnelStatusEvent{ID: id, Status: string(status), Message: message})
	}
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

// RestartTunnel 启动已停止的隧道，或重新建立运行中的隧道。
func (m *Manager) RestartTunnel(id string) error {
	t, err := m.getTunnel(id)
	if err != nil {
		return err
	}
	if t.Status() == TunnelRunning {
		t.Stop()
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
		logx.Warnf("[sshx] 移除隧道失败(不存在): id=%s", id)
		return fmt.Errorf(i18n.T("隧道不存在: %s"), id)
	}
	t.Stop()
	return nil
}

// ListTunnels 返回全部隧道摘要，按创建顺序（ID）排序，避免列表顺序在刷新之间跳动。
func (m *Manager) ListTunnels() []models.TunnelInfo {
	m.mu.RLock()
	infos := make([]models.TunnelInfo, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		infos = append(infos, t.Info())
	}
	m.mu.RUnlock()
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

func (m *Manager) getTunnel(id string) (*Tunnel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tunnels[id]
	if !ok {
		return nil, fmt.Errorf(i18n.T("隧道不存在: %s"), id)
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
	c := m.sysinfo[sessionID]
	delete(m.sysinfo, sessionID)
	delete(m.sessions, sessionID)
	// 会话级实时刷新只在本次终端有效：关闭会话即失效，下次连接回到全局设置。
	delete(m.sysInfoRealtime, sessionID)
	m.mu.Unlock()
	logx.Debugf("[sshx] 会话已从管理器移除: session=%s collectors=%v remainingSessions=%d",
		sessionID, c != nil, len(m.sessions))
	if c != nil {
		c.Stop()
	}
}

// ---- SysInfo Dashboard ----

// StartSysInfoCollector 为已连接会话启动系统指标采集。
//
// 只有应用设置允许自动采集（sysInfoCollectEnabled，默认开启）时才启动；关闭时
// 登录服务器不会向远端执行任何采集命令（底层直接返回，并顺手停掉可能残留的采集器）。
func (m *Manager) StartSysInfoCollector(sessionID string) error {
	if !m.currentSettings().SysInfoCollectEnabled {
		logx.Infof("[sshx] 系统指标自动采集已关闭，跳过启动: session=%s", sessionID)
		m.mu.Lock()
		c, ok := m.sysinfo[sessionID]
		if ok {
			delete(m.sysinfo, sessionID)
		}
		m.mu.Unlock()
		if ok {
			c.Stop()
		}
		return nil
	}
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if existing, ok := m.sysinfo[sessionID]; ok {
		m.mu.Unlock()
		logx.Debugf("[sshx] 系统指标采集器已存在，复用: session=%s", sessionID)
		return existing.Start()
	}
	c := newSysInfoCollector(sessionID, s.client,
		func(snap models.SysInfoSnapshot) {
			m.notify("sysinfo:snapshot:"+sessionID, snap)
		},
		func(msg string) {
			m.notify("sysinfo:snapshot:"+sessionID, models.SysInfoSnapshot{
				SessionID:   sessionID,
				Error:       msg,
				CollectedAt: time.Now().UnixMilli(),
			})
		},
	)
	c.realtime = func() bool { return m.sysInfoRealtimeFor(sessionID) }
	m.sysinfo[sessionID] = c
	m.mu.Unlock()
	return c.Start()
}

// SetSysInfoRealtime 设置指定会话的实时刷新状态（仅本次会话有效，不写全局设置）。
// 终端关闭（会话移除）时自动清除；重连同一个终端仍保留。
func (m *Manager) SetSysInfoRealtime(sessionID string, enabled bool) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	m.mu.Lock()
	m.sysInfoRealtime[sessionID] = enabled
	m.mu.Unlock()
	logx.Infof("[sshx] 系统指标实时刷新已切换: session=%s enabled=%v", sessionID, enabled)
}

// sysInfoRealtimeFor 返回会话的有效实时刷新状态：会话级覆盖优先，否则用全局设置。
func (m *Manager) sysInfoRealtimeFor(sessionID string) bool {
	m.mu.RLock()
	override, ok := m.sysInfoRealtime[sessionID]
	m.mu.RUnlock()
	if ok {
		return override
	}
	return m.currentSettings().SysInfoRealtimeEnabled
}

// StopSysInfoCollector 停止系统指标采集。
func (m *Manager) StopSysInfoCollector(sessionID string) error {
	m.mu.Lock()
	c, ok := m.sysinfo[sessionID]
	if ok {
		delete(m.sysinfo, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		logx.Debugf("[sshx] 停止系统指标采集未命中: session=%s", sessionID)
		return nil
	}
	logx.Infof("[sshx] 停止系统指标采集: session=%s", sessionID)
	c.Stop()
	return nil
}

// SetSysInfoIdle 切换采集频率（前台 3s / 后台 10s）。
func (m *Manager) SetSysInfoIdle(sessionID string, idle bool) {
	m.mu.RLock()
	c := m.sysinfo[sessionID]
	m.mu.RUnlock()
	if c != nil {
		logx.Debugf("[sshx] 切换采集空闲频率: session=%s idle=%v", sessionID, idle)
		c.SetIdle(idle)
	}
}

// StopAllSysInfoCollectors 停止全部会话的系统指标采集。
// 用于「登录后自动采集」开关被关闭时立即生效：已连接的会话也不再向远端执行任何采集命令。
func (m *Manager) StopAllSysInfoCollectors() {
	m.mu.Lock()
	collectors := make([]*SysInfoCollector, 0, len(m.sysinfo))
	for id, c := range m.sysinfo {
		collectors = append(collectors, c)
		delete(m.sysinfo, id)
	}
	m.mu.Unlock()
	logx.Infof("[sshx] 停止全部系统指标采集: collectors=%d", len(collectors))
	for _, c := range collectors {
		c.Stop()
	}
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
