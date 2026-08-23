package localterm

import (
	"fmt"
	"sync"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
)

// Notifier 事件通知回调（与 sshx.Notifier 同形）。
type Notifier func(eventName string, payload interface{})

// ShellPrefGetter 读取本地 Shell 偏好。
type ShellPrefGetter func() string

// Manager 管理本机终端会话。
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	notify   Notifier
	getShell ShellPrefGetter
}

// NewManager 创建本地终端管理器。
func NewManager(notify Notifier) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		notify:   notify,
	}
}

// SetShellPrefGetter 注入设置中的本地 Shell 偏好读取函数。
func (m *Manager) SetShellPrefGetter(fn ShellPrefGetter) {
	m.getShell = fn
}

func (m *Manager) shellPref() string {
	if m.getShell != nil {
		return m.getShell()
	}
	return DefaultShell()
}

// Has 判断会话是否为本机终端。
func (m *Manager) Has(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sessions[sessionID]
	return ok
}

// Connect 启动本机终端会话。shellPref 为空时使用设置中的默认值。
func (m *Manager) Connect(sessionID string, shellPref string, cols, rows int) error {
	if sessionID == "" {
		return fmt.Errorf("sessionID 不能为空")
	}
	if shellPref == "" {
		shellPref = m.shellPref()
	}
	logx.Debugf("发起本地终端连接: session=%s shell=%s size=%dx%d", sessionID, shellPref, cols, rows)

	m.mu.Lock()
	if old, ok := m.sessions[sessionID]; ok {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		old.close(models.StatusClosed, "被新连接替换")
		m.mu.Lock()
	}
	m.mu.Unlock()

	s, err := newSession(
		sessionID,
		shellPref,
		cols,
		rows,
		func(id string, data []byte) {
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
	)
	if err != nil {
		m.notify("ssh:status:"+sessionID, models.StatusEvent{
			SessionID: sessionID,
			Status:    models.StatusError,
			Message:   err.Error(),
		})
		return err
	}

	m.mu.Lock()
	m.sessions[sessionID] = s
	m.mu.Unlock()

	m.notify("ssh:status:"+sessionID, models.StatusEvent{
		SessionID: sessionID,
		Status:    models.StatusConnected,
		Message:   "",
	})
	return nil
}

// Write 向本机终端写入输入（base64）。
func (m *Manager) Write(sessionID, dataBase64 string) error {
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

// Resize 调整本机终端尺寸。
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	return s.Resize(cols, rows)
}

// Disconnect 关闭本机终端。
func (m *Manager) Disconnect(sessionID string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	s.close(models.StatusClosed, "用户主动断开")
	return nil
}

// CloseAll 关闭全部本机终端。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	for _, s := range list {
		s.close(models.StatusClosed, "应用退出")
	}
}

func (m *Manager) get(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("本地会话不存在")
	}
	return s, nil
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}
