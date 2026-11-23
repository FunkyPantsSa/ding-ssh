package apphost

import (
	"context"
	"database/sql"
	"ding-ssh/internal/autostart"
	"ding-ssh/internal/i18n"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"os"
	"reflect"
	"runtime"
	rdebug "runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/localterm"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"ding-ssh/internal/store"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"strconv"
)

// App 是 Wails 绑定到前端的应用对象。
type App struct {
	desktop       *application.App
	window        *application.WebviewWindow
	tray          *application.SystemTray
	notifications *notifications.NotificationService
	dock          *dock.DockService
	closePending  atomic.Bool
	quitting      atomic.Bool
	debugDialog  func(approvalDialogOptions) (string, error)
	updates      updateState

	ctx         context.Context
	manager     *sshx.Manager
	local       *localterm.Manager
	db          *sql.DB // SQLite 数据库句柄（应用退出时关闭）
	store       store.Store
	settings    store.SettingsStore
	credentials store.CredentialStore
	groups      store.GroupStore
	history     store.HistoryStore
	vault       *cryptox.Vault
	vaultErr    error // Initialization failure must never enable plaintext fallback.

	unlockMu          sync.Mutex
	unlockFails       int
	unlockLockedUntil time.Time

	// traceMu / traceTabs 需要全量跟踪输出的会话（设置里的 LogTraceTabs 的内存快照）。
	// 终端输出是高频事件，回调里不能每次查库，因此只在启动 / 保存设置时刷新这份快照。
	traceMu   sync.RWMutex
	traceTabs map[string]bool

	debugBoot DebugBoot        // 启动期确定的调试模式配置（CDP 端口需在 app.Run 前决定）
	debug     *debugController // 调试模式运行时（未开启时为 nil）

	// 审计 / 两段式确认（见 internal/debugsrv）：由 App 持有而不是 debugController，
	// 因为「设置 → AI 记录」页签与 SetCapability 前端开关在调试模式关闭时也要工作。
	audits   *debugsrv.AuditLog
	confirms *debugsrv.ConfirmManager

	// startedAt 应用启动时刻（毫秒），供 app.health 计算已运行时长。
	startedAt int64
	// dataDir 数据目录（SQLite / 设置所在目录，用于 app.health 展示）。
	dataDir string
}

// 高频路径限流器（apphost 内的绑定方法）：
//   - runtimeStatusLog：运行时状态轮询（每秒级）只按时间采样；
//   - terminalWriteLog：终端输入转发只看长度，绝不记录内容；
//   - terminalResizeLog：前端 resize 与滚动同属高频 RPC；
//   - historyQueryLog：命令补全按前缀查询，输入时高频触发；
//   - eventPushLog：事件推送回调（终端输出 / 进度 / 状态）按次数与时间双重限流。
var (
	runtimeStatusLog  = logx.NewSampler(0, 3*time.Second)
	terminalWriteLog  = logx.NewSampler(0, 2*time.Second)
	terminalResizeLog = logx.NewSampler(8, 2*time.Second)
	historyQueryLog   = logx.NewSampler(16, 2*time.Second)
	eventPushLog      = logx.NewSampler(512, 2*time.Second)
)

// NewApp 创建 App 实例。boot 为启动期读到的调试模式配置（见 ReadDebugBoot）。
func NewApp(boot DebugBoot) *App {
	// token 属于敏感值，只记录是否已生成与调试模式开关，绝不记录 token 本身。
	logx.Debugf("[apphost] 创建应用实例: debugEnabled=%v debugPort=%d bindLan=%v cdpEnabled=%v",
		boot.Enabled, boot.Port, boot.BindLAN, boot.CDPEnabled)
	return &App{debugBoot: boot}
}

// startup 在应用启动时初始化依赖。
func (a *App) startup(ctx context.Context) {
	start := time.Now()
	a.ctx = ctx
	a.startedAt = time.Now().UnixMilli()
	a.dataDir = appdir.ConfigDir()
	logx.Infof("[apphost] 应用启动: version=%s dataDir=%q pid=%d", appVersion(), a.dataDir, os.Getpid())
	// 审计 + 两段式确认：先于 startDebug 创建，保证启动早期的调用也被记录
	// （调试模式关闭时它们仍然被前端「AI 记录」页签使用）。
	a.audits = debugsrv.NewAuditLog()
	a.confirms = debugsrv.NewConfirmManager(debugsrv.DefaultConfirmTTL)
	logx.Debugf("[apphost] 审计日志与两段式确认管理器已初始化")
	notify := func(eventName string, payload interface{}) {
		// 事件推送是高频路径：只采样记录事件名，不记录 payload 内容。
		logx.DebugSampled(eventPushLog, "[apphost] 推送事件: event=%s", eventName)
		a.emit(eventName, payload)
		// 调试模式：同一份事件同时投递给 SSE 订阅者（GET /v1/stream）
		if a.debug != nil && a.debug.hub != nil {
			a.debug.hub.PublishRaw(eventName, payload)
		}
		// 日志：命中 LogTraceTabs 的会话，其输出旁路一份到日志（见 debug.go 的 traceEvent）
		a.traceEvent(eventName, payload)
	}
	a.manager = sshx.NewManager(notify)
	a.local = localterm.NewManager(notify)
	logx.Debugf("[apphost] SSH 会话管理器与本机终端管理器已初始化: elapsed=%s", time.Since(start))

	// 调试模式（可选）：HTTP 控制面 + 前端桥，供 AI / 自动化调用。
	a.startDebug()

	// 安全保险库（主密钥 / 主密码）
	vault, err := cryptox.OpenVault(a.dataDir)
	a.vaultErr = err
	if err != nil {
		logx.Errorf("[apphost] 初始化安全保险库失败: err=%v", err)
	} else {
		a.vault = vault
		logx.Debugf("[apphost] 安全保险库已打开: unlocked=%v masterPassword=%v needsUnlock=%v elapsed=%s",
			vault.Status().Unlocked, vault.Status().MasterPasswordEnabled, vault.Status().NeedsUnlock, time.Since(start))
	}

	// 存储层：优先 SQLite（含旧版 JSON 数据一次性迁移），失败时回退 JSON。
	db, err := store.OpenSQLite(store.DefaultSQLitePath())
	if err != nil {
		logx.Errorf("[apphost] 初始化 SQLite 存储失败，回退 JSON: err=%v", err)
		a.initJSONStores()
		return
	}
	a.db = db
	logx.Debugf("[apphost] SQLite 存储已打开: path=%q elapsed=%s", store.DefaultSQLitePath(), time.Since(start))
	if err := store.MigrateLegacyJSON(db); err != nil {
		logx.Errorf("[apphost] 迁移旧版 JSON 数据到 SQLite 失败: err=%v", err)
	}
	// 旧数据里含空段/首尾分隔符的分组路径做一次幂等规范化（正常数据零改动）。
	if err := store.NormalizeLegacyGroupPaths(db); err != nil {
		logx.Errorf("[apphost] 规范化旧分组路径失败（继续启动）: err=%v", err)
	}
	sqlStore := store.NewSQLiteStore(db)
	sqlCreds := store.NewSQLiteCredentialStore(db)
	if a.vault != nil && a.vault.Status().Unlocked {
		sqlStore.SetCipher(a.vault)
		sqlCreds.SetCipher(a.vault)
		if a.vault.NeedsMigration() {
			if n, err := store.MigratePlaintextSecrets(db, a.vault); err != nil {
				logx.Errorf("[apphost] 明文敏感字段加密迁移失败: err=%v", err)
			} else {
				_ = a.vault.MarkMigrated()
				if n > 0 {
					logx.Infof("[apphost] 明文敏感字段已加密迁移: count=%d", n)
				}
			}
		}
	}
	a.store = sqlStore
	a.settings = store.NewSQLiteSettingsStore(db)
	a.credentials = sqlCreds
	a.groups = store.NewSQLiteGroupTreeStore(db)
	if err := store.EnsureHistorySchema(db); err != nil {
		logx.Errorf("[apphost] 初始化命令历史表失败: err=%v", err)
		a.history = store.NoopHistoryStore{}
	} else {
		a.history = store.NewSQLiteHistoryStore(db)
	}
	logx.Debugf("[apphost] SQLite 存储层初始化完成: store=servers settings credentials groups history elapsed=%s",
		time.Since(start))

	// 加载设置并应用日志开关（控制台 / 文件 / 级别 / 调用日志）。
	settings, err := a.settings.Get()
	if err != nil {
		logx.Errorf("[apphost] 读取设置失败: err=%v", err)
	}
	a.applyLogSettings(settings)
	a.configureDebugAudit(settings.Debug)
	logx.Debugf("[apphost] 设置已加载并应用日志开关: elapsed=%s", time.Since(start))
	// 启动自检：把版本 / 日志目录 / 各项开关写进日志，便于排查「日志页没内容」
	a.logStartupSelfCheck(settings)

	// 注入设置读取器，供 Manager 在建立会话时获取最新配置。
	a.manager.SetSettingsGetter(func() models.Settings {
		s, _ := a.settings.Get()
		return s
	})
	a.local.SetShellPrefGetter(func() string {
		s, _ := a.settings.Get()
		return s.LocalShell
	})
	logx.Infof("[apphost] 应用启动完成: elapsed=%s goroutines=%d", time.Since(start), runtime.NumGoroutine())
}

func (a *App) attachCipher() {
	if a.vault == nil || !a.vault.Status().Unlocked {
		logx.Debugf("[apphost] 保险库未解锁，跳过注入字段加密器")
		return
	}
	attached := 0
	if s, ok := a.store.(*store.SQLiteStore); ok {
		s.SetCipher(a.vault)
		attached++
	}
	if c, ok := a.credentials.(*store.SQLiteCredentialStore); ok {
		c.SetCipher(a.vault)
		attached++
	}
	logx.Debugf("[apphost] 字段加密器已注入: stores=%d", attached)
}

func (a *App) ensureUnlocked() error {
	if a.vaultErr != nil {
		return fmt.Errorf("%s: %w", i18n.T("保险库未初始化"), a.vaultErr)
	}
	if a.vault == nil {
		return nil
	}
	st := a.vault.Status()
	if st.NeedsUnlock || !st.Unlocked {
		logx.Debugf("[apphost] 操作被拒（保险库未解锁）: needsUnlock=%v", st.NeedsUnlock)
		return fmt.Errorf("%s", i18n.T("请先解锁主密码"))
	}
	return nil
}

