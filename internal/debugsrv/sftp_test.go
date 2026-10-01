package debugsrv

// M8（SFTP + 终端自动化）的用例：能力位 / 确认登记、路径白名单矩阵、体积截断、
// sftp.read 的 text/base64 与 sha256、terminal.run / terminal.expect 的命中 / 超时 / 静默 /
// 命令包裹 / 并发拒绝、参数校验的中文错误。
//
// 全部用替身（sftpStubHandler + sftpStubScope + 真实 Hub），**不连 SSH、不碰真实服务器**：
//   - 「远端」的行为由替身 Handler 按 op 返回（SFTPListPayload / SFTPStatPayload / …）；
//   - 终端输出由替身 Handler 在收到 OpTerminalInput 时往 Hub 发一条 ssh:output 事件，
//     与真实链路（sshx/localterm → notify → Hub.PublishRaw）的形状完全一致。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 替身 ----

// sftpStubScope 可写的白名单来源（替身宿主）。
type sftpStubScope struct {
	mu   sync.Mutex
	list []string
}

func (s *sftpStubScope) SFTPWriteAllowlist() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.list...)
}

func (s *sftpStubScope) set(list ...string) {
	s.mu.Lock()
	s.list = list
	s.mu.Unlock()
}

// sftpStubHandler 按 op 返回预设结果，并记录调用与入参。
//
// onInput 在收到 OpTerminalInput 时被调用（用于把「终端输出」造进 Hub，或做并发同步）。
type sftpStubHandler struct {
	mu      sync.Mutex
	calls   []string
	args    []map[string]any
	replies map[string]any
	errs    map[string]error
	hub     *Hub
	onInput func(sessionID, data string)
}

func (h *sftpStubHandler) Handle(_ context.Context, op string, raw json.RawMessage) (any, error) {
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	h.mu.Lock()
	h.calls = append(h.calls, op)
	h.args = append(h.args, m)
	reply, hasReply := h.replies[op]
	err := h.errs[op]
	onInput, hub := h.onInput, h.hub
	h.mu.Unlock()

	if op == OpTerminalInput && onInput != nil {
		id, _ := m["id"].(string)
		data, _ := m["data"].(string)
		onInput(id, data)
	}
	if err != nil {
		return nil, err
	}
	if hasReply {
		return reply, nil
	}
	_ = hub
	return map[string]any{"op": op}, nil
}

func (h *sftpStubHandler) called(op string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.calls {
		if c == op {
			return true
		}
	}
	return false
}

func (h *sftpStubHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

// lastArgsFor 返回某 op 最近一次的入参。
func (h *sftpStubHandler) lastArgsFor(op string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.calls) - 1; i >= 0; i-- {
		if h.calls[i] == op {
			return h.args[i]
		}
	}
	return nil
}

// sftpTestEnv 一套组装好的 M8 替身环境。
type sftpTestEnv struct {
	srv   *Server
	h     *sftpStubHandler
	hub   *Hub
	scope *sftpStubScope
	audit *AuditLog
	exec  *fakeExecutor
}

// newSFTPEnv 组装环境（白名单默认为空 = 拒绝一切写，与真实默认一致）。
func newSFTPEnv(t *testing.T) *sftpTestEnv {
	t.Helper()
	env := &sftpTestEnv{
		h:     &sftpStubHandler{replies: map[string]any{}, errs: map[string]error{}},
		hub:   NewHub(),
		scope: &sftpStubScope{},
		audit: NewAuditLog(),
		exec:  &fakeExecutor{},
	}
	env.h.hub = env.hub
	env.srv = New(Options{
		Handler:       env.h,
		Hub:           env.hub,
		Gate:          &fakeGate{},
		Audits:        env.audit,
		Confirms:      NewConfirmManager(DefaultConfirmTTL),
		Executor:      env.exec,
		RemoteFSPaths: env.scope,
	})
	return env
}

// call 调一次 MCP 工具并返回 (isError, 文本, JSON-RPC 错误)。
func (e *sftpTestEnv) call(t *testing.T, name string, args map[string]any) (bool, string, string) {
	t.Helper()
	return callTool(t, e.srv, name, args)
}

