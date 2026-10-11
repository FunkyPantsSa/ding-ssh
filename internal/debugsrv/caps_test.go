package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// ---- 测试替身 ----
//
// 这一组测试直接打 MCP 的 dispatch 层（Server.mcpDispatch），因为它就是 AI 实际
// 走的那条路径：能力校验 → 两段式确认 → 执行 → 审计，全部在同一处完成。
// 用替身 Handler / Gate / Executor 把「宿主」隔离掉，测试只关心权限内核本身。

// fakeGate 可配置的能力门：denied 里的能力返回中文拒绝原因。
type fakeGate struct {
	denied map[Capability]string
	caps   map[Capability]bool
}

func (g *fakeGate) Allow(cap Capability, detail string) error {
	if cap == CapRead {
		return nil
	}
	if msg, bad := g.denied[cap]; bad {
		if detail != "" {
			return fmt.Errorf("%s（涉及：%s）", msg, detail)
		}
		return errors.New(msg)
	}
	return nil
}

func (g *fakeGate) Snapshot() map[Capability]bool {
	out := make(map[Capability]bool, len(allCapabilities))
	for _, c := range allCapabilities {
		out[c] = g.caps[c]
	}
	out[CapRead] = true
	return out
}

// fakeHandler 记录被调用过的 op，并按 op 返回固定结果。
type fakeHandler struct {
	mu    sync.Mutex
	calls []string
}

func (h *fakeHandler) Handle(_ context.Context, op string, _ json.RawMessage) (any, error) {
	h.mu.Lock()
	h.calls = append(h.calls, op)
	h.mu.Unlock()
	return map[string]any{"op": op}, nil
}

func (h *fakeHandler) called(op string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.calls {
		if c == op {
			return true
		}
	}
	return false
}

func (h *fakeHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

// fakeExecutor 记录被执行的确认动作。
type fakeExecutor struct {
	mu      sync.Mutex
	actions []string
	args    map[string]any
	err     error
}

func (e *fakeExecutor) ExecuteAction(_ context.Context, action string, args map[string]any) (any, error) {
	e.mu.Lock()
	e.actions = append(e.actions, action)
	e.args = args
	e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	return map[string]any{"executed": action}, nil
}

func (e *fakeExecutor) executed() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.actions...)
}

// testServer 是一套组装好的替身环境。
type testServer struct {
	srv   *Server
	h     *fakeHandler
	exec  *fakeExecutor
	audit *AuditLog
	gate  *fakeGate
}

// lastAudit 返回最近一条审计记录（没有则返回零值）。
func (ts *testServer) lastAudit() AuditRecord {
	recs := ts.audit.Recent(1, "", "")
	if len(recs) == 0 {
		return AuditRecord{}
	}
	return recs[0]
}

// newTestServer 组装一个带替身的调试服务（Gate 为 nil 时保留 nil，用于测兜底实现）。
func newTestServer(t *testing.T, gate Gate, exec Executor) *testServer {
	t.Helper()
	fg, _ := gate.(*fakeGate)
	fe, _ := exec.(*fakeExecutor)
	if fe == nil {
		fe = &fakeExecutor{}
	}
	ts := &testServer{h: &fakeHandler{}, exec: fe, audit: NewAuditLog(), gate: fg}
	ts.srv = New(Options{
		Handler:  ts.h,
		Hub:      NewHub(),
		Gate:     gate,
		Audits:   ts.audit,
		Confirms: NewConfirmManager(DefaultConfirmTTL),
		Executor: exec,
	})
	return ts
}

// callTool 调一次 MCP tools/call，返回 (isError, 文本, JSON-RPC 错误)。
func callTool(t *testing.T, s *Server, name string, args map[string]any) (bool, string, string) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatalf("序列化参数失败: %v", err)
	}
	resp := s.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params,
	})
	if resp.Error != nil {
		return false, "", resp.Error.Message + " " + resp.Error.Data
	}
	raw, _ := json.Marshal(resp.Result)
	var out struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 tools/call 结果失败: %v", err)
	}
	text := ""
	if len(out.Content) > 0 {
		text = out.Content[0].Text
	}
	return out.IsError, text, ""
}

