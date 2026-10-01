// Package debugsrv 实现「调试模式」：应用内置的本地 HTTP 控制面，供 AI / 自动化调用。
//
// 设计要点：
//   - 只做传输与鉴权，具体能力由宿主实现的 Handler 提供（后端直连 + 前端桥）；
//   - 默认只监听 127.0.0.1，且每次启动生成随机 token（见 Options.Token）；
//   - 路由与操作名（Op*）一一对应，后续 MCP 适配层复用同一套 op。
package debugsrv

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Handler 由宿主实现：把一次操作翻译成对后端 / 前端的调用。
// op 取值见 Op* 常量，args 为 JSON（可为 nil），返回值会被序列化为 JSON。
type Handler interface {
	Handle(ctx context.Context, op string, args json.RawMessage) (any, error)
}

// 操作名。HTTP 路由 → op 的映射见 Start()。
const (
	OpHealth            = "health"
	OpState             = "state"
	OpTerminals         = "terminals"
	OpTerminalBuffer    = "terminal.buffer"
	OpTerminalInput     = "terminal.input"
	OpTerminalScroll    = "terminal.scroll"
	OpTerminalResize    = "terminal.resize"
	OpSessions          = "sessions"
	OpSessionReconnect  = "session.reconnect"
	OpSessionDisconnect = "session.disconnect"
	OpTabOpen           = "tab.open"
	OpTabClose          = "tab.close"
	OpEval              = "eval"
	OpSettings          = "settings"
	OpSettingsUpdate    = "settings.update"
	OpServers           = "servers"
	OpScreenshot        = "screenshot"
	// 日志：查看（列表 / 尾部）、清空、导出诊断包、调整日志开关
	OpLogs        = "logs"
	OpLogsTail    = "logs.tail"
	OpLogsClear   = "logs.clear"
	OpLogsExport  = "logs.export"
	OpLogsOptions = "logs.options"
)

// 供 Handler 返回的可识别错误（映射为 HTTP 状态码）。
var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrBadInput  = errors.New("bad request")
)

// Options 服务器配置。
type Options struct {
	Addr    string // 监听地址，如 127.0.0.1:8765 / 0.0.0.0:8765
	Token   string // 启动时随机生成；空字符串表示不鉴权（仅用于本地测试）
	Handler Handler
	Hub     *Hub // 事件总线（用于 GET /v1/stream 的 SSE 实时流），可为 nil
	Logf    func(format string, args ...any)
	// APILogf 调用日志输出（每条 HTTP 请求一行，见 apiLog）；为 nil 时不记录。
	// 由宿主注入（通常是 logx.APILogf 的脱敏封装），调试服务本身不关心日志实现。
	APILogf func(format string, args ...any)
}

// Server 调试 HTTP 服务。
type Server struct {
	opts Options
	srv  *http.Server
	ln   net.Listener
	mu   sync.Mutex

	// MCP 会话（支持服务端推送的客户端用 GET /mcp 挂 SSE 通道）
	mcpMu       sync.Mutex
	mcpSessions map[string]*mcpSession
}

// New 创建调试服务。
func New(o Options) *Server {
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return &Server{opts: o, mcpSessions: make(map[string]*mcpSession)}
}

