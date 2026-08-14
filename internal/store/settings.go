package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ding-ssh/internal/models"
)

// SettingsStore 应用设置存储接口。
type SettingsStore interface {
	Get() (models.Settings, error)
	Save(settings models.Settings) error
}

// JSONSettingsStore 基于 JSON 文件的设置存储，与服务器列表分离存储。
type JSONSettingsStore struct {
	mu   sync.Mutex
	path string
}

// NewJSONSettingsStore 创建设置存储，并确保配置目录存在。
func NewJSONSettingsStore(path string) (*JSONSettingsStore, error) {
	if path == "" {
		path = DefaultSettingsPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	return &JSONSettingsStore{path: path}, nil
}

// DefaultSettingsPath 返回默认设置文件路径。
func DefaultSettingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "settings.json"
	}
	return filepath.Join(dir, "ding-ssh", "settings.json")
}

// Get 读取设置，文件不存在时返回默认值（日志默认关闭）。
func (s *JSONSettingsStore) Get() (models.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return models.Settings{
			WebGLEnabled:         true,
			CompletionEnabled:    true,
			CompletionNavHotkey:  "Alt+ArrowDown",
			CompletionPanelLimit: 8,
			SftpToTerminalSync:   true,
			TerminalToSftpSync:   true,
			UIScale:              100,
		}, nil
	}
	if err != nil {
		return models.Settings{}, err
	}
	var settings models.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return models.Settings{}, err
	}
	// 旧配置文件可能缺少新字段：缺省开启 WebGL / 补全
	if !bytesContains(data, []byte(`"webGLEnabled"`)) {
		settings.WebGLEnabled = true
	}
	if !bytesContains(data, []byte(`"completionEnabled"`)) {
		settings.CompletionEnabled = true
	}
	if settings.CompletionNavHotkey == "" {
		settings.CompletionNavHotkey = "Alt+ArrowDown"
	}
	if settings.CompletionPanelLimit <= 0 {
		settings.CompletionPanelLimit = 8
	}
	if !bytesContains(data, []byte(`"sftpToTerminalSync"`)) {
		settings.SftpToTerminalSync = true
	}
	if !bytesContains(data, []byte(`"terminalToSftpSync"`)) {
		settings.TerminalToSftpSync = true
	}
	if settings.UIScale <= 0 {
		settings.UIScale = 100
	}
	return settings, nil
}

func bytesContains(haystack, needle []byte) bool {
	return strings.Contains(string(haystack), string(needle))
}

// Save 保存设置（原子写入）。
func (s *JSONSettingsStore) Save(settings models.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