// callJSON 调一次工具并把返回文本解析成 map（失败即 Fatal）。
func (e *sftpTestEnv) callJSON(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	isErr, text, rpcErr := e.call(t, name, args)
	if rpcErr != "" {
		t.Fatalf("%s 返回 JSON-RPC 错误：%s", name, rpcErr)
	}
	if isErr {
		t.Fatalf("%s 意外失败：%s", name, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("解析 %s 返回失败：%v（%s）", name, err, truncateForLog(text, 200))
	}
	return out
}

// callErr 调一次工具并断言失败，返回错误文本。
func (e *sftpTestEnv) callErr(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	isErr, text, rpcErr := e.call(t, name, args)
	if rpcErr != "" {
		t.Fatalf("%s 返回 JSON-RPC 错误（应为 isError 文本）：%s", name, rpcErr)
	}
	if !isErr {
		t.Fatalf("%s 应失败，实际成功：%s", name, truncateForLog(text, 200))
	}
	return text
}

// withToken 为写类工具准备两段式确认 token 并塞进参数（action 与工具名不一定相同，
// 例如 sftp.syncCwd 复用 action=sftp.write）。
func (e *sftpTestEnv) withToken(t *testing.T, action string, args map[string]any) map[string]any {
	t.Helper()
	token, _ := e.srv.opts.Confirms.Prepare(action, args, "测试："+action)
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["confirm"] = token
	return out
}

// publishOutput 往 Hub 里发一条 ssh:output 事件（形状与 sshx/localterm 完全一致）。
func publishOutput(hub *Hub, sessionID, text string) {
	hub.PublishRaw("ssh:output:"+sessionID, map[string]any{
		"sessionId": sessionID,
		"data":      base64.StdEncoding.EncodeToString([]byte(text)),
	})
}

// ensureSFTPRegistrations 见 registry_test.go（M9 收尾：跨域注册助手统一归位到那里）。

// ---- 1. 能力位 / 确认登记 ----

func TestSFTPCapabilityAndConfirmRegistration(t *testing.T) {
	before := len(RegistrationErrors())
	ensureSFTPRegistrations(t)
	if errs := RegistrationErrors(); len(errs) != before {
		t.Fatalf("注册期不应产生新错误：%v", errs[before:])
	}
	for _, want := range sftpWantRegistry {
		caps, ok := ToolCapabilities(want.Tool)
		if !ok {
			t.Fatalf("工具 %s 没有登记能力位", want.Tool)
		}
		found := false
		for _, c := range caps {
			if c == want.Cap {
				found = true
			}
		}
		if !found {
			t.Fatalf("工具 %s 的能力位 = %v，期望包含 %s", want.Tool, caps, want.Cap)
		}
		action, need := ToolConfirmAction(want.Tool)
		if want.Action == "" {
			if need {
				t.Fatalf("工具 %s 不应要求两段式确认，实际 action=%q", want.Tool, action)
			}
			continue
		}
		if !need || action != want.Action {
			t.Fatalf("工具 %s 的确认 action = %q/%v，期望 %q/true", want.Tool, action, need, want.Action)
		}
		if !ActionNeedsConfirm(want.Action) {
			t.Fatalf("action %s 应进入可确认清单（否则 confirm.prepare 会拒绝）", want.Action)
		}
	}
	// 规格里的 action 名（域.动词）都在可确认清单里
	confirmable := map[string]bool{}
	for _, a := range ConfirmableActions() {
		confirmable[a] = true
	}
	for _, a := range []string{"sftp.write", "sftp.mkdir", "sftp.rename", "sftp.remove", "sftp.cancel"} {
		if !confirmable[a] {
			t.Fatalf("ConfirmableActions 缺少 %s：%v", a, ConfirmableActions())
		}
	}
	// 读类工具必须永远允许（CapRead 不出现在能力快照的"关"里也无所谓，这里只断言登记值）
	for _, name := range []string{OpSftpList, OpSftpStat, OpSftpRead, OpSftpTransfers, OpTerminalExpect} {
		caps, _ := ToolCapabilities(name)
		if len(caps) != 1 || caps[0] != CapRead {
			t.Fatalf("读类工具 %s 的能力位应为 [read]，实际 %v", name, caps)
		}
	}
}

// 规格表工具都必须出现在 tools/list，且描述里带自动生成的能力要求行。
func TestSFTPToolsListed(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	listed, tools := toolsListFromMCP(t, env.srv)
	for _, name := range sftpToolNames {
		if !listed[name] {
			t.Fatalf("tools/list 里缺少 M8 工具 %s", name)
		}
	}
	for _, tool := range tools {
		if !strings.Contains(tool.Description, "能力要求：") {
			t.Fatalf("工具 %s 描述缺少能力要求行", tool.Name)
		}
	}
	// 写类描述必须说明白名单与 token；terminal.run 必须说明并发行为
	byName := map[string]mcpTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if d := byName[OpSftpWrite].Description; !strings.Contains(d, "白名单") || !strings.Contains(d, "confirm.prepare") {
		t.Fatalf("sftp.write 描述应说明白名单与两段式确认：%s", d)
	}
	if d := byName[OpTerminalRun].Description; !strings.Contains(d, "并发") || !strings.Contains(d, "直接拒绝") {
		t.Fatalf("terminal.run 描述应说明并发行为：%s", d)
	}
	if d := byName[OpTerminalExpect].Description; !strings.Contains(d, "不需要 confirm.prepare") {
		t.Fatalf("terminal.expect 是读类，不应要求 token：%s", d)
	}
}

// ---- 2. 白名单矩阵 ----

func TestSFTPPathAllowedMatrix(t *testing.T) {
	allow := []string{"/srv/app", "/var/log/"}
	cases := []struct {
		path string
		want bool
	}{
		{"/srv/app", true},
		{"/srv/app/", true},
		{"/srv/app/sub/file.txt", true},
		{"/var/log", true}, // 白名单里的尾斜杠被归一化
		{"/var/log/syslog", true},
		{"/srv/app-evil", false}, // 关键：段边界，前缀不能越界匹配
		{"/srv/app-evil/x", false},
		{"/srv/ap", false},
		{"/srv", false},
		{"/", false}, // 根目录不参与匹配
		{"/var/logs", false},
		{"", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := SFTPPathAllowed(allow, c.path); got != c.want {
			t.Fatalf("SFTPPathAllowed(%q) = %v，期望 %v", c.path, got, c.want)
		}
	}
	// 空白名单：一切拒绝
	for _, p := range []string{"/", "/srv/app", "/tmp/x"} {
		if SFTPPathAllowed(nil, p) {
			t.Fatalf("空白名单不应放行 %q", p)
		}
	}
	// 空前缀（把整个 / 写进白名单）不参与匹配，避免「白名单 / 等于放行一切」
	if SFTPPathAllowed([]string{""}, "/etc/passwd") {
		t.Fatalf("空前缀不应放行任何路径")
	}
}

func TestNormalizeRemotePathMatrix(t *testing.T) {
	ok := []struct {
		in   string
		want string
	}{
		{"/srv/app", "/srv/app"},
		{"/srv/app/", "/srv/app"},
		{"/srv//app///x", "/srv/app/x"},
		{"/srv/./app", "/srv/app"},
		{"/", "/"},
		{"  /srv/app  ", "/srv/app"},
	}
	for _, c := range ok {
		got, err := NormalizeRemotePath(c.in)
		if err != nil || got != c.want {
			t.Fatalf("NormalizeRemotePath(%q) = %q/%v，期望 %q", c.in, got, err, c.want)
		}
	}
	bad := []string{"", "   ", "srv/app", "./srv", "/srv/../etc", "/../etc", "/srv/app/../../root"}
	for _, in := range bad {
		if _, err := NormalizeRemotePath(in); err == nil {
			t.Fatalf("NormalizeRemotePath(%q) 应报错（相对路径或含 .. 段）", in)
		}
	}
}

// 空白名单 ⇒ 一切写 / 删 / 改名 / 同步都被拒绝，且中文原因要说明去哪配置。
func TestSFTPWriteRejectedWhenAllowlistEmpty(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set() // 空

	writes := []struct {
		tool string
		args map[string]any
	}{
		{OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/x.txt", "content": "hi"}},
		{OpSftpMkdir, map[string]any{"sessionId": "s1", "path": "/srv/app/dir"}},
		{OpSftpRename, map[string]any{"sessionId": "s1", "from": "/srv/app/a", "to": "/srv/app/b"}},
		{OpSftpRemove, map[string]any{"sessionId": "s1", "path": "/srv/app/a"}},
		{OpSftpSyncCwd, map[string]any{"sessionId": "s1", "remotePath": "/srv/app"}},
	}
	for _, w := range writes {
		text := env.callErr(t, w.tool, w.args)
		if !strings.Contains(text, "白名单") {
			t.Fatalf("%s 的拒绝原因应说明白名单：%s", w.tool, text)
		}
		if !strings.Contains(text, "设置") {
			t.Fatalf("%s 的拒绝原因应说明去哪配置：%s", w.tool, text)
		}
	}
	// 白名单为空时**没有**任何宿主调用（连 op 都没发出去）
	if env.h.count() != 0 {
		t.Fatalf("空白名单的写操作不应调用宿主，实际调用：%v", env.h.calls)
	}
}

// 路径不在白名单内（含段边界 / .. 穿越）必须拒绝，且**不能烧掉两段式确认 token**。
func TestSFTPWritePreflightRejectsPathAndKeepsToken(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")

	args := map[string]any{"sessionId": "s1", "path": "/srv/app-evil/x.txt", "content": "hi"}
	token, _ := env.srv.opts.Confirms.Prepare(OpSftpWrite, args, "测试：写入越界路径")
	callArgs := map[string]any{}
	for k, v := range args {
		callArgs[k] = v
	}
	callArgs["confirm"] = token

	text := env.callErr(t, OpSftpWrite, callArgs)
	if !strings.Contains(text, "白名单") || !strings.Contains(text, "/srv/app-evil/x.txt") {
		t.Fatalf("拒绝原因应说明白名单与具体路径：%s", text)
	}
	if env.h.called(OpSftpWrite) {
		t.Fatalf("被白名单拒绝时不应调用宿主 op")
	}
	if got := env.srv.opts.Confirms.PendingCount(); got != 1 {
		t.Fatalf("预检在 token 校验之前，token 不应被消费（PendingCount=%d）", got)
	}
	// 换成白名单内的路径 + 同一个 token：应当通过（证明 token 确实没被烧掉）
	okArgs := map[string]any{"sessionId": "s1", "path": "/srv/app/x.txt", "content": "hi"}
	token2, _ := env.srv.opts.Confirms.Prepare(OpSftpWrite, okArgs, "测试：写入白名单内路径")
	callArgs2 := map[string]any{}
	for k, v := range okArgs {
		callArgs2[k] = v
	}
	callArgs2["confirm"] = token2
	env.h.replies[OpSftpWrite] = SFTPWritePayload{Path: "/srv/app/x.txt", Bytes: 2}
	out := env.callJSON(t, OpSftpWrite, callArgs2)
	if out["ok"] != true {
		t.Fatalf("白名单内写入应成功：%v", out)
	}
	if !env.h.called(OpSftpWrite) {
		t.Fatalf("白名单内写入应调用宿主 op")
	}
	sent := env.h.lastArgsFor(OpSftpWrite)
	if sent["path"] != "/srv/app/x.txt" {
		t.Fatalf("宿主收到的路径应为归一化后的路径：%v", sent)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(anyStr(sent["base64"])); string(decoded) != "hi" {
		t.Fatalf("宿主应收到 base64 编码的内容：%v", sent["base64"])
	}
}

// rename 的 from 与 to 都要过白名单。
func TestSFTPRenameRequiresBothEnds(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	env.h.replies[OpSftpRename] = SFTPMutationPayload{OK: true}

	// to 越界
	text := env.callErr(t, OpSftpRename, map[string]any{"sessionId": "s1", "from": "/srv/app/a", "to": "/srv/app-evil/b"})
	if !strings.Contains(text, "to") || !strings.Contains(text, "白名单") {
		t.Fatalf("to 越界应被拒绝并指明参数名：%s", text)
	}
	// from 越界
	text = env.callErr(t, OpSftpRename, map[string]any{"sessionId": "s1", "from": "/tmp/a", "to": "/srv/app/b"})
	if !strings.Contains(text, "from") {
		t.Fatalf("from 越界应被拒绝并指明参数名：%s", text)
	}
	if env.h.called(OpSftpRename) {
		t.Fatalf("两端校验未通过时不应调用宿主")
	}
	// 两端都在白名单内 → 通过
	env.callJSON(t, OpSftpRename, env.withToken(t, OpSftpRename, map[string]any{
		"sessionId": "s1", "from": "/srv/app/a", "to": "/srv/app/b",
	}))
	sent := env.h.lastArgsFor(OpSftpRename)
	if sent["from"] != "/srv/app/a" || sent["to"] != "/srv/app/b" {
		t.Fatalf("宿主应收到两端路径：%v", sent)
	}
}

// .. 穿越（含白名单前缀内的 ..）必须被拒绝。
func TestSFTPWriteRejectsDotDot(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	for _, p := range []string{"/srv/app/../etc/passwd", "/srv/../etc/passwd", "/srv/app/a/../../b"} {
		text := env.callErr(t, OpSftpWrite, map[string]any{"sessionId": "s1", "path": p, "content": "x"})
		if !strings.Contains(text, "..") && !strings.Contains(text, "穿越") {
			t.Fatalf("含 .. 的路径应被拒绝并说明原因：%s", text)
		}
	}
	if env.h.called(OpSftpWrite) {
		t.Fatalf("含 .. 的路径不应调用宿主")
	}
	// 相对路径也拒绝（写类只接受绝对路径）
	text := env.callErr(t, OpSftpMkdir, map[string]any{"sessionId": "s1", "path": "srv/app"})
	if !strings.Contains(text, "绝对路径") {
		t.Fatalf("相对路径应被拒绝并说明原因：%s", text)
	}
}

// 写入内容体积上限（1MB）在预检阶段就被拦住。
func TestSFTPWriteContentTooLarge(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	big := strings.Repeat("a", sftpWriteMaxBytes+1)
	text := env.callErr(t, OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/big.txt", "content": big})
	if !strings.Contains(text, "过大") {
		t.Fatalf("超大内容应返回中文体积错误：%s", truncateForLog(text, 200))
	}
	if env.h.called(OpSftpWrite) {
		t.Fatalf("超大内容不应调用宿主")
	}
	// 缺少内容
	text = env.callErr(t, OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/x"})
	if !strings.Contains(text, "content") {
		t.Fatalf("缺少内容应给出中文提示：%s", text)
	}
	// 非法 encoding
	text = env.callErr(t, OpSftpWrite, map[string]any{"sessionId": "s1", "path": "/srv/app/x", "content": "x", "encoding": "hex"})
	if !strings.Contains(text, "encoding") {
		t.Fatalf("非法 encoding 应给出中文提示：%s", text)
	}
}

// ---- 3. 读：list / stat / read / transfers ----

func TestSFTPListShapingAndTruncation(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)

	// ① 小目录：排序（目录在前 + 名字升序）与字段映射（isSymlink / mode）
	entries := []SFTPEntryInfo{
		{Name: "z.txt", Path: "/srv/app/z.txt", Size: 2, Mode: "-rw-r--r--", ModTime: 20},
		{Name: "sub", Path: "/srv/app/sub", IsDir: true, Mode: "drwxr-xr-x", ModTime: 10},
		{Name: "link", Path: "/srv/app/link", IsSymlink: true, Mode: "Lrwxrwxrwx", Size: 4},
	}
	env.h.replies[OpSftpList] = SFTPListPayload{Path: "/srv/app", Entries: entries}
	out := env.callJSON(t, OpSftpList, map[string]any{"sessionId": "s1", "path": "/srv/app"})
	if out["path"] != "/srv/app" {
		t.Fatalf("path 应为宿主解析后的路径：%v", out["path"])
	}
	if out["truncated"] != false || out["count"] != float64(3) {
		t.Fatalf("小目录不应截断：%v", out)
	}
	list, _ := out["entries"].([]any)
	if len(list) != 3 {
		t.Fatalf("应有 3 条：%v", list)
	}
	first, _ := list[0].(map[string]any)
	if first["name"] != "sub" || first["isDir"] != true || first["mode"] != "drwxr-xr-x" {
		t.Fatalf("目录应排在最前且带 mode：%v", first)
	}
	var link map[string]any
	for _, item := range list {
		m, _ := item.(map[string]any)
		if m["name"] == "link" {
			link = m
		}
	}
	if link == nil || link["isSymlink"] != true || link["mode"] != "Lrwxrwxrwx" || link["size"] != float64(4) {
		t.Fatalf("符号链接条目应带 isSymlink / mode / size：%v", link)
	}

	// ② 大目录：截断到 500 条并标注
	big := make([]SFTPEntryInfo, 0, sftpMaxListEntries+10)
	for i := 0; i < sftpMaxListEntries+10; i++ {
		big = append(big, SFTPEntryInfo{Name: "f" + strconv.Itoa(i), Path: "/srv/app/f" + strconv.Itoa(i)})
	}
	env.h.replies[OpSftpList] = SFTPListPayload{Path: "/srv/app", Entries: big}
	out = env.callJSON(t, OpSftpList, map[string]any{"sessionId": "s1", "path": "/srv/app"})
	if out["truncated"] != true {
		t.Fatalf("超过 500 条应标 truncated=true：%v", out["truncated"])
	}
	if out["count"] != float64(sftpMaxListEntries) {
		t.Fatalf("count 应为 %d，实际 %v", sftpMaxListEntries, out["count"])
	}
	list, _ = out["entries"].([]any)
	if len(list) != sftpMaxListEntries {
		t.Fatalf("entries 应被裁剪到 %d 条，实际 %d", sftpMaxListEntries, len(list))
	}
	// 宿主只收到「多要一条」的上限（用于判断截断）
	if got := env.h.lastArgsFor(OpSftpList)["limit"]; got != float64(sftpMaxListEntries+1) {
		t.Fatalf("宿主 limit 应为 %d（多要一条用于判断截断）：%v", sftpMaxListEntries+1, got)
	}
	// 会话参数缺省 → 中文错误
	env.callErr(t, OpSftpList, map[string]any{"path": "/"})
}

func TestSFTPStatAndTransfers(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.replies[OpSftpStat] = SFTPStatPayload{
		Path: "/srv/app/link", IsSymlink: true, Mode: "Lrwxrwxrwx",
		Size: 4, ModTime: 7, Target: "/srv/app/real", TargetIsDir: true,
	}
	out := env.callJSON(t, OpSftpStat, map[string]any{"sessionId": "s1", "path": "/srv/app/link"})
	if out["isSymlink"] != true || out["target"] != "/srv/app/real" || out["targetIsDir"] != true {
		t.Fatalf("stat 应返回符号链接信息：%v", out)
	}
	env.callErr(t, OpSftpStat, map[string]any{"sessionId": "s1"})

	// 传输快照：由既有的 sftp:transfer 进度事件折叠而来
	env.hub.PublishRaw("sftp:transfer:s1", map[string]any{
		"sessionId": "s1", "direction": "upload", "name": "a.bin", "transferred": 10, "total": 100, "done": false,
	})
	env.hub.PublishRaw("sftp:transfer:s1", map[string]any{
		"sessionId": "s1", "direction": "upload", "name": "a.bin", "transferred": 100, "total": 100, "done": true, "error": "",
	})
	env.hub.PublishRaw("sftp:transfer:s2", map[string]any{
		"sessionId": "s2", "direction": "download", "name": "b.bin", "transferred": 5, "total": 5, "done": true, "error": "已取消",
	})
	env.hub.PublishRaw("sftp:transfer:s2", map[string]any{
		"sessionId": "s2", "direction": "download", "name": "c.bin", "transferred": 1, "total": 9, "done": false,
	})
	all := env.callJSON(t, OpSftpTransfers, map[string]any{})
	if all["count"] != float64(3) {
		t.Fatalf("应有 3 条传输快照，实际 %v", all["count"])
	}
	filtered := env.callJSON(t, OpSftpTransfers, map[string]any{"sessionId": "s2"})
	if filtered["count"] != float64(2) {
		t.Fatalf("按会话过滤应剩 2 条，实际 %v", filtered["count"])
	}
	items, _ := all["transfers"].([]any)
	states := map[string]string{}
	for _, item := range items {
		m, _ := item.(map[string]any)
		states[m["id"].(string)] = m["state"].(string)
	}
	if states["s1|upload|a.bin"] != "done" {
		t.Fatalf("upload 的最后状态应为 done：%v", states)
	}
	if states["s2|download|b.bin"] != "cancelled" {
		t.Fatalf("error=已取消 应折叠成 cancelled：%v", states)
	}
	if states["s2|download|c.bin"] != "running" {
		t.Fatalf("done=false 应为 running：%v", states)
	}
}

func TestParseTransferIDMatrix(t *testing.T) {
	ok := []struct {
		in        string
		sess      string
		direction string
		name      string
	}{
		{"s1|upload|a.bin", "s1", "upload", "a.bin"},
		{"tab-1|download|weird|name.txt", "tab-1", "download", "weird|name.txt"},
	}
	for _, c := range ok {
		s, d, n, err := ParseTransferID(c.in)
		if err != nil || s != c.sess || d != c.direction || n != c.name {
			t.Fatalf("ParseTransferID(%q) = %q/%q/%q/%v", c.in, s, d, n, err)
		}
	}
	for _, bad := range []string{"", "s1", "s1|upload", "s1|move|x", "|upload|x", "s1|upload|"} {
		if _, _, _, err := ParseTransferID(bad); err == nil {
			t.Fatalf("ParseTransferID(%q) 应报错", bad)
		}
	}
}

func TestSFTPCancelAndSyncCwd(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	env.h.replies[OpSftpCancel] = SFTPMutationPayload{OK: true}
	env.h.replies[OpSftpSyncCwd] = SFTPMutationPayload{OK: true, Path: "/srv/app"}

	out := env.callJSON(t, OpSftpCancel, env.withToken(t, OpSftpCancel, map[string]any{"transferId": "s1|upload|a.bin"}))
	if out["ok"] != true || out["direction"] != "upload" {
		t.Fatalf("cancel 应解析出方向与文件名：%v", out)
	}
	sent := env.h.lastArgsFor(OpSftpCancel)
	if sent["sessionId"] != "s1" || sent["direction"] != "upload" || sent["name"] != "a.bin" {
		t.Fatalf("宿主应收到 sessionId/direction/name：%v", sent)
	}
	env.callErr(t, OpSftpCancel, map[string]any{"transferId": "bad-id"})

	// syncCwd：走白名单，且只推同步事件（不向终端键入任何字符）
	env.callJSON(t, OpSftpSyncCwd, env.withToken(t, OpSftpWrite, map[string]any{
		"sessionId": "s1", "remotePath": "/srv/app/sub",
	}))
	if env.h.called(OpTerminalInput) {
		t.Fatalf("sftp.syncCwd 不应向终端写入任何数据")
	}
	sentSync := env.h.lastArgsFor(OpSftpSyncCwd)
	if sentSync["remotePath"] != "/srv/app/sub" {
		t.Fatalf("宿主应收到归一化后的路径：%v", sentSync)
	}
}

func TestSFTPReadTextBase64AndTruncation(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)

	content := "hello 世界\n"
	readReply := SFTPReadPayload{Path: "/etc/hostname", Data: []byte(content), Size: int64(len(content)), EOF: true}
	env.h.replies[OpSftpRead] = readReply

	out := env.callJSON(t, OpSftpRead, map[string]any{"sessionId": "s1", "path": "/etc/hostname"})
	if out["content"] != content {
		t.Fatalf("text 编码应返回原文：%v", out["content"])
	}
	if out["encoding"] != "text" || out["truncated"] != false {
		t.Fatalf("encoding/truncated 不正确：%v", out)
	}
	if out["bytes"] != float64(len(content)) {
		t.Fatalf("bytes 应为 %d：%v", len(content), out["bytes"])
	}
	sum := sha256Hex([]byte(content))
	if out["sha256"] != sum {
		t.Fatalf("sha256 应为 %s，实际 %v", sum, out["sha256"])
	}
	if _, has := out["base64"]; has {
		t.Fatalf("text 编码不应返回 base64 字段")
	}

	// base64 编码：二进制内容原样返回
	bin := []byte{0x00, 0x01, 0xff, 0xfe}
	env.h.replies[OpSftpRead] = SFTPReadPayload{Path: "/bin/x", Data: bin, Size: 4, EOF: false}
	out = env.callJSON(t, OpSftpRead, map[string]any{"sessionId": "s1", "path": "/bin/x", "encoding": "base64"})
	if out["base64"] != base64.StdEncoding.EncodeToString(bin) {
		t.Fatalf("base64 编码应返回 base64 字段：%v", out)
	}
	if out["truncated"] != true {
		t.Fatalf("EOF=false 应标 truncated=true：%v", out)
	}
	if out["sha256"] != sha256Hex(bin) {
		t.Fatalf("base64 场景的 sha256 不对：%v", out["sha256"])
	}

	// 分片读：offset 传给宿主
	env.h.replies[OpSftpRead] = SFTPReadPayload{Path: "/big", Data: []byte("xx"), Size: 100, EOF: false}
	env.callJSON(t, OpSftpRead, map[string]any{"sessionId": "s1", "path": "/big", "offset": 50, "maxBytes": 2})
	sent := env.h.lastArgsFor(OpSftpRead)
	if sent["offset"] != float64(50) || sent["maxBytes"] != float64(2) {
		t.Fatalf("offset / maxBytes 应原样传给宿主：%v", sent)
	}

	// 目录 → 中文错误
	env.h.replies[OpSftpRead] = SFTPReadPayload{Path: "/srv", IsDir: true}
	text := env.callErr(t, OpSftpRead, map[string]any{"sessionId": "s1", "path": "/srv"})
	if !strings.Contains(text, "目录") {
		t.Fatalf("读取目录应给出中文提示：%s", text)
	}
}

func TestSFTPReadArgValidation(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"path": "/x"}, "sessionId"},
		{map[string]any{"sessionId": "s1"}, "path"},
		{map[string]any{"sessionId": "s1", "path": "/x", "encoding": "utf16"}, "encoding"},
		{map[string]any{"sessionId": "s1", "path": "/x", "maxBytes": 0}, "maxBytes"},
		{map[string]any{"sessionId": "s1", "path": "/x", "maxBytes": sftpReadMaxBytes + 1}, "maxBytes"},
		{map[string]any{"sessionId": "s1", "path": "/x", "offset": -1}, "offset"},
	}
	for _, c := range cases {
		text := env.callErr(t, OpSftpRead, c.args)
		if !strings.Contains(text, c.want) {
			t.Fatalf("参数 %v 的错误应提到 %q：%s", c.args, c.want, text)
		}
	}
}

// ---- 4. 终端自动化 ----

func TestTerminalRunExpectMatched(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		// 模拟远端回显：命令行 + 结果 + ANSI 颜色 + CRLF
		publishOutput(env.hub, id, "\x1b[32mecho MCP-M8-OK\x1b[0m\r\nMCP-M8-OK\r\n$ ")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "echo MCP-M8-OK", "expect": "MCP-M8-OK", "timeoutMs": 3000,
	})
	if out["matched"] != true || out["reason"] != "matched" {
		t.Fatalf("expect 应命中：%v", out)
	}
	body, _ := out["output"].(string)
	if !strings.Contains(body, "MCP-M8-OK") {
		t.Fatalf("output 应含输出：%q", body)
	}
	if strings.Contains(body, "\x1b") {
		t.Fatalf("output 应去掉 ANSI 转义：%q", body)
	}
	if strings.Contains(body, "\r") {
		t.Fatalf("output 不应含孤立的 \\r：%q", body)
	}
	// 发送的 data 必须自动补 \r，且未开启包裹时不能改动用户命令
	sent := env.h.lastArgsFor(OpTerminalInput)
	if sent["data"] != "echo MCP-M8-OK\r" {
		t.Fatalf("发送内容应为 command+\\r：%q", sent["data"])
	}
	if out["wrapped"] != false {
		t.Fatalf("未开启 wrapExitMarker 时 wrapped 应为 false：%v", out["wrapped"])
	}
	// 审计：终端自动化也要留记录
	recs := env.audit.Recent(5, "", "")
	if len(recs) == 0 || recs[0].Tool != OpTerminalRun {
		t.Fatalf("terminal.run 应留审计记录：%+v", recs)
	}
}

