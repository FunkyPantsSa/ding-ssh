package store

import (
	"database/sql"
	"ding-ssh/internal/i18n"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"

	_ "modernc.org/sqlite"
)

// DefaultSQLitePath 返回默认 SQLite 数据库文件路径。
func DefaultSQLitePath() string {
	return filepath.Join(appdir.ConfigDir(), "ding-ssh.db")
}

// schema 建表语句：servers / settings / credentials / groups。
const schema = `
CREATE TABLE IF NOT EXISTS servers (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	grp         TEXT NOT NULL DEFAULT '',
	host        TEXT NOT NULL,
	port        INTEGER NOT NULL DEFAULT 22,
	user        TEXT NOT NULL DEFAULT '',
	auth_type   TEXT NOT NULL DEFAULT 'password',
	password    TEXT NOT NULL DEFAULT '',
	key_path    TEXT NOT NULL DEFAULT '',
	key_content TEXT NOT NULL DEFAULT '',
	bg_image    TEXT NOT NULL DEFAULT '',
	blur_amount INTEGER NOT NULL DEFAULT 0,
	env_vars    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	user     TEXT NOT NULL DEFAULT '',
	password TEXT NOT NULL DEFAULT '',
	auth_type TEXT NOT NULL DEFAULT 'password',
	key_path  TEXT NOT NULL DEFAULT '',
	key_content TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS groups (
	name TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS command_history (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	server_id   TEXT NOT NULL,
	command     TEXT NOT NULL,
	executed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_command_history_server
	ON command_history(server_id, executed_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_history_cmd
	ON command_history(server_id, command);
`

