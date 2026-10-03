package debugsrv

// M10：terminal.sudo（用应用里保存的密码执行 sudo -i）的用例。
//
// 全部用替身（sudoStubHandler + 真实 Hub），**不连 SSH、不碰真实服务器、更不会真的执行 sudo**：
//   - 「本机终端 / 无服务器 / 无保存密码」由替身在 OpSudoStage 上返回对应的中文错误来模拟
//     （真实实现在 debug.go 的 ctlSudoCredential：App.local.Has → 本机终端；session.node → 服务器节点；
//     节点上 Password 为空 → 没有保存密码）；
//   - 「终端输出」由替身在收到 OpTerminalInput 时往 Hub 发 ssh:output 事件，形状与真实链路一致；
//   - 「输出净化」由替身按真实宿主的语义实现（把密码换成 ***），从而断言返回值里没有明文。
//
// 覆盖的规格点：能力位 / 确认登记、拒绝路径（本机终端 / 无服务器 / 无保存密码 / AllowSecrets=false /
// 白名单不含 /tmp / 能力未开）、临时文件生命周期（写入 → chmod → 使用 → 删除 → 校验，失败路径也要删）、
// 输出净化、密码错误识别、超时、静默、并发拒绝、schema 里没有 password 字段、审计 args 里没有密码。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ---- 替身 ----

// sudoStubHandler sudo 场景的替身宿主。
//
// 它把「宿主才会做/才知道的事」显式建模出来：
//   - OpSudoStage：写密码临时文件；stageErr 用来模拟三条拒绝路径；
//   - OpSudoScrub：拿真文本做净化（密码 → ***），与 debug.go 的 sudoHostScrub 语义一致；
//   - 其它 op（OpTerminalInput 等）交给 sftpStubHandler（它会把输出造进 Hub）。
type sudoStubHandler struct {
	*sftpStubHandler

	mu         sync.Mutex
	password   string
	stageErr   error
	stageCalls []string // 每次 stage 的临时文件路径（同一路径出现两次 = 重复写，测试会拦）
	scrubSeen  []string // 每次净化请求的原文（用来证明「宿主确实看到了含密码的文本」）
	inputs     []string // 每次 OpTerminalInput 的数据（用来断言 shell 命令的构造）
}

func (h *sudoStubHandler) Handle(ctx context.Context, op string, raw json.RawMessage) (any, error) {
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	switch op {
	case OpSudoStage:
		p, _ := m["path"].(string)
		h.mu.Lock()
		h.stageCalls = append(h.stageCalls, p)
		err := h.stageErr
		h.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "path": p, "bytes": 8}, nil
	case OpSudoScrub:
		text, _ := m["text"].(string)
		h.mu.Lock()
		h.scrubSeen = append(h.scrubSeen, text)
		pw := h.password
		h.mu.Unlock()
		out := text
		if pw != "" && strings.Contains(out, pw) {
			out = strings.ReplaceAll(out, pw, "***")
		}
		return map[string]any{"text": out, "scrubbed": true}, nil
	}
	// 记录终端输入（便于断言 shell 命令的构造），再交给通用替身（它负责把输出造进 Hub）。
	if op == OpTerminalInput {
		data, _ := m["data"].(string)
		h.mu.Lock()
		h.inputs = append(h.inputs, data)
		h.mu.Unlock()
	}
	return h.sftpStubHandler.Handle(ctx, op, raw)
}

func (h *sudoStubHandler) setPassword(pw string) {
	h.mu.Lock()
	h.password = pw
	h.mu.Unlock()
}

func (h *sudoStubHandler) setStageErr(err error) {
	h.mu.Lock()
	h.stageErr = err
	h.mu.Unlock()
}

func (h *sudoStubHandler) stages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.stageCalls...)
}

func (h *sudoStubHandler) scrubbedTexts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.scrubSeen...)
}

func (h *sudoStubHandler) inputData() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.inputs...)
}

// sudoTestEnv 一套 sudo 替身环境（Gate 可替换，便于覆盖「能力未开」）。
type sudoTestEnv struct {
	srv   *Server
	h     *sudoStubHandler
	hub   *Hub
	scope *sftpStubScope
	audit *AuditLog
	exec  *fakeExecutor
}

// ensureSudoRegistrations 保证 terminal.sudo 的能力位 / 确认登记存在。
//
// 同包其它用例会调 ResetRegistrationsForTest（见 register_test.go），因此每个用例都必须重新登记一次。
func ensureSudoRegistrations(t *testing.T) {
	t.Helper()
	before := len(RegistrationErrors())
	registerSudoCaps()
	if errs := RegistrationErrors(); len(errs) > before {
		t.Fatalf("登记 terminal.sudo 能力位时报错：%v", errs[before:])
	}
}

