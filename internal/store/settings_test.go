package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ding-ssh/internal/models"
)

// 旧配置文件（无 tabBarPlacement 字段）应回落到默认值「左侧导航」。
func TestJSONSettingsStoreTabBarPlacementDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"logEnabled":false,"uiScale":100,"localShell":"zsh"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.TabBarPlacement != "side" {
		t.Fatalf("旧配置缺省标签页位置 = %q，期望 side", got.TabBarPlacement)
	}
}

// 保存后应持久化「左侧导航」并在重新读取时保持不变。
func TestJSONSettingsStoreTabBarPlacementRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	settings.TabBarPlacement = "side"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.TabBarPlacement != "side" {
		t.Fatalf("标签页位置 = %q，期望 side", reloaded.TabBarPlacement)
	}
	// 未识别的取值应回落默认，避免脏数据导致标签栏消失
	settings.TabBarPlacement = "bottom"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.TabBarPlacement != "side" {
		t.Fatalf("非法标签页位置 = %q，期望回落 side", fallback.TabBarPlacement)
	}
}

// 旧配置文件（无 rightClickAction 字段）应回落到默认值「打开选项栏」。
func TestJSONSettingsStoreRightClickActionDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"logEnabled":false,"uiScale":100,"localShell":"zsh"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.RightClickAction != models.RightClickMenu {
		t.Fatalf("旧配置缺省右键行为 = %q，期望 %q", got.RightClickAction, models.RightClickMenu)
	}
}

// 保存「直接粘贴」后应持久化；非法取值回落「打开选项栏」。
func TestJSONSettingsStoreRightClickActionRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	settings.RightClickAction = models.RightClickPaste
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.RightClickAction != models.RightClickPaste {
		t.Fatalf("右键行为 = %q，期望 %q", reloaded.RightClickAction, models.RightClickPaste)
	}

	settings.RightClickAction = "unknown"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.RightClickAction != models.RightClickMenu {
		t.Fatalf("非法右键行为 = %q，期望回落 %q", fallback.RightClickAction, models.RightClickMenu)
	}
}

// 旧配置文件（无 navSectionOrder 字段）应回落到默认「导航 → 会话 → 标签页」。
func TestJSONSettingsStoreNavSectionOrderDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"logEnabled":false,"uiScale":100}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.NavSectionOrder != models.DefaultNavSectionOrder {
		t.Fatalf("旧配置缺省导航区段顺序 = %q，期望 %q", got.NavSectionOrder, models.DefaultNavSectionOrder)
	}
}

// 自定义排列应持久化；非法排列（重复 / 缺项 / 未知键）回落默认。
func TestJSONSettingsStoreNavSectionOrderRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	settings.NavSectionOrder = "tabs,sessions,nav"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.NavSectionOrder != "tabs,sessions,nav" {
		t.Fatalf("导航区段顺序 = %q，期望 %q", reloaded.NavSectionOrder, "tabs,sessions,nav")
	}

	for _, bad := range []string{"nav,nav,tabs", "nav,sessions", "nav,foo,tabs", ""} {
		settings.NavSectionOrder = bad
		if err := s.Save(settings); err != nil {
			t.Fatalf("保存设置失败: %v", err)
		}
		fallback, err := s.Get()
		if err != nil {
			t.Fatalf("重新读取设置失败: %v", err)
		}
		if fallback.NavSectionOrder != models.DefaultNavSectionOrder {
			t.Fatalf("非法导航区段顺序 %q = %q，期望回落 %q", bad, fallback.NavSectionOrder, models.DefaultNavSectionOrder)
		}
	}
}

