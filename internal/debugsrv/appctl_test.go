package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
)

// M7 应用控制面的用例：能力位 / 确认 action 登记、参数校验、明文脱敏、schema 覆盖、命令规则。
//
// 全部用替身（appctlStubHandler + fakeGate），不连 SSH、不碰真实存储。

// appctlStubHandler 按 op 返回预设结果，并记录调用与入参。
type appctlStubHandler struct {
	mu      sync.Mutex
	calls   []string
	args    []map[string]any
	replies map[string]any
	errs    map[string]error
	// replyFunc 优先级最高：按 op + 入参动态返回（用于「同一 op 不同入参不同结果」的用例）。
	replyFunc func(op string, args map[string]any) (any, error)
}

func (h *appctlStubHandler) Handle(_ context.Context, op string, raw json.RawMessage) (any, error) {
	var m map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	h.mu.Lock()
	h.calls = append(h.calls, op)
	h.args = append(h.args, m)
	fn := h.replyFunc
	h.mu.Unlock()
	if fn != nil {
		return fn(op, m)
	}
	if h.errs != nil {
		if err, bad := h.errs[op]; bad {
			return nil, err
		}
	}
	if h.replies != nil {
		if v, ok := h.replies[op]; ok {
			return v, nil
		}
	}
	return map[string]any{"op": op}, nil
}

func (h *appctlStubHandler) called(op string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.calls {
		if c == op {
			return true
		}
	}
	return false
}

func (h *appctlStubHandler) lastArgs() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.args) == 0 {
		return nil
	}
	return h.args[len(h.args)-1]
}

// newAppctlTestServer 组装一个带替身 Handler 的调试服务。
func newAppctlTestServer(t *testing.T, gate Gate, replies map[string]any, errs map[string]error) (*testServer, *appctlStubHandler) {
	t.Helper()
	h := &appctlStubHandler{replies: replies, errs: errs}
	return newAppctlTestServerWith(t, gate, h), h
}

// newAppctlTestServerWith 用给定的替身 Handler 组装调试服务。
func newAppctlTestServerWith(t *testing.T, gate Gate, h Handler) *testServer {
	t.Helper()
	ts := &testServer{exec: &fakeExecutor{}, audit: NewAuditLog()}
	if fg, ok := gate.(*fakeGate); ok {
		ts.gate = fg
	}
	ts.srv = New(Options{
		Handler:  h,
		Hub:      NewHub(),
		Gate:     gate,
		Audits:   ts.audit,
		Confirms: NewConfirmManager(DefaultConfirmTTL),
		Executor: &fakeExecutor{},
	})
	return ts
}

// ---- 1. 能力位 / 确认 action 登记 ----

// 规格表：工具 → 能力位 → 确认 action（空 action 表示不需要 token）。
var appctlWantRegistry = []struct {
	tool   string
	cap    Capability
	action string
}{
	{OpServersList, CapRead, ""},
	{OpServersGet, CapRead, ""},
	{OpServersCreate, CapConfigWrite, "servers.create"},
	{OpServersUpdate, CapConfigWrite, "servers.update"},
	{OpServersDelete, CapConfigWrite, "servers.delete"},
	{OpServersDuplicate, CapConfigWrite, "servers.create"},
	{OpServersMoveGroup, CapConfigWrite, "servers.update"},
	{OpServersExport, CapRead, ""},
	{OpServersImport, CapConfigWrite, "servers.create"},
	{OpCredentialsList, CapRead, ""},
	{OpCredentialsCreate, CapSecretsWrite, "credentials.create"},
	{OpCredentialsUpdate, CapSecretsWrite, "credentials.update"},
	{OpCredentialsDelete, CapSecretsWrite, "credentials.delete"},
	{OpCredentialsAttach, CapConfigWrite, "servers.update"},
	{OpTunnelsList, CapRead, ""},
	{OpTunnelsCreate, CapConfigWrite, "tunnels.create"},
	{OpTunnelsUpdate, CapConfigWrite, "tunnels.update"},
	{OpTunnelsDelete, CapConfigWrite, "tunnels.delete"},
	{OpTunnelsStart, CapConfigWrite, "tunnels.start"},
	{OpTunnelsStop, CapConfigWrite, "tunnels.stop"},
	{OpSettingsSchema, CapRead, ""},
	{OpSettingsReset, CapConfigWrite, "settings.reset"},
	{OpAppCommands, CapRead, ""},
	{OpAppCommand, CapRead, ""}, // 按命令判定，见 appctlAppCommand
	{OpAppQuit, CapLifecycle, "app.quit"},
}

