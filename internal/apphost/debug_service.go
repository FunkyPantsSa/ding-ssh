package apphost

import (
	"ding-ssh/internal/debugsrv"
)

// DebugService 负责 HTTP 调试服务、MCP 控制面、审计日志与两段式安全确认。
type DebugService struct {
	app *App
}

func NewDebugService(app *App) *DebugService {
	return &DebugService{app: app}
}

func (s *DebugService) DebugInfo() map[string]any {
	return s.app.DebugInfo()
}

func (s *DebugService) DebugReply(id string, payload string) {
	s.app.DebugReply(id, payload)
}

func (s *DebugService) ApplyDebugPreset(mode string) error {
	return s.app.ApplyDebugPreset(mode)
}

func (s *DebugService) CreateDebugClient(policyJSON string) (map[string]any, error) {
	return s.app.CreateDebugClient(policyJSON)
}

func (s *DebugService) ListDebugClients() []debugsrv.ClientPolicy {
	return s.app.ListDebugClients()
}

func (s *DebugService) RevokeDebugClient(prefix string) {
	s.app.RevokeDebugClient(prefix)
}

func (s *DebugService) GetCapabilities() map[string]any {
	return s.app.GetCapabilities()
}

func (s *DebugService) SetCapability(capName string, enabled bool) error {
	return s.app.SetCapability(capName, enabled)
}

func (s *DebugService) GetAuditRecords(limit int, capName, source string) []map[string]any {
	return s.app.GetAuditRecords(limit, capName, source)
}

func (s *DebugService) UndoAuditRecord(id string, tool, args string) error {
	return s.app.UndoAuditRecord(id, tool, args)
}

func (s *DebugService) GetLogInfo() map[string]any {
	return s.app.GetLogInfo()
}

func (s *DebugService) ReadLogTail(lines int) map[string]any {
	return s.app.ReadLogTail(lines)
}

func (s *DebugService) OpenLogFolder() error {
	return s.app.OpenLogFolder()
}

func (s *DebugService) ClearLogs() map[string]any {
	return s.app.ClearLogs()
}

func (s *DebugService) SetLogOptions(opts map[string]any) error {
	return s.app.SetLogOptions(opts)
}

func (s *DebugService) SetLogTrace(tabId string, enabled bool) error {
	return s.app.SetLogTrace(tabId, enabled)
}

func (s *DebugService) ExportDiagnostics() map[string]any {
	return s.app.ExportDiagnostics()
}

func (s *DebugService) LogClient(level, message string) {
	s.app.LogClient(level, message)
}