// initJSONStores 使用 JSON 文件存储兜底（SQLite 不可用时）。
func (a *App) initJSONStores() {
	start := time.Now()
	logx.Warnf("[apphost] 启用 JSON 文件存储兜底（SQLite 不可用）")
	js, err := store.NewJSONStore(store.DefaultPath())
	if err != nil {
		logx.Errorf("[apphost] 初始化配置存储失败: err=%v", err)
		js, _ = store.NewJSONStore("servers.json")
		logx.Warnf("[apphost] 配置存储回退到相对路径: path=servers.json")
	}
	a.store = js

	settingsStore, err := store.NewJSONSettingsStore(store.DefaultSettingsPath())
	if err != nil {
		logx.Errorf("[apphost] 初始化设置存储失败: err=%v", err)
		settingsStore, _ = store.NewJSONSettingsStore("settings.json")
		logx.Warnf("[apphost] 设置存储回退到相对路径: path=settings.json")
	}
	a.settings = settingsStore
	settings, err := a.settings.Get()
	if err != nil {
		logx.Errorf("[apphost] 读取设置失败: err=%v", err)
	}
	a.applyLogSettings(settings)
	a.logStartupSelfCheck(settings)
	logx.Infof("[apphost] 应用启动（JSON 兜底）: consoleLog=%v", settings.LogEnabled)

	credentialStore, err := store.NewJSONCredentialStore(store.DefaultCredentialPath())
	if err != nil {
		logx.Errorf("[apphost] 初始化凭证存储失败: err=%v", err)
		credentialStore, _ = store.NewJSONCredentialStore("credentials.json")
		logx.Warnf("[apphost] 凭证存储回退到相对路径: path=credentials.json")
	}
	a.credentials = credentialStore

	groupStore, err := store.NewJSONGroupStore(store.DefaultGroupPath())
	if err != nil {
		logx.Errorf("[apphost] 初始化分组存储失败: err=%v", err)
		groupStore, _ = store.NewJSONGroupStore("groups.json")
		logx.Warnf("[apphost] 分组存储回退到相对路径: path=groups.json")
	}
	a.groups = groupStore
	a.history = store.NoopHistoryStore{}
	logx.Infof("[apphost] 应用启动完成（JSON 兜底）: elapsed=%s goroutines=%d",
		time.Since(start), runtime.NumGoroutine())
}

// shutdown 在应用退出时清理资源。
func (a *App) shutdown() {
	start := time.Now()
	logx.Infof("[apphost] 应用开始退出: uptimeSec=%d", (time.Now().UnixMilli()-a.startedAt)/1000)
	// 完成日志最后执行（LIFO：先跑 installPendingUpdate，再记退出完成）。
	defer func() {
		logx.Infof("[apphost] 应用退出清理完成: elapsed=%s", time.Since(start))
	}()
	defer a.installPendingUpdate()
	a.stopDebug()
	if a.local != nil {
		a.local.CloseAll()
		logx.Debugf("[apphost] 本机终端会话已全部关闭")
	}
	if a.manager != nil {
		a.manager.CloseAll()
		logx.Debugf("[apphost] SSH 会话与隧道已全部关闭")
	}
	if a.db != nil {
		_ = a.db.Close()
		a.db = nil
		logx.Debugf("[apphost] SQLite 数据库已关闭")
	}
}

// ---- 服务器节点管理 ----

// GetServers 返回已保存的服务器节点列表。
func (a *App) GetServers() []models.ServerNode {
	if err := a.ensureUnlocked(); err != nil {
		logx.Debugf("[apphost] 读取服务器列表被拒: err=%v", err)
		return []models.ServerNode{}
	}
	nodes, err := a.store.List()
	if err != nil {
		logx.Errorf("[apphost] 读取服务器列表失败: err=%v", err)
		return []models.ServerNode{}
	}
	logx.Debugf("[apphost] 读取服务器列表: count=%d", len(nodes))
	return nodes
}

// SaveServer 新增或更新服务器节点，ID 为空时自动生成。
func (a *App) SaveServer(node models.ServerNode) (models.ServerNode, error) {
	start := time.Now()
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 保存服务器被拒（保险库未解锁）: name=%s err=%v", node.Name, err)
		return models.ServerNode{}, err
	}
	if node.ID == "" {
		node.ID = uuid.NewString()
	}
	if node.Port == 0 {
		node.Port = 22
	}
	if normalized, err := store.NormalizeLegacyGroupPath(node.Group); err == nil {
		node.Group = normalized
	}
	logx.Debugf("[apphost] 保存服务器: id=%s name=%s host=%s port=%d user=%s auth=%s jumps=%d",
		node.ID, node.Name, node.Host, node.Port, node.User, node.AuthType, len(node.JumpServerIDs))
	if _, err := a.resolveSSHNode(node); err != nil {
		logx.Warnf("[apphost] 保存服务器被拒（连接路径校验失败）: id=%s name=%s err=%v", node.ID, node.Name, err)
		return models.ServerNode{}, err
	}
	if err := a.store.Save(node); err != nil {
		logx.Errorf("[apphost] 保存服务器失败: id=%s name=%s host=%s err=%v", node.ID, node.Name, node.Host, err)
		return models.ServerNode{}, fmt.Errorf(i18n.T("保存服务器失败: %w"), err)
	}
	logx.Infof("[apphost] 保存服务器完成: id=%s name=%s host=%s port=%d elapsed=%s",
		node.ID, node.Name, node.Host, node.Port, time.Since(start))
	return node, nil
}

// SelectKeyFile 弹出系统文件选择框，返回私钥文件路径。
func (a *App) SelectKeyFile() (string, error) {
	logx.Debugf("[apphost] 打开 SSH 私钥文件选择对话框")
	file, err := a.openFile(application.OpenFileDialogOptions{
		Title: i18n.T("选择 SSH 私钥文件"),
	})
	if err != nil {
		logx.Warnf("[apphost] 打开 SSH 私钥文件选择对话框失败: err=%v", err)
		return "", err
	}
	logx.Debugf("[apphost] SSH 私钥文件选择结果: path=%q canceled=%v", file, file == "")
	return file, nil
}

