package debugsrv

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 注册 API 的用例：后续三路（UI 观测 / 应用控制 / SFTP）会在各自的文件里 init() 注册，
// 这里验证「注册后描述自动出现能力要求」「同名覆盖」「非法注册不静默」等行为。

// resetRegistry 保证每个用例从干净状态开始，并在结束时清掉注册（避免互相影响）。
func resetRegistry(t *testing.T) {
	t.Helper()
	ResetRegistrationsForTest()
	t.Cleanup(ResetRegistrationsForTest)
}

// 注册能力位 + 确认动作后：tools/list 的描述里自动出现「能力要求：…」与 token 提示。
func TestRegisterToolCapAppearsInToolsList(t *testing.T) {
	resetRegistry(t)
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("初始不应有注册错误：%v", errs)
	}
	RegisterToolCap("sftp.write", CapRemoteFSWrite)
	RegisterToolConfirm("sftp.write", "sftp.write")
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("合法注册不应报错：%v", errs)
	}

	// ToolCapabilities 能查到注册值
	caps, ok := ToolCapabilities("sftp.write")
	if !ok || len(caps) != 1 || caps[0] != CapRemoteFSWrite {
		t.Fatalf("注册后 ToolCapabilities = %v/%v", caps, ok)
	}
	if action, need := ToolConfirmAction("sftp.write"); !need || action != "sftp.write" {
		t.Fatalf("注册后 ToolConfirmAction = %q/%v", action, need)
	}
	if !ActionNeedsConfirm("sftp.write") {
		t.Fatalf("注册的 action 应进入需要确认的清单")
	}
	// ConfirmableActions 合并注册项
	found := false
	for _, a := range ConfirmableActions() {
		if a == "sftp.remove" || a == "sftp.write" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ConfirmableActions 应包含注册的 sftp 动作：%v", ConfirmableActions())
	}

	// 描述自动带能力要求（把工具挂进 mcpTools 的描述生成路径验证）
	note := testServerForNotes(t).capabilityRequirementText("sftp.write")
	if !strings.Contains(note, "远端文件写入") || !strings.Contains(note, "fs.remote.write") {
		t.Fatalf("能力要求文案应含能力名：%s", note)
	}
	if !strings.Contains(note, "confirm.prepare") || !strings.Contains(note, "sftp.write") {
		t.Fatalf("破坏性工具的能力要求应提示 confirm.prepare：%s", note)
	}

	// tools/list 的整体输出里也应出现（sftp.write 是各域注册进来的工具，不在内置表里，
	// 因此这里直接用描述生成函数验证；后续三路把工具加进 mcpTools 后即自动生效）
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})
	resp := ts.srv.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list",
	})
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "能力要求：") {
		t.Fatalf("tools/list 的所有工具描述都应含「能力要求：」")
	}
}

// 只注册能力位、不注册确认：描述里应说明「不需要 confirm.prepare」。
func TestRegisterToolCapWithoutConfirm(t *testing.T) {
	resetRegistry(t)
	RegisterToolCap("ui.apply_css", CapUIWrite)
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("合法注册不应报错：%v", errs)
	}
	note := testServerForNotes(t).capabilityRequirementText("ui.apply_css")
	if !strings.Contains(note, "界面写入") {
		t.Fatalf("能力要求文案应含能力名：%s", note)
	}
	if strings.Contains(note, "需先 confirm.prepare") {
		t.Fatalf("未注册确认的工具不应提示 token：%s", note)
	}
	if !strings.Contains(note, "不需要 confirm.prepare") {
		t.Fatalf("应显式说明可逆 / 高频操作不需要 token：%s", note)
	}
	if ActionNeedsConfirm("ui.write") {
		t.Fatalf("注册能力位不应顺带要求确认")
	}
}

// 同名重复注册：以最后一次为准（覆盖内置表也要生效）。
func TestRegisterToolCapOverride(t *testing.T) {
	resetRegistry(t)
	RegisterToolCap("send_input", CapTerminalInput)
	RegisterToolCap("send_input", CapUIWrite) // 覆盖
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("覆盖注册不应报错：%v", errs)
	}
	caps, _ := ToolCapabilities("send_input")
	if len(caps) != 1 || caps[0] != CapUIWrite {
		t.Fatalf("重复注册应以最后一次为准，实际 %v", caps)
	}

	// 覆盖内置工具：eval_js 内置 CapEval，注册表覆盖成 CapUIWrite
	RegisterToolCap("eval_js", CapUIWrite)
	caps, _ = ToolCapabilities("eval_js")
	if len(caps) != 1 || caps[0] != CapUIWrite {
		t.Fatalf("注册表应覆盖内置表，实际 %v", caps)
	}
}

