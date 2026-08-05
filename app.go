package main

import (
	"context"
	"fmt"
	"strings"

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
	store       store.Store
	settings    store.SettingsStore
	credentials store.CredentialStore
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
	js, err := store.NewJSONStore(store.DefaultPath())
	if err != nil {
		logx.Errorf("初始化配置存储失败: %v", err)
		js, _ = store.NewJSONStore("servers.json")
	}
	a.store = js

	// 加载设置并应用日志开关（默认关闭）。
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
	logx.Infof("应用启动，日志开关: %v", settings.LogEnabled)

	// 加载凭证存储。
	credentialStore, err := store.NewJSONCredentialStore(store.DefaultCredentialPath())
	if err != nil {
		logx.Errorf("初始化凭证存储失败: %v", err)
		credentialStore, _ = store.NewJSONCredentialStore("")
	}
	a.credentials = credentialStore
}

// shutdown 在应用退出时清理资源。
func (a *App) shutdown(ctx context.Context) {
	if a.manager != nil {
		a.manager.CloseAll()
	}
}

// ---- 服务器节点管理 ----

// GetServers 返回已保存的服务器节点列表。
func (a *App) GetServers() []models.ServerNode {
	nodes, err := a.store.List()
	if err != nil {
		logx.Errorf("读取服务器列表失败: %v", err)
		return []models.ServerNode{}
	}
	return nodes
}

// SaveServer 新增或更新服务器节点，ID 为空时自动生成。
func (a *App) SaveServer(node models.ServerNode) (models.ServerNode, error) {
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
	return nil
}

// ---- 凭证管理 ----

// GetCredentials 返回已保存的常用凭证列表。
func (a *App) GetCredentials() []models.Credential {
	list, err := a.credentials.List()
	if err != nil {
		logx.Errorf("读取凭证列表失败: %v", err)
		return []models.Credential{}
	}
	return list
}

// SaveCredential 新增或更新常用凭证，ID 为空时自动生成。
func (a *App) SaveCredential(c models.Credential) (models.Credential, error) {
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