func TestTerminalRunExitMarkerWrap(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, "done\r\n\r\n__DING_EXIT__3\r\n$ ")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "false", "wrapExitMarker": true,
		"expect": "__DING_EXIT__[0-9]", "timeoutMs": 3000,
	})
	if out["wrapped"] != true {
		t.Fatalf("wrapped 应为 true：%v", out)
	}
	if out["exitMarker"] != float64(3) {
		t.Fatalf("应解析出退出码 3：%v", out["exitMarker"])
	}
	sent := env.h.lastArgsFor(OpTerminalInput)
	data, _ := sent["data"].(string)
	if !strings.Contains(data, "printf") || !strings.Contains(data, terminalMarkerPrefix) {
		t.Fatalf("wrapExitMarker 应把命令包成 printf 形式：%q", data)
	}
	if !strings.HasPrefix(data, "false;") {
		t.Fatalf("原文命令应保留在包裹后的命令里：%q", data)
	}
	if !strings.HasSuffix(data, "\r") {
		t.Fatalf("包裹后的命令也要以 \\r 结尾：%q", data)
	}
	if wc, _ := out["wrappedCommand"].(string); wc != data {
		t.Fatalf("返回里的 wrappedCommand 应如实说明实际执行的命令：%q vs %q", wc, data)
	}
}