// newSudoEnv 组装环境（白名单默认为空 = 拒绝一切写，与真实默认一致；gate 为 nil 时全部允许）。
func newSudoEnv(t *testing.T, gate Gate) *sudoTestEnv {
	t.Helper()
	ensureSudoRegistrations(t)
	if gate == nil {
		gate = &fakeGate{}
	}
	env := &sudoTestEnv{
		h:     &sudoStubHandler{sftpStubHandler: &sftpStubHandler{replies: map[string]any{}, errs: map[string]error{}}},
		hub:   NewHub(),
		scope: &sftpStubScope{},
		audit: NewAuditLog(),
		exec:  &fakeExecutor{},
	}
	env.h.hub = env.hub
	env.srv = New(Options{
		Handler:       env.h,
		Hub:           env.hub,
		Gate:          gate,
		Audits:        env.audit,
		Confirms:      NewConfirmManager(DefaultConfirmTTL),
		Executor:      env.exec,
		RemoteFSPaths: env.scope,
	})
	return env
}

func (e *sudoTestEnv) call(t *testing.T, args map[string]any) (bool, string, string) {
	t.Helper()
	return callTool(t, e.srv, OpTerminalSudo, args)
}

// withToken 为 terminal.sudo 准备两段式确认 token 并塞进参数（规格里的参数名是 confirm）。
func (e *sudoTestEnv) withToken(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	token, _ := e.srv.opts.Confirms.Prepare(OpTerminalSudo, args, "测试：sudo")
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["confirm"] = token
	return out
}

// callJSON 调一次并解析返回（失败即 Fatal）。
func (e *sudoTestEnv) callJSON(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	isErr, text, rpcErr := e.call(t, e.withToken(t, args))
	if rpcErr != "" {
		t.Fatalf("terminal.sudo 返回 JSON-RPC 错误（应为 isError 文本）：%s", rpcErr)
	}
	if isErr {
		t.Fatalf("terminal.sudo 意外失败：%s", truncateForLog(text, 300))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("解析返回失败：%v（%s）", err, truncateForLog(text, 200))
	}
	return out
}

// callErr 调一次并断言失败，返回错误文本。
func (e *sudoTestEnv) callErr(t *testing.T, args map[string]any) string {
	t.Helper()
	isErr, text, rpcErr := e.call(t, e.withToken(t, args))
	if rpcErr != "" {
		t.Fatalf("terminal.sudo 返回 JSON-RPC 错误（应为 isError 文本）：%s", rpcErr)
	}
	if !isErr {
		t.Fatalf("terminal.sudo 应失败，实际成功：%s", truncateForLog(text, 200))
	}
	return text
}

// lastAuditRecord 返回最近一条审计记录。
func (e *sudoTestEnv) lastAuditRecord() AuditRecord {
	recs := e.audit.Recent(1, "", "")
	if len(recs) == 0 {
		return AuditRecord{}
	}
	return recs[0]
}

// ---- 测试辅助 ----

// reSudoTempPath 从 shell 命令里抽出临时文件路径（用于断言「同一个路径贯穿 chmod / sudo / rm / ls」）。
var reSudoTempPathTest = regexp.MustCompile(`/tmp/\.ding-sudo-[0-9a-f]{12}`)

// installSudoInput 让替身按「回显 → 输出 → 标记」的顺序造输出（并在兜底清理时回清理标记）。
//
// extra 是 sudo 命令本身产生的输出（例如 uid=0(root) / Sorry, try again）。
func installSudoInput(env *sudoTestEnv, extra string, marker string) {
	env.h.onInput = func(id, data string) {
		if strings.Contains(data, sudoCleanMarker) {
			publishOutput(env.hub, id, fakeWrappedEcho(data, 0))
			publishOutput(env.hub, id, fmt.Sprintf("%s%s\r\n", sudoCleanMarker, "2"))
			return
		}
		publishOutput(env.hub, id, fakeWrappedEcho(data, 40))
		if extra != "" {
			publishOutput(env.hub, id, extra)
		}
		if marker != "" {
			publishOutput(env.hub, id, marker+"\r\n$ ")
		}
	}
}

// ---- 1. 能力位 / 确认登记 ----

