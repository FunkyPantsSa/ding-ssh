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

// Executor 由宿主实现：执行一次已经过两段式确认的动作（见 confirm.go）。
//
// 为什么单独一个接口：确认只解决「用户同意了吗」，真正改状态的动作（删服务器、
// 清日志、退出应用、开能力位）必须由宿主来做 —— debugsrv 不认识 models/store。
// 约定：action 名见 ConfirmableActions()，args 为规范化后的参数（不含 token）。
type Executor interface {
	ExecuteAction(ctx context.Context, action string, args map[string]any) (any, error)
}

// 操作名。HTTP 路由 → op 的映射见 Start()。
const (
	// OpHealth 调试服务自身的健康/发现信息（含 token，供本机调用方自动发现）。
	OpHealth = "health"
	// OpStatus 应用健康信息（app.health）：比 OpHealth 多出 uptime / 数据目录 / 能力位快照，
	// 且 token 已打码（OpHealth 保留原样以兼容既有 /v1/health 与 app://debug/info）。
	OpStatus            = "status"
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

	// ---- M7 应用控制面（实现见 appctl.go）----
	//
	// 与既有 op 命名的关系：MCP 工具名即 op 名（见 ui.go 的同名约定），
	// 但有一个例外需要单列：settings.reset 与 settings.update 同域、语义完全不同
	//（重置 vs 合并），因此单列一个 op，绝不复用 OpSettingsUpdate。
	//
	// 两个特殊工具（常量仍列出来 —— 工具表、注册表、命令规则都用它）：
	//   - servers.list：有自己的 op（宿主返回**未脱敏**的节点列表，脱敏统一在 appctl.go 侧做，
	//     避免宿主与 debugsrv 两处各抹一次、规则不一致）；
	//   - servers.export：**没有 op** —— 导出文档完全在 debugsrv 侧组装（读 servers.list 后
	//     脱敏 + 字节限额），因此不需要宿主实现。
	OpServersList       = "servers.list"
	OpServersExport     = "servers.export"
	OpServersGet        = "servers.get"
	OpServersCreate     = "servers.create"
	OpServersUpdate     = "servers.update"
	OpServersDelete     = "servers.delete"
	OpServersDuplicate  = "servers.duplicate"
	OpServersMoveGroup  = "servers.moveGroup"
	OpServersImport     = "servers.import"
	OpCredentialsList   = "credentials.list"
	OpCredentialsCreate = "credentials.create"
	OpCredentialsUpdate = "credentials.update"
	OpCredentialsDelete = "credentials.delete"
	OpCredentialsAttach = "credentials.attach"
	OpTunnelsList       = "tunnels.list"
	OpTunnelsCreate     = "tunnels.create"
	OpTunnelsUpdate     = "tunnels.update"
	OpTunnelsDelete     = "tunnels.delete"
	OpTunnelsStart      = "tunnels.start"
	OpTunnelsStop       = "tunnels.stop"
	OpSettingsSchema    = "settings.schema"
	OpSettingsReset     = "settings.reset"
	OpAppCommands       = "app.commands"
	OpAppCommand        = "app.command"
	OpAppQuit           = "app.quit"
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

	// ---- 权限内核 / 确认 / 审计（本包新增，均可为 nil）----
	//
	// Gate 能力校验：为 nil 时按 denyAllGate 处理（只允许 read），
	// 即「宿主忘记注入」不会变成「全部放开」。
	Gate Gate
	// Confirms 两段式确认管理器：为 nil 时所有需要确认的操作一律拒绝并提示先 prepare。
	Confirms *ConfirmManager
	// Audits 审计环形缓冲：为 nil 时不记录（[api] 日志仍然可用）。
	Audits *AuditLog
	// Executor 执行已确认的动作（confirm.commit / 写工具携带 token 时由本包调用）。
	// 为 nil 时 confirm.commit 返回中文错误「宿主未实现动作执行器」。
	Executor Executor

	// ---- M8（SFTP + 终端自动化）----
	//
	// RemoteFSPaths 远端写白名单的来源（设置 → 调试模式 → MCP 能力 → 白名单）。
	// 为 nil 时按「空白名单 = 拒绝一切远端写入」处理（最保守，与 caps.go 的 denyAllGate 同一思路）。
	// 为什么单独注入而不是复用 Gate：Gate 一次只能校验一个路径（rename 有两端），
	// 而白名单规则（空前缀 / .. 穿越 / 路径段边界）必须能在没有宿主的单测里完整覆盖。
	RemoteFSPaths RemoteFSPathScope
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

	// M8：终端自动化的「同会话串行化」状态（见 sftp.go 的 acquireTerminal）。
	// 键是终端 / 会话 id；同一时刻只允许一个 terminal.run / terminal.expect 在跑，
	// 第二个调用被直接拒绝（不排队），避免两个调用互相偷走对方的输出增量。
	termMu   sync.Mutex
	termBusy map[string]bool
}

