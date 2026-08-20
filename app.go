package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"ding-ssh/internal/store"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是 Wails 绑定到前端的应用对象。
type App struct {
	ctx         context.Context
	manager     *sshx.Manager
	db          *sql.DB // SQLite 数据库句柄（应用退出时关闭）
	store       store.Store
	settings    store.SettingsStore
	credentials store.CredentialStore
	groups      store.GroupStore
	history     store.HistoryStore
	vault       *cryptox.Vault

	unlockMu       sync.Mutex
	unlockFails    int
	unlockLockedUntil time.Time
}

// NewApp 创建 App 实例。
func NewApp() *App {
	return &App{}
}

// startup 在应用启动时初始化依赖。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.manager = sshx.NewManager(func(eventName string, payload interface{}) {
		runtime.EventsEmit(a.ctx, eventName, payload)
	})

	// 安全保险库（主密钥 / 主密码）
	configDir := filepath.Join(func() string {
		d, err := os.UserConfigDir()
		if err != nil {
			return "."
		}
		return d
	}(), "ding-ssh")
	vault, err := cryptox.OpenVault(configDir)
	if err != nil {
		logx.Errorf("初始化安全保险库失败: %v", err)
	} else {
		a.vault = vault
	}

	// 存储层：优先 SQLite（含旧版 JSON 数据一次性迁移），失败时回退 JSON。
	db, err := store.OpenSQLite(store.DefaultSQLitePath())
	if err != nil {
		logx.Errorf("初始化 SQLite 存储失败，回退 JSON: %v", err)
		a.initJSONStores()
		return
	}
	a.db = db
	if err := store.MigrateLegacyJSON(db); err != nil {
		logx.Errorf("迁移旧版 JSON 数据到 SQLite 失败: %v", err)
	}
	sqlStore := store.NewSQLiteStore(db)
	sqlCreds := store.NewSQLiteCredentialStore(db)
	if a.vault != nil && a.vault.Status().Unlocked {
		sqlStore.SetCipher(a.vault)
		sqlCreds.SetCipher(a.vault)
		if a.vault.NeedsMigration() {
			if n, err := store.MigratePlaintextSecrets(db, a.vault); err != nil {
				logx.Errorf("明文敏感字段加密迁移失败: %v", err)
			} else {
				_ = a.vault.MarkMigrated()
				if n > 0 {
					logx.Infof("已加密迁移 %d 条敏感字段", n)
				}
			}
		}
	}
	a.store = sqlStore
	a.settings = store.NewSQLiteSettingsStore(db)
	a.credentials = sqlCreds
	a.groups = store.NewSQLiteGroupStore(db)
	if err := store.EnsureHistorySchema(db); err != nil {
		logx.Errorf("初始化命令历史表失败: %v", err)
		a.history = store.NoopHistoryStore{}
	} else {
		a.history = store.NewSQLiteHistoryStore(db)
	}

	// 加载设置并应用日志开关（默认关闭）。
	settings, err := a.settings.Get()
	if err != nil {
		logx.Errorf("读取设置失败: %v", err)
	}
	logx.SetEnabled(settings.LogEnabled)
	logx.Infof("应用启动，日志开关: %v", settings.LogEnabled)

	// 注入设置读取器，供 Manager 在建立会话时获取最新配置。
	a.manager.SetSettingsGetter(func() models.Settings {
		s, _ := a.settings.Get()
		return s
	})
}

func (a *App) attachCipher() {
	if a.vault == nil || !a.vault.Status().Unlocked {
		return
	}
	if s, ok := a.store.(*store.SQLiteStore); ok {
		s.SetCipher(a.vault)
	}
	if c, ok := a.credentials.(*store.SQLiteCredentialStore); ok {
		c.SetCipher(a.vault)
	}
}

func (a *App) ensureUnlocked() error {
	if a.vault == nil {
		return nil
	}
	st := a.vault.Status()
	if st.NeedsUnlock || !st.Unlocked {
		return fmt.Errorf("请先解锁主密码")
	}
	return nil
}