// SelectImageFile 弹出系统文件选择框，返回背景图片路径。
func (a *App) SelectImageFile() (string, error) {
	logx.Debugf("[apphost] 打开背景图片选择对话框")
	file, err := a.openFile(application.OpenFileDialogOptions{
		Title:   i18n.T("选择背景图片"),
		Filters: []application.FileFilter{{DisplayName: i18n.T("图片文件"), Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.webp"}},
	})
	if err != nil {
		logx.Warnf("[apphost] 打开背景图片选择对话框失败: err=%v", err)
		return "", err
	}
	logx.Debugf("[apphost] 背景图片选择结果: path=%q canceled=%v", file, file == "")
	return file, nil
}

// DeleteServer 删除服务器节点。
func (a *App) DeleteServer(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	nodes, err := a.store.List()
	if err != nil {
		logx.Errorf("[apphost] 删除服务器失败（读取现有服务器）: id=%s err=%v", id, err)
		return err
	}
	targetName := ""
	for _, n := range nodes {
		if n.ID == id {
			targetName = n.Name
			break
		}
	}
	for _, n := range nodes {
		for _, jump := range n.JumpServerIDs {
			if jump == id {
				logx.Warnf("[apphost] 删除服务器被拒（被引用为跳板机）: id=%s refBy=%s", id, n.Name)
				return fmt.Errorf(i18n.T("服务器被 %s 引用为跳板机，请先修改其连接路径"), n.Name)
			}
		}
	}

	if err := a.store.Delete(id); err != nil {
		logx.Errorf("[apphost] 删除服务器失败: id=%s err=%v", id, err)
		return fmt.Errorf(i18n.T("删除服务器失败: %w"), err)
	}
	logx.Infof("[apphost] 删除服务器完成: id=%s name=%s", id, targetName)
	return nil
}

// TestServer 测试单台服务器在线状态（TCP 延迟 + SSH 端口连通性）。
func (a *App) TestServer(node models.ServerNode) models.ServerTestResult {
	if len(node.JumpServerIDs) > 0 {
		logx.Debugf("[apphost] 服务器含跳板机，改用登录测试: id=%s name=%s jumps=%d",
			node.ID, node.Name, len(node.JumpServerIDs))
		return a.TestServerLogin(node)
	}
	logx.Debugf("[apphost] 测试服务器 TCP 连通性: id=%s host=%s port=%d", node.ID, node.Host, node.Port)
	res := sshx.TestConnectivity(node.Host, node.Port)
	res.NodeID = node.ID
	if res.Reachable {
		logx.Debugf("[apphost] 服务器 TCP 连通性正常: id=%s host=%s port=%d latencyMs=%d",
			node.ID, node.Host, node.Port, res.LatencyMS)
	} else {
		logx.Warnf("[apphost] 服务器 TCP 连通性异常: id=%s host=%s port=%d err=%s",
			node.ID, node.Host, node.Port, res.Error)
	}
	return res
}

// TestServerLogin 使用节点配置实际完成一次 SSH 登录，验证用户名 / 密码 / 私钥能否通过鉴权
// （供新建 / 编辑服务器对话框的「测试连接」使用）。
func (a *App) TestServerLogin(node models.ServerNode) models.ServerTestResult {
	logx.Debugf("[apphost] 测试服务器登录: id=%s name=%s host=%s port=%d user=%s auth=%s jumps=%d",
		node.ID, node.Name, node.Host, node.Port, node.User, node.AuthType, len(node.JumpServerIDs))
	resolved, err := a.resolveSSHNode(node)
	if err != nil {
		logx.Warnf("[apphost] 测试服务器登录被拒（连接路径校验失败）: id=%s name=%s err=%v", node.ID, node.Name, err)
		return models.ServerTestResult{NodeID: node.ID, LatencyMS: -1, Error: err.Error()}
	}
	res := sshx.TestServerLogin(resolved)
	if res.Error != "" || !res.Reachable {
		logx.Warnf("[apphost] 测试服务器登录失败: id=%s host=%s user=%s reachable=%v err=%s",
			node.ID, node.Host, node.User, res.Reachable, res.Error)
	} else {
		logx.Infof("[apphost] 测试服务器登录成功: id=%s host=%s user=%s latencyMs=%d",
			node.ID, node.Host, node.User, res.LatencyMS)
	}
	return res
}

// TestServers 并发测试多台服务器在线状态，结果按传入顺序返回。
func (a *App) TestServers(nodes []models.ServerNode) []models.ServerTestResult {
	start := time.Now()
	logx.Debugf("[apphost] 批量测试服务器在线状态: count=%d", len(nodes))
	results := make([]models.ServerTestResult, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, n models.ServerNode) {
			defer wg.Done()
			res := a.TestServer(n)
			res.NodeID = n.ID
			results[i] = res
		}(i, node)
	}
	wg.Wait()
	ok, fail := 0, 0
	for _, res := range results {
		if res.Reachable && res.Error == "" {
			ok++
		} else {
			fail++
		}
	}
	logx.Infof("[apphost] 批量测试服务器完成: total=%d ok=%d fail=%d elapsed=%s",
		len(nodes), ok, fail, time.Since(start))
	return results
}

// ---- 设置管理 ----

// GetSettings 返回应用设置。
func (a *App) GetSettings() models.Settings {
	settings, err := a.settings.Get()
	if err != nil {
		logx.Errorf("[apphost] 读取设置失败: err=%v", err)
		return models.Settings{}
	}
	logx.Debugf("[apphost] 读取设置: consoleLog=%v fileLog=%v level=%s debugEnabled=%v",
		settings.LogEnabled, settings.LogToFile, settings.LogLevel, settings.Debug.Enabled)
	syncNativeAppearance(settings.Appearance.BaseTone)
	return settings
}

// SaveSettings 保存应用设置并立即生效（无需重启）。
func (a *App) SaveSettings(settings models.Settings) error {
	start := time.Now()
	logx.Debugf("[apphost] 保存设置开始: autoStart=%v keepAlive=%v sysInfoCollect=%v debugEnabled=%v logLevel=%s",
		settings.AutoStart, settings.KeepAliveEnabled, settings.SysInfoCollectEnabled,
		settings.Debug.Enabled, settings.LogLevel)
	previous, err := a.settings.Get()
	if err != nil {
		logx.Errorf("[apphost] 保存设置失败（读取旧设置）: err=%v", err)
		return err
	}
	if err := a.approveDebugSettingsChange(previous.Debug, settings.Debug); err != nil {
		return err
	}
	if settings.AutoStart != previous.AutoStart {
		logx.Debugf("[apphost] 切换开机自启: enabled=%v", settings.AutoStart)
		if a.desktop != nil && a.desktop.Autostart != nil {
			var aerr error
			if settings.AutoStart {
				aerr = a.desktop.Autostart.Enable()
			} else {
				aerr = a.desktop.Autostart.Disable()
			}
			if aerr != nil {
				logx.Errorf("[apphost] 设置开机自启失败: enabled=%v err=%v", settings.AutoStart, aerr)
				return aerr
			}
		} else {
			if err := autostart.Set(settings.AutoStart); err != nil {
				logx.Errorf("[apphost] 设置开机自启失败 (fallback): enabled=%v err=%v", settings.AutoStart, err)
				return err
			}
		}
	}

	if err := a.settings.Save(settings); err != nil {
		if settings.AutoStart != previous.AutoStart {
			var rerr error
			if a.desktop != nil && a.desktop.Autostart != nil {
				if previous.AutoStart {
					rerr = a.desktop.Autostart.Enable()
				} else {
					rerr = a.desktop.Autostart.Disable()
				}
			} else {
				rerr = autostart.Set(previous.AutoStart)
			}
			if rerr == nil {
				logx.Warnf("[apphost] 保存设置失败，开机自启已回滚: enabled=%v", previous.AutoStart)
			} else {
				logx.Warnf("[apphost] 保存设置失败且开机自启回滚失败: enabled=%v err=%v", previous.AutoStart, rerr)
			}
		}
		logx.Errorf("[apphost] 保存设置失败: err=%v", err)
		return fmt.Errorf(i18n.T("保存设置失败: %w"), err)
	}
	if !reflect.DeepEqual(previous.Debug, settings.Debug) && a.debug != nil && a.debug.srv != nil {
		a.debug.srv.InvalidateAuthorizations()
	}
	a.configureDebugAudit(settings.Debug)
	if !reflect.DeepEqual(previous.Debug, settings.Debug) && a.audits != nil {
		a.audits.Append(debugsrv.AuditRecord{Source: "ui", Tool: "authorization.settings", ClientID: "local-owner", OK: true})
	}
	syncNativeAppearance(settings.Appearance.BaseTone)
	// 日志四项开关（控制台 / 文件 / 级别 / 调用日志）立即生效
	a.applyLogSettings(settings)
	logx.Infof("[apphost] 设置已保存并生效: elapsed=%s consoleLog=%v fileLog=%v level=%s apiLog=%v keepAlive=%v sysInfoCollect=%v",
		time.Since(start), settings.LogEnabled, logx.FileEnabled(), logx.Level(), logx.APILogEnabled(),
		settings.KeepAliveEnabled, settings.SysInfoCollectEnabled)
	// 立即同步所有活跃会话的心跳开关
	a.manager.SetKeepAliveEnabled(settings.KeepAliveEnabled)
	// 「登录后自动采集」开关关闭时，立即停掉所有会话的远端采集（登录后不再执行采集命令）
	if !settings.SysInfoCollectEnabled {
		a.manager.StopAllSysInfoCollectors()
		logx.Debugf("[apphost] 已停止全部会话的远端系统信息采集")
	}
	// 调试模式开关 / 端口变化立即生效（CDP 端口需下次启动）
	a.reloadDebug(settings.Debug)
	return nil
}

// applyLogSettings 把设置里的日志开关同步到 logx（启动与保存设置共用，
// 保证「设置里的值」与「logx 运行时状态」永远一致）。
func (a *App) applyLogSettings(settings models.Settings) {
	// 控制台输出：沿用历史行为（LogEnabled 只管 stdout，不受文件开关影响）
	logx.SetEnabled(settings.LogEnabled)
	// 文件日志：打开失败（目录不可写等）只警告，不影响其余开关
	if err := logx.SetFileEnabled(settings.LogToFile); err != nil {
		logx.Warnf("[apphost] 开启文件日志失败: err=%v", err)
	}
	logx.SetLevel(settings.LogLevel)
	if err := logx.SetAPILogEnabled(settings.LogAPICalls); err != nil {
		logx.Warnf("[apphost] 设置调用日志开关失败: err=%v", err)
	}
	// 跟踪会话列表的内存快照，供 notify 旁路判断（见 debug.go 的 traceEvent）
	a.setTraceTabs(settings.LogTraceTabs)
	logx.Debugf("[apphost] 已应用日志设置: console=%v file=%v level=%s api=%v traceTabs=%d",
		logx.Enabled(), logx.FileEnabled(), logx.Level(), logx.APILogEnabled(), len(settings.LogTraceTabs))
}

// logStartupSelfCheck 启动自检：把版本、时间、日志目录与各项日志开关写进日志。
// 用户在「日志」页看不到内容时，先看这条就知道配置是否生效。
// 整条内容过一遍脱敏，避免环境信息里意外带上凭据。
func (a *App) logStartupSelfCheck(settings models.Settings) {
	msg := fmt.Sprintf("[apphost] 启动自检: version=%s time=%s logDir=%q consoleLog=%v fileLog=%v level=%s apiLog=%v debugMode=%v",
		appVersion(), time.Now().Format(time.RFC3339), logx.LogDir(),
		logx.Enabled(), logx.FileEnabled(), logx.Level(), logx.APILogEnabled(), settings.Debug.Enabled)
	logx.Infof("%s", logx.Redact(msg))
}

// appVersion 返回应用版本描述：优先取构建信息里的模块版本与 VCS 修订号，
// 拿不到（未写入构建信息 / 本地 go run）时返回 (devel)。
func appVersion() string {
	info, ok := rdebug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	parts := make([]string, 0, 2)
	if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
		parts = append(parts, v)
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			rev := s.Value
			if len(rev) > 8 {
				rev = rev[:8]
			}
			parts = append(parts, "rev="+rev)
			break
		}
	}
	if len(parts) == 0 {
		return "(devel)"
	}
	return strings.Join(parts, " ")
}

// ---- 凭证管理 ----

// GetCredentials 返回已保存的常用凭证列表。
func (a *App) GetCredentials() []models.Credential {
	if err := a.ensureUnlocked(); err != nil {
		logx.Debugf("[apphost] 读取凭证列表被拒: err=%v", err)
		return []models.Credential{}
	}
	list, err := a.credentials.List()
	if err != nil {
		logx.Errorf("[apphost] 读取凭证列表失败: err=%v", err)
		return []models.Credential{}
	}
	logx.Debugf("[apphost] 读取凭证列表: count=%d", len(list))
	return list
}

// SaveCredential 新增或更新常用凭证，ID 为空时自动生成。
// 日志只记 id / name / authType，绝不记录密码或私钥内容。
func (a *App) SaveCredential(c models.Credential) (models.Credential, error) {
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 保存凭证被拒（保险库未解锁）: name=%s err=%v", c.Name, err)
		return models.Credential{}, err
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	logx.Debugf("[apphost] 保存凭证: id=%s name=%s auth=%s user=%s", c.ID, c.Name, c.AuthType, c.User)
	if err := a.credentials.Save(c); err != nil {
		logx.Errorf("[apphost] 保存凭证失败: id=%s name=%s auth=%s err=%v", c.ID, c.Name, c.AuthType, err)
		return models.Credential{}, fmt.Errorf(i18n.T("保存凭证失败: %w"), err)
	}
	logx.Infof("[apphost] 保存凭证完成: id=%s name=%s auth=%s", c.ID, c.Name, c.AuthType)
	return c, nil
}

// DeleteCredential 删除常用凭证。
func (a *App) DeleteCredential(id string) error {
	if err := a.ensureUnlocked(); err != nil {
		return err
	}
	logx.Debugf("[apphost] 删除凭证: id=%s", id)
	if err := a.credentials.Delete(id); err != nil {
		logx.Errorf("[apphost] 删除凭证失败: id=%s err=%v", id, err)
		return fmt.Errorf(i18n.T("删除凭证失败: %w"), err)
	}
	logx.Infof("[apphost] 删除凭证完成: id=%s", id)
	return nil
}

// ---- SFTP ----

// SftpList 列出指定会话的远程目录。
func (a *App) SftpList(sessionID, path string) ([]models.SFTPEntry, error) {
	logx.Debugf("[apphost] 列出远端目录: session=%s path=%q", sessionID, path)
	entries, err := a.manager.SftpList(sessionID, path)
	if err != nil {
		logx.Errorf("[apphost] 列出远端目录失败: session=%s path=%q err=%v", sessionID, path, err)
		return nil, err
	}
	logx.Debugf("[apphost] 列出远端目录完成: session=%s path=%q count=%d", sessionID, path, len(entries))
	return entries, nil
}

// SelectLocalFile 选择本地文件（用于 SFTP 上传）。
func (a *App) SelectLocalFile() (string, error) {
	logx.Debugf("[apphost] 打开上传文件选择对话框")
	file, err := a.openFile(application.OpenFileDialogOptions{
		Title: i18n.T("选择要上传的文件"),
	})
	if err != nil {
		logx.Warnf("[apphost] 打开上传文件选择对话框失败: err=%v", err)
		return "", err
	}
	logx.Debugf("[apphost] 上传文件选择结果: path=%q canceled=%v", file, file == "")
	return file, nil
}

// SelectLocalFiles 弹出系统文件选择框（支持多选），返回所选文件路径列表（用于 SFTP 批量上传）。
func (a *App) SelectLocalFiles() ([]string, error) {
	logx.Debugf("[apphost] 打开批量上传文件选择对话框")
	files, err := a.openFiles(application.OpenFileDialogOptions{
		Title: i18n.T("选择要上传的文件（可多选）"),
	})
	if err != nil {
		logx.Warnf("[apphost] 打开批量上传文件选择对话框失败: err=%v", err)
		return nil, err
	}
	logx.Debugf("[apphost] 批量上传文件选择结果: count=%d", len(files))
	return files, nil
}

// SelectLocalFolder 弹出系统文件夹选择框，返回所选文件夹路径（用于 SFTP 上传文件夹）。
func (a *App) SelectLocalFolder() (string, error) {
	logx.Debugf("[apphost] 打开上传文件夹选择对话框")
	dir, err := a.openDirectory(application.OpenFileDialogOptions{
		Title: i18n.T("选择要上传的文件夹"),
	})
	if err != nil {
		logx.Warnf("[apphost] 打开上传文件夹选择对话框失败: err=%v", err)
		return "", err
	}
	logx.Debugf("[apphost] 上传文件夹选择结果: path=%q", dir)
	return dir, nil
}

