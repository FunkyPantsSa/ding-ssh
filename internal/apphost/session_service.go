package apphost

import (
	"ding-ssh/internal/localterm"
	"ding-ssh/internal/models"
)

// SessionService 负责 SSH 会话与本机终端的生命周期、数据流与终端控制。
type SessionService struct {
	app *App
}

func NewSessionService(app *App) *SessionService {
	return &SessionService{app: app}
}

func (s *SessionService) Connect(sessionID string, node models.ServerNode, cols, rows int) (models.ConnectResult, error) {
	return s.app.Connect(sessionID, node, cols, rows)
}

func (s *SessionService) ConnectAuthenticated(id string, node models.ServerNode, cols, rows int, phrases map[string]string) (models.ConnectResult, error) {
	return s.app.ConnectAuthenticated(id, node, cols, rows, phrases)
}

func (s *SessionService) ConnectLocal(sessionID string, cols, rows int) (models.ConnectResult, error) {
	return s.app.ConnectLocal(sessionID, cols, rows)
}

func (s *SessionService) Reconnect(sessionID string, cols, rows int) (models.ConnectResult, error) {
	return s.app.Reconnect(sessionID, cols, rows)
}

func (s *SessionService) Disconnect(sessionID string) error {
	return s.app.Disconnect(sessionID)
}

func (s *SessionService) DisconnectAll() {
	s.app.DisconnectAll()
}

func (s *SessionService) ListSessions() []models.SessionInfo {
	return s.app.ListSessions()
}

func (s *SessionService) Write(sessionID, data string) error {
	return s.app.Write(sessionID, data)
}

func (s *SessionService) WriteInteractive(sessionID, data, command string) error {
	return s.app.WriteInteractive(sessionID, data, command)
}

func (s *SessionService) WriteRaw(sessionID string, data string) error {
	return s.app.WriteRaw(sessionID, []byte(data))
}

func (s *SessionService) WriteRawInteractive(sessionID string, data, command string) error {
	return s.app.WriteRawInteractive(sessionID, []byte(data), command)
}

func (s *SessionService) Resize(sessionID string, cols, rows int) error {
	return s.app.Resize(sessionID, cols, rows)
}

func (s *SessionService) SubmitSudoPassword(sessionID, requestID, password string, useSaved bool) error {
	return s.app.SubmitSudoPassword(sessionID, requestID, password, useSaved)
}

func (s *SessionService) CancelSudoPrompt(sessionID, requestID string) error {
	return s.app.CancelSudoPrompt(sessionID, requestID)
}

func (s *SessionService) GetLocalShellOptions() []localterm.ShellOption {
	return s.app.GetLocalShellOptions()
}

func (s *SessionService) GetAgentIdentities(node models.ServerNode) ([]AgentIdentity, error) {
	return s.app.GetAgentIdentities(node)
}

func (s *SessionService) StartSysInfoCollector(sessionID string) error {
	return s.app.StartSysInfoCollector(sessionID)
}

func (s *SessionService) StopSysInfoCollector(sessionID string) error {
	return s.app.StopSysInfoCollector(sessionID)
}

func (s *SessionService) SetSysInfoIdle(sessionID string, idle bool) {
	s.app.SetSysInfoIdle(sessionID, idle)
}

func (s *SessionService) SetSysInfoRealtime(sessionID string, enabled bool) error {
	return s.app.SetSysInfoRealtime(sessionID, enabled)
}

func (s *SessionService) OpenTerminalWindow(sessionID, title string) error {
	return s.app.OpenTerminalWindow(sessionID, title)
}