// initJSONStores 使用 JSON 文件存储兜底（SQLite 不可用时）。
func (a *App) initJSONStores() {
	js, err := store.NewJSONStore(store.DefaultPath())
	if err != nil {
		logx.Errorf("初始化配置存储失败: %v", err)
		js, _ = store.NewJSONStore("servers.json")
	}
	a.store = js

	settingsStore, err := store.NewJSONSettingsStore(store.DefaultSettingsPath())
	if err != nil {
		logx.Errorf("初始化设置存储失败: %v", err)
		settingsStore, _ = store.NewJSONSettingsStore("")
	}
	a.settings = settingsStore
	settings, err := a.settings.Get()
	if err != nil {
		logx.Errorf("读取设置失败: %v", err)
	}
	logx.SetEnabled(settings.LogEnabled)
	logx.Infof("应用启动（JSON 兜底），日志开关: %v", settings.LogEnabled)

	credentialStore, err := store.NewJSONCredentialStore(store.DefaultCredentialPath())
	if err != nil {
		logx.Errorf("初始化凭证存储失败: %v", err)
		credentialStore, _ = store.NewJSONCredentialStore("")
	}
	a.credentials = credentialStore

	groupStore, err := store.NewJSONGroupStore(store.DefaultGroupPath())
	if err != nil {
		logx.Errorf("初始化分组存储失败: %v", err)
		groupStore, _ = store.NewJSONGroupStore("")
	}
	a.groups = groupStore
	a.history = store.NoopHistoryStore{}
}

// shutdown 在应用退出时清理资源。
func (a *App) shutdown(ctx context.Context) {
	if a.manager != nil {
		a.manager.CloseAll()
	}
	if a.db != nil {
		_ = a.db.Close()
		a.db = nil
	}
}

// ---- 服务器节点管理 ----

// GetServers 返回已保存的服务器节点列表。
func (a *App) GetServers() []models.ServerNode {
	if err := a.ensureUnlocked(); err != nil {
		return []models.ServerNode{}
	}
	nodes, err := a.store.List()
	if err != nil {
		logx.Errorf("读取服务器列表失败: %v", err)
		return []models.ServerNode{}
	}
	return nodes
}

// SaveServer 新增或更新服务器节点，ID 为空时自动生成。
func (a *App) SaveServer(node models.ServerNode) (models.ServerNode, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.ServerNode{}, err
	}
	if node.ID == "" {
		node.ID = uuid.NewString()
	}
	if node.Port == 0 {
		node.Port = 22
	}
	if err := a.store.Save(node); err != nil {
		return models.ServerNode{}, fmt.Errorf("保存服务器失败: %w", err)
	}
	return node, nil
}

// SelectKeyFile 弹出系统文件选择框，返回私钥文件路径。
func (a *App) SelectKeyFile() (string, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 SSH 私钥文件",
	})
	if err != nil {
		return "", err
	}
	return file, nil
}

// SelectImageFile 弹出系统文件选择框，返回背景图片路径。
func (a *App) SelectImageFile() (string, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "选择背景图片",
		Filters: []runtime.FileFilter{{DisplayName: "图片文件", Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.webp"}},
	})
	if err != nil {
		return "", err
	}
	return file, nil
}

// DeleteServer 删除服务器节点。
func (a *App) DeleteServer(id string) error {
	if err := a.store.Delete(id); err != nil {
		return fmt.Errorf("删除服务器失败: %w", err)
	}
	return nil
}

// TestServer 测试单台服务器在线状态（TCP 延迟 + SSH 端口连通性）。
func (a *App) TestServer(node models.ServerNode) models.ServerTestResult {
	res := sshx.TestConnectivity(node.Host, node.Port)
	res.NodeID = node.ID
	return res
}

// TestServers 并发测试多台服务器在线状态，结果按传入顺序返回。
func (a *App) TestServers(nodes []models.ServerNode) []models.ServerTestResult {
	results := make([]models.ServerTestResult, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, n models.ServerNode) {
			defer wg.Done()
			res := sshx.TestConnectivity(n.Host, n.Port)
			res.NodeID = n.ID
			results[i] = res
		}(i, node)
	}
	wg.Wait()
	return results
}