// New 创建调试服务。
func New(o Options) *Server {
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return &Server{opts: o, mcpSessions: make(map[string]*mcpSession), termBusy: make(map[string]bool)}
}

// Start 启动监听并返回实际监听地址（端口填 0 时由系统分配）。
func (s *Server) Start() (string, error) {
	if s.opts.Handler == nil {
		return "", errors.New("debugsrv: Handler 不能为空")
	}
	mux := http.NewServeMux()

	// 只读：read 能力永远允许，因此不套 guard（审计由 auditLog 中间件统一记账，
	// tool 名由 s.named 标注，与 MCP 工具同名，便于 app.recent_calls 统一过滤）。
	read := func(pattern string, tool string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.named(tool, h))
	}
	write := func(pattern string, route httpRoute, h http.HandlerFunc) {
		if route.cap == "" {
			route.cap = CapRead
		}
		mux.HandleFunc(pattern, s.guarded(route, h))
	}

	read("GET /v1/health", "app.health", s.op(OpHealth))
	read("GET /v1/state", "app_state", s.op(OpState))
	read("GET /v1/terminals", "list_terminals", s.op(OpTerminals))
	read("GET /v1/sessions", "list_sessions", s.op(OpSessions))
	read("GET /v1/terminals/{id}/buffer", "read_terminal", s.opWithID(OpTerminalBuffer))
	read("GET /v1/settings", "get_settings", s.op(OpSettings))
	read("GET /v1/servers", "list_servers", s.op(OpServers))
	read("GET /v1/logs", "list_logs", s.op(OpLogs))
	read("GET /v1/logs/tail", "read_logs", s.op(OpLogsTail))
	// ---- M9：工具目录（GET /v1/tools）----
	// 返回全部工具的 {name, capability[], confirmAction?, needsToken, description, inputSchema,
	// hasStructuredOutput, outputSchema?}，与 MCP tools/list 同源。
	// 这里没有用 read() 标注审计工具名：它不是某个 MCP 工具，而是「工具目录」本身，
	// 审计记录里保持 "GET /v1/tools" 更诚实（避免伪造一个不存在的工具名）。
	// 鉴权仍然沿用同一个 Bearer token（auth 中间件包住整张 mux，不做读类免鉴权的例外）。
	mux.HandleFunc("GET /v1/tools", s.toolsHTTP)
	// 截图单独返回 PNG（审计由 auditLog 中间件按 GET 记录）
	mux.HandleFunc("GET /v1/screenshot", s.screenshot)

	// 写：每条写类路由声明「所需能力 + 确认 action + 审计用工具名」，
	// 由 guarded 统一做 Gate 校验、两段式确认与审计登记（见 guarded 的说明）。
	//
	// action 字段只对**破坏性 / 不可逆**路由生效（confirmActionNeedsConfirm 判定）；
	// 可逆 / 高频路由（terminal.input、eval）即使写了 action 也不会要求 token ——
	// 保留字段只为「路由的主语义」自解释，避免读者以为它们不受任何校验（能力位仍然校验）。
	//
	// 注意 HTTP 与 MCP 的确认语义差别：HTTP 调用方是脚本 / 人，token 走
	// ?confirmToken=…（见 httpConfirmToken）；MCP 调用方是 AI，token 走工具参数
	// token=…（见 mcp.go）。两者共用同一个 ConfirmManager，因此 token 可以互相使用。
	write("POST /v1/terminals/{id}/input", httpRoute{
		cap: CapTerminalInput, action: string(CapTerminalInput), tool: "send_input"}, s.opWithID(OpTerminalInput))
	write("POST /v1/terminals/{id}/scroll", httpRoute{tool: "scroll_terminal"}, s.opWithID(OpTerminalScroll))
	write("POST /v1/terminals/{id}/resize", httpRoute{tool: "resize_terminal"}, s.opWithID(OpTerminalResize))
	write("POST /v1/sessions/{id}/reconnect", httpRoute{tool: "reconnect_session"}, s.opWithID(OpSessionReconnect))
	write("POST /v1/sessions/{id}/disconnect", httpRoute{tool: "disconnect_session"}, s.opWithID(OpSessionDisconnect))
	write("POST /v1/tabs", httpRoute{tool: "open_tab"}, s.op(OpTabOpen))
	write("DELETE /v1/tabs/{id}", httpRoute{tool: "close_tab"}, s.opWithID(OpTabClose))
	write("POST /v1/eval", httpRoute{
		cap: CapEval, action: string(CapEval), tool: "eval_js"}, s.op(OpEval))
	write("PUT /v1/settings", httpRoute{
		cap: CapConfigWrite, action: "settings.update", tool: "update_settings"}, s.op(OpSettingsUpdate))
	// 日志：清空是破坏性操作（cap=lifecycle，与 MCP 的 clear_logs 保持一致），
	// 调整日志开关属于 config.write；导出诊断包只读磁盘，不需要能力位。
	write("POST /v1/logs/clear", httpRoute{
		cap: CapLifecycle, action: "logs.clear", tool: "clear_logs"}, s.op(OpLogsClear))
	write("POST /v1/logs/export", httpRoute{tool: "export_diagnostics"}, s.op(OpLogsExport))
	write("POST /v1/logs/options", httpRoute{
		cap: CapConfigWrite, action: "settings.update", tool: "set_log_options"}, s.op(OpLogsOptions))
	// 实时事件流（SSE）：AI 可以"看着"终端输出而不必轮询
	mux.HandleFunc("GET /v1/stream", s.stream)
	// 最近事件（无需订阅，直接取环形缓冲）
	mux.HandleFunc("GET /v1/events", s.events)
	// ---- UI 观测 / 实验域（M5+M6）：路由、能力位与观察器都在 ui.go 里集中注册 ----
	registerUIRoutes(mux, s)
	// MCP（Streamable HTTP）：把上述操作暴露为 MCP 工具/资源，供任意 AI 客户端接入
	mux.HandleFunc("/mcp", s.mcp)

	s.srv = &http.Server{
		// apiLog 放在最外层：鉴权失败的请求（401）同样要留下调用记录。
		// auditLog 紧贴它之内：MCP 的工具调用由 mcp.go 自己记账（那里信息更全，
		// 含能力位与是否可撤销），这里只负责非 /mcp 的 HTTP 路由。
		Handler:           s.apiLog(s.auditLog(s.auth(mux))),
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

// httpRoute 一条 HTTP 路由的权限/审计元信息。
//
// 为什么把元信息写在路由表里（而不是散落在 dispatch 的 if 里）：
// 这样「哪条路由需要哪个能力、要不要二次确认、审计里记成什么工具名」一眼可见，
// 新增路由时只需在同一行补全三件事，不会漏掉校验。
type httpRoute struct {
	cap    Capability // 所需能力（空 = read，永远允许）
	action string     // 需要两段式确认时的 action（空 = 不需要确认）
	tool   string     // 审计记录里的工具名（与 MCP 工具同名，便于统一过滤）
}

// auditInfo 由 guarded / named 写入请求上下文，供 auditLog 中间件在响应后记账。
type auditInfo struct {
	Tool   string
	Cap    string
	Args   map[string]any
	ErrMsg string // 守卫层产生的失败原因（例如能力被拒 / 缺少确认 token）
	Action string // 确认 action（审计里 cap 与 action 相对应）
}

type auditCtxKey struct{}

// withAuditInfo 把一次调用的审计信息挂到请求上下文（后写入的覆盖先写入的）。
func withAuditInfo(r *http.Request, info *auditInfo) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), auditCtxKey{}, info))
}