// wrapExitMarker 且未给 expect：自动等到退出码标记（不靠「输出静默」猜）。
func TestTerminalRunExitMarkerAutoExpect(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		time.Sleep(20 * time.Millisecond)
		publishOutput(env.hub, id, "out\r\n__DING_EXIT__0\r\n$ ")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "true", "wrapExitMarker": true, "timeoutMs": 3000,
	})
	if out["matched"] != true || out["reason"] != "matched" {
		t.Fatalf("wrapExitMarker 未给 expect 时应等到退出码标记：%v", out)
	}
	if out["exitMarker"] != float64(0) {
		t.Fatalf("应解析出退出码 0：%v", out["exitMarker"])
	}
	// 命令行回显里是 __DING_EXIT__%s（没有数字），不应被自动等待的正则命中
	echoOnly := newSFTPEnv(t)
	echoOnly.h.onInput = func(id, _ string) {
		publishOutput(echoOnly.hub, id, "true; printf '\\n__DING_EXIT__%s\\n' \"$?\"\r\n")
	}
	out2 := echoOnly.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "true", "wrapExitMarker": true, "timeoutMs": 300,
	})
	if out2["matched"] != false || out2["reason"] != "timeout" {
		t.Fatalf("只有命令行回显时不应命中退出码标记：%v", out2)
	}
	if code, has := out2["exitMarker"]; has && code != nil {
		t.Fatalf("回显阶段不应有退出码：%v", code)
	}
}

