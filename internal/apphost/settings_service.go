package apphost

import (
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshconfig"
	"ding-ssh/internal/sshx"
)

// SettingsService 负责服务器节点配置、保密柜、凭证、分组、文件选择及偏好设置。
type SettingsService struct {
	app *App
}

func NewSettingsService(app *App) *SettingsService {
	return &SettingsService{app: app}
}

func (s *SettingsService) GetServers() []models.ServerNode {
	return s.app.GetServers()
}

func (s *SettingsService) SaveServer(node models.ServerNode) (models.ServerNode, error) {
	return s.app.SaveServer(node)
}

func (s *SettingsService) DeleteServer(id string) error {
	return s.app.DeleteServer(id)
}

func (s *SettingsService) TestServer(node models.ServerNode) models.ServerTestResult {
	return s.app.TestServer(node)
}

func (s *SettingsService) TestServers(nodes []models.ServerNode) []models.ServerTestResult {
	return s.app.TestServers(nodes)
}

func (s *SettingsService) TestServerLogin(node models.ServerNode) models.ServerTestResult {
	return s.app.TestServerLogin(node)
}

func (s *SettingsService) TestServerLoginAuthenticated(node models.ServerNode, phrases map[string]string) models.ServerTestResult {
	return s.app.TestServerLoginAuthenticated(node, phrases)
}

func (s *SettingsService) GetSettings() models.Settings {
	return s.app.GetSettings()
}

func (s *SettingsService) SaveSettings(settings models.Settings) error {
	return s.app.SaveSettings(settings)
}

func (s *SettingsService) GetCredentials() []models.Credential {
	return s.app.GetCredentials()
}

func (s *SettingsService) SaveCredential(cred models.Credential) (models.Credential, error) {
	return s.app.SaveCredential(cred)
}

func (s *SettingsService) DeleteCredential(id string) error {
	return s.app.DeleteCredential(id)
}

func (s *SettingsService) GetGroups() []string {
	return s.app.GetGroups()
}

func (s *SettingsService) AddGroup(path string) error {
	return s.app.AddGroup(path)
}

func (s *SettingsService) RemoveGroup(path string, keepChildren bool) error {
	return s.app.RemoveGroup(path, keepChildren)
}

func (s *SettingsService) RenameGroup(oldPath, newLeaf string) error {
	return s.app.RenameGroup(oldPath, newLeaf)
}

func (s *SettingsService) GetSecurityStatus() models.SecurityStatus {
	return s.app.GetSecurityStatus()
}

func (s *SettingsService) UnlockWithMasterPassword(password string) error {
	return s.app.UnlockWithMasterPassword(password)
}

func (s *SettingsService) EnableMasterPassword(password string) error {
	return s.app.EnableMasterPassword(password)
}

func (s *SettingsService) DisableMasterPassword(password string) error {
	return s.app.DisableMasterPassword(password)
}

func (s *SettingsService) ChangeMasterPassword(oldPassword, newPassword string) error {
	return s.app.ChangeMasterPassword(oldPassword, newPassword)
}

func (s *SettingsService) GetHostTrust() ([]sshx.HostTrustRecord, error) {
	return s.app.GetHostTrust()
}

func (s *SettingsService) DeleteHostTrust(line int, fingerprint string) error {
	return s.app.DeleteHostTrust(line, fingerprint)
}

func (s *SettingsService) ResolveHostTrust(id string, approve, replace bool) error {
	return s.app.ResolveHostTrust(id, approve, replace)
}

func (s *SettingsService) ExportConfig(passphrase string) (string, error) {
	return s.app.ExportConfig(passphrase)
}

func (s *SettingsService) ImportConfig(passphrase string, overwrite bool) (models.ImportConfigResult, error) {
	return s.app.ImportConfig(passphrase, overwrite)
}

func (s *SettingsService) PreviewSSHConfig(path string) ([]sshconfig.Preview, error) {
	return s.app.PreviewSSHConfig(path)
}

func (s *SettingsService) SelectSSHConfig() (string, error) {
	return s.app.SelectSSHConfig()
}

func (s *SettingsService) ImportSSHConfig(path string, choices []SSHImportChoice) (int, error) {
	return s.app.ImportSSHConfig(path, choices)
}

func (s *SettingsService) AddCommandHistory(serverID, command string) {
	s.app.AddCommandHistory(serverID, command)
}

func (s *SettingsService) ClearCommandHistory(serverID string) error {
	return s.app.ClearCommandHistory(serverID)
}

func (s *SettingsService) QueryCommandHistory(serverID, prefix string, limit int) []models.CommandSuggestion {
	return s.app.QueryCommandHistory(serverID, prefix, limit)
}

func (s *SettingsService) SelectLocalFile() (string, error) {
	return s.app.SelectLocalFile()
}

func (s *SettingsService) SelectLocalFiles() ([]string, error) {
	return s.app.SelectLocalFiles()
}

func (s *SettingsService) SelectLocalFolder() (string, error) {
	return s.app.SelectLocalFolder()
}

func (s *SettingsService) SelectSavePath(defaultName string) (string, error) {
	return s.app.SelectSavePath(defaultName)
}

func (s *SettingsService) SelectSaveFolder() (string, error) {
	return s.app.SelectSaveFolder()
}

func (s *SettingsService) SelectKeyFile() (string, error) {
	return s.app.SelectKeyFile()
}

func (s *SettingsService) SetUILanguage(language string) {
	s.app.SetUILanguage(language)
}

func (s *SettingsService) SetWindowContentProtection(enabled bool) {
	s.app.SetWindowContentProtection(enabled)
}

func (s *SettingsService) RegisterGlobalShortcut(shortcut string) error {
	return s.app.RegisterGlobalShortcut(shortcut)
}

func (s *SettingsService) UnregisterGlobalShortcut(shortcut string) error {
	return s.app.UnregisterGlobalShortcut(shortcut)
}