func TestSudoCapabilityAndConfirmRegistration(t *testing.T) {
	ensureSudoRegistrations(t)
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("注册期不应有错误：%v", errs)
	}
	for _, want := range sudoWantRegistry {
		if want.Tool != OpTerminalSudo {
			t.Fatalf("规格表里的工具名 = %q，期望 %q", want.Tool, OpTerminalSudo)
		}
		caps, ok := ToolCapabilities(OpTerminalSudo)
		if !ok {
			t.Fatalf("terminal.sudo 没有登记能力位")
		}
		found := false
		for _, c := range caps {
			if c == want.Cap {
				found = true
			}
		}
		if !found {
			t.Fatalf("terminal.sudo 的能力位 = %v，期望包含 %s", caps, want.Cap)
		}
		action, need := ToolConfirmAction(OpTerminalSudo)
		if !need || action != want.Action {
			t.Fatalf("terminal.sudo 的确认动作 = %q/%v，期望 %q/true", action, need, want.Action)
		}
		if !ActionNeedsConfirm(want.Action) {
			t.Fatalf("action %s 必须进可确认清单（否则 confirm.prepare 会拒绝）", want.Action)
		}
	}
	// 两个能力位都要登记（sudo.credential 是本质，terminal.input 是「仍在敲命令」）
	caps, _ := ToolCapabilities(OpTerminalSudo)
	if len(caps) != 2 {
		t.Fatalf("terminal.sudo 的能力位应为 [sudo.credential terminal.input]，实际 %v", caps)
	}
	// ConfirmableActions 必须包含它
	confirmable := map[string]bool{}
	for _, a := range ConfirmableActions() {
		confirmable[a] = true
	}
	if !confirmable[OpTerminalSudo] {
		t.Fatalf("ConfirmableActions 缺少 terminal.sudo：%v", ConfirmableActions())
	}
	// 能力 → 动作 的反向映射（工具描述自动生成用）
	if action, ok := ToolConfirmActionByCap(CapSudoCredential); !ok || action != OpTerminalSudo {
		t.Fatalf("ToolConfirmActionByCap(sudo.credential) = %q/%v，期望 %q/true", action, ok, OpTerminalSudo)
	}
	if c, ok := CapabilityOfAction(OpTerminalSudo); !ok || c != CapSudoCredential {
		t.Fatalf("CapabilityOfAction(terminal.sudo) = %q/%v，期望 %q/true", c, ok, CapSudoCredential)
	}
	// 能力位本身要有中文说明与短名（防止新增能力忘了写）
	if strings.TrimSpace(CapabilityDescription(CapSudoCredential)) == "" {
		t.Fatalf("sudo.credential 缺少中文说明")
	}
	if CapabilityLabel(CapSudoCredential) == string(CapSudoCredential) {
		t.Fatalf("sudo.credential 缺少中文短名")
	}
}

// tools/list 里的描述必须自带「能力要求」与「需先 confirm.prepare」，且列出两个能力。
func TestSudoToolDescription(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	listed, tools := toolsListFromMCP(t, env.srv)
	if !listed[OpTerminalSudo] {
		t.Fatalf("tools/list 里缺少 %s", OpTerminalSudo)
	}
	var entry mcpTool
	for _, tool := range tools {
		if tool.Name == OpTerminalSudo {
			entry = tool
		}
	}
	for _, want := range []string{"能力要求：", "需先 confirm.prepare", "sudo.credential", "terminal.input", "密码"} {
		if !strings.Contains(entry.Description, want) {
			t.Fatalf("terminal.sudo 的描述缺少 %q：%s", want, entry.Description)
		}
	}
}

// ---- 2. schema / 参数（密码永不出现在 schema 里）----

func TestSudoSchemaHasNoPasswordField(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	_, tools := toolsListFromMCP(t, env.srv)
	var schema map[string]any
	for _, tool := range tools {
		if tool.Name == OpTerminalSudo {
			schema = tool.InputSchema
		}
	}
	if schema == nil {
		t.Fatalf("terminal.sudo 没有 inputSchema")
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		t.Fatalf("inputSchema.properties 为空：%v", schema)
	}
	for key := range props {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "pass") || strings.Contains(lower, "pwd") ||
			strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
			t.Fatalf("schema 里不允许出现密码类字段，实际有 %q", key)
		}
	}
	// 规格里的四个参数都在（外加 confirm）
	for _, want := range []string{"id", "command", "timeoutMs", "idleMs"} {
		if _, has := props[want]; !has {
			t.Fatalf("schema 缺少参数 %q：%v", want, props)
		}
	}
	// 调用方硬塞 password → 永不接受
	ensureSudoRegistrations(t)
	env2 := newSudoEnv(t, nil)
	env2.scope.set("/tmp")
	text := env2.callErr(t, map[string]any{"id": "tab-1", "command": "id", "password": "hunter2"})
	if !strings.Contains(text, "不接受 password") {
		t.Fatalf("显式传 password 应被拒绝，实际：%s", text)
	}
	if len(env2.h.stages()) != 0 {
		t.Fatalf("被拒绝的调用不应写临时文件")
	}
}