// OpenSQLite 打开（必要时创建）SQLite 数据库并初始化表结构。
func OpenSQLite(path string) (*sql.DB, error) {
	start := time.Now()
	if path == "" {
		path = DefaultSQLitePath()
	}
	logx.Infof("[store] 打开 SQLite 数据库: path=%q", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logx.Errorf("[store] 创建数据目录失败: dir=%q err=%v", filepath.Dir(path), err)
		return nil, fmt.Errorf(i18n.T("创建数据目录失败: %w"), err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		logx.Errorf("[store] 打开 SQLite 数据库失败: path=%q err=%v", path, err)
		return nil, fmt.Errorf(i18n.T("打开数据库失败: %w"), err)
	}
	// WAL 提升并发读写体验；busy_timeout 避免多连接写冲突。
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		logx.Errorf("[store] 设置 WAL 模式失败: path=%q err=%v", path, err)
		return nil, fmt.Errorf(i18n.T("设置 WAL 模式失败: %w"), err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		logx.Errorf("[store] 设置 busy_timeout 失败: path=%q err=%v", path, err)
		return nil, fmt.Errorf(i18n.T("设置 busy_timeout 失败: %w"), err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		logx.Errorf("[store] 初始化表结构失败: path=%q err=%v", path, err)
		return nil, fmt.Errorf(i18n.T("初始化表结构失败: %w"), err)
	}
	logx.Debugf("[store] 数据库基础表结构就绪: path=%q", path)
	// 凭证表新列迁移（旧数据库可能不含 auth_type / key_path / key_content）。
	for _, stmt := range []string{
		`ALTER TABLE servers ADD COLUMN ssh_options TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE credentials ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'password'`,
		`ALTER TABLE credentials ADD COLUMN key_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE credentials ADD COLUMN key_content TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			// 列已存在属正常升级路径；其它错误记录 WARN 但不阻断启动（保持历史行为）。
			if strings.Contains(err.Error(), "duplicate column name") {
				logx.Debugf("[store] 数据库列已存在，跳过迁移: stmt=%q", stmt)
			} else {
				logx.Warnf("[store] 数据库列迁移失败，跳过: stmt=%q err=%v", stmt, err)
			}
		}
	}
	logx.Infof("[store] SQLite 数据库已就绪: path=%q elapsed=%s", path, time.Since(start))
	return db, nil
}

// SQLiteStore 基于 SQLite 的服务器节点存储。
type SQLiteStore struct {
	cipherMu sync.RWMutex
	db       *sql.DB
	cipher   cryptox.FieldCipher
}

// NewSQLiteStore 基于已打开的数据库创建服务器存储。
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	logx.Debugf("[store] 创建 SQLite 服务器存储")
	return &SQLiteStore{db: db}
}

// SetCipher 注入敏感字段加解密器（Phase 4A）。
func (s *SQLiteStore) SetCipher(c cryptox.FieldCipher) {
	s.cipherMu.Lock()
	defer s.cipherMu.Unlock()
	s.cipher = c
	logx.Debugf("[store] 服务器存储已注入字段加解密器: enabled=%v", c != nil)
}

func scanServer(row interface{ Scan(...any) error }) (models.ServerNode, error) {
	var n models.ServerNode
	var env string
	var options string
	var group string
	if err := row.Scan(
		&n.ID, &n.Name, &group, &n.Host, &n.Port, &n.User,
		&n.AuthType, &n.Password, &n.KeyPath, &n.KeyContent,
		&n.BgImage, &n.BlurAmount, &env, &options,
	); err != nil {
		return n, err
	}
	if err := json.Unmarshal([]byte(options), &n.SSHOptions); err != nil {
		logx.Warnf("[store] 服务器 SSH 配置解析失败，按空配置处理: id=%s err=%v", n.ID, err)
		return n, fmt.Errorf(i18n.T("解析 SSH 配置失败: %w"), err)
	}
	n.Group = group
	n.EnvVars = map[string]string{}
	if err := json.Unmarshal([]byte(env), &n.EnvVars); err != nil {
		logx.Warnf("[store] 服务器环境变量解析失败，回落空表: id=%s err=%v", n.ID, err)
		n.EnvVars = map[string]string{}
	}
	return n, nil
}

// List 返回全部服务器节点。
func (s *SQLiteStore) List() ([]models.ServerNode, error) {
	s.cipherMu.RLock()
	defer s.cipherMu.RUnlock()
	defer cryptox.BeginFieldOperation(s.cipher)()
	start := time.Now()
	logx.Debugf("[store] 读取服务器列表(SQLite): cipher=%v", s.cipher != nil)
	rows, err := s.db.Query(`
		SELECT id, name, grp, host, port, user, auth_type, password, key_path,
		       key_content, bg_image, blur_amount, env_vars, ssh_options
		FROM servers ORDER BY name`)
	if err != nil {
		logx.Errorf("[store] 查询服务器列表失败: err=%v", err)
		return nil, err
	}
	defer rows.Close()
	nodes := []models.ServerNode{}
	for rows.Next() {
		n, err := scanServer(rows)
		if err != nil {
			logx.Errorf("[store] 扫描服务器行失败: err=%v", err)
			return nil, err
		}
		if err := s.decryptServer(&n); err != nil {
			logx.Errorf("[store] 解密服务器敏感字段失败: id=%s err=%v", n.ID, err)
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		logx.Errorf("[store] 遍历服务器列表失败: err=%v", err)
		return nil, err
	}
	logx.Debugf("[store] 服务器列表读取完成(SQLite): count=%d elapsed=%s", len(nodes), time.Since(start))
	return nodes, nil
}

// Save 新增或更新服务器节点（按 ID 匹配）。
func (s *SQLiteStore) Save(node models.ServerNode) error {
	s.cipherMu.RLock()
	defer s.cipherMu.RUnlock()
	defer cryptox.BeginFieldOperation(s.cipher)()
	start := time.Now()
	logx.Debugf("[store] 保存服务器(SQLite): id=%s name=%q host=%s:%d user=%s auth=%s",
		node.ID, node.Name, node.Host, node.Port, node.User, node.AuthType)
	if err := s.saveWith(s.db, node); err != nil {
		logx.Errorf("[store] 保存服务器失败(SQLite): id=%s name=%q err=%v", node.ID, node.Name, err)
		return err
	}
	logx.Infof("[store] 保存服务器成功(SQLite): id=%s name=%q elapsed=%s", node.ID, node.Name, time.Since(start))
	return nil
}
func (s *SQLiteStore) SaveBatch(nodes []models.ServerNode) error {
	s.cipherMu.RLock()
	defer s.cipherMu.RUnlock()
	defer cryptox.BeginFieldOperation(s.cipher)()
	start := time.Now()
	logx.Debugf("[store] 批量保存服务器(SQLite): count=%d", len(nodes))
	tx, err := s.db.Begin()
	if err != nil {
		logx.Errorf("[store] 批量保存服务器开启事务失败: count=%d err=%v", len(nodes), err)
		return err
	}
	defer tx.Rollback()
	for _, node := range nodes {
		if err := s.saveWith(tx, node); err != nil {
			logx.Errorf("[store] 批量保存服务器失败(SQLite): id=%s err=%v", node.ID, err)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		logx.Errorf("[store] 批量保存服务器提交失败: count=%d err=%v", len(nodes), err)
		return err
	}
	logx.Infof("[store] 批量保存服务器完成(SQLite): count=%d elapsed=%s", len(nodes), time.Since(start))
	return nil
}
func (s *SQLiteStore) saveWith(executor interface {
	Exec(string, ...any) (sql.Result, error)
}, node models.ServerNode) error {
	node.Group = normalizeStoredGroup(node.Group)
	options, err := json.Marshal(node.SSHOptions)
	if err != nil {
		logx.Warnf("[store] 序列化服务器 SSH 配置失败: id=%s err=%v", node.ID, err)
		return err
	}
	env, err := json.Marshal(node.EnvVars)
	if err != nil {
		logx.Warnf("[store] 序列化服务器环境变量失败: id=%s count=%d err=%v", node.ID, len(node.EnvVars), err)
		return fmt.Errorf(i18n.T("序列化环境变量失败: %w"), err)
	}
	password, err := cryptox.SealPlain(s.cipher, node.Password)
	if err != nil {
		logx.Warnf("[store] 加密服务器密码失败: id=%s err=%v", node.ID, err)
		return fmt.Errorf(i18n.T("加密密码失败: %w"), err)
	}
	keyContent, err := cryptox.SealPlain(s.cipher, node.KeyContent)
	if err != nil {
		logx.Warnf("[store] 加密服务器私钥失败: id=%s err=%v", node.ID, err)
		return fmt.Errorf(i18n.T("加密私钥失败: %w"), err)
	}
	_, err = executor.Exec(`
		INSERT INTO servers (id, name, grp, host, port, user, auth_type, password,
		                    key_path, key_content, bg_image, blur_amount, env_vars, ssh_options)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, grp = excluded.grp, host = excluded.host,
			port = excluded.port, user = excluded.user, auth_type = excluded.auth_type,
			password = excluded.password, key_path = excluded.key_path,
			key_content = excluded.key_content, bg_image = excluded.bg_image,
			blur_amount = excluded.blur_amount, env_vars = excluded.env_vars, ssh_options = excluded.ssh_options`,
		node.ID, node.Name, node.Group, node.Host, node.Port, node.User,
		node.AuthType, password, node.KeyPath, keyContent,
		node.BgImage, node.BlurAmount, string(env), string(options),
	)
	if err != nil {
		logx.Warnf("[store] 写入服务器记录失败: id=%s name=%q err=%v", node.ID, node.Name, err)
		return fmt.Errorf(i18n.T("保存服务器失败: %w"), err)
	}
	return nil
}

func (s *SQLiteStore) decryptServer(n *models.ServerNode) error {
	pw, err := cryptox.OpenField(s.cipher, n.Password)
	if err != nil {
		logx.Warnf("[store] 解密服务器密码失败: id=%s err=%v", n.ID, err)
		return fmt.Errorf(i18n.T("解密服务器密码失败: %w"), err)
	}
	kc, err := cryptox.OpenField(s.cipher, n.KeyContent)
	if err != nil {
		logx.Warnf("[store] 解密服务器私钥失败: id=%s err=%v", n.ID, err)
		return fmt.Errorf(i18n.T("解密服务器私钥失败: %w"), err)
	}
	n.Password = pw
	n.KeyContent = kc
	return nil
}

// Delete 按 ID 删除服务器节点。
func (s *SQLiteStore) Delete(id string) error {
	start := time.Now()
	logx.Debugf("[store] 删除服务器(SQLite): id=%s", id)
	res, err := s.db.Exec(`DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		logx.Errorf("[store] 删除服务器失败(SQLite): id=%s err=%v", id, err)
		return fmt.Errorf(i18n.T("删除服务器失败: %w"), err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		logx.Warnf("[store] 删除服务器(SQLite)未命中: id=%s", id)
	} else {
		logx.Infof("[store] 删除服务器成功(SQLite): id=%s affected=%d elapsed=%s", id, affected, time.Since(start))
	}
	return nil
}

// SQLiteSettingsStore 基于 SQLite 的设置存储（key-value 表）。
type SQLiteSettingsStore struct {
	db *sql.DB
}

// NewSQLiteSettingsStore 基于已打开的数据库创建设置存储。
func NewSQLiteSettingsStore(db *sql.DB) *SQLiteSettingsStore {
	logx.Debugf("[store] 创建 SQLite 设置存储")
	return &SQLiteSettingsStore{db: db}
}

// Get 读取设置，缺少的键使用默认值（日志默认关闭）。
func (s *SQLiteSettingsStore) Get() (models.Settings, error) {
	start := time.Now()
	logx.Debugf("[store] 读取设置(SQLite)")
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		logx.Errorf("[store] 查询设置表失败: err=%v", err)
		return models.Settings{}, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			logx.Errorf("[store] 扫描设置行失败: err=%v", err)
			return models.Settings{}, err
		}
		values[k] = v
	}
	if err := rows.Err(); err != nil {
		logx.Errorf("[store] 遍历设置表失败: err=%v", err)
		return models.Settings{}, err
	}
	logx.Debugf("[store] 设置键读取完成(SQLite): keys=%d", len(values))
	st := models.Settings{
		UISchema:             2,
		ConfirmCloseTab:      true,
		ConfirmCloseApp:      true,
		Theme:                models.DefaultTheme(),
		Appearance:           models.DefaultAppearance(),
		Fonts:                models.DefaultFonts(),
		WebGLEnabled:         true,  // 默认开启 WebGL
		CompletionEnabled:    false, // 默认开启智能补全
		CompletionNavHotkey:  "Alt+ArrowDown",
		CompletionPanelLimit: 8,
		SftpToTerminalSync:   true,
		TerminalToSftpSync:   true,
		UIScale:              100,
		AutoReconnect:        true,
		KeepAliveEnabled:     true,
		// 登录后自动采集系统指标：默认开启（旧库缺键时同样补默认值）
		SysInfoCollectEnabled: true,
		TabBarPlacement:       "side", // 默认会话标签页放在左侧导航
		RightClickAction:      models.RightClickMenu,
		NavSectionOrder:       models.DefaultNavSectionOrder,
		// 保持原有行为：新数据库默认语义着色；已有库缺键同样回落 auto。
		TerminalColorMode: models.ColorModeAuto,
		LogLevel:          models.NormalizeLogLevel(""), // 日志级别默认 info
		// 调试模式默认值：新增读取和全部执行能力默认关闭
		Debug: models.DefaultDebugSettings(),
	}
	if v, ok := values["logEnabled"]; ok {
		st.LogEnabled = v == "true"
	}
	if v, ok := values["uiSchema"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			st.UISchema = n
		} else {
			logx.Warnf("[store] 设置项解析失败，UISchema 回落默认: key=uiSchema err=%v", err)
		}
	}
	if v, ok := values["copyOnSelect"]; ok {
		st.CopyOnSelect = v == "true"
	}
	if v, ok := values["webGLEnabled"]; ok {
		st.WebGLEnabled = v == "true"
	}
	if v, ok := values["completionEnabled"]; ok {
		st.CompletionEnabled = v == "true"
	}
	if v, ok := values["completionNavHotkey"]; ok && v != "" {
		st.CompletionNavHotkey = v
	}
	if v, ok := values["completionPanelLimit"]; ok && v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			st.CompletionPanelLimit = n
		} else {
			logx.Warnf("[store] 设置项非法，补全面板条数回落默认: key=completionPanelLimit")
		}
	}
	if v, ok := values["sftpToTerminalSync"]; ok {
		st.SftpToTerminalSync = v == "true"
	}
	if v, ok := values["terminalToSftpSync"]; ok {
		st.TerminalToSftpSync = v == "true"
	}
	if v, ok := values["uiScale"]; ok && v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			st.UIScale = n
		} else {
			logx.Warnf("[store] 设置项非法，界面缩放回落默认: key=uiScale")
		}
	}
	if v, ok := values["autoStart"]; ok {
		st.AutoStart = v == "true"
	}
	if v, ok := values["confirmCloseTab"]; ok {
		st.ConfirmCloseTab = v == "true"
	}
	if v, ok := values["confirmCloseApp"]; ok {
		st.ConfirmCloseApp = v == "true"
	}
	if v, ok := values["smoothAnimations"]; ok {
		st.SmoothAnimations = v == "true"
	}
	if v, ok := values["autoReconnect"]; ok {
		st.AutoReconnect = v == "true"
	}
	if v, ok := values["keepAliveEnabled"]; ok {
		st.KeepAliveEnabled = v == "true"
	}
	// 登录后自动采集系统指标：旧库没有该键 → 保持默认（开启）
	if v, ok := values["sysInfoRealtimeEnabled"]; ok {
		st.SysInfoRealtimeEnabled = v == "true"
	}
	if v, ok := values["sysInfoCollectEnabled"]; ok {
		st.SysInfoCollectEnabled = v == "true"
	}
	// 自动恢复上次会话：旧库没有该键 → 保持默认（关闭）
	if v, ok := values["autoLoginLastServer"]; ok {
		st.AutoLoginLastServer = v == "true"
	}
	// 上次打开的服务器 ID 列表落库为 JSON 数组字符串；解析失败按空处理（不阻断其余设置）
	if v, ok := values["lastOpenServers"]; ok && v != "" {
		var ids []string
		if err := json.Unmarshal([]byte(v), &ids); err == nil {
			st.LastOpenServers = ids
		} else {
			logx.Warnf("[store] 设置项解析失败，上次打开的服务器列表按空处理: key=lastOpenServers err=%v", err)
		}
	}
	// 日志：旧库没有这些键 → 保持默认（控制台 / 文件 / 调用日志全关，级别 info）
	if v, ok := values["logToFile"]; ok {
		st.LogToFile = v == "true"
	}
	if v, ok := values["logApiCalls"]; ok {
		st.LogAPICalls = v == "true"
	}
	if v, ok := values["logLevel"]; ok {
		if raw := strings.ToLower(strings.TrimSpace(v)); raw != "" && models.NormalizeLogLevel(v) != raw {
			logx.Warnf("[store] 设置项非法，日志级别回落 info: key=logLevel value=%q", v)
		}
		st.LogLevel = models.NormalizeLogLevel(v)
	}
	// 跟踪会话列表落库为 JSON 数组字符串；解析失败按空处理（不阻断其余设置）
	if v, ok := values["logTraceTabs"]; ok && v != "" {
		var tabs []string
		if err := json.Unmarshal([]byte(v), &tabs); err == nil {
			st.LogTraceTabs = tabs
		} else {
			logx.Warnf("[store] 设置项解析失败，日志跟踪会话列表按空处理: key=logTraceTabs err=%v", err)
		}
	}
	if v, ok := values["localShell"]; ok {
		st.LocalShell = v
	}
	// 标签页位置：旧库没有该键 → 保持默认「左侧导航」；非法值同样回落默认
	if v, ok := values["tabBarPlacement"]; ok {
		if v == "top" || v == "side" {
			st.TabBarPlacement = v
		} else {
			logx.Warnf("[store] 设置项非法，标签页位置回落 side: key=tabBarPlacement value=%q", v)
		}
	}
	// 右键行为：旧库没有该键 → 保持默认「打开选项栏」；非法值同样回落默认
	if v, ok := values["rightClickAction"]; ok {
		if v != "" && models.NormalizeRightClickAction(v) != v {
			logx.Warnf("[store] 设置项非法，右键行为回落默认: key=rightClickAction value=%q", v)
		}
		st.RightClickAction = models.NormalizeRightClickAction(v)
	}
	// 导航区段顺序：旧库没有该键 → 保持默认「导航 → 会话 → 标签页」；非法排列同样回落默认
	if v, ok := values["navSectionOrder"]; ok {
		if v != "" && models.NormalizeNavSectionOrder(v) != v {
			logx.Warnf("[store] 设置项非法，导航区段顺序回落默认: key=navSectionOrder value=%q", v)
		}
		st.NavSectionOrder = models.NormalizeNavSectionOrder(v)
	}
	// 语义着色档位：旧库没有该键 → 保持默认「语义着色」；非法值同样回落默认
	if v, ok := values["terminalColorMode"]; ok {
		if v != "" && models.NormalizeTerminalColorMode(v) != v {
			logx.Warnf("[store] 设置项非法，终端着色档位回落 auto: key=terminalColorMode value=%q", v)
		}
		st.TerminalColorMode = models.NormalizeTerminalColorMode(v)
	} else if len(values) > 0 {
		// An existing database predating this key keeps the historic auto behavior.
		st.TerminalColorMode = models.ColorModeAuto
	}
	if v, ok := values["theme"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &st.Theme); err != nil {
			logx.Warnf("[store] 设置项解析失败，主题回落默认: key=theme err=%v", err)
		}
	}
	// 调试模式：旧库没有该键 → 保持默认（关闭）；越界端口回落
	if v, ok := values["debug"]; ok && v != "" {
		var d models.DebugSettings
		if err := json.Unmarshal([]byte(v), &d); err == nil {
			st.Debug = d
		} else {
			logx.Warnf("[store] 设置项解析失败，调试模式回落默认: key=debug err=%v", err)
		}
		// 能力位：升级前的库里没有这些字段，反序列化得到「全 false」，
		// 缺失字段保持默认关闭，显式保存的已有能力保持原值。
		if !models.HasDebugCapabilityFields([]byte(v)) {
			st.Debug.CapTerminalInput = models.DefaultDebugSettings().CapTerminalInput
		}
	}
	st.Debug = models.NormalizeDebugSettings(st.Debug)
	if v, ok := values["appearance"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &st.Appearance); err != nil {
			logx.Warnf("[store] 设置项解析失败，外观回落默认: key=appearance err=%v", err)
		}
	}
	if v, ok := values["fonts"]; ok && v != "" {
		if err := json.Unmarshal([]byte(v), &st.Fonts); err != nil {
			logx.Warnf("[store] 设置项解析失败，字体回落默认: key=fonts err=%v", err)
		}
	}
	// 旧库可能缺少新键：补齐默认值
	if st.Appearance.Mode == "" {
		st.Appearance = models.DefaultAppearance()
	}
	if st.Fonts.TerminalFont == "" {
		st.Fonts = models.DefaultFonts()
	}
	if st.Theme.Black == "" {
		models.FillThemeAnsi(&st.Theme)
	}
	// 日志：级别与跟踪列表统一归一化（非法级别回落 info，列表去空去重）
	st.LogLevel = models.NormalizeLogLevel(st.LogLevel)
	st.LogTraceTabs = models.NormalizeLogTraceTabs(st.LogTraceTabs)
	// 上次打开的服务器列表：去空白项、保持顺序与重复项
	st.LastOpenServers = models.NormalizeLastOpenServers(st.LastOpenServers)
	// 语义着色档位：无论来源（旧库缺键 / 非法值）统一归一化，空值回落「语义着色」
	st.TerminalColorMode = models.NormalizeTerminalColorMode(st.TerminalColorMode)
	logx.Debugf("[store] 设置读取完成(SQLite): keys=%d logLevel=%s traceTabs=%d lastOpen=%d elapsed=%s",
		len(values), st.LogLevel, len(st.LogTraceTabs), len(st.LastOpenServers), time.Since(start))
	return st, nil
}