func TestTerminalRunExpectTimeoutIsNotProtocolError(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, "some partial output\r\n")
	}
	start := time.Now()
	isErr, text, rpcErr := env.call(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "sleep 99", "expect": "__NOPE__", "timeoutMs": 300,
	})
	if rpcErr != "" {
		t.Fatalf("超时不应是 JSON-RPC 错误：%s", rpcErr)
	}
	if isErr {
		t.Fatalf("超时不应是 isError（应返回 matched:false + 输出）：%s", text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("解析返回失败：%v", err)
	}
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("超时应返回 matched=false / reason=timeout：%v", out)
	}
	if body, _ := out["output"].(string); !strings.Contains(body, "some partial output") {
		t.Fatalf("超时也要返回已捕获的输出：%v", out)
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "不是协议错误") {
		t.Fatalf("超时应给中文说明：%v", hint)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("超时用例不应提前返回：%v", elapsed)
	}
}

func TestTerminalRunIdleFinishWithoutExpect(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, "MCP-M8-IDLE\r\n")
	}
	start := time.Now()
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "uname -a", "timeoutMs": 5000,
	})
	if out["reason"] != "idle" {
		t.Fatalf("未给 expect 时应等到输出静默：%v", out)
	}
	if out["matched"] != false {
		t.Fatalf("未给 expect 时 matched 应为 false：%v", out)
	}
	if body, _ := out["output"].(string); !strings.Contains(body, "MCP-M8-IDLE") {
		t.Fatalf("output 应含静默前的输出：%v", out)
	}
	if elapsed := time.Since(start); elapsed < terminalIdleMS*time.Millisecond {
		t.Fatalf("静默判定至少等待 %dms：%v", terminalIdleMS, elapsed)
	}
}