// ---- 设置管理 ----

// GetSettings 返回应用设置。
func (a *App) GetSettings() models.Settings {
	settings, err := a.settings.Get()
	if err != nil {
		logx.Errorf("读取设置失败: %v", err)
		return models.Settings{}
	}
	return settings
}

// SaveSettings 保存应用设置并立即生效（无需重启）。
func (a *App) SaveSettings(settings models.Settings) error {
	if err := a.settings.Save(settings); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}
	logx.SetEnabled(settings.LogEnabled)
	logx.Infof("设置已更新，日志开关: %v", settings.LogEnabled)
	// 立即同步所有活跃会话的心跳开关
	a.manager.SetKeepAliveEnabled(settings.KeepAliveEnabled)
	return nil
}

// ---- 凭证管理 ----

// GetCredentials 返回已保存的常用凭证列表。
func (a *App) GetCredentials() []models.Credential {
	if err := a.ensureUnlocked(); err != nil {
		return []models.Credential{}
	}
	list, err := a.credentials.List()
	if err != nil {
		logx.Errorf("读取凭证列表失败: %v", err)
		return []models.Credential{}
	}
	return list
}

// SaveCredential 新增或更新常用凭证，ID 为空时自动生成。
func (a *App) SaveCredential(c models.Credential) (models.Credential, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.Credential{}, err
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if err := a.credentials.Save(c); err != nil {
		return models.Credential{}, fmt.Errorf("保存凭证失败: %w", err)
	}
	return c, nil
}

// DeleteCredential 删除常用凭证。
func (a *App) DeleteCredential(id string) error {
	if err := a.credentials.Delete(id); err != nil {
		return fmt.Errorf("删除凭证失败: %w", err)
	}
	return nil
}

// ---- SFTP ----

// SftpList 列出指定会话的远程目录。
func (a *App) SftpList(sessionID, path string) ([]models.SFTPEntry, error) {
	return a.manager.SftpList(sessionID, path)
}

// SelectLocalFile 选择本地文件（用于 SFTP 上传）。
func (a *App) SelectLocalFile() (string, error) {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
	})
	if err != nil {
		return "", err
	}
	return file, nil
}

