package debugsrv

// MCP（Model Context Protocol）适配层。
//
// 传输：Streamable HTTP —— 客户端对 `POST /mcp` 发送 JSON-RPC 2.0 请求，
// 服务端以 application/json 单次响应（请求/响应型工具足够用，不依赖 SSE）。
// 工具与资源都复用调试 API 既有的 op（见 server.go 的 Op*），因此后端/前端桥的实现只有一份。
//
// 典型接入（任意 MCP 客户端）：
//   { "mcpServers": { "ding-ssh": { "type": "http",
//       "url": "http://127.0.0.1:8765/mcp",
//       "headers": { "Authorization": "Bearer <token>" } } } }

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	mcpProtocolVersion = "2025-03-26"
	mcpServerName      = "ding-ssh-debug"
	mcpServerVersion   = "1.0.0"
	// mcpBodyLimit /mcp 请求体上限（8MB）：既防超大请求，也让调用日志中间件
	// 读请求体时与 mcpPost 用同一个边界（见 server.go 的 requestDetail）。
	mcpBodyLimit = 8 << 20
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorBody   `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// mcpTools 工具清单（名称 → 调试 API 的 op 一一对应）。
func mcpTools() []mcpTool {
	return []mcpTool{
		{Name: "app_state", Description: "读取应用状态：视图、标签、会话、隧道、设置与调试模式信息。",
			InputSchema: obj(nil)},
		{Name: "list_terminals", Description: "列出所有终端及其诊断信息：rows/cols、缓冲区行数、baseY/viewportY、回滚是否可达、可见文本与 DOM 几何。",
			InputSchema: obj(nil)},
		{Name: "read_terminal", Description: "读取某个终端缓冲区的文本行（含回滚）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("终端 id（即标签 clientId，见 list_terminals）"),
				"from":  strProp("起始位置：head | tail | 行号（默认 tail）"),
				"lines": intProp("读取行数（默认 200，最多 20000）"),
			}, "id")},
		{Name: "send_input", Description: "向会话写入数据（等价于在该终端里键入）。",
			InputSchema: obj(map[string]any{
				"id":     strProp("终端 id"),
				"data":   strProp("要写入的文本（\\r 表示回车）"),
				"base64": strProp("二进制输入（base64，与 data 二选一）"),
			}, "id")},
		{Name: "scroll_terminal", Description: "滚动终端视口：top / bottom / line / lines。",
			InputSchema: obj(map[string]any{
				"id":    strProp("终端 id"),
				"to":    strProp("top | bottom | line | lines"),
				"value": intProp("line/lines 时的目标行或行数增量"),
			}, "id", "to")},
		{Name: "resize_terminal", Description: "调整终端的行列数（只改模拟器尺寸，不改 PTY；用于复现布局/渲染问题）。",
			InputSchema: obj(map[string]any{
				"id":   strProp("终端 id"),
				"cols": intProp("列数"),
				"rows": intProp("行数"),
			}, "id")},
		{Name: "open_tab", Description: "打开一个新会话标签：连接已保存的服务器（给 serverId）或本机终端（local=true）。",
			InputSchema: obj(map[string]any{
				"serverId": strProp("服务器 id（见 list_servers）"),
				"node":     map[string]any{"type": "object", "description": "完整服务器节点对象（与 serverId 二选一）"},
				"local":    boolProp("true 表示打开本机终端"),
			})},
		{Name: "close_tab", Description: "关闭标签（断开并移除）。",
			InputSchema: obj(map[string]any{"id": strProp("终端/标签 id")}, "id")},
		{Name: "reconnect_session", Description: "重连指定会话（保留终端缓冲区，不会清屏）。",
			InputSchema: obj(map[string]any{"id": strProp("终端 id")}, "id")},
		{Name: "disconnect_session", Description: "断开指定会话（不关闭标签）。",
			InputSchema: obj(map[string]any{"id": strProp("会话 id（sshx 会话 id，通常等于标签 clientId）")}, "id")},
		{Name: "list_servers", Description: "列出已保存的服务器（默认脱敏：不含密码/私钥，除非开启「允许读取敏感数据」）。",
			InputSchema: obj(nil)},
		{Name: "get_settings", Description: "读取应用设置（受「允许读取敏感数据」开关影响）。",
			InputSchema: obj(nil)},
		{Name: "update_settings", Description: "部分更新应用设置（只传要改的字段），保存后立即生效。",
			InputSchema: obj(map[string]any{"settings": map[string]any{"type": "object", "description": "要合并进现有设置的字段"}}, "settings")},
		{Name: "eval_js", Description: "在页面上下文执行 JS（默认关闭，需在设置 → 调试模式中开启「允许执行 JS」）。",
			InputSchema: obj(map[string]any{"js": strProp("要执行的 JavaScript 表达式或语句")}, "js")},
		{Name: "screenshot", Description: "抓取应用窗口截图，返回 PNG 图片内容（受遮挡影响，窗口需可见）。",
			InputSchema: obj(nil)},
		{Name: "recent_events", Description: "读取最近的后端事件（会话输出/状态/进度/隧道），用于了解「刚才发生了什么」，避免轮询。",
			InputSchema: obj(map[string]any{
				"limit":  intProp("返回条数（默认 50，最多 300）"),
				"topics": strProp("可选，逗号分隔的 topic 过滤，如 ssh.output,ssh.status"),
			})},
		// 日志相关：先 list_logs 看日志在哪、有多少，再 read_logs 看具体内容，
		// 需要用户配合时 export_diagnostics 打包发给你。
		{Name: "list_logs", Description: "列出日志文件与当前日志设置（目录、大小、开关、级别、保留策略）。用于排查问题时先看日志在哪、有多少。",
			InputSchema: obj(nil)},
		{Name: "read_logs", Description: "读取应用日志（默认最新文件，按需过滤），用于问题定位。返回文本行；其中的 token/密码/私钥已被自动打码。" +
			"level 按行内 [DEBUG]/[INFO]/[WARN]/[ERROR] 标记过滤，取该级别及以上（level=warn 同时含 WARN 与 ERROR；" +
			"指定 level 时没有级别标记的第三方库输出会被过滤掉）；keyword 为大小写不敏感的子串。" +
			"注意：只有日志级别为 debug 时才会记录终端跟踪等更细的调试信息（见 set_log_options）。",
			InputSchema: obj(map[string]any{
				"lines":   intProp("读取行数（默认 200，最多 5000）"),
				"level":   strProp("只保留该级别及以上的行：debug | info | warn | error（可选）"),
				"keyword": strProp("只保留含该子串的行，大小写不敏感（可选）"),
				"file":    strProp("日志文件名（见 list_logs 的 files），默认最新文件"),
			})},
		{Name: "export_diagnostics", Description: "把最近日志、应用状态、环境信息打包成 zip（返回路径），适合让用户直接发给你。",
			InputSchema: obj(nil)},
		{Name: "clear_logs", Description: "清空日志文件（返回删除数量）。",
			InputSchema: obj(nil)},
		{Name: "set_log_options", Description: "调整日志设置以便定位问题：打开文件日志/控制台日志、把级别设为 debug、或打开 MCP/调试 API 调用日志。" +
			"只传要改的字段，保存后立即生效。level=debug 才会记录终端跟踪与更细的调试信息；" +
			"apiLogEnabled 打开后每次 HTTP/MCP 调用都会留一行 [api]（只记 path、不含 query，不会泄露 token）。",
			InputSchema: obj(map[string]any{
				"consoleEnabled": boolProp("控制台日志开关"),
				"fileEnabled":    boolProp("文件日志开关（落盘到 list_logs 的 dir）"),
				"level":          strProp("日志级别：debug | info | warn | error（debug 才记录终端跟踪）"),
				"apiLogEnabled":  boolProp("MCP/调试 API 调用日志开关（每次调用一行 [api]）"),
			})},
	}
}

// mcpSession MCP 会话：为支持服务端推送的客户端保留一条事件通道。
// initialize 成功时创建（并把 id 放在 Mcp-Session-Id 响应头里），
// 客户端随后用 GET /mcp（带同一个头）挂 SSE 通道接收通知，DELETE /mcp 结束会话。
type mcpSession struct {
	id        string
	topics    map[string]bool
	createdAt time.Time
	ch        <-chan Event
	hubID     int
}

// mcpSessionHeader MCP 会话头（2025-03-26 规范）。
const mcpSessionHeader = "Mcp-Session-Id"

func (s *Server) mcpNewSession(topics []string) (*mcpSession, error) {
	if s.opts.Hub == nil {
		return nil, errors.New("事件流未启用，无法建立 MCP 会话")
	}
	id := newSessionID()
	hubID, ch := s.opts.Hub.Subscribe(topics...)
	set := make(map[string]bool, len(topics))
	for _, t := range topics {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	sess := &mcpSession{id: id, topics: set, createdAt: time.Now(), ch: ch, hubID: hubID}
	s.mcpMu.Lock()
	s.mcpSessions[id] = sess
	s.mcpMu.Unlock()
	return sess, nil
}

func (s *Server) mcpGetSession(id string) *mcpSession {
	if id == "" {
		return nil
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	return s.mcpSessions[id]
}

func (s *Server) mcpDropSession(id string) bool {
	s.mcpMu.Lock()
	sess := s.mcpSessions[id]
	delete(s.mcpSessions, id)
	s.mcpMu.Unlock()
	if sess == nil {
		return false
	}
	if s.opts.Hub != nil {
		s.opts.Hub.Unsubscribe(sess.hubID)
	}
	return true
}

func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// mcp 处理 /mcp：
//   - POST   JSON-RPC 请求（请求/响应型工具、资源）
//   - GET    服务端推送通道（SSE，需 Mcp-Session-Id）
//   - DELETE 结束会话
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.mcpPost(w, r)
	case http.MethodGet:
		s.mcpStream(w, r)
	case http.MethodDelete:
		id := mcpSessionIDFrom(r)
		if id == "" || !s.mcpDropSession(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知或无会话"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "仅支持 POST / GET / DELETE"})
	}
}

func mcpSessionIDFrom(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(mcpSessionHeader)); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("sessionId"))
}

// mcpCallSummary 从 /mcp 请求体里提取调用日志用的摘要：
//
//	initialize / tools/list …        → 直接返回 JSON-RPC 方法名
//	tools/call（带 params.name）      → "tools/call=list_terminals"
//
// 解析失败返回空串（调用日志仍然会记录这次请求，只是没有方法名）。
func mcpCallSummary(body []byte) string {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		return ""
	}
	if req.Method == "tools/call" {
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err == nil && p.Name != "" {
			return req.Method + "=" + p.Name
		}
	}
	return req.Method
}

// mcpPost 处理 JSON-RPC 请求；initialize 成功时建立会话并返回 Mcp-Session-Id。
func (s *Server) mcpPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpBodyLimit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "读取请求体失败"})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", Error: &rpcErrorBody{Code: -32700, Message: "JSON 解析失败"}})
		return
	}
	// 通知（无 id）：执行副作用后返回 202
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	res := s.mcpDispatch(r.Context(), req)
	// initialize：建立会话，把会话 id 放进响应头（推送通道需要它）
	if req.Method == "initialize" && res.Error == nil {
		var topics []string
		if v := strings.TrimSpace(r.URL.Query().Get("topics")); v != "" {
			topics = strings.Split(v, ",")
		}
		if sess, err := s.mcpNewSession(topics); err == nil {
			w.Header().Set(mcpSessionHeader, sess.id)
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// mcpStream 建立 SSE 推送通道：把后端事件转成 JSON-RPC 通知推给客户端。
func (s *Server) mcpStream(w http.ResponseWriter, r *http.Request) {
	sess := s.mcpGetSession(mcpSessionIDFrom(r))
	if sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知或无会话：请先用 initialize 获取 " + mcpSessionHeader})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "当前环境不支持 SSE"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set(mcpSessionHeader, sess.id)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": mcp session "+sess.id+"\n\n")
	flusher.Flush()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev, open := <-sess.ch:
			if !open {
				return
			}
			note := map[string]any{
				"jsonrpc": "2.0",
				"method":  "notifications/message",
				"params": map[string]any{
					"level":  "info",
					"logger": "ding-ssh/" + ev.Topic,
					"data": map[string]any{
						"topic":     ev.Topic,
						"sessionId": ev.SessionID,
						"name":      ev.Name,
						"ts":        ev.TS,
						"uri":       eventResourceURI(ev),
						"payload":   json.RawMessage(ev.Data),
					},
				},
			}
			b, err := json.Marshal(note)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			flusher.Flush()
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// eventResourceURI 把事件映射到对应的 MCP 资源（终端输出 → 缓冲区资源），便于客户端失效缓存。
func eventResourceURI(ev Event) string {
	if ev.SessionID != "" && strings.HasPrefix(ev.Topic, "ssh.output") {
		return "terminal://" + ev.SessionID + "/buffer"
	}
	if strings.HasPrefix(ev.Topic, "tunnel") {
		return "app://state"
	}
	return ""
}

func (s *Server) mcpDispatch(ctx context.Context, req rpcRequest) rpcResponse {
	ok := func(v any) rpcResponse { return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: v} }
	fail := func(code int, msg string, data string) rpcResponse {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcErrorBody{Code: code, Message: msg, Data: data}}
	}

	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpProtocolVersion
		if p.ProtocolVersion == "2024-11-05" || p.ProtocolVersion == "2025-03-26" || p.ProtocolVersion == "2025-06-18" {
			version = p.ProtocolVersion
		}
		return ok(map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"subscribe": false, "listChanged": false},
			},
			"serverInfo": map[string]any{"name": mcpServerName, "version": mcpServerVersion},
			"instructions": "ding-ssh 调试控制面。先调 list_terminals 看终端状态；" +
				"发送命令用 send_input（\\r 结尾）；看输出用 read_terminal 或 recent_events；" +
				"screenshot 可拿到界面截图。危险操作（eval_js）需在应用设置里单独开启。",
		})

	case "ping":
		return ok(map[string]any{})

	case "tools/list":
		return ok(map[string]any{"tools": mcpTools()})

	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "tools/call 参数无效", err.Error())
		}
		content, err := s.mcpCallTool(ctx, p.Name, p.Arguments)
		if err != nil {
			return ok(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
				"isError": true,
			})
		}
		return ok(map[string]any{"content": content, "isError": false})

	case "resources/list":
		list := []any{
			map[string]any{"uri": "app://state", "name": "应用状态", "mimeType": "application/json"},
			map[string]any{"uri": "app://debug/info", "name": "调试模式信息", "mimeType": "application/json"},
			map[string]any{"uri": "app://servers", "name": "服务器列表（脱敏）", "mimeType": "application/json"},
			map[string]any{"uri": "app://logs", "name": "日志文件与日志设置", "mimeType": "application/json"},
			map[string]any{"uri": "app://logs/tail", "name": "最新日志末尾 200 行", "mimeType": "text/plain"},
		}
		if s.opts.Handler != nil {
			if v, err := s.opts.Handler.Handle(ctx, OpTerminals, nil); err == nil {
				if arr, isArr := v.([]any); isArr {
					for _, item := range arr {
						if m, isMap := item.(map[string]any); isMap {
							if id, hasID := m["clientId"].(string); hasID && id != "" {
								list = append(list, map[string]any{
									"uri": "terminal://" + id + "/buffer", "name": "终端缓冲区 " + id, "mimeType": "text/plain",
								})
							}
						}
					}
				}
			}
		}
		return ok(map[string]any{"resources": list})

	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "resources/read 参数无效", err.Error())
		}
		text, err := s.mcpReadResource(ctx, p.URI)
		if err != nil {
			return fail(-32602, err.Error(), p.URI)
		}
		return ok(map[string]any{"contents": []any{map[string]any{
			"uri": p.URI, "mimeType": mcpResourceMimeType(p.URI), "text": text,
		}}})

	case "prompts/list":
		return ok(map[string]any{"prompts": []any{}})
	}
	return fail(-32601, "不支持的方法: "+req.Method, "")
}

// mcpCallTool 执行工具调用，返回 MCP 的 content 数组。
func (s *Server) mcpCallTool(ctx context.Context, name string, args map[string]any) ([]any, error) {
	text := func(v any) []any {
		b, _ := json.MarshalIndent(v, "", "  ")
		return []any{map[string]any{"type": "text", "text": string(b)}}
	}
	raw, _ := json.Marshal(args)
	h := s.opts.Handler
	if h == nil {
		return nil, errors.New("调试服务未就绪")
	}
	call := func(op string, a json.RawMessage) ([]any, error) {
		v, err := h.Handle(ctx, op, a)
		if err != nil {
			return nil, err
		}
		return text(v), nil
	}

	switch name {
	case "app_state":
		return call(OpState, nil)
	case "list_terminals":
		return call(OpTerminals, nil)
	case "read_terminal":
		return call(OpTerminalBuffer, raw)
	case "send_input":
		return call(OpTerminalInput, raw)
	case "scroll_terminal":
		return call(OpTerminalScroll, raw)
	case "resize_terminal":
		return call(OpTerminalResize, raw)
	case "open_tab":
		return call(OpTabOpen, raw)
	case "close_tab":
		return call(OpTabClose, raw)
	case "reconnect_session":
		return call(OpSessionReconnect, raw)
	case "disconnect_session":
		return call(OpSessionDisconnect, raw)
	case "list_servers":
		return call(OpServers, nil)
	case "get_settings":
		return call(OpSettings, nil)
	case "update_settings":
		patch, has := args["settings"]
		if !has {
			return nil, errors.New("缺少 settings")
		}
		b, err := json.Marshal(patch)
		if err != nil {
			return nil, err
		}
		return call(OpSettingsUpdate, b)
	case "eval_js":
		return call(OpEval, raw)
	case "recent_events":
		if s.opts.Hub == nil {
			return nil, errors.New("事件流未启用")
		}
		limit := 50
		if v, has := args["limit"]; has {
			if f, isNum := v.(float64); isNum {
				limit = int(f)
			}
		}
		var topics []string
		if v, has := args["topics"]; has {
			if s, isStr := v.(string); isStr {
				topics = strings.Split(s, ",")
			}
		}
		events := s.opts.Hub.Recent(limit, topics...)
		return text(map[string]any{"count": len(events), "events": events}), nil
	case "list_logs":
		return call(OpLogs, nil)
	case "read_logs":
		return call(OpLogsTail, raw)
	case "export_diagnostics":
		return call(OpLogsExport, nil)
	case "clear_logs":
		return call(OpLogsClear, nil)
	case "set_log_options":
		return call(OpLogsOptions, raw)
	case "screenshot":
		png, err := captureWindowPNG()
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(png),
			"mimeType": "image/png",
		}}, nil
	}
	return nil, fmt.Errorf("未知工具: %s", name)
}

// mcpResourceMimeType 返回资源在 resources/read 响应里的 MIME 类型。
// 纯文本资源（日志尾部）用 text/plain；其余沿用既有行为（application/json）。
func mcpResourceMimeType(uri string) string {
	if uri == "app://logs/tail" {
		return "text/plain"
	}
	return "application/json"
}

// mcpReadResource 读取资源内容（JSON 文本）。
func (s *Server) mcpReadResource(ctx context.Context, uri string) (string, error) {
	h := s.opts.Handler
	if h == nil {
		return "", errors.New("调试服务未就绪")
	}
	render := func(v any, err error) (string, error) {
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b), nil
	}
	switch {
	case uri == "app://state":
		return render(h.Handle(ctx, OpState, nil))
	case uri == "app://debug/info":
		return render(h.Handle(ctx, OpHealth, nil))
	case uri == "app://servers":
		return render(h.Handle(ctx, OpServers, nil))
	case uri == "app://logs":
		return render(h.Handle(ctx, OpLogs, nil))
	case uri == "app://logs/tail":
		// 最新日志最后 200 行，纯文本；与 read_logs 共用同一个 op。
		args, _ := json.Marshal(map[string]any{"lines": 200})
		v, err := h.Handle(ctx, OpLogsTail, args)
		if err != nil {
			return "", err
		}
		if m, isMap := v.(map[string]any); isMap {
			if t, hasText := m["text"].(string); hasText {
				return t, nil
			}
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b), nil
	case strings.HasPrefix(uri, "terminal://"):
		rest := strings.TrimPrefix(uri, "terminal://")
		id := strings.TrimSuffix(rest, "/buffer")
		if id == "" {
			return "", errors.New("资源 uri 缺少终端 id")
		}
		args, _ := json.Marshal(map[string]any{"id": id, "from": "tail", "lines": 500})
		v, err := h.Handle(ctx, OpTerminalBuffer, args)
		if err != nil {
			return "", err
		}
		if m, isMap := v.(map[string]any); isMap {
			if t, hasText := m["text"].(string); hasText {
				return t, nil
			}
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b), nil
	}
	return "", fmt.Errorf("未知资源: %s", uri)
}