// auditInfoFrom 取出请求上下文里的审计信息（可能为 nil）。
func auditInfoFrom(r *http.Request) *auditInfo {
	v, _ := r.Context().Value(auditCtxKey{}).(*auditInfo)
	return v
}

// named 给读类路由标注审计用的工具名（不改变行为，只写入上下文供中间件记账）。
func (s *Server) named(tool string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 复用中间件预先放好的指针（没有则自己放一个），保证外层能读到。
		info := auditInfoFrom(r)
		if info == nil {
			info = &auditInfo{}
			r = withAuditInfo(r, info)
		}
		info.Tool = tool
		info.Args = queryArgs(r)
		next(w, r)
	}
}

// guarded 给写类路由套上「能力校验 + 两段式确认 + 审计登记」。
//
// 顺序很重要：先查能力位（没开的能力直接 403），再查 token（能力开了但没确认也拒绝），
// 两者都通过才交给真正的 handler。被拒时返回 403 + 中文原因，AI 能直接读懂要开什么。
func (s *Server) guarded(route httpRoute, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		args, _ := s.readArgsQuietly(r)
		info := auditInfoFrom(r)
		if info == nil {
			info = &auditInfo{}
			r = withAuditInfo(r, info)
		}
		info.Tool, info.Cap, info.Args, info.Action = route.tool, string(route.cap), args, route.action

		if route.cap != "" && route.cap != CapRead {
			if err := s.allowCap(route.cap, httpDetail(args)); err != nil {
				info.ErrMsg = err.Error()
				writeJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
				return
			}
		}
		if route.action != "" && confirmActionNeedsConfirm(route.action) {
			token := httpConfirmToken(r)
			if token == "" {
				info.ErrMsg = confirmRequiredMessage(route.action)
				writeJSON(w, http.StatusForbidden, map[string]any{"error": info.ErrMsg, "action": route.action})
				return
			}
			// 确认绑定的参数形状必须与 confirm.prepare 时一致：设置类路由用
			// {"settings": {...}} 包一层（见 confirmRouteArgs），否则 AI 先 prepare
			// 再用 HTTP 带 token 调用会「参数不一致」。
			if _, err := s.takeConfirm(token, route.action, confirmRouteArgs(route.action, args)); err != nil {
				info.ErrMsg = err.Error()
				writeJSON(w, http.StatusForbidden, map[string]any{"error": err.Error(), "action": route.action})
				return
			}
		}
		next(w, r)
	}
}