// 同一会话上并发调用：第二个**直接拒绝**（不排队）。
func TestTerminalRunConcurrentRejected(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	env.h.onInput = func(id, _ string) {
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	done := make(chan string, 1)
	go func() {
		_, text, _ := callTool(t, env.srv, OpTerminalRun, map[string]any{
			"id": "tab-1", "command": "slow", "expect": "__NEVER__", "timeoutMs": 3000,
		})
		done <- text
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("第一次调用未进入发送阶段")
	}
	// 第一个调用正占着 tab-1：第二个必须立刻被拒绝
	text := env.callErr(t, OpTerminalRun, map[string]any{"id": "tab-1", "command": "ls"})
	if !strings.Contains(text, "只允许一个") || !strings.Contains(text, "不排队") {
		t.Fatalf("并发拒绝的中文原因应说明串行化行为：%s", text)
	}
	text = env.callErr(t, OpTerminalExpect, map[string]any{"id": "tab-1", "pattern": "x"})
	if !strings.Contains(text, "terminal.expect") {
		t.Fatalf("terminal.expect 与 terminal.run 共用同一套串行化：%s", text)
	}
	// 另一个会话不受影响（先等第一次结束，避免测试变量竞争）
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("第一次调用未在超时前结束")
	}
	env.h.onInput = func(id, _ string) { publishOutput(env.hub, id, "ok\r\n") }
	if out := env.callJSON(t, OpSftpList, map[string]any{"sessionId": "tab-2"}); out == nil {
		t.Fatalf("释放后应能正常调用")
	}
}

