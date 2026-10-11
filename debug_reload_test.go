package main

// M9：调试服务「就地生效」的回归测试。
//
// 缺陷：保存设置会触发 reloadDebug，而它原先在**任何**调试设置字段变化时都
// stop + start 调试服务 —— 能力位（capability）变化也不例外。调试服务重启后
// Port=0（自动端口）会换成新的随机端口，正在连接的 MCP / HTTP 客户端直接掉线。
//
// 修法：只有影响监听地址的三个字段（Enabled / Port / BindLAN）变化时才重启；
// 其余（能力位、allowEval、allowSecrets、CDP、日志相关）只更新内存兜底快照。
//
// 这里用**真实的** debugsrv.Server（随机端口）+ 临时目录里的设置存储来断言：
//   - 能力位切换（走 SetCapability 路径 → SaveSettings → reloadDebug）后，
//     底层 server 实例、监听地址与 token 都不变；
//   - 监听字段变化（BindLAN）时确实会重建 server 实例。

import (
	"path/filepath"
	"testing"
	"time"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"ding-ssh/internal/store"
)

// newReloadTestApp 组装一个可用于 reloadDebug 测试的最小 App：
// 临时目录里的设置存储 + 真实调试服务（Port=0，系统随机端口）。
//
// 为什么用 Port=0：这样「重启」几乎必然会换地址，测试才能真正区分
// 「就地生效」与「重启服务」两种行为。
func newReloadTestApp(t *testing.T) *App {
	t.Helper()
	js, err := store.NewJSONSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("创建设置存储失败: %v", err)
	}
	cur, err := js.Get()
	if err != nil {
		t.Fatalf("读取默认设置失败: %v", err)
	}
	d := models.DefaultDebugSettings()
	d.Enabled = true
	d.Port = 0 // 自动端口
	cur.Debug = d
	if err := js.Save(cur); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}

	a := NewApp(DebugBoot{Enabled: true, Port: 0, Token: "test-token-m9", CapTerminalInput: true})
	a.settings = js
	// SaveSettings 会调用 manager.SetKeepAliveEnabled，不能是 nil。
	a.manager = sshx.NewManager(func(string, any) {})
	a.audits = debugsrv.NewAuditLog()
	a.confirms = debugsrv.NewConfirmManager(debugsrv.DefaultConfirmTTL)
	a.startDebug()
	if a.debug == nil || a.debug.srv == nil {
		t.Fatal("测试前置条件不满足：调试服务未启动")
	}
	// 测试里没有真实 WebView：把前端桥回包超时压到最短，
	// 让 ui.revision 观察器尽快自行退出（它连续失败 5 次后会关闭）。
	a.debug.bridge.SetTimeout(200 * time.Millisecond)
	t.Cleanup(a.stopDebug)
	return a
}

// debugSnapshot 读取调试服务的可观测状态（不复制含锁的结构体，避免 vet 报 copylocks）。
type debugSnapshot struct {
	srv  *debugsrv.Server
	addr string
	port int
	tok  string
}

func snapshotDebug(c *debugController) debugSnapshot {
	c.mu.Lock()
	addr, port := c.addr, c.port
	c.mu.Unlock()
	return debugSnapshot{srv: c.srv, addr: addr, port: port, tok: c.boot.Token}
}

// 能力位切换（SetCapability / SaveSettings 两条路径都汇入 reloadDebug）：
// 监听地址、token、底层 server 实例都不应变。
func TestReloadDebugCapabilityChangeKeepsListener(t *testing.T) {
	a := newReloadTestApp(t)
	before := snapshotDebug(a.debug)

	// 1) 前端开关路径：SetCapability → applyCapability → SaveSettings → reloadDebug
	if err := a.SetCapability(string(debugsrv.CapConfigWrite), true); err != nil {
		t.Fatalf("开启 config.write 失败: %v", err)
	}
	// 2) 设置保存路径：SaveSettings → reloadDebug（同时改「允许读取敏感数据」）
	cur, err := a.settings.Get()
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	cur.Debug.AllowSecrets = true
	cur.Debug.SFTPWriteAllowlist = []string{"/tmp/m9"}
	if err := a.SaveSettings(cur); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}

	after := snapshotDebug(a.debug)
	if after.srv != before.srv {
		t.Fatalf("能力位切换不应重建调试服务：server 实例被替换（%p → %p）", before.srv, after.srv)
	}
	if after.addr != before.addr || after.port != before.port {
		t.Fatalf("能力位切换不应改变监听地址：%q:%d → %q:%d", before.addr, before.port, after.addr, after.port)
	}
	if after.tok != before.tok {
		t.Fatalf("能力位切换不应更换 token：%q → %q", before.tok, after.tok)
	}
	// 兜底快照必须跟上（存储短暂不可用时会回退到它），否则能力位会「看起来又关回去了」。
	if !a.debugBoot.CapConfigWrite || !a.debugBoot.AllowSecrets {
		t.Fatalf("reloadDebug 未同步兜底快照：capConfigWrite=%v allowSecrets=%v",
			a.debugBoot.CapConfigWrite, a.debugBoot.AllowSecrets)
	}
	// 真实的权限判定读设置（debugGate），必须是开启状态。
	if d := a.currentDebugSettings(); !d.CapConfigWrite || !d.AllowSecrets {
		t.Fatalf("设置里的能力位未生效：%+v", d)
	}
}

// 影响监听地址的字段变化（BindLAN）必须重建调试服务，且沿用同一个 token
// （token 只在「首次开启调试模式」时生成）。
func TestReloadDebugListenerChangeRestarts(t *testing.T) {
	a := newReloadTestApp(t)
	before := snapshotDebug(a.debug)

	next := a.currentDebugSettings()
	next.BindLAN = true
	a.reloadDebug(next)

	after := snapshotDebug(a.debug)
	if after.srv == before.srv {
		t.Fatalf("BindLAN 变化应重建调试服务（server 实例未替换）")
	}
	if after.addr == before.addr {
		t.Fatalf("BindLAN 变化应改变监听地址：%q", after.addr)
	}
	if after.tok != before.tok {
		t.Fatalf("重启调试服务不应更换 token：%q → %q", before.tok, after.tok)
	}
	// 重启后新服务必须处于运行状态（不是「停掉了」）
	if a.debug == nil || a.debug.srv == nil {
		t.Fatal("重启后调试服务未运行")
	}
}