// SelectSaveFolder 弹出系统文件夹选择框，返回所选保存位置（用于 SFTP 下载文件夹）。
func (a *App) SelectSaveFolder() (string, error) {
	logx.Debugf("[apphost] 打开下载保存文件夹选择对话框")
	dir, err := a.openDirectory(application.OpenFileDialogOptions{
		Title: i18n.T("选择保存到本地文件夹"),
	})
	if err != nil {
		logx.Warnf("[apphost] 打开下载保存文件夹选择对话框失败: err=%v", err)
		return "", err
	}
	logx.Debugf("[apphost] 下载保存文件夹选择结果: path=%q", dir)
	return dir, nil
}

// LocalPathIsDir 判断本地路径是否为文件夹（拖拽上传时前端据此分流文件 / 文件夹）。
func (a *App) LocalPathIsDir(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		logx.Warnf("[apphost] 判断本地路径类型失败（不存在或不可访问）: path=%q err=%v", path, err)
		return false, fmt.Errorf(i18n.T("本地路径不存在或不可访问: %s"), path)
	}
	logx.Debugf("[apphost] 本地路径类型: path=%q isDir=%v", path, fi.IsDir())
	return fi.IsDir(), nil
}

// SelectSavePath 选择本地保存路径（用于 SFTP 下载）。
func (a *App) SelectSavePath(defaultName string) (string, error) {
	logx.Debugf("[apphost] 打开本地保存路径对话框: defaultName=%q", defaultName)
	path, err := a.saveFile(application.SaveFileDialogOptions{
		Title:    i18n.T("保存到本地"),
		Filename: defaultName,
	})
	if err != nil {
		logx.Warnf("[apphost] 打开本地保存路径对话框失败: err=%v", err)
		return "", err
	}
	logx.Debugf("[apphost] 本地保存路径选择结果: path=%q canceled=%v", path, path == "")
	return path, nil
}

// SftpUpload 上传本地文件到远程路径（含进度事件）。
func (a *App) SftpUpload(sessionID, localPath, remotePath string) error {
	start := time.Now()
	logx.Infof("[apphost] SFTP 上传开始: session=%s local=%q remote=%q", sessionID, localPath, remotePath)
	if err := a.manager.SftpUpload(sessionID, localPath, remotePath); err != nil {
		logx.Errorf("[apphost] SFTP 上传失败: session=%s local=%s remote=%s err=%v", sessionID, localPath, remotePath, err)
		return err
	}
	logx.Infof("[apphost] SFTP 上传完成: session=%s local=%q remote=%q elapsed=%s",
		sessionID, localPath, remotePath, time.Since(start))
	return nil
}

// SftpUploadFolder 递归上传本地文件夹到远端目录（保留文件夹名与内部结构，整条传输带进度事件）。
func (a *App) SftpUploadFolder(sessionID, localDir, remoteDir string) error {
	start := time.Now()
	logx.Infof("[apphost] SFTP 上传文件夹开始: session=%s local=%q remote=%q", sessionID, localDir, remoteDir)
	if err := a.manager.SftpUploadFolder(sessionID, localDir, remoteDir); err != nil {
		logx.Errorf("[apphost] SFTP 上传文件夹失败: session=%s local=%s remote=%s err=%v", sessionID, localDir, remoteDir, err)
		return err
	}
	logx.Infof("[apphost] SFTP 上传文件夹完成: session=%s local=%q remote=%q elapsed=%s",
		sessionID, localDir, remoteDir, time.Since(start))
	return nil
}

// SftpDownload 下载远程文件到本地路径（含进度事件）。
func (a *App) SftpDownload(sessionID, remotePath, localPath string) error {
	start := time.Now()
	logx.Infof("[apphost] SFTP 下载开始: session=%s remote=%q local=%q", sessionID, remotePath, localPath)
	if err := a.manager.SftpDownload(sessionID, remotePath, localPath); err != nil {
		logx.Errorf("[apphost] SFTP 下载失败: session=%s remote=%s local=%s err=%v", sessionID, remotePath, localPath, err)
		return err
	}
	logx.Infof("[apphost] SFTP 下载完成: session=%s remote=%q local=%q elapsed=%s",
		sessionID, remotePath, localPath, time.Since(start))
	return nil
}

// SftpDownloadFolder 递归下载远端文件夹到本地目录（保留文件夹名与内部结构，整条传输带进度事件）。
func (a *App) SftpDownloadFolder(sessionID, remoteDir, localDir string) error {
	start := time.Now()
	logx.Infof("[apphost] SFTP 下载文件夹开始: session=%s remote=%q local=%q", sessionID, remoteDir, localDir)
	if err := a.manager.SftpDownloadFolder(sessionID, remoteDir, localDir); err != nil {
		logx.Errorf("[apphost] SFTP 下载文件夹失败: session=%s remote=%s local=%s err=%v", sessionID, remoteDir, localDir, err)
		return err
	}
	logx.Infof("[apphost] SFTP 下载文件夹完成: session=%s remote=%q local=%q elapsed=%s",
		sessionID, remoteDir, localDir, time.Since(start))
	return nil
}

// SftpCancelTransfer 取消指定会话中正在进行的 SFTP 上传/下载。
func (a *App) SftpCancelTransfer(sessionID, direction, name string) error {
	if err := a.manager.SftpCancelTransfer(sessionID, direction, name); err != nil {
		logx.Debugf("[apphost] SFTP 取消传输未命中: session=%s direction=%s name=%s err=%v", sessionID, direction, name, err)
		return err
	}
	logx.Debugf("[apphost] SFTP 传输已取消: session=%s direction=%s name=%s", sessionID, direction, name)
	return nil
}

// SftpRename 重命名远程文件或目录。
func (a *App) SftpRename(sessionID, oldPath, newPath string) error {
	logx.Debugf("[apphost] SFTP 重命名: session=%s old=%q new=%q", sessionID, oldPath, newPath)
	if err := a.manager.SftpRename(sessionID, oldPath, newPath); err != nil {
		logx.Errorf("[apphost] SFTP 重命名失败: session=%s old=%s new=%s err=%v", sessionID, oldPath, newPath, err)
		return err
	}
	logx.Infof("[apphost] SFTP 重命名完成: session=%s old=%q new=%q", sessionID, oldPath, newPath)
	return nil
}

// SftpMkdir 在远程路径新建目录。
func (a *App) SftpMkdir(sessionID, path string) error {
	logx.Debugf("[apphost] SFTP 新建目录: session=%s path=%q", sessionID, path)
	if err := a.manager.SftpMkdir(sessionID, path); err != nil {
		logx.Errorf("[apphost] SFTP 新建目录失败: session=%s path=%s err=%v", sessionID, path, err)
		return err
	}
	logx.Infof("[apphost] SFTP 新建目录完成: session=%s path=%q", sessionID, path)
	return nil
}

// SftpRemove 删除远程文件或目录（目录递归删除）。
func (a *App) SftpRemove(sessionID, path string) error {
	logx.Debugf("[apphost] SFTP 删除: session=%s path=%q", sessionID, path)
	if err := a.manager.SftpRemove(sessionID, path); err != nil {
		logx.Errorf("[apphost] SFTP 删除失败: session=%s path=%s err=%v", sessionID, path, err)
		return err
	}
	logx.Infof("[apphost] SFTP 删除完成: session=%s path=%q", sessionID, path)
	return nil
}

// ---- SSH 隧道 ----

// StartTunnel 创建并启动一条 SSH 隧道（mode: local | remote | dynamic）。
func (a *App) StartTunnel(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	return a.startTunnel(node, name, mode, localPort, remoteHost, remotePort, "")
}

func (a *App) startTunnel(node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string) (models.TunnelInfo, error) {
	start := time.Now()
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 启动 SSH 隧道被拒（保险库未解锁）: name=%s err=%v", name, err)
		return models.TunnelInfo{}, err
	}
	logx.Debugf("[apphost] 启动 SSH 隧道: name=%s mode=%s server=%s listenHost=%s local=%d remote=%s:%d",
		name, mode, node.Name, listenHost, localPort, remoteHost, remotePort)
	node, err := a.resolveSSHNode(node)
	if err != nil {
		logx.Warnf("[apphost] 启动 SSH 隧道被拒（连接路径校验失败）: name=%s err=%v", name, err)
		return models.TunnelInfo{}, err
	}
	info, err := a.manager.StartTunnel(node, name, mode, localPort, remoteHost, remotePort, listenHost)
	if err != nil {
		logx.Errorf("[apphost] 启动 SSH 隧道失败: mode=%s name=%s local=%d remote=%s:%d err=%v", mode, name, localPort, remoteHost, remotePort, err)
		return models.TunnelInfo{}, err
	}
	logx.Infof("[apphost] 启动 SSH 隧道完成: id=%s name=%s mode=%s local=%d remote=%s:%d elapsed=%s",
		info.ID, info.Name, info.Mode, info.LocalPort, info.RemoteHost, info.RemotePort, time.Since(start))
	return info, nil
}

// UpdateTunnel 修改已有隧道的配置，运行中的隧道会按新配置重启。
func (a *App) UpdateTunnel(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int) (models.TunnelInfo, error) {
	return a.updateTunnel(id, node, name, mode, localPort, remoteHost, remotePort, "")
}

func (a *App) updateTunnel(id string, node models.ServerNode, name, mode string, localPort int, remoteHost string, remotePort int, listenHost string) (models.TunnelInfo, error) {
	start := time.Now()
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 修改 SSH 隧道被拒（保险库未解锁）: id=%s err=%v", id, err)
		return models.TunnelInfo{}, err
	}
	logx.Debugf("[apphost] 修改 SSH 隧道: id=%s name=%s mode=%s server=%s listenHost=%s local=%d remote=%s:%d",
		id, name, mode, node.Name, listenHost, localPort, remoteHost, remotePort)
	node, err := a.resolveSSHNode(node)
	if err != nil {
		logx.Warnf("[apphost] 修改 SSH 隧道被拒（连接路径校验失败）: id=%s err=%v", id, err)
		return models.TunnelInfo{}, err
	}
	info, err := a.manager.UpdateTunnel(id, node, name, mode, localPort, remoteHost, remotePort, listenHost)
	if err != nil {
		logx.Errorf("[apphost] 修改 SSH 隧道失败: id=%s mode=%s name=%s local=%d remote=%s:%d err=%v", id, mode, name, localPort, remoteHost, remotePort, err)
		return info, err
	}
	logx.Infof("[apphost] 修改 SSH 隧道完成: id=%s name=%s mode=%s elapsed=%s", id, info.Name, info.Mode, time.Since(start))
	return info, nil
}

// StopTunnel 停止指定隧道（保留条目，可重新启动）。
func (a *App) StopTunnel(id string) error {
	logx.Debugf("[apphost] 停止 SSH 隧道: id=%s", id)
	if err := a.manager.StopTunnel(id); err != nil {
		logx.Errorf("[apphost] 停止 SSH 隧道失败: id=%s err=%v", id, err)
		return err
	}
	logx.Infof("[apphost] 停止 SSH 隧道完成: id=%s", id)
	return nil
}

// RestartTunnel 重新启动已停止/异常的隧道。
func (a *App) RestartTunnel(id string) error {
	logx.Debugf("[apphost] 重启 SSH 隧道: id=%s", id)
	if err := a.manager.RestartTunnel(id); err != nil {
		logx.Errorf("[apphost] 重启 SSH 隧道失败: id=%s err=%v", id, err)
		return err
	}
	logx.Infof("[apphost] 重启 SSH 隧道完成: id=%s", id)
	return nil
}

