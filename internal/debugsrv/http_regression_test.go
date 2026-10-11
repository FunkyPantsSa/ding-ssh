package debugsrv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件是「HTTP 链路」的回归测试：这些细节单测 dispatch 层时不会暴露，
// 但一旦回归就会让「AI 先 prepare、再带 token 调 HTTP」直接不可用。

// 回归测试：confirmToken（以及鉴权 token）绝不能混进业务参数。
//
// 曾经的 bug：guarded 用 buildArgs 解析参数时把 ?confirmToken=… 也当成业务字段，
// 于是「设置补丁」里多出一个 confirmToken，导致
// 「AI 先 confirm.prepare、再带 token 调 HTTP」永远报「参数与确认时不一致」。
func TestConfirmTokenNotPartOfBusinessArgs(t *testing.T) {
	gate := &stubGate{open: map[Capability]bool{CapConfigWrite: true}}
	base, _ := startHTTP(t, gate, &stubExecutor{})

	token := mcpPrepareToken(t, base, "settings.update", map[string]any{"settings": map[string]any{"uiScale": 120}})
	status, body := httpDo(t, base, http.MethodPut, "/v1/settings?confirmToken="+token+"&token=ignored", map[string]any{"uiScale": 120})
	if status != http.StatusOK {
		t.Fatalf("带 confirmToken 的写请求应成功，实际 %d：%s", status, body)
	}
}

// 回归测试：buildArgs 读完请求体后必须放回，否则 guarded 之后的 handler 收到空 body。
func TestBuildArgsRestoresBody(t *testing.T) {
	s := New(Options{Handler: &fakeHandler{}})
	req := httptest.NewRequest(http.MethodPut, "/v1/settings", strings.NewReader(`{"uiScale":120}`))
	first, err := s.buildArgs(req, "")
	if err != nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	second, err := s.buildArgs(req, "")
	if err != nil {
		t.Fatalf("二次解析失败: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("请求体未放回：first=%s second=%s", first, second)
	}
	if !strings.Contains(string(second), "uiScale") {
		t.Fatalf("二次解析应仍能看到请求体字段：%s", second)
	}
}

// 回归测试：审计中间件与内层 handler 必须共享同一个 auditInfo 指针，
// 否则 HTTP 来源的记录会丢掉工具名 / 能力位（只剩 "PUT /v1/settings"）。
func TestAuditMiddlewareSharesInfo(t *testing.T) {
	gate := &stubGate{open: map[Capability]bool{CapConfigWrite: true}}
	base, audit := startHTTP(t, gate, &stubExecutor{})
	token := mcpPrepareToken(t, base, "settings.update", map[string]any{"settings": map[string]any{"uiScale": 120}})
	status, _ := httpDo(t, base, http.MethodPut, "/v1/settings?confirmToken="+token, map[string]any{"uiScale": 120})
	if status != http.StatusOK {
		t.Fatalf("写请求应成功，实际 %d", status)
	}
	rec := audit.Recent(1, "", "http")[0]
	if rec.Tool != "update_settings" {
		t.Fatalf("审计工具名应为 MCP 同名工具，实际 %q", rec.Tool)
	}
	if rec.Cap != string(CapConfigWrite) {
		t.Fatalf("审计能力位应为 config.write，实际 %q", rec.Cap)
	}
	if !strings.Contains(rec.Args, "uiScale") || strings.Contains(rec.Args, "confirmToken") {
		t.Fatalf("审计参数应只含业务参数：%s", rec.Args)
	}
}