// SQLite 设置存储是主存储：默认「导航 → 会话 → 标签页」，保存后必须真正落库。
func TestSQLiteSettingsStoreNavSectionOrder(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "ding-ssh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	s := NewSQLiteSettingsStore(db)
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if settings.NavSectionOrder != models.DefaultNavSectionOrder {
		t.Fatalf("默认导航区段顺序 = %q，期望 %q", settings.NavSectionOrder, models.DefaultNavSectionOrder)
	}

	settings.NavSectionOrder = "sessions,tabs,nav"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.NavSectionOrder != "sessions,tabs,nav" {
		t.Fatalf("落库后的导航区段顺序 = %q，期望 %q", reloaded.NavSectionOrder, "sessions,tabs,nav")
	}

	settings.NavSectionOrder = "nav,tabs"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.NavSectionOrder != models.DefaultNavSectionOrder {
		t.Fatalf("非法导航区段顺序 = %q，期望回落 %q", fallback.NavSectionOrder, models.DefaultNavSectionOrder)
	}
}
func TestJSONSettingsStoreRightClickActionKeepsExplicitValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"logEnabled":false,"uiScale":100,"rightClickAction":"paste"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.RightClickAction != models.RightClickPaste {
		t.Fatalf("显式右键行为 = %q，期望 %q", got.RightClickAction, models.RightClickPaste)
	}
}

// SQLite 设置存储是主存储：默认「打开选项栏」，保存后必须真正落库，非法值回落默认。
func TestSQLiteSettingsStoreRightClickAction(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "ding-ssh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	s := NewSQLiteSettingsStore(db)
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if settings.RightClickAction != models.RightClickMenu {
		t.Fatalf("默认右键行为 = %q，期望 %q", settings.RightClickAction, models.RightClickMenu)
	}

	settings.RightClickAction = models.RightClickPaste
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.RightClickAction != models.RightClickPaste {
		t.Fatalf("落库后的右键行为 = %q，期望 %q", reloaded.RightClickAction, models.RightClickPaste)
	}

	settings.RightClickAction = "unknown"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.RightClickAction != models.RightClickMenu {
		t.Fatalf("非法右键行为 = %q，期望回落 %q", fallback.RightClickAction, models.RightClickMenu)
	}
}

// 显式选过「顶栏」的用户不应被新默认值覆盖。
func TestJSONSettingsStoreTabBarPlacementKeepsExplicitTop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"logEnabled":false,"uiScale":100,"tabBarPlacement":"top"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}

	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.TabBarPlacement != "top" {
		t.Fatalf("显式顶栏配置 = %q，期望 top", got.TabBarPlacement)
	}
}

// SQLite 设置存储是主存储：默认「左侧导航」，且保存后必须真正落库。
func TestSQLiteSettingsStoreTabBarPlacement(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "ding-ssh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	s := NewSQLiteSettingsStore(db)
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if settings.TabBarPlacement != "side" {
		t.Fatalf("默认标签页位置 = %q，期望 side", settings.TabBarPlacement)
	}

	settings.TabBarPlacement = "top"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.TabBarPlacement != "top" {
		t.Fatalf("落库后的标签页位置 = %q，期望 top", reloaded.TabBarPlacement)
	}

	// 非法值落回默认，避免脏数据导致标签栏消失
	settings.TabBarPlacement = "bottom"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.TabBarPlacement != "side" {
		t.Fatalf("非法标签页位置 = %q，期望回落 side", fallback.TabBarPlacement)
	}
}

// ---- 日志设置 ----