// Start 启动监听并返回实际监听地址（端口填 0 时由系统分配）。
func (s *Server) Start() (string, error) {
	if s.opts.Handler == nil {
		return "", errors.New("debugsrv: Handler 不能为空")
	}
	mux := http.NewServeMux()

	// 只读
	mux.HandleFunc("GET /v1/health", s.op(OpHealth))
	mux.HandleFunc("GET /v1/state", s.op(OpState))
	mux.HandleFunc("GET /v1/terminals", s.op(OpTerminals))
	mux.HandleFunc("GET /v1/sessions", s.op(OpSessions))
	mux.HandleFunc("GET /v1/terminals/{id}/buffer", s.opWithID(OpTerminalBuffer))
	// 写
	mux.HandleFunc("POST /v1/terminals/{id}/input", s.opWithID(OpTerminalInput))
	mux.HandleFunc("POST /v1/terminals/{id}/scroll", s.opWithID(OpTerminalScroll))
	mux.HandleFunc("POST /v1/terminals/{id}/resize", s.opWithID(OpTerminalResize))
	mux.HandleFunc("POST /v1/sessions/{id}/reconnect", s.opWithID(OpSessionReconnect))
	mux.HandleFunc("POST /v1/sessions/{id}/disconnect", s.opWithID(OpSessionDisconnect))
	mux.HandleFunc("POST /v1/tabs", s.op(OpTabOpen))
	mux.HandleFunc("DELETE /v1/tabs/{id}", s.opWithID(OpTabClose))
	mux.HandleFunc("POST /v1/eval", s.op(OpEval))
	mux.HandleFunc("GET /v1/settings", s.op(OpSettings))
	mux.HandleFunc("PUT /v1/settings", s.op(OpSettingsUpdate))
	mux.HandleFunc("GET /v1/servers", s.op(OpServers))
	mux.HandleFunc("GET /v1/screenshot", s.screenshot)
	// 日志：先看日志在哪/有哪些（logs），再按级别/关键字看尾部（logs.tail），
	// 需要用户配合时导出诊断包（logs.export），必要时清空（logs.clear）或调整开关（logs.options）
	mux.HandleFunc("GET /v1/logs", s.op(OpLogs))
	mux.HandleFunc("GET /v1/logs/tail", s.op(OpLogsTail))
	mux.HandleFunc("POST /v1/logs/clear", s.op(OpLogsClear))
	mux.HandleFunc("POST /v1/logs/export", s.op(OpLogsExport))
	mux.HandleFunc("POST /v1/logs/options", s.op(OpLogsOptions))
	// 实时事件流（SSE）：AI 可以"看着"终端输出而不必轮询
	mux.HandleFunc("GET /v1/stream", s.stream)
	// 最近事件（无需订阅，直接取环形缓冲）
	mux.HandleFunc("GET /v1/events", s.events)
	// MCP（Streamable HTTP）：把上述操作暴露为 MCP 工具/资源，供任意 AI 客户端接入
	mux.HandleFunc("/mcp", s.mcp)

	s.srv = &http.Server{
		// apiLog 放在最外层：鉴权失败的请求（401）同样要留下调用记录。
		Handler:           s.apiLog(s.auth(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.opts.Logf("调试服务退出: %v", err)
		}
	}()
	s.opts.Logf("调试服务已启动: http://%s", ln.Addr().String())
	return ln.Addr().String(), nil
}

// Stop 关闭监听。
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// apiLog 调用日志中间件：每次请求记录一行
//
//	<method> <path> [<JSON-RPC 摘要>] <status> <耗时>ms <响应字节>b
//
// （[api] 前缀由宿主的日志实现补上，见 Options.APILogf）。
// SSE 长连接（GET /v1/stream、GET /mcp）只记录开始与结束两行：
// 这类连接可能持续数小时，逐条推送都落盘会把日志淹掉，也拖慢 SSE 推送。
func (s *Server) apiLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.APILogf == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		// 请求细节（MCP 的 JSON-RPC 方法 / 工具名）必须在 handler 之前取：
		// 请求体只有第一次能读，requestDetail 会把它放回去。
		detail := s.requestDetail(r)
		lw := &logResponseWriter{ResponseWriter: w}

		if isStreamRequest(r) {
			s.opts.APILogf("%s %s%s sse=start", r.Method, r.URL.Path, detail)
			next.ServeHTTP(lw, r)
			s.opts.APILogf("%s %s%s %d sse=end %dms %db",
				r.Method, r.URL.Path, detail, lw.statusCode(), time.Since(start).Milliseconds(), lw.bytes)
			return
		}
		next.ServeHTTP(lw, r)
		s.opts.APILogf("%s %s%s %d %dms %db",
			r.Method, r.URL.Path, detail, lw.statusCode(), time.Since(start).Milliseconds(), lw.bytes)
	})
}

// isStreamRequest 判断是否为 SSE 长连接请求。
func isStreamRequest(r *http.Request) bool {
	if r.URL.Path == "/v1/stream" {
		return true
	}
	// GET /mcp 是 MCP 的服务端推送通道；POST/DELETE 仍按普通请求记录。
	return r.URL.Path == "/mcp" && r.Method == http.MethodGet
}