// Save 保存设置（upsert 各键）。
func (s *SQLiteSettingsStore) Save(st models.Settings) error {
	start := time.Now()
	logx.Debugf("[store] 保存设置(SQLite): logEnabled=%v logToFile=%v logLevel=%s autoStart=%v",
		st.LogEnabled, st.LogToFile, st.LogLevel, st.AutoStart)
	if st.UISchema < 2 {
		logx.Debugf("[store] 设置 UISchema 过低，升级为 2: before=%d", st.UISchema)
		st.UISchema = 2
	}
	theme, err := json.Marshal(st.Theme)
	if err != nil {
		logx.Errorf("[store] 序列化主题失败: err=%v", err)
		return fmt.Errorf(i18n.T("序列化主题失败: %w"), err)
	}
	appearance, err := json.Marshal(st.Appearance)
	if err != nil {
		logx.Errorf("[store] 序列化外观失败: err=%v", err)
		return fmt.Errorf(i18n.T("序列化外观失败: %w"), err)
	}
	fonts, err := json.Marshal(st.Fonts)
	if err != nil {
		logx.Errorf("[store] 序列化字体失败: err=%v", err)
		return fmt.Errorf(i18n.T("序列化字体失败: %w"), err)
	}
	if st.CompletionNavHotkey == "" {
		logx.Warnf("[store] 保存设置时补全导航快捷键为空，回落默认: key=completionNavHotkey")
		st.CompletionNavHotkey = "Alt+ArrowDown"
	}
	if st.CompletionPanelLimit <= 0 {
		logx.Warnf("[store] 保存设置时补全面板条数非法，回落默认: key=completionPanelLimit value=%d", st.CompletionPanelLimit)
		st.CompletionPanelLimit = 8
	}
	if st.Appearance.Mode == "" {
		st.Appearance = models.DefaultAppearance()
	}
	if st.Fonts.TerminalFont == "" {
		st.Fonts = models.DefaultFonts()
	}
	// 标签页位置：仅接受 top / side，其余（含空值）落回默认「左侧导航」
	if st.TabBarPlacement != "top" && st.TabBarPlacement != "side" {
		if st.TabBarPlacement != "" {
			logx.Warnf("[store] 保存设置时标签页位置非法，回落 side: value=%q", st.TabBarPlacement)
		}
		st.TabBarPlacement = "side"
	}
	// 右键行为：仅接受 menu / paste，其余（含空值）落回默认「打开选项栏」
	if raw := st.RightClickAction; raw != "" && models.NormalizeRightClickAction(raw) != raw {
		logx.Warnf("[store] 保存设置时右键行为非法，回落默认: value=%q", raw)
	}
	st.RightClickAction = models.NormalizeRightClickAction(st.RightClickAction)
	// 导航区段顺序：必须是 nav / sessions / tabs 的排列，其余（含空值）落回默认
	if raw := st.NavSectionOrder; raw != "" && models.NormalizeNavSectionOrder(raw) != raw {
		logx.Warnf("[store] 保存设置时导航区段顺序非法，回落默认: value=%q", raw)
	}
	st.NavSectionOrder = models.NormalizeNavSectionOrder(st.NavSectionOrder)
	// 调试模式：端口越界回落默认
	st.Debug = models.NormalizeDebugSettings(st.Debug)
	debugJSON, err := json.Marshal(st.Debug)
	if err != nil {
		logx.Errorf("[store] 序列化调试模式设置失败: err=%v", err)
		return fmt.Errorf(i18n.T("序列化调试模式设置失败: %w"), err)
	}
	// 日志：级别非法回落 info，跟踪列表落库为 JSON 数组字符串
	st.LogLevel = models.NormalizeLogLevel(st.LogLevel)
	traceJSON, err := json.Marshal(models.NormalizeLogTraceTabs(st.LogTraceTabs))
	if err != nil {
		logx.Errorf("[store] 序列化日志跟踪列表失败: err=%v", err)
		return fmt.Errorf(i18n.T("序列化日志跟踪列表失败: %w"), err)
	}
	// 上次打开的服务器 ID 列表：去空白项、保持顺序与重复项，落库为 JSON 数组字符串
	openServersJSON, err := json.Marshal(models.NormalizeLastOpenServers(st.LastOpenServers))
	if err != nil {
		logx.Errorf("[store] 序列化上次打开的服务器列表失败: err=%v", err)
		return fmt.Errorf(i18n.T("序列化上次打开的服务器列表失败: %w"), err)
	}
	entries := []struct{ k, v string }{
		{"uiSchema", fmt.Sprintf("%d", st.UISchema)},
		{"logEnabled", boolStr(st.LogEnabled)},
		{"logToFile", boolStr(st.LogToFile)},
		{"logLevel", st.LogLevel},
		{"logApiCalls", boolStr(st.LogAPICalls)},
		{"logTraceTabs", string(traceJSON)},
		{"copyOnSelect", boolStr(st.CopyOnSelect)},
		{"webGLEnabled", boolStr(st.WebGLEnabled)},
		{"completionEnabled", boolStr(st.CompletionEnabled)},
		{"completionNavHotkey", st.CompletionNavHotkey},
		{"completionPanelLimit", fmt.Sprintf("%d", st.CompletionPanelLimit)},
		{"sftpToTerminalSync", boolStr(st.SftpToTerminalSync)},
		{"terminalToSftpSync", boolStr(st.TerminalToSftpSync)},
		{"uiScale", fmt.Sprintf("%d", st.UIScale)},
		{"theme", string(theme)},
		{"appearance", string(appearance)},
		{"fonts", string(fonts)},
		{"autoReconnect", boolStr(st.AutoReconnect)},
		{"autoStart", boolStr(st.AutoStart)},
		{"confirmCloseTab", boolStr(st.ConfirmCloseTab)},
		{"confirmCloseApp", boolStr(st.ConfirmCloseApp)},
		{"smoothAnimations", boolStr(st.SmoothAnimations)},

		{"keepAliveEnabled", boolStr(st.KeepAliveEnabled)},
		{"sysInfoCollectEnabled", boolStr(st.SysInfoCollectEnabled)},
		{"sysInfoRealtimeEnabled", boolStr(st.SysInfoRealtimeEnabled)},
		{"autoLoginLastServer", boolStr(st.AutoLoginLastServer)},
		{"lastOpenServers", string(openServersJSON)},
		{"localShell", st.LocalShell},
		{"tabBarPlacement", st.TabBarPlacement},
		{"rightClickAction", st.RightClickAction},
		{"navSectionOrder", st.NavSectionOrder},
		{"terminalColorMode", st.TerminalColorMode},
		{"debug", string(debugJSON)},
	}
	for _, e := range entries {
		if _, err := s.db.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, e.k, e.v); err != nil {
			logx.Errorf("[store] 保存设置项失败: key=%s err=%v", e.k, err)
			return fmt.Errorf(i18n.T("保存设置失败: %w"), err)
		}
	}
	logx.Infof("[store] 设置已保存(SQLite): keys=%d elapsed=%s", len(entries), time.Since(start))
	return nil
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// SQLiteCredentialStore 基于 SQLite 的凭证存储。
type SQLiteCredentialStore struct {
	cipherMu sync.RWMutex
	db       *sql.DB
	cipher   cryptox.FieldCipher
}

