package sshx

import (
	"testing"

	"ding-ssh/internal/models"
)

// 会话级实时刷新覆盖：只影响目标会话，关闭会话后清除，不写全局设置。
func TestSysInfoRealtimeOverridePerSession(t *testing.T) {
	m := NewManager(func(string, interface{}) {})
	m.SetSettingsGetter(func() models.Settings {
		return models.Settings{SysInfoRealtimeEnabled: false}
	})

	if m.sysInfoRealtimeFor("sess-a") {
		t.Fatal("默认应跟随全局设置（false）")
	}
	m.SetSysInfoRealtime("sess-a", true)
	if !m.sysInfoRealtimeFor("sess-a") {
		t.Fatal("会话 a 的覆盖应生效")
	}
	if m.sysInfoRealtimeFor("sess-b") {
		t.Fatal("会话 b 不应受 a 的覆盖影响")
	}

	// 关闭终端后覆盖失效，回到全局默认。
	m.remove("sess-a")
	if m.sysInfoRealtimeFor("sess-a") {
		t.Fatal("会话关闭后覆盖应被清除")
	}

	// 只改全局默认时，没有覆盖的会话跟随新值。
	m.SetSettingsGetter(func() models.Settings {
		return models.Settings{SysInfoRealtimeEnabled: true}
	})
	if !m.sysInfoRealtimeFor("sess-b") {
		t.Fatal("无覆盖的会话应跟随全局默认")
	}
	m.SetSysInfoRealtime("sess-b", false)
	if m.sysInfoRealtimeFor("sess-b") {
		t.Fatal("显式关闭的会话不应被全局 true 覆盖")
	}
}