func TestAppctlCapabilityAndConfirmRegistration(t *testing.T) {
	ensureAllRegistrations(t)
	if errs := RegistrationErrors(); len(errs) != 0 {
		t.Fatalf("注册期不应有错误：%v", errs)
	}
	for _, want := range appctlWantRegistry {
		caps, ok := ToolCapabilities(want.tool)
		if !ok {
			t.Fatalf("工具 %s 没有登记能力位", want.tool)
		}
		found := false
		for _, c := range caps {
			if c == want.cap {
				found = true
			}
		}
		if !found {
			t.Fatalf("工具 %s 的能力位 = %v，期望包含 %s", want.tool, caps, want.cap)
		}
		action, need := ToolConfirmAction(want.tool)
		if want.action == "" {
			if need {
				t.Fatalf("工具 %s 不应要求两段式确认，实际 action=%q", want.tool, action)
			}
			continue
		}
		if !need || action != want.action {
			t.Fatalf("工具 %s 的确认 action = %q/%v，期望 %q/true", want.tool, action, need, want.action)
		}
		if !ActionNeedsConfirm(want.action) {
			t.Fatalf("action %s 应进入可确认清单（否则 confirm.prepare 会拒绝）", want.action)
		}
	}
	// 全部工具都必须有工具条目（即出现在 tools/list 里）
	if err := appctlCheckToolEntriesRegistered(); err != nil {
		t.Fatalf("工具条目与能力登记不一致：%v", err)
	}
	// 规格里的 action 名（域.动词）都在可确认清单里
	confirmable := map[string]bool{}
	for _, a := range ConfirmableActions() {
		confirmable[a] = true
	}
	for _, a := range []string{
		"servers.create", "servers.update", "servers.delete",
		"credentials.create", "credentials.update", "credentials.delete",
		"tunnels.create", "tunnels.update", "tunnels.delete", "tunnels.start", "tunnels.stop",
		"settings.reset", "app.quit", "app.command",
	} {
		if !confirmable[a] {
			t.Fatalf("ConfirmableActions 缺少 %s：%v", a, ConfirmableActions())
		}
	}
}

// 工具总数不再写死：改为注册表驱动（每个登记过能力位的工具都必须出现在 tools/list）。
func TestAppctlToolsListedAndRegistryDriven(t *testing.T) {
	ensureAllRegistrations(t)
	ts, _ := newAppctlTestServer(t, &fakeGate{}, nil, nil)
	listed, tools := toolsListFromMCP(t, ts.srv)
	if len(tools) == 0 {
		t.Fatalf("tools/list 为空")
	}
	assertRegistryToolsListed(t, listed)

	for _, name := range appctlToolNames {
		if !listed[name] {
			t.Fatalf("tools/list 里缺少应用控制面工具 %s", name)
		}
	}
	// 读类工具的能力要求行应说明「永远允许」；写类工具应提示 confirm.prepare
	for _, tool := range tools {
		if !strings.Contains(tool.Description, "能力要求：") {
			t.Fatalf("工具 %s 的描述缺少「能力要求」行", tool.Name)
		}
	}
	for _, tc := range []struct{ tool, want string }{
		{OpAppQuit, "confirm.prepare"},
		{OpServersDelete, "confirm.prepare"},
		{OpServersImport, "confirm.prepare"},
		{OpSettingsReset, "confirm.prepare"},
		{OpTunnelsCreate, "confirm.prepare"},
		{OpServersList, "永远允许"},
		{OpAppCommands, "永远允许"},
	} {
		desc := ""
		for _, tool := range tools {
			if tool.Name == tc.tool {
				desc = tool.Description
			}
		}
		if !strings.Contains(desc, tc.want) {
			t.Fatalf("工具 %s 的描述应含 %q：%s", tc.tool, tc.want, desc)
		}
	}
}

// toolsListFromMCP / toolNamesOf / assertRegistryToolsListed 已归位到 registry_test.go（M9 收尾）。

// ---- 2. 参数校验（中文 ErrBadInput）----