// 能力被拒时：工具返回 isError + 中文原因，且**没有**执行到 Handler，同时留下审计记录。
func TestToolCallDeniedByGate(t *testing.T) {
	gate := &fakeGate{denied: map[Capability]string{
		CapConfigWrite: "「配置写入」能力未开启：请在 设置 → 调试模式 → MCP 能力 中开启",
	}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	isErr, text, protoErr := callTool(t, ts.srv, "update_settings", map[string]any{"settings": map[string]any{"uiScale": 120}})
	if protoErr != "" {
		t.Fatalf("被拒不应是 JSON-RPC 协议错误：%s", protoErr)
	}
	if !isErr {
		t.Fatalf("能力被拒时 isError 应为 true")
	}
	if !strings.Contains(text, "配置写入") || !strings.Contains(text, "MCP 能力") {
		t.Fatalf("拒绝原因应包含能力名与开启路径，实际：%s", text)
	}
	if ts.h.called(OpSettingsUpdate) {
		t.Fatalf("能力被拒时不应执行到底层 op")
	}
	rec := ts.lastAudit()
	if rec.Tool != "update_settings" || rec.OK {
		t.Fatalf("审计记录应为失败：%+v", rec)
	}
	if rec.Cap != string(CapConfigWrite) {
		t.Fatalf("审计记录的 cap = %q，期望 %q", rec.Cap, CapConfigWrite)
	}
	if !strings.Contains(rec.Error, "配置写入") {
		t.Fatalf("审计记录的失败原因应含中文原因，实际：%s", rec.Error)
	}
}

// prepareToken 走一次 confirm.prepare 拿 token（写工具的必经步骤）。
func prepareToken(t *testing.T, s *Server, action string, args map[string]any) string {
	t.Helper()
	isErr, text, protoErr := callTool(t, s, "confirm.prepare", map[string]any{
		"action": action, "args": args, "summary": "测试",
	})
	if isErr || protoErr != "" {
		t.Fatalf("confirm.prepare 失败：%s %s", text, protoErr)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("解析 confirm.prepare 结果失败: %v（%s）", err, text)
	}
	if out.Token == "" {
		t.Fatalf("confirm.prepare 未返回 token：%s", text)
	}
	return out.Token
}

// 能力放行后应正常执行，并留下成功审计记录。
func TestToolCallAllowedByGate(t *testing.T) {
	gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	args := map[string]any{"settings": map[string]any{"uiScale": 120}}
	token := prepareToken(t, ts.srv, "settings.update", args)
	args["token"] = token

	isErr, text, protoErr := callTool(t, ts.srv, "update_settings", args)
	if isErr || protoErr != "" {
		t.Fatalf("能力开启 + 已确认时不应失败：isError=%v protoErr=%s text=%s", isErr, protoErr, text)
	}
	if !ts.h.called(OpSettingsUpdate) {
		t.Fatalf("能力开启时应执行到底层 op")
	}
	rec := ts.lastAudit()
	if !rec.OK || rec.Cap != string(CapConfigWrite) {
		t.Fatalf("审计记录应为成功且带能力位：%+v", rec)
	}
	if !rec.Reversible || rec.RevertHint == "" {
		t.Fatalf("设置类写操作应标记为可撤销：%+v", rec)
	}
}

// 缺少 token 时必须拒绝执行并提示先 confirm.prepare（不能直接放行）。
func TestWriteToolRequiresToken(t *testing.T) {
	gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	isErr, text, protoErr := callTool(t, ts.srv, "update_settings", map[string]any{"settings": map[string]any{"uiScale": 120}})
	if protoErr != "" {
		t.Fatalf("缺 token 不应是协议错误：%s", protoErr)
	}
	if !isErr {
		t.Fatalf("缺 token 时应 isError")
	}
	if !strings.Contains(text, "confirm.prepare") || !strings.Contains(text, "token") {
		t.Fatalf("缺 token 的提示应说明怎么拿 token：%s", text)
	}
	if ts.h.count() != 0 {
		t.Fatalf("缺 token 时不得执行底层 op，实际 %d 次", ts.h.count())
	}
	// 审计里要留下「被拒」的痕迹
	if rec := ts.lastAudit(); rec.OK || !strings.Contains(rec.Error, "confirm.prepare") {
		t.Fatalf("缺 token 的审计记录应标记失败并含原因：%+v", rec)
	}
}

// 读类工具不受能力开关限制：Gate 全部拒绝也能读。
func TestReadToolsAlwaysAllowed(t *testing.T) {
	gate := &fakeGate{denied: map[Capability]string{
		CapTerminalInput: "no", CapConfigWrite: "no", CapLifecycle: "no", CapEval: "no",
	}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	for _, tc := range []struct {
		tool string
		op   string
		args map[string]any
	}{
		{"app_state", OpState, nil},
		{"list_terminals", OpTerminals, nil},
		{"get_settings", OpSettings, nil},
		{"list_logs", OpLogs, nil},
		{"app.permissions", "", nil},
		{"app.health", OpStatus, nil},
	} {
		isErr, text, protoErr := callTool(t, ts.srv, tc.tool, tc.args)
		if isErr || protoErr != "" {
			t.Fatalf("读类工具 %s 不应被拒：%s %s", tc.tool, text, protoErr)
		}
		if tc.op != "" && !ts.h.called(tc.op) {
			t.Fatalf("读类工具 %s 应执行到底层 op %s", tc.tool, tc.op)
		}
	}
}

// 未注入 Gate 时必须按最保守策略（只允许 read），而不是「全部放行」。
func TestNilGateDeniesWrites(t *testing.T) {
	ts := newTestServer(t, nil, &fakeExecutor{})
	isErr, text, _ := callTool(t, ts.srv, "update_settings", map[string]any{"settings": map[string]any{"uiScale": 120}})
	if !isErr {
		t.Fatalf("Gate 为 nil 时写操作应被拒")
	}
	if !strings.Contains(text, "最保守") {
		t.Fatalf("拒绝原因应说明原因，实际：%s", text)
	}
	if ts.h.count() != 0 {
		t.Fatalf("Gate 为 nil 时不应有任何底层调用，实际 %d 次", ts.h.count())
	}
}

// 工具表：数量、描述末尾的「能力要求：…」行、以及能力表登记完整。
func TestToolListCapabilityNotes(t *testing.T) {
	gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	resp := ts.srv.mcpDispatch(context.Background(), rpcRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("tools/list 失败: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var out struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 tools/list 失败: %v", err)
	}
	// 工具总数不再写死：改为**注册表驱动** —— 遍历 AllRegisteredToolNames() 断言
	// 「每个已注册能力位的工具都出现在 tools/list、且都能查到能力登记」。
	// 后续各域（UI 观测 / 应用控制 / SFTP）继续追加工具时，这里不需要改任何数字。
	assertRegistryToolsListed(t, toolNamesOf(out.Tools))
	for _, tool := range out.Tools {
		if !strings.Contains(tool.Description, "能力要求：") {
			t.Fatalf("工具 %s 的描述缺少「能力要求」行", tool.Name)
		}
		// 能力位可以登记在内置表（toolCaps）或各域自己的注册表（RegisterToolCap），
		// 因此这里统一走 ToolCapabilities（先注册表、后内置表）。
		if _, known := ToolCapabilities(tool.Name); !known {
			t.Fatalf("工具 %s 没有登记能力位（toolCaps 或 RegisterToolCap）", tool.Name)
		}
	}
	// 写类工具必须提示先 confirm.prepare（AI 只有看到提示才会去准备 token）
	for _, tool := range out.Tools {
		if tool.Name != "update_settings" {
			continue
		}
		if !strings.Contains(tool.Description, "confirm.prepare") {
			t.Fatalf("写类工具描述应提示先 confirm.prepare：%s", tool.Description)
		}
	}
}

// app.permissions 返回快照 + 说明 + 工具能力表。
func TestAppPermissionsView(t *testing.T) {
	gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})
	isErr, text, protoErr := callTool(t, ts.srv, "app.permissions", nil)
	if isErr || protoErr != "" {
		t.Fatalf("app.permissions 失败：%s %s", text, protoErr)
	}
	for _, want := range []string{"config.write", "terminal.input", "fs.remote.write", "descriptions", "confirmActions"} {
		if !strings.Contains(text, want) {
			t.Fatalf("app.permissions 输出缺少 %q：%s", want, text)
		}
	}
}

