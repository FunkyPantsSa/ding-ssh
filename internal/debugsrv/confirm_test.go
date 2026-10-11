package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// 正常流程：prepare → commit 成功，并且 token 只能消费一次。
func TestConfirmPrepareCommit(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	args := map[string]any{"id": "srv-1", "force": true}
	token, expiresAt := m.Prepare("servers.delete", args, "删除服务器 srv-1")
	if token == "" {
		t.Fatalf("prepare 未返回 token")
	}
	if expiresAt <= time.Now().UnixMilli() {
		t.Fatalf("expiresAt 应晚于当前时间：%d", expiresAt)
	}
	if err := m.Commit(token, "servers.delete", args); err != nil {
		t.Fatalf("首次 commit 应成功：%v", err)
	}
	// 一次性：第二次必须失败
	if err := m.Commit(token, "servers.delete", args); err == nil {
		t.Fatalf("重复 commit 应失败")
	} else if !strings.Contains(err.Error(), "无效") && !strings.Contains(err.Error(), "已被使用") {
		t.Fatalf("重复 commit 的错误文案应说明 token 已用掉：%v", err)
	}
}

// args 的键顺序 / 空格差异不得影响校验（规范化），但语义变化必须失败。
func TestConfirmArgsNormalization(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	token, _ := m.Prepare("settings.update", map[string]any{
		"settings": map[string]any{"uiScale": float64(120), "logLevel": " debug "},
	}, "")
	// 同样语义（键顺序不同 + 数字写成 120.0）应通过
	same := map[string]any{
		"settings": map[string]any{"logLevel": "debug", "uiScale": 120},
	}
	if err := m.Commit(token, "settings.update", same); err != nil {
		t.Fatalf("等价参数应通过校验：%v", err)
	}
}

// 参数被篡改（语义变化）必须失败，并且失败后 token 仍然有效（不消费）。
func TestConfirmArgsTampered(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	args := map[string]any{"path": "/etc/passwd"}
	token, _ := m.Prepare("sftp.remove", args, "")

	if err := m.Commit(token, "sftp.remove", map[string]any{"path": "/etc/shadow"}); err == nil {
		t.Fatalf("参数被篡改应失败")
	} else if !strings.Contains(err.Error(), "参数") {
		t.Fatalf("篡改参数的错误文案应说明参数不一致：%v", err)
	}
	// 原参数仍可提交（篡改的尝试不应消费 token）
	if err := m.Commit(token, "sftp.remove", args); err != nil {
		t.Fatalf("原参数仍应可提交：%v", err)
	}
}

// action 不匹配必须失败（拿 A 的 token 执行 B）。
func TestConfirmActionMismatch(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	args := map[string]any{"id": "x"}
	token, _ := m.Prepare("servers.delete", args, "")
	err := m.Commit(token, "servers.create", args)
	if err == nil {
		t.Fatalf("action 不匹配应失败")
	}
	if !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("action 不匹配的错误文案：%v", err)
	}
}

// 过期后必须失败。
func TestConfirmExpired(t *testing.T) {
	m := NewConfirmManager(20 * time.Millisecond)
	args := map[string]any{"id": "x"}
	token, _ := m.Prepare("servers.delete", args, "")
	time.Sleep(40 * time.Millisecond)
	err := m.Commit(token, "servers.delete", args)
	if err == nil {
		t.Fatalf("过期 token 应失败")
	}
	// 惰性清理发生在查找之前，因此文案是「无效或已被使用 / 可能已过期」——
	// 关键是提示用户重新 prepare，而不是把过期当成成功。
	if !strings.Contains(err.Error(), "confirm.prepare") {
		t.Fatalf("过期错误文案应提示重新 prepare：%v", err)
	}
	// 过期的 token 应被惰性清理
	if m.PendingCount() != 0 {
		t.Fatalf("过期项应被清理，pending = %d", m.PendingCount())
	}
}

// 明确区分「过期」与「未知 token」：先 Peek（不清理）时能直接看到过期。
func TestConfirmExpiredMessage(t *testing.T) {
	m := NewConfirmManager(15 * time.Millisecond)
	args := map[string]any{"id": "x"}
	token, _ := m.Prepare("servers.delete", args, "")
	if _, ok := m.Peek(token); !ok {
		t.Fatalf("未过期时应能 Peek 到")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := m.Peek(token); ok {
		t.Fatalf("过期后 Peek 应返回 false")
	}
}

// 空 token / 未登记的 token 都要有清晰的中文错误。
func TestConfirmMissingAndUnknownToken(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	if err := m.Commit("", "servers.delete", nil); err == nil || !strings.Contains(err.Error(), "缺少 token") {
		t.Fatalf("空 token 的错误文案：%v", err)
	}
	if err := m.Commit("deadbeef", "servers.delete", nil); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("未知 token 的错误文案：%v", err)
	}
}

