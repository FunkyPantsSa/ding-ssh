package sshx

import (
	"testing"

	"ding-ssh/internal/models"
)

// TestUpdateTunnelAppliesConfigAndKeepsID 确认修改已停止的隧道时配置整体生效，
// 且沿用原 ID，使列表中的条目不会被替换成新的一条。
func TestUpdateTunnelAppliesConfigAndKeepsID(t *testing.T) {
	m := NewManager(func(string, interface{}) {})
	node := models.ServerNode{ID: "srv-1", Name: "生产", Host: "10.0.0.1", Port: 22}
	m.tunnels["tunnel-1"] = newTunnel("tunnel-1", "旧名称", TunnelLocal, node, 13306, "127.0.0.1", 3306, m.tunnelStatusNotifier())

	info, err := m.UpdateTunnel("tunnel-1", node, "新名称", string(TunnelLocal), 15432, "10.0.0.8", 5432)
	if err != nil {
		t.Fatalf("UpdateTunnel: %v", err)
	}
	if info.ID != "tunnel-1" {
		t.Fatalf("ID = %q, want tunnel-1", info.ID)
	}
	if info.Name != "新名称" || info.LocalPort != 15432 || info.RemoteHost != "10.0.0.8" || info.RemotePort != 5432 {
		t.Fatalf("配置未生效: %+v", info)
	}
	if info.Status != string(TunnelStopped) {
		t.Fatalf("Status = %q, want stopped", info.Status)
	}
	if len(m.ListTunnels()) != 1 {
		t.Fatalf("隧道数量 = %d, want 1", len(m.ListTunnels()))
	}
}

func TestUpdateTunnelRejectsInvalidInput(t *testing.T) {
	m := NewManager(func(string, interface{}) {})
	node := models.ServerNode{ID: "srv-1", Name: "生产"}
	m.tunnels["tunnel-1"] = newTunnel("tunnel-1", "隧道", TunnelLocal, node, 13306, "127.0.0.1", 3306, m.tunnelStatusNotifier())

	if _, err := m.UpdateTunnel("tunnel-404", node, "隧道", string(TunnelLocal), 13306, "127.0.0.1", 3306); err == nil {
		t.Fatal("修改不存在的隧道应返回错误")
	}
	if _, err := m.UpdateTunnel("tunnel-1", node, "隧道", string(TunnelLocal), 0, "127.0.0.1", 3306); err == nil {
		t.Fatal("本地端口无效时应返回错误")
	}
	// 校验失败不应影响原有隧道。
	if got := m.tunnels["tunnel-1"].Info().LocalPort; got != 13306 {
		t.Fatalf("LocalPort = %d, want 13306", got)
	}
}

// TestNormalizeTunnelConfigDefaults 动态模式不需要远端地址，其余模式缺省回落到 127.0.0.1。
func TestNormalizeTunnelConfigDefaults(t *testing.T) {
	node := models.ServerNode{Name: "srv"}

	mode, host, name, err := normalizeTunnelConfig(node, "", string(TunnelDynamic), 1080, "", 0)
	if err != nil {
		t.Fatalf("dynamic: %v", err)
	}
	if mode != TunnelDynamic || host != "" || name != "SOCKS5:1080" {
		t.Fatalf("dynamic 结果异常: mode=%s host=%q name=%q", mode, host, name)
	}

	_, host, name, err = normalizeTunnelConfig(node, "", string(TunnelLocal), 13306, "", 3306)
	if err != nil {
		t.Fatalf("local: %v", err)
	}
	if host != "127.0.0.1" || name != "srv:13306" {
		t.Fatalf("local 结果异常: host=%q name=%q", host, name)
	}
}