// 非法注册：不写入、不 panic，记进 RegistrationErrors。
func TestRegisterToolCapInvalid(t *testing.T) {
	resetRegistry(t)

	RegisterToolCap("bad.cap", Capability("not.a.cap"))
	errs := RegistrationErrors()
	if len(errs) != 1 {
		t.Fatalf("非法能力名应记 1 条错误，实际 %d：%v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "未知能力") {
		t.Fatalf("错误文案应说明未知能力：%v", errs[0])
	}
	if _, ok := ToolCapabilities("bad.cap"); ok {
		t.Fatalf("非法注册不应写入注册表")
	}

	// 空工具名
	RegisterToolCap("  ")
	// 空能力列表
	RegisterToolCap("no.caps")
	if got := len(RegistrationErrors()); got != 3 {
		t.Fatalf("应累计 3 条错误，实际 %d：%v", got, RegistrationErrors())
	}

	// 清掉错误后可以正常注册
	ResetRegistrationsForTest()
	RegisterToolCap("ok.tool", CapRead)
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("ResetRegistrationsForTest 后应无错误：%v", errs)
	}
}

// 非法 confirm 注册：空 action / 缺少「域.动词」形式都要报错。
func TestRegisterToolConfirmInvalid(t *testing.T) {
	resetRegistry(t)
	RegisterToolConfirm("sftp.remove", "")
	RegisterToolConfirm("sftp.remove", "remove") // 缺少域前缀
	if got := len(RegistrationErrors()); got != 2 {
		t.Fatalf("应记 2 条错误，实际 %d：%v", got, RegistrationErrors())
	}
	for _, err := range RegistrationErrors() {
		if !strings.Contains(err.Error(), "action") {
			t.Fatalf("错误文案应提到 action：%v", err)
		}
	}
	// 合法注册
	ResetRegistrationsForTest()
	RegisterToolConfirm("sftp.remove", "sftp.remove")
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("合法注册不应报错：%v", errs)
	}
	if !ActionNeedsConfirm("sftp.remove") {
		t.Fatalf("注册后应需要确认")
	}
}

// UnregisterToolConfirm：显式取消内置工具的确认要求（空字符串 = 不需要确认）。
func TestUnregisterToolConfirm(t *testing.T) {
	resetRegistry(t)
	if action, need := ToolConfirmAction("clear_logs"); !need || action != "logs.clear" {
		t.Fatalf("内置 clear_logs 应需要确认，实际 %q/%v", action, need)
	}
	UnregisterToolConfirm("clear_logs")
	if action, need := ToolConfirmAction("clear_logs"); need {
		t.Fatalf("取消后不应需要确认，实际 action=%q", action)
	}
	if ActionNeedsConfirm("logs.clear") {
		// logs.clear 同时是内置清单里的 action，因此仍然需要确认 ——
		// 这是有意的：取消的是「工具级」要求，动作级清单仍由 confirmActionNeedsConfirm 决定。
		t.Logf("说明：logs.clear 仍在内置 action 清单里（工具级取消不影响动作级）")
	}
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("取消注册不应报错：%v", errs)
	}
}

// 并发注册不 race（后续三路并行 init 时也要安全）；配合 -race 更有意义。
func TestRegisterToolCapConcurrent(t *testing.T) {
	resetRegistry(t)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			RegisterToolCap("concurrent.tool", CapRead)
			RegisterToolConfirm("concurrent.tool", "settings.update")
			_ = ConfirmableActions()
			_, _ = ToolCapabilities("concurrent.tool")
			_, _ = ToolConfirmAction("concurrent.tool")
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("并发注册不应报错：%v", errs)
	}
}

// 内置工具的能力登记必须完整（注册表不影响内置表），且**不写死数量**：
// 断言「内置工具条目 ↔ 内置能力表 toolCaps」双向一一对应（screenshot 这类无 op 的读类工具除外）。
func TestBuiltinToolCapsComplete(t *testing.T) {
	resetRegistry(t)
	entries := builtinToolEntries()
	if len(entries) == 0 {
		t.Fatal("内置工具条目为空（builtinToolEntries 被清空了？）")
	}
	entryNames := make(map[string]bool, len(entries))
	for _, e := range entries {
		entryNames[e.Name] = true
		if _, ok := toolCaps[e.Name]; !ok && !builtinToolsWithoutCaps[e.Name] {
			t.Fatalf("内置工具 %s 有工具条目但没有能力登记（toolCaps）", e.Name)
		}
	}
	for name := range toolCaps {
		if !entryNames[name] {
			t.Fatalf("toolCaps 里的 %s 没有工具条目（不会出现在 tools/list）", name)
		}
	}
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("初始注册表应无错误：%v", errs)
	}
}

// testServerForNotes 造一个只用于生成描述文案的 Server（不需要 Gate 等依赖）。
func testServerForNotes(t *testing.T) *Server {
	t.Helper()
	return New(Options{Handler: &fakeHandler{}})
}