// app.recent_calls 读取审计记录（含过滤参数）。
func TestAppRecentCalls(t *testing.T) {
	gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})
	// 先制造一条写操作记录
	callTool(t, ts.srv, "update_settings", map[string]any{"settings": map[string]any{"uiScale": 130}})

	isErr, text, protoErr := callTool(t, ts.srv, "app.recent_calls", map[string]any{"limit": 10, "source": "mcp"})
	if isErr || protoErr != "" {
		t.Fatalf("app.recent_calls 失败：%s %s", text, protoErr)
	}
	if !strings.Contains(text, "update_settings") || !strings.Contains(text, "config.write") {
		t.Fatalf("app.recent_calls 应返回刚发生的调用：%s", text)
	}
}

// initialize 的 instructions 里必须带上当前能力快照。
func TestInitializeIncludesCapabilitySnapshot(t *testing.T) {
	gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true, CapTerminalInput: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})
	resp := ts.srv.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize",
		Params: json.RawMessage(`{"protocolVersion":"2025-03-26"}`),
	})
	raw, _ := json.Marshal(resp.Result)
	var out struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 initialize 失败: %v", err)
	}
	if !strings.Contains(out.Instructions, "当前能力快照") {
		t.Fatalf("instructions 缺少能力快照：%s", out.Instructions)
	}
	if !strings.Contains(out.Instructions, "配置写入(config.write)") {
		t.Fatalf("instructions 未列出已开启的能力：%s", out.Instructions)
	}
	if !strings.Contains(out.Instructions, "confirm.prepare") {
		t.Fatalf("instructions 未说明两段式确认：%s", out.Instructions)
	}
}