func TestAppctlBadInput(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{OpServersGet, nil, "缺少 id"},
		{OpServersCreate, nil, "缺少 node"},
		{OpServersCreate, map[string]any{"node": map[string]any{"host": "h", "user": "u", "port": 70000.0}}, "port"},
		{OpServersCreate, map[string]any{"node": map[string]any{"host": "h", "user": "u", "authType": "keyboard"}}, "authType"},
		{OpServersCreate, map[string]any{"node": map[string]any{"host": "h"}}, "node.user"},
		{OpServersUpdate, map[string]any{"id": "s1"}, "缺少 node"},
		{OpServersUpdate, map[string]any{"node": map[string]any{"name": "x"}}, "缺少 id"},
		{OpServersDelete, nil, "缺少 id"},
		{OpServersDuplicate, nil, "缺少 id"},
		{OpServersMoveGroup, map[string]any{"group": "g"}, "缺少 ids"},
		{OpServersImport, nil, "缺少 json"},
		{OpServersImport, map[string]any{"json": "{}"}, "没有 servers"},
		{OpServersImport, map[string]any{"json": `[{"host":"h"}]`}, "校验失败"},
		{OpServersImport, map[string]any{"json": "[1,2]", "mode": "replace"}, "校验失败"},
		{OpServersImport, map[string]any{"json": `[{"host":"h","user":"u"}]`, "mode": "upsert"}, "非法 mode"},
		{OpCredentialsCreate, map[string]any{"name": "n"}, "非法 type"},
		{OpCredentialsCreate, map[string]any{"name": "n", "type": "password"}, "缺少 content"},
		{OpCredentialsCreate, map[string]any{"type": "password", "content": "c"}, "缺少 name"},
		{OpCredentialsUpdate, map[string]any{"id": "c1"}, "至少要给"},
		{OpCredentialsUpdate, nil, "缺少 id"},
		{OpCredentialsDelete, nil, "缺少 id"},
		{OpCredentialsAttach, map[string]any{"serverId": "s1"}, "缺少 credentialId"},
		{OpCredentialsAttach, map[string]any{"credentialId": "c1"}, "缺少 serverId"},
		{OpTunnelsDelete, nil, "缺少 id"},
		{OpTunnelsStart, nil, "缺少 id"},
		{OpTunnelsStop, nil, "缺少 id"},
		{OpTunnelsCreate, map[string]any{"type": "local", "listenPort": 8080.0}, "需要 serverId 或 sessionId"},
		{OpTunnelsCreate, map[string]any{"serverId": "s1", "type": "socks5", "listenPort": 1080.0}, "非法 type"},
		{OpTunnelsCreate, map[string]any{"serverId": "s1", "type": "local"}, "缺少 listenPort"},
		{OpTunnelsCreate, map[string]any{"serverId": "s1", "type": "local", "listenPort": 8080.0}, "需要 targetPort"},
		{OpTunnelsCreate, map[string]any{"serverId": "s1", "type": "dynamic", "listenPort": 99999.0}, "越界"},
		{OpTunnelsUpdate, map[string]any{"type": "local", "listenPort": 8080.0, "targetPort": 80.0}, "缺少 id"},
		{OpTunnelsUpdate, map[string]any{"id": "t1", "serverId": "s1", "type": "local", "listenPort": 8080.0}, "需要 targetPort"},
		{OpSettingsReset, map[string]any{"paths": []any{1.0}}, "paths"},
		{OpSettingsReset, map[string]any{"paths": []any{""}}, "空字符串"},
		// 显式空数组不得被当成「整体恢复默认」（方向性错误代价太大）
		{OpSettingsReset, map[string]any{"paths": []any{}}, "不能是空数组"},
		{OpAppCommand, nil, "缺少 id"},
	}
	for _, tc := range cases {
		ts, h := newAppctlTestServer(t, &fakeGate{}, nil, nil)
		_, err := ts.srv.appctlDispatch(context.Background(), tc.tool, tc.args)
		if err == nil {
			t.Fatalf("%s%v 应当报错（期望含 %q）", tc.tool, tc.args, tc.want)
		}
		if !errors.Is(err, ErrBadInput) {
			t.Fatalf("%s 的错误应为 ErrBadInput，实际 %v", tc.tool, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s 的错误应含 %q，实际：%v", tc.tool, tc.want, err)
		}
		if h.called(tc.tool) {
			// 参数非法时不应落到宿主（params 校验在 dispatch 之前完成）
			t.Fatalf("%s 参数非法时不应调用宿主 op", tc.tool)
		}
	}
}

// servers.update 允许「只给要改的字段」（局部更新），但仍拒绝明显非法的值。
func TestAppctlServersUpdatePartialNode(t *testing.T) {
	ts, h := newAppctlTestServer(t, &fakeGate{}, map[string]any{
		OpServersUpdate: map[string]any{"server": map[string]any{"id": "s1", "name": "n2", "host": "h", "user": "u"}},
	}, nil)
	if _, err := ts.srv.appctlDispatch(context.Background(), OpServersUpdate,
		map[string]any{"id": "s1", "node": map[string]any{"name": "n2"}}); err != nil {
		t.Fatalf("只改名字应该被接受：%v", err)
	}
	if !h.called(OpServersUpdate) {
		t.Fatalf("servers.update 应落到宿主 op")
	}
	// 非法值仍要拒绝
	_, err := ts.srv.appctlDispatch(context.Background(), OpServersUpdate,
		map[string]any{"id": "s1", "node": map[string]any{"port": 70000.0}})
	if err == nil || !errors.Is(err, ErrBadInput) || !strings.Contains(err.Error(), "port") {
		t.Fatalf("越界端口应被拒：%v", err)
	}
	_, err = ts.srv.appctlDispatch(context.Background(), OpServersUpdate,
		map[string]any{"id": "s1", "node": map[string]any{"authType": "keyboard"}})
	if err == nil || !strings.Contains(err.Error(), "authType") {
		t.Fatalf("非法 authType 应被拒：%v", err)
	}
}

// 工具名非法 / 未登记时直接报中文错误。
func TestAppctlUnknownTool(t *testing.T) {
	ts, _ := newAppctlTestServer(t, &fakeGate{}, nil, nil)
	if _, err := ts.srv.appctlDispatch(context.Background(), "servers.nope", nil); err == nil {
		t.Fatalf("未知工具应报错")
	}
}

// ---- 3. 服务器：脱敏 / 明文标注 / includeSecrets 能力位 ----

func exportStubServers() map[string]any {
	return map[string]any{
		"count": 1,
		"servers": []any{
			map[string]any{
				"id": "srv-1", "name": "生产", "host": "10.0.0.1", "port": 22.0, "user": "root",
				"authType": "password", "password": "hunter2-secret", "keyContent": "-----BEGIN KEY-----secret",
			},
		},
	}
}