func TestSudoArgValidation(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"command": "id"}, "缺少 id"},
		{map[string]any{"id": "tab-1"}, "缺少 command"},
		{map[string]any{"id": "tab-1", "command": "id", "timeoutMs": 10}, "越界"},
		{map[string]any{"id": "tab-1", "command": "id", "idleMs": 10}, "越界"},
		{map[string]any{"id": "tab-1", "command": "id", "idleMs": 60001}, "越界"},
	}
	for _, tc := range cases {
		text := env.callErr(t, tc.args)
		if !strings.Contains(text, tc.want) {
			t.Fatalf("参数 %v 的错误应包含 %q，实际：%s", tc.args, tc.want, text)
		}
	}
}

// ---- 3. 能力位 / 前置条件 / 两段式确认 ----

func TestSudoRequiresTwoPhaseConfirm(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	isErr, text, rpcErr := env.call(t, map[string]any{"id": "tab-1", "command": "id"})
	if rpcErr != "" {
		t.Fatalf("缺 token 不应是协议错误：%s", rpcErr)
	}
	if !isErr || !strings.Contains(text, "confirm.prepare") {
		t.Fatalf("缺 token 应提示先 confirm.prepare：isError=%v text=%s", isErr, text)
	}
	if len(env.h.stages()) != 0 {
		t.Fatalf("缺 token 时不应写临时文件 / 执行 sudo")
	}
	if rec := env.lastAuditRecord(); rec.Tool != OpTerminalSudo || rec.OK {
		t.Fatalf("被拒也应留审计记录：%+v", rec)
	}
}

func TestSudoCapabilityDenied(t *testing.T) {
	ensureSudoRegistrations(t)
	gate := &fakeGate{denied: map[Capability]string{
		CapSudoCredential: "「sudo 凭证提权」能力未开启：还需要同时开启「允许读取敏感数据」",
	}}
	env := newSudoEnv(t, gate)
	env.scope.set("/tmp")
	text := env.callErr(t, map[string]any{"id": "tab-1", "command": "id"})
	if !strings.Contains(text, "sudo 凭证提权") || !strings.Contains(text, "允许读取敏感数据") {
		t.Fatalf("能力未开时要说清缺哪个前置条件，实际：%s", text)
	}
	if len(env.h.stages()) != 0 {
		t.Fatalf("能力未开时不应执行到底层 op")
	}
	// terminal.input 也是必需能力：单独关掉它同样要被拒（另一个前置条件）
	env2 := newSudoEnv(t, &fakeGate{denied: map[Capability]string{CapTerminalInput: "「终端输入」能力未开启"}})
	env2.scope.set("/tmp")
	if text := env2.callErr(t, map[string]any{"id": "tab-1", "command": "id"}); !strings.Contains(text, "终端输入") {
		t.Fatalf("terminal.input 未开时也应被拒，实际：%s", text)
	}
}

// ---- 4. 拒绝路径：本机终端 / 无服务器 / 无保存密码 / 白名单不含 /tmp ----

func TestSudoRejectPathsFromHost(t *testing.T) {
	ensureSudoRegistrations(t)
	cases := []struct {
		name     string
		stageErr error
		want     string
	}{
		{"本机终端", errors.New("本机终端不支持 sudo 凭证提权：本机终端没有远端账号，也没有可用的 SFTP 通道"),
			"本机终端不支持 sudo 凭证提权"},
		{"无服务器", errors.New("会话 tab-1 没有对应的服务器：sudo 凭证提权只能用于「由已保存服务器打开的 SSH 会话」"),
			"没有对应的服务器"},
		{"无保存密码", errors.New("服务器「测试机」没有保存密码：请先在应用里为该服务器保存密码（服务器 → 编辑 → 密码）"),
			"请先在应用里为该服务器保存密码"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newSudoEnv(t, nil)
			env.scope.set("/tmp")
			env.h.setStageErr(tc.stageErr)
			text := env.callErr(t, map[string]any{"id": "tab-1", "command": "id"})
			if !strings.Contains(text, tc.want) {
				t.Fatalf("拒绝文案应包含 %q，实际：%s", tc.want, text)
			}
			// 拒绝时不能执行任何终端输入（既没 chmod 也没 sudo）
			if len(env.h.inputData()) != 0 {
				t.Fatalf("stage 被拒后不应向终端发送任何字符：%v", env.h.inputData())
			}
		})
	}
}