// NewSQLiteCredentialStore 基于已打开的数据库创建凭证存储。
func NewSQLiteCredentialStore(db *sql.DB) *SQLiteCredentialStore {
	logx.Debugf("[store] 创建 SQLite 凭证存储")
	return &SQLiteCredentialStore{db: db}
}

// SetCipher 注入敏感字段加解密器。
func (s *SQLiteCredentialStore) SetCipher(c cryptox.FieldCipher) {
	s.cipherMu.Lock()
	defer s.cipherMu.Unlock()
	s.cipher = c
	logx.Debugf("[store] 凭证存储已注入字段加解密器: enabled=%v", c != nil)
}

// List 返回全部凭证。
func (s *SQLiteCredentialStore) List() ([]models.Credential, error) {
	s.cipherMu.RLock()
	defer s.cipherMu.RUnlock()
	defer cryptox.BeginFieldOperation(s.cipher)()
	start := time.Now()
	logx.Debugf("[store] 读取凭证列表(SQLite): cipher=%v", s.cipher != nil)
	rows, err := s.db.Query(`SELECT id, name, user, password, auth_type, key_path, key_content FROM credentials ORDER BY name`)
	if err != nil {
		logx.Errorf("[store] 查询凭证列表失败: err=%v", err)
		return nil, err
	}
	defer rows.Close()
	list := []models.Credential{}
	for rows.Next() {
		var c models.Credential
		if err := rows.Scan(&c.ID, &c.Name, &c.User, &c.Password, &c.AuthType, &c.KeyPath, &c.KeyContent); err != nil {
			logx.Errorf("[store] 扫描凭证行失败: err=%v", err)
			return nil, err
		}
		pw, err := cryptox.OpenField(s.cipher, c.Password)
		if err != nil {
			logx.Errorf("[store] 解密凭证密码失败: id=%s err=%v", c.ID, err)
			return nil, fmt.Errorf(i18n.T("解密凭证密码失败: %w"), err)
		}
		kc, err := cryptox.OpenField(s.cipher, c.KeyContent)
		if err != nil {
			logx.Errorf("[store] 解密凭证私钥失败: id=%s err=%v", c.ID, err)
			return nil, fmt.Errorf(i18n.T("解密凭证私钥失败: %w"), err)
		}
		c.Password = pw
		c.KeyContent = kc
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		logx.Errorf("[store] 遍历凭证列表失败: err=%v", err)
		return nil, err
	}
	logx.Debugf("[store] 凭证列表读取完成(SQLite): count=%d elapsed=%s", len(list), time.Since(start))
	return list, nil
}

