package store

import (
	"ding-ssh/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/appdir"
	"ding-ssh/internal/logx"
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
	logx.Debugf("[store] 创建 JSON 设置存储: path=%q", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logx.Errorf("[store] 创建设置存储目录失败: dir=%q err=%v", filepath.Dir(path), err)
		return nil, fmt.Errorf(i18n.T("创建配置目录失败: %w"), err)
	}
	logx.Debugf("[store] JSON 设置存储就绪: path=%q", path)
	return &JSONSettingsStore{path: path}, nil
}

// DefaultSettingsPath 返回默认设置文件路径。
func DefaultSettingsPath() string {
	return filepath.Join(appdir.ConfigDir(), "settings.json")
}

// Get 读取设置，文件不存在时返回默认值（日志默认关闭）。
func (s *JSONSettingsStore) Get() (models.Settings, error) {
	if s == nil {
		return models.Settings{
			UISchema:             2,
			ConfirmCloseTab:      true,
			KeepAliveEnabled:     true,
			AutoReconnect:        true,
			SysInfoCollectEnabled: true,
		}, nil
	}
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		logx.Debugf("[store] 设置文件不存在，返回默认设置: path=%q", s.path)
		return models.Settings{
			UISchema:             2,
			ConfirmCloseTab:      true,
			ConfirmCloseApp:      true,
			WebGLEnabled:         true,
			CompletionEnabled:    false,
			CompletionNavHotkey:  "Alt+ArrowDown",
			CompletionPanelLimit: 8,
			SftpToTerminalSync:   true,
			TerminalToSftpSync:   true,
			UIScale:              100,
			AutoReconnect:        true,
			KeepAliveEnabled:     true,
			// 登录后自动采集系统指标：默认开启
			SysInfoCollectEnabled: true,
			TabBarPlacement:       "side",                         // 会话标签页默认放在左侧导航
			SettingsPlacement:     models.SettingsPlacementBottom, // 设置按钮默认放在侧栏底部
			RightClickAction:      models.RightClickMenu,
			NavSectionOrder:       models.DefaultNavSectionOrder,
			// 保持原有行为：新安装也默认语义着色；旧配置缺字段同样回落 auto。
			TerminalColorMode: models.ColorModeAuto,
			// 自动恢复上次会话：默认关闭，无历史记录
			AutoLoginLastServer: false,
			// 日志：控制台 / 文件 / 调用日志默认全关，级别默认 info，无跟踪会话
			LogLevel:   models.NormalizeLogLevel(""),
			Debug:      models.DefaultDebugSettings(),
			Theme:      models.DefaultTheme(),
			Appearance: models.DefaultAppearance(),
			Fonts:      models.DefaultFonts(),
		}, nil
	}
	if err != nil {
		logx.Errorf("[store] 读取设置文件失败: path=%q err=%v", s.path, err)
		return models.Settings{}, err
	}
	var settings models.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		logx.Errorf("[store] 解析设置文件失败: path=%q bytes=%d err=%v", s.path, len(data), err)
		return models.Settings{}, err
	}
	// 旧配置文件可能缺少新字段：缺省开启 WebGL / 补全
	if !bytesContains(data, []byte(`"webGLEnabled"`)) {
		settings.WebGLEnabled = true
	}
	if !bytesContains(data, []byte(`"completionEnabled"`)) {
		settings.CompletionEnabled = false
	}
	// 能力位：升级前的配置完全没有这些字段，反序列化会得到「全 false」，
	// 缺失字段保持关闭，不自动增加执行授权；
	// 用户显式关掉（字段存在且为 false）必须保持关闭。
	if !models.HasDebugCapabilityFields(data) {
		settings.Debug.CapTerminalInput = models.DefaultDebugSettings().CapTerminalInput
	}
	if settings.CompletionNavHotkey == "" {
		logx.Warnf("[store] 设置项为空，补全导航快捷键回落默认: key=completionNavHotkey value=Alt+ArrowDown")
		settings.CompletionNavHotkey = "Alt+ArrowDown"
	}
	if settings.CompletionPanelLimit <= 0 {
		logx.Warnf("[store] 设置项非法，补全面板条数回落默认: key=completionPanelLimit value=%d", settings.CompletionPanelLimit)
		settings.CompletionPanelLimit = 8
	}
	if !bytesContains(data, []byte(`"sftpToTerminalSync"`)) {
		settings.SftpToTerminalSync = true
	}
	if !bytesContains(data, []byte(`"terminalToSftpSync"`)) {
		settings.TerminalToSftpSync = true
	}
	if settings.UIScale <= 0 {
		logx.Warnf("[store] 设置项非法，界面缩放回落默认: key=uiScale value=%d", settings.UIScale)
		settings.UIScale = 100
	}
	if !bytesContains(data, []byte(`"confirmCloseTab"`)) {
		settings.ConfirmCloseTab = true
	}
	if !bytesContains(data, []byte(`"confirmCloseApp"`)) {
		settings.ConfirmCloseApp = true
	}
	if !bytesContains(data, []byte(`"autoReconnect"`)) {
		settings.AutoReconnect = true
	}
	if !bytesContains(data, []byte(`"keepAliveEnabled"`)) {
		settings.KeepAliveEnabled = true
	}
	// 登录后自动采集系统指标：旧配置没有该字段 → 默认开启
	if !bytesContains(data, []byte(`"sysInfoCollectEnabled"`)) {
		settings.SysInfoCollectEnabled = true
	}
	// 标签页位置：旧配置没有该字段 → 新默认「左侧导航」；非法值同样回落默认
	if settings.TabBarPlacement != "top" && settings.TabBarPlacement != "side" {
		if settings.TabBarPlacement != "" {
			logx.Warnf("[store] 设置项非法，标签页位置回落 side: key=tabBarPlacement value=%q", settings.TabBarPlacement)
		}
		settings.TabBarPlacement = "side"
	}
	// 设置按钮位置：旧配置没有该字段 → 默认「左下方」；非法值同样回落默认
	if raw := settings.SettingsPlacement; raw != "" && models.NormalizeSettingsPlacement(raw) != raw {
		logx.Warnf("[store] 设置项非法，设置按钮位置回落 bottom: key=settingsPlacement value=%q", raw)
	}
	settings.SettingsPlacement = models.NormalizeSettingsPlacement(settings.SettingsPlacement)
	// 右键行为：旧配置没有该字段 → 默认「打开选项栏」；非法值同样回落默认
	if raw := settings.RightClickAction; raw != "" {
		if normalized := models.NormalizeRightClickAction(raw); normalized != raw {
			logx.Warnf("[store] 设置项非法，右键行为回落默认: key=rightClickAction value=%q", raw)
		}
	}
	settings.RightClickAction = models.NormalizeRightClickAction(settings.RightClickAction)
	// 导航区段顺序：旧配置没有该字段 → 默认「导航 → 会话 → 标签页」；非法排列同样回落默认
	if raw := settings.NavSectionOrder; raw != "" {
		if normalized := models.NormalizeNavSectionOrder(raw); normalized != raw {
			logx.Warnf("[store] 设置项非法，导航区段顺序回落默认: key=navSectionOrder value=%q", raw)
		}
	}
	settings.NavSectionOrder = models.NormalizeNavSectionOrder(settings.NavSectionOrder)
	// 语义着色档位：旧配置没有该字段（空值）→ 默认「语义着色」；非法值同样回落默认
	if raw := settings.TerminalColorMode; raw != "" {
		if normalized := models.NormalizeTerminalColorMode(raw); normalized != raw {
			logx.Warnf("[store] 设置项非法，终端着色档位回落 auto: key=terminalColorMode value=%q", raw)
		}
	}
	settings.TerminalColorMode = models.NormalizeTerminalColorMode(settings.TerminalColorMode)
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
	if raw := strings.ToLower(strings.TrimSpace(settings.LogLevel)); raw != "" {
		if normalized := models.NormalizeLogLevel(settings.LogLevel); normalized != raw {
			logx.Warnf("[store] 设置项非法，日志级别回落 info: key=logLevel value=%q", settings.LogLevel)
		}
	}
	settings.LogLevel = models.NormalizeLogLevel(settings.LogLevel)
	if rawTabs := len(settings.LogTraceTabs); rawTabs > 0 {
		settings.LogTraceTabs = models.NormalizeLogTraceTabs(settings.LogTraceTabs)
		if len(settings.LogTraceTabs) != rawTabs {
			logx.Debugf("[store] 日志跟踪会话列表已归一化: before=%d after=%d", rawTabs, len(settings.LogTraceTabs))
		}
	} else {
		settings.LogTraceTabs = models.NormalizeLogTraceTabs(settings.LogTraceTabs)
	}
	// 上次打开的服务器列表：可能被写脏（空串 / 非字符串），统一归一化（去空白项，保持顺序）
	settings.LastOpenServers = models.NormalizeLastOpenServers(settings.LastOpenServers)
	logx.Debugf("[store] 设置读取完成: path=%q bytes=%d logLevel=%s traceTabs=%d lastOpen=%d elapsed=%s",
		s.path, len(data), settings.LogLevel, len(settings.LogTraceTabs), len(settings.LastOpenServers), time.Since(start))
	return settings, nil
}