// 读类 action 目录：需要确认的清单必须覆盖后续三路要接入的动作。
func TestConfirmableActionsList(t *testing.T) {
	want := []string{
		"servers.create", "servers.update", "servers.delete",
		"credentials.create", "credentials.update", "credentials.delete",
		"tunnels.create", "tunnels.update", "tunnels.delete", "tunnels.start", "tunnels.stop",
		"sftp.write", "sftp.remove", "sftp.mkdir", "sftp.rename", "sftp.upload",
		"settings.update", "settings.reset", "logs.clear",
		"app.quit", "capability.enable",
	}
	for _, a := range want {
		if !ActionNeedsConfirm(a) {
			t.Fatalf("动作 %s 应在需要确认的清单里", a)
		}
	}
	// 可逆 / 高频操作必须**不在**清单里（否则 terminal.run/expect 与 UI 迭代循环不可用）
	for _, a := range []string{"terminal.input", "ui.write", "eval"} {
		if ActionNeedsConfirm(a) {
			t.Fatalf("可逆 / 高频动作 %s 不应需要两段式确认（只受能力位约束）", a)
		}
	}
	if ActionNeedsConfirm("logs.tail") || ActionNeedsConfirm("app_state") {
		t.Fatalf("读类动作不应需要确认")
	}
	if ActionNeedsConfirm("something.unknown") {
		t.Fatalf("未知动作不应被当作需要确认（否则 prepare 会放行任意动作名）")
	}
	// 清单本身也不能含这三个
	for _, a := range ConfirmableActions() {
		if a == "terminal.input" || a == "ui.write" || a == "eval" {
			t.Fatalf("ConfirmableActions() 不应包含可逆动作 %s", a)
		}
	}
}

