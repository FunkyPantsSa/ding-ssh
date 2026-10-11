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
			AutoReconnect:        true,
			KeepAliveEnabled:     true,
			TabBarPlacement:      "side", // 会话标签页默认放在左侧导航
			RightClickAction:     models.RightClickMenu,
			NavSectionOrder:      models.DefaultNavSectionOrder,
			// 日志：控制台 / 文件 / 调用日志默认全关，级别默认 info，无跟踪会话
			LogLevel:   models.NormalizeLogLevel(""),
			Debug:      models.DefaultDebugSettings(),
			Theme:      models.DefaultTheme(),
			Appearance: models.DefaultAppearance(),
			Fonts:      models.DefaultFonts(),
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
	// 能力位：升级前的配置完全没有这些字段，反序列化会得到「全 false」，
	// 但默认值要求「终端输入」开启 —— 因此只在缺字段时补默认值，
	// 用户显式关掉（字段存在且为 false）必须保持关闭。
	if !models.HasDebugCapabilityFields(data) {
		settings.Debug.CapTerminalInput = true
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
	if !bytesContains(data, []byte(`"autoReconnect"`)) {
		settings.AutoReconnect = true
	}
	if !bytesContains(data, []byte(`"keepAliveEnabled"`)) {
		settings.KeepAliveEnabled = true
	}
	// 标签页位置：旧配置没有该字段 → 新默认「左侧导航」；非法值同样回落默认
	if settings.TabBarPlacement != "top" && settings.TabBarPlacement != "side" {
		settings.TabBarPlacement = "side"
	}
	// 右键行为：旧配置没有该字段 → 默认「打开选项栏」；非法值同样回落默认
	settings.RightClickAction = models.NormalizeRightClickAction(settings.RightClickAction)
	// 导航区段顺序：旧配置没有该字段 → 默认「导航 → 会话 → 标签页」；非法排列同样回落默认
	settings.NavSectionOrder = models.NormalizeNavSectionOrder(settings.NavSectionOrder)
	// 调试模式：端口越界 / 旧配置缺字段时回落默认
	settings.Debug = models.NormalizeDebugSettings(settings.Debug)
	// 旧配置文件缺少外观 / 字体 / ANSI 色字段：补默认值
	if !bytesContains(data, []byte(`"appearance"`)) {
		settings.Appearance = models.DefaultAppearance()
	}
	if !bytesContains(data, []byte(`"fonts"`)) {
		settings.Fonts = models.DefaultFonts()
	}
	if !bytesContains(data, []byte(`"black"`)) {
		models.FillThemeAnsi(&settings.Theme)
	}
	// 日志：旧配置没有日志字段时，零值（false）恰好就是期望的默认值；
	// 级别与跟踪列表可能被写脏，统一归一化（非法级别回落 info）。
	settings.LogLevel = models.NormalizeLogLevel(settings.LogLevel)
	settings.LogTraceTabs = models.NormalizeLogTraceTabs(settings.LogTraceTabs)
	return settings, nil
}

func bytesContains(haystack, needle []byte) bool {
	return strings.Contains(string(haystack), string(needle))
}

// Save 保存设置（原子写入）。
func (s *JSONSettingsStore) Save(settings models.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 归一化后再落盘：保证磁盘上的级别与跟踪列表永远是合法值
	settings.LogLevel = models.NormalizeLogLevel(settings.LogLevel)
	settings.LogTraceTabs = models.NormalizeLogTraceTabs(settings.LogTraceTabs)
	// 调试模式（含能力位与 SFTP 写白名单）同样归一化：端口越界回落、白名单去空去重
	settings.Debug = models.NormalizeDebugSettings(settings.Debug)
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