func TestAppctlServersExportRedactsSecretsByDefault(t *testing.T) {
	ts, h := newAppctlTestServer(t, &fakeGate{}, map[string]any{OpServersList: exportStubServers()}, nil)
	res, err := ts.srv.appctlDispatch(context.Background(), OpServersExport, nil)
	if err != nil {
		t.Fatalf("servers.export 失败：%v", err)
	}
	if !h.called(OpServersList) {
		t.Fatalf("servers.export 应通过 servers.list 取数")
	}
	text, _ := json.Marshal(res)
	if strings.Contains(string(text), "hunter2-secret") || strings.Contains(string(text), "BEGIN KEY") {
		t.Fatalf("默认导出不得出现明文密钥：%s", text)
	}
	// 导出文本本身（json 字段）也必须脱敏
	inner, _ := res["json"].(string)
	if strings.Contains(inner, "hunter2-secret") || strings.Contains(inner, "-----BEGIN KEY-----secret") {
		t.Fatalf("导出 JSON 文本里出现了明文：%s", inner)
	}
	if res["redacted"] != true {
		t.Fatalf("默认应标记 redacted=true：%v", res["redacted"])
	}
	names := fmt.Sprint(res["redactedFields"])
	if !strings.Contains(names, "password") || !strings.Contains(names, "keyContent") {
		t.Fatalf("redactedFields 应列出被清空的字段：%v", res["redactedFields"])
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(inner), &doc); err != nil {
		t.Fatalf("导出的 json 字段应是合法 JSON：%v（%s）", err, inner)
	}
	if doc["format"] != "ding-ssh.servers" {
		t.Fatalf("导出文档缺少 format：%v", doc)
	}
}

func TestAppctlServersExportIncludeSecrets(t *testing.T) {
	// 1) 未开 secrets.read：必须被拒
	ts, _ := newAppctlTestServer(t, &fakeGate{denied: map[Capability]string{
		CapSecretsRead: "「读取敏感数据」能力未开启：请先开启「允许读取敏感数据」",
	}}, map[string]any{OpServersList: exportStubServers()}, nil)
	_, err := ts.srv.appctlDispatch(context.Background(), OpServersExport, map[string]any{"includeSecrets": true})
	if err == nil || !strings.Contains(err.Error(), "读取敏感数据") {
		t.Fatalf("includeSecrets 需要 secrets.read，实际：%v", err)
	}
	// 2) 开启后：返回明文但必须标注明文字段
	ts2, _ := newAppctlTestServer(t, &fakeGate{}, map[string]any{OpServersList: exportStubServers()}, nil)
	res, err := ts2.srv.appctlDispatch(context.Background(), OpServersExport, map[string]any{"includeSecrets": true})
	if err != nil {
		t.Fatalf("开启 secrets.read 后应成功：%v", err)
	}
	inner, _ := res["json"].(string)
	if !strings.Contains(inner, "hunter2-secret") {
		t.Fatalf("includeSecrets=true 时应返回明文：%s", inner)
	}
	plain := fmt.Sprint(res["plaintextFields"])
	if !strings.Contains(plain, "password") {
		t.Fatalf("应标注明文字段：%v", res["plaintextFields"])
	}
	if note, _ := res["note"].(string); !strings.Contains(note, "明文") || !strings.Contains(note, "请勿落盘") {
		t.Fatalf("返回里应警告含明文、请勿落盘：%v", res["note"])
	}
}