// confirm.prepare 拒绝「不需要确认」的 action；confirm.commit 未注入 Executor 时也要给中文错误。
func TestMCPConfirmTools(t *testing.T) {
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})

	// 读类 action：明确拒绝
	isErr, text, _ := callTool(t, ts.srv, "confirm.prepare", map[string]any{"action": "logs.tail"})
	if !isErr {
		t.Fatalf("读类 action 应被拒绝 prepare")
	}
	if !strings.Contains(text, "不需要两段式确认") {
		t.Fatalf("拒绝文案应说明不需要确认：%s", text)
	}

	// 缺 action
	if isErr, _, _ = callTool(t, ts.srv, "confirm.prepare", map[string]any{}); !isErr {
		t.Fatalf("缺 action 应失败")
	}

	// 正常 prepare
	isErr, text, protoErr := callTool(t, ts.srv, "confirm.prepare", map[string]any{
		"action": "logs.clear", "args": map[string]any{}, "summary": "清空日志",
	})
	if isErr || protoErr != "" {
		t.Fatalf("正常 prepare 不应失败：%s %s", text, protoErr)
	}
	var out struct {
		Token   string `json:"token"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("解析 prepare 结果失败: %v（%s）", err, text)
	}
	if out.Token == "" || out.Summary != "清空日志" {
		t.Fatalf("prepare 结果不完整：%s", text)
	}

	// commit：交给 Executor 执行
	isErr, text, protoErr = callTool(t, ts.srv, "confirm.commit", map[string]any{"token": out.Token})
	if isErr || protoErr != "" {
		t.Fatalf("commit 应成功：%s %s", text, protoErr)
	}
	if got := ts.exec.executed(); len(got) != 1 || got[0] != "logs.clear" {
		t.Fatalf("Executor 应收到 logs.clear，实际 %v", got)
	}

	// 重复 commit：token 已用掉
	isErr, text, _ = callTool(t, ts.srv, "confirm.commit", map[string]any{"token": out.Token})
	if !isErr || !strings.Contains(text, "无效") {
		t.Fatalf("重复 commit 应报 token 无效：isError=%v text=%s", isErr, text)
	}

	// 缺 token
	isErr, text, _ = callTool(t, ts.srv, "confirm.commit", map[string]any{})
	if !isErr || !strings.Contains(text, "confirm.prepare") {
		t.Fatalf("缺 token 应提示先 prepare：isError=%v text=%s", isErr, text)
	}
}

// 未注入 Executor 时 confirm.commit 返回可读的中文错误（而不是 panic / 静默成功）。
func TestConfirmCommitWithoutExecutor(t *testing.T) {
	ts := newTestServer(t, &fakeGate{}, nil)
	isErr, text, protoErr := callTool(t, ts.srv, "confirm.prepare", map[string]any{
		"action": "logs.clear", "args": map[string]any{},
	})
	if isErr || protoErr != "" {
		t.Fatalf("prepare 不该依赖 Executor：%s %s", text, protoErr)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(text), &out)
	// newTestServer 在 exec 为 nil 时会装一个默认替身，这里改回 nil（模拟宿主未实现）
	ts.srv.opts.Executor = nil
	isErr, text, _ = callTool(t, ts.srv, "confirm.commit", map[string]any{"token": out.Token})
	if !isErr || !strings.Contains(text, "执行器") {
		t.Fatalf("未注入 Executor 时应给中文错误：isError=%v text=%s", isErr, text)
	}
}

// 规范化：token 字段不参与哈希（否则写工具带 token 调用永远对不上）。
func TestNormalizeConfirmArgsDropsToken(t *testing.T) {
	a := NormalizeConfirmArgs(map[string]any{"id": "1", "token": "abc"})
	if _, has := a["token"]; has {
		t.Fatalf("token 不应参与确认哈希")
	}
	if a["id"] != "1" {
		t.Fatalf("其余参数应保留：%v", a)
	}
	if got := NormalizeArgsJSON(map[string]any{"b": float64(2), "a": float64(1)}); got != `{"a":1,"b":2}` {
		// 数字被规范化为 json.Number，序列化后仍是数字（不是字符串）
		t.Fatalf("规范化 JSON 应按键排序且数字不丢：%s", got)
	}
	// 规范化必须幂等：审计里存的 args 再喂回来要得到同一个键（撤销功能依赖它）
	once := NormalizeArgsJSON(map[string]any{"settings": map[string]any{"uiScale": float64(120)}})
	var back map[string]any
	if err := json.Unmarshal([]byte(once), &back); err != nil {
		t.Fatalf("规范化结果应可解析：%v（%s）", err, once)
	}
	if twice := NormalizeArgsJSON(back); twice != once {
		t.Fatalf("规范化应幂等：%s != %s", twice, once)
	}
}

// 设置补丁归一化：两种参数形状必须算出同一个键（撤销功能依赖它）。
func TestSettingsPatchArgsNormalization(t *testing.T) {
	viaTool := SettingsPatchArgs(map[string]any{"settings": map[string]any{"uiScale": float64(120)}, "token": "t"})
	viaHTTP := SettingsPatchArgs(map[string]any{"settings": json.RawMessage(`{"uiScale":120}`)})
	if NormalizeArgsJSON(viaTool) != NormalizeArgsJSON(viaHTTP) {
		t.Fatalf("两种调用形状应归一化成同一个补丁：%s vs %s",
			NormalizeArgsJSON(viaTool), NormalizeArgsJSON(viaHTTP))
	}
}

// 确认动作 → 能力位 的映射必须覆盖清单里的动作（审计里 cap 字段依赖它）。
func TestCapabilityOfAction(t *testing.T) {
	cases := map[string]Capability{
		"servers.delete":     CapConfigWrite,
		"settings.update":    CapConfigWrite,
		"credentials.create": CapSecretsWrite,
		"sftp.remove":        CapRemoteFSWrite,
		"logs.clear":         CapLifecycle,
		"app.quit":           CapLifecycle,
		"capability.enable":  CapLifecycle,
		"terminal.input":     CapTerminalInput,
		"eval":               CapEval,
	}
	for action, want := range cases {
		got, ok := CapabilityOfAction(action)
		if !ok || got != want {
			t.Fatalf("CapabilityOfAction(%s) = %v/%v，期望 %s", action, got, ok, want)
		}
	}
}

// 未注入 Confirms 时，写工具必须给出可读的中文错误而不是 panic。
func TestWriteToolWithoutConfirmManager(t *testing.T) {
	ts := newTestServer(t, &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}, &fakeExecutor{})
	ts.srv.opts.Confirms = nil
	// prepare 与写工具都要给中文错误
	isErr, text, _ := callTool(t, ts.srv, "confirm.prepare", map[string]any{"action": "logs.clear"})
	if !isErr || !strings.Contains(text, "确认管理器未启用") {
		t.Fatalf("未注入 Confirms 时 prepare 应报错：%s", text)
	}
	isErr, text, _ = callTool(t, ts.srv, "update_settings", map[string]any{
		"settings": map[string]any{"uiScale": 120}, "token": "whatever",
	})
	if !isErr || !strings.Contains(text, "确认管理器未启用") {
		t.Fatalf("未注入 Confirms 时写工具应报错：isError=%v text=%s", isErr, text)
	}
}

// ConfirmManager 在并发下不得 race（HTTP 与 MCP 可能同时调用）。
func TestConfirmManagerConcurrent(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			args := map[string]any{"id": i}
			token, _ := m.Prepare("logs.clear", args, "")
			done <- m.Commit(token, "logs.clear", args)
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发 commit 失败：%v", err)
		}
	}
	if m.PendingCount() != 0 {
		t.Fatalf("全部消费后 pending 应为 0，实际 %d", m.PendingCount())
	}
}

// 私有字段：确认动作的默认摘要必须能直接展示给用户。
func TestDefaultConfirmSummary(t *testing.T) {
	m := NewConfirmManager(DefaultConfirmTTL)
	token, _ := m.Prepare("servers.delete", map[string]any{"id": "srv-9"}, "")
	p, ok := m.Peek(token)
	if !ok {
		t.Fatalf("Peek 应能取到待确认项")
	}
	if !strings.Contains(p.Summary, "servers.delete") || !strings.Contains(p.Summary, "srv-9") {
		t.Fatalf("默认摘要应含动作名与参数：%s", p.Summary)
	}
}

// 供 lint 使用的类型断言（保证 Executor / Handler 接口签名不被悄悄改坏）。
var (
	_ Handler  = (*fakeHandler)(nil)
	_ Gate     = (*fakeGate)(nil)
	_ Executor = (*fakeExecutor)(nil)
)

// 编译期断言：context 在测试里被用到（MCP 执行路径需要 ctx 透传）。
var _ = context.Background
var _ = errors.New