// SelectLocalFiles 弹出系统文件选择框（支持多选），返回所选文件路径列表（用于 SFTP 批量上传）。
func (a *App) SelectLocalFiles() ([]string, error) {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传的文件（可多选）",
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// SelectSavePath 选择本地保存路径（用于 SFTP 下载）。
func (a *App) SelectSavePath(defaultName string) (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存到本地",
		DefaultFilename: defaultName,
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// SftpUpload 上传本地文件到远程路径（含进度事件）。
func (a *App) SftpUpload(sessionID, localPath, remotePath string) error {
	if err := a.manager.SftpUpload(sessionID, localPath, remotePath); err != nil {
		logx.Errorf("SFTP 上传失败: session=%s local=%s remote=%s err=%v", sessionID, localPath, remotePath, err)
		return err
	}
	return nil
}

// SftpDownload 下载远程文件到本地路径（含进度事件）。
func (a *App) SftpDownload(sessionID, remotePath, localPath string) error {
	if err := a.manager.SftpDownload(sessionID, remotePath, localPath); err != nil {
		logx.Errorf("SFTP 下载失败: session=%s remote=%s local=%s err=%v", sessionID, remotePath, localPath, err)
		return err
	}
	return nil
}

// SftpCancelTransfer 取消指定会话中正在进行的 SFTP 上传/下载。
func (a *App) SftpCancelTransfer(sessionID, direction, name string) error {
	if err := a.manager.SftpCancelTransfer(sessionID, direction, name); err != nil {
		logx.Debugf("SFTP 取消传输未命中: session=%s direction=%s name=%s err=%v", sessionID, direction, name, err)
		return err
	}
	logx.Debugf("SFTP 传输已取消: session=%s direction=%s name=%s", sessionID, direction, name)
	return nil
}

// SftpRename 重命名远程文件或目录。
func (a *App) SftpRename(sessionID, oldPath, newPath string) error {
	if err := a.manager.SftpRename(sessionID, oldPath, newPath); err != nil {
		logx.Errorf("SFTP 重命名失败: session=%s old=%s new=%s err=%v", sessionID, oldPath, newPath, err)
		return err
	}
	return nil
}

// SftpMkdir 在远程路径新建目录。
func (a *App) SftpMkdir(sessionID, path string) error {
	if err := a.manager.SftpMkdir(sessionID, path); err != nil {
		logx.Errorf("SFTP 新建目录失败: session=%s path=%s err=%v", sessionID, path, err)
		return err
	}
	return nil
}

// SftpRemove 删除远程文件或目录（目录递归删除）。
func (a *App) SftpRemove(sessionID, path string) error {
	if err := a.manager.SftpRemove(sessionID, path); err != nil {
		logx.Errorf("SFTP 删除失败: session=%s path=%s err=%v", sessionID, path, err)
		return err
	}
	return nil
}

// ---- SSH 隧道 ----

// StartTunnel 创建并启动一条 SSH 隧道（mode: local | remote | dynamic）。
func (a *App) StartTunnel(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.TunnelInfo{}, err
	}
	info, err := a.manager.StartTunnel(node, name, mode, localPort, remoteHost, remotePort)
	if err != nil {
		logx.Errorf("启动 SSH 隧道失败: mode=%s name=%s local=%d remote=%s:%d err=%v", mode, name, localPort, remoteHost, remotePort, err)
		return models.TunnelInfo{}, err
	}
	return info, nil
}

// UpdateTunnel 修改已有隧道的配置，运行中的隧道会按新配置重启。
func (a *App) UpdateTunnel(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	if err := a.ensureUnlocked(); err != nil {
		return models.TunnelInfo{}, err
	}
	info, err := a.manager.UpdateTunnel(id, node, name, mode, localPort, remoteHost, remotePort)
	if err != nil {
		logx.Errorf("修改 SSH 隧道失败: id=%s mode=%s name=%s local=%d remote=%s:%d err=%v", id, mode, name, localPort, remoteHost, remotePort, err)
		return info, err
	}
	return info, nil
}

// StopTunnel 停止指定隧道（保留条目，可重新启动）。
func (a *App) StopTunnel(id string) error {
	if err := a.manager.StopTunnel(id); err != nil {
		logx.Errorf("停止 SSH 隧道失败: id=%s err=%v", id, err)
		return err
	}
	return nil
}

// RestartTunnel 重新启动已停止/异常的隧道。
func (a *App) RestartTunnel(id string) error {
	if err := a.manager.RestartTunnel(id); err != nil {
		logx.Errorf("重启 SSH 隧道失败: id=%s err=%v", id, err)
		return err
	}
	return nil
}

// RemoveTunnel 停止并移除指定隧道。
func (a *App) RemoveTunnel(id string) error {
	if err := a.manager.RemoveTunnel(id); err != nil {
		logx.Errorf("移除 SSH 隧道失败: id=%s err=%v", id, err)
		return err
	}
	return nil
}

// ListTunnels 返回全部隧道摘要。
func (a *App) ListTunnels() []models.TunnelInfo {
	return a.manager.ListTunnels()
}

// ---- 分组管理 ----

// GetGroups 返回全部分组：手动创建的分组与服务器节点使用的分组取并集。
func (a *App) GetGroups() []string {
	set := map[string]struct{}{}
	if stored, err := a.groups.List(); err != nil {
		logx.Errorf("读取分组失败: %v", err)
	} else {
		for _, g := range stored {
			set[g] = struct{}{}
		}
	}
	if nodes, err := a.store.List(); err != nil {
		logx.Errorf("读取服务器列表失败: %v", err)
	} else {
		for _, n := range nodes {
			if g := strings.TrimSpace(n.Group); g != "" {
				set[g] = struct{}{}
			}
		}
	}
	groups := make([]string, 0, len(set))
	for g := range set {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

// AddGroup 手动新增分组。
func (a *App) AddGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("分组名称不能为空")
	}
	if err := a.groups.Add(name); err != nil {
		return fmt.Errorf("新增分组失败: %w", err)
	}
	return nil
}

// RenameGroup 重命名分组，并同步更新该分组下的服务器节点。
func (a *App) RenameGroup(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("分组名称不能为空")
	}
	if oldName == newName {
		return nil
	}
	if err := a.groups.Rename(oldName, newName); err != nil {
		return fmt.Errorf("重命名分组失败: %w", err)
	}
	nodes, err := a.store.List()
	if err != nil {
		return fmt.Errorf("读取服务器列表失败: %w", err)
	}
	for _, n := range nodes {
		if n.Group == oldName {
			n.Group = newName
			if err := a.store.Save(n); err != nil {
				logx.Errorf("更新分组下的服务器失败: id=%s err=%v", n.ID, err)
			}
		}
	}
	return nil
}