// confirmRouteArgs 把 HTTP 路由解析出来的参数整理成「与 confirm.prepare 一致」的形状。
//
// 背景：MCP 的 update_settings 工具要求调用方传 {"settings": {...}}，
// 而 HTTP 是直接 PUT 一份设置补丁（{"uiScale":120}）。两者语义相同、形状不同，
// 若不做归一会导致「AI prepare 成功、HTTP commit 报参数不一致」。
// 其他动作（terminal.input / eval / logs.clear）本身就是同一个对象，直接返回。
func confirmRouteArgs(action string, args map[string]any) map[string]any {
	switch action {
	case "settings.update", "settings.reset":
		return SettingsPatchArgs(args)
	default:
		return args
	}
}

// takeConfirm 消费一次确认 token，并保证「未注入 Confirms」时给出可读的中文错误。
func (s *Server) takeConfirm(token, action string, args map[string]any) (PendingConfirm, error) {
	if s.opts.Confirms == nil {
		return PendingConfirm{}, errors.New("确认管理器未启用：请先在应用内开启调试模式")
	}
	return s.opts.Confirms.Take(token, action, args)
}

// confirmRequiredMessage 缺少 token 时的标准中文提示（HTTP 与 MCP 共用同一句话）。
//
// 措辞特意点明「不可逆」：可逆 / 高频操作不需要 token（见 confirm.go 的设计原则），
// 否则 AI 会误以为所有写操作都要走确认。
func confirmRequiredMessage(action string) string {
	return fmt.Sprintf("该操作（%s）属于破坏性 / 不可逆操作，需要两段式确认：请先调用 confirm.prepare 获取 token，"+
		"再把 token 作为参数重新调用本操作（可逆 / 高频操作如 send_input、eval_js 不需要 token）。", action)
}