func TestTerminalExpectSinceMarker(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	go func() {
		time.Sleep(30 * time.Millisecond)
		publishOutput(env.hub, "tab-1", "line-1\r\nline-2\r\nPROMPT$ ")
	}()
	out := env.callJSON(t, OpTerminalExpect, map[string]any{
		"id": "tab-1", "pattern": "line-2", "timeoutMs": 2000,
	})
	if out["matched"] != true {
		t.Fatalf("expect 应命中：%v", out)
	}
	if body, _ := out["output"].(string); !strings.Contains(body, "line-1") {
		t.Fatalf("未传 sinceMarker 时应返回本次捕获的全文：%v", out)
	}
	if out["marker"] == "" || out["marker"] == nil {
		t.Fatalf("应返回 marker 供下次跳过：%v", out)
	}
	marker, _ := out["marker"].(string)
	// 再等一次：同样的输出（模拟重复提示符）应被 sinceMarker 剪掉
	// （必须在订阅之后才发事件，否则会被 Hub 直接丢掉 —— 与真实链路一致）
	go func() {
		time.Sleep(30 * time.Millisecond)
		publishOutput(env.hub, "tab-1", marker+"fresh\r\n")
	}()
	out2 := env.callJSON(t, OpTerminalExpect, map[string]any{
		"id": "tab-1", "pattern": "fresh", "timeoutMs": 2000, "sinceMarker": marker,
	})
	body, _ := out2["output"].(string)
	if strings.Contains(body, marker) {
		t.Fatalf("sinceMarker 应剪掉重复内容：%q", body)
	}
	if !strings.Contains(body, "fresh") {
		t.Fatalf("sinceMarker 之后的新内容应保留：%q", body)
	}
}

func TestTerminalArgValidation(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{OpTerminalRun, map[string]any{"command": "ls"}, "id"},
		{OpTerminalRun, map[string]any{"id": "tab-1"}, "command"},
		{OpTerminalRun, map[string]any{"id": "tab-1", "command": "ls", "expect": "(["}, "正则"},
		{OpTerminalRun, map[string]any{"id": "tab-1", "command": "ls", "timeoutMs": 10}, "timeoutMs"},
		{OpTerminalRun, map[string]any{"id": "tab-1", "command": "ls", "maxBytes": 99999999}, "maxBytes"},
		{OpTerminalExpect, map[string]any{"id": "tab-1"}, "pattern"},
		{OpTerminalExpect, map[string]any{"id": "tab-1", "pattern": "(["}, "正则"},
	}
	for _, c := range cases {
		text := env.callErr(t, c.tool, c.args)
		if !strings.Contains(text, c.want) {
			t.Fatalf("%s %v 的错误应提到 %q：%s", c.tool, c.args, c.want, text)
		}
	}
	// 终端不存在（宿主报错）时原样返回中文原因
	env.h.errs[OpTerminalInput] = errTerminalMissing
	text := env.callErr(t, OpTerminalRun, map[string]any{"id": "tab-9", "command": "ls"})
	if !strings.Contains(text, "未找到终端") {
		t.Fatalf("宿主错误应带上下文返回：%s", text)
	}
}