// RemoveGroup 删除分组，该分组下的服务器变为未分组。
func (a *App) RemoveGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("分组名称不能为空")
	}
	if err := a.groups.Remove(name); err != nil {
		return fmt.Errorf("删除分组失败: %w", err)
	}
	nodes, err := a.store.List()
	if err != nil {
		return fmt.Errorf("读取服务器列表失败: %w", err)
	}
	for _, n := range nodes {
		if n.Group == name {
			n.Group = ""
			if err := a.store.Save(n); err != nil {
				logx.Errorf("清除分组下的服务器失败: id=%s err=%v", n.ID, err)
			}
		}
	}
	return nil
}

// ---- SSH 会话管理 ----

// Connect 建立 SSH 终端会话。sessionID 由前端生成并提前订阅进度事件，
// 为空时后端自动生成。
func (a *App) Connect(sessionID string, node models.ServerNode, cols, rows int) (models.ConnectResult, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = uuid.NewString()
	}
	if node.Port == 0 {
		node.Port = 22
	}
	if err := a.manager.Connect(sessionID, node, cols, rows); err != nil {
		return models.ConnectResult{}, err
	}
	return models.ConnectResult{SessionID: sessionID, Server: node.Name}, nil
}

// Disconnect 断开指定会话。
func (a *App) Disconnect(sessionID string) error {
	return a.manager.Disconnect(sessionID)
}

// Reconnect 重新建立已断开的 SSH 会话，复用原会话配置与回调。
func (a *App) Reconnect(sessionID string, cols, rows int) (models.ConnectResult, error) {
	if err := a.manager.Reconnect(sessionID, cols, rows); err != nil {
		return models.ConnectResult{}, err
	}
	return models.ConnectResult{SessionID: sessionID}, nil
}

// Write 向会话写入终端输入（data 为 base64 编码）。
func (a *App) Write(sessionID, data string) error {
	return a.manager.Write(sessionID, data)
}

// Resize 调整会话终端尺寸。
func (a *App) Resize(sessionID string, cols, rows int) error {
	return a.manager.Resize(sessionID, cols, rows)
}

// ListSessions 返回当前活动会话列表。
func (a *App) ListSessions() []models.SessionInfo {
	return a.manager.List()
}

// Reconnect 重新建立已断开的 SSH 会话，复用原会话配置与回调。
// ---- Phase 2: SFTP 与 Shell 双向同步 ----

// SetSftpPathFromTerminal 当 Shell 侧检测到目录变更时，强制更新 SFTP 面板路径。
func (a *App) SetSftpPathFromTerminal(sessionID, path string) error {
	return a.manager.SetSftpPathFromTerminal(sessionID, path)
}

// SyncSftpToTerminal 在 SFTP 面板双击文件夹时，向终端发送 cd 命令实现目录同步。
func (a *App) SyncSftpToTerminal(sessionID, path string) error {
	return a.manager.SyncSftpToTerminal(sessionID, path)
}

// ---- Phase 3: 命令历史 ----

