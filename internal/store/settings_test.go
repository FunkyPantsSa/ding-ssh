package store

import (
	"os"
	"path/filepath"
	"testing"
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