func TestAppctlServersGetAndListRedaction(t *testing.T) {
	srvNode := exportStubServers()["servers"].([]any)[0]
	h := &appctlStubHandler{replyFunc: func(op string, args map[string]any) (any, error) {
		switch op {
		case OpServersGet:
			if args["id"] == "srv-1" {
				return map[string]any{"server": srvNode}, nil
			}
			return nil, fmt.Errorf("%w: 未找到服务器", ErrNotFound)
		case OpServersList:
			return exportStubServers(), nil
		}
		return map[string]any{"op": op}, nil
	}}
	ts := newAppctlTestServerWith(t, &fakeGate{}, h)

	res, err := ts.srv.appctlDispatch(context.Background(), OpServersGet, map[string]any{"id": "srv-1"})
	if err != nil {
		t.Fatalf("servers.get 失败：%v", err)
	}
	text, _ := json.Marshal(res)
	if strings.Contains(string(text), "hunter2-secret") {
		t.Fatalf("servers.get 默认必须脱敏：%s", text)
	}
	node, _ := res["server"].(map[string]any)
	if node["password"] != "" {
		t.Fatalf("password 应被清空：%v", node["password"])
	}
	if node["host"] != "10.0.0.1" || node["user"] != "root" {
		t.Fatalf("非敏感字段应保留：%v", node)
	}

	list, err := ts.srv.appctlDispatch(context.Background(), OpServersList, nil)
	if err != nil {
		t.Fatalf("servers.list 失败：%v", err)
	}
	text, _ = json.Marshal(list)
	if strings.Contains(string(text), "hunter2-secret") {
		t.Fatalf("servers.list 默认必须脱敏：%s", text)
	}
	if list["count"] != 1 {
		t.Fatalf("servers.list count 应为 1：%v", list["count"])
	}

	// 未找到 id 时返回 ErrNotFound
	_, err = ts.srv.appctlDispatch(context.Background(), OpServersGet, map[string]any{"id": "nope"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("未找到服务器应返回 ErrNotFound，实际 %v", err)
	}
	// includeSecrets 需要 secrets.read：被拒时给中文原因
	deniedTS, _ := newAppctlTestServer(t, &fakeGate{denied: map[Capability]string{
		CapSecretsRead: "「读取敏感数据」能力未开启：请先开启「允许读取敏感数据」",
	}}, map[string]any{OpServersGet: map[string]any{"server": srvNode}}, nil)
	if _, err = deniedTS.srv.appctlDispatch(context.Background(), OpServersGet,
		map[string]any{"id": "srv-1", "includeSecrets": true}); err == nil || !strings.Contains(err.Error(), "读取敏感数据") {
		t.Fatalf("未开 secrets.read 时 includeSecrets 应被拒，实际 %v", err)
	}
	// 开启后：返回明文 + 标注明文字段 + 警告
	res2, err := ts.srv.appctlDispatch(context.Background(), OpServersGet, map[string]any{"id": "srv-1", "includeSecrets": true})
	if err != nil {
		t.Fatalf("secrets.read 放行后 includeSecrets 应成功：%v", err)
	}
	if fmt.Sprint(res2["plaintextFields"]) == "[]" {
		t.Fatalf("应标注明文字段：%v", res2["plaintextFields"])
	}
	if note, _ := res2["note"].(string); !strings.Contains(note, "明文") {
		t.Fatalf("应警告返回含明文：%v", res2["note"])
	}
}

// 服务器的写操作落到宿主时不应把明文塞进返回值，且参数要原样传下去。
func TestAppctlServersCreatePassesNodeWithoutID(t *testing.T) {
	ts, h := newAppctlTestServer(t, &fakeGate{}, map[string]any{
		OpServersCreate: map[string]any{"server": map[string]any{"id": "new-1", "name": "n", "host": "h", "user": "u", "password": "p"}},
	}, nil)
	res, err := ts.srv.appctlDispatch(context.Background(), OpServersCreate, map[string]any{
		"node": map[string]any{"id": "should-be-dropped", "host": "h", "user": "u", "password": "p"},
	})
	if err != nil {
		t.Fatalf("servers.create 失败：%v", err)
	}
	sent := h.lastArgs()
	node, _ := sent["node"].(map[string]any)
	if _, has := node["id"]; has {
		t.Fatalf("servers.create 不应把 id 传给宿主（避免误覆盖）：%v", node)
	}
	text, _ := json.Marshal(res)
	if strings.Contains(string(text), `"p"`) {
		t.Fatalf("返回值不应回显密码：%s", text)
	}
}

// ---- 4. 凭据：绝不返回明文 ----

func stubCredentials() map[string]any {
	return map[string]any{
		"count": 2,
		"credentials": []any{
			map[string]any{"id": "cred-1", "name": "生产 root", "type": "password", "user": "root",
				"password": "cred-password-secret", "hasContent": true},
			map[string]any{"id": "cred-2", "name": "私钥", "type": "privateKey", "user": "deploy",
				"keyContent": "-----BEGIN OPENSSH PRIVATE KEY-----secret", "hasContent": true},
		},
	}
}

func TestAppctlCredentialsListHasNoPlaintext(t *testing.T) {
	ts, _ := newAppctlTestServer(t, &fakeGate{}, map[string]any{OpCredentialsList: stubCredentials()}, nil)
	res, err := ts.srv.appctlDispatch(context.Background(), OpCredentialsList, nil)
	if err != nil {
		t.Fatalf("credentials.list 失败：%v", err)
	}
	text, _ := json.Marshal(res)
	for _, leak := range []string{"cred-password-secret", "BEGIN OPENSSH PRIVATE KEY"} {
		if strings.Contains(string(text), leak) {
			t.Fatalf("credentials.list 泄漏了明文：%s", text)
		}
	}
	items, _ := res["credentials"].([]any)
	if len(items) != 2 {
		t.Fatalf("应返回 2 条元信息：%v", res["credentials"])
	}
	first, _ := items[0].(map[string]any)
	if first["id"] != "cred-1" || first["name"] != "生产 root" || first["type"] != "password" {
		t.Fatalf("元信息字段不正确：%v", first)
	}
	if first["hasContent"] != true {
		t.Fatalf("hasContent 应为 true：%v", first)
	}
	if _, has := first["password"]; has {
		t.Fatalf("元信息里不应有 password 字段：%v", first)
	}
}

func TestAppctlCredentialsCreateReturnsMetaOnly(t *testing.T) {
	ts, h := newAppctlTestServer(t, &fakeGate{}, map[string]any{
		OpCredentialsCreate: map[string]any{"credential": map[string]any{"id": "cred-9", "name": "n", "type": "password", "hasContent": true}},
	}, nil)
	const secret = "super-secret-passphrase"
	res, err := ts.srv.appctlDispatch(context.Background(), OpCredentialsCreate, map[string]any{
		"name": "n", "type": "password", "content": secret,
	})
	if err != nil {
		t.Fatalf("credentials.create 失败：%v", err)
	}
	text, _ := json.Marshal(res)
	if strings.Contains(string(text), secret) {
		t.Fatalf("返回值不得包含明文：%s", text)
	}
	if res["hasContent"] != true || res["id"] != "cred-9" {
		t.Fatalf("返回值应只含元信息：%v", res)
	}
	// 明文必须原样传给宿主（否则凭据存不下来）
	if got := fmt.Sprint(h.lastArgs()["content"]); got != secret {
		t.Fatalf("明文应原样传给宿主，实际 %q", got)
	}
}

// ---- 5. 审计参数脱敏（明文密钥零泄漏）----

func TestAuditSafeArgsRedactsSecrets(t *testing.T) {
	args := map[string]any{
		"name": "n", "content": "plain-secret", "token": "tok-1", "confirm": "tok-2",
		"node":  map[string]any{"password": "pw", "host": "h"},
		"args":  map[string]any{"content": "nested-secret"},
		"other": "keep-me",
	}
	safe := auditSafeArgs(OpCredentialsCreate, args)
	text, _ := json.Marshal(safe)
	for _, leak := range []string{"plain-secret", "pw", "nested-secret", "tok-1", "tok-2"} {
		if strings.Contains(string(text), leak) {
			t.Fatalf("审计参数泄漏了 %q：%s", leak, text)
		}
	}
	if !strings.Contains(string(text), "keep-me") {
		t.Fatalf("非敏感参数应保留：%s", text)
	}
	// servers.import 的整段导入数据折叠成摘要
	imp := auditSafeArgs(OpServersImport, map[string]any{"json": `[{"host":"h","password":"pw"}]`, "mode": "merge"})
	impTextBytes, _ := json.Marshal(imp)
	impText := string(impTextBytes)
	if strings.Contains(impText, "pw") || !strings.Contains(impText, "字节的导入数据") {
		t.Fatalf("导入数据应折叠成摘要：%s", impText)
	}
}

// 端到端：凭据写入的明文不得进入审计环形缓冲（AI 能用 app.recent_calls 读到它）。
func TestAppctlCredentialsCreateAuditHasNoPlaintext(t *testing.T) {
	ensureAllRegistrations(t)
	ts, _ := newAppctlTestServer(t, &fakeGate{}, map[string]any{
		OpCredentialsCreate: map[string]any{"credential": map[string]any{"id": "cred-1", "name": "n", "type": "password", "hasContent": true}},
	}, nil)
	const secret = "audit-leak-canary"
	args := map[string]any{"name": "n", "type": "password", "content": secret}
	token := prepareToken(t, ts.srv, "credentials.create", args)
	args["token"] = token
	isErr, text, protoErr := callTool(t, ts.srv, OpCredentialsCreate, args)
	if isErr || protoErr != "" {
		t.Fatalf("credentials.create 不应失败：isError=%v proto=%s text=%s", isErr, protoErr, text)
	}
	if strings.Contains(text, secret) {
		t.Fatalf("工具返回值泄漏明文：%s", text)
	}
	recs := ts.audit.Recent(10, "", "")
	if len(recs) == 0 {
		t.Fatalf("应有审计记录")
	}
	for _, rec := range recs {
		if strings.Contains(rec.Args, secret) || strings.Contains(rec.Error, secret) {
			t.Fatalf("审计记录泄漏明文：%+v", rec)
		}
	}
	// 能力位应被记录（primaryCapability 走注册表）
	foundCap := false
	for _, rec := range recs {
		if rec.Tool == OpCredentialsCreate && rec.Cap == string(CapSecretsWrite) {
			foundCap = true
		}
	}
	if !foundCap {
		t.Fatalf("审计记录应带 secrets.write 能力位：%+v", recs)
	}
}

// ---- 6. settings.schema 覆盖全部字段 ----

func stubSettingsSchema() map[string]any {
	return map[string]any{
		"defaults": map[string]any{
			"uiScale":      100.0,
			"logLevel":     "info",
			"theme":        map[string]any{"background": "#0c1016", "blurAmount": 12.0},
			"fonts":        map[string]any{"terminalFontSize": 13.0},
			"logTraceTabs": []any{},
			"debug":        map[string]any{"enabled": false, "capConfigWrite": false},
		},
		"current": map[string]any{
			"uiScale":      120.0,
			"logLevel":     "debug",
			"theme":        map[string]any{"background": "#000000", "blurAmount": 4.0},
			"fonts":        map[string]any{"terminalFontSize": 15.0},
			"logTraceTabs": []any{"tab-1"},
			"debug":        map[string]any{"enabled": true, "capConfigWrite": true},
		},
		"meta": map[string]any{
			"logLevel":             map[string]any{"enum": []any{"debug", "info", "warn", "error"}},
			"debug.capConfigWrite": map[string]any{"risky": true, "note": "cap"},
			"debug.enabled":        map[string]any{"restartRequired": false},
		},
		"redactedPaths": []any{"localShell"},
	}
}

// 覆盖性由实现保证（从 defaults 树递归走），这里用测试自己的遍历做独立核对。
func TestAppctlSettingsSchemaCoversAllFields(t *testing.T) {
	ts, _ := newAppctlTestServer(t, &fakeGate{}, map[string]any{OpSettingsSchema: stubSettingsSchema()}, nil)
	res, err := ts.srv.appctlDispatch(context.Background(), OpSettingsSchema, nil)
	if err != nil {
		t.Fatalf("settings.schema 失败：%v", err)
	}
	fields, _ := res["fields"].([]any)
	got := map[string]map[string]any{}
	for _, item := range fields {
		m, _ := item.(map[string]any)
		path, _ := m["path"].(string)
		got[path] = m
	}
	// 独立核对：把 defaults 树里所有叶子路径枚举出来，逐个断言出现在 schema 里
	want := map[string]string{
		"uiScale":                "int",
		"logLevel":               "string",
		"theme.background":       "string",
		"theme.blurAmount":       "int",
		"fonts.terminalFontSize": "int",
		"logTraceTabs":           "stringArray",
		"debug.enabled":          "bool",
		"debug.capConfigWrite":   "bool",
	}
	for path, typ := range want {
		m, ok := got[path]
		if !ok {
			t.Fatalf("settings.schema 缺少字段 %s（实际字段：%v）", path, sortedFieldPaths(got))
		}
		if m["type"] != typ {
			t.Fatalf("字段 %s 的 type = %v，期望 %s", path, m["type"], typ)
		}
		if _, has := m["default"]; !has {
			t.Fatalf("字段 %s 缺少 default", path)
		}
		if _, has := m["current"]; !has {
			t.Fatalf("字段 %s 缺少 current", path)
		}
		if _, has := m["restartRequired"]; !has {
			t.Fatalf("字段 %s 缺少 restartRequired", path)
		}
		if _, has := m["risky"]; !has {
			t.Fatalf("字段 %s 缺少 risky", path)
		}
	}
	// 注解来自宿主 meta
	if enum := fmt.Sprint(got["logLevel"]["enum"]); !strings.Contains(enum, "warn") {
		t.Fatalf("logLevel 应带 enum：%v", got["logLevel"]["enum"])
	}
	if got["debug.capConfigWrite"]["risky"] != true {
		t.Fatalf("debug.capConfigWrite 应标记 risky：%v", got["debug.capConfigWrite"])
	}
	if got["uiScale"]["current"] != 120.0 {
		t.Fatalf("uiScale 的 current 应为 120：%v", got["uiScale"]["current"])
	}
	// 容器字段也要出现（便于整块重置）
	if _, ok := got["theme"]; !ok {
		t.Fatalf("容器字段 theme 也应出现：%v", sortedFieldPaths(got))
	}
	// defaults 为空时给中文错误
	empty, _ := newAppctlTestServer(t, &fakeGate{}, map[string]any{OpSettingsSchema: map[string]any{}}, nil)
	if _, err := empty.srv.appctlDispatch(context.Background(), OpSettingsSchema, nil); err == nil || !strings.Contains(err.Error(), "defaults") {
		t.Fatalf("宿主没给 defaults 时应报中文错误，实际 %v", err)
	}
}

func sortedFieldPaths(fields map[string]map[string]any) []string {
	out := make([]string, 0, len(fields))
	for k := range fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- 7. app.command 的「按命令」权限判定 ----

func TestAppctlAppCommandPerCommandRules(t *testing.T) {
	// 1) 只读 / 切换视图类命令：即使所有能力都被拒也必须成功
	denyAll := &fakeGate{denied: map[Capability]string{
		CapConfigWrite: "「配置写入」能力未开启", CapLifecycle: "「应用生命周期」能力未开启",
	}}
	ts, h := newAppctlTestServer(t, denyAll, map[string]any{OpAppCommand: map[string]any{"ok": true, "id": "workspace"}}, nil)
	for _, id := range []string{"workspace", "servers", "tunnel", "settings", "new", "local", "hide-tool", "show-sftp", "show-sys", "connect-srv-1"} {
		if _, err := ts.srv.appctlDispatch(context.Background(), OpAppCommand, map[string]any{"id": id}); err != nil {
			t.Fatalf("只读 / 视图类命令 %s 不应需要能力位：%v", id, err)
		}
	}
	if !h.called(OpAppCommand) {
		t.Fatalf("app.command 应落到宿主 op")
	}

	// 2) 未登记的命令：默认要求 config.write + 确认
	_, err := ts.srv.appctlDispatch(context.Background(), OpAppCommand, map[string]any{"id": "danger.unknown"})
	if err == nil || !strings.Contains(err.Error(), "配置写入") {
		t.Fatalf("未登记命令应要求 config.write，实际 %v", err)
	}

	// 3) 能力开了但没有 token → 提示先 confirm.prepare
	open := &fakeGate{caps: map[Capability]bool{CapConfigWrite: true}}
	ts2, _ := newAppctlTestServer(t, open, map[string]any{OpAppCommand: map[string]any{"ok": true}}, nil)
	_, err = ts2.srv.appctlDispatch(context.Background(), OpAppCommand, map[string]any{"id": "danger.unknown"})
	if err == nil || !strings.Contains(err.Error(), "confirm.prepare") {
		t.Fatalf("缺 token 时应提示 confirm.prepare，实际 %v", err)
	}

	// 4) 带合法 token → 放行
	args := map[string]any{"id": "danger.unknown"}
	token := prepareToken(t, ts2.srv, appctlCommandConfirmAction, args)
	args["token"] = token
	res, err := ts2.srv.appctlDispatch(context.Background(), OpAppCommand, args)
	if err != nil {
		t.Fatalf("带 token 的未登记命令应放行：%v", err)
	}
	if res["ok"] != true || res["id"] != "danger.unknown" {
		t.Fatalf("返回不正确：%v", res)
	}

	// 5) token 复用会被拒（一次性）
	if _, err := ts2.srv.appctlDispatch(context.Background(), OpAppCommand, args); err == nil {
		t.Fatalf("token 复用应被拒")
	}
}

// confirm.prepare 必须能为 app.command 登记 token（否则「会改配置的命令」无法确认）。
func TestAppctlAppCommandConfirmable(t *testing.T) {
	ensureAllRegistrations(t)
	ts, _ := newAppctlTestServer(t, &fakeGate{}, nil, nil)
	isErr, text, protoErr := callTool(t, ts.srv, "confirm.prepare", map[string]any{
		"action": appctlCommandConfirmAction, "args": map[string]any{"id": "danger.unknown"},
	})
	if isErr || protoErr != "" {
		t.Fatalf("confirm.prepare(action=app.command) 应成功：isError=%v proto=%s text=%s", isErr, protoErr, text)
	}
	if !strings.Contains(text, "token") {
		t.Fatalf("confirm.prepare 应返回 token：%s", text)
	}
}

// ---- 8. 其余写工具：缺 token 时不得落到宿主 ----

func TestAppctlWriteToolsRequireToken(t *testing.T) {
	open := &fakeGate{caps: map[Capability]bool{
		CapConfigWrite: true, CapSecretsWrite: true, CapLifecycle: true,
	}}
	for _, tc := range []struct {
		tool   string
		action string
		args   map[string]any
	}{
		{OpServersCreate, "servers.create", map[string]any{"node": map[string]any{"host": "h", "user": "u"}}},
		{OpServersUpdate, "servers.update", map[string]any{"id": "s1", "node": map[string]any{"host": "h2", "user": "u"}}},
		{OpServersDelete, "servers.delete", map[string]any{"id": "s1"}},
		{OpServersDuplicate, "servers.create", map[string]any{"id": "s1"}},
		{OpServersMoveGroup, "servers.update", map[string]any{"ids": []any{"s1"}, "group": "g"}},
		{OpServersImport, "servers.create", map[string]any{"json": `[{"host":"h","user":"u"}]`}},
		{OpCredentialsCreate, "credentials.create", map[string]any{"name": "n", "type": "password", "content": "c"}},
		{OpCredentialsUpdate, "credentials.update", map[string]any{"id": "c1", "name": "n2"}},
		{OpCredentialsDelete, "credentials.delete", map[string]any{"id": "c1"}},
		{OpCredentialsAttach, "servers.update", map[string]any{"serverId": "s1", "credentialId": "c1"}},
		{OpTunnelsCreate, "tunnels.create", map[string]any{"serverId": "s1", "type": "local", "listenPort": 8080.0, "targetPort": 80.0}},
		{OpTunnelsUpdate, "tunnels.update", map[string]any{"id": "t1", "serverId": "s1", "type": "local", "listenPort": 8080.0, "targetPort": 80.0}},
		{OpTunnelsDelete, "tunnels.delete", map[string]any{"id": "t1"}},
		{OpTunnelsStart, "tunnels.start", map[string]any{"id": "t1"}},
		{OpTunnelsStop, "tunnels.stop", map[string]any{"id": "t1"}},
		{OpSettingsReset, "settings.reset", map[string]any{"paths": []any{"uiScale"}}},
		{OpAppQuit, "app.quit", nil},
	} {
		ts, h := newAppctlTestServer(t, open, nil, nil)
		isErr, text, protoErr := callTool(t, ts.srv, tc.tool, tc.args)
		if protoErr != "" {
			t.Fatalf("%s 缺 token 不应是协议错误：%s", tc.tool, protoErr)
		}
		if !isErr || !strings.Contains(text, "confirm.prepare") {
			t.Fatalf("%s 缺 token 时应拒绝并提示 confirm.prepare：isError=%v text=%s", tc.tool, isErr, text)
		}
		if action, need := ToolConfirmAction(tc.tool); !need || action != tc.action {
			t.Fatalf("%s 的 action 应为 %s，实际 %q/%v", tc.tool, tc.action, action, need)
		}
		if h.called(tc.tool) {
			t.Fatalf("%s 缺 token 时不得落到宿主 op", tc.tool)
		}
	}
}

// 能力未开启时：写工具返回中文拒绝原因，且不落到宿主。
func TestAppctlWriteToolsDeniedWithoutCapability(t *testing.T) {
	deny := &fakeGate{denied: map[Capability]string{
		CapConfigWrite: "「配置写入」能力未开启：请在 设置 → 调试模式 → MCP 能力 中开启",
	}}
	ts, h := newAppctlTestServer(t, deny, nil, nil)
	isErr, text, _ := callTool(t, ts.srv, OpServersCreate, map[string]any{
		"node": map[string]any{"host": "h", "user": "u"},
	})
	if !isErr || !strings.Contains(text, "配置写入") {
		t.Fatalf("能力未开启时应返回中文拒绝原因：isError=%v text=%s", isErr, text)
	}
	if h.called(OpServersCreate) {
		t.Fatalf("能力被拒时不得落到宿主 op")
	}
	// secrets.write 需要 AllowSecrets（由 debugGate 校验，这里用替身模拟）
	denySecrets := &fakeGate{denied: map[Capability]string{
		CapSecretsWrite: "「凭据写入」能力未开启：凭据写入需要同时开启「允许读取敏感数据」",
	}}
	ts2, h2 := newAppctlTestServer(t, denySecrets, nil, nil)
	isErr, text, _ = callTool(t, ts2.srv, OpCredentialsCreate, map[string]any{"name": "n", "type": "password", "content": "c"})
	if !isErr || !strings.Contains(text, "凭据写入") {
		t.Fatalf("secrets.write 未开启时应拒绝：isError=%v text=%s", isErr, text)
	}
	if h2.called(OpCredentialsCreate) {
		t.Fatalf("能力被拒时不得落到宿主 op")
	}
}