// 单次输出超过 maxBytes：截断 + 标记 + 中文提示（体积有界）。
func TestTerminalOutputTruncated(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	big := strings.Repeat("A", 4096)
	env.h.onInput = func(id, _ string) { publishOutput(env.hub, id, big) }
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "cat big", "maxBytes": 1024, "timeoutMs": 2000,
	})
	if out["truncated"] != true {
		t.Fatalf("应标 truncated=true：%v", out)
	}
	if out["outputBytes"] != float64(len(big)) {
		t.Fatalf("outputBytes 应为截断前的字节数：%v", out["outputBytes"])
	}
	if body, _ := out["output"].(string); len(body) != 1024 {
		t.Fatalf("output 应被截断到 1024 字节，实际 %d", len(body))
	}
	if hint, _ := out["truncatedHint"].(string); hint == "" {
		t.Fatalf("应给出中文截断提示：%v", out)
	}
}

// ---- 5. 资源与审计脱敏 ----

func TestSFTPResources(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.replies[OpTerminals] = []any{
		map[string]any{"clientId": "tab-1", "title": "srv"},
	}
	env.h.replies[OpSftpList] = SFTPListPayload{Path: "/home/u", Entries: []SFTPEntryInfo{
		{Name: "a.txt", Path: "/home/u/a.txt", Size: 3, Mode: "-rw-r--r--"},
	}}
	env.h.replies[OpSessions] = map[string]any{"tabs": []any{
		map[string]any{"clientId": "tab-1", "sessionId": "tab-1", "sftpPath": "/home/u"},
	}}
	env.hub.PublishRaw("sftp:transfer:tab-1", map[string]any{
		"sessionId": "tab-1", "direction": "upload", "name": "a.bin", "transferred": 1, "total": 2, "done": false,
	})

	resp := env.srv.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "resources/list",
	})
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), TransfersResourceURI) || !strings.Contains(string(raw), "sftp://tab-1/cwd") {
		t.Fatalf("resources/list 应包含 M8 资源：%s", truncateForLog(string(raw), 300))
	}

	text, err := env.srv.mcpReadResource(context.Background(), TransfersResourceURI)
	if err != nil {
		t.Fatalf("读 transfers://current 失败：%v", err)
	}
	if !strings.Contains(text, "tab-1|upload|a.bin") {
		t.Fatalf("资源内容应含传输快照：%s", truncateForLog(text, 300))
	}

	text, err = env.srv.mcpReadResource(context.Background(), SftpCwdResourceURI("tab-1"))
	if err != nil {
		t.Fatalf("读 sftp://tab-1/cwd 失败：%v", err)
	}
	if !strings.Contains(text, "/home/u") || !strings.Contains(text, "a.txt") {
		t.Fatalf("cwd 资源应含当前路径与条目摘要：%s", truncateForLog(text, 300))
	}
	// 目录读失败时不报协议错误，而是把中文原因放进内容
	env.h.errs[OpSftpList] = errTerminalMissing
	text, err = env.srv.mcpReadResource(context.Background(), SftpCwdResourceURI("tab-1"))
	if err != nil {
		t.Fatalf("目录读失败也不应让资源读失败：%v", err)
	}
	if !strings.Contains(text, "未找到终端") {
		t.Fatalf("失败原因应写进资源内容：%s", truncateForLog(text, 200))
	}
	// 未知 uri
	if _, err := env.srv.mcpReadResource(context.Background(), "sftp://tab-1/other"); err == nil {
		t.Fatalf("非法 sftp 资源 uri 应报错")
	}
}

func TestSFTPAuditSafeArgsFolding(t *testing.T) {
	long := strings.Repeat("x", 1024)
	got := sftpAuditSafeArgs(OpSftpWrite, map[string]any{
		"sessionId": "s1", "path": "/a", "content": long, "base64": long,
	})
	if s, _ := got["content"].(string); !strings.Contains(s, "已省略") {
		t.Fatalf("超长 content 应折叠成摘要：%v", got["content"])
	}
	if s, _ := got["base64"].(string); !strings.Contains(s, "已省略") {
		t.Fatalf("超长 base64 应折叠成摘要：%v", got["base64"])
	}
	if got["path"] != "/a" {
		t.Fatalf("普通参数应原样保留：%v", got)
	}
	// 端到端：审计记录里不应出现完整内容
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.scope.set("/srv/app")
	env.h.replies[OpSftpWrite] = SFTPWritePayload{Path: "/srv/app/a.txt", Bytes: 1024}
	env.callJSON(t, OpSftpWrite, env.withToken(t, OpSftpWrite, map[string]any{
		"sessionId": "s1", "path": "/srv/app/a.txt", "content": long,
		"base64": base64.StdEncoding.EncodeToString([]byte(long)),
	}))
	recs := env.audit.Recent(1, "", "")
	if len(recs) != 1 {
		t.Fatalf("应有 1 条审计：%+v", recs)
	}
	if strings.Contains(recs[0].Args, long) {
		t.Fatalf("审计参数不应包含完整内容：%s", truncateForLog(recs[0].Args, 200))
	}
	if !strings.Contains(recs[0].Args, "已省略") {
		t.Fatalf("审计参数应是折叠后的摘要：%s", truncateForLog(recs[0].Args, 200))
	}
}

// ---- 小工具 ----

// errTerminalMissing 模拟前端桥「未找到终端」的中文错误。
var errTerminalMissing = errors.New("未找到终端: tab-9")

// anyStr 从 JSON 形状的 map 里取字符串（元组断言，取不到返回空串）。
func anyStr(v any) string {
	s, _ := v.(string)
	return s
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