func TestSudoWhitelistRequiresTmp(t *testing.T) {
	ensureSudoRegistrations(t)
	// 1) 白名单为空（默认）→ 拒绝，并说明去配置白名单
	env := newSudoEnv(t, nil)
	text := env.callErr(t, map[string]any{"id": "tab-1", "command": "id"})
	if !strings.Contains(text, "白名单") || !strings.Contains(text, "/tmp") {
		t.Fatalf("空白名单应说明需要 /tmp 前缀，实际：%s", text)
	}
	if len(env.h.stages()) != 0 {
		t.Fatalf("白名单不通过时不应调用宿主写密码")
	}
	// 2) 白名单非空但不含 /tmp → 同样拒绝
	env.scope.set("/srv/app")
	text = env.callErr(t, map[string]any{"id": "tab-1", "command": "id"})
	if !strings.Contains(text, "白名单") {
		t.Fatalf("白名单不含 /tmp 时应拒绝，实际：%s", text)
	}
	if len(env.h.stages()) != 0 {
		t.Fatalf("白名单不含 /tmp 时不应调用宿主写密码")
	}
	// 3) 含 /tmp → 放行（走完白名单校验，进入 stage）
	env.scope.set("/tmp")
	env.h.setStageErr(errors.New("停在这里：仅验证白名单放行"))
	_ = env.callErr(t, map[string]any{"id": "tab-1", "command": "id"})
	if len(env.h.stages()) != 1 {
		t.Fatalf("/tmp 在白名单里时应调用宿主写密码，实际 %v", env.h.stages())
	}
}

// ---- 5. 临时文件生命周期 + 输出净化 ----

func TestSudoTempFileLifecycleAndScrub(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	env.h.setPassword("hunter2")
	// 输出里故意带上密码（模拟任何意外回显），验证返回前被替换成 ***
	installSudoInput(env, "hunter2\r\nuid=0(root) gid=0(root)\r\n",
		"__DING_SUDO_RC__0__LS__2__CHMOD__0")

	out := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id -u", "timeoutMs": 3000})

	// 1) 先写密码临时文件，且**只**由宿主做（返回值里没有密码）
	stages := env.h.stages()
	if len(stages) != 1 {
		t.Fatalf("应恰好写一次临时文件，实际 %v", stages)
	}
	tempFile := stages[0]
	if !reSudoTempPathTest.MatchString(tempFile) {
		t.Fatalf("临时文件路径 %q 不符合 /tmp/.ding-sudo-<12 位十六进制>", tempFile)
	}
	if got := out["tempFile"]; got != tempFile {
		t.Fatalf("返回里的 tempFile = %v，期望 %q", got, tempFile)
	}

	// 2) 一条 shell 命令里必须同时有 chmod / sudo（密码经文件重定向）/ rm / ls 校验
	data := env.h.inputData()
	if len(data) == 0 {
		t.Fatalf("应向终端发送命令")
	}
	line := data[0]
	for _, want := range []string{
		"chmod 600 '" + tempFile + "'",
		"sudo -S -p '' -i -- /bin/sh -c 'id -u' < '" + tempFile + "'",
		"rm -f '" + tempFile + "'",
		"ls -d '" + tempFile + "'",
		sudoMarkerPrefix,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("shell 命令缺少 %q：\n%s", want, line)
		}
	}
	// 顺序：chmod 在 sudo 之前，rm 在 sudo 之后
	if !(strings.Index(line, "chmod 600") < strings.Index(line, "sudo -S") &&
		strings.Index(line, "sudo -S") < strings.Index(line, "rm -f")) {
		t.Fatalf("chmod / sudo / rm 的顺序不对：\n%s", line)
	}

	// 3) 返回结构与净化结果
	if out["ok"] != true || out["sudo"] != true || out["matched"] != true || out["reason"] != "matched" {
		t.Fatalf("成功路径的返回不对：%v", out)
	}
	if uiNum(out["exitCode"]) != 0 {
		t.Fatalf("exitCode 应为 0：%v", out)
	}
	if out["cleanedUp"] != true {
		t.Fatalf("ls 退出码非 0 = 文件已删除 → cleanedUp 应为 true：%v", out)
	}
	if out["scrubbed"] != true {
		t.Fatalf("净化应生效：%v", out)
	}
	body, _ := out["output"].(string)
	if strings.Contains(body, "hunter2") {
		t.Fatalf("返回值里不允许出现密码：%s", body)
	}
	if !strings.Contains(body, "***") || !strings.Contains(body, "uid=0(root)") {
		t.Fatalf("净化的输出应保留其余内容并把密码换成 ***：%s", body)
	}
	// 宿主确实看到了含密码的文本（否则「净化」就是空转）
	seen := env.h.scrubbedTexts()
	if len(seen) == 0 || !strings.Contains(seen[0], "hunter2") {
		t.Fatalf("净化请求里应包含含密码的原文：%v", seen)
	}
	// 净化请求里的文本**只**在宿主与 debugsrv 之间传递，不进审计
	if rec := env.lastAuditRecord(); strings.Contains(rec.Args, "hunter2") || strings.Contains(rec.Error, "hunter2") {
		t.Fatalf("审计记录里不允许出现密码：%+v", rec)
	}
}