// AddCommandHistory 记录一条已执行命令（写库失败静默忽略，不影响终端）。
func (a *App) AddCommandHistory(serverID, command string) {
	if a.history == nil {
		return
	}
	if err := a.history.Add(serverID, command); err != nil {
		logx.Debugf("写入命令历史失败: server=%s err=%v", serverID, err)
	}
}

// ClearCommandHistory 清理命令历史；serverID 为空则清空全部。
func (a *App) ClearCommandHistory(serverID string) error {
	if a.history == nil {
		return nil
	}
	if err := a.history.Clear(serverID); err != nil {
		logx.Errorf("清理命令历史失败: %v", err)
		return err
	}
	return nil
}

// QueryCommandHistory 按前缀查询高频历史命令，供智能补全使用。
func (a *App) QueryCommandHistory(serverID, prefix string, limit int) []models.CommandSuggestion {
	if a.history == nil {
		return []models.CommandSuggestion{}
	}
	if limit <= 0 {
		limit = 8
	}
	list, err := a.history.Query(serverID, prefix, limit)
	if err != nil {
		logx.Debugf("查询命令历史失败: server=%s err=%v", serverID, err)
		return []models.CommandSuggestion{}
	}
	return list
}

// ---- Phase 3: 本地文件读写（Zmodem） ----

// ReadLocalFileBase64 读取本地文件并返回 base64 内容。
func (a *App) ReadLocalFileBase64(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("文件路径为空")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取本地文件失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// WriteLocalFileBase64 将 base64 内容写入本地文件。
func (a *App) WriteLocalFileBase64(path, dataBase64 string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("文件路径为空")
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return fmt.Errorf("解码文件内容失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入本地文件失败: %w", err)
	}
	return nil
}

// ---- Phase 4: 安全 / SysInfo / dingpack ----

// GetSecurityStatus 返回加密保险库状态。
func (a *App) GetSecurityStatus() models.SecurityStatus {
	if a.vault == nil {
		return models.SecurityStatus{Unlocked: true, KeyringAvailable: false}
	}
	st := a.vault.Status()
	return models.SecurityStatus{
		Unlocked:              st.Unlocked,
		MasterPasswordEnabled: st.MasterPasswordEnabled,
		KeyringAvailable:      st.KeyringAvailable,
		NeedsUnlock:           st.NeedsUnlock,
	}
}

// UnlockWithMasterPassword 使用主密码解锁（错误三次短锁 30s）。
func (a *App) UnlockWithMasterPassword(password string) error {
	if a.vault == nil {
		return errors.New("保险库未初始化")
	}
	a.unlockMu.Lock()
	if time.Now().Before(a.unlockLockedUntil) {
		remain := int(time.Until(a.unlockLockedUntil).Seconds())
		a.unlockMu.Unlock()
		return fmt.Errorf("尝试过多，请 %d 秒后再试", remain)
	}
	a.unlockMu.Unlock()

	if err := a.vault.Unlock(password); err != nil {
		a.unlockMu.Lock()
		a.unlockFails++
		if a.unlockFails >= 3 {
			a.unlockLockedUntil = time.Now().Add(30 * time.Second)
			a.unlockFails = 0
		}
		a.unlockMu.Unlock()
		return err
	}
	a.unlockMu.Lock()
	a.unlockFails = 0
	a.unlockMu.Unlock()
	a.attachCipher()
	if a.db != nil && a.vault.NeedsMigration() {
		if _, err := store.MigratePlaintextSecrets(a.db, a.vault); err == nil {
			_ = a.vault.MarkMigrated()
		}
	}
	return nil
}

// EnableMasterPassword 开启启动主密码。
func (a *App) EnableMasterPassword(password string) error {
	if a.vault == nil {
		return errors.New("保险库未初始化")
	}
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	oldKey, newKey, err := a.vault.EnableMasterPassword(password)
	if err != nil {
		return err
	}
	if a.db != nil {
		if err := store.ReencryptAllSecrets(a.db, oldKey, newKey); err != nil {
			return fmt.Errorf("重加密敏感字段失败: %w", err)
		}
	}
	a.attachCipher()
	return nil
}