// httpDetail 给能力校验提供补充信息（能力被拒时用于提示「写哪个路径」）。
func httpDetail(args map[string]any) string {
	for _, k := range []string{"path", "remotePath", "newPath", "remote", "dir"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// httpConfirmToken 从 query（token）或请求体（confirmToken）里取确认 token。
//
// 为什么不复用请求体里的 token：请求体的 token 是调试服务鉴权用的（?token= 也可以），
// 与「确认 token」语义不同；这里显式用 confirmToken 字段，避免两者混淆。
func httpConfirmToken(r *http.Request) string {
	if v := strings.TrimSpace(r.URL.Query().Get("confirmToken")); v != "" {
		return v
	}
	return ""
}

// readArgsQuietly 在不打扰 handler 的前提下解析出这次请求的参数（用于审计与确认绑定）。
//
// 实现上直接复用 buildArgs：这样「审计里看到的参数」「confirm 绑定的参数」
// 与「handler 实际收到的参数」永远是同一份，不会出现三者不一致的诡异问题。
// 注意 buildArgs 不带 {id} 路径参数（那是 opWithID 在 dispatch 时才补的），
// 因此对带 id 的路由，args 里可能没有 id —— 这没关系：确认动作的绑定以调用方
// 显式传的参数为准，路径参数属于同一资源定位信息，不参与哈希。
func (s *Server) readArgsQuietly(r *http.Request) (map[string]any, error) {
	raw, err := s.buildArgs(r, "")
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// auditLog 审计中间件：为非 /mcp 的请求留下一条结构化记录。
//
// /mcp 由 mcp.go 自己记账（那里能拿到工具名与 args，信息更全），因此这里直接跳过，
// 避免一次调用记两条。
//
// 关键实现细节：中间件先放一个空的 *auditInfo 进上下文，内层 handler（guarded / named）
// 再把工具名 / 能力位 / 参数填进同一个指针。**必须是指针**：handler 用 r.WithContext
// 生成新请求后，外层拿到的仍是旧请求，只有共享同一个指针才能把信息传回来。
func (s *Server) auditLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Audits == nil || r.URL.Path == "/mcp" {
			next.ServeHTTP(w, r)
			return
		}
		// 先给一个兜底信息（工具名 = 方法 + 路径），内层 handler 会覆盖成与 MCP 同名的工具名。
		info := &auditInfo{Tool: r.Method + " " + r.URL.Path, Args: queryArgs(r)}
		r = withAuditInfo(r, info)

		start := time.Now()
		lw := &logResponseWriter{ResponseWriter: w}
		next.ServeHTTP(lw, r)
		status := lw.statusCode()
		var err error
		switch {
		case info.ErrMsg != "":
			err = errors.New(info.ErrMsg)
		case status >= 400:
			err = fmt.Errorf("HTTP %d", status)
		}
		reversible, hint := auditRevertHint(info.Tool, info.Args)
		rec := AuditRecord{
			TS:         start.UnixMilli(),
			Source:     "http",
			Tool:       info.Tool,
			Cap:        info.Cap,
			Args:       NormalizeArgsJSON(info.Args),
			OK:         err == nil,
			DurationMS: time.Since(start).Milliseconds(),
		}
		// 只有成功且确实可撤销的才带撤销提示（失败的操作没有「改动前」可恢复）
		if rec.OK && reversible {
			rec.Reversible = true
			rec.RevertHint = hint
		}
		if err != nil {
			rec.Error = err.Error()
		}
		out := s.opts.Audits.Append(rec)
		publishAudit(s.opts.Hub, out)
	})
}

// queryArgs 把查询参数收集成 map（读类请求的审计参数）。
// 调试服务自身的 token 不记录；其余取值统一过一遍脱敏（防止脚本把凭据写在 query 里）。
func queryArgs(r *http.Request) map[string]any {
	q := r.URL.Query()
	if len(q) == 0 {
		return nil
	}
	out := make(map[string]any, len(q))
	for k, vs := range q {
		if k == "token" || k == "confirmToken" || len(vs) == 0 {
			continue
		}
		out[k] = RedactArgsJSON(vs[0])
	}
	return out
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
		// token / confirmToken 都是控制面自身的凭据，不能混进业务参数：
		// token 是调试服务鉴权，confirmToken 是「本次破坏性操作已获确认」的一次性凭据。
		if k == "token" || k == "confirmToken" || len(vs) == 0 {
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
		// 读完放回去：guarded 里的能力校验 / 确认绑定会先解析一次参数，
		// 真正的 handler（dispatch）随后还要再读一次；不放回的话请求体会变空。
		r.Body = io.NopCloser(bytes.NewReader(body))
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