// Save 新增或更新凭证（按 ID 匹配）。
func (s *SQLiteCredentialStore) Save(c models.Credential) error {
	s.cipherMu.RLock()
	defer s.cipherMu.RUnlock()
	defer cryptox.BeginFieldOperation(s.cipher)()
	start := time.Now()
	logx.Debugf("[store] 保存凭证(SQLite): id=%s name=%q user=%s auth=%s", c.ID, c.Name, c.User, c.AuthType)
	if c.AuthType == "" {
		c.AuthType = "password"
		logx.Debugf("[store] 凭证未指定认证方式，回落 password: id=%s", c.ID)
	}
	password, err := cryptox.SealPlain(s.cipher, c.Password)
	if err != nil {
		logx.Errorf("[store] 加密凭证密码失败: id=%s err=%v", c.ID, err)
		return fmt.Errorf(i18n.T("加密凭证密码失败: %w"), err)
	}
	keyContent, err := cryptox.SealPlain(s.cipher, c.KeyContent)
	if err != nil {
		logx.Errorf("[store] 加密凭证私钥失败: id=%s err=%v", c.ID, err)
		return fmt.Errorf(i18n.T("加密凭证私钥失败: %w"), err)
	}
	_, err = s.db.Exec(`
		INSERT INTO credentials (id, name, user, password, auth_type, key_path, key_content) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, user = excluded.user,
			password = excluded.password, auth_type = excluded.auth_type,
			key_path = excluded.key_path, key_content = excluded.key_content`,
		c.ID, c.Name, c.User, password, c.AuthType, c.KeyPath, keyContent)
	if err != nil {
		logx.Errorf("[store] 保存凭证失败(SQLite): id=%s name=%q err=%v", c.ID, c.Name, err)
		return fmt.Errorf(i18n.T("保存凭证失败: %w"), err)
	}
	logx.Infof("[store] 保存凭证成功(SQLite): id=%s name=%q elapsed=%s", c.ID, c.Name, time.Since(start))
	return nil
}