// 日志默认值：控制台 / 文件 / 调用日志全关，级别 info，无跟踪会话。
func TestJSONSettingsStoreLogDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.LogToFile || got.LogAPICalls {
		t.Fatalf("日志默认值 = 文件 %v / 调用 %v，期望都为 false", got.LogToFile, got.LogAPICalls)
	}
	if got.LogLevel != models.LogLevelInfo {
		t.Fatalf("默认日志级别 = %q，期望 %q", got.LogLevel, models.LogLevelInfo)
	}
	if len(got.LogTraceTabs) != 0 {
		t.Fatalf("默认跟踪会话 = %v，期望为空", got.LogTraceTabs)
	}

	// 旧配置（完全没有日志字段）同样应回落到上述默认值
	legacy := `{"logEnabled":true,"uiScale":100}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}
	old, err := s.Get()
	if err != nil {
		t.Fatalf("读取旧配置失败: %v", err)
	}
	if !old.LogEnabled || old.LogToFile || old.LogAPICalls {
		t.Fatalf("旧配置日志开关 = %v/%v/%v，期望 true/false/false", old.LogEnabled, old.LogToFile, old.LogAPICalls)
	}
	if old.LogLevel != models.LogLevelInfo {
		t.Fatalf("旧配置日志级别 = %q，期望 %q", old.LogLevel, models.LogLevelInfo)
	}
}

// 非法日志级别在保存与读取两处都回落 info；合法值原样保留。
func TestJSONSettingsStoreLogLevelFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	settings.LogLevel = "verbose"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	// 落盘内容本身就该是归一化后的值
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	if !strings.Contains(string(raw), `"logLevel": "info"`) {
		t.Fatalf("非法级别落盘内容 = %s，期望写入 info", string(raw))
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.LogLevel != models.LogLevelInfo {
		t.Fatalf("非法日志级别 = %q，期望回落 %q", fallback.LogLevel, models.LogLevelInfo)
	}

	// 合法值（含大小写与空白）应保留为小写标准形式
	settings.LogLevel = " WARN "
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	kept, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if kept.LogLevel != models.LogLevelWarn {
		t.Fatalf("日志级别 = %q，期望 %q", kept.LogLevel, models.LogLevelWarn)
	}
}

// 跟踪会话列表往返：去空白项、去重，清空后回落空。
func TestJSONSettingsStoreLogTraceTabsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	settings.LogTraceTabs = []string{"tab-1", " tab-2 ", "tab-1", ""}
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if len(reloaded.LogTraceTabs) != 2 || reloaded.LogTraceTabs[0] != "tab-1" || reloaded.LogTraceTabs[1] != "tab-2" {
		t.Fatalf("跟踪会话列表 = %v，期望 [tab-1 tab-2]", reloaded.LogTraceTabs)
	}

	settings.LogTraceTabs = nil
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	cleared, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if len(cleared.LogTraceTabs) != 0 {
		t.Fatalf("清空后的跟踪会话列表 = %v，期望为空", cleared.LogTraceTabs)
	}
}

// SQLite 设置存储是主存储：日志字段必须真正落库（四个键各一列值）。
func TestSQLiteSettingsStoreLogSettings(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "ding-ssh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	s := NewSQLiteSettingsStore(db)
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if settings.LogLevel != models.LogLevelInfo || settings.LogToFile || settings.LogAPICalls {
		t.Fatalf("SQLite 日志默认值 = %q/%v/%v，期望 info/false/false",
			settings.LogLevel, settings.LogToFile, settings.LogAPICalls)
	}

	settings.LogToFile = true
	settings.LogAPICalls = true
	settings.LogLevel = "debug"
	settings.LogTraceTabs = []string{"tab-9"}
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if !reloaded.LogToFile || !reloaded.LogAPICalls {
		t.Fatalf("落库后的文件/调用日志开关 = %v/%v，期望 true/true", reloaded.LogToFile, reloaded.LogAPICalls)
	}
	if reloaded.LogLevel != models.LogLevelDebug {
		t.Fatalf("落库后的日志级别 = %q，期望 debug", reloaded.LogLevel)
	}
	if len(reloaded.LogTraceTabs) != 1 || reloaded.LogTraceTabs[0] != "tab-9" {
		t.Fatalf("落库后的跟踪会话列表 = %v，期望 [tab-9]", reloaded.LogTraceTabs)
	}

	// 直接查库确认键名与取值（防止只改了内存结构没写库）
	for key, want := range map[string]string{
		"logToFile":    "true",
		"logApiCalls":  "true",
		"logLevel":     "debug",
		"logTraceTabs": `["tab-9"]`,
	} {
		var got string
		if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&got); err != nil {
			t.Fatalf("读取设置键 %s 失败: %v", key, err)
		}
		if got != want {
			t.Fatalf("设置键 %s = %q，期望 %q", key, got, want)
		}
	}

	// 非法级别落库时回落 info
	settings.LogLevel = "trace"
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	fallback, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if fallback.LogLevel != models.LogLevelInfo {
		t.Fatalf("非法日志级别落库后 = %q，期望 %q", fallback.LogLevel, models.LogLevelInfo)
	}
}

// ---- 调试模式能力位（第四波：MCP 权限内核）----

// 全新存储（无任何配置文件）的能力位默认值：只有「终端输入」开启，其余危险能力全关。
func TestJSONSettingsStoreDebugCapabilityDefaults(t *testing.T) {
	s, err := NewJSONSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if !got.Debug.CapTerminalInput {
		t.Fatalf("默认值：终端输入应为开启")
	}
	for name, on := range map[string]bool{
		"ui.write":        got.Debug.CapUIWrite,
		"config.write":    got.Debug.CapConfigWrite,
		"secrets.write":   got.Debug.CapSecretWrite,
		"fs.remote.write": got.Debug.CapRemoteFSWrite,
		"sudo.credential": got.Debug.CapSudoCredential,
		"lifecycle":       got.Debug.CapLifecycle,
	} {
		if on {
			t.Fatalf("默认值：%s 应为关闭，实际开启", name)
		}
	}
	if len(got.Debug.SFTPWriteAllowlist) != 0 {
		t.Fatalf("默认 SFTP 写白名单 = %v，期望为空", got.Debug.SFTPWriteAllowlist)
	}
}

// 旧配置文件（完全没有能力位字段）升级后应补上默认值「终端输入开启」。
func TestJSONSettingsStoreDebugCapabilityLegacyDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// 旧版本写出的 debug 段：只有总开关/端口等老字段，没有任何 cap* 字段
	legacy := `{"logEnabled":false,"debug":{"enabled":true,"port":8765,"bindLan":false,"allowEval":false,"allowSecrets":false,"cdpEnabled":false}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if !got.Debug.CapTerminalInput {
		t.Fatalf("旧配置升级后：终端输入应补默认值 true，实际 false")
	}
	if !got.Debug.Enabled || got.Debug.Port != 8765 {
		t.Fatalf("旧配置的既有字段被破坏：enabled=%v port=%d", got.Debug.Enabled, got.Debug.Port)
	}
}

