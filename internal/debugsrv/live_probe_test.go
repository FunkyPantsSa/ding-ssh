package debugsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// 本文件把一份「活体自测」的原始响应打到 stdout（go test -run TestManualLiveProbe -v）。
// 它模拟的是应用内真实运行的调试服务：真实监听端口 + 真实中间件链 + 真实 Gate/Executor 替身。
// 用途：应用未运行时用它给出可复核的实际输出；参数 -probe-live 才会真正执行，
// 因此默认（普通 go test）只做一次编译检查，不产生副作用。
func TestManualLiveProbe(t *testing.T) {
	if os.Getenv("DSH_LIVE_PROBE") == "" {
		t.Skip("设置 DSH_LIVE_PROBE=1 才执行活体探测（会产生 stdout 输出）")
	}
	gate := &stubGate{denied: map[Capability]string{
		CapConfigWrite: "「配置写入」能力未开启：请在 设置 → 调试模式 → MCP 能力 中开启",
	}, open: map[Capability]bool{CapTerminalInput: true}}
	audit := NewAuditLog()
	s := New(Options{
		Addr: "127.0.0.1:0", Token: "probe-token", Handler: &fakeHandler{}, Hub: NewHub(),
		Gate: gate, Audits: audit, Confirms: NewConfirmManager(DefaultConfirmTTL), Executor: &stubExecutor{},
		Logf: func(string, ...any) {},
	})
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Stop(ctx)
	}()
	base := "http://" + addr
	probe := func(label, method, path string, body any) string {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, r)
		req.Header.Set("Authorization", "Bearer probe-token")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Sprintf("%s: 请求失败 %v", label, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Sprintf("%s [HTTP %d] %s", label, resp.StatusCode, truncateForLog(strings.TrimSpace(string(data)), 400))
	}

	var out []string
	out = append(out, probe("GET /v1/health", http.MethodGet, "/v1/health", nil))
	out = append(out, probe("PUT /v1/settings（能力未开）", http.MethodPut, "/v1/settings", map[string]any{"uiScale": 120}))
	out = append(out, probe("initialize", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26"}}))
	out = append(out, probe("tools/list", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list"}))
	out = append(out, probe("app.permissions", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "app.permissions"}}))
	out = append(out, probe("update_settings（缺 token）", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "update_settings", "arguments": map[string]any{"settings": map[string]any{"uiScale": 120}}}}))
	out = append(out, probe("send_input（可逆/高频，不带 token）", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{"name": "send_input", "arguments": map[string]any{"id": "tab-1", "data": "ls\r"}}}))
	out = append(out, probe("eval_js（可逆/高频，不带 token）", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 6, "method": "tools/call",
		"params": map[string]any{"name": "eval_js", "arguments": map[string]any{"js": "1+1"}}}))
	out = append(out, probe("app.recent_calls", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": "app.recent_calls", "arguments": map[string]any{"limit": 5}}}))
	out = append(out, probe("app.health", http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 8, "method": "tools/call",
		"params": map[string]any{"name": "app.health"}}))
	fmt.Println("=== LIVE PROBE ===")
	for _, line := range out {
		fmt.Println(line)
	}
	fmt.Println("=== confirmActionNeedsConfirm 最终清单 ===")
	fmt.Println("ConfirmableActions() =", strings.Join(ConfirmableActions(), " "))
	fmt.Printf("可逆/高频（不需要 token）: terminal.input=%v ui.write=%v eval=%v\n",
		ActionNeedsConfirm("terminal.input"), ActionNeedsConfirm("ui.write"), ActionNeedsConfirm("eval"))
	fmt.Println("=== END ===")
}