// Delete 按 ID 删除凭证。
func (s *SQLiteCredentialStore) Delete(id string) error {
	start := time.Now()
	logx.Debugf("[store] 删除凭证(SQLite): id=%s", id)
	res, err := s.db.Exec(`DELETE FROM credentials WHERE id = ?`, id)
	if err != nil {
		logx.Errorf("[store] 删除凭证失败(SQLite): id=%s err=%v", id, err)
		return fmt.Errorf(i18n.T("删除凭证失败: %w"), err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		logx.Warnf("[store] 删除凭证(SQLite)未命中: id=%s", id)
	} else {
		logx.Infof("[store] 删除凭证成功(SQLite): id=%s affected=%d elapsed=%s", id, affected, time.Since(start))
	}
	return nil
}

// SQLiteGroupStore 旧版扁平分组存储，仅保留给 MigrateLegacyJSON 迁移使用。
// 新代码请使用路径化的 SQLiteGroupTreeStore（见 grouptree.go），它会在事务内级联服务器分组。
type SQLiteGroupStore struct {
	db *sql.DB
}

// NewSQLiteGroupStore 基于已打开的数据库创建分组存储。
func NewSQLiteGroupStore(db *sql.DB) *SQLiteGroupStore {
	logx.Debugf("[store] 创建 SQLite 分组存储")
	return &SQLiteGroupStore{db: db}
}

// List 返回全部分组（已排序）。
func (s *SQLiteGroupStore) List() ([]string, error) {
	start := time.Now()
	logx.Debugf("[store] 读取分组列表(SQLite)")
	rows, err := s.db.Query(`SELECT name FROM groups`)
	if err != nil {
		logx.Errorf("[store] 查询分组列表失败: err=%v", err)
		return nil, err
	}
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			logx.Errorf("[store] 扫描分组行失败: err=%v", err)
			return nil, err
		}
		list = append(list, name)
	}
	if err := rows.Err(); err != nil {
		logx.Errorf("[store] 遍历分组列表失败: err=%v", err)
		return nil, err
	}
	sort.Strings(list)
	logx.Debugf("[store] 分组列表读取完成(SQLite): count=%d elapsed=%s", len(list), time.Since(start))
	return list, nil
}