// DisableMasterPassword 关闭主密码（需验证当前密码）。
func (a *App) DisableMasterPassword(password string) error {
	if a.vault == nil {
		return errors.New("保险库未初始化")
	}
	oldKey, newKey, err := a.vault.DisableMasterPassword(password)
	if err != nil {
		return err
	}
	if a.db != nil {
		if err := store.ReencryptAllSecrets(a.db, oldKey, newKey); err != nil {
			return fmt.Errorf("重加密敏感字段失败: %w", err)
		}
	}
	a.attachCipher()
	return nil
}

// ChangeMasterPassword 更换主密码。
func (a *App) ChangeMasterPassword(oldPassword, newPassword string) error {
	if a.vault == nil {
		return errors.New("保险库未初始化")
	}
	oldKey, newKey, err := a.vault.ChangeMasterPassword(oldPassword, newPassword)
	if err != nil {
		return err
	}
	if a.db != nil {
		if err := store.ReencryptAllSecrets(a.db, oldKey, newKey); err != nil {
			return fmt.Errorf("重加密敏感字段失败: %w", err)
		}
	}
	a.attachCipher()
	return nil
}

// StartSysInfoCollector 启动会话系统监控采集。
func (a *App) StartSysInfoCollector(sessionID string) error {
	return a.manager.StartSysInfoCollector(sessionID)
}

// StopSysInfoCollector 停止会话系统监控采集。
func (a *App) StopSysInfoCollector(sessionID string) error {
	return a.manager.StopSysInfoCollector(sessionID)
}

// SetSysInfoIdle 切换监控采集频率（后台降频）。
func (a *App) SetSysInfoIdle(sessionID string, idle bool) {
	a.manager.SetSysInfoIdle(sessionID, idle)
}

// ExportConfig 导出加密 .dingpack 到用户选择的路径。
func (a *App) ExportConfig(passphrase string) (string, error) {
	if err := a.ensureUnlocked(); err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出配置包",
		DefaultFilename: "ding-ssh.dingpack",
		Filters:         []runtime.FileFilter{{DisplayName: "dingpack", Pattern: "*.dingpack"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	servers, err := a.store.List()
	if err != nil {
		return "", err
	}
	creds, err := a.credentials.List()
	if err != nil {
		return "", err
	}
	groups, _ := a.groups.List()
	if err := store.WriteDingpackFile(path, passphrase, servers, creds, groups); err != nil {
		return "", err
	}
	return path, nil
}

// ImportConfig 从用户选择的 .dingpack 导入配置；overwrite=true 时同 ID 覆盖。
func (a *App) ImportConfig(passphrase string, overwrite bool) (models.ImportConfigResult, error) {
	var empty models.ImportConfigResult
	if err := a.ensureUnlocked(); err != nil {
		return empty, err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "导入配置包",
		Filters: []runtime.FileFilter{{DisplayName: "dingpack", Pattern: "*.dingpack"}},
	})
	if err != nil || path == "" {
		return empty, err
	}
	payload, err := store.ReadDingpackFile(path, passphrase)
	if err != nil {
		return empty, err
	}
	existingServers, _ := a.store.List()
	existIDs := map[string]bool{}
	for _, s := range existingServers {
		existIDs[s.ID] = true
	}
	result := models.ImportConfigResult{}
	for _, s := range payload.Servers {
		if s.ID == "" {
			s.ID = uuid.NewString()
		}
		if existIDs[s.ID] && !overwrite {
			continue
		}
		if err := a.store.Save(s); err != nil {
			return result, err
		}
		result.Servers++
	}
	existingCreds, _ := a.credentials.List()
	existCred := map[string]bool{}
	for _, c := range existingCreds {
		existCred[c.ID] = true
	}
	for _, c := range payload.Credentials {
		if c.ID == "" {
			c.ID = uuid.NewString()
		}
		if existCred[c.ID] && !overwrite {
			continue
		}
		if err := a.credentials.Save(c); err != nil {
			return result, err
		}
		result.Credentials++
	}
	for _, g := range payload.Groups {
		if g == "" {
			continue
		}
		_ = a.groups.Add(g)
		result.Groups++
	}
	return result, nil
}

