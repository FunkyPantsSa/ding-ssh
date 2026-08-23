package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"ding-ssh/internal/cryptox"
	"ding-ssh/internal/models"

	_ "modernc.org/sqlite"
)

// DefaultSQLitePath 返回默认 SQLite 数据库文件路径。
func DefaultSQLitePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "ding-ssh.db"
	}
	return filepath.Join(dir, "ding-ssh", "ding-ssh.db")
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
	if path == "" {
		path = DefaultSQLitePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// WAL 提升并发读写体验；busy_timeout 避免多连接写冲突。
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 WAL 模式失败: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 busy_timeout 失败: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}
	// 凭证表新列迁移（旧数据库可能不含 auth_type / key_path / key_content）。
	for _, stmt := range []string{
		`ALTER TABLE credentials ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'password'`,
		`ALTER TABLE credentials ADD COLUMN key_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE credentials ADD COLUMN key_content TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = db.Exec(stmt) // 列已存在时忽略错误
	}
	return db, nil
}

// SQLiteStore 基于 SQLite 的服务器节点存储。
type SQLiteStore struct {
	db     *sql.DB
	cipher cryptox.FieldCipher
}

// NewSQLiteStore 基于已打开的数据库创建服务器存储。
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// SetCipher 注入敏感字段加解密器（Phase 4A）。
func (s *SQLiteStore) SetCipher(c cryptox.FieldCipher) {
	s.cipher = c
}

func scanServer(row interface{ Scan(...any) error }) (models.ServerNode, error) {
	var n models.ServerNode
	var env string
	var group string
	if err := row.Scan(
		&n.ID, &n.Name, &group, &n.Host, &n.Port, &n.User,
		&n.AuthType, &n.Password, &n.KeyPath, &n.KeyContent,
		&n.BgImage, &n.BlurAmount, &env,
	); err != nil {
		return n, err
	}
	n.Group = group
	n.EnvVars = map[string]string{}
	if err := json.Unmarshal([]byte(env), &n.EnvVars); err != nil {
		n.EnvVars = map[string]string{}
	}
	return n, nil
}

// List 返回全部服务器节点。
func (s *SQLiteStore) List() ([]models.ServerNode, error) {
	rows, err := s.db.Query(`
		SELECT id, name, grp, host, port, user, auth_type, password, key_path,
		       key_content, bg_image, blur_amount, env_vars
		FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []models.ServerNode{}
	for rows.Next() {
		n, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		if err := s.decryptServer(&n); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// Save 新增或更新服务器节点（按 ID 匹配）。
func (s *SQLiteStore) Save(node models.ServerNode) error {
	env, err := json.Marshal(node.EnvVars)
	if err != nil {
		return fmt.Errorf("序列化环境变量失败: %w", err)
	}
	password, err := cryptox.SealPlain(s.cipher, node.Password)
	if err != nil {
		return fmt.Errorf("加密密码失败: %w", err)
	}
	keyContent, err := cryptox.SealPlain(s.cipher, node.KeyContent)
	if err != nil {
		return fmt.Errorf("加密私钥失败: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO servers (id, name, grp, host, port, user, auth_type, password,
		                    key_path, key_content, bg_image, blur_amount, env_vars)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, grp = excluded.grp, host = excluded.host,
			port = excluded.port, user = excluded.user, auth_type = excluded.auth_type,
			password = excluded.password, key_path = excluded.key_path,
			key_content = excluded.key_content, bg_image = excluded.bg_image,
			blur_amount = excluded.blur_amount, env_vars = excluded.env_vars`,
		node.ID, node.Name, node.Group, node.Host, node.Port, node.User,
		node.AuthType, password, node.KeyPath, keyContent,
		node.BgImage, node.BlurAmount, string(env),
	)
	if err != nil {
		return fmt.Errorf("保存服务器失败: %w", err)
	}
	return nil
}

func (s *SQLiteStore) decryptServer(n *models.ServerNode) error {
	pw, err := cryptox.OpenField(s.cipher, n.Password)
	if err != nil {
		return fmt.Errorf("解密服务器密码失败: %w", err)
	}
	kc, err := cryptox.OpenField(s.cipher, n.KeyContent)
	if err != nil {
		return fmt.Errorf("解密服务器私钥失败: %w", err)
	}
	n.Password = pw
	n.KeyContent = kc
	return nil
}

// Delete 按 ID 删除服务器节点。
func (s *SQLiteStore) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM servers WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除服务器失败: %w", err)
	}
	return nil
}

// SQLiteSettingsStore 基于 SQLite 的设置存储（key-value 表）。
type SQLiteSettingsStore struct {
	db *sql.DB
}

// NewSQLiteSettingsStore 基于已打开的数据库创建设置存储。
func NewSQLiteSettingsStore(db *sql.DB) *SQLiteSettingsStore {
	return &SQLiteSettingsStore{db: db}
}

// Get 读取设置，缺少的键使用默认值（日志默认关闭）。
func (s *SQLiteSettingsStore) Get() (models.Settings, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return models.Settings{}, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return models.Settings{}, err
		}
		values[k] = v
	}
	if err := rows.Err(); err != nil {
		return models.Settings{}, err
	}
	st := models.Settings{
		Theme:                models.DefaultTheme(),
		WebGLEnabled:         true, // 默认开启 WebGL
		CompletionEnabled:    true, // 默认开启智能补全
		CompletionNavHotkey:  "Alt+ArrowDown",
		CompletionPanelLimit: 8,
		SftpToTerminalSync:   true,
		TerminalToSftpSync:   true,
		UIScale:              100,
		AutoReconnect:        true,
		KeepAliveEnabled:     true,
	}
	if v, ok := values["logEnabled"]; ok {
		st.LogEnabled = v == "true"
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
		}
	}
	if v, ok := values["autoReconnect"]; ok {
		st.AutoReconnect = v == "true"
	}
	if v, ok := values["keepAliveEnabled"]; ok {
		st.KeepAliveEnabled = v == "true"
	}
	if v, ok := values["localShell"]; ok {
		st.LocalShell = v
	}
	if v, ok := values["theme"]; ok && v != "" {
		_ = json.Unmarshal([]byte(v), &st.Theme)
	}
	return st, nil
}

// Save 保存设置（upsert 各键）。
func (s *SQLiteSettingsStore) Save(st models.Settings) error {
	theme, err := json.Marshal(st.Theme)
	if err != nil {
		return fmt.Errorf("序列化主题失败: %w", err)
	}
	if st.CompletionNavHotkey == "" {
		st.CompletionNavHotkey = "Alt+ArrowDown"
	}
	if st.CompletionPanelLimit <= 0 {
		st.CompletionPanelLimit = 8
	}
	entries := []struct{ k, v string }{
		{"logEnabled", boolStr(st.LogEnabled)},
		{"copyOnSelect", boolStr(st.CopyOnSelect)},
		{"webGLEnabled", boolStr(st.WebGLEnabled)},
		{"completionEnabled", boolStr(st.CompletionEnabled)},
		{"completionNavHotkey", st.CompletionNavHotkey},
		{"completionPanelLimit", fmt.Sprintf("%d", st.CompletionPanelLimit)},
		{"sftpToTerminalSync", boolStr(st.SftpToTerminalSync)},
		{"terminalToSftpSync", boolStr(st.TerminalToSftpSync)},
		{"uiScale", fmt.Sprintf("%d", st.UIScale)},
		{"theme", string(theme)},
		{"autoReconnect", boolStr(st.AutoReconnect)},
		{"keepAliveEnabled", boolStr(st.KeepAliveEnabled)},
		{"localShell", st.LocalShell},
	}
	for _, e := range entries {
		if _, err := s.db.Exec(`
			INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, e.k, e.v); err != nil {
			return fmt.Errorf("保存设置失败: %w", err)
		}
	}
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
	db     *sql.DB
	cipher cryptox.FieldCipher
}

// NewSQLiteCredentialStore 基于已打开的数据库创建凭证存储。
func NewSQLiteCredentialStore(db *sql.DB) *SQLiteCredentialStore {
	return &SQLiteCredentialStore{db: db}
}

// SetCipher 注入敏感字段加解密器。
func (s *SQLiteCredentialStore) SetCipher(c cryptox.FieldCipher) {
	s.cipher = c
}

// List 返回全部凭证。
func (s *SQLiteCredentialStore) List() ([]models.Credential, error) {
	rows, err := s.db.Query(`SELECT id, name, user, password, auth_type, key_path, key_content FROM credentials ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []models.Credential{}
	for rows.Next() {
		var c models.Credential
		if err := rows.Scan(&c.ID, &c.Name, &c.User, &c.Password, &c.AuthType, &c.KeyPath, &c.KeyContent); err != nil {
			return nil, err
		}
		pw, err := cryptox.OpenField(s.cipher, c.Password)
		if err != nil {
			return nil, fmt.Errorf("解密凭证密码失败: %w", err)
		}
		kc, err := cryptox.OpenField(s.cipher, c.KeyContent)
		if err != nil {
			return nil, fmt.Errorf("解密凭证私钥失败: %w", err)
		}
		c.Password = pw
		c.KeyContent = kc
		list = append(list, c)
	}
	return list, rows.Err()
}

// Save 新增或更新凭证（按 ID 匹配）。
func (s *SQLiteCredentialStore) Save(c models.Credential) error {
	if c.AuthType == "" {
		c.AuthType = "password"
	}
	password, err := cryptox.SealPlain(s.cipher, c.Password)
	if err != nil {
		return fmt.Errorf("加密凭证密码失败: %w", err)
	}
	keyContent, err := cryptox.SealPlain(s.cipher, c.KeyContent)
	if err != nil {
		return fmt.Errorf("加密凭证私钥失败: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO credentials (id, name, user, password, auth_type, key_path, key_content) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, user = excluded.user,
			password = excluded.password, auth_type = excluded.auth_type,
			key_path = excluded.key_path, key_content = excluded.key_content`,
		c.ID, c.Name, c.User, password, c.AuthType, c.KeyPath, keyContent)
	if err != nil {
		return fmt.Errorf("保存凭证失败: %w", err)
	}
	return nil
}

// Delete 按 ID 删除凭证。
func (s *SQLiteCredentialStore) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM credentials WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除凭证失败: %w", err)
	}
	return nil
}