// RemoveTunnel 停止并移除指定隧道。
func (a *App) RemoveTunnel(id string) error {
	logx.Debugf("[apphost] 移除 SSH 隧道: id=%s", id)
	if err := a.manager.RemoveTunnel(id); err != nil {
		logx.Errorf("[apphost] 移除 SSH 隧道失败: id=%s err=%v", id, err)
		return err
	}
	logx.Infof("[apphost] 移除 SSH 隧道完成: id=%s", id)
	return nil
}

// ListTunnels 返回全部隧道摘要。
func (a *App) ListTunnels() []models.TunnelInfo {
	tunnels := a.manager.ListTunnels()
	logx.Debugf("[apphost] 读取隧道列表: count=%d", len(tunnels))
	return tunnels
}

// ---- 分组管理（路径即身份：'/' 分隔层级，空串=未分组）----

// canonicalServerGroup 规范化服务器节点上的分组路径；空或非法路径返回 false。
func canonicalServerGroup(raw string) (string, bool) {
	path, err := store.NormalizeLegacyGroupPath(raw)
	if err != nil {
		logx.Warnf("[apphost] 服务器分组路径非法，已忽略: raw=%q err=%v", raw, err)
		return "", false
	}
	if path == "" {
		return "", false
	}
	return path, true
}

// groupStoreCascadesServers 判断分组存储是否会在写操作内自行级联服务器分组（SQLite 事务实现）。
func groupStoreCascadesServers(groups store.GroupStore) bool {
	cascade, ok := groups.(store.GroupServerCascadeStore)
	return ok && cascade.CascadesServers()
}

// groupPathInList 判断路径集合中是否存在 path 本身或它的子孙。
func groupPathInList(paths []string, path string) bool {
	for _, raw := range paths {
		name, err := store.NormalizeLegacyGroupPath(raw)
		if err != nil {
			name = strings.TrimSpace(raw)
		}
		if name == path || store.IsGroupDescendant(name, path) {
			return true
		}
	}
	return false
}

