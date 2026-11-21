package localterm

import (
	"ding-ssh/internal/i18n"
	"fmt"
	"sync"
	"time"

	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
)

// 本地终端输入/输出是逐键、逐块的最高频路径：只采样记录字节数，绝不记录内容。
var (
	localWriteLog    = logx.NewSampler(128, 2*time.Second)
	localWriteErrLog = logx.NewSampler(32, 5*time.Second)
	localResizeLog   = logx.NewSampler(32, 2*time.Second)
	localGetLog      = logx.NewSampler(64, 2*time.Second)
)

// Notifier 事件通知回调（与 sshx.Notifier 同形）。
type Notifier func(eventName string, payload interface{})

// ShellPrefGetter 读取本地 Shell 偏好。
type ShellPrefGetter func() string

// Manager 管理本机终端会话。
type Manager struct {
	lifecycle sync.Mutex // Serialize same-ID replacements and shutdown.
	mu        sync.RWMutex
	sessions  map[string]*Session
	notify    Notifier
	getShell  ShellPrefGetter
}

// NewManager 创建本地终端管理器。
func NewManager(notify Notifier) *Manager {
	logx.Debugf("[localterm] 创建本地终端管理器")
	return &Manager{
		sessions: make(map[string]*Session),
		notify:   notify,
	}
}

// SetShellPrefGetter 注入设置中的本地 Shell 偏好读取函数。
func (m *Manager) SetShellPrefGetter(fn ShellPrefGetter) {
	logx.Debugf("[localterm] 注入本地 Shell 偏好读取函数: enabled=%v", fn != nil)
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

// SessionName reports the shell actually started for this session.
func (m *Manager) SessionName(sessionID string) string {
	s, err := m.get(sessionID)
	if err != nil {
		return ShellTitle("")
	}
	return ShellTitle(s.label)
}

// Connect 启动本机终端会话。shellPref 为空时使用设置中的默认值。
func (m *Manager) Connect(sessionID string, shellPref string, cols, rows int) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	start := time.Now()
	if sessionID == "" {
		logx.Warnf("[localterm] 连接本地终端被拒绝: sessionID 为空")
		return fmt.Errorf("%s", i18n.T("sessionID 不能为空"))
	}
	if shellPref == "" {
		shellPref = m.shellPref()
		logx.Debugf("[localterm] 未指定 Shell，使用设置默认值: session=%s shell=%s", sessionID, shellPref)
	}
	logx.Debugf("[localterm] 发起本地终端连接: session=%s shell=%s size=%dx%d", sessionID, shellPref, cols, rows)

	m.mu.Lock()
	if old, ok := m.sessions[sessionID]; ok {
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		logx.Warnf("[localterm] 本地终端会话被新连接替换: session=%s", sessionID)
		old.close(models.StatusClosed, i18n.T("被新连接替换"))
		m.mu.Lock()
	}
	m.mu.Unlock()

	var s *Session
	var err error
	s, err = newSession(
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
			m.remove(id, s)
		},
	)
	if err != nil {
		logx.Errorf("[localterm] 创建本地终端失败: session=%s shell=%s err=%v", sessionID, shellPref, err)
		m.notify("ssh:status:"+sessionID, models.StatusEvent{
			SessionID: sessionID,
			Status:    models.StatusError,
			Message:   err.Error(),
		})
		return err
	}

	m.mu.Lock()
	m.sessions[sessionID] = s
	count := len(m.sessions)
	m.mu.Unlock()

	m.notify("ssh:status:"+sessionID, models.StatusEvent{
		SessionID: sessionID,
		Status:    models.StatusConnected,
		Message:   "",
	})
	s.start()
	logx.Infof("[localterm] 本地终端连接成功: session=%s shell=%s size=%dx%d sessions=%d elapsed=%s",
		sessionID, shellPref, cols, rows, count, time.Since(start))
	return nil
}

// Write 向本机终端写入输入（base64）。
func (m *Manager) Write(sessionID, dataBase64 string) error {
	s, err := m.get(sessionID)
	if err != nil {
		logx.WarnSampled(localWriteErrLog, "[localterm] 写入本地终端失败，会话不存在: session=%s", sessionID)
		return err
	}
	data, err := decodeBase64(dataBase64)
	if err != nil {
		logx.WarnSampled(localWriteErrLog, "[localterm] 本地终端输入 base64 解码失败: session=%s inputLen=%d err=%v",
			sessionID, len(dataBase64), err)
		return err
	}
	err = s.Write(data)
	if err != nil {
		logx.WarnSampled(localWriteErrLog, "[localterm] 写入本地终端失败: session=%s bytes=%d err=%v", sessionID, len(data), err)
		return err
	}
	// 逐键输入最高频：采样记录方向与字节数，绝不记录输入内容。
	logx.DebugSampled(localWriteLog, "[localterm] 本地终端输入: session=%s bytes=%d", sessionID, len(data))
	return nil
}

// Resize 调整本机终端尺寸。
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	s, err := m.get(sessionID)
	if err != nil {
		logx.WarnSampled(localResizeLog, "[localterm] 调整本地终端尺寸失败，会话不存在: session=%s", sessionID)
		return err
	}
	if err := s.Resize(cols, rows); err != nil {
		logx.WarnSampled(localResizeLog, "[localterm] 调整本地终端尺寸失败: session=%s size=%dx%d err=%v",
			sessionID, cols, rows, err)
		return err
	}
	logx.DebugSampled(localResizeLog, "[localterm] 本地终端尺寸已调整: session=%s size=%dx%d", sessionID, cols, rows)
	return nil
}

// Disconnect 关闭本机终端。
func (m *Manager) Disconnect(sessionID string) error {
	s, err := m.get(sessionID)
	if err != nil {
		logx.Warnf("[localterm] 断开本地终端失败，会话不存在: session=%s err=%v", sessionID, err)
		return err
	}
	logx.Infof("[localterm] 用户断开本地终端: session=%s", sessionID)
	s.close(models.StatusClosed, i18n.T("用户主动断开"))
	return nil
}

// CloseAll 关闭全部本机终端。
func (m *Manager) CloseAll() {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	logx.Infof("[localterm] 关闭全部本地终端: count=%d", len(list))
	for _, s := range list {
		s.close(models.StatusClosed, i18n.T("应用退出"))
	}
}

func (m *Manager) get(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		logx.DebugSampled(localGetLog, "[localterm] 本地会话不存在: session=%s", sessionID)
		return nil, fmt.Errorf("%s", i18n.T("本地会话不存在"))
	}
	return s, nil
}

func (m *Manager) remove(id string, expected *Session) {
	m.mu.Lock()
	if m.sessions[id] == expected {
		delete(m.sessions, id)
	}
	count := len(m.sessions)
	m.mu.Unlock()
	logx.Debugf("[localterm] 本地终端已从管理器移除: session=%s remaining=%d", id, count)
}
