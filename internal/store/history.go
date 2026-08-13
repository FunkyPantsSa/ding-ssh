package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ding-ssh/internal/models"
)

const (
	historyMaxPerServer = 2000
	historyMaxCmdLen    = 2000
)

var (
	ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	ansiEsc = regexp.MustCompile(`\x1b.`)
	reKeyNoise = regexp.MustCompile(`^(\[[A-D]|O[A-D]|~)+$`)
	reGlueOut = regexp.MustCompile(`[a-z0-9](warning|NAME|STATUS|Ready|error:)`)
	reUserHost = regexp.MustCompile(`[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:`)
)

// sanitizeHistoryCommand 去除方向键/ANSI 转义残留，避免污染命令历史。
func sanitizeHistoryCommand(command string) string {
	command = ansiCSI.ReplaceAllString(command, "")
	command = ansiOSC.ReplaceAllString(command, "")
	command = ansiEsc.ReplaceAllString(command, "")
	command = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' {
			return -1
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, command)
	// 折叠多余空白，便于「kubectl  get  node」与「kubectl get node」去重合并
	command = strings.Join(strings.Fields(command), " ")
	return command
}

// HistoryStore 命令历史存储接口。
type HistoryStore interface {
	Add(serverID, command string) error
	Query(serverID, prefix string, limit int) ([]models.CommandSuggestion, error)
	Clear(serverID string) error // serverID 为空则清空全部
}

// SQLiteHistoryStore 基于 SQLite 的命令历史存储。
type SQLiteHistoryStore struct {
	db *sql.DB
}

// NewSQLiteHistoryStore 创建命令历史存储。
func NewSQLiteHistoryStore(db *sql.DB) *SQLiteHistoryStore {
	return &SQLiteHistoryStore{db: db}
}

// EnsureHistorySchema 确保 command_history 表存在（兼容旧库升级）。
func EnsureHistorySchema(db *sql.DB) error {
	_, err := db.Exec(`
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
`)
	return err
}