// requestDetail 提取附加到调用日志的请求细节（目前只有 MCP 的 JSON-RPC 方法/工具名）。
// 返回空串表示没有额外信息；日志里形如 " tools/call=list_terminals"。
func (s *Server) requestDetail(r *http.Request) string {
	if r.Method != http.MethodPost || r.URL.Path != "/mcp" || r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpBodyLimit))
	if err != nil {
		return ""
	}
	// 读过的请求体放回去，否则 mcpPost 拿到的是空 body。
	r.Body = io.NopCloser(bytes.NewReader(body))
	if summary := mcpCallSummary(body); summary != "" {
		return " " + summary
	}
	return ""
}

// logResponseWriter 记录状态码与响应字节数。
// 必须透传 Flush：SSE 处理器会对 ResponseWriter 做 http.Flusher 断言，
// 包装后断言失败会让 /v1/stream 与 GET /mcp 直接报错。
type logResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

// WriteHeader 记录首个状态码（与 net/http 一致，重复调用只生效一次）。
func (w *logResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write 记录响应字节数（未显式写头时按 200 处理）。
func (w *logResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// Flush 透传底层 Flusher，保证 SSE 推送不被中间件破坏。
func (w *logResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap 让 http.ResponseController 能取到原始 ResponseWriter。
func (w *logResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// statusCode 返回响应状态码，handler 未写任何内容时视为 200。
func (w *logResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// auth 校验 Bearer token（也允许 ?token= 便于浏览器/脚本直接访问）。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Token != "" {
			tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if tok == "" {
				tok = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.Token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// op 构造 basePath 上的处理器（不带路径参数）。
func (s *Server) op(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.dispatch(w, r, name, "")
	}
}

// opWithID 构造带 {id} 路径参数的处理器，id 会以 args.id 传入。
func (s *Server) opWithID(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.dispatch(w, r, name, r.PathValue("id"))
	}
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, op, id string) {
	args, err := s.buildArgs(r, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	res, err := s.opts.Handler.Handle(r.Context(), op, args)
	if err != nil {
		writeJSON(w, statusFor(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// buildArgs 把 URL 路径参数 / 查询参数 / JSON body 合并成一次操作的入参。
func (s *Server) buildArgs(r *http.Request, id string) (json.RawMessage, error) {
	args := map[string]any{}
	if id != "" {
		args["id"] = id
	}
	for k, vs := range r.URL.Query() {
		if k == "token" || len(vs) == 0 {
			continue
		}
		v := vs[0]
		if n, err := strconv.Atoi(v); err == nil {
			args[k] = n
		} else if b, err := strconv.ParseBool(v); err == nil {
			args[k] = b
		} else {
			args[k] = v
		}
	}
	if r.Body != nil && r.Method != http.MethodGet {
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			return nil, fmt.Errorf("读取请求体失败: %w", err)
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			var extra map[string]any
			if err := json.Unmarshal(body, &extra); err != nil {
				return nil, fmt.Errorf("请求体不是合法 JSON: %w", err)
			}
			for k, v := range extra {
				args[k] = v
			}
		}
	}
	if len(args) == 0 {
		return nil, nil
	}
	return json.Marshal(args)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrBadInput):
		return http.StatusBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// stream SSE 实时事件流：订阅后端事件（会话输出/状态/进度/隧道）并持续推送。
// 用法：GET /v1/stream?topics=ssh.output,ssh.status&token=...
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	if s.opts.Hub == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "事件流未启用"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "当前环境不支持 SSE"})
		return
	}
	var topics []string
	if v := strings.TrimSpace(r.URL.Query().Get("topics")); v != "" {
		topics = strings.Split(v, ",")
	}
	id, ch := s.opts.Hub.Subscribe(topics...)
	defer s.opts.Hub.Unsubscribe(id)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return
			}
			body, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Topic, body)
			flusher.Flush()
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// events 返回最近的后端事件（环形缓冲），供不想订阅 SSE 的调用方使用。
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if s.opts.Hub == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "事件流未启用"})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	var topics []string
	if v := strings.TrimSpace(r.URL.Query().Get("topics")); v != "" {
		topics = strings.Split(v, ",")
	}
	events := s.opts.Hub.Recent(limit, topics...)
	writeJSON(w, http.StatusOK, map[string]any{"count": len(events), "events": events})
}

// screenshot 返回应用窗口的 PNG 截图（平台相关实现；用于 AI 目视校验界面）。
func (s *Server) screenshot(w http.ResponseWriter, r *http.Request) {
	data, err := captureWindowPNG()
	if err != nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
