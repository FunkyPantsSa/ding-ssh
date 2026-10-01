package debugsrv

// M9：工具目录（GET /v1/tools）与 MCP 结构化输出（structuredContent / outputSchema）。
//
// 这两件事的共同点：都要求「工具条目表是唯一事实来源」——
//   - /v1/tools 的条数必须等于 tools/list 的条数（同一份 mcpToolEntries），
//     并且覆盖**全部已注册工具**（注册表驱动，不写死数字）；
//   - structuredContent 只对声明了 outputSchema 的工具返回，且文本内容 content[0] 必须保持原样
//     （老客户端与既有断言都依赖文本）。

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// structuredExpect 规格表：应当声明 outputSchema / 返回 structuredContent 的工具（13 个）。
//
// 这是**规格清单**（任务要求逐个点名），不是「总数」——总数仍由注册表 / mcpToolEntries 推导。
var structuredExpect = []string{
	"app_state", "app.health", "app.permissions",
	"list_terminals", "read_terminal",
	OpUILayout, OpUIDiff, OpUIQuery,
	OpSftpList,
	OpServersList, OpServersGet,
	OpSettingsSchema,
	"app.recent_calls",
}

// toolsListHTTP 走一次真实 HTTP 的 /mcp tools/list，返回工具名集合与条数。
func toolsListHTTP(t *testing.T, base string) (map[string]bool, int, []map[string]any) {
	t.Helper()
	status, body := httpDo(t, base, http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list 应 200，实际 %d：%s", status, body)
	}
	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("解析 tools/list 失败：%v", err)
	}
	names := make(map[string]bool, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names[uiArgString(tool, "name")] = true
	}
	return names, len(resp.Result.Tools), resp.Result.Tools
}

// GET /v1/tools：条数与 tools/list 完全一致、覆盖全部已注册工具、字段齐全且自洽。
func TestToolsCatalogHTTP(t *testing.T) {
	ensureAllRegistrations(t)
	base, audit := startHTTP(t, &stubGate{}, &stubExecutor{})

	// 1) Bearer 鉴权沿用同一个 token（不带 token 必须 401）
	req, err := http.NewRequest(http.MethodGet, base+"/v1/tools", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	respNoToken, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求 /v1/tools 失败: %v", err)
	}
	respNoToken.Body.Close()
	if respNoToken.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /v1/tools 未带 token 应 401，实际 %d", respNoToken.StatusCode)
	}

	// 2) 正常调用
	status, body := httpDo(t, base, http.MethodGet, "/v1/tools", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/tools 应 200，实际 %d：%s", status, body)
	}
	var catalog struct {
		Count           int              `json:"count"`
		StructuredCount int              `json:"structuredCount"`
		Tools           []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatalf("解析 /v1/tools 失败：%v", err)
	}
	if catalog.Count != len(catalog.Tools) || catalog.Count == 0 {
		t.Fatalf("count(%d) 与 tools 条数(%d) 不一致或为空", catalog.Count, len(catalog.Tools))
	}

	// 3) 与 tools/list 同源：名字集合与条数必须完全一致（注册表驱动，不写死数字）
	listed, listedCount, _ := toolsListHTTP(t, base)
	if listedCount != catalog.Count {
		t.Fatalf("/v1/tools 条数(%d) 应等于 tools/list 条数(%d)", catalog.Count, listedCount)
	}
	// 4) 覆盖全部已注册工具
	registered := AllRegisteredToolNames()
	if len(registered) == 0 {
		t.Fatal("AllRegisteredToolNames 为空（注册表被清空？）")
	}
	catalogNames := make(map[string]bool, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		name := uiArgString(tool, "name")
		catalogNames[name] = true
		if !listed[name] {
			t.Fatalf("/v1/tools 里的 %s 不在 tools/list 里", name)
		}
		// 字段齐全
		for _, key := range []string{"name", "capability", "needsToken", "description", "inputSchema", "hasStructuredOutput"} {
			if _, has := tool[key]; !has {
				t.Fatalf("工具 %s 的目录项缺少字段 %s：%v", name, key, tool)
			}
		}
		if !strings.Contains(uiArgString(tool, "description"), "能力要求：") {
			t.Fatalf("工具 %s 的描述缺少「能力要求」行", name)
		}
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema == nil || schema["type"] != "object" {
			t.Fatalf("工具 %s 的 inputSchema 不是 object：%v", name, tool["inputSchema"])
		}
		// capability 必须是已知能力名（读类为空数组）
		caps, _ := tool["capability"].([]any)
		for _, c := range caps {
			if _, ok := KnownCapability(uiArgString(map[string]any{"c": c}, "c")); !ok {
				t.Fatalf("工具 %s 的能力位 %v 不是已知能力", name, c)
			}
		}
		// needsToken 与 confirmAction 自洽，且该 action 确实需要确认
		needToken, _ := tool["needsToken"].(bool)
		action := uiArgString(tool, "confirmAction")
		if needToken {
			if action == "" || !ActionNeedsConfirm(action) {
				t.Fatalf("工具 %s 标了 needsToken 但 confirmAction=%q 不自洽", name, action)
			}
		} else if action != "" {
			t.Fatalf("工具 %s 标了 confirmAction=%q 但 needsToken=false", name, action)
		}
		// structuredContent 声明自洽：有 outputSchema 才算有结构化输出
		hasStructured, _ := tool["hasStructuredOutput"].(bool)
		_, hasSchema := tool["outputSchema"]
		if hasStructured != hasSchema {
			t.Fatalf("工具 %s 的 hasStructuredOutput=%v 与 outputSchema 存在性(%v) 不一致", name, hasStructured, hasSchema)
		}
		if hasStructured {
			os, _ := tool["outputSchema"].(map[string]any)
			if os == nil || os["type"] != "object" {
				t.Fatalf("工具 %s 的 outputSchema 不是 object：%v", name, tool["outputSchema"])
			}
		}
	}
	for _, name := range registered {
		if !catalogNames[name] {
			t.Fatalf("注册表里的工具 %s 没出现在 /v1/tools", name)
		}
	}

	// 5) 结构化输出清单与规格一致（13 个）
	var got []string
	for _, tool := range catalog.Tools {
		if ok, _ := tool["hasStructuredOutput"].(bool); ok {
			got = append(got, uiArgString(tool, "name"))
		}
	}
	sort.Strings(got)
	want := append([]string(nil), structuredExpect...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("声明结构化输出的工具清单不符：\n got=%v\nwant=%v", got, want)
	}
	if catalog.StructuredCount != len(want) {
		t.Fatalf("structuredCount=%d，期望 %d", catalog.StructuredCount, len(want))
	}

	// 6) 审计：/v1/tools 是读类请求，也会留一条（工具名保持 "GET /v1/tools"，不伪造 MCP 工具名）
	recs := audit.Recent(5, "", "http")
	found := false
	for _, r := range recs {
		if r.Tool == "GET /v1/tools" && r.OK {
			found = true
		}
	}
	if !found {
		t.Fatalf("GET /v1/tools 应留下审计记录：%+v", recs)
	}
}