// SQLiteGroupStore 基于 SQLite 的空分组存储。
// 分组实际归属由服务器节点的 Group 字段决定，这里仅保存「手动创建但暂无服务器的分组」。
type SQLiteGroupStore struct {
	db *sql.DB
}

// NewSQLiteGroupStore 基于已打开的数据库创建分组存储。
func NewSQLiteGroupStore(db *sql.DB) *SQLiteGroupStore {
	return &SQLiteGroupStore{db: db}
}

// List 返回全部分组（已排序）。
func (s *SQLiteGroupStore) List() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM groups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		list = append(list, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(list)
	return list, nil
}

// Add 新增分组（已存在则忽略）。
func (s *SQLiteGroupStore) Add(name string) error {
	_, err := s.db.Exec(`INSERT INTO groups (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, name)
	if err != nil {
		return fmt.Errorf("新增分组失败: %w", err)
	}
	return nil
}

// Rename 重命名分组。
func (s *SQLiteGroupStore) Rename(oldName, newName string) error {
	if _, err := s.db.Exec(`UPDATE groups SET name = ? WHERE name = ?`, newName, oldName); err != nil {
		return fmt.Errorf("重命名分组失败: %w", err)
	}
	return nil
}

// Remove 删除分组。
func (s *SQLiteGroupStore) Remove(name string) error {
	if _, err := s.db.Exec(`DELETE FROM groups WHERE name = ?`, name); err != nil {
		return fmt.Errorf("删除分组失败: %w", err)
	}
	return nil
}

// MigrateLegacyJSON 将旧版 JSON 数据一次性导入 SQLite（仅在对应表为空时执行）。
// 迁移成功后保留 JSON 文件作为备份，不自动删除。
func MigrateLegacyJSON(db *sql.DB) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	base := filepath.Join(dir, "ding-ssh")

	if empty, err := tableEmpty(db, "servers"); err != nil {
		return err
	} else if empty {
		js, jerr := NewJSONStore(filepath.Join(base, "servers.json"))
		if jerr == nil {
			if nodes, lerr := js.List(); lerr == nil && len(nodes) > 0 {
				ss := NewSQLiteStore(db)
				for _, n := range nodes {
					if serr := ss.Save(n); serr != nil {
						return fmt.Errorf("迁移服务器数据失败: %w", serr)
					}
				}
			}
		}
	}

	if empty, err := tableEmpty(db, "settings"); err != nil {
		return err
	} else if empty {
		js, jerr := NewJSONSettingsStore(filepath.Join(base, "settings.json"))
		if jerr == nil {
			if st, serr := js.Get(); serr == nil && (st.LogEnabled || st.CopyOnSelect || !themeIsDefault(st.Theme)) {
				if serr := NewSQLiteSettingsStore(db).Save(st); serr != nil {
					return fmt.Errorf("迁移设置数据失败: %w", serr)
				}
			}
		}
	}

	if empty, err := tableEmpty(db, "credentials"); err != nil {
		return err
	} else if empty {
		js, jerr := NewJSONCredentialStore(filepath.Join(base, "credentials.json"))
		if jerr == nil {
			if list, lerr := js.List(); lerr == nil && len(list) > 0 {
				cs := NewSQLiteCredentialStore(db)
				for _, c := range list {
					if serr := cs.Save(c); serr != nil {
						return fmt.Errorf("迁移凭证数据失败: %w", serr)
					}
				}
			}
		}
	}

	if empty, err := tableEmpty(db, "groups"); err != nil {
		return err
	} else if empty {
		js, jerr := NewJSONGroupStore(filepath.Join(base, "groups.json"))
		if jerr == nil {
			if list, lerr := js.List(); lerr == nil && len(list) > 0 {
				gs := NewSQLiteGroupStore(db)
				for _, g := range list {
					if serr := gs.Add(g); serr != nil {
						return fmt.Errorf("迁移分组数据失败: %w", serr)
					}
				}
			}
		}
	}

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
	_ GroupStore      = (*SQLiteGroupStore)(nil)
)