// serverGroupPaths 返回服务器节点使用的规范化分组路径（去重）。
func serverGroupPaths(nodes []models.ServerNode) []string {
	out := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		path, ok := canonicalServerGroup(n.Group)
		if !ok {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// GetGroups 返回全部分组路径：手动创建的空分组、服务器使用的分组，以及它们的隐式祖先。
func (a *App) GetGroups() []string {
	set := map[string]struct{}{}
	addPath := func(raw string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		path, err := store.NormalizeLegacyGroupPath(raw)
		if err != nil {
			// 无法规范化的旧数据按扁平路径原样展示，避免分组凭空消失。
			logx.Warnf("[apphost] 分组路径非法，按原样展示: raw=%q err=%v", raw, err)
			path = strings.TrimSpace(raw)
			if path == "" {
				return
			}
		}
		for _, ancestor := range store.GroupAncestors(path) {
			set[ancestor] = struct{}{}
		}
		set[path] = struct{}{}
	}
	if a.groups != nil {
		if stored, err := a.groups.List(); err != nil {
			logx.Errorf("[apphost] 读取分组失败: err=%v", err)
		} else {
			for _, g := range stored {
				addPath(g)
			}
		}
	}
	if a.store != nil {
		if nodes, err := a.store.List(); err != nil {
			logx.Errorf("[apphost] 读取服务器列表失败: err=%v", err)
		} else {
			for _, n := range nodes {
				addPath(n.Group)
			}
		}
	}
	groups := make([]string, 0, len(set))
	for g := range set {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	logx.Debugf("[apphost] 读取分组列表: count=%d", len(groups))
	return groups
}

// AddGroup 新增分组路径及其所有祖先；已存在时幂等。
func (a *App) AddGroup(path string) error {
	start := time.Now()
	normalized, err := store.NormalizeGroupPath(path)
	if err != nil {
		logx.Warnf("[apphost] 新增分组被拒（路径非法）: path=%q err=%v", path, err)
		return err
	}
	if err := a.groups.Add(normalized); err != nil {
		logx.Errorf("[apphost] 新增分组失败: path=%q err=%v", normalized, err)
		return err
	}
	logx.Infof("[apphost] 新增分组完成: path=%q ancestors=%d elapsed=%s",
		normalized, len(store.GroupAncestors(normalized)), time.Since(start))
	return nil
}

// RenameGroup 只修改分组路径的最后一段，并级联更新子分组与服务器分组。
// newLeaf 是新的叶子名称，不能包含 '/'。
func (a *App) RenameGroup(path, newLeaf string) error {
	start := time.Now()
	oldPath, targetPath, err := store.ResolveGroupRename(path, newLeaf)
	if err != nil {
		logx.Warnf("[apphost] 重命名分组被拒（参数非法）: path=%q newLeaf=%q err=%v", path, newLeaf, err)
		return err
	}
	if oldPath == targetPath {
		logx.Debugf("[apphost] 重命名分组跳过（路径未变）: path=%q", oldPath)
		return nil
	}
	logx.Debugf("[apphost] 重命名分组: old=%q new=%q", oldPath, targetPath)
	cascade := groupStoreCascadesServers(a.groups)
	// SQLite 事务实现会在存储层级联服务器；JSON 兜底需要在 app 层补更新（非事务）。
	var nodes []models.ServerNode
	if !cascade {
		stored, lerr := a.groups.List()
		if lerr != nil {
			logx.Errorf("[apphost] 重命名分组失败（读取分组列表）: old=%q err=%v", oldPath, lerr)
			return fmt.Errorf(i18n.T("读取分组失败: %w"), lerr)
		}
		nodes, err = a.store.List()
		if err != nil {
			logx.Errorf("[apphost] 重命名分组失败（读取服务器列表）: old=%q err=%v", oldPath, err)
			return fmt.Errorf(i18n.T("读取服务器列表失败: %w"), err)
		}
		if groupPathInList(stored, targetPath) || groupPathInList(serverGroupPaths(nodes), targetPath) {
			logx.Warnf("[apphost] 重命名分组被拒（目标已存在）: old=%q new=%q", oldPath, targetPath)
			return errors.New(i18n.T("目标分组已存在"))
		}
		if !groupPathInList(stored, oldPath) && !groupPathInList(serverGroupPaths(nodes), oldPath) {
			logx.Warnf("[apphost] 重命名分组被拒（分组不存在）: old=%q", oldPath)
			return errors.New(i18n.T("分组不存在"))
		}
	}
	if err := a.groups.Rename(oldPath, store.GroupBase(targetPath)); err != nil {
		logx.Warnf("[apphost] 重命名分组失败: old=%q new=%q err=%v", oldPath, targetPath, err)
		return err
	}
	if cascade {
		logx.Infof("[apphost] 重命名分组完成(事务): old=%q new=%q elapsed=%s", oldPath, targetPath, time.Since(start))
		return nil
	}
	moved, err := a.syncServersAfterGroupRename(nodes, oldPath, targetPath)
	if err != nil {
		return err
	}
	logx.Infof("[apphost] 重命名分组完成(JSON 兜底、非事务): old=%q new=%q servers=%d elapsed=%s",
		oldPath, targetPath, moved, time.Since(start))
	return nil
}

// syncServersAfterGroupRename 把服务器分组路径的前缀从 oldPath 换成 targetPath。
// JSON 兜底没有跨文件事务：中途失败会回滚已保存的服务器分组并返回错误。
func (a *App) syncServersAfterGroupRename(nodes []models.ServerNode, oldPath, targetPath string) (int, error) {
	saved := make([]models.ServerNode, 0, len(nodes))
	moved := 0
	for _, n := range nodes {
		group, ok := canonicalServerGroup(n.Group)
		if !ok {
			continue
		}
		var newGroup string
		switch {
		case group == oldPath:
			newGroup = targetPath
		case store.IsGroupDescendant(group, oldPath):
			newGroup = targetPath + group[len(oldPath):]
		default:
			continue
		}
		updated := n
		updated.Group = newGroup
		if err := a.store.Save(updated); err != nil {
			logx.Errorf("[apphost] 重命名分组失败（更新服务器分组）: id=%s err=%v", n.ID, err)
			a.rollbackServerGroups(saved)
			return moved, fmt.Errorf(i18n.T("同步服务器分组失败: %w"), err)
		}
		saved = append(saved, n)
		moved++
	}
	return moved, nil
}

// rollbackServerGroups 尽力回滚已保存的服务器分组（JSON 兜底的非事务补偿）。
func (a *App) rollbackServerGroups(nodes []models.ServerNode) {
	if len(nodes) == 0 {
		return
	}
	for _, n := range nodes {
		if err := a.store.Save(n); err != nil {
			logx.Errorf("[apphost] 回滚服务器分组失败: id=%s err=%v", n.ID, err)
		}
	}
	logx.Warnf("[apphost] 已回滚服务器分组: count=%d", len(nodes))
}

// RemoveGroup 删除分组。
//   - keepChildren=false：递归删除该组与全部子孙分组，子树内服务器变为未分组；
//   - keepChildren=true：删除该组，直属服务器移到父级（根组置空），子孙分组整体上移一级；
//     上移后与已有分组冲突时拒绝。
func (a *App) RemoveGroup(path string, keepChildren bool) error {
	start := time.Now()
	canonical, err := store.CanonicalGroupPath(path)
	if err != nil {
		logx.Warnf("[apphost] 删除分组被拒（路径非法）: path=%q err=%v", path, err)
		return err
	}
	logx.Debugf("[apphost] 删除分组: path=%q keepChildren=%v", canonical, keepChildren)
	cascade := groupStoreCascadesServers(a.groups)
	var nodes []models.ServerNode
	if !cascade {
		nodes, err = a.store.List()
		if err != nil {
			logx.Errorf("[apphost] 删除分组失败（读取服务器列表）: path=%q err=%v", canonical, err)
			return fmt.Errorf(i18n.T("读取服务器列表失败: %w"), err)
		}
		if keepChildren {
			stored, lerr := a.groups.List()
			if lerr != nil {
				logx.Errorf("[apphost] 删除分组失败（读取分组列表）: path=%q err=%v", canonical, lerr)
				return fmt.Errorf(i18n.T("读取分组失败: %w"), lerr)
			}
			if cerr := store.CheckGroupLiftConflict(stored, serverGroupPaths(nodes), canonical); cerr != nil {
				logx.Warnf("[apphost] 删除分组被拒（子分组上移冲突）: path=%q err=%v", canonical, cerr)
				return cerr
			}
		}
	}
	if err := a.groups.Remove(canonical, keepChildren); err != nil {
		logx.Warnf("[apphost] 删除分组失败: path=%q err=%v", canonical, err)
		return err
	}
	if cascade {
		logx.Infof("[apphost] 删除分组完成(事务): path=%q keepChildren=%v elapsed=%s", canonical, keepChildren, time.Since(start))
		return nil
	}
	moved, cleared, err := a.syncServersAfterGroupRemove(nodes, canonical, keepChildren)
	if err != nil {
		return err
	}
	logx.Infof("[apphost] 删除分组完成(JSON 兜底、非事务): path=%q keepChildren=%v moved=%d cleared=%d elapsed=%s",
		canonical, keepChildren, moved, cleared, time.Since(start))
	return nil
}

// syncServersAfterGroupRemove 在 JSON 兜底下同步服务器分组；返回移动到父级的数量与置空的数量。
func (a *App) syncServersAfterGroupRemove(nodes []models.ServerNode, path string, keepChildren bool) (moved, cleared int, err error) {
	parent := store.GroupParent(path)
	for _, n := range nodes {
		group, ok := canonicalServerGroup(n.Group)
		if !ok {
			continue
		}
		direct := group == path
		if !direct && !store.IsGroupDescendant(group, path) {
			continue
		}
		var newGroup string
		if keepChildren {
			if direct {
				newGroup = parent
			} else {
				newGroup = store.JoinGroupPath(parent, strings.TrimPrefix(group, path+"/"))
			}
		} else {
			newGroup = ""
		}
		updated := n
		updated.Group = newGroup
		if err := a.store.Save(updated); err != nil {
			logx.Errorf("[apphost] 删除分组失败（更新服务器分组）: id=%s err=%v", n.ID, err)
			return moved, cleared, fmt.Errorf(i18n.T("同步服务器分组失败: %w"), err)
		}
		if newGroup == "" {
			cleared++
		} else {
			moved++
		}
	}
	return moved, cleared, nil
}

// ---- SSH 会话管理 ----

// Connect 建立 SSH 终端会话。sessionID 由前端生成并提前订阅进度事件，
// 为空时后端自动生成。
func (a *App) Connect(sessionID string, node models.ServerNode, cols, rows int) (models.ConnectResult, error) {
	start := time.Now()
	if strings.TrimSpace(sessionID) == "" {
		sessionID = uuid.NewString()
	}
	if node.Port == 0 {
		node.Port = 22
	}
	logx.Infof("[apphost] 发起 SSH 连接: session=%s server=%s host=%s:%d user=%s auth=%s jumps=%d cols=%d rows=%d",
		sessionID, node.Name, node.Host, node.Port, node.User, node.AuthType, len(node.JumpServerIDs), cols, rows)
	node, err := a.resolveSSHNode(node)
	if err != nil {
		logx.Warnf("[apphost] SSH 连接被拒（连接路径校验失败）: session=%s server=%s err=%v", sessionID, node.Name, err)
		return models.ConnectResult{}, err
	}
	if err := a.manager.Connect(sessionID, node, cols, rows); err != nil {
		logx.Errorf("[apphost] SSH 连接失败: session=%s server=%s host=%s:%d user=%s err=%v",
			sessionID, node.Name, node.Host, node.Port, node.User, err)
		return models.ConnectResult{}, err
	}
	logx.Infof("[apphost] SSH 连接已建立: session=%s server=%s host=%s:%d elapsed=%s",
		sessionID, node.Name, node.Host, node.Port, time.Since(start))
	a.updateBadge()
	return models.ConnectResult{SessionID: sessionID, Server: node.Name}, nil
}

// ConnectLocal 打开本机终端会话（复用 ssh:output / ssh:status 事件通道）。
func (a *App) ConnectLocal(sessionID string, cols, rows int) (models.ConnectResult, error) {
	start := time.Now()
	if a.local == nil {
		logx.Errorf("[apphost] 打开本机终端失败（管理器未初始化）")
		return models.ConnectResult{}, fmt.Errorf("%s", i18n.T("本地终端未初始化"))
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = uuid.NewString()
	}
	logx.Infof("[apphost] 打开本机终端: session=%s cols=%d rows=%d", sessionID, cols, rows)
	if err := a.local.Connect(sessionID, "", cols, rows); err != nil {
		logx.Errorf("[apphost] 打开本机终端失败: session=%s err=%v", sessionID, err)
		return models.ConnectResult{}, err
	}
	label := a.local.SessionName(sessionID)
	logx.Infof("[apphost] 本机终端已打开: session=%s shell=%s elapsed=%s", sessionID, label, time.Since(start))
	a.updateBadge()
	return models.ConnectResult{SessionID: sessionID, Server: label}, nil
}

// GetRuntimeStatus 返回用于排查应用无响应的后端快照。
func (a *App) GetRuntimeStatus() map[string]int64 {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var uptime int64
	if a.startedAt > 0 {
		uptime = (time.Now().UnixMilli() - a.startedAt) / 1000
	}
	status := map[string]int64{
		"uptimeSec":     uptime,
		"goroutines":    int64(runtime.NumGoroutine()),
		"heapBytes":     int64(mem.HeapAlloc),
		"goMemoryBytes": int64(mem.Sys - mem.HeapReleased),
	}
	addProcessUsage(status)
	// 运行时状态由前端周期轮询：按时间采样，只记关键指标。
	logx.DebugSampled(runtimeStatusLog, "[apphost] 运行时状态采样: uptimeSec=%d goroutines=%d heapBytes=%d rssBytes=%d cpuMs=%d",
		status["uptimeSec"], status["goroutines"], status["heapBytes"], status["processRSSBytes"], status["processCPUms"])
	return status
}

// GetPlatform 返回当前操作系统标识（darwin / windows / linux）。
func (a *App) GetPlatform() string {
	logx.Debugf("[apphost] 读取平台标识: platform=%s", runtime.GOOS)
	return runtime.GOOS
}

// RevealLocalPath 在系统文件管理器中打开本地文件 / 目录（SFTP 下载完成后「打开所在文件夹」）。
//
// path 可以指向文件（选中该文件）或目录（打开该目录）。空路径、不存在的路径一律返回错误，
// 由前端把错误提示给用户。实现按平台分为 revealLocalPath（localpath_*.go）。
func (a *App) RevealLocalPath(path string) error {
	target := strings.TrimSpace(path)
	logx.Debugf("[apphost] 在文件管理器中打开本地路径: path=%q", target)
	if target == "" {
		logx.Warnf("[apphost] 打开本地路径被拒（路径为空）")
		return fmt.Errorf("%s", i18n.T("路径为空，无法打开所在文件夹"))
	}
	info, err := os.Stat(target)
	if err != nil {
		logx.Warnf("[apphost] 打开本地路径失败（不存在或不可访问）: path=%q err=%v", target, err)
		return fmt.Errorf(i18n.T("本地路径不存在或不可访问: %s"), target)
	}
	if a.desktop != nil && a.desktop.Env != nil {
		if err := a.desktop.Env.OpenFileManager(target, !info.IsDir()); err == nil {
			logx.Infof("[apphost] 已使用 Wails 环境管理器高亮打开本地路径: path=%q", target)
			return nil
		}
	}
	if err := revealLocalPath(target); err != nil {
		logx.Errorf("[apphost] 打开本地文件夹失败: path=%q err=%v", target, err)
		return fmt.Errorf(i18n.T("打开本地文件夹失败: %w"), err)
	}
	logx.Infof("[apphost] 已在文件管理器中打开本地路径: path=%q", target)
	return nil
}

// GetLocalShellOptions 返回当前平台可选的本机 Shell 列表。
func (a *App) GetLocalShellOptions() []localterm.ShellOption {
	options := localterm.OptionsForPlatform()
	logx.Debugf("[apphost] 读取本机 Shell 选项: count=%d", len(options))
	return options
}

// Disconnect 断开指定会话（SSH 或本机）。
func (a *App) Disconnect(sessionID string) error {
	defer a.updateBadge()
	if a.local != nil && a.local.Has(sessionID) {
		logx.Debugf("[apphost] 断开本机终端: session=%s", sessionID)
		if err := a.local.Disconnect(sessionID); err != nil {
			logx.Errorf("[apphost] 断开本机终端失败: session=%s err=%v", sessionID, err)
			return err
		}
		logx.Infof("[apphost] 本机终端已断开: session=%s", sessionID)
		return nil
	}
	logx.Debugf("[apphost] 断开 SSH 会话: session=%s", sessionID)
	if err := a.manager.Disconnect(sessionID); err != nil {
		logx.Errorf("[apphost] 断开 SSH 会话失败: session=%s err=%v", sessionID, err)
		return err
	}
	logx.Infof("[apphost] SSH 会话已断开: session=%s", sessionID)
	return nil
}

// Reconnect 重新建立已断开的 SSH 会话，复用原会话配置与回调。
func (a *App) Reconnect(sessionID string, cols, rows int) (models.ConnectResult, error) {
	defer a.updateBadge()
	start := time.Now()
	if a.local != nil && a.local.Has(sessionID) {
		logx.Infof("[apphost] 重连本机终端: session=%s cols=%d rows=%d", sessionID, cols, rows)
		if err := a.local.Connect(sessionID, "", cols, rows); err != nil {
			logx.Errorf("[apphost] 重连本机终端失败: session=%s err=%v", sessionID, err)
			return models.ConnectResult{}, err
		}
		logx.Infof("[apphost] 本机终端重连完成: session=%s elapsed=%s", sessionID, time.Since(start))
		return models.ConnectResult{SessionID: sessionID}, nil
	}
	logx.Infof("[apphost] 重连 SSH 会话: session=%s cols=%d rows=%d", sessionID, cols, rows)
	if err := a.manager.Reconnect(sessionID, cols, rows); err != nil {
		logx.Errorf("[apphost] 重连 SSH 会话失败: session=%s err=%v", sessionID, err)
		return models.ConnectResult{}, err
	}
	logx.Infof("[apphost] SSH 会话重连完成: session=%s elapsed=%s", sessionID, time.Since(start))
	return models.ConnectResult{SessionID: sessionID}, nil
}

// Write 向会话写入终端输入（data 为 base64 编码）。
// 高频路径：只采样记录长度，绝不记录输入内容。
func (a *App) Write(sessionID, data string) error {
	logx.DebugSampled(terminalWriteLog, "[apphost] 终端输入转发: session=%s base64Len=%d", sessionID, len(data))
	if a.local != nil && a.local.Has(sessionID) {
		if err := a.local.Write(sessionID, data); err != nil {
			logx.Debugf("[apphost] 本机终端写入失败: session=%s err=%v", sessionID, err)
			return err
		}
		return nil
	}
	if err := a.manager.Write(sessionID, data); err != nil {
		logx.Debugf("[apphost] SSH 会话写入失败: session=%s err=%v", sessionID, err)
		return err
	}
	return nil
}

// WriteRaw 向会话直接写入原始字节数据（Stream 通道使用）。
func (a *App) WriteRaw(sessionID string, data []byte) error {
	if a.local != nil && a.local.Has(sessionID) {
		return a.local.WriteRaw(sessionID, data)
	}
	if a.manager != nil {
		return a.manager.WriteRaw(sessionID, data)
	}
	return errors.New("会话不存在")
}

// WriteRawInteractive 向会话直接写入原始字节并附带命令上下文（Stream 通道使用）。
func (a *App) WriteRawInteractive(sessionID string, data []byte, command string) error {
	if a.local != nil && a.local.Has(sessionID) {
		return a.local.WriteRaw(sessionID, data)
	}
	if a.manager != nil {
		return a.manager.WriteRawInteractive(sessionID, data, command)
	}
	return errors.New("会话不存在")
}

// RegisterTerminalStream 注册指定会话的原始二进制输出监听器（跨 SSH 与本地终端）。
//
//wails:ignore
func (a *App) RegisterTerminalStream(sessionID string, fn func([]byte)) func() {
	var unsubs []func()
	if a.manager != nil {
		unsubs = append(unsubs, a.manager.SubscribeRawOutput(sessionID, fn))
	}
	if a.local != nil {
		unsubs = append(unsubs, a.local.SubscribeRawOutput(sessionID, fn))
	}
	return func() {
		for _, u := range unsubs {
			u()
		}
	}
}

// Notify 发送系统原生桌面通知并闪烁窗口。
func (a *App) Notify(title, body string) error {
	if a.window != nil {
		a.window.Flash(true)
	}
	if a.notifications == nil {
		return nil
	}
	return a.notifications.SendNotification(notifications.NotificationOptions{
		Title: title,
		Body:  body,
	})
}

// HandleExternalLaunch 处理从外部协议或命令行传入的参数（如 ssh://url 或 .dingpack 文件）。
func (a *App) HandleExternalLaunch(args []string) {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "ssh://") || strings.HasPrefix(trimmed, "ding-ssh://") {
			logx.Infof("[apphost] 收到外部协议拉起: %s", trimmed)
			a.emit("app:external-url", trimmed)
		} else if strings.HasSuffix(strings.ToLower(trimmed), ".dingpack") {
			logx.Infof("[apphost] 收到外部文件关联拉起: %s", trimmed)
			a.emit("app:import-file", trimmed)
		}
	}
}

// DisconnectAll 断开所有活动 SSH 会话与正在运行的隧道。
func (a *App) DisconnectAll() {
	defer a.updateBadge()
	if a.manager != nil {
		a.manager.CloseAll()
	}
	if a.local != nil {
		a.local.CloseAll()
	}
}

// OpenTerminalWindow 打开独立的终端弹窗。
func (a *App) OpenTerminalWindow(sessionID, title string) error {
	if a.desktop == nil {
		return errors.New("桌面环境未就绪")
	}
	winName := "terminal-" + sessionID
	if existing, ok := a.desktop.Window.GetByName(winName); ok {
		existing.Show()
		existing.Focus()
		return nil
	}
	if title == "" {
		title = "DingSSH - Terminal"
	}
	opts := application.WebviewWindowOptions{
		Name:             winName,
		Title:            title,
		Width:            1020,
		Height:           680,
		MinWidth:         640,
		MinHeight:        420,
		InitialPosition:  application.WindowCentered,
		Frameless:        runtime.GOOS == "windows",
		URL:              "/?mode=detached-terminal&session=" + sessionID,
		EnableFileDrop:   true,
		BackgroundColour: application.NewRGB(15, 23, 42),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHidden,
			Backdrop: application.MacBackdropTranslucent,
		},
		Windows: application.WindowsWindow{
			BackdropType: application.Acrylic,
		},
	}
	if a.desktop.Screen != nil {
		opts.Screen = a.desktop.Screen.GetPrimary()
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		opts.BackgroundType = application.BackgroundTypeTranslucent
	}
	win := a.desktop.Window.NewWithOptions(opts)
	win.Show()
	win.Focus()
	return nil
}