// 每次调用的临时文件名必须不同（随机性）。
func TestSudoTempFileIsRandom(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	installSudoInput(env, "ok\r\n", "__DING_SUDO_RC__0__LS__2__CHMOD__0")
	first := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id", "timeoutMs": 3000})
	second := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id", "timeoutMs": 3000})
	if first["tempFile"] == second["tempFile"] {
		t.Fatalf("两次调用的临时文件名相同（%v）：随机性不足", first["tempFile"])
	}
	if len(env.h.stages()) != 2 {
		t.Fatalf("两次调用应写两次临时文件：%v", env.h.stages())
	}
}

// 失败路径也必须清理：超时（标记没出现）时补一次 rm + ls 校验。
func TestSudoTimeoutCleansUpTempFile(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	env.h.setPassword("hunter2")
	// 只回显，不给退出码标记 → 一定会超时
	installSudoInput(env, "", "")

	out := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id", "timeoutMs": 300})

	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("超时应返回 matched=false / reason=timeout：%v", out)
	}
	if out["ok"] != false {
		t.Fatalf("超时不是成功：%v", out)
	}
	if out["cleanedUp"] != true {
		t.Fatalf("超时后应兜底清理并确认删除：%v", out)
	}
	// 第二条输入 = 兜底清理命令（rm + ls 校验）
	data := env.h.inputData()
	if len(data) < 2 {
		t.Fatalf("超时后应再发一条兜底清理命令：%v", data)
	}
	clean := data[len(data)-1]
	if !strings.Contains(clean, "rm -f") || !strings.Contains(clean, "ls -d") ||
		!strings.Contains(clean, sudoCleanMarker) {
		t.Fatalf("兜底清理命令不完整：%s", clean)
	}
	if !strings.Contains(clean, env.h.stages()[0]) {
		t.Fatalf("兜底清理必须针对同一个临时文件：%s", clean)
	}
	hint, _ := out["hint"].(string)
	if !strings.Contains(hint, "超时") || !strings.Contains(hint, "不是协议错误") {
		t.Fatalf("超时的中文提示不对：%q", hint)
	}
}

// idleMs 显式给出时启用「静默提前返回」；返回 reason=idle + 中文提示（不是协议错误）。
func TestSudoIdleReturnsChineseHint(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	// 有真实输出但一直不打印标记 → 静默窗口（idleMs=80）先到期
	installSudoInput(env, "uid=0(root)\r\n", "")

	out := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id", "idleMs": 80, "timeoutMs": 5000})
	if out["reason"] != "idle" || out["matched"] != false {
		t.Fatalf("应静默返回 idle：%v", out)
	}
	if uiNum(out["idleMs"]) != 80 {
		t.Fatalf("返回里应如实给出 idleMs：%v", out["idleMs"])
	}
	hint, _ := out["hint"].(string)
	if !strings.Contains(hint, "静默") || !strings.Contains(hint, "不是协议错误") {
		t.Fatalf("静默的中文提示不对：%q", hint)
	}
	if out["cleanedUp"] != true {
		t.Fatalf("静默返回前也要兜底清理：%v", out)
	}
}

// chmod 失败：不执行 sudo，且提示里说明原因。
func TestSudoChmodFailureSkipsSudo(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	installSudoInput(env, "", "__DING_SUDO_RC__-1__LS__2__CHMOD__1")

	out := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id", "timeoutMs": 3000})
	if out["ok"] != false {
		t.Fatalf("chmod 失败不算成功：%v", out)
	}
	if _, has := out["exitCode"]; has {
		t.Fatalf("chmod 失败时不应给出 exitCode：%v", out)
	}
	hint, _ := out["hint"].(string)
	if !strings.Contains(hint, "chmod 600 失败") {
		t.Fatalf("应说明 chmod 失败：%q", hint)
	}
	// shell 命令里 chmod 失败会跳过 sudo（if 分支）
	line := env.h.inputData()[0]
	if !strings.Contains(line, `if [ "$__ding_sudo_chmod" = "0" ]`) {
		t.Fatalf("shell 命令应在 chmod 失败时跳过 sudo：%s", line)
	}
}