// callToolResult 走一次 MCP tools/call，返回解析后的完整 result（含 structuredContent）。
func callToolResult(t *testing.T, s *Server, name string, args map[string]any) map[string]any {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatalf("序列化参数失败: %v", err)
	}
	resp := s.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params,
	})
	if resp.Error != nil {
		t.Fatalf("%s 返回 JSON-RPC 错误：%+v", name, resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 tools/call 结果失败：%v", err)
	}
	if isErr, _ := out["isError"].(bool); isErr {
		t.Fatalf("%s 意外失败：%v", name, out["content"])
	}
	return out
}

// callToolText 取出 content[0] 的文本（必须继续存在）。
func callToolText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("content 为空：%v", result)
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text" {
		t.Fatalf("content[0] 必须是 text（向后兼容）：%v", first)
	}
	text, _ := first["text"].(string)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("content[0].text 不能为空")
	}
	return text
}

// 结构化输出：文本与 structuredContent 同时存在；文本保持既有形状（数组仍是数组）。
func TestMCPStructuredContent(t *testing.T) {
	ensureAllRegistrations(t)
	h := &appctlStubHandler{replyFunc: func(op string, _ map[string]any) (any, error) {
		if op == OpTerminals {
			// list_terminals 的文本内容是数组（既有格式，不许改）
			return []any{map[string]any{"clientId": "tab-1", "rows": 24.0}}, nil
		}
		return map[string]any{"op": op}, nil
	}}
	s := New(Options{
		Handler: h, Hub: NewHub(), Gate: &fakeGate{}, Audits: NewAuditLog(),
		Confirms: NewConfirmManager(DefaultConfirmTTL), Executor: &fakeExecutor{},
	})

	// 1) list_terminals：文本是数组，structuredContent 是对象 {count, terminals}
	res := callToolResult(t, s, "list_terminals", nil)
	text := callToolText(t, res)
	if !strings.HasPrefix(strings.TrimSpace(text), "[") {
		t.Fatalf("list_terminals 的文本内容应保持数组形状：%s", text)
	}
	sc, _ := res["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatalf("list_terminals 应返回 structuredContent：%v", res)
	}
	if uiNum(sc["count"]) != 1 {
		t.Fatalf("structuredContent.count = %v，期望 1", sc["count"])
	}
	if arr, _ := sc["terminals"].([]any); len(arr) != 1 {
		t.Fatalf("structuredContent.terminals 应为数组：%v", sc)
	}

	// 2) 对象型工具：structuredContent 与文本是同一份数据
	res = callToolResult(t, s, "app_state", nil)
	text = callToolText(t, res)
	sc, _ = res["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatalf("app_state 应返回 structuredContent：%v", res)
	}
	var fromText map[string]any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil {
		t.Fatalf("app_state 文本不是 JSON：%v", err)
	}
	if fromText["op"] != sc["op"] {
		t.Fatalf("structuredContent 应与文本同源：text=%v structured=%v", fromText, sc)
	}

	// 3) app.permissions：structuredContent 里能看到新增的 toolCount
	res = callToolResult(t, s, "app.permissions", nil)
	sc, _ = res["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatalf("app.permissions 应返回 structuredContent：%v", res)
	}
	if uiNum(sc["toolCount"]) != float64(len(mcpTools(s))) {
		t.Fatalf("app.permissions 的 toolCount=%v，期望 %d", sc["toolCount"], len(mcpTools(s)))
	}

	// 4) 未声明 outputSchema 的工具：不许出现 structuredContent
	res = callToolResult(t, s, "list_logs", nil)
	if _, has := res["structuredContent"]; has {
		t.Fatalf("list_logs 未声明 outputSchema，不应返回 structuredContent：%v", res)
	}
	callToolText(t, res) // 文本必须仍在
}