// OpenSFTPWindow 打开独立的 SFTP 弹窗。
func (a *App) OpenSFTPWindow(sessionID, title string) error {
	if a.desktop == nil {
		return errors.New("桌面环境未就绪")
	}
	winName := "sftp-" + sessionID
	if existing, ok := a.desktop.Window.GetByName(winName); ok {
		existing.Show()
		existing.Focus()
		return nil
	}
	if title == "" {
		title = "DingSSH - SFTP"
	}
	opts := application.WebviewWindowOptions{
		Name:             winName,
		Title:            title,
		Width:            960,
		Height:           640,
		MinWidth:         600,
		MinHeight:        400,
		InitialPosition:  application.WindowCentered,
		Frameless:        runtime.GOOS == "windows",
		URL:              "/?mode=detached-sftp&session=" + sessionID,
		EnableFileDrop:   true,
		BackgroundColour: application.NewRGB(15, 23, 42),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHidden,
			Backdrop: application.MacBackdropTranslucent,
		},
		Windows: application.WindowsWindow{
			BackdropType: application.Acrylic,
		},
	}
	if a.desktop.Screen != nil {
		opts.Screen = a.desktop.Screen.GetPrimary()
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		opts.BackgroundType = application.BackgroundTypeTranslucent
	}
	win := a.desktop.Window.NewWithOptions(opts)
	win.Show()
	win.Focus()
	return nil
}

func (a *App) updateBadge() {
	if a.dock == nil {
		return
	}
	count := len(a.ListSessions())
	if count > 0 {
		_ = a.dock.SetBadge(strconv.Itoa(count))
	} else {
		_ = a.dock.RemoveBadge()
	}
}

// SetWindowContentProtection 开启或关闭窗口隐私防截屏保护。
func (a *App) SetWindowContentProtection(enabled bool) {
	if a.window != nil {
		a.window.SetContentProtection(enabled)
		logx.Infof("[apphost] 窗口防截屏/防录屏隐私保护已变更: enabled=%v", enabled)
	}
}

// RegisterGlobalShortcut 动态注册全局系统快捷键。
func (a *App) RegisterGlobalShortcut(shortcut string) error {
	if a.desktop == nil || a.desktop.GlobalShortcut == nil {
		return errors.New("桌面环境未就绪")
	}
	return a.desktop.GlobalShortcut.Register(shortcut, func() {
		if a.window != nil {
			if a.window.IsVisible() && a.window.IsFocused() {
				a.window.Hide()
			} else {
				a.window.Show()
				a.window.Focus()
			}
		}
	})
}

// UnregisterGlobalShortcut 注销指定的全局快捷键。
func (a *App) UnregisterGlobalShortcut(shortcut string) error {
	if a.desktop == nil || a.desktop.GlobalShortcut == nil {
		return errors.New("桌面环境未就绪")
	}
	return a.desktop.GlobalShortcut.Unregister(shortcut)
}

// Resize 调整会话终端尺寸（前端 resize 高频 RPC，采样记录）。
func (a *App) Resize(sessionID string, cols, rows int) error {
	logx.DebugSampled(terminalResizeLog, "[apphost] 终端尺寸调整: session=%s cols=%d rows=%d", sessionID, cols, rows)
	if a.local != nil && a.local.Has(sessionID) {
		if err := a.local.Resize(sessionID, cols, rows); err != nil {
			logx.Debugf("[apphost] 本机终端尺寸调整失败: session=%s err=%v", sessionID, err)
			return err
		}
		return nil
	}
	if err := a.manager.Resize(sessionID, cols, rows); err != nil {
		logx.Debugf("[apphost] SSH 会话尺寸调整失败: session=%s err=%v", sessionID, err)
		return err
	}
	return nil
}

// ListSessions 返回当前活动会话列表。
func (a *App) ListSessions() []models.SessionInfo {
	sessions := a.manager.List()
	logx.Debugf("[apphost] 读取活动会话列表: count=%d", len(sessions))
	return sessions
}

// Reconnect 重新建立已断开的 SSH 会话，复用原会话配置与回调。
// ---- Phase 2: SFTP 与 Shell 双向同步 ----

// SetSftpPathFromTerminal 当 Shell 侧检测到目录变更时，强制更新 SFTP 面板路径。
func (a *App) SetSftpPathFromTerminal(sessionID, path string) error {
	logx.Debugf("[apphost] 终端目录同步到 SFTP 面板: session=%s path=%q", sessionID, path)
	if err := a.manager.SetSftpPathFromTerminal(sessionID, path); err != nil {
		logx.Debugf("[apphost] 同步 SFTP 面板路径失败: session=%s path=%q err=%v", sessionID, path, err)
		return err
	}
	return nil
}

// SyncSftpToTerminal 在 SFTP 面板双击文件夹时，向终端发送 cd 命令实现目录同步。
func (a *App) SyncSftpToTerminal(sessionID, path string) error {
	logx.Debugf("[apphost] SFTP 面板目录同步到终端: session=%s path=%q", sessionID, path)
	if err := a.manager.SyncSftpToTerminal(sessionID, path); err != nil {
		logx.Debugf("[apphost] 同步终端目录失败: session=%s path=%q err=%v", sessionID, path, err)
		return err
	}
	return nil
}

// ---- Phase 3: 命令历史 ----

// AddCommandHistory 记录一条已执行命令（写库失败静默忽略，不影响终端）。
func (a *App) AddCommandHistory(serverID, command string) {
	if a.history == nil {
		return
	}
	if err := a.history.Add(serverID, command); err != nil {
		// 命令内容本身不落日志（等价于终端输入）。
		logx.Debugf("[apphost] 写入命令历史失败: server=%s err=%v", serverID, err)
	}
}

// ClearCommandHistory 清理命令历史；serverID 为空则清空全部。
func (a *App) ClearCommandHistory(serverID string) error {
	if a.history == nil {
		return nil
	}
	if err := a.history.Clear(serverID); err != nil {
		logx.Errorf("[apphost] 清理命令历史失败: server=%s err=%v", serverID, err)
		return err
	}
	logx.Infof("[apphost] 清理命令历史完成: server=%s", serverID)
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
		logx.Debugf("[apphost] 查询命令历史失败: server=%s err=%v", serverID, err)
		return []models.CommandSuggestion{}
	}
	// 输入时高频触发：限流记录条数，不记录前缀内容。
	logx.DebugSampled(historyQueryLog, "[apphost] 查询命令历史: server=%s count=%d", serverID, len(list))
	return list
}

// ---- Phase 3: 本地文件读写（Zmodem） ----