// 能力位与 SFTP 白名单往返：显式关闭必须保持关闭，白名单过滤空串 / 去重 / 去尾斜杠。
func TestJSONSettingsStoreDebugCapabilityRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	settings.Debug.CapTerminalInput = false // 用户显式关闭
	settings.Debug.CapConfigWrite = true
	settings.Debug.SFTPWriteAllowlist = []string{"/srv/app", " ", "/srv/app/", "/tmp", ""}
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if reloaded.Debug.CapTerminalInput {
		t.Fatalf("显式关闭的终端输入在往返后变成了开启")
	}
	if !reloaded.Debug.CapConfigWrite {
		t.Fatalf("config.write 往返后丢失")
	}
	want := []string{"/srv/app", "/tmp"}
	if len(reloaded.Debug.SFTPWriteAllowlist) != len(want) {
		t.Fatalf("白名单 = %v，期望 %v", reloaded.Debug.SFTPWriteAllowlist, want)
	}
	for i := range want {
		if reloaded.Debug.SFTPWriteAllowlist[i] != want[i] {
			t.Fatalf("白名单 = %v，期望 %v", reloaded.Debug.SFTPWriteAllowlist, want)
		}
	}
}

// SQLite 是主存储：能力位与白名单必须真正落库（一个 debug 键，值为 JSON）。
func TestSQLiteSettingsStoreDebugCapabilities(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "ding-ssh.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	s := NewSQLiteSettingsStore(db)
	settings, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if !settings.Debug.CapTerminalInput {
		t.Fatalf("SQLite 默认能力位：终端输入应为开启")
	}
	if settings.Debug.CapConfigWrite || settings.Debug.CapRemoteFSWrite || settings.Debug.CapLifecycle {
		t.Fatalf("SQLite 默认能力位：危险能力应全关，实际 %+v", settings.Debug)
	}

	settings.Debug.CapRemoteFSWrite = true
	settings.Debug.CapLifecycle = true
	settings.Debug.SFTPWriteAllowlist = []string{"/home/deploy", "/home/deploy"}
	settings.Debug.CapSecretWrite = true // 但 AllowSecrets=false → 归一化时必须回落 false
	settings.Debug.CapSudoCredential = true // 同上：不允许读取敏感数据时不可能用保存的密码提权
	if err := s.Save(settings); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if !reloaded.Debug.CapRemoteFSWrite || !reloaded.Debug.CapLifecycle {
		t.Fatalf("落库后能力位丢失：%+v", reloaded.Debug)
	}
	if reloaded.Debug.CapSecretWrite {
		t.Fatalf("未开启「允许读取敏感数据」时 secrets.write 应为 false")
	}
	if reloaded.Debug.CapSudoCredential {
		t.Fatalf("未开启「允许读取敏感数据」时 sudo.credential 应为 false")
	}
	if len(reloaded.Debug.SFTPWriteAllowlist) != 1 || reloaded.Debug.SFTPWriteAllowlist[0] != "/home/deploy" {
		t.Fatalf("落库后的白名单 = %v，期望 [/home/deploy]", reloaded.Debug.SFTPWriteAllowlist)
	}

	// 直接查库确认 debug 键里确实带了新字段（防止只改了内存结构没写库）
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, "debug").Scan(&raw); err != nil {
		t.Fatalf("读取 debug 设置键失败: %v", err)
	}
	for _, frag := range []string{
		`"capRemoteFsWrite":true`, `"capLifecycle":true`, `"capSudoCredential":false`,
		`"sftpWriteAllowlist":["/home/deploy"]`,
	} {
		if !strings.Contains(raw, frag) {
			t.Fatalf("库里的 debug 值缺少 %s：%s", frag, raw)
		}
	}
}