// 密码错误 / 无 sudo 权限 / 缺 TTY 等常见失败都要给中文提示。
func TestSudoCommonFailureHints(t *testing.T) {
	ensureSudoRegistrations(t)
	cases := []struct {
		name   string
		extra  string
		code   string
		want   string
		absent string
	}{
		{"密码错误", "sudo: 1 incorrect password attempt\r\nSorry, try again.\r\n", "1",
			"保存的密码不正确或该账号无 sudo 权限", ""},
		{"不在 sudoers", "testuser is not in the sudoers file.  This incident will be reported.\r\n", "1",
			"没有 sudo 权限", ""},
		{"要求 TTY", "sudo: a terminal is required to read the password; either use the -S option\r\n", "1",
			"无法从标准输入读到密码", ""},
		{"没有 sudo 命令", "bash: sudo: command not found\r\n", "127",
			"远端没有 sudo 命令", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newSudoEnv(t, nil)
			env.scope.set("/tmp")
			installSudoInput(env, tc.extra,
				fmt.Sprintf("__DING_SUDO_RC__%s__LS__2__CHMOD__0", tc.code))
			out := env.callJSON(t, map[string]any{"id": "tab-1", "command": "id", "timeoutMs": 3000})
			hint, _ := out["hint"].(string)
			if !strings.Contains(hint, tc.want) {
				t.Fatalf("hint 应包含 %q，实际：%q", tc.want, hint)
			}
			if uiNum(out["exitCode"]) != float64(atoiOrZero(tc.code)) {
				t.Fatalf("exitCode 应如实返回 %s：%v", tc.code, out)
			}
		})
	}
}

func atoiOrZero(s string) int {
	n := 0
	neg := strings.HasPrefix(s, "-")
	for _, r := range strings.TrimPrefix(s, "-") {
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}

// 单元口径：shell 拼装必须把命令原样（含单引号）交给远端 shell，且清理始终在同一条命令里。
func TestSudoShellLineQuoting(t *testing.T) {
	if got := shellSingleQuote(`it's ok`); got != `'it'\''s ok'` {
		t.Fatalf("shellSingleQuote = %s", got)
	}
	line := sudoShellLine("/tmp/.ding-sudo-abcdef012345", `echo 'hi'; rm -rf /`)
	for _, want := range []string{
		`-i -- /bin/sh -c 'echo '\''hi'\''; rm -rf /'`,
		"< '/tmp/.ding-sudo-abcdef012345'",
		"rm -f '/tmp/.ding-sudo-abcdef012345' || true",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("shell 命令缺少 %q：\n%s", want, line)
		}
	}
	// 清理命令同样只做「删 + 校验」
	clean := sudoCleanupLine("/tmp/.ding-sudo-abcdef012345")
	if !strings.Contains(clean, "rm -f") || !strings.Contains(clean, "ls -d") {
		t.Fatalf("兜底清理命令不完整：%s", clean)
	}
}