// ReadLocalFileBase64 读取本地文件并返回 base64 内容（Zmodem 传输用，日志只记路径与字节数）。
func (a *App) ReadLocalFileBase64(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		logx.Warnf("[apphost] 读取本地文件被拒（路径为空）")
		return "", errors.New(i18n.T("文件路径为空"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logx.Errorf("[apphost] 读取本地文件失败: path=%q err=%v", path, err)
		return "", fmt.Errorf(i18n.T("读取本地文件失败: %w"), err)
	}
	logx.Debugf("[apphost] 读取本地文件: path=%q bytes=%d", path, len(data))
	return base64.StdEncoding.EncodeToString(data), nil
}

// WriteLocalFileBase64 将 base64 内容写入本地文件（日志只记路径与字节数，绝不记录内容）。
func (a *App) WriteLocalFileBase64(path, dataBase64 string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		logx.Warnf("[apphost] 写入本地文件被拒（路径为空）")
		return errors.New(i18n.T("文件路径为空"))
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		logx.Errorf("[apphost] 写入本地文件失败（内容解码失败）: path=%q err=%v", path, err)
		return fmt.Errorf(i18n.T("解码文件内容失败: %w"), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logx.Errorf("[apphost] 写入本地文件失败: path=%q bytes=%d err=%v", path, len(data), err)
		return fmt.Errorf(i18n.T("写入本地文件失败: %w"), err)
	}
	logx.Infof("[apphost] 写入本地文件完成: path=%q bytes=%d", path, len(data))
	return nil
}

// ---- Phase 4: 安全 / SysInfo / dingpack ----

// GetSecurityStatus 返回加密保险库状态。
func (a *App) GetSecurityStatus() models.SecurityStatus {
	if a.vaultErr != nil {
		return models.SecurityStatus{NeedsUnlock: true, InitializationError: i18n.T("保险库未初始化")}
	}
	if a.vault == nil {
		logx.Debugf("[apphost] 读取安全状态: vault=unavailable")
		return models.SecurityStatus{Unlocked: true, KeyringAvailable: false}
	}
	st := a.vault.Status()
	logx.Debugf("[apphost] 读取安全状态: unlocked=%v masterPassword=%v keyring=%v needsUnlock=%v",
		st.Unlocked, st.MasterPasswordEnabled, st.KeyringAvailable, st.NeedsUnlock)
	return models.SecurityStatus{
		Unlocked:              st.Unlocked,
		MasterPasswordEnabled: st.MasterPasswordEnabled,
		KeyringAvailable:      st.KeyringAvailable,
		NeedsUnlock:           st.NeedsUnlock,
	}
}

// UnlockWithMasterPassword 使用主密码解锁（错误三次短锁 30s）。
func (a *App) UnlockWithMasterPassword(password string) error {
	// 口令本身绝不记录；只记尝试结果与锁定状态。
	if a.vault == nil {
		logx.Errorf("[apphost] 解锁保险库失败（未初始化）")
		return errors.New(i18n.T("保险库未初始化"))
	}
	a.unlockMu.Lock()
	if time.Now().Before(a.unlockLockedUntil) {
		remain := int(time.Until(a.unlockLockedUntil).Seconds())
		a.unlockMu.Unlock()
		logx.Warnf("[apphost] 解锁保险库被拒（尝试过多，仍在锁定中）: remainSec=%d", remain)
		return fmt.Errorf(i18n.T("尝试过多，请 %d 秒后再试"), remain)
	}
	a.unlockMu.Unlock()

	if err := a.vault.Unlock(password); err != nil {
		a.unlockMu.Lock()
		a.unlockFails++
		fails := a.unlockFails
		locked := false
		if a.unlockFails >= 3 {
			a.unlockLockedUntil = time.Now().Add(30 * time.Second)
			a.unlockFails = 0
			locked = true
		}
		a.unlockMu.Unlock()
		logx.Warnf("[apphost] 解锁保险库失败: attempt=%d locked=%v err=%v", fails, locked, err)
		return err
	}
	a.unlockMu.Lock()
	a.unlockFails = 0
	a.unlockMu.Unlock()
	a.attachCipher()
	if a.db != nil && a.vault.NeedsMigration() {
		if n, err := store.MigratePlaintextSecrets(a.db, a.vault); err == nil {
			_ = a.vault.MarkMigrated()
			logx.Infof("[apphost] 解锁后已加密迁移明文敏感字段: count=%d", n)
		} else {
			logx.Warnf("[apphost] 解锁后加密迁移明文敏感字段失败: err=%v", err)
		}
	}
	logx.Infof("[apphost] 保险库已解锁")
	return nil
}

// EnableMasterPassword 开启启动主密码。
func (a *App) EnableMasterPassword(password string) error {
	if a.vault == nil {
		logx.Errorf("[apphost] 开启主密码失败（保险库未初始化）")
		return errors.New(i18n.T("保险库未初始化"))
	}
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 开启主密码被拒（保险库未解锁）: err=%v", err)
		return err
	}
	logx.Debugf("[apphost] 开启主密码: 开始重加密敏感字段")
	_, _, err := a.vault.EnableMasterPasswordWithMigration(password, a.migrateVaultKey)
	if err != nil {
		logx.Errorf("[apphost] 开启主密码失败: err=%v", err)
		return err
	}
	a.attachCipher()
	logx.Infof("[apphost] 主密码已开启")
	return nil
}

// DisableMasterPassword 关闭主密码（需验证当前密码）。
func (a *App) DisableMasterPassword(password string) error {
	if a.vault == nil {
		logx.Errorf("[apphost] 关闭主密码失败（保险库未初始化）")
		return errors.New(i18n.T("保险库未初始化"))
	}
	logx.Debugf("[apphost] 关闭主密码: 开始重加密敏感字段")
	_, _, err := a.vault.DisableMasterPasswordWithMigration(password, a.migrateVaultKey)
	if err != nil {
		logx.Warnf("[apphost] 关闭主密码失败（校验当前密码未通过）: err=%v", err)
		return err
	}
	a.attachCipher()
	logx.Infof("[apphost] 主密码已关闭")
	return nil
}

// ChangeMasterPassword 更换主密码。
func (a *App) ChangeMasterPassword(oldPassword, newPassword string) error {
	if a.vault == nil {
		logx.Errorf("[apphost] 更换主密码失败（保险库未初始化）")
		return errors.New(i18n.T("保险库未初始化"))
	}
	logx.Debugf("[apphost] 更换主密码: 开始重加密敏感字段")
	_, _, err := a.vault.ChangeMasterPasswordWithMigration(oldPassword, newPassword, a.migrateVaultKey)
	if err != nil {
		logx.Warnf("[apphost] 更换主密码失败（校验旧密码未通过）: err=%v", err)
		return err
	}
	a.attachCipher()
	logx.Infof("[apphost] 主密码已更换")
	return nil
}

func (a *App) migrateVaultKey(oldKey, newKey []byte) error {
	if a.db == nil {
		return nil
	}
	if err := store.ReencryptAllSecrets(a.db, oldKey, newKey); err != nil {
		return fmt.Errorf(i18n.T("重加密敏感字段失败: %w"), err)
	}
	return nil
}

// StartSysInfoCollector 启动会话系统监控采集。
func (a *App) StartSysInfoCollector(sessionID string) error {
	logx.Debugf("[apphost] 启动会话系统信息采集: session=%s", sessionID)
	if err := a.manager.StartSysInfoCollector(sessionID); err != nil {
		logx.Errorf("[apphost] 启动会话系统信息采集失败: session=%s err=%v", sessionID, err)
		return err
	}
	logx.Infof("[apphost] 会话系统信息采集已启动: session=%s", sessionID)
	return nil
}

// StopSysInfoCollector 停止会话系统监控采集。
func (a *App) StopSysInfoCollector(sessionID string) error {
	logx.Debugf("[apphost] 停止会话系统信息采集: session=%s", sessionID)
	if err := a.manager.StopSysInfoCollector(sessionID); err != nil {
		logx.Errorf("[apphost] 停止会话系统信息采集失败: session=%s err=%v", sessionID, err)
		return err
	}
	logx.Infof("[apphost] 会话系统信息采集已停止: session=%s", sessionID)
	return nil
}

// SetSysInfoIdle 切换监控采集频率（后台降频）。
func (a *App) SetSysInfoIdle(sessionID string, idle bool) {
	logx.Debugf("[apphost] 切换会话系统信息采集频率: session=%s idle=%v", sessionID, idle)
	a.manager.SetSysInfoIdle(sessionID, idle)
}

// SetSysInfoRealtime 切换指定会话的实时刷新（仅本次终端有效，不修改全局设置）。
func (a *App) SetSysInfoRealtime(sessionID string, enabled bool) error {
	if strings.TrimSpace(sessionID) == "" {
		logx.Warnf("[apphost] 切换实时刷新被拒（会话 ID 为空）")
		return errors.New(i18n.T("会话 ID 为空"))
	}
	logx.Debugf("[apphost] 切换会话实时刷新: session=%s enabled=%v", sessionID, enabled)
	a.manager.SetSysInfoRealtime(sessionID, enabled)
	return nil
}

// ExportConfig 导出加密 .dingpack 到用户选择的路径（口令绝不记录）。
func (a *App) ExportConfig(passphrase string) (string, error) {
	start := time.Now()
	logx.Debugf("[apphost] 导出配置包开始")
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 导出配置包被拒（保险库未解锁）: err=%v", err)
		return "", err
	}
	path, err := a.saveFile(application.SaveFileDialogOptions{
		Title:    i18n.T("导出配置包"),
		Filename: "ding-ssh.dingpack",
		Filters:  []application.FileFilter{{DisplayName: "dingpack", Pattern: "*.dingpack"}},
	})
	if err != nil || path == "" {
		if err != nil {
			logx.Warnf("[apphost] 导出配置包失败（选择保存路径）: err=%v", err)
		} else {
			logx.Debugf("[apphost] 用户取消了导出配置包")
		}
		return "", err
	}
	servers, err := a.store.List()
	if err != nil {
		logx.Errorf("[apphost] 导出配置包失败（读取服务器）: err=%v", err)
		return "", err
	}
	creds, err := a.credentials.List()
	if err != nil {
		logx.Errorf("[apphost] 导出配置包失败（读取凭证）: err=%v", err)
		return "", err
	}
	groups, _ := a.groups.List()
	if err := store.WriteDingpackFile(path, passphrase, servers, creds, groups); err != nil {
		logx.Errorf("[apphost] 导出配置包失败: path=%q servers=%d credentials=%d err=%v",
			path, len(servers), len(creds), err)
		return "", err
	}
	size := int64(0)
	if fi, serr := os.Stat(path); serr == nil {
		size = fi.Size()
	}
	logx.Infof("[apphost] 导出配置包完成: path=%q servers=%d credentials=%d groups=%d bytes=%d elapsed=%s",
		path, len(servers), len(creds), len(groups), size, time.Since(start))
	return path, nil
}

// ImportConfig 从用户选择的 .dingpack 导入配置；overwrite=true 时同 ID 覆盖。
func (a *App) ImportConfig(passphrase string, overwrite bool) (models.ImportConfigResult, error) {
	start := time.Now()
	var empty models.ImportConfigResult
	logx.Debugf("[apphost] 导入配置包开始: overwrite=%v", overwrite)
	if err := a.ensureUnlocked(); err != nil {
		logx.Warnf("[apphost] 导入配置包被拒（保险库未解锁）: err=%v", err)
		return empty, err
	}
	path, err := a.openFile(application.OpenFileDialogOptions{
		Title:   i18n.T("导入配置包"),
		Filters: []application.FileFilter{{DisplayName: "dingpack", Pattern: "*.dingpack"}},
	})
	if err != nil || path == "" {
		if err != nil {
			logx.Warnf("[apphost] 导入配置包失败（选择文件）: err=%v", err)
		} else {
			logx.Debugf("[apphost] 用户取消了导入配置包")
		}
		return empty, err
	}
	logx.Infof("[apphost] 导入配置包: path=%q overwrite=%v", path, overwrite)
	payload, err := store.ReadDingpackFile(path, passphrase)
	if err != nil {
		logx.Errorf("[apphost] 导入配置包失败（读取或解密）: path=%q err=%v", path, err)
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
			logx.Errorf("[apphost] 导入配置包失败（写入服务器）: id=%s err=%v", s.ID, err)
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
			logx.Errorf("[apphost] 导入配置包失败（写入凭证）: id=%s err=%v", c.ID, err)
			return result, err
		}
		result.Credentials++
	}
	for _, g := range payload.Groups {
		if g == "" {
			continue
		}
		if err := a.groups.Add(g); err != nil {
			// 保持既有行为：分组导入失败不影响整体导入结果，只留痕。
			logx.Warnf("[apphost] 导入配置包时新增分组失败，已跳过: name=%q err=%v", g, err)
		}
		result.Groups++
	}
	logx.Infof("[apphost] 导入配置包完成: path=%q servers=%d credentials=%d groups=%d elapsed=%s",
		path, result.Servers, result.Credentials, result.Groups, time.Since(start))
	return result, nil
}