// 能力枚举与说明必须一一对应（防止新增能力忘了写说明）。
func TestCapabilityDescriptionsComplete(t *testing.T) {
	for _, c := range AllCapabilities() {
		if strings.TrimSpace(CapabilityDescription(c)) == "" {
			t.Fatalf("能力 %s 缺少中文说明", c)
		}
		if strings.TrimSpace(CapabilityLabel(c)) == "" {
			t.Fatalf("能力 %s 缺少中文短名", c)
		}
	}
	if _, ok := KnownCapability("not.a.cap"); ok {
		t.Fatalf("未知能力不应被识别")
	}
}

// 未知工具名：返回明确的中文错误，并留下审计记录。
func TestUnknownTool(t *testing.T) {
	ts := newTestServer(t, &fakeGate{}, &fakeExecutor{})
	isErr, text, protoErr := callTool(t, ts.srv, "does.not.exist", nil)
	if !isErr || protoErr != "" {
		t.Fatalf("未知工具应返回 isError：isError=%v proto=%s", isErr, protoErr)
	}
	if !strings.Contains(text, "未知工具") {
		t.Fatalf("未知工具的提示：%s", text)
	}
	if ts.h.count() != 0 {
		t.Fatalf("未知工具不应调用底层 op")
	}
	if rec := ts.lastAudit(); rec.Tool != "does.not.exist" {
		t.Fatalf("未知工具也应留审计记录：%+v", rec)
	}
}

// 钉住地基阶段那批工具的名字与所需能力：后续各域不得改动它们的语义（这里是规格表，不是总数）。
func TestLegacyToolCapabilityTable(t *testing.T) {
	want := map[string]Capability{
		"send_input":      CapTerminalInput,
		"update_settings": CapConfigWrite,
		"set_log_options": CapConfigWrite,
		"clear_logs":      CapLifecycle,
		"eval_js":         CapEval,
	}
	for tool, capName := range want {
		caps, ok := ToolCapabilities(tool)
		if !ok {
			t.Fatalf("工具 %s 未登记能力位", tool)
		}
		found := false
		for _, c := range caps {
			if c == capName {
				found = true
			}
		}
		if !found && capName != "" {
			t.Fatalf("工具 %s 的能力位 = %v，期望包含 %s", tool, caps, capName)
		}
	}
	// 读类工具的登记值必须是 nil（而不是空切片），保证「读类不受限」语义清晰
	for _, tool := range []string{"app_state", "list_terminals", "read_terminal", "app.permissions"} {
		if caps, ok := toolCaps[tool]; !ok || caps != nil {
			t.Fatalf("读类工具 %s 的能力位应为 nil，实际 %v", tool, caps)
		}
	}
}