// sudo.credential（M10）：默认关、由 AllowSecrets 兜底、JSON 往返不丢。
func TestDebugSettingsSudoCredentialNormalization(t *testing.T) {
	if models.DefaultDebugSettings().CapSudoCredential {
		t.Fatalf("sudo.credential 默认必须是关闭")
	}
	// 未开启「允许读取敏感数据」→ 归一化回落 false（与 capSecretWrite 同款）
	off := models.NormalizeDebugSettings(models.DebugSettings{CapSudoCredential: true})
	if off.CapSudoCredential {
		t.Fatalf("AllowSecrets=false 时 sudo.credential 必须回落 false")
	}
	on := models.NormalizeDebugSettings(models.DebugSettings{AllowSecrets: true, CapSudoCredential: true})
	if !on.CapSudoCredential {
		t.Fatalf("AllowSecrets=true 时 sudo.credential 应保持 true")
	}
	// 旧配置（完全没有 capSudoCredential 字段）升级后必须保持关闭
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := `{"logEnabled":false,"debug":{"enabled":true,"port":8765,"allowEval":false,"allowSecrets":true,"capTerminalInput":true}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}
	s, err := NewJSONSettingsStore(path)
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	if got.Debug.CapSudoCredential {
		t.Fatalf("旧配置里没有该字段 → 升级后必须保持关闭")
	}
	// 显式打开 → 落盘 → 往返仍在
	got.Debug.CapSudoCredential = true
	if err := s.Save(got); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	if !strings.Contains(string(raw), `"capSudoCredential": true`) {
		t.Fatalf("配置文件里应写入 capSudoCredential：%s", string(raw))
	}
	reloaded, err := s.Get()
	if err != nil {
		t.Fatalf("重新读取设置失败: %v", err)
	}
	if !reloaded.Debug.CapSudoCredential {
		t.Fatalf("capSudoCredential 往返后丢失：%+v", reloaded.Debug)
	}
}