// Add 新增分组（已存在则忽略）。
func (s *SQLiteGroupStore) Add(name string) error {
	start := time.Now()
	logx.Debugf("[store] 新增分组(SQLite): name=%q", name)
	if _, err := s.db.Exec(`INSERT INTO groups (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, name); err != nil {
		logx.Errorf("[store] 新增分组失败(SQLite): name=%q err=%v", name, err)
		return fmt.Errorf(i18n.T("新增分组失败: %w"), err)
	}
	logx.Infof("[store] 新增分组成功(SQLite): name=%q elapsed=%s", name, time.Since(start))
	return nil
}

// Rename 重命名分组。
func (s *SQLiteGroupStore) Rename(oldName, newName string) error {
	start := time.Now()
	logx.Debugf("[store] 重命名分组(SQLite): old=%q new=%q", oldName, newName)
	res, err := s.db.Exec(`UPDATE groups SET name = ? WHERE name = ?`, newName, oldName)
	if err != nil {
		logx.Errorf("[store] 重命名分组失败(SQLite): old=%q new=%q err=%v", oldName, newName, err)
		return fmt.Errorf(i18n.T("重命名分组失败: %w"), err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		logx.Warnf("[store] 重命名分组(SQLite)未命中: old=%q new=%q", oldName, newName)
	} else {
		logx.Infof("[store] 重命名分组成功(SQLite): old=%q new=%q affected=%d elapsed=%s",
			oldName, newName, affected, time.Since(start))
	}
	return nil
}

// Remove 删除分组。
func (s *SQLiteGroupStore) Remove(name string) error {
	start := time.Now()
	logx.Debugf("[store] 删除分组(SQLite): name=%q", name)
	res, err := s.db.Exec(`DELETE FROM groups WHERE name = ?`, name)
	if err != nil {
		logx.Errorf("[store] 删除分组失败(SQLite): name=%q err=%v", name, err)
		return fmt.Errorf(i18n.T("删除分组失败: %w"), err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		logx.Warnf("[store] 删除分组(SQLite)未命中: name=%q", name)
	} else {
		logx.Infof("[store] 删除分组成功(SQLite): name=%q affected=%d elapsed=%s", name, affected, time.Since(start))
	}
	return nil
}

// MigrateLegacyJSON 将旧版 JSON 数据一次性导入 SQLite（仅在对应表为空时执行）。
// 迁移成功后保留 JSON 文件作为备份，不自动删除。
func MigrateLegacyJSON(db *sql.DB) error {
	start := time.Now()
	base := appdir.ConfigDir()
	logx.Infof("[store] 开始旧版 JSON 数据迁移: dir=%q", base)

	if empty, err := tableEmpty(db, "servers"); err != nil {
		logx.Errorf("[store] 迁移检查 servers 表失败: err=%v", err)
		return err
	} else if empty {
		logx.Debugf("[store] servers 表为空，检查旧版 servers.json")
		js, jerr := NewJSONStore(filepath.Join(base, "servers.json"))
		if jerr != nil {
			logx.Warnf("[store] 打开旧版服务器 JSON 存储失败，跳过服务器迁移: err=%v", jerr)
		} else if nodes, lerr := js.List(); lerr != nil {
			logx.Warnf("[store] 读取旧版服务器 JSON 失败，跳过服务器迁移: err=%v", lerr)
		} else if len(nodes) > 0 {
			ss := NewSQLiteStore(db)
			for _, n := range nodes {
				if serr := ss.Save(n); serr != nil {
					logx.Warnf("[store] 迁移服务器数据中止: id=%s err=%v", n.ID, serr)
					return fmt.Errorf(i18n.T("迁移服务器数据失败: %w"), serr)
				}
			}
			logx.Infof("[store] 旧版服务器数据已迁移: count=%d", len(nodes))
		} else {
			logx.Debugf("[store] 旧版 servers.json 无数据，跳过服务器迁移")
		}
	}

	if empty, err := tableEmpty(db, "settings"); err != nil {
		logx.Errorf("[store] 迁移检查 settings 表失败: err=%v", err)
		return err
	} else if empty {
		logx.Debugf("[store] settings 表为空，检查旧版 settings.json")
		js, jerr := NewJSONSettingsStore(filepath.Join(base, "settings.json"))
		if jerr != nil {
			logx.Warnf("[store] 打开旧版设置 JSON 存储失败，跳过设置迁移: err=%v", jerr)
		} else if st, serr := js.Get(); serr != nil {
			logx.Warnf("[store] 读取旧版设置 JSON 失败，跳过设置迁移: err=%v", serr)
		} else if st.LogEnabled || st.CopyOnSelect || !themeIsDefault(st.Theme) {
			if serr := NewSQLiteSettingsStore(db).Save(st); serr != nil {
				logx.Warnf("[store] 迁移设置数据中止: err=%v", serr)
				return fmt.Errorf(i18n.T("迁移设置数据失败: %w"), serr)
			}
			logx.Infof("[store] 旧版设置数据已迁移")
		} else {
			logx.Debugf("[store] 旧版设置为默认值，跳过设置迁移")
		}
	}

	if empty, err := tableEmpty(db, "credentials"); err != nil {
		logx.Errorf("[store] 迁移检查 credentials 表失败: err=%v", err)
		return err
	} else if empty {
		logx.Debugf("[store] credentials 表为空，检查旧版 credentials.json")
		js, jerr := NewJSONCredentialStore(filepath.Join(base, "credentials.json"))
		if jerr != nil {
			logx.Warnf("[store] 打开旧版凭证 JSON 存储失败，跳过凭证迁移: err=%v", jerr)
		} else if list, lerr := js.List(); lerr != nil {
			logx.Warnf("[store] 读取旧版凭证 JSON 失败，跳过凭证迁移: err=%v", lerr)
		} else if len(list) > 0 {
			cs := NewSQLiteCredentialStore(db)
			for _, c := range list {
				if serr := cs.Save(c); serr != nil {
					logx.Warnf("[store] 迁移凭证数据中止: id=%s err=%v", c.ID, serr)
					return fmt.Errorf(i18n.T("迁移凭证数据失败: %w"), serr)
				}
			}
			logx.Infof("[store] 旧版凭证数据已迁移: count=%d", len(list))
		} else {
			logx.Debugf("[store] 旧版 credentials.json 无数据，跳过凭证迁移")
		}
	}

	if empty, err := tableEmpty(db, "groups"); err != nil {
		logx.Errorf("[store] 迁移检查 groups 表失败: err=%v", err)
		return err
	} else if empty {
		logx.Debugf("[store] groups 表为空，检查旧版 groups.json")
		js, jerr := NewJSONGroupStore(filepath.Join(base, "groups.json"))
		if jerr != nil {
			logx.Warnf("[store] 打开旧版分组 JSON 存储失败，跳过分组迁移: err=%v", jerr)
		} else if list, lerr := js.List(); lerr != nil {
			logx.Warnf("[store] 读取旧版分组 JSON 失败，跳过分组迁移: err=%v", lerr)
		} else if len(list) > 0 {
			gs := NewSQLiteGroupStore(db)
			for _, g := range list {
				if serr := gs.Add(g); serr != nil {
					logx.Warnf("[store] 迁移分组数据中止: name=%q err=%v", g, serr)
					return fmt.Errorf(i18n.T("迁移分组数据失败: %w"), serr)
				}
			}
			logx.Infof("[store] 旧版分组数据已迁移: count=%d", len(list))
		} else {
			logx.Debugf("[store] 旧版 groups.json 无数据，跳过分组迁移")
		}
	}

	logx.Infof("[store] 旧版 JSON 数据迁移完成: elapsed=%s", time.Since(start))
	return nil
}

func tableEmpty(db *sql.DB, table string) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

func themeIsDefault(t models.Theme) bool {
	def := models.DefaultTheme()
	return t == def
}

// 编译期断言：各实现满足对应接口。
var (
	_ Store           = (*SQLiteStore)(nil)
	_ SettingsStore   = (*SQLiteSettingsStore)(nil)
	_ CredentialStore = (*SQLiteCredentialStore)(nil)
)