// ---- 收窄后的两段式范围：可逆 / 高频操作只要能力位，不要 token ----

// send_input 不再需要 token：能力位开启后可直接调用（终端自动化 terminal.run/expect 依赖它）。
func TestSendInputNoLongerRequiresToken(t *testing.T) {
	if action, need := ToolConfirmAction("send_input"); need {
		t.Fatalf("send_input 不应再要求两段式确认，实际 action=%q", action)
	}
	if _, need := toolConfirmActions["send_input"]; need {
		t.Fatalf("内置确认表里不应再有 send_input")
	}
	gate := &fakeGate{caps: map[Capability]bool{CapTerminalInput: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	// 不带 token 也应成功执行
	isErr, text, protoErr := callTool(t, ts.srv, "send_input", map[string]any{"id": "tab-1", "data": "ls\r"})
	if isErr || protoErr != "" {
		t.Fatalf("send_input 不带 token 应成功：isError=%v protoErr=%s text=%s", isErr, protoErr, text)
	}
	if !ts.h.called(OpTerminalInput) {
		t.Fatalf("send_input 应执行到底层 op")
	}
	// 仍然要求能力位
	denied := newTestServer(t, &fakeGate{denied: map[Capability]string{CapTerminalInput: "「终端输入」能力未开启"}}, &fakeExecutor{})
	isErr, text, _ = callTool(t, denied.srv, "send_input", map[string]any{"id": "tab-1", "data": "ls\r"})
	if !isErr || !strings.Contains(text, "终端输入") {
		t.Fatalf("send_input 仍应受能力位约束：isError=%v text=%s", isErr, text)
	}
	// 仍然留审计记录
	if rec := denied.lastAudit(); rec.Tool != "send_input" || rec.OK {
		t.Fatalf("send_input 仍应留审计记录：%+v", rec)
	}
}

// eval_js 不再需要 token：能力位（allowEval）开启后可直接调用。
func TestEvalNoLongerRequiresToken(t *testing.T) {
	if action, need := ToolConfirmAction("eval_js"); need {
		t.Fatalf("eval_js 不应再要求两段式确认，实际 action=%q", action)
	}
	gate := &fakeGate{caps: map[Capability]bool{CapEval: true}}
	ts := newTestServer(t, gate, &fakeExecutor{})

	isErr, text, protoErr := callTool(t, ts.srv, "eval_js", map[string]any{"js": "1+1"})
	if isErr || protoErr != "" {
		t.Fatalf("eval_js 不带 token 应成功：isError=%v protoErr=%s text=%s", isErr, protoErr, text)
	}
	if !ts.h.called(OpEval) {
		t.Fatalf("eval_js 应执行到底层 op")
	}
}

// ui.write 能力位目前没有对应工具（留给后续 UI 观测），但 action 本身不得要求确认。
func TestUIWriteActionDoesNotRequireToken(t *testing.T) {
	if ActionNeedsConfirm("ui.write") {
		t.Fatalf("ui.write 不应要求两段式确认（UI 迭代是高频可逆操作）")
	}
}

// 破坏性操作仍然需要 token（对照上面的用例，防止收窄时误伤）。
func TestDestructiveToolsStillRequireToken(t *testing.T) {
	for _, tc := range []struct {
		tool   string
		action string
		args   map[string]any
	}{
		{"update_settings", "settings.update", map[string]any{"settings": map[string]any{"uiScale": 120}}},
		{"set_log_options", "settings.update", map[string]any{"level": "debug"}},
	} {
		action, need := ToolConfirmAction(tc.tool)
		if !need || action != tc.action {
			t.Fatalf("工具 %s 的确认 action = %q/%v，期望 %q/true", tc.tool, action, need, tc.action)
		}
		gate := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
		ts := newTestServer(t, gate, &fakeExecutor{})
		isErr, text, _ := callTool(t, ts.srv, tc.tool, tc.args)
		if !isErr || !strings.Contains(text, "confirm.prepare") {
			t.Fatalf("破坏性工具 %s 缺 token 时应拒绝并提示 prepare：isError=%v text=%s", tc.tool, isErr, text)
		}
	}
}