// Add 写入一条命令历史；去重最近相同命令；超限滚动删除。
func (s *SQLiteHistoryStore) Add(serverID, command string) error {
	serverID = strings.TrimSpace(serverID)
	command = sanitizeHistoryCommand(command)
	if serverID == "" || command == "" {
		return nil
	}
	// 过滤纯方向键残留（如 [A [B）或过短无意义串
	if looksLikeKeyNoise(command) {
		return nil
	}
	if looksLikeOutputPollution(command) {
		return nil
	}
	if utf8.RuneCountInString(command) > historyMaxCmdLen {
		runes := []rune(command)
		command = string(runes[:historyMaxCmdLen])
	}
	// 启发式：过滤可能含明文密码的命令
	if looksLikeSecretCommand(command) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 去重：若最近一条相同则只更新时间戳
	var lastID int64
	var lastCmd string
	err = tx.QueryRow(`
		SELECT id, command FROM command_history
		WHERE server_id = ? ORDER BY executed_at DESC, id DESC LIMIT 1`,
		serverID).Scan(&lastID, &lastCmd)
	now := time.Now().UnixMilli()
	if err == nil && lastCmd == command {
		if _, err := tx.Exec(`UPDATE command_history SET executed_at = ? WHERE id = ?`, now, lastID); err != nil {
			return fmt.Errorf("更新命令历史失败: %w", err)
		}
		return tx.Commit()
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("查询最近命令失败: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO command_history (server_id, command, executed_at) VALUES (?, ?, ?)`,
		serverID, command, now); err != nil {
		return fmt.Errorf("写入命令历史失败: %w", err)
	}

	// 滚动删除：保留最近 historyMaxPerServer 条
	if _, err := tx.Exec(`
		DELETE FROM command_history WHERE server_id = ? AND id NOT IN (
			SELECT id FROM command_history WHERE server_id = ?
			ORDER BY executed_at DESC, id DESC LIMIT ?
		)`, serverID, serverID, historyMaxPerServer); err != nil {
		return fmt.Errorf("滚动清理命令历史失败: %w", err)
	}

	return tx.Commit()
}

// Query 按前缀/模糊匹配返回高频命令（按频次降序）。
func (s *SQLiteHistoryStore) Query(serverID, prefix string, limit int) ([]models.CommandSuggestion, error) {
	if limit <= 0 {
		limit = 8
	}
	serverID = strings.TrimSpace(serverID)
	prefix = strings.TrimSpace(prefix)
	if serverID == "" {
		return []models.CommandSuggestion{}, nil
	}

	var rows *sql.Rows
	var err error
	if prefix == "" {
		rows, err = s.db.Query(`
			SELECT command, COUNT(*) AS cnt FROM command_history
			WHERE server_id = ?
			GROUP BY command
			ORDER BY cnt DESC, MAX(executed_at) DESC
			LIMIT ?`, serverID, limit)
	} else {
		like := "%" + escapeLike(prefix) + "%"
		rows, err = s.db.Query(`
			SELECT command, COUNT(*) AS cnt FROM command_history
			WHERE server_id = ? AND command LIKE ? ESCAPE '\'
			GROUP BY command
			ORDER BY
				CASE WHEN command LIKE ? ESCAPE '\' THEN 0 ELSE 1 END,
				cnt DESC,
				MAX(executed_at) DESC
			LIMIT ?`, serverID, like, escapeLike(prefix)+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.CommandSuggestion{}
	for rows.Next() {
		var sug models.CommandSuggestion
		if err := rows.Scan(&sug.Command, &sug.Count); err != nil {
			return nil, err
		}
		sug.Source = "history"
		out = append(out, sug)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// looksLikeKeyNoise 识别方向键 CSI 残留（如 [A、OA）或仅括号字母噪声。
func looksLikeKeyNoise(cmd string) bool {
	if cmd == "" {
		return true
	}
	// 整段仅由若干 [A/[B/OA 等组成
	if reKeyNoise.MatchString(cmd) {
		return true
	}
	// 过短且无字母数字：多半是控制残留
	hasAlnum := false
	for _, r := range cmd {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasAlnum = true
			break
		}
	}
	if !hasAlnum && utf8.RuneCountInString(cmd) <= 4 {
		return true
	}
	return false
}

// looksLikeOutputPollution 拒绝把终端输出粘进历史（kubectl 表头、warning、二次 prompt）。
func looksLikeOutputPollution(cmd string) bool {
	if utf8.RuneCountInString(cmd) > 400 {
		return true
	}
	lower := strings.ToLower(cmd)
	if strings.Contains(lower, "warning:") {
		return true
	}
	if strings.Contains(cmd, "NAME") && strings.Contains(cmd, "STATUS") {
		return true
	}
	if reGlueOut.MatchString(cmd) {
		return true
	}
	if reUserHost.MatchString(cmd) &&
		(strings.Contains(cmd, "#") || strings.Contains(cmd, "$")) {
		return true
	}
	return false
}

func looksLikeSecretCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	needles := []string{
		"password=", "passwd ", "mysql -p", "pgpassword",
		"aws_secret", "secret_key", "private_key",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// NoopHistoryStore JSON 兜底时的空实现（写库失败静默降级）。
type NoopHistoryStore struct{}

func (NoopHistoryStore) Add(serverID, command string) error { return nil }
func (NoopHistoryStore) Query(serverID, prefix string, limit int) ([]models.CommandSuggestion, error) {
	return []models.CommandSuggestion{}, nil
}
func (NoopHistoryStore) Clear(serverID string) error { return nil }

var _ HistoryStore = (*SQLiteHistoryStore)(nil)
var _ HistoryStore = NoopHistoryStore{}

// Clear 清理命令历史；serverID 为空时清空全部服务器记录。
func (s *SQLiteHistoryStore) Clear(serverID string) error {
	serverID = strings.TrimSpace(serverID)
	var err error
	if serverID == "" {
		_, err = s.db.Exec(`DELETE FROM command_history`)
	} else {
		_, err = s.db.Exec(`DELETE FROM command_history WHERE server_id = ?`, serverID)
	}
	if err != nil {
		return fmt.Errorf("清理命令历史失败: %w", err)
	}
	return nil
}
