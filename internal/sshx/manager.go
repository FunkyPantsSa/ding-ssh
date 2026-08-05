package sshx

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
)

// ErrSessionNotFound 会话不存在。
var ErrSessionNotFound = errors.New("会话不存在")

// Notifier 事件通知回调，由上层（App）注入，实现与 Wails runtime 解耦。
type Notifier func(eventName string, payload interface{})

// Manager 会话管理器：维护全部活动的 SSH 会话。
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	notify   Notifier
}

// NewManager 创建会话管理器。
func NewManager(notify Notifier) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		notify:   notify,
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
	infos, err := client.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("读取远程目录失败: %w", err)
	}
	base := path
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

// CloseAll 关闭全部会话（应用退出时调用）。
func (m *Manager) CloseAll() {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()
	for _, s := range sessions {
		s.close(models.StatusClosed, "应用退出")
	}
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
