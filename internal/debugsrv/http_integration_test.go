package debugsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 端到端自测：真实起一个调试服务（net/http + 真实中间件链），用 HTTP 请求走一遍
// 「能力校验 → 两段式确认 → 执行 → 审计」四条链路。
//
// 为什么要有它：单元测试打的是 dispatch 层，而真实使用（脚本 / 其他工具）走的是
// HTTP 路由；两者的差异（路径参数、请求体解析、状态码、中间件顺序）只在这里能覆盖。
// 本机应用未运行时，这个测试等价于「活体自测」的替代品。

// stubGate 一个可控的 Gate：denied 里的能力被拒（中文原因）。
type stubGate struct {
	denied map[Capability]string
	open   map[Capability]bool
}

func (g *stubGate) Allow(cap Capability, _ string) error {
	if cap == CapRead {
		return nil
	}
	if msg, bad := g.denied[cap]; bad {
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (g *stubGate) Snapshot() map[Capability]bool {
	out := map[Capability]bool{CapRead: true}
	for c := range g.open {
		out[c] = true
	}
	return out
}

// stubExecutor 记录被执行的确认动作。
type stubExecutor struct {
	actions []string
}

func (e *stubExecutor) ExecuteAction(_ context.Context, action string, _ map[string]any) (any, error) {
	e.actions = append(e.actions, action)
	return map[string]any{"ok": true, "action": action}, nil
}

// startHTTP 起一个真实监听的服务，返回 base URL 与审计日志。
func startHTTP(t *testing.T, gate Gate, exec Executor) (string, *AuditLog) {
	t.Helper()
	audit := NewAuditLog()
	h := &fakeHandler{}
	s := New(Options{
		Addr:     "127.0.0.1:0",
		Token:    "test-token",
		Handler:  h,
		Hub:      NewHub(),
		Gate:     gate,
		Audits:   audit,
		Confirms: NewConfirmManager(DefaultConfirmTTL),
		Executor: exec,
		Logf:     func(string, ...any) {},
	})
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("启动调试服务失败: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	})
	return "http://" + addr, audit
}

// httpDo 发一次带 token 的请求，返回状态码与响应体（限长，避免日志被淹没）。
func httpDo(t *testing.T, base, method, path string, body any) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("序列化请求体失败: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求 %s %s 失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	return resp.StatusCode, strings.TrimSpace(string(data))
}

// 写类路由：能力未开启 → 403 + 中文原因（AI / 脚本能直接读懂）。
func TestHTTPWriteRouteDeniedWithoutCapability(t *testing.T) {
	gate := &stubGate{denied: map[Capability]string{
		CapConfigWrite: "「配置写入」能力未开启：请在 设置 → 调试模式 → MCP 能力 中开启",
	}}
	base, audit := startHTTP(t, gate, &stubExecutor{})

	status, body := httpDo(t, base, http.MethodPut, "/v1/settings", map[string]any{"uiScale": 120})
	if status != http.StatusForbidden {
		t.Fatalf("能力未开启应返回 403，实际 %d：%s", status, body)
	}
	if !strings.Contains(body, "配置写入") {
		t.Fatalf("403 响应应含中文原因：%s", body)
	}
	// 审计：HTTP 来源、失败、能力位
	recs := audit.Recent(1, "", "")
	if len(recs) != 1 {
		t.Fatalf("应留下 1 条审计记录，实际 %d", len(recs))
	}
	if recs[0].Source != "http" || recs[0].OK || recs[0].Cap != string(CapConfigWrite) {
		t.Fatalf("审计记录不正确：%+v", recs[0])
	}
	if recs[0].Tool != "update_settings" {
		t.Fatalf("审计工具名应与 MCP 工具同名：%+v", recs[0])
	}
}

// 写类路由：能力开启但缺 confirmToken → 403 且提示先 confirm.prepare（仅限破坏性路由）。
func TestHTTPWriteRouteRequiresConfirmToken(t *testing.T) {
	gate := &stubGate{open: map[Capability]bool{CapConfigWrite: true}}
	base, _ := startHTTP(t, gate, &stubExecutor{})

	status, body := httpDo(t, base, http.MethodPut, "/v1/settings", map[string]any{"uiScale": 120})
	if status != http.StatusForbidden {
		t.Fatalf("缺确认 token 应返回 403，实际 %d：%s", status, body)
	}
	if !strings.Contains(body, "confirm.prepare") || !strings.Contains(body, "settings.update") {
		t.Fatalf("403 响应应说明如何确认：%s", body)
	}
}

// 可逆 / 高频路由（terminal.input）不再需要 confirmToken：能力位开启即可直接调用。
//
// 回归背景：曾经 HTTP 的 POST /v1/terminals/{id}/input 也要 token，会让脚本 / 自动化
// 没法高频写终端（terminal.run / expect）。
func TestHTTPTerminalInputNoConfirmToken(t *testing.T) {
	gate := &stubGate{open: map[Capability]bool{CapTerminalInput: true}}
	base, audit := startHTTP(t, gate, &stubExecutor{})

	status, body := httpDo(t, base, http.MethodPost, "/v1/terminals/tab-1/input", map[string]any{"data": "ls\r"})
	if status != http.StatusOK {
		t.Fatalf("terminal.input 不带 confirmToken 应成功，实际 %d：%s", status, body)
	}
	rec := audit.Recent(1, "", "http")[0]
	if rec.Tool != "send_input" || !rec.OK || rec.Cap != string(CapTerminalInput) {
		t.Fatalf("terminal.input 仍应留能力位审计：%+v", rec)
	}
	if rec.Reversible || rec.RevertHint != "" {
		t.Fatalf("terminal.input 不提供撤销：%+v", rec)
	}
	// 能力位关闭时仍然 403（收窄的是 token，不是能力位）
	deniedBase, _ := startHTTP(t, &stubGate{denied: map[Capability]string{CapTerminalInput: "「终端输入」能力未开启"}}, &stubExecutor{})
	status, body = httpDo(t, deniedBase, http.MethodPost, "/v1/terminals/tab-1/input", map[string]any{"data": "ls\r"})
	if status != http.StatusForbidden || !strings.Contains(body, "终端输入") {
		t.Fatalf("能力位关闭时 terminal.input 仍应 403：%d %s", status, body)
	}
}

// 完整两段式：prepare（走 MCP）→ 带 confirmToken 调 HTTP 写路由 → 200。
func TestHTTPTwoStageConfirmFlow(t *testing.T) {
	gate := &stubGate{open: map[Capability]bool{CapConfigWrite: true}}
	base, audit := startHTTP(t, gate, &stubExecutor{})

	// 1) 通过 MCP 的 confirm.prepare 拿 token（与 HTTP 共用同一个 ConfirmManager）
	token := mcpPrepareToken(t, base, "settings.update", map[string]any{"settings": map[string]any{"uiScale": 120}})
	// 2) 带 token 调 HTTP 写路由
	status, body := httpDo(t, base, http.MethodPut, "/v1/settings?confirmToken="+token, map[string]any{"uiScale": 120})
	if status != http.StatusOK {
		t.Fatalf("带有效 token 应成功，实际 %d：%s", status, body)
	}
	// 3) token 一次性：再用一次必须 403
	status, body = httpDo(t, base, http.MethodPut, "/v1/settings?confirmToken="+token, map[string]any{"uiScale": 120})
	if status != http.StatusForbidden {
		t.Fatalf("重复使用 token 应 403，实际 %d：%s", status, body)
	}
	if !strings.Contains(body, "token") {
		t.Fatalf("重复使用的提示应提到 token：%s", body)
	}
	// 审计：两次 HTTP 请求各留一条（第一次成功、第二次因 token 已用掉而失败）
	recs := audit.Recent(10, "", "http")
	if len(recs) != 2 {
		t.Fatalf("HTTP 请求应各留一条审计，实际 %d 条：%+v", len(recs), recs)
	}
	if recs[0].OK || recs[0].Reversible {
		t.Fatalf("第二条（重复使用 token）应为失败且不可撤销：%+v", recs[0])
	}
	if !recs[1].OK || !recs[1].Reversible || recs[1].RevertHint == "" {
		t.Fatalf("第一条（成功写入设置）应可撤销：%+v", recs[1])
	}
}

// 读类路由不需要能力位也不需要确认，并且同样留下审计记录。
func TestHTTPReadRouteAlwaysAllowed(t *testing.T) {
	gate := &stubGate{denied: map[Capability]string{
		CapConfigWrite: "no", CapLifecycle: "no", CapTerminalInput: "no",
	}}
	base, audit := startHTTP(t, gate, &stubExecutor{})

	status, body := httpDo(t, base, http.MethodGet, "/v1/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("读类路由应放行，实际 %d：%s", status, body)
	}
	status, body = httpDo(t, base, http.MethodGet, "/v1/health", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/health 应放行，实际 %d：%s", status, body)
	}
	recs := audit.Recent(5, "", "http")
	if len(recs) != 2 {
		t.Fatalf("读类请求也应各留一条审计，实际 %d 条：%+v", len(recs), recs)
	}
	for _, r := range recs {
		if !r.OK {
			t.Fatalf("读类请求的审计应成功：%+v", r)
		}
	}
}

// MCP 的 initialize 必须能通过真实 HTTP 走到（含能力快照文本）。
func TestHTTPMCPInitializeAndToolsList(t *testing.T) {
	gate := &stubGate{open: map[Capability]bool{CapConfigWrite: true, CapTerminalInput: true}}
	base, _ := startHTTP(t, gate, &stubExecutor{})

	status, body := httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-03-26"},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize 应 200，实际 %d：%s", status, body)
	}
	if !strings.Contains(body, "当前能力快照") || !strings.Contains(body, "confirm.prepare") {
		t.Fatalf("initialize 应包含能力快照与确认说明：%s", body)
	}

	status, body = httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list 应 200，实际 %d：%s", status, body)
	}
	var listResp struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &listResp); err != nil {
		t.Fatalf("解析 tools/list 失败：%v（%s）", err, truncateForLog(body, 200))
	}
	// 工具总数不再写死（各域会持续追加）：改为**注册表驱动** ——
	// 遍历 AllRegisteredToolNames() 断言每个已注册工具都出现在 tools/list 且都有能力登记。
	// 注意：注册表可能被同包其它用例 ResetRegistrationsForTest 过，因此断言里会重新登记一遍。
	listed := make(map[string]bool, len(listResp.Result.Tools))
	for _, tool := range listResp.Result.Tools {
		listed[tool.Name] = true
	}
	assertRegistryToolsListed(t, listed)
	for _, tool := range listResp.Result.Tools {
		if !strings.Contains(tool.Description, "能力要求：") {
			t.Fatalf("工具 %s 描述缺少能力要求行", tool.Name)
		}
	}
}

// mcpPrepareToken 通过真实 HTTP 调 MCP 的 confirm.prepare 拿 token。
func mcpPrepareToken(t *testing.T, base, action string, args map[string]any) string {
	t.Helper()
	status, body := httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 9, "method": "tools/call",
		"params": map[string]any{"name": "confirm.prepare", "arguments": map[string]any{"action": action, "args": args}},
	})
	if status != http.StatusOK {
		t.Fatalf("confirm.prepare 失败：%d %s", status, body)
	}
	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("解析 confirm.prepare 响应失败: %v（%s）", err, body)
	}
	if resp.Result.IsError {
		t.Fatalf("confirm.prepare 返回错误：%s", resp.Result.Content[0].Text)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &out); err != nil {
		t.Fatalf("解析 token 失败: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("未拿到 token：%s", resp.Result.Content[0].Text)
	}
	return out.Token
}

// truncateForLog 截断过长的响应，避免测试输出刷屏。
func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
