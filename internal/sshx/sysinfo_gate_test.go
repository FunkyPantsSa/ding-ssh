package sshx

import (
	"errors"
	"testing"

	"ding-ssh/internal/models"
)

// 测试「登录后自动采集」开关（Settings.SysInfoCollectEnabled）对远端采集的放行 / 拦截。
// 这是 ding-ssh 是否在登录服务器后向远端执行命令的唯一闸门，必须保证：
// 关闭时即使会话不存在也不会 panic，更不会启动采集器。
func TestStartSysInfoCollectorGatedBySetting(t *testing.T) {
	t.Run("未注入设置读取器时默认开启", func(t *testing.T) {
		m := NewManager(func(string, interface{}) {})
		if !m.currentSettings().SysInfoCollectEnabled {
			t.Fatal("未注入 settings getter 时应默认开启自动采集")
		}
	})

	t.Run("关闭时不采集且会话不存在也不报错", func(t *testing.T) {
		m := NewManager(func(string, interface{}) {})
		m.SetSettingsGetter(func() models.Settings {
			return models.Settings{SysInfoCollectEnabled: false}
		})
		// 远端采集被关闭：即使会话不存在也必须直接返回 nil（不得去 get 会话，也不得 panic）。
		if err := m.StartSysInfoCollector("no-such-session"); err != nil {
			t.Fatalf("关闭开关后 StartSysInfoCollector 应返回 nil，实际 %v", err)
		}
	})

	t.Run("开启但会话不存在时返回会话不存在", func(t *testing.T) {
		m := NewManager(func(string, interface{}) {})
		m.SetSettingsGetter(func() models.Settings {
			return models.Settings{SysInfoCollectEnabled: true}
		})
		err := m.StartSysInfoCollector("no-such-session")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("开启开关但会话不存在时应返回 ErrSessionNotFound，实际 %v", err)
		}
	})

	t.Run("StopAllSysInfoCollectors 空表安全", func(t *testing.T) {
		m := NewManager(func(string, interface{}) {})
		m.StopAllSysInfoCollectors() // 不应 panic
	})
}