// 回归：提权调用必须是 `-i -- /bin/sh -c '<command>'` 形式（sudo 不接受 `-c` 选项）。
//
// 断言三件事：
//  1. sudo 自己的选项段（`--` 之前）**不允许**出现 `-c`（sudo 没有这个选项）；
//  2. 复合命令交给内层 `/bin/sh -c` 执行（`-c` 只能作为 /bin/sh 的参数出现）；
//  3. 复合命令整体作为**单个参数**（一对单引号）传递，含空格 / 分号也不会被拆成多个 argv。
//
// 背景（2026-10 真机实测，QNAP Linux 5.10.60-qnap aarch64 / sudo 1.9.12p2）：
//   - `sudo -i -c '<cmd>'` → sudo 打 usage 并以 1 退出，看起来像「密码错误」，实际根本没读密码；
//   - `sudo -i -- 'id; uname -srmo; whoami'` → `-sh: id; uname -srmo; whoami: command not found`：
//     sudo 对 -s/-i 的每个位置参数会**重新加引号**再拼成命令串，整条复合命令因此变成一个命令名
//     （单字命令 `id` 反而能跑）；
//   - `sudo -i -- /bin/sh -c '<cmd>'` → 正常执行。
func TestSudoShellLineUsesShellDashC(t *testing.T) {
	tempFile := "/tmp/.ding-sudo-abcdef012345"
	command := `id; uname -srmo; whoami`
	line := sudoShellLine(tempFile, command)

	// 1) `--` 之前的 sudo 选项段里不允许出现 `-c`。
	sep := strings.Index(line, " -- ")
	if sep < 0 {
		t.Fatalf("缺少 -- （用于结束 sudo 的选项解析）：\n%s", line)
	}
	if re := regexp.MustCompile(`(^|\s)-c(\s|$)`); re.MatchString(line[:sep]) {
		t.Fatalf("sudo 不接受 -c（-c 只能作为 /bin/sh 的参数）：\n%s", line)
	}
	// 2) 完整形式：`sudo -S -p '' -i -- /bin/sh -c '<单引号包裹的整条命令>' < '<临时文件>'`
	want := "sudo -S -p '' -i -- /bin/sh -c '" + command + "' < '" + tempFile + "'"
	if !strings.Contains(line, want) {
		t.Fatalf("提权调用形式应为 %q：\n%s", want, line)
	}
	// 3) 命令必须整体落在一对单引号里（= 单个 argv，不会被拆开）。
	rest := line[sep+len(" -- "):]
	if !strings.HasPrefix(rest, "/bin/sh -c '"+command+"' < '") {
		t.Fatalf("复合命令必须整体单引号包裹后交给 /bin/sh -c，实际：%q", rest)
	}
	// 4) 命令里的单引号仍按 shell 规则转义（原样交给远端 shell）。
	escaped := sudoShellLine(tempFile, `echo 'hi'; rm -rf /`)
	if !strings.Contains(escaped, `-i -- /bin/sh -c 'echo '\''hi'\''; rm -rf /'`) {
		t.Fatalf("命令内的单引号转义被破坏：\n%s", escaped)
	}
	// 5) 单字命令同样成立（不额外加壳）。
	plain := sudoShellLine(tempFile, "id")
	if !strings.Contains(plain, "sudo -S -p '' -i -- /bin/sh -c 'id' < '"+tempFile+"'") {
		t.Fatalf("单字命令的拼装不对：\n%s", plain)
	}
}

// ---- 6. 并发（与 terminal.run / terminal.expect 共用同一名额）----

func TestSudoSharesTerminalSlot(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	if !env.srv.acquireTerminal("tab-1") {
		t.Fatalf("测试自身应能占住名额")
	}
	defer env.srv.releaseTerminal("tab-1")
	text := env.callErr(t, map[string]any{"id": "tab-1", "command": "id"})
	if !strings.Contains(text, "只允许一个") || !strings.Contains(text, "terminal.run") {
		t.Fatalf("并发应直接被拒并说明共用名额，实际：%s", text)
	}
	if len(env.h.stages()) != 0 {
		t.Fatalf("并发被拒时不应写临时文件")
	}
}

// ---- 7. 审计：args 里只有 id / command / timeoutMs / idleMs，没有密码 ----

func TestSudoAuditArgsHaveNoPassword(t *testing.T) {
	ensureSudoRegistrations(t)
	env := newSudoEnv(t, nil)
	env.scope.set("/tmp")
	env.h.setPassword("hunter2")
	installSudoInput(env, "hunter2\r\nok\r\n", "__DING_SUDO_RC__0__LS__2__CHMOD__0")
	_ = env.callJSON(t, map[string]any{"id": "tab-1", "command": "id -u", "timeoutMs": 3000})

	rec := env.lastAuditRecord()
	if rec.Tool != OpTerminalSudo {
		t.Fatalf("审计工具名 = %q", rec.Tool)
	}
	if rec.Cap != string(CapSudoCredential) {
		t.Fatalf("审计的 cap 应是 sudo.credential（主能力），实际 %q", rec.Cap)
	}
	for _, bad := range []string{"hunter2", "password", "/tmp/.ding-sudo"} {
		if strings.Contains(rec.Args, bad) {
			t.Fatalf("审计 args 不允许包含 %q：%s", bad, rec.Args)
		}
	}
	// 两段式确认 token 属于敏感参数：auditSafeArgs 会把它打成 ***
	if !strings.Contains(rec.Args, `"confirm":"***"`) {
		t.Fatalf("审计 args 里 token 应被打码：%s", rec.Args)
	}
	for _, want := range []string{`"id"`, `"command"`, `"timeoutMs"`} {
		if !strings.Contains(rec.Args, want) {
			t.Fatalf("审计 args 应包含 %s：%s", want, rec.Args)
		}
	}
}