func bytesContains(haystack, needle []byte) bool {
	return strings.Contains(string(haystack), string(needle))
}

// Save 保存设置（原子写入）。
func (s *JSONSettingsStore) Save(settings models.Settings) error {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	logx.Debugf("[store] 保存设置(JSON): path=%q logEnabled=%v logToFile=%v logLevel=%s",
		s.path, settings.LogEnabled, settings.LogToFile, settings.LogLevel)
	// 归一化后再落盘：保证磁盘上的级别与跟踪列表永远是合法值
	if raw := strings.ToLower(strings.TrimSpace(settings.LogLevel)); raw != "" {
		if normalized := models.NormalizeLogLevel(settings.LogLevel); normalized != raw {
			logx.Warnf("[store] 保存设置时日志级别非法，回落 info: value=%q", settings.LogLevel)
		}
	}
	settings.LogLevel = models.NormalizeLogLevel(settings.LogLevel)
	settings.LogTraceTabs = models.NormalizeLogTraceTabs(settings.LogTraceTabs)
	// 上次打开的服务器列表：去空白项、保持顺序与重复项（重复 = 同一服务器开了多个标签）
	settings.LastOpenServers = models.NormalizeLastOpenServers(settings.LastOpenServers)
	// 调试模式（含能力位与 SFTP 写白名单）同样归一化：端口越界回落、白名单去空去重
	settings.Debug = models.NormalizeDebugSettings(settings.Debug)
	// 语义着色档位：落盘永远写合法值（空 / 非法值回落 auto）
	if raw := settings.TerminalColorMode; raw != "" {
		if normalized := models.NormalizeTerminalColorMode(raw); normalized != raw {
			logx.Warnf("[store] 保存设置时终端着色档位非法，回落 auto: value=%q", raw)
		}
	}
	settings.TerminalColorMode = models.NormalizeTerminalColorMode(settings.TerminalColorMode)
	if settings.UISchema < 2 {
		logx.Debugf("[store] 设置 UISchema 过低，升级为 2: before=%d", settings.UISchema)
		settings.UISchema = 2
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		logx.Errorf("[store] 序列化设置失败: path=%q err=%v", s.path, err)
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logx.Errorf("[store] 写入设置临时文件失败: path=%q bytes=%d err=%v", tmp, len(data), err)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		logx.Errorf("[store] 原子替换设置文件失败: path=%q err=%v", s.path, err)
		return err
	}
	logx.Infof("[store] 设置已保存(JSON): path=%q bytes=%d elapsed=%s", s.path, len(data), time.Since(start))
	return nil
}
