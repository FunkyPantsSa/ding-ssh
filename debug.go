package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/models"
	"ding-ssh/internal/sshx"
	"ding-ssh/internal/store"

	"github.com/pkg/sftp"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DebugBoot 启动期就需要的调试模式信息。
// WebView2 的远程调试端口必须在 wails.Run 之前确定，因此这部分设置会在启动前先读一次。
type DebugBoot struct {
	Enabled      bool
	Port         int
	BindLAN      bool
	AllowEval    bool
	AllowSecrets bool
	CDPEnabled   bool
	CDPPort      int
	Token        string

	// 能力位：运行期由设置存储实时读取（见 App.currentDebugSettings），
	// 这里只作为「存储不可用」时的兜底快照。
	CapTerminalInput bool
	CapUIWrite       bool
	CapConfigWrite   bool
	CapSecretWrite   bool
	CapRemoteFSWrite bool
	CapSudoCredential bool
	CapLifecycle     bool
}

// DebugInfoPath 调试信息文件：端口 / token / CDP 端口，供 AI 与脚本自动发现。
func DebugInfoPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ding-ssh", "debug.json")
}

// readDebugBoot 在 wails.Run 之前读一次设置（此时 startup 尚未初始化存储层）。
func readDebugBoot() DebugBoot {
	d := models.DefaultDebugSettings()
	if db, err := store.OpenSQLite(store.DefaultSQLitePath()); err == nil {
		if st, gerr := store.NewSQLiteSettingsStore(db).Get(); gerr == nil {
			d = models.NormalizeDebugSettings(st.Debug)
		}
		_ = db.Close()
	} else if js, jerr := store.NewJSONSettingsStore(store.DefaultSettingsPath()); jerr == nil {
		if st, gerr := js.Get(); gerr == nil {
			d = models.NormalizeDebugSettings(st.Debug)
		}
	}
	boot := DebugBoot{
		Enabled:      d.Enabled,
		Port:         d.Port,
		BindLAN:      d.BindLAN,
		AllowEval:    d.AllowEval,
		AllowSecrets: d.AllowSecrets,
		CDPEnabled:   d.CDPEnabled,

		CapTerminalInput: d.CapTerminalInput,
		CapUIWrite:       d.CapUIWrite,
		CapConfigWrite:   d.CapConfigWrite,
		CapSecretWrite:   d.CapSecretWrite,
		CapRemoteFSWrite: d.CapRemoteFSWrite,
		CapSudoCredential: d.CapSudoCredential,
		CapLifecycle:     d.CapLifecycle,
	}
	if !boot.Enabled {
		return boot
	}
	boot.Token = newDebugToken()
	if boot.CDPEnabled {
		boot.CDPPort = freeTCPPort()
	}
	return boot
}

func newDebugToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// freeTCPPort 取一个空闲的本地端口（0 表示失败）。
func freeTCPPort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer func() { _ = ln.Close() }()
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// debugController 调试模式的运行时状态。
type debugController struct {
	app    *App
	boot   DebugBoot
	bridge *debugsrv.Bridge
	hub    *debugsrv.Hub
	srv    *debugsrv.Server

	// audits / confirms 由 App 持有（不随调试模式开关启停）：前端「AI 记录」页签与
	// SetCapability 在调试模式关闭时也要工作（见 app.go 的 startup）。
	audits   *debugsrv.AuditLog
	confirms *debugsrv.ConfirmManager

	// settingsUndo 保存「可撤销」的设置改动前快照：key 见 settingsUndoKey，
	// 撤销时把这份值写回设置（见 App.UndoAuditRecord）。只保留最近 settingsUndoKeep 条。
	undoMu       sync.Mutex
	settingsUndo map[string]models.Settings
	undoOrder    []string

	mu   sync.Mutex
	addr string
	port int
}

// start 启动调试服务（幂等）。
func (c *debugController) start() {
	c.mu.Lock()
	if c.srv != nil {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	host := "127.0.0.1"
	if c.boot.BindLAN {
		host = "0.0.0.0"
	}
	srv := debugsrv.New(debugsrv.Options{
		Addr:     fmt.Sprintf("%s:%d", host, c.boot.Port),
		Token:    c.boot.Token,
		Handler:  c,
		Hub:      c.hub,
		Logf:     logx.Infof,
		APILogf:  apiLogf,
		Gate:     debugGate{c},
		Confirms: c.confirms,
		Audits:   c.audits,
		Executor: debugExecutor{ctrl: c},
		// M8：远端写白名单交给 debugsrv 做预检（与 Gate 读的是同一份设置）
		RemoteFSPaths: debugSFTPPaths{ctrl: c},
	})
	addr, err := srv.Start()
	if err != nil {
		// 端口被占用：回退到系统分配的空闲端口，保证调试模式仍可用
		logx.Errorf("调试服务启动失败（端口 %d）: %v，改用随机端口重试", c.boot.Port, err)
		srv = debugsrv.New(debugsrv.Options{
			Addr:     fmt.Sprintf("%s:0", host),
			Token:    c.boot.Token,
			Handler:  c,
			Hub:      c.hub,
			Logf:     logx.Infof,
			APILogf:  apiLogf,
			Gate:     debugGate{c},
			Confirms: c.confirms,
			Audits:   c.audits,
			Executor: debugExecutor{ctrl: c},
			// M8：同上，随机端口重试时也要把白名单来源接上
			RemoteFSPaths: debugSFTPPaths{ctrl: c},
		})
		addr, err = srv.Start()
		if err != nil {
			logx.Errorf("调试服务启动失败: %v", err)
			return
		}
	}
	port := 0
	if _, p, perr := net.SplitHostPort(addr); perr == nil {
		_, _ = fmt.Sscanf(p, "%d", &port)
	}
	c.mu.Lock()
	c.srv = srv
	c.addr = addr
	c.port = port
	c.mu.Unlock()
	c.writeInfoFile()
}

// stop 关闭调试服务并清理信息文件。
func (c *debugController) stop() {
	c.mu.Lock()
	srv := c.srv
	c.srv = nil
	c.addr = ""
	c.port = 0
	c.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		logx.Errorf("调试服务关闭失败: %v", err)
	}
	c.removeInfoFile()
}

// info 返回调试模式运行状态（不含 token 之外的新信息；token 仅写给本机调用方）。
func (c *debugController) info() map[string]any {
	c.mu.Lock()
	addr, port := c.addr, c.port
	running := c.srv != nil
	c.mu.Unlock()

	host := "127.0.0.1"
	if c.boot.BindLAN {
		host = "0.0.0.0"
	}
	url := ""
	if port > 0 {
		url = fmt.Sprintf("http://%s:%d", host, port)
	}
	return map[string]any{
		"running":      running,
		"enabled":      c.boot.Enabled,
		"bindLan":      c.boot.BindLAN,
		"addr":         addr,
		"port":         port,
		"url":          url,
		"lanUrl":       lanURL(port),
		"token":        c.boot.Token,
		"cdpEnabled":   c.boot.CDPEnabled,
		"cdpPort":      c.boot.CDPPort,
		"allowEval":    c.boot.AllowEval,
		"allowSecrets": c.boot.AllowSecrets,
		"infoFile":     DebugInfoPath(),
		"pid":          os.Getpid(),
		"hint":         "Debug mode listens on localhost/LAN only. The token changes on every launch. Turn it off in Settings when unused.",
		"streamHint":   "Realtime events: GET " + url + "/v1/stream?topics=ssh.output,ssh.status (SSE)",
	}
}

func lanURL(port int) string {
	if port <= 0 {
		return ""
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return fmt.Sprintf("http://%s:%d", ipnet.IP.String(), port)
			}
		}
	}
	return ""
}

func (c *debugController) writeInfoFile() {
	path := DebugInfoPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(c.info(), "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		logx.Errorf("写入调试信息文件失败: %v", err)
		return
	}
	logx.Infof("调试模式已开启：%s（token 见 %s）", c.info()["url"], path)
}

func (c *debugController) removeInfoFile() {
	_ = os.Remove(DebugInfoPath())
}

// statusView 组装 app.health 的返回：版本 / 运行环境 / 目录 / 调试服务状态 / 能力位。
//
// 与 /v1/health（info()）的区别：info() 面向「调试服务自身」（含 token，供本机调用方
// 自动发现），statusView 面向「AI 判断环境」：token 打码，并多出 uptime 与能力位快照。
func (c *debugController) statusView() map[string]any {
	debugInfo := c.info()
	token, _ := debugInfo["token"].(string)
	debugInfo["token"] = maskToken(token)
	uptimeSec := int64(0)
	if c.app.startedAt > 0 {
		uptimeSec = (time.Now().UnixMilli() - c.app.startedAt) / 1000
	}
	pending := 0
	if c.app.confirms != nil {
		pending = c.app.confirms.PendingCount()
	}
	return map[string]any{
		"appVersion":      appVersion(),
		"goVersion":       runtime.Version(),
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
		"uptimeSec":       uptimeSec,
		"dataDir":         c.app.dataDir,
		"logDir":          logx.LogDir(),
		"debug":           debugInfo,
		"capabilities":    c.app.capabilitySnapshot(),
		"pendingConfirms": pending,
	}
}

// maskToken 给 token 打码：保留前 6 位，其余用 * 代替并标注长度。
// 与 cmd/ding-ssh-mcp 的 maskToken 行为一致（那边用于 stdio 桥的日志脱敏）。
func maskToken(tok string) string {
	if tok == "" {
		return ""
	}
	keep := 6
	if len(tok) <= keep {
		keep = 2
	}
	if keep > len(tok) {
		keep = len(tok)
	}
	return tok[:keep] + strings.Repeat("*", len(tok)-keep) + fmt.Sprintf("(len=%d)", len(tok))
}

// debugArgs 调试模式入参（各 op 需要的字段合集，按需取用）。
type debugArgs struct {
	ID       string          `json:"id"`
	From     string          `json:"from"`
	Lines    int             `json:"lines"`
	Raw      bool            `json:"raw"`
	Data     string          `json:"data"`
	Base64   string          `json:"base64"`
	To       string          `json:"to"`
	Value    int             `json:"value"`
	Cols     int             `json:"cols"`
	Rows     int             `json:"rows"`
	JS       string          `json:"js"`
	Expr     string          `json:"expr"`
	Local    bool            `json:"local"`
	ServerID string          `json:"serverId"`
	Node     json.RawMessage `json:"node"`
	// 日志相关（logs.tail）：级别过滤、关键字过滤、指定日志文件名（默认最新）。
	Level   string `json:"level"`
	Keyword string `json:"keyword"`
	File    string `json:"file"`
}

// Handle 实现 debugsrv.Handler：把 op 分派到后端（会话/隧道）或前端（xterm / 页面）。
func (c *debugController) Handle(ctx context.Context, op string, args json.RawMessage) (any, error) {
	// ---- UI 观测 / 实验域（M5+M6）：op 形如 ui.*，参数形状各异，原样透传给前端桥 ----
	// 说明：Go 侧的裁剪 / 坐标换算 / 像素差异 / 快照落盘在 debugsrv/ui.go 里完成，
	// 这里只负责「页面内事实」的取数（frontend/src/debug/ui.ts 实现具体逻辑）。
	if strings.HasPrefix(op, "ui.") {
		var uiArgs map[string]any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &uiArgs); err != nil {
				return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
			}
		}
		return c.callFrontend(ctx, op, uiArgs)
	}

	// ---- 应用控制面（M7）：op 形如 servers.* / credentials.* / tunnels.* / settings.<x> / app.<x> ----
	// 说明：这些 op 的入参形状差异很大（node / ids / paths / json / type+端口…），
	// 统一按 map 解析后交给 debug.go 末尾的 handleAppctlOp（它落到 app.go 的既有绑定）。
	// 注意 settings.update（既有 op）不在这个集合里，见 debugsrv.IsAppctlOp 的注释。
	if debugsrv.IsAppctlOp(op) {
		var m map[string]any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &m); err != nil {
				return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
			}
		}
		return c.handleAppctlOp(ctx, op, m)
	}

	// ---- M8：SFTP 宿主实现（op 形如 sftp.*）----
	// 参数形状按 map 解析；真正碰远端的实现在 debug.go 末尾的 handleSftpOp
	// （复用会话已有的 SFTP 客户端，见那里的注释）。
	if isHostSftpOp(op) {
		var m map[string]any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &m); err != nil {
				return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
			}
		}
		return c.handleSftpOp(ctx, op, m)
	}

	// ---- M10：sudo 凭证提权的宿主实现（op 形如 sudo.stage / sudo.scrub）----
	// 这里是**唯一持有明文密码**的一侧：stage 负责「取保存的密码 + 用会话已有 SFTP 客户端写临时文件」，
	// scrub 负责「把输出里出现的密码替换成 ***」。密码不回传、不落日志、不进审计
	//（详见文件末尾那一节的注释）。
	if isHostSudoOp(op) {
		var m map[string]any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &m); err != nil {
				return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
			}
		}
		return c.handleSudoOp(ctx, op, m)
	}

	var in debugArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
		}
	}

	switch op {
	case debugsrv.OpHealth:
		return c.info(), nil
	case debugsrv.OpStatus:
		return c.statusView(), nil
	case debugsrv.OpState:
		return c.state(ctx)
	case debugsrv.OpSessions:
		out := map[string]any{"sessions": c.app.manager.List()}
		if tabs, err := c.callFrontend(ctx, "tabs", nil); err == nil {
			out["tabs"] = tabs
		} else {
			out["tabsError"] = err.Error()
		}
		return out, nil
	case debugsrv.OpTerminals:
		return c.callFrontend(ctx, "terminals", nil)
	case debugsrv.OpTerminalBuffer:
		return c.callFrontend(ctx, "terminal.buffer", map[string]any{
			"id": in.ID, "from": in.From, "lines": in.Lines, "raw": in.Raw,
		})
	case debugsrv.OpTerminalInput:
		if in.ID == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		if in.Base64 == "" && in.Data == "" {
			return nil, fmt.Errorf("%w: 缺少 data / base64", debugsrv.ErrBadInput)
		}
		return c.callFrontend(ctx, "terminal.input", map[string]any{
			"id": in.ID, "data": in.Data, "base64": in.Base64,
		})
	case debugsrv.OpTerminalScroll:
		return c.callFrontend(ctx, "terminal.scroll", map[string]any{
			"id": in.ID, "to": in.To, "value": in.Value,
		})
	case debugsrv.OpTerminalResize:
		return c.callFrontend(ctx, "terminal.resize", map[string]any{
			"id": in.ID, "cols": in.Cols, "rows": in.Rows,
		})
	case debugsrv.OpSessionDisconnect:
		if in.ID == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		if err := c.app.manager.Disconnect(in.ID); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "id": in.ID}, nil
	case debugsrv.OpSessionReconnect:
		return c.callFrontend(ctx, "session.reconnect", map[string]any{"id": in.ID})
	case debugsrv.OpTabOpen:
		return c.callFrontend(ctx, "tab.open", map[string]any{
			"node": in.Node, "local": in.Local, "serverId": in.ServerID,
		})
	case debugsrv.OpTabClose:
		return c.callFrontend(ctx, "tab.close", map[string]any{"id": in.ID})
	case debugsrv.OpEval:
		if !c.boot.AllowEval {
			return nil, fmt.Errorf("%w: eval 未开启（设置 → 调试模式 → 允许执行 JS）", debugsrv.ErrForbidden)
		}
		js := in.JS
		if js == "" {
			js = in.Expr
		}
		if strings.TrimSpace(js) == "" {
			return nil, fmt.Errorf("%w: 缺少 js", debugsrv.ErrBadInput)
		}
		return c.callFrontend(ctx, "eval", map[string]any{"js": js})
	case debugsrv.OpSettings:
		st, err := c.app.settings.Get()
		if err != nil {
			return nil, err
		}
		return c.settingsView(st), nil
	case debugsrv.OpSettingsUpdate:
		if len(args) == 0 {
			return nil, fmt.Errorf("%w: 需要 settings JSON（支持部分字段）", debugsrv.ErrBadInput)
		}
		cur, err := c.app.settings.Get()
		if err != nil {
			return nil, err
		}
		// 记下改动前的值：审计记录里「可撤销」的提示与实际恢复都依赖它。
		before := cur
		if err := json.Unmarshal(args, &cur); err != nil {
			return nil, fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
		}
		if err := c.app.SaveSettings(cur); err != nil {
			return nil, err
		}
		c.saveSettingsUndo("update_settings", debugsrv.SettingsPatchArgs(map[string]any{"settings": json.RawMessage(args)}), before)
		return c.settingsView(cur), nil
	case debugsrv.OpServers:
		nodes := c.app.GetServers()
		if !c.boot.AllowSecrets {
			for i := range nodes {
				nodes[i].Password = ""
				nodes[i].KeyContent = ""
			}
		}
		return map[string]any{"count": len(nodes), "servers": nodes, "redacted": !c.boot.AllowSecrets}, nil
	case debugsrv.OpLogs:
		// 日志目录 / 开关 / 级别 / 总大小 / 文件列表 / 保留策略，与设置页「日志」页同源。
		return c.app.GetLogInfo(), nil
	case debugsrv.OpLogsTail:
		return c.app.logTailView(in)
	case debugsrv.OpLogsClear:
		return c.app.ClearLogs(), nil
	case debugsrv.OpLogsExport:
		return c.app.ExportDiagnostics(), nil
	case debugsrv.OpLogsOptions:
		if len(args) == 0 {
			return nil, fmt.Errorf("%w: 需要日志设置 JSON（支持部分字段：consoleEnabled/fileEnabled/level/apiLogEnabled）", debugsrv.ErrBadInput)
		}
		var opts map[string]any
		if err := json.Unmarshal(args, &opts); err != nil {
			return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
		}
		before, _ := c.app.settings.Get()
		if err := c.app.SetLogOptions(opts); err != nil {
			return nil, err
		}
		c.saveSettingsUndo("set_log_options", debugsrv.SettingsPatchArgs(opts), before)
		// 返回最新设置，调用方不必再查一次。
		return c.app.GetLogInfo(), nil
	}
	return nil, fmt.Errorf("%w: 未知操作 %q", debugsrv.ErrBadInput, op)
}

// state 汇总后端与前端的状态快照。
func (c *debugController) state(ctx context.Context) (any, error) {
	out := map[string]any{
		"debug":    c.info(),
		"sessions": c.app.manager.List(),
		"tunnels":  c.app.manager.ListTunnels(),
	}
	if st, err := c.app.settings.Get(); err == nil {
		for k, v := range c.settingsView(st) {
			out[k] = v
		}
	}
	if fe, err := c.callFrontend(ctx, "state", nil); err == nil {
		out["frontend"] = fe
	} else {
		out["frontendError"] = err.Error()
	}
	return out, nil
}

// settingsView 返回设置快照；未开启 allowSecrets 时抹掉潜在敏感字段。
func (c *debugController) settingsView(st models.Settings) map[string]any {
	if c.boot.AllowSecrets {
		return map[string]any{"settings": st}
	}
	st.LocalShell = ""
	return map[string]any{"settings": st, "settingsRedacted": []string{"localShell"}}
}

// callFrontend 通过前端桥执行操作（无回包时视为成功）。
func (c *debugController) callFrontend(ctx context.Context, op string, args any) (any, error) {
	if c.bridge == nil {
		return nil, errors.New("前端桥未初始化")
	}
	raw, err := c.bridge.Call(ctx, op, args)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]any{"ok": true}, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw), nil
	}
	return v, nil
}

// startDebug 在 startup 中调用。
func (a *App) startDebug() {
	if !a.debugBoot.Enabled {
		return
	}
	bridge := debugsrv.NewBridge(func(event string, payload any) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, event, payload)
		}
	})
	c := &debugController{
		app:          a,
		boot:         a.debugBoot,
		bridge:       bridge,
		hub:          debugsrv.NewHub(),
		audits:       a.audits,
		confirms:     a.confirms,
		settingsUndo: make(map[string]models.Settings),
	}
	a.debug = c
	c.start()
}

// stopDebug 在 shutdown 中调用。
func (a *App) stopDebug() {
	if a.debug == nil {
		return
	}
	a.debug.stop()
	a.debug = nil
}

// reloadDebug 在设置保存后调用：**只有影响监听地址的字段变化时才重启调试服务**。
//
// 为什么必须区分（M9 修的体验缺陷）：能力位 / 「允许执行 JS」/「允许读取敏感数据」/ 日志相关
// 都是**就地生效**的（debugGate 每次工具调用都实时读设置），而重启调试服务会：
//   - Port=0（自动端口）时换一个系统随机端口 → url 变了；
//   - 旧连接被 Shutdown 掐断 → 正在连的 MCP / HTTP 客户端直接掉线。
//
// 用户只是在「设置 → 调试模式」里勾一个能力位，不应该把 AI 踢下线。
//
// 影响监听地址的字段只有三个：
//   - Enabled：调试服务总开关（false → 停，true → 起）；
//   - Port：监听端口（0 = 系统随机分配，重启必然换地址）；
//   - BindLAN：127.0.0.1 ↔ 0.0.0.0（监听地址本身变了）。
//
// CDPEnabled / CDPPort 不影响本服务的监听地址（CDP 端口只在应用启动时分配，切换后需重启应用），
// 因此同样只更新兜底快照，不重启。
//
// SaveSettings 与 SetCapability 两条路径最终都调用本函数（见 app.go 的 SaveSettings
// 与 debug.go 的 applyCapability），因此这里改一处即可同时覆盖。
func (a *App) reloadDebug(next models.DebugSettings) {
	next = models.NormalizeDebugSettings(next)
	if a.debug == nil && !next.Enabled {
		return
	}
	listenChanged := a.debugBoot.Enabled != next.Enabled ||
		a.debugBoot.Port != next.Port ||
		a.debugBoot.BindLAN != next.BindLAN
	// a.debug == nil 但设置要求开启：说明服务没起来（宿主接线异常 / 启动失败），
	// 这时即使监听字段没变也要尝试启动一次。
	needsStart := a.debug == nil && next.Enabled
	if !listenChanged && !needsStart {
		// 就地生效：只把启动期兜底快照同步成最新值。
		// 兜底快照的用途是「存储短暂不可用时 currentDebugSettings 的回退值」，
		// 真实校验读的是存储（currentDebugSettings），因此这里不同步也不会放行错误的能力位 ——
		// 但同步后行为更可预期（例如 app.health 里的 debug 信息与设置一致）。
		a.debugBoot = a.debugBoot.withRuntimeSettings(next)
		return
	}
	a.stopDebug()
	boot := a.debugBoot
	boot.Enabled = next.Enabled
	boot.Port = next.Port
	boot.BindLAN = next.BindLAN
	boot = boot.withRuntimeSettings(next)
	if boot.Enabled && boot.Token == "" {
		boot.Token = newDebugToken()
	}
	a.debugBoot = boot
	a.startDebug()
}

// withRuntimeSettings 把设置里「不影响监听地址」的字段合并进启动快照（不重启服务）。
//
// 与 DebugBoot.settings() 互为反向操作：那边把快照还原成 models.DebugSettings，
// 这里把 models.DebugSettings 的其余字段写回快照。监听地址三字段（Enabled/Port/BindLAN）
// 不由本函数处理 —— 它们只在启停路径里改，避免「就地生效」路径意外改掉监听配置。
func (b DebugBoot) withRuntimeSettings(next models.DebugSettings) DebugBoot {
	b.AllowEval = next.AllowEval
	b.AllowSecrets = next.AllowSecrets
	b.CDPEnabled = next.CDPEnabled
	b.CapTerminalInput = next.CapTerminalInput
	b.CapUIWrite = next.CapUIWrite
	b.CapConfigWrite = next.CapConfigWrite
	b.CapSecretWrite = next.CapSecretWrite
	b.CapRemoteFSWrite = next.CapRemoteFSWrite
	b.CapSudoCredential = next.CapSudoCredential
	b.CapLifecycle = next.CapLifecycle
	return b
}

// DebugReply 前端桥回包入口（Wails 绑定）。
func (a *App) DebugReply(id string, payload string) {
	if a.debug == nil || a.debug.bridge == nil {
		return
	}
	a.debug.bridge.Reply(id, payload)
}

// DebugInfo 供设置页展示调试模式运行状态（token 也一并返回，设置页只在本机展示）。
func (a *App) DebugInfo() map[string]any {
	if a.debug == nil {
		enabled := a.debugBoot.Enabled
		return map[string]any{
			"running":    false,
			"enabled":    enabled,
			"cdpEnabled": a.debugBoot.CDPEnabled,
			"cdpPort":    a.debugBoot.CDPPort,
			"infoFile":   DebugInfoPath(),
			"hint":       "Debug mode is not running. Enable it in Settings; it takes effect immediately (the WebView2 CDP port is not available under Wails v2.13 - use /v1/eval and /v1/screenshot instead).",
		}
	}
	return a.debug.info()
}

// ---- 日志（设置页「日志」页后端）----
//
// 日志的落盘与轮转由 internal/logx 负责，这里只做三件事：
//   - 把运行状态与文件清单汇报给前端；
//   - 接收前端的开关变更并立即生效（写回设置，行为与「设置」页一致）；
//   - 生成诊断包（最近日志 + 调试信息 + 状态快照 + 环境信息）。

const (
	// logRetentionMaxBytes / logRetentionKeepFiles 日志目录的保留策略，
	// 与 internal/logx/file.go 的 maxFileSize / maxFileCount 对应（那里未导出，这里只用于展示）。
	logRetentionMaxBytes  = 8 << 20
	logRetentionKeepFiles = 20
	// logTailDefaultLines ReadLogTail 未指定行数时的默认值。
	logTailDefaultLines = 200
	// debugLogTailDefaultLines / debugLogTailMaxLines 调试接口 logs.tail 的默认与上限行数。
	debugLogTailDefaultLines = 200
	debugLogTailMaxLines     = 5000
	// debugLogTailScanFactor / debugLogTailScanMin logs.tail 在过滤前的扩大读取倍数与下限：
	// 先多读一些，按级别/关键字过滤后仍能凑够请求的行数。
	debugLogTailScanFactor = 5
	debugLogTailScanMin    = 500
	// logTailMaxReadBytes 读取「非最新」日志文件尾部时的最大回读字节数，
	// 与 internal/logx 的回读上限一致：避免超长文件被整体读进内存。
	logTailMaxReadBytes = 4 << 20
	// diagnosticsMaxLogFiles 诊断包内附带的最大日志文件数（取最新的）。
	diagnosticsMaxLogFiles = 3
	// diagnosticsBridgeTimeout 诊断包等待前端回包的超时，避免导出卡住。
	diagnosticsBridgeTimeout = 5 * time.Second
	// traceTextMaxRunes 单条跟踪日志保留的最大字符数：终端一次输出可能上万行，
	// 不截断会把日志文件瞬间写满。
	traceTextMaxRunes = 2000
	// traceOutputPrefix / ssOutputTopic 会话输出事件名前缀与对应的 SSE topic，
	// 与 internal/sshx 的 notify 命名（ssh:output:<id>）保持一致。
	traceOutputPrefix = "ssh:output:"
	ssOutputTopic     = "ssh.output"
)

// ansiEscapeRe 匹配常见 ANSI 转义序列（CSI / OSC / 单字符转义），
// 跟踪日志只保留可见文本，避免把颜色控制串与清屏指令写进日志。
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// apiLogf 调用日志统一入口：先脱敏（logx.Redact）再交给 logx.APILogf，
// [api] 前缀与开关判断都在 logx 内完成。
func apiLogf(format string, args ...any) {
	logx.APILogf("%s", logx.Redact(fmt.Sprintf(format, args...)))
}

// GetLogInfo 返回「日志」页需要的全部信息。
func (a *App) GetLogInfo() map[string]any {
	traceTabs := []string{}
	if a.settings != nil {
		if st, err := a.settings.Get(); err == nil {
			traceTabs = models.NormalizeLogTraceTabs(st.LogTraceTabs)
		} else {
			logx.Errorf("读取日志设置失败: %v", err)
		}
	}
	if traceTabs == nil {
		traceTabs = []string{}
	}
	files := make([]map[string]any, 0)
	if list, err := logx.ListFiles(); err != nil {
		logx.Errorf("读取日志文件列表失败: %v", err)
	} else {
		for _, f := range list {
			files = append(files, map[string]any{
				"name":    f.Name,
				"size":    f.Size,
				"modTime": f.ModTime,
			})
		}
	}
	total, err := logx.TotalSize()
	if err != nil {
		logx.Errorf("统计日志目录大小失败: %v", err)
	}
	return map[string]any{
		"dir":            logx.LogDir(),
		"consoleEnabled": logx.Enabled(),
		"fileEnabled":    logx.FileEnabled(),
		"level":          logx.Level(),
		"apiLogEnabled":  logx.APILogEnabled(),
		"traceTabs":      traceTabs,
		"totalSize":      total,
		"files":          files,
		"retention": map[string]any{
			"maxBytes":  logRetentionMaxBytes,
			"keepFiles": logRetentionKeepFiles,
		},
	}
}

// SetLogOptions 部分更新日志开关（consoleEnabled / fileEnabled / level / apiLogEnabled），
// 写回设置并立即生效；未出现的字段保持原值。
func (a *App) SetLogOptions(opts map[string]any) error {
	if a.settings == nil {
		return errors.New("设置存储未初始化")
	}
	cur, err := a.settings.Get()
	if err != nil {
		return fmt.Errorf("读取设置失败: %w", err)
	}
	if v, ok := opts["consoleEnabled"].(bool); ok {
		cur.LogEnabled = v
	}
	if v, ok := opts["fileEnabled"].(bool); ok {
		cur.LogToFile = v
	}
	if v, ok := opts["level"].(string); ok {
		cur.LogLevel = models.NormalizeLogLevel(v)
	}
	if v, ok := opts["apiLogEnabled"].(bool); ok {
		cur.LogAPICalls = v
	}
	// 复用 SaveSettings：写库 + 立即应用四项日志开关，行为与「设置」页保存完全一致。
	return a.SaveSettings(cur)
}

// OpenLogFolder 在文件管理器中打开日志目录（先确保目录存在）。
func (a *App) OpenLogFolder() error {
	dir := logx.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开日志目录失败: %w", err)
	}
	// 不阻塞等待（explorer 会常驻）：异步回收进程句柄，避免僵尸进程。
	go func() { _ = cmd.Wait() }()
	return nil
}

// ClearLogs 清空日志目录内的日志文件，返回删除数量。
func (a *App) ClearLogs() map[string]any {
	removed, err := logx.Clear()
	out := map[string]any{"removed": removed}
	if err != nil {
		out["error"] = err.Error()
		logx.Errorf("清空日志失败: %v", err)
	}
	return out
}

// ReadLogTail 读取最新日志文件的末尾若干行（lines <= 0 时取 200 行）。
func (a *App) ReadLogTail(lines int) map[string]any {
	if lines <= 0 {
		lines = logTailDefaultLines
	}
	text, err := logx.ReadTail(lines)
	out := map[string]any{"text": text, "files": 0, "totalSize": int64(0)}
	if list, lerr := logx.ListFiles(); lerr == nil {
		out["files"] = len(list)
	}
	if total, terr := logx.TotalSize(); terr == nil {
		out["totalSize"] = total
	}
	if err != nil {
		out["error"] = err.Error()
		logx.Errorf("读取日志失败: %v", err)
	}
	return out
}

// logLevelNames / logLevelMarkers 日志级别名与日志行里的级别标记，下标即严重程度
// （0=debug … 3=error），两处顺序必须一致（parseLogLevelFilter 与 logLineLevel 靠它对齐）。
var (
	logLevelNames   = []string{"debug", "info", "warn", "error"}
	logLevelMarkers = []string{"[DEBUG]", "[INFO]", "[WARN]", "[ERROR]"}
)

// parseLogLevelFilter 解析 logs.tail / read_logs 的 level 参数：
// 返回级别对应的严重程度（0=debug…3=error）；空串表示不过滤，返回 -1；
// 其余取值返回 ErrBadInput。
func parseLogLevelFilter(name string) (int, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return -1, nil
	}
	for i, n := range logLevelNames {
		if n == name {
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w: 不支持的 level %q（可选 debug|info|warn|error）", debugsrv.ErrBadInput, name)
}

// logLineLevel 返回日志行的严重程度；没有级别标记的行返回 -1
// （例如第三方库经标准库 log 写入的行，或只有一半的旧行）。
// 取行内最先出现的标记：正文里顺带提到 "[ERROR]" 之类字样时不会被误判成该级别。
func logLineLevel(line string) int {
	best, bestAt := -1, -1
	for lv, marker := range logLevelMarkers {
		at := strings.Index(line, marker)
		if at >= 0 && (bestAt < 0 || at < bestAt) {
			best, bestAt = lv, at
		}
	}
	return best
}

// logTailView 实现 logs.tail：读取日志尾部若干行，按 level / keyword 过滤后返回最后 lines 行。
//
// 过滤语义（与 MCP 工具 read_logs 的描述一致）：
//   - level 取「该级别及以上」（level=warn 同时包含 WARN 与 ERROR）；
//   - 指定 level 时，没有级别标记的行（第三方库输出）会被过滤掉；
//   - keyword 为大小写不敏感的子串匹配；
//   - file 只允许 logx.ListFiles() 返回的名字，为空时取最新文件。
//
// 返回 {file, dir, lines, matched, scanned, text}：lines 为实际返回行数，
// matched 为过滤后命中的行数（大于 lines 说明被截断），
// scanned 为过滤前读取的总行数（便于判断是不是压根没日志）。
func (a *App) logTailView(in debugArgs) (map[string]any, error) {
	lines := in.Lines
	if lines <= 0 {
		lines = debugLogTailDefaultLines
	}
	if lines > debugLogTailMaxLines {
		lines = debugLogTailMaxLines
	}
	level, err := parseLogLevelFilter(in.Level)
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(in.Keyword)

	// 只允许 ListFiles 返回的文件名：外部传入的名字一律拒绝，杜绝目录穿越。
	files, err := logx.ListFiles()
	if err != nil {
		return nil, fmt.Errorf("读取日志文件列表失败: %w", err)
	}
	name := strings.TrimSpace(in.File)
	if name != "" {
		// 用列表里的规范名字（大小写不同也接受，Windows 文件系统本身不区分大小写）。
		canonical, ok := logFileName(files, name)
		if !ok {
			return nil, fmt.Errorf("%w: 未知日志文件 %q（见 list_logs 的 files 列表）", debugsrv.ErrBadInput, name)
		}
		name = canonical
	}

	// 过滤前多读一些，保证过滤后仍有足够行数。
	scan := lines * debugLogTailScanFactor
	if scan < debugLogTailScanMin {
		scan = debugLogTailScanMin
	}
	text := ""
	if name == "" {
		if len(files) > 0 {
			name = files[0].Name // ListFiles 已按修改时间倒序，首个即最新
		}
		if text, err = logx.ReadTail(scan); err != nil {
			return nil, fmt.Errorf("读取日志失败: %w", err)
		}
	} else {
		if text, err = readLogFileTail(name, scan); err != nil {
			return nil, fmt.Errorf("读取日志文件 %q 失败: %w", name, err)
		}
	}

	scanned := splitLogLines(text)
	matched := make([]string, 0, len(scanned))
	lowerKeyword := strings.ToLower(keyword)
	for _, ln := range scanned {
		// 指定 level 时，无级别标记的行按「不达标」处理。
		if level >= 0 && logLineLevel(ln) < level {
			continue
		}
		if lowerKeyword != "" && !strings.Contains(strings.ToLower(ln), lowerKeyword) {
			continue
		}
		matched = append(matched, ln)
	}
	hits := len(matched)
	if hits > lines {
		matched = matched[hits-lines:]
	}
	out := map[string]any{
		"file":    name,
		"dir":     logx.LogDir(),
		"lines":   len(matched),
		"matched": hits,
		"scanned": len(scanned),
		"text":    strings.Join(matched, "\n"),
	}
	if in.Level != "" {
		out["level"] = strings.ToLower(strings.TrimSpace(in.Level))
	}
	if keyword != "" {
		out["keyword"] = keyword
	}
	return out, nil
}

// logFileName 在日志文件列表里查找名字（忽略大小写），返回列表中的规范名字。
func logFileName(files []logx.LogFile, name string) (string, bool) {
	for _, f := range files {
		if strings.EqualFold(f.Name, name) {
			return f.Name, true
		}
	}
	return "", false
}

// splitLogLines 把日志文本切成行（去掉结尾换行；空文本返回空切片）。
func splitLogLines(text string) []string {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

// readLogFileTail 读取日志目录内某个日志文件的最后 lines 行。
// name 必须来自 logx.ListFiles()（调用方已校验），因此不存在目录穿越。
// logx.ReadTail 只读最新文件，因此「指定历史文件」需要在这里自己回读。
func readLogFileTail(name string, lines int) (string, error) {
	f, err := os.Open(filepath.Join(logx.LogDir(), name))
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size == 0 {
		return "", nil
	}
	// 只回读尾部若干字节（日志按 8MB 轮转，正常不会触发上限）。
	var offset int64
	if size > logTailMaxReadBytes {
		offset = size - logTailMaxReadBytes
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text := string(buf)
	if offset > 0 {
		// 从行中间开始读：丢掉首行半截内容。
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return lastLogLines(text, lines), nil
}

// lastLogLines 返回文本的最后 n 行（去掉结尾换行造成的空行）。
func lastLogLines(text string, n int) string {
	if n <= 0 {
		return ""
	}
	parts := splitLogLines(text)
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "\n")
}

// ExportDiagnostics 在日志目录生成诊断包，返回 {path, bytes}（失败时附带 error）。
func (a *App) ExportDiagnostics() map[string]any {
	dir := logx.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return map[string]any{"error": fmt.Sprintf("创建日志目录失败: %v", err)}
	}
	path := filepath.Join(dir, "diagnostics-"+time.Now().Format("20060102-150405")+".zip")
	f, err := os.Create(path)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("创建诊断包失败: %v", err)}
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	writeErr := a.writeDiagnostics(zw, dir)
	if err := zw.Close(); err != nil && writeErr == nil {
		writeErr = err
	}
	var size int64
	if info, statErr := f.Stat(); statErr == nil {
		size = info.Size()
	}
	out := map[string]any{"path": path, "bytes": size}
	if writeErr != nil {
		out["error"] = writeErr.Error()
		logx.Errorf("导出诊断包失败: %v", writeErr)
	}
	logx.Infof("诊断包已导出: %s（%d 字节）", path, size)
	return out
}

// writeDiagnostics 往压缩包里写入：最近日志文件、debug.json、state.json、env.json。
// 单个条目失败不中断其余条目，返回第一个错误。
func (a *App) writeDiagnostics(zw *zip.Writer, dir string) error {
	var firstErr error
	fail := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// 最近 N 个日志文件（ListFiles 已按修改时间倒序）
	if files, err := logx.ListFiles(); err != nil {
		fail(err)
	} else {
		if len(files) > diagnosticsMaxLogFiles {
			files = files[:diagnosticsMaxLogFiles]
		}
		for _, lf := range files {
			fail(addFileToZip(zw, lf.Name, filepath.Join(dir, lf.Name)))
		}
	}

	// debug.json（调试模式运行时才有）
	infoPath := DebugInfoPath()
	if _, err := os.Stat(infoPath); err == nil {
		fail(addFileToZip(zw, "debug.json", infoPath))
	}

	// state.json：/v1/state 的等价快照（会话列表 + 终端诊断）
	if data, err := json.MarshalIndent(a.diagnosticsState(), "", "  "); err != nil {
		fail(err)
	} else {
		fail(addBytesToZip(zw, "state.json", data))
	}

	// env.json：运行环境
	env := map[string]any{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"goVersion":  runtime.Version(),
		"appVersion": appVersion(),
		"logDir":     dir,
		"logLevel":   logx.Level(),
		"time":       time.Now().Format(time.RFC3339),
	}
	if data, err := json.MarshalIndent(env, "", "  "); err != nil {
		fail(err)
	} else {
		fail(addBytesToZip(zw, "env.json", data))
	}
	return firstErr
}

// diagnosticsState 生成调试服务 /v1/state 的等价快照。
// 会话列表由后端直接给出；终端诊断（xterm 的行列 / 缓冲区 / 可见文本）只有前端知道，
// 通过调试服务的前端桥获取，取不到就写 error，保证诊断包本身一定能生成。
func (a *App) diagnosticsState() map[string]any {
	out := map[string]any{}
	if a.manager != nil {
		out["sessions"] = a.manager.List()
		out["tunnels"] = a.manager.ListTunnels()
	}
	if a.settings != nil {
		if st, err := a.settings.Get(); err == nil {
			if a.debug != nil {
				for k, v := range a.debug.settingsView(st) {
					out[k] = v
				}
			} else {
				out["settings"] = st
			}
		}
	}
	if a.debug == nil || a.debug.bridge == nil {
		out["error"] = "调试模式未开启，无法获取终端诊断（terminals）"
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticsBridgeTimeout)
	defer cancel()
	if v, err := a.debug.callFrontend(ctx, "terminals", nil); err == nil {
		out["terminals"] = v
	} else {
		out["error"] = err.Error()
	}
	if v, err := a.debug.callFrontend(ctx, "state", nil); err == nil {
		out["frontend"] = v
	} else {
		out["frontendError"] = err.Error()
	}
	return out
}

// addFileToZip 把一个磁盘文件写进压缩包（arcName 为包内路径）。
func addFileToZip(zw *zip.Writer, arcName, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = arcName
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

// addBytesToZip 把一段内存数据写进压缩包。
func addBytesToZip(zw *zip.Writer, arcName string, data []byte) error {
	w, err := zw.Create(arcName)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ---- 按会话追踪输出（item 6）----
//
// 前端渲染输出时本来就能拿到原始字节，但「把某个会话的输出全量落盘」只需要后端能力：
// 会话输出在 app.go 的 notify 闭包里本来就要发布到调试服务的事件总线，
// 那里顺带判断该会话是否在 LogTraceTabs 中，命中就写一条 [trace:<id>] 日志。
// 这样既不用改 internal/sshx（不改 onOutput 包装），也不会引入新的 goroutine。

// traceSessionID 从事件名与 payload 里取出会话 id（事件名形如 ssh:output:<id>）。
// 前缀不匹配或 id 为空返回空串。
func traceSessionID(eventName string, payload any) (string, bool) {
	topic, sessionID := debugsrv.SplitEventName(eventName)
	if topic != ssOutputTopic {
		return "", false
	}
	if ev, ok := payload.(models.OutputEvent); ok && ev.SessionID != "" {
		sessionID = ev.SessionID
	}
	if sessionID == "" {
		return "", false
	}
	return sessionID, true
}

// traceEvent 在事件旁路里落地被跟踪会话的输出（未命中跟踪列表时立即返回）。
//
// 说明：按规格使用 Debugf（需要把日志级别设为 debug 才会输出），
// 因此「跟踪会话输出」实际上要求：日志级别 debug + 控制台或文件日志至少开启一项。
func (a *App) traceEvent(eventName string, payload any) {
	// 快速路径：绝大多数事件不是终端输出（状态 / 进度 / SFTP / 隧道），
	// 先用前缀判断挡掉，避免高频事件都做一次字符串切分。
	if !strings.HasPrefix(eventName, traceOutputPrefix) {
		return
	}
	sessionID, ok := traceSessionID(eventName, payload)
	if !ok || !a.isTraced(sessionID) {
		return
	}
	ev, ok := payload.(models.OutputEvent)
	if !ok {
		return
	}
	data, err := base64.StdEncoding.DecodeString(ev.Data)
	if err != nil {
		logx.Debugf("[trace:%s] 输出解码失败: %v", sessionID, err)
		return
	}
	// 消息里可能含 % 号，必须作为参数传入而不是格式化串。
	logx.Debugf("%s", logx.Redact(fmt.Sprintf("[trace:%s] %s", sessionID, visibleText(data))))
}

// visibleText 把终端原始字节转成一行可读文本：
//   - 去掉 ANSI 转义序列与控制字符（只保留可见文本）；
//   - 非 UTF-8 或完全没有可见字符时记为 [binary n bytes]；
//   - 换行 / 制表符压成空格，保证一次输出只占一行；
//   - 超过 traceTextMaxRunes 个字符时截断（避免日志被单次输出淹没）。
func visibleText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if !utf8.Valid(data) {
		return fmt.Sprintf("[binary %d bytes]", len(data))
	}
	text := ansiEscapeRe.ReplaceAllString(string(data), "")
	var sb strings.Builder
	sb.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\n' || r == '\t':
			sb.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// 其余控制字符（含残留的 ESC）不写入日志
		default:
			sb.WriteRune(r)
		}
	}
	visible := strings.TrimSpace(sb.String())
	if visible == "" {
		return fmt.Sprintf("[binary %d bytes]", len(data))
	}
	if runes := []rune(visible); len(runes) > traceTextMaxRunes {
		return string(runes[:traceTextMaxRunes]) + fmt.Sprintf("...(truncated, %d runes total)", len(runes))
	}
	return visible
}

// setTraceTabs 更新内存里的跟踪会话集合（启动、保存设置、SetLogTrace 时调用）。
// 事件回调是高频路径，不可能每次去查库，因此这里维护一份只读快照。
func (a *App) setTraceTabs(tabs []string) {
	set := make(map[string]bool, len(tabs))
	for _, t := range tabs {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	a.traceMu.Lock()
	a.traceTabs = set
	a.traceMu.Unlock()
}

// isTraced 判断会话（== 标签 clientId）是否处于全量跟踪状态。
func (a *App) isTraced(id string) bool {
	if id == "" {
		return false
	}
	a.traceMu.RLock()
	defer a.traceMu.RUnlock()
	return a.traceTabs[id]
}

// SetLogTrace 增删跟踪会话并保存设置；随后该会话的输出会以 [trace:<id>] 落到日志。
func (a *App) SetLogTrace(tabId string, enabled bool) error {
	tabId = strings.TrimSpace(tabId)
	if tabId == "" {
		return errors.New("标签 id 不能为空")
	}
	if a.settings == nil {
		return errors.New("设置存储未初始化")
	}
	st, err := a.settings.Get()
	if err != nil {
		return fmt.Errorf("读取设置失败: %w", err)
	}
	tabs := make([]string, 0, len(st.LogTraceTabs)+1)
	for _, id := range st.LogTraceTabs {
		if id == tabId {
			continue
		}
		tabs = append(tabs, id)
	}
	if enabled {
		tabs = append(tabs, tabId)
	}
	st.LogTraceTabs = models.NormalizeLogTraceTabs(tabs)
	if err := a.settings.Save(st); err != nil {
		return fmt.Errorf("保存日志跟踪设置失败: %w", err)
	}
	a.setTraceTabs(st.LogTraceTabs)
	if enabled {
		logx.Infof("[trace] 开始跟踪会话输出: %s", tabId)
	} else {
		logx.Infof("[trace] 停止跟踪会话输出: %s", tabId)
	}
	return nil
}

// LogClient 供前端把诊断信息写进同一份日志（前缀 [client]，级别 debug|info|warn|error）。
func (a *App) LogClient(level string, message string) error {
	msg := logx.Redact(strings.TrimSpace(message))
	if msg == "" {
		return errors.New("日志内容不能为空")
	}
	// 消息里可能含 % 号，必须作为参数传入而不是格式化串。
	line := "[client] " + msg
	switch models.NormalizeLogLevel(level) {
	case models.LogLevelDebug:
		logx.Debugf("%s", line)
	case models.LogLevelWarn:
		logx.Warnf("%s", line)
	case models.LogLevelError:
		logx.Errorf("%s", line)
	default:
		logx.Infof("%s", line)
	}
	return nil
}

// -------- 能力位（权限内核）--------
//
// 这一节是「能力位」在宿主侧的唯一实现：把 internal/debugsrv 的能力枚举映射到
// models.DebugSettings 的实际开关。后续三路（UI 观测 / 应用控制 / SFTP）只要在
// debugsrv 的 toolCaps 里登记能力位，校验就自动生效，不需要改这里。

// currentDebugSettings 读取当前调试设置（能力位的唯一事实来源）。
//
// 每次调用都读一次存储：能力开关可能在运行中被用户改（设置页 / capability.enable），
// 缓存会带来「刚关掉仍在放行」的窗口；而这里的读取频率是「每次 MCP 工具调用」，
// SQLite 的点查代价可以忽略。
func (a *App) currentDebugSettings() models.DebugSettings {
	if a.settings != nil {
		if st, err := a.settings.Get(); err == nil {
			return models.NormalizeDebugSettings(st.Debug)
		}
	}
	return models.NormalizeDebugSettings(a.debugBoot.settings())
}

// settings 把启动期读到的调试配置还原成 models.DebugSettings
// （存储不可用时的兜底：至少让能力位与启动时一致，而不是全部关闭）。
func (b DebugBoot) settings() models.DebugSettings {
	return models.DebugSettings{
		Enabled:          b.Enabled,
		Port:             b.Port,
		BindLAN:          b.BindLAN,
		AllowEval:        b.AllowEval,
		AllowSecrets:     b.AllowSecrets,
		CDPEnabled:       b.CDPEnabled,
		CapTerminalInput: b.CapTerminalInput,
		CapUIWrite:       b.CapUIWrite,
		CapConfigWrite:   b.CapConfigWrite,
		CapSecretWrite:   b.CapSecretWrite,
		CapRemoteFSWrite: b.CapRemoteFSWrite,
		CapSudoCredential: b.CapSudoCredential,
		CapLifecycle:     b.CapLifecycle,
	}
}

// debugGate 实现 debugsrv.Gate：宿主侧的能力校验。
type debugGate struct{ ctrl *debugController }

// Allow 校验能力位，被拒时返回可直接展示给用户 / AI 的中文原因。
func (g debugGate) Allow(cap debugsrv.Capability, detail string) error {
	if cap == debugsrv.CapRead {
		return nil
	}
	d := g.ctrl.app.currentDebugSettings()
	label := debugsrv.CapabilityLabel(cap)
	deny := func(reason string) error {
		suffix := ""
		if detail != "" {
			suffix = fmt.Sprintf("（本次操作涉及：%s）", detail)
		}
		return fmt.Errorf("「%s」能力未开启：%s%s。可在 设置 → 调试模式 → MCP 能力 中开启，"+
			"或先 confirm.prepare（action=capability.enable, args={\"cap\":%q}）再 confirm.commit。",
			label, reason, suffix, string(cap))
	}
	switch cap {
	case debugsrv.CapTerminalInput:
		if !d.CapTerminalInput {
			return deny("它控制「向终端写入数据」（等价于代替用户敲命令）")
		}
	case debugsrv.CapUIWrite:
		if !d.CapUIWrite {
			return deny("它控制「修改界面」（CSS / 令牌 / store / 合成事件）")
		}
	case debugsrv.CapConfigWrite:
		if !d.CapConfigWrite {
			return deny("它控制「写入配置」（服务器 / 隧道 / 设置）")
		}
	case debugsrv.CapSecretsRead:
		if !d.AllowSecrets {
			return deny("它需要同时开启「允许读取敏感数据」")
		}
	case debugsrv.CapSecretsWrite:
		if !d.AllowSecrets {
			return deny("凭据写入需要同时开启「允许读取敏感数据」")
		}
		if !d.CapSecretWrite {
			return deny("它控制「写入凭据」（新增 / 修改 / 删除保存的凭据）")
		}
	case debugsrv.CapRemoteFSWrite:
		if !d.CapRemoteFSWrite {
			return deny("它控制「远端文件写入」（SFTP 写 / 删 / 改名 / 建目录 / 上传）")
		}
		if len(d.SFTPWriteAllowlist) == 0 {
			return deny("已开启远端文件写入，但路径白名单为空（留空 = 禁止任何写入路径），请先填写允许的前缀")
		}
		if detail != "" && !pathAllowed(d.SFTPWriteAllowlist, detail) {
			return fmt.Errorf("远端路径 %q 不在写入白名单内（当前白名单：%s）",
				detail, strings.Join(d.SFTPWriteAllowlist, "、"))
		}
	case debugsrv.CapSudoCredential:
		// sudo 凭证提权有**两个**前置条件（用户明确要求「缺哪个就指出缺哪个」）：
		// 能力位本身 + 「允许读取敏感数据」（密码只从凭据库读，不允许读密钥就不可能提权）。
		var missing []string
		if !d.CapSudoCredential {
			missing = append(missing, "「sudo 凭证提权」(sudo.credential) 本身未开启")
		}
		if !d.AllowSecrets {
			missing = append(missing, "「允许读取敏感数据」(secrets.read) 未开启 —— 本能力要用应用里为该服务器保存的密码")
		}
		if len(missing) > 0 {
			return deny("它允许 AI 在当前会话对应的服务器上用保存的密码执行 sudo -i（等于交出该机器的 root）。缺：" +
				strings.Join(missing, "；"))
		}
	case debugsrv.CapLifecycle:
		if !d.CapLifecycle {
			return deny("它控制「应用生命周期」（重载 / 退出 / 重启）与清空日志")
		}
	case debugsrv.CapEval:
		if !d.AllowEval {
			return deny("它沿用既有开关「允许执行 JS」")
		}
	default:
		return fmt.Errorf("未知能力 %q：请更新应用（该能力可能是更新版本才支持的）", cap)
	}
	return nil
}

// Snapshot 返回当前能力快照（用于 app.permissions 与 initialize.instructions）。
func (g debugGate) Snapshot() map[debugsrv.Capability]bool {
	d := g.ctrl.app.currentDebugSettings()
	return map[debugsrv.Capability]bool{
		debugsrv.CapRead:          true, // 读永远允许
		debugsrv.CapTerminalInput: d.CapTerminalInput,
		debugsrv.CapUIWrite:       d.CapUIWrite,
		debugsrv.CapConfigWrite:   d.CapConfigWrite,
		debugsrv.CapSecretsRead:   d.AllowSecrets,
		debugsrv.CapSecretsWrite:  d.AllowSecrets && d.CapSecretWrite,
		debugsrv.CapRemoteFSWrite: d.CapRemoteFSWrite,
		// sudo 凭证提权同样是「能力位 + 允许读取敏感数据」双条件（与 CapSecretsWrite 同款）。
		debugsrv.CapSudoCredential: d.AllowSecrets && d.CapSudoCredential,
		debugsrv.CapLifecycle:      d.CapLifecycle,
		debugsrv.CapEval:           d.AllowEval,
	}
}

// pathAllowed 判断远端路径是否命中白名单前缀。
//
// 规则（后续 SFTP 写入必须复用同一函数，避免两处判断不一致）：
//   - 前缀完全相等，或以「前缀 + /」开头（防止 /srv/app 把 /srv/app-evil 也放进去）；
//   - 大小写敏感（远端多为 Linux）。
func pathAllowed(allowlist []string, path string) bool {
	p := strings.TrimRight(strings.TrimSpace(path), "/")
	if p == "" {
		return false
	}
	for _, prefix := range allowlist {
		prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
		if prefix == "" {
			continue
		}
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// -------- 动作执行器（confirm.commit 的真正执行者）--------

// debugExecutor 实现 debugsrv.Executor：执行一次已经过两段式确认的动作。
type debugExecutor struct{ ctrl *debugController }

// ExecuteAction 执行确认过的动作。
//
// 两条路径共用同一份实现：
//   - MCP 写工具：token 由 mcp.go 校验并消费，随后经 Handler 的 op 落到 handleAppctlOp；
//   - confirm.commit：token 在这里被消费后，由下面的动作 → op 映射走同一个 handleAppctlOp。
//
// 已实现：capability.enable / settings.update / settings.reset / logs.clear 与
// M7 的全部 servers.* / credentials.* / tunnels.* / app.quit / app.command
// （映射见 ctlActionOps）。
func (e debugExecutor) ExecuteAction(ctx context.Context, action string, args map[string]any) (any, error) {
	c := e.ctrl
	switch strings.TrimSpace(action) {
	case "capability.enable":
		return c.executeCapabilityEnable(args)
	case "settings.update":
		return c.executeSettingsUpdate(args)
	case "settings.reset":
		return c.ctlSettingsReset(ctlArgList(args, "paths"))
	case "logs.clear":
		removed := c.app.ClearLogs()
		c.logAudit("mcp", "logs.clear", string(debugsrv.CapLifecycle), args, true, "", false, "")
		return removed, nil
	}
	// ---- M7 应用控制面：动作 → op ----
	// 为什么不在这里重写一遍业务逻辑：MCP 工具路径与 confirm.commit 路径必须完全一致，
	// 复用 Handle（→ handleAppctlOp → app.go 的既有绑定）是唯一不会写歪的做法。
	if op, ok := ctlActionOps[strings.TrimSpace(action)]; ok {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("%w: 动作参数无法序列化: %v", debugsrv.ErrBadInput, err)
		}
		return c.Handle(ctx, op, b)
	}
	return nil, fmt.Errorf("动作 %q 暂未实现执行器：本波可通过应用界面完成，"+
		"或等待对应工具接入后再调用", action)
}

// ctlActionOps 确认动作名 → 宿主 op 的映射（只列 confirm.commit 需要直接执行的动作）。
//
// 注意 servers.duplicate 对应动作名 servers.create、servers.moveGroup 与 credentials.attach
// 对应 servers.update（见 appctl_register.go 的登记）：同一个 action 名被多个工具复用，
// 因此这里不能靠 action 反推 op —— AI 用 confirm.commit 时应改用对应的写工具本身
// （写工具会把 token 与自己的参数一起提交，语义更精确）。
var ctlActionOps = map[string]string{
	"servers.create":     debugsrv.OpServersCreate,
	"servers.update":     debugsrv.OpServersUpdate,
	"servers.delete":     debugsrv.OpServersDelete,
	"credentials.create": debugsrv.OpCredentialsCreate,
	"credentials.update": debugsrv.OpCredentialsUpdate,
	"credentials.delete": debugsrv.OpCredentialsDelete,
	"tunnels.create":     debugsrv.OpTunnelsCreate,
	"tunnels.update":     debugsrv.OpTunnelsUpdate,
	"tunnels.delete":     debugsrv.OpTunnelsDelete,
	"tunnels.start":      debugsrv.OpTunnelsStart,
	"tunnels.stop":       debugsrv.OpTunnelsStop,
	"app.command":        debugsrv.OpAppCommand,
	"app.quit":           debugsrv.OpAppQuit,
	// ---- M8：SFTP 写类动作（沿用同一条「动作 → op」路径，宿主侧会再校验能力位 + 白名单）----
	// 注意 sftp.syncCwd 复用动作名 sftp.write（见 sftp_register.go），因此用 confirm.commit
	// 执行时会落到 OpSftpWrite —— 与 M7 的 servers.create 复用情形一样，
	// 精确执行请改用写工具本身（把 token 作为工具参数传入）。
	"sftp.write":  debugsrv.OpSftpWrite,
	"sftp.mkdir":  debugsrv.OpSftpMkdir,
	"sftp.rename": debugsrv.OpSftpRename,
	"sftp.remove": debugsrv.OpSftpRemove,
	"sftp.cancel": debugsrv.OpSftpCancel,
}

// executeCapabilityEnable 执行 capability.enable：打开 / 关闭一个能力位。
//
// 为什么把「开启能力」也列为破坏性动作：它等于给 AI 扩权，用户明确要求
// 「SFTP 写/删打开需要多次确认」，因此它必须走 confirm.prepare → confirm.commit。
func (c *debugController) executeCapabilityEnable(args map[string]any) (any, error) {
	name, _ := args["cap"].(string)
	capName, ok := debugsrv.KnownCapability(name)
	if !ok {
		return nil, fmt.Errorf("未知能力 %q：可选值见 app.permissions 的 descriptions", strings.TrimSpace(name))
	}
	enabled := true
	if v, has := args["enabled"]; has {
		if b, isBool := v.(bool); isBool {
			enabled = b
		}
	}
	if err := c.app.applyCapability(capName, enabled, "mcp"); err != nil {
		return nil, err
	}
	return map[string]any{
		"cap":     string(capName),
		"enabled": enabled,
		"caps":    c.app.capabilitySnapshot(),
	}, nil
}

// executeSettingsUpdate 执行 settings.update：把设置局部合并后保存（与 update_settings 同路径）。
func (c *debugController) executeSettingsUpdate(args map[string]any) (any, error) {
	patch, has := args["settings"]
	if !has {
		return nil, fmt.Errorf("%w: 缺少 settings", debugsrv.ErrBadInput)
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("%w: settings 不是合法 JSON: %v", debugsrv.ErrBadInput, err)
	}
	cur, err := c.app.settings.Get()
	if err != nil {
		return nil, err
	}
	before := cur
	if err := json.Unmarshal(b, &cur); err != nil {
		return nil, fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
	}
	if err := c.app.SaveSettings(cur); err != nil {
		return nil, err
	}
	c.saveSettingsUndo("update_settings", debugsrv.SettingsPatchArgs(map[string]any{"settings": patch}), before)
	return c.settingsView(cur), nil
}

// executeSettingsReset 已由 ctlSettingsReset 取代（M7：支持 paths 局部重置，
// 且默认值抽成 ctlDefaultSettings 供 settings.schema 与重置共用同一份事实）。

// logAudit 追加一条来自宿主内部（executor / UI）的审计记录并推给前端。
func (c *debugController) logAudit(source, tool, capName string, args map[string]any, ok bool, errMsg string, reversible bool, hint string) {
	if c.audits == nil {
		return
	}
	rec := c.audits.Append(debugsrv.AuditRecord{
		TS:         time.Now().UnixMilli(),
		Source:     source,
		Tool:       tool,
		Cap:        capName,
		Args:       debugsrv.NormalizeArgsJSON(args),
		OK:         ok,
		Error:      errMsg,
		Reversible: reversible,
		RevertHint: hint,
	})
	if c.hub != nil {
		c.hub.PublishRaw("debug:audit", rec)
	}
}

// -------- 能力位：前端绑定（设置 → 调试模式 → MCP 能力）--------

// GetCapabilities 供设置页渲染「MCP 能力」卡片：当前快照 + 说明 + 工具能力表。
func (a *App) GetCapabilities() map[string]any {
	return map[string]any{
		"caps":               a.capabilitySnapshot(),
		"descriptions":       debugsrv.CapabilityDescriptions(),
		"labels":             a.capabilityLabels(),
		"requiresConfirm":    []string{"fs.remote.write", "sudo.credential", "lifecycle"}, // 前端打开时二次确认的能力
		"confirmActions":     debugsrv.ConfirmableActions(),
		"sftpWriteAllowlist": a.sftpWriteAllowlist(),
		"readAlwaysAllowed":  true,
		"confirmNote": "破坏性操作两段式确认：固定开启（AI 必须先 confirm.prepare 拿到 token 才能执行写操作），" +
			"不提供开关。「远端文件写入」「sudo 凭证提权」与「应用生命周期」在打开时需要二次确认。",
	}
}

// capabilitySnapshot 返回能力位快照（与 debugGate.Snapshot 同源，供前端与 app.health 使用）。
func (a *App) capabilitySnapshot() map[string]bool {
	d := a.currentDebugSettings()
	return map[string]bool{
		string(debugsrv.CapRead):          true,
		string(debugsrv.CapTerminalInput): d.CapTerminalInput,
		string(debugsrv.CapUIWrite):       d.CapUIWrite,
		string(debugsrv.CapConfigWrite):   d.CapConfigWrite,
		string(debugsrv.CapSecretsRead):   d.AllowSecrets,
		string(debugsrv.CapSecretsWrite):  d.AllowSecrets && d.CapSecretWrite,
		string(debugsrv.CapRemoteFSWrite): d.CapRemoteFSWrite,
		// sudo.credential：能力位 + 「允许读取敏感数据」双条件（密码只能从凭据库读）。
		string(debugsrv.CapSudoCredential): d.AllowSecrets && d.CapSudoCredential,
		string(debugsrv.CapLifecycle):      d.CapLifecycle,
		string(debugsrv.CapEval):           d.AllowEval,
	}
}

// capabilityLabels 返回「能力名 → 中文短名」（前端列表标题）。
func (a *App) capabilityLabels() map[string]string {
	out := make(map[string]string, len(debugsrv.AllCapabilities()))
	for _, c := range debugsrv.AllCapabilities() {
		out[string(c)] = debugsrv.CapabilityLabel(c)
	}
	return out
}

// sftpWriteAllowlist 当前 SFTP 写白名单（前端白名单编辑区使用）。
func (a *App) sftpWriteAllowlist() []string {
	d := a.currentDebugSettings()
	if d.SFTPWriteAllowlist == nil {
		return []string{}
	}
	return d.SFTPWriteAllowlist
}

// SetCapability 前端「MCP 能力」开关：只改设置并即时生效。
//
// 后端仍然要校验前置条件（不能因为前端拦过一次就放松）：
//   - secrets.write 需要 AllowSecrets 已开启；
//   - fs.remote.write 打开后若白名单为空，能力本身是「开着但什么都写不了」，
//     这里不报错（用户可能先开开关再填白名单），但 app.permissions 的说明里会提示。
//
// 敏感能力（fs.remote.write / lifecycle）的「二次确认」由前端交互保证
// （参考「清理日志」的确认交互）；后端不做 token 校验 —— 这是用户在界面上的直接操作，
// 与 MCP 的 AI 调用不同，后者必须走 confirm.prepare → confirm.commit。
func (a *App) SetCapability(capName string, enabled bool) error {
	c, ok := debugsrv.KnownCapability(capName)
	if !ok {
		return fmt.Errorf("未知能力 %q", capName)
	}
	return a.applyCapability(c, enabled, "ui")
}

// applyCapability 改一个能力位并立即生效（前端开关与 capability.enable 共用）。
//
// 为什么共用：两条路径的校验规则必须完全一致，否则会出现「UI 能开、AI 开不了」
// 之类的不一致行为。source 只影响审计记录的来源标记（ui / mcp）。
func (a *App) applyCapability(c debugsrv.Capability, enabled bool, source string) error {
	if a.settings == nil {
		return errors.New("设置存储未初始化")
	}
	cur, err := a.settings.Get()
	if err != nil {
		return fmt.Errorf("读取设置失败: %w", err)
	}
	before := cur.Debug
	// 关掉敏感能力时不清空白名单：用户可能只是临时关掉再打开，
	// 保留已填好的路径更符合直觉。
	switch c {
	case debugsrv.CapTerminalInput:
		cur.Debug.CapTerminalInput = enabled
	case debugsrv.CapUIWrite:
		cur.Debug.CapUIWrite = enabled
	case debugsrv.CapConfigWrite:
		cur.Debug.CapConfigWrite = enabled
	case debugsrv.CapSecretsWrite:
		if enabled && !cur.Debug.AllowSecrets {
			return errors.New("开启「凭据写入」前必须先开启「允许读取敏感数据」（凭据写入必然要读回凭据）")
		}
		cur.Debug.CapSecretWrite = enabled
	case debugsrv.CapSecretsRead:
		// 读取敏感数据对应既有 AllowSecrets 开关（与调试模式页签里的那个开关同源）
		cur.Debug.AllowSecrets = enabled
		if !enabled {
			cur.Debug.CapSecretWrite = false // 不允许读就不可能允许写
			// sudo 提权同样要读回保存的密码：不允许读时一并回落（NormalizeDebugSettings 也会兜底）
			cur.Debug.CapSudoCredential = false
		}
	case debugsrv.CapRemoteFSWrite:
		cur.Debug.CapRemoteFSWrite = enabled
	case debugsrv.CapSudoCredential:
		if enabled && !cur.Debug.AllowSecrets {
			return errors.New("开启「sudo 凭证提权」前必须先开启「允许读取敏感数据」（密码只能从凭据库读出来，" +
				"不允许读取敏感数据就不可能用保存的密码提权）")
		}
		cur.Debug.CapSudoCredential = enabled
	case debugsrv.CapLifecycle:
		cur.Debug.CapLifecycle = enabled
	case debugsrv.CapEval:
		cur.Debug.AllowEval = enabled
	case debugsrv.CapRead:
		if !enabled {
			return errors.New("「读取」能力永远允许，无法关闭（调试模式下只读是基础能力）")
		}
		return nil
	default:
		return fmt.Errorf("能力 %q 不支持通过该接口修改", c)
	}
	cur.Debug = models.NormalizeDebugSettings(cur.Debug)
	if err := a.SaveSettings(cur); err != nil {
		return fmt.Errorf("保存能力设置失败: %w", err)
	}
	// 审计：能力开关是「扩权」动作，必须有记录（含改动前的值，便于用户回看自己开了什么）。
	args := map[string]any{"cap": string(c), "enabled": enabled, "before": capabilityFieldValue(before, c)}
	a.logCapabilityChange(source, args)
	logx.Infof("MCP 能力变更（来源 %s）：%s = %v", source, c, enabled)
	return nil
}

// logCapabilityChange 记录一次能力开关变更（调试服务未启动时直接写审计缓冲）。
func (a *App) logCapabilityChange(source string, args map[string]any) {
	const hint = "可在「设置 → 调试模式 → MCP 能力」中手动改回"
	capName, _ := args["cap"].(string)
	if a.debug != nil {
		a.debug.logAudit(source, "capability.enable", capName, args, true, "", true, hint)
		return
	}
	if a.audits == nil {
		return
	}
	_ = a.audits.Append(debugsrv.AuditRecord{
		TS: time.Now().UnixMilli(), Source: source, Tool: "capability.enable", Cap: capName,
		Args: debugsrv.NormalizeArgsJSON(args), OK: true, Reversible: true, RevertHint: hint,
	})
}

// capabilityFieldValue 读取某个能力位在设置里的当前值（审计用）。
func capabilityFieldValue(d models.DebugSettings, c debugsrv.Capability) bool {
	switch c {
	case debugsrv.CapTerminalInput:
		return d.CapTerminalInput
	case debugsrv.CapUIWrite:
		return d.CapUIWrite
	case debugsrv.CapConfigWrite:
		return d.CapConfigWrite
	case debugsrv.CapSecretsWrite:
		return d.CapSecretWrite
	case debugsrv.CapSecretsRead:
		return d.AllowSecrets
	case debugsrv.CapRemoteFSWrite:
		return d.CapRemoteFSWrite
	case debugsrv.CapSudoCredential:
		return d.CapSudoCredential
	case debugsrv.CapLifecycle:
		return d.CapLifecycle
	case debugsrv.CapEval:
		return d.AllowEval
	}
	return false
}

// -------- 审计：前端绑定（设置 → AI 记录）--------

// GetAuditRecords 返回最近的审计记录（最新的在前），供「AI 记录」页签展示。
//
// limit <= 0 时默认 50；capName / source 为空表示不过滤（与 MCP 的 app.recent_calls 一致）。
func (a *App) GetAuditRecords(limit int, capName string, source string) []map[string]any {
	if a.audits == nil {
		return []map[string]any{}
	}
	records := a.audits.Recent(limit, capName, source)
	out := make([]map[string]any, 0, len(records))
	for _, r := range records {
		// 逐条转成 map（前端只读取）：顺带把「能否撤销」算好，
		// 免得前端自己猜 —— 撤销能力取决于宿主是否还留着改动前快照。
		out = append(out, map[string]any{
			"id":         r.ID,
			"ts":         r.TS,
			"source":     r.Source,
			"tool":       r.Tool,
			"cap":        r.Cap,
			"args":       r.Args,
			"ok":         r.OK,
			"error":      r.Error,
			"durationMs": r.DurationMS,
			"reversible": r.Reversible && a.canUndo(r),
			"revertHint": r.RevertHint,
		})
	}
	return out
}

// canUndo 判断某条记录现在是否真的能撤销（只有设置类改动且快照还在缓存里才行）。
//
// 审计记录里的 Args 与快照的键同源：两侧都先用 debugsrv.SettingsPatchArgs
// 归一化成「只含 settings 补丁」的形状（见那里的说明）。
func (a *App) canUndo(rec debugsrv.AuditRecord) bool {
	if a.debug == nil {
		return false
	}
	if !isSettingsUndoTool(rec.Tool) {
		return false
	}
	key := settingsUndoKey(rec.Tool, settingsPatchJSON(rec.Args))
	a.debug.undoMu.Lock()
	defer a.debug.undoMu.Unlock()
	_, ok := a.debug.settingsUndo[key]
	return ok
}

// settingsPatchJSON 把「审计记录里的 args JSON」归一化成设置补丁的规范化 JSON。
// 解析失败时退回原文（宁可算不出键、不显示撤销按钮，也不要误撤销）。
func settingsPatchJSON(argsJSON string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return argsJSON
	}
	return debugsrv.NormalizeArgsJSON(debugsrv.SettingsPatchArgs(m))
}

// UndoAuditRecord 撤销一条设置类审计记录：把设置恢复为那次改动前的值。
//
// 参数用 (id, tool, args)：前端从审计记录里原样取出这三项传回来，
// 这样即使环形缓冲已把记录淘汰，只要有记录快照就仍能撤销。
// 本波只支持 update_settings / set_log_options（记录里 reversible=true 的那些）；
// 其余记录返回中文错误，前端会把「撤销」按钮置灰并显示原因。
func (a *App) UndoAuditRecord(id string, tool string, args string) error {
	if a.debug == nil {
		return errors.New("调试服务未初始化，无法撤销")
	}
	tool = strings.TrimSpace(tool)
	if !isSettingsUndoTool(tool) {
		return fmt.Errorf("该记录（%s）不支持撤销：本波只对设置类改动提供「恢复为改动前值」，其余动作无法可靠回滚", tool)
	}
	key := settingsUndoKey(tool, settingsPatchJSON(strings.TrimSpace(args)))
	a.debug.undoMu.Lock()
	before, has := a.debug.settingsUndo[key]
	if has {
		delete(a.debug.settingsUndo, key) // 快照一次性：撤销后不能重复撤销
	}
	a.debug.undoMu.Unlock()
	if !has {
		return fmt.Errorf("找不到记录 %s 的改动前快照（只保留最近 %d 条，或已被撤销过）", id, settingsUndoKeep)
	}
	if err := a.SaveSettings(before); err != nil {
		return fmt.Errorf("恢复设置失败: %w", err)
	}
	a.debug.logAudit("ui", "settings.undo", string(debugsrv.CapConfigWrite),
		map[string]any{"auditId": id, "tool": tool}, true, "", false, "")
	logx.Infof("已撤销审计记录 %s（恢复为改动前的设置值）", id)
	return nil
}

// isSettingsUndoTool 判断工具名是否属于「可撤销的设置类改动」。
func isSettingsUndoTool(tool string) bool {
	return tool == "update_settings" || tool == "set_log_options"
}

// settingsUndoKeep 设置撤销快照的保留条数（超出后丢弃最旧的）。
const settingsUndoKeep = 20

// settingsUndoKey 生成撤销快照的键。
//
// 为什么用「JSON 参数哈希 + 工具名」而不是审计记录 ID：快照必须在 handler 里
// （改动之前）就存好，而审计记录 ID 要等中间件记账后才产生 —— 两者无法互相引用。
// 参数是调用方传入的原始 payload，两次调用只要 payload 相同就算同一次改动，
// 这对「AI 改了设置 → 用户点撤销」的场景完全够用（不确定时宁可不给撤销按钮）。
func settingsUndoKey(tool string, patchJSON string) string {
	sum := sha256.Sum256([]byte(patchJSON))
	return tool + "|" + hex.EncodeToString(sum[:8])
}

// saveSettingsUndo 记录一次设置改动前的快照（供「AI 记录」页签撤销）。
// payload 为调用方传入的参数（已规范化），与审计记录里的 args 完全同源。
func (c *debugController) saveSettingsUndo(tool string, payload map[string]any, before models.Settings) {
	if c == nil {
		return
	}
	key := settingsUndoKey(tool, debugsrv.NormalizeArgsJSON(payload))
	c.undoMu.Lock()
	if c.settingsUndo == nil {
		c.settingsUndo = make(map[string]models.Settings)
	}
	c.settingsUndo[key] = before
	c.undoOrder = append(c.undoOrder, key)
	for len(c.undoOrder) > settingsUndoKeep {
		oldest := c.undoOrder[0]
		c.undoOrder = c.undoOrder[1:]
		delete(c.settingsUndo, oldest)
	}
	c.undoMu.Unlock()
}

// -------- 应用控制面（M7）：宿主实现 --------
//
// 这一节是 internal/debugsrv/appctl.go 的「另一侧」：把所有 servers.* / credentials.* /
// tunnels.* / settings.* / app.* 的 op 落到 app.go 的**既有绑定**上
//（GetServers / SaveServer / DeleteServer / GetCredentials / SaveCredential /
// StartTunnel / UpdateTunnel / StopTunnel / RestartTunnel / RemoveTunnel / ListTunnels /
// SaveSettings / ClearLogs …），绝不直接读写 SQLite。
//
// 分权与脱敏都发生在 debugsrv 侧（能力位、两段式确认、明文脱敏、体积限额），
// 这里只负责「做事」：因此本节的返回值可以包含明文（同一进程内），
// 但不会有任何一条明文被写进返回值给 AI —— 脱敏在 appctl.go 里统一做。

// ctlArgStr 读取字符串参数（去首尾空白）。
func ctlArgStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// ctlArgInt 读取整数参数（JSON 数字统一是 float64）。
func ctlArgInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// ctlArgBool 读取布尔参数。
func ctlArgBool(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

// ctlArgList 读取字符串数组参数。
func ctlArgList(args map[string]any, key string) []string {
	switch v := args[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			out = append(out, strings.TrimSpace(s))
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	}
	return nil
}

// ctlArgMap 读取对象参数。
func ctlArgMap(args map[string]any, key string) map[string]any {
	if v, ok := args[key].(map[string]any); ok {
		return v
	}
	return nil
}

// ctlArgObjList 读取「对象数组」参数。
func ctlArgObjList(args map[string]any, key string) []map[string]any {
	arr, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, isMap := item.(map[string]any); isMap {
			out = append(out, m)
		}
	}
	return out
}

// ctlIsEmptyValue 判断「未提供 / 空值」：空值在 servers.update 的合并语义里表示「不修改」。
func ctlIsEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case float64:
		return t == 0
	case bool:
		return !t
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// ctlServerNodeFields models.ServerNode 的全部可写字段（JSON 名）。
//
// 为什么显式列出：合并更新要拒绝拼错的字段名（否则 AI 以为改了、实际被忽略），
// 而 ServerNode 带 omitempty，用「序列化后再比对键」会把空字段当成未知字段。
var ctlServerNodeFields = []string{
	"id", "name", "group", "host", "port", "user", "authType",
	"password", "keyPath", "keyContent", "bgImage", "blurAmount", "envVars",
}

// ctlServerNodeFromArgs 把 args[node] 转成 models.ServerNode。
func ctlServerNodeFromArgs(args map[string]any) (models.ServerNode, error) {
	raw := ctlArgMap(args, "node")
	if len(raw) == 0 {
		return models.ServerNode{}, fmt.Errorf("%w: 缺少 node", debugsrv.ErrBadInput)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return models.ServerNode{}, fmt.Errorf("%w: node 无法序列化: %v", debugsrv.ErrBadInput, err)
	}
	var node models.ServerNode
	if err := json.Unmarshal(b, &node); err != nil {
		return models.ServerNode{}, fmt.Errorf("%w: node 不是合法服务器对象: %v", debugsrv.ErrBadInput, err)
	}
	return node, nil
}

// findServer 按 id 找服务器（找不到返回中文 ErrNotFound）。
func (c *debugController) findServer(id string) (models.ServerNode, error) {
	for _, n := range c.app.GetServers() {
		if n.ID == id {
			return n, nil
		}
	}
	return models.ServerNode{}, fmt.Errorf("%w: 未找到服务器 %s（可用 servers.list 查看）", debugsrv.ErrNotFound, id)
}

// ctlCredentialMeta 凭据元信息（**唯一**允许离开宿主的凭据形状：不含密码 / 私钥内容）。
func ctlCredentialMeta(c models.Credential) map[string]any {
	return map[string]any{
		"id":         c.ID,
		"name":       c.Name,
		"type":       c.AuthType,
		"user":       c.User,
		"keyPath":    c.KeyPath,
		"hasContent": c.Password != "" || c.KeyContent != "",
	}
}

// handleAppctlOp 执行一个应用控制面 op（op 集合见 debugsrv.IsAppctlOp）。
func (c *debugController) handleAppctlOp(ctx context.Context, op string, args map[string]any) (any, error) {
	switch op {
	// ---- A. 服务器 ----
	case debugsrv.OpServersList:
		nodes := c.app.GetServers()
		return map[string]any{"count": len(nodes), "servers": nodes}, nil

	case debugsrv.OpServersGet:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		node, err := c.findServer(id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"server": node}, nil

	case debugsrv.OpServersCreate:
		node, err := ctlServerNodeFromArgs(args)
		if err != nil {
			return nil, err
		}
		// 新建语义：ID 由 SaveServer 生成，不接受调用方指定（避免误覆盖既有服务器）。
		node.ID = ""
		saved, err := c.app.SaveServer(node)
		if err != nil {
			return nil, err
		}
		return map[string]any{"server": saved}, nil

	case debugsrv.OpServersUpdate:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		cur, err := c.findServer(id)
		if err != nil {
			return nil, err
		}
		patch := ctlArgMap(args, "node")
		if len(patch) == 0 {
			return nil, fmt.Errorf("%w: 缺少 node", debugsrv.ErrBadInput)
		}
		merged, err := ctlMergeServerNode(cur, patch)
		if err != nil {
			return nil, err
		}
		saved, err := c.app.SaveServer(merged)
		if err != nil {
			return nil, err
		}
		return map[string]any{"server": saved}, nil

	case debugsrv.OpServersDelete:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		node, err := c.findServer(id)
		if err != nil {
			return nil, err
		}
		if err := c.app.DeleteServer(id); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "id": id, "name": node.Name}, nil

	case debugsrv.OpServersDuplicate:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		node, err := c.findServer(id)
		if err != nil {
			return nil, err
		}
		node.ID = ""
		if name := ctlArgStr(args, "name"); name != "" {
			node.Name = name
		} else {
			if strings.TrimSpace(node.Name) == "" {
				node.Name = node.User + "@" + node.Host
			}
			node.Name += "（副本）"
		}
		saved, err := c.app.SaveServer(node)
		if err != nil {
			return nil, err
		}
		return map[string]any{"server": saved}, nil

	case debugsrv.OpServersMoveGroup:
		ids := ctlArgList(args, "ids")
		if len(ids) == 0 {
			return nil, fmt.Errorf("%w: 缺少 ids", debugsrv.ErrBadInput)
		}
		group := ctlArgStr(args, "group")
		moved := make([]string, 0, len(ids))
		missing := make([]string, 0)
		for _, id := range ids {
			node, err := c.findServer(id)
			if err != nil {
				missing = append(missing, id)
				continue
			}
			node.Group = group
			if _, err := c.app.SaveServer(node); err != nil {
				return nil, err
			}
			moved = append(moved, id)
		}
		return map[string]any{"moved": len(moved), "ids": moved, "missing": missing, "group": group}, nil

	case debugsrv.OpServersImport:
		items := ctlArgObjList(args, "servers")
		if len(items) == 0 {
			return nil, fmt.Errorf("%w: 缺少 servers（导入数据）", debugsrv.ErrBadInput)
		}
		mode := ctlArgStr(args, "mode")
		if mode == "" {
			mode = "merge"
		}
		if mode != "merge" && mode != "replace" {
			return nil, fmt.Errorf("%w: 非法 mode %q", debugsrv.ErrBadInput, mode)
		}
		deleted := 0
		if mode == "replace" {
			for _, n := range c.app.GetServers() {
				if err := c.app.DeleteServer(n.ID); err != nil {
					return nil, err
				}
				deleted++
			}
		}
		existing := map[string]bool{}
		for _, n := range c.app.GetServers() {
			existing[n.ID] = true
		}
		created, updated := 0, 0
		for _, m := range items {
			b, err := json.Marshal(m)
			if err != nil {
				return nil, fmt.Errorf("%w: 导入条目无法序列化: %v", debugsrv.ErrBadInput, err)
			}
			var node models.ServerNode
			if err := json.Unmarshal(b, &node); err != nil {
				return nil, fmt.Errorf("%w: 导入条目不是合法服务器对象: %v", debugsrv.ErrBadInput, err)
			}
			isUpdate := node.ID != "" && existing[node.ID]
			saved, err := c.app.SaveServer(node)
			if err != nil {
				return nil, err
			}
			if isUpdate {
				updated++
			} else {
				created++
			}
			existing[saved.ID] = true
		}
		return map[string]any{
			"mode": mode, "imported": created, "updated": updated, "deleted": deleted,
			"total": len(c.app.GetServers()),
		}, nil

	// ---- B. 凭据 ----
	case debugsrv.OpCredentialsList:
		if err := c.app.ensureUnlocked(); err != nil {
			return nil, fmt.Errorf("%w: %v（请先在应用内解锁主密码）", debugsrv.ErrForbidden, err)
		}
		creds := c.app.GetCredentials()
		metas := make([]map[string]any, 0, len(creds))
		for _, cred := range creds {
			// 只把元信息交给 debugsrv（明文不出宿主）。
			metas = append(metas, ctlCredentialMeta(cred))
		}
		return map[string]any{"count": len(metas), "credentials": metas}, nil

	case debugsrv.OpCredentialsCreate:
		name := ctlArgStr(args, "name")
		typ := ctlArgStr(args, "type")
		content := ctlArgStr(args, "content")
		if name == "" || content == "" || (typ != "password" && typ != "privateKey") {
			return nil, fmt.Errorf("%w: 需要 name / type(password|privateKey) / content", debugsrv.ErrBadInput)
		}
		cred := models.Credential{Name: name, AuthType: typ, User: ctlArgStr(args, "user")}
		if typ == "privateKey" {
			cred.KeyContent = content
		} else {
			cred.Password = content
		}
		saved, err := c.app.SaveCredential(cred)
		if err != nil {
			return nil, err
		}
		return map[string]any{"credential": ctlCredentialMeta(saved)}, nil

	case debugsrv.OpCredentialsUpdate:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		if err := c.app.ensureUnlocked(); err != nil {
			return nil, fmt.Errorf("%w: %v（请先在应用内解锁主密码）", debugsrv.ErrForbidden, err)
		}
		var found *models.Credential
		for _, cred := range c.app.GetCredentials() {
			if cred.ID == id {
				c := cred
				found = &c
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("%w: 未找到凭据 %s", debugsrv.ErrNotFound, id)
		}
		if v := ctlArgStr(args, "name"); v != "" {
			found.Name = v
		}
		if v := ctlArgStr(args, "user"); v != "" {
			found.User = v
		}
		if content := ctlArgStr(args, "content"); content != "" {
			// 内容只在「同一种认证方式」内替换：换了类型要显式改 authType（本工具不改类型，
			// 避免 password 与 keyContent 两份内容同时残留）。
			if found.AuthType == "privateKey" {
				found.KeyContent = content
			} else {
				found.Password = content
			}
		}
		saved, err := c.app.SaveCredential(*found)
		if err != nil {
			return nil, err
		}
		return map[string]any{"credential": ctlCredentialMeta(saved)}, nil

	case debugsrv.OpCredentialsDelete:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		if err := c.app.DeleteCredential(id); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "id": id}, nil

	case debugsrv.OpCredentialsAttach:
		serverID := ctlArgStr(args, "serverId")
		credID := ctlArgStr(args, "credentialId")
		if serverID == "" || credID == "" {
			return nil, fmt.Errorf("%w: 需要 serverId 与 credentialId", debugsrv.ErrBadInput)
		}
		node, err := c.findServer(serverID)
		if err != nil {
			return nil, err
		}
		if err := c.app.ensureUnlocked(); err != nil {
			return nil, fmt.Errorf("%w: %v（请先在应用内解锁主密码）", debugsrv.ErrForbidden, err)
		}
		var cred *models.Credential
		for _, item := range c.app.GetCredentials() {
			if item.ID == credID {
				v := item
				cred = &v
				break
			}
		}
		if cred == nil {
			return nil, fmt.Errorf("%w: 未找到凭据 %s", debugsrv.ErrNotFound, credID)
		}
		// 只写入凭据里**有值**的字段，避免把服务器原有的配置清空。
		applied := make([]string, 0, 5)
		if cred.User != "" {
			node.User = cred.User
			applied = append(applied, "user")
		}
		if cred.AuthType != "" {
			node.AuthType = cred.AuthType
			applied = append(applied, "authType")
		}
		if cred.Password != "" {
			node.Password = cred.Password
			node.KeyContent = ""
			node.KeyPath = ""
			applied = append(applied, "password")
		}
		if cred.KeyContent != "" {
			node.KeyContent = cred.KeyContent
			node.Password = ""
			applied = append(applied, "keyContent")
		}
		if cred.KeyPath != "" {
			node.KeyPath = cred.KeyPath
			applied = append(applied, "keyPath")
		}
		saved, err := c.app.SaveServer(node)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "server": saved, "fields": applied}, nil

	// ---- C. 隧道 ----
	case debugsrv.OpTunnelsList:
		tunnels := c.app.ListTunnels()
		return map[string]any{"count": len(tunnels), "tunnels": tunnels}, nil

	case debugsrv.OpTunnelsCreate, debugsrv.OpTunnelsUpdate:
		return c.ctlTunnelUpsert(ctx, op, args)

	case debugsrv.OpTunnelsStart:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		if err := c.app.RestartTunnel(id); err != nil {
			return nil, err
		}
		return map[string]any{"tunnel": c.ctlTunnelInfo(id)}, nil

	case debugsrv.OpTunnelsStop:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		if err := c.app.StopTunnel(id); err != nil {
			return nil, err
		}
		return map[string]any{"tunnel": c.ctlTunnelInfo(id)}, nil

	case debugsrv.OpTunnelsDelete:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		info := c.ctlTunnelInfo(id) // 删除前先取一份（删除后查不到名字）
		if err := c.app.RemoveTunnel(id); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "id": id, "name": info.Name}, nil

	// ---- D. 设置 ----
	case debugsrv.OpSettingsSchema:
		cur, err := c.app.settings.Get()
		if err != nil {
			return nil, err
		}
		redacted := []string{}
		if !ctlArgBool(args, "includeSecrets") {
			// localShell 是本机路径（与既有 get_settings 的 settingsRedacted 行为一致）。
			cur.LocalShell = ""
			redacted = append(redacted, "localShell")
		}
		return map[string]any{
			"defaults":      ctlDefaultSettings(),
			"current":       cur,
			"meta":          ctlSettingsFieldMeta(),
			"redactedPaths": redacted,
			"notes": []string{
				"字段清单由默认设置树递归生成，覆盖全部设置项（含 theme.* / appearance.* / fonts.* / debug.* 嵌套字段）。",
				"settings.reset 整体恢复会刻意保留 debug.enabled / debug.port / debug.bindLan / debug.allowEval / debug.allowSecrets，" +
					"因此这几个字段的 default 与「整体重置后的实际值」可能不同（避免把调试控制面自己关掉导致失联）。",
			},
		}, nil

	case debugsrv.OpSettingsReset:
		return c.ctlSettingsReset(ctlArgList(args, "paths"))

	// ---- E. 内置命令（页面内事实：命令面板的定义与执行都在前端，见 bridge.ts）----
	case debugsrv.OpAppCommands:
		return c.callFrontend(ctx, "app.commands", nil)

	case debugsrv.OpAppCommand:
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id", debugsrv.ErrBadInput)
		}
		payload := map[string]any{"id": id}
		if v, has := args["args"]; has && v != nil {
			payload["args"] = v
		}
		return c.callFrontend(ctx, "app.command", payload)

	// ---- F. 生命周期 ----
	case debugsrv.OpAppQuit:
		// 必须「先回包、后退出」：否则响应还没写回连接，应用就没了（AI 只会看到连接被断开）。
		go func() {
			time.Sleep(300 * time.Millisecond)
			if c.app.ctx != nil {
				wailsruntime.Quit(c.app.ctx)
			}
		}()
		return map[string]any{"ok": true, "quitting": true}, nil
	}
	return nil, fmt.Errorf("%w: 未知的应用控制面操作 %q", debugsrv.ErrBadInput, op)
}

// ctlMergeServerNode 把 patch 合并到现有节点上：只覆盖「非空」字段，id 不可改。
func ctlMergeServerNode(cur models.ServerNode, patch map[string]any) (models.ServerNode, error) {
	known := make(map[string]bool, len(ctlServerNodeFields))
	for _, f := range ctlServerNodeFields {
		known[f] = true
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return cur, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return cur, err
	}
	for k, v := range patch {
		if k == "id" {
			continue // 主键不可通过 node 修改
		}
		if !known[k] {
			return cur, fmt.Errorf("%w: 未知的服务器字段 %q", debugsrv.ErrBadInput, k)
		}
		if ctlIsEmptyValue(v) {
			continue // 空值 = 不修改（避免误清空密码 / 私钥）
		}
		m[k] = v
	}
	merged, err := json.Marshal(m)
	if err != nil {
		return cur, err
	}
	var out models.ServerNode
	if err := json.Unmarshal(merged, &out); err != nil {
		return cur, fmt.Errorf("%w: 合并后的服务器对象不合法: %v", debugsrv.ErrBadInput, err)
	}
	out.ID = cur.ID
	return out, nil
}

// ctlTunnelUpsert 新建 / 修改隧道：把 MCP 侧的统一参数映射到 sshx 的隧道参数。
//
// 映射规则（与 internal/sshx.normalizeTunnelConfig 对齐，详见 appctl.go 的说明）：
//
//	local   : localPort=listenPort，remoteHost=targetHost（默认 127.0.0.1），remotePort=targetPort
//	remote  : remoteHost=listenHost（默认 127.0.0.1），remotePort=listenPort，localPort=targetPort
//	          （当前实现里本地目标固定 127.0.0.1）
//	dynamic : localPort=listenPort（SOCKS5），不需要 target*
func (c *debugController) ctlTunnelUpsert(ctx context.Context, op string, args map[string]any) (any, error) {
	node, err := c.resolveTunnelNode(ctx, ctlArgStr(args, "serverId"), ctlArgStr(args, "sessionId"))
	if err != nil {
		return nil, err
	}
	mode := ctlArgStr(args, "type")
	name := ctlArgStr(args, "name")
	listenHost := ctlArgStr(args, "listenHost")
	listenPort := ctlArgInt(args, "listenPort")
	targetHost := ctlArgStr(args, "targetHost")
	targetPort := ctlArgInt(args, "targetPort")

	localPort, remoteHost, remotePort := 0, "", 0
	switch mode {
	case "local":
		localPort, remoteHost, remotePort = listenPort, targetHost, targetPort
		if remoteHost == "" {
			remoteHost = "127.0.0.1"
		}
	case "remote":
		remoteHost, remotePort, localPort = listenHost, listenPort, targetPort
		if remoteHost == "" {
			remoteHost = "127.0.0.1"
		}
	case "dynamic":
		localPort = listenPort
	default:
		return nil, fmt.Errorf("%w: 非法 type %q（只支持 local | remote | dynamic）", debugsrv.ErrBadInput, mode)
	}

	var info models.TunnelInfo
	if op == debugsrv.OpTunnelsCreate {
		info, err = c.app.StartTunnel(node, name, mode, localPort, remoteHost, remotePort)
	} else {
		id := ctlArgStr(args, "id")
		if id == "" {
			return nil, fmt.Errorf("%w: 缺少 id（隧道 id）", debugsrv.ErrBadInput)
		}
		info, err = c.app.UpdateTunnel(id, node, name, mode, localPort, remoteHost, remotePort)
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{"tunnel": info}
	switch {
	case mode == "remote":
		out["targetNote"] = "remote 模式下本地目标固定为 127.0.0.1:targetPort（当前实现），targetHost 被忽略。"
	case mode != "remote" && listenHost != "" && listenHost != "127.0.0.1":
		out["listenHostNote"] = fmt.Sprintf("当前实现里 %s 模式的本地监听地址固定为 127.0.0.1，已忽略 listenHost=%s。", mode, listenHost)
	}
	return out, nil
}

// resolveTunnelNode 解析隧道要用的服务器节点：serverId 走本地存储，sessionId 走前端桥
// （标签里保存着完整的服务器节点，只有页面知道）。
func (c *debugController) resolveTunnelNode(ctx context.Context, serverID, sessionID string) (models.ServerNode, error) {
	if serverID != "" {
		return c.findServer(serverID)
	}
	if sessionID == "" {
		return models.ServerNode{}, fmt.Errorf("%w: 需要 serverId 或 sessionId", debugsrv.ErrBadInput)
	}
	v, err := c.callFrontend(ctx, "session.node", map[string]any{"id": sessionID})
	if err != nil {
		return models.ServerNode{}, fmt.Errorf("解析会话 %s 的服务器节点失败：%w", sessionID, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return models.ServerNode{}, err
	}
	var node models.ServerNode
	if err := json.Unmarshal(b, &node); err != nil {
		return models.ServerNode{}, fmt.Errorf("%w: 前端返回的会话节点不是合法服务器对象", debugsrv.ErrBadInput)
	}
	if node.Host == "" {
		return models.ServerNode{}, fmt.Errorf("会话 %s 没有可用的服务器节点（本机终端不能作为隧道的 SSH 连接）", sessionID)
	}
	return node, nil
}

// ctlTunnelInfo 从隧道列表里取一条（找不到返回零值，调用方按需处理）。
func (c *debugController) ctlTunnelInfo(id string) models.TunnelInfo {
	for _, t := range c.app.ListTunnels() {
		if t.ID == id {
			return t
		}
	}
	return models.TunnelInfo{}
}

// ctlDefaultSettings 应用设置的默认值（与 store 在「配置文件不存在」时给的默认一致）。
//
// 单独抽出来是为了让 settings.schema 的 default 与 settings.reset 的落点**共用同一份事实**，
// 避免两处各写一套默认值后悄悄漂移。
func ctlDefaultSettings() models.Settings {
	return models.Settings{
		WebGLEnabled:         true,
		CompletionEnabled:    true,
		CompletionNavHotkey:  "Alt+ArrowDown",
		CompletionPanelLimit: 8,
		SftpToTerminalSync:   true,
		TerminalToSftpSync:   true,
		UIScale:              100,
		AutoReconnect:        true,
		KeepAliveEnabled:     true,
		TabBarPlacement:      "side",
		RightClickAction:     models.RightClickMenu,
		NavSectionOrder:      models.DefaultNavSectionOrder,
		LogLevel:             models.NormalizeLogLevel(""),
		Theme:                models.DefaultTheme(),
		Appearance:           models.DefaultAppearance(),
		Fonts:                models.DefaultFonts(),
		Debug:                models.DefaultDebugSettings(),
	}
}

// ctlSettingsFieldMeta 设置字段的补充元信息（enum / restartRequired / risky / note）。
//
// 只列「需要注解」的字段；未列出的字段走默认（无 enum、restartRequired=false、risky=false）。
// risky=true 的含义：改动会削弱安全边界，或直接影响调试控制面自身（改错了 AI 会失联）。
func ctlSettingsFieldMeta() map[string]any {
	capRisky := "这是 MCP 能力位：打开等于给 AI 扩权（见 设置 → 调试模式 → MCP 能力）"
	return map[string]any{
		"logLevel":                 map[string]any{"enum": []any{"debug", "info", "warn", "error"}, "note": "只有 debug 才会记录终端跟踪等细粒度信息"},
		"tabBarPlacement":          map[string]any{"enum": []any{"top", "side"}, "note": "会话标签页位置：顶栏横向 / 左侧导航纵向"},
		"rightClickAction":         map[string]any{"enum": []any{"menu", "paste"}, "note": "终端鼠标右键行为"},
		"navSectionOrder":          map[string]any{"note": "nav / sessions / tabs 三个键的排列，逗号分隔（如 \"nav,sessions,tabs\"）"},
		"completionNavHotkey":      map[string]any{"note": "补全导航开关键，如 Alt+ArrowDown"},
		"uiScale":                  map[string]any{"note": "界面缩放百分比（80-150）"},
		"logTraceTabs":             map[string]any{"risky": true, "note": "被跟踪会话的终端输出会写入日志文件（含命令与输出内容），注意隐私"},
		"appearance.mode":          map[string]any{"enum": []any{"preset", "custom"}},
		"appearance.baseTone":      map[string]any{"enum": []any{"auto", "light", "dark"}},
		"debug.enabled":            map[string]any{"risky": true, "note": "关掉后调试服务立即停止（MCP 连接一并失效）"},
		"debug.port":               map[string]any{"note": "0 = 自动挑空闲端口；改动会立即重启调试服务"},
		"debug.bindLan":            map[string]any{"risky": true, "note": "true = 监听 0.0.0.0，局域网内可访问（务必配合 token）"},
		"debug.allowEval":          map[string]any{"risky": true, "note": "允许在页面上下文执行任意 JS"},
		"debug.allowSecrets":       map[string]any{"risky": true, "note": "允许读取含密码 / 私钥的敏感字段（secrets.read 也依赖它）"},
		"debug.cdpEnabled":         map[string]any{"restartRequired": true, "note": "WebView2 远程调试端口需要在启动时确定"},
		"debug.capTerminalInput":   map[string]any{"risky": true, "note": capRisky},
		"debug.capUiWrite":         map[string]any{"risky": true, "note": capRisky},
		"debug.capConfigWrite":     map[string]any{"risky": true, "note": capRisky},
		"debug.capSecretWrite":     map[string]any{"risky": true, "note": capRisky + "（还需「允许读取敏感数据」）"},
		"debug.capRemoteFsWrite":   map[string]any{"risky": true, "note": capRisky + "（SFTP 写入，另受 sftpWriteAllowlist 白名单限制）"},
		"debug.capSudoCredential":  map[string]any{"risky": true, "note": capRisky + "（用应用里保存的密码执行 sudo -i = 交出该机器 root；还需「允许读取敏感数据」，且每次执行仍需两段式确认）"},
		"debug.capLifecycle":       map[string]any{"risky": true, "note": capRisky + "（重载 / 退出 / 清空日志）"},
		"debug.sftpWriteAllowlist": map[string]any{"risky": true, "note": "远端可写路径前缀白名单；留空 = 禁止任何写入"},
	}
}

// ctlSettingsMap 把 Settings 转成 JSON 对象（重置单字段时用来按路径读写）。
func ctlSettingsMap(s models.Settings) (map[string]any, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ctlLookupPath 按点分路径在 JSON 对象里取值。
func ctlLookupPath(root map[string]any, path string) (any, bool) {
	var cur any = root
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, has := m[part]
		if !has {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// ctlSetPath 按点分路径写入 JSON 对象（路径必须已存在，避免悄悄造出不存在的字段）。
func ctlSetPath(root map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	cur := root
	for i, part := range parts {
		if i == len(parts)-1 {
			if _, has := cur[part]; !has {
				return fmt.Errorf("%w: 未知设置字段 %q（可用 settings.schema 查看全部字段）", debugsrv.ErrBadInput, path)
			}
			cur[part] = value
			return nil
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			return fmt.Errorf("%w: 设置字段 %q 不是对象，无法按路径访问", debugsrv.ErrBadInput, strings.Join(parts[:i+1], "."))
		}
		cur = next
	}
	return nil
}

// ctlPreserveOnReset 判断某字段是否属于「重置时刻意保留」的调试通道字段。
func ctlPreserveOnReset(path string) bool {
	switch path {
	case "debug.enabled", "debug.port", "debug.bindLan":
		return true
	}
	return false
}

// ctlSettingsReset 重置设置：paths 为空 = 整体恢复默认（保留调试通道字段）；否则只重置指定字段。
func (c *debugController) ctlSettingsReset(paths []string) (any, error) {
	cur, err := c.app.settings.Get()
	if err != nil {
		return nil, err
	}
	before := cur
	if len(paths) == 0 {
		def := ctlDefaultSettings()
		// 刻意保留调试模式的「连通性」字段：否则重置会把调试服务本身关掉，AI 立刻失联。
		def.Debug.Enabled = cur.Debug.Enabled
		def.Debug.Port = cur.Debug.Port
		def.Debug.BindLAN = cur.Debug.BindLAN
		def.Debug.AllowEval = cur.Debug.AllowEval
		def.Debug.AllowSecrets = cur.Debug.AllowSecrets
		if err := c.app.SaveSettings(def); err != nil {
			return nil, err
		}
		c.saveSettingsUndo("update_settings", debugsrv.SettingsPatchArgs(map[string]any{"reset": true}), before)
		out := c.settingsView(def)
		out["reset"] = "all"
		out["resetPaths"] = []string{}
		return out, nil
	}

	curMap, err := ctlSettingsMap(cur)
	if err != nil {
		return nil, err
	}
	defMap, err := ctlSettingsMap(ctlDefaultSettings())
	if err != nil {
		return nil, err
	}
	resetPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		if ctlPreserveOnReset(p) {
			return nil, fmt.Errorf("%w: 字段 %q 在重置时被刻意保留（改了会把调试控制面自己关掉、导致 AI 失联）；"+
				"如确需修改请用 update_settings", debugsrv.ErrBadInput, p)
		}
		if _, ok := ctlLookupPath(curMap, p); !ok {
			return nil, fmt.Errorf("%w: 未知设置字段 %q（可用 settings.schema 查看全部字段）", debugsrv.ErrBadInput, p)
		}
		def, ok := ctlLookupPath(defMap, p)
		if !ok {
			return nil, fmt.Errorf("%w: 字段 %q 没有默认值，无法重置", debugsrv.ErrBadInput, p)
		}
		if err := ctlSetPath(curMap, p, def); err != nil {
			return nil, err
		}
		resetPaths = append(resetPaths, p)
	}
	b, err := json.Marshal(curMap)
	if err != nil {
		return nil, err
	}
	var next models.Settings
	if err := json.Unmarshal(b, &next); err != nil {
		return nil, fmt.Errorf("%w: 重置后的设置无法解析: %v", debugsrv.ErrBadInput, err)
	}
	if err := c.app.SaveSettings(next); err != nil {
		return nil, err
	}
	c.saveSettingsUndo("update_settings", debugsrv.SettingsPatchArgs(map[string]any{"paths": paths}), before)
	out := c.settingsView(next)
	out["reset"] = "paths"
	out["resetPaths"] = resetPaths
	return out, nil
}

// -------- M8：SFTP 宿主实现（sftp.* op）--------
//
// 分工（见 internal/debugsrv/sftp.go 顶部注释）：
//   - debugsrv：工具条目、入参校验、路径白名单、体积限额与截断标记、终端输出增量；
//   - 宿主（本段）：**只负责真的去碰远端**，且一律复用会话上已经建立的那一份 SFTP 客户端
//     （App.manager 的 Manager.SftpClient → sshx.Session.SftpClient，懒创建、随会话关闭），
//     不新建 SSH 连接、不自己实现 SFTP 协议；
//   - 终端（terminal.run / terminal.expect）完全在 debugsrv 侧实现，这里不新增终端逻辑。
//
// 为什么白名单在这里**再校验一次**：confirm.commit 走的是
// debugExecutor.ExecuteAction → ctlActionOps → 本文件的 op，会绕过 debugsrv 的 preflight；
// 两条路径共用 debugsrv 导出的同一套规则函数（NormalizeRemotePath / SFTPPathAllowed），
// 不会出现「工具调用拦得住、commit 拦不住」的缺口。

// debugSFTPPaths 实现 debugsrv.RemoteFSPathScope：把设置里的远端写白名单交给 debugsrv 预检。
type debugSFTPPaths struct{ ctrl *debugController }

// ctlSFTPWriteMaxBytes 单次远端写入的内容上限（与 debugsrv 的 sftpWriteMaxBytes 保持一致）。
const ctlSFTPWriteMaxBytes = 1 << 20

// SFTPWriteAllowlist 返回当前白名单（始终非 nil，便于调用方判断「空 = 拒绝一切写」）。
func (p debugSFTPPaths) SFTPWriteAllowlist() []string {
	if p.ctrl == nil || p.ctrl.app == nil {
		return []string{}
	}
	return p.ctrl.app.sftpWriteAllowlist()
}

// isHostSftpOp 判断 op 是否属于本段实现的 SFTP 宿主操作。
//
// 注意 terminal.run / terminal.expect 不在其中：它们完全由 debugsrv 实现
// （订阅 Hub 的 ssh.output 事件 + 复用既有 OpTerminalInput）。
func isHostSftpOp(op string) bool {
	switch strings.TrimSpace(op) {
	case debugsrv.OpSftpList, debugsrv.OpSftpStat, debugsrv.OpSftpRead, debugsrv.OpSftpWrite,
		debugsrv.OpSftpMkdir, debugsrv.OpSftpRename, debugsrv.OpSftpRemove,
		debugsrv.OpSftpSyncCwd, debugsrv.OpSftpCancel:
		return true
	}
	return false
}

// handleSftpOp 分发 SFTP 宿主 op。
func (c *debugController) handleSftpOp(ctx context.Context, op string, args map[string]any) (any, error) {
	switch op {
	case debugsrv.OpSftpList:
		return c.sftpHostList(args)
	case debugsrv.OpSftpStat:
		return c.sftpHostStat(args)
	case debugsrv.OpSftpRead:
		return c.sftpHostRead(args)
	case debugsrv.OpSftpWrite:
		return c.sftpHostWrite(args)
	case debugsrv.OpSftpMkdir:
		return c.sftpHostMkdir(args)
	case debugsrv.OpSftpRename:
		return c.sftpHostRename(args)
	case debugsrv.OpSftpRemove:
		return c.sftpHostRemove(args)
	case debugsrv.OpSftpSyncCwd:
		return c.sftpHostSyncCwd(args)
	case debugsrv.OpSftpCancel:
		return c.sftpHostCancel(args)
	}
	return nil, fmt.Errorf("%w: 未知 SFTP 操作 %q", debugsrv.ErrBadInput, op)
}

// ctlSftpSessionID 读取会话 id（主名 sessionId，兼容 id）。
func ctlSftpSessionID(args map[string]any) string {
	if sess := ctlArgStr(args, "sessionId"); sess != "" {
		return sess
	}
	return ctlArgStr(args, "id")
}

// ctlSftpClient 取会话的 SFTP 客户端（复用会话已有的连接，不新建）。
func (c *debugController) ctlSftpClient(args map[string]any) (*sftp.Client, string, error) {
	sess := ctlSftpSessionID(args)
	if sess == "" {
		return nil, "", fmt.Errorf("%w: 缺少 sessionId", debugsrv.ErrBadInput)
	}
	if c.app == nil || c.app.manager == nil {
		return nil, sess, errors.New("会话管理器未初始化")
	}
	client, err := c.app.manager.SftpClient(sess)
	if err != nil {
		if errors.Is(err, sshx.ErrSessionNotFound) {
			return nil, sess, fmt.Errorf("会话 %s 不存在或不是 SSH 会话（本机终端没有 SFTP）："+
				"请先用 list_terminals 确认会话 id", sess)
		}
		return nil, sess, fmt.Errorf("建立 SFTP 连接失败（会话 %s）：%w", sess, err)
	}
	if client == nil {
		return nil, sess, fmt.Errorf("会话 %s 的 SFTP 连接不可用（可能已断开）", sess)
	}
	return client, sess, nil
}

// ctlRequireRemoteFSWrite 宿主侧的能力位 + 白名单复核。
//
// detail 传**已归一化**的远端路径；为空时只校验「能力位已开 + 白名单非空」
// （例如取消传输没有路径，但仍属于远端写类）。
func (c *debugController) ctlRequireRemoteFSWrite(detail string) error {
	gate := debugGate{c}
	return gate.Allow(debugsrv.CapRemoteFSWrite, detail)
}

// ctlRequireSftpWritePaths 逐条校验写目标路径，返回归一化后的路径。
func (c *debugController) ctlRequireSftpWritePaths(paths ...string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		p, err := debugsrv.NormalizeRemotePath(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
		}
		if err := c.ctlRequireRemoteFSWrite(p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ctlExpandRemoteTilde 把「~」「~/…」展开成会话的远端 home。
//
// 只对**读**路径生效（写路径在 debugsrv 层已被要求是绝对路径，不会走到这里）。
// 依据：OpenSSH 的 SFTP 子系统起始目录就是登录用户的 home，Getwd 即可拿到；
// 这样 AI 用 sftp.list{sessionId}（默认 "."）或 path="~" 都能落到 home，
// 而不是收到一句英文的「file does not exist」。
func ctlExpandRemoteTilde(client *sftp.Client, p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	wd, err := client.Getwd()
	if err != nil || wd == "" {
		return p
	}
	if p == "~" {
		return wd
	}
	return strings.TrimRight(wd, "/") + p[1:]
}

// ctlSftpErrText 把 SFTP / OS 的常见英文错误翻成中文（保留原文便于排查）。
func ctlSftpErrText(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case errors.Is(err, os.ErrNotExist),
		strings.Contains(msg, "file does not exist"),
		strings.Contains(msg, "no such file"):
		return "远端路径不存在（" + msg + "）"
	case errors.Is(err, os.ErrPermission), strings.Contains(msg, "permission denied"):
		return "权限不足（" + msg + "）"
	case errors.Is(err, os.ErrExist):
		return "远端路径已存在（" + msg + "）"
	}
	return msg
}

// sftpHostList 列目录：直接用会话的 SFTP 客户端 ReadDir。
//
// 为什么不复用 App.SftpList：那条路径是 SWR 缓存（先回缓存再后台校验），
// AI 需要的是**此刻的真实目录**，而且 models.SFTPEntry 没有 mode / isSymlink 字段。
// 写入类操作完成后会调用 Manager.InvalidateSftpCache 让面板缓存失效，
// 因此「AI 写文件 → 用户面板刷新」不会读到旧值。
func (c *debugController) sftpHostList(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	path := ctlArgStr(args, "path")
	if path == "" {
		path = "."
	}
	path = ctlExpandRemoteTilde(client, path)
	limit := ctlArgInt(args, "limit")
	if limit <= 0 {
		limit = 500
	}
	infos, err := client.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("读取远端目录 %q 失败（会话 %s）：%s", path, sess, ctlSftpErrText(err))
	}
	resolved := path
	if !strings.HasPrefix(path, "/") {
		if wd, werr := client.Getwd(); werr == nil && strings.HasPrefix(wd, "/") {
			resolved = wd
		}
	}
	base := resolved
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	entries := make([]debugsrv.SFTPEntryInfo, 0, len(infos))
	for _, fi := range infos {
		if len(entries) >= limit {
			break
		}
		entries = append(entries, debugsrv.SFTPEntryInfo{
			Name:      fi.Name(),
			Path:      base + fi.Name(),
			IsDir:     fi.IsDir(),
			IsSymlink: fi.Mode()&os.ModeSymlink != 0,
			Size:      fi.Size(),
			Mode:      fi.Mode().String(),
			ModTime:   fi.ModTime().UnixMilli(),
		})
	}
	return debugsrv.SFTPListPayload{Path: resolved, Entries: entries}, nil
}

// sftpHostStat 读单个路径的属性（Lstat 语义：符号链接看链接本身）。
func (c *debugController) sftpHostStat(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	path := ctlArgStr(args, "path")
	if path == "" {
		return nil, fmt.Errorf("%w: 缺少 path", debugsrv.ErrBadInput)
	}
	path = ctlExpandRemoteTilde(client, path)
	fi, err := client.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("读取远端路径 %q 属性失败（会话 %s）：%s", path, sess, ctlSftpErrText(err))
	}
	out := debugsrv.SFTPStatPayload{
		Path:      path,
		IsDir:     fi.IsDir(),
		IsSymlink: fi.Mode()&os.ModeSymlink != 0,
		Size:      fi.Size(),
		Mode:      fi.Mode().String(),
		ModTime:   fi.ModTime().UnixMilli(),
	}
	if out.IsSymlink {
		if target, rerr := client.ReadLink(path); rerr == nil {
			out.Target = target
			if tfi, serr := client.Stat(path); serr == nil {
				out.TargetIsDir = tfi.IsDir()
			}
		}
	}
	return out, nil
}

// ctlSftpWriteContentB64 把写类入参解释成 base64 内容（sftpHostWrite 用）。
//
// 两条路径共用：
//   - MCP 工具路径：debugsrv 的 sftpWrite 已经把内容编码好，传 {"base64": "…"}；
//   - confirm.commit 路径：宿主直接收到 AI 在 confirm.prepare 时登记的自然形状
//     （{"content": "…"}），因此这里按 sftp.go 的 sftpWriteContent 同一套规则再解释一次。
//
// 判定依据是**键是否存在**而不是值是否为空：空字符串是合法内容（写 0 字节文件），
// 必须与「两个键都没给」区分开 —— 否则 `sftp.write{content:""}`（sftpWriteContent 的
// 中文提示里明确支持的写法）会被误判成「缺少写入内容」，返回里也只能看到一句
// 与真实原因不符的错误（M9 收尾修）。
func ctlSftpWriteContentB64(args map[string]any) (string, error) {
	if raw, hasB64 := args["base64"]; hasB64 {
		s, isStr := raw.(string)
		if !isStr {
			return "", fmt.Errorf("%w: base64 必须是字符串", debugsrv.ErrBadInput)
		}
		return strings.TrimSpace(s), nil
	}
	raw, hasContent := args["content"]
	if !hasContent {
		return "", fmt.Errorf("%w: 缺少写入内容（content / base64）", debugsrv.ErrBadInput)
	}
	s, isStr := raw.(string)
	if !isStr {
		return "", fmt.Errorf("%w: content 必须是字符串", debugsrv.ErrBadInput)
	}
	if strings.EqualFold(ctlArgStr(args, "encoding"), "base64") {
		if _, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(s)); derr != nil {
			return "", fmt.Errorf("%w: content 不是合法 base64：%v", debugsrv.ErrBadInput, derr)
		}
		return strings.TrimSpace(s), nil
	}
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}

// sftpHostRead 读文件的一段字节（最多 maxBytes+1，多读的那一个字节用来判断有没有被截断）。
func (c *debugController) sftpHostRead(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	path := ctlArgStr(args, "path")
	if path == "" {
		return nil, fmt.Errorf("%w: 缺少 path", debugsrv.ErrBadInput)
	}
	path = ctlExpandRemoteTilde(client, path)
	maxBytes := ctlArgInt(args, "maxBytes")
	if maxBytes <= 0 {
		maxBytes = 256 << 10
	}
	offset := int64(ctlArgInt(args, "offset"))
	if offset < 0 {
		offset = 0
	}
	f, err := client.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开远端文件 %q 失败（会话 %s）：%s", path, sess, ctlSftpErrText(err))
	}
	defer func() { _ = f.Close() }()
	if fi, serr := f.Stat(); serr == nil {
		if fi.IsDir() {
			return debugsrv.SFTPReadPayload{Path: path, IsDir: true}, nil
		}
	}
	if offset > 0 {
		if _, serr := f.Seek(offset, io.SeekStart); serr != nil {
			return nil, fmt.Errorf("定位远端文件 %q 到偏移 %d 失败：%w", path, offset, serr)
		}
	}
	buf, rerr := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if rerr != nil {
		return nil, fmt.Errorf("读取远端文件 %q 失败：%w", path, rerr)
	}
	eof := len(buf) <= maxBytes
	if !eof {
		buf = buf[:maxBytes]
	}
	size := int64(0)
	if fi, serr := f.Stat(); serr == nil {
		size = fi.Size()
	}
	return debugsrv.SFTPReadPayload{Path: path, Data: buf, Size: size, EOF: eof}, nil
}

// sftpHostWrite 写入 / 追加远端文件。
func (c *debugController) sftpHostWrite(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	// 宿主侧复核（confirm.commit 路径绕过了 debugsrv 的 preflight）。
	paths, err := c.ctlRequireSftpWritePaths(ctlArgStr(args, "path"))
	if err != nil {
		return nil, err
	}
	target := paths[0]
	contentB64, err := ctlSftpWriteContentB64(args)
	if err != nil {
		return nil, err
	}
	data, derr := base64.StdEncoding.DecodeString(contentB64)
	if derr != nil {
		return nil, fmt.Errorf("%w: base64 解码失败：%v", debugsrv.ErrBadInput, derr)
	}
	if len(data) > ctlSFTPWriteMaxBytes {
		return nil, fmt.Errorf("%w: 写入内容 %d 字节超过上限 %d 字节", debugsrv.ErrBadInput, len(data), ctlSFTPWriteMaxBytes)
	}
	appendMode := ctlArgBool(args, "append")
	if ctlArgBool(args, "createDirs") {
		if parent := path.Dir(target); parent != "" && parent != "/" {
			if merr := client.MkdirAll(parent); merr != nil {
				return nil, fmt.Errorf("创建远端父目录 %q 失败：%w", parent, merr)
			}
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, oerr := client.OpenFile(target, flags)
	if oerr != nil {
		return nil, fmt.Errorf("打开远端文件 %q 写入失败：%w", target, oerr)
	}
	n, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return nil, fmt.Errorf("写入远端文件 %q 失败：%w", target, werr)
	}
	if cerr != nil {
		return nil, fmt.Errorf("关闭远端文件 %q 失败：%w", target, cerr)
	}
	c.app.manager.InvalidateSftpCache(path.Dir(target))
	logx.Infof("MCP SFTP 写入: session=%s path=%s bytes=%d append=%v", sess, target, n, appendMode)
	return debugsrv.SFTPWritePayload{Path: target, Bytes: n, Appended: appendMode}, nil
}

// sftpHostMkdir 创建远端目录（recursive=true 等价于 mkdir -p）。
func (c *debugController) sftpHostMkdir(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	paths, err := c.ctlRequireSftpWritePaths(ctlArgStr(args, "path"))
	if err != nil {
		return nil, err
	}
	target := paths[0]
	recursive := ctlArgBool(args, "recursive")
	if recursive {
		err = client.MkdirAll(target)
	} else {
		err = client.Mkdir(target)
	}
	if err != nil {
		return nil, fmt.Errorf("创建远端目录 %q 失败（会话 %s，recursive=%v）：%w", target, sess, recursive, err)
	}
	c.app.manager.InvalidateSftpCache(path.Dir(target))
	logx.Infof("MCP SFTP 建目录: session=%s path=%s recursive=%v", sess, target, recursive)
	return debugsrv.SFTPMutationPayload{OK: true, Path: target, Recursive: recursive}, nil
}

// sftpHostRename 重命名 / 移动（from 与 to 都已过白名单）。
func (c *debugController) sftpHostRename(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	paths, err := c.ctlRequireSftpWritePaths(ctlArgStr(args, "from"), ctlArgStr(args, "to"))
	if err != nil {
		return nil, err
	}
	from, to := paths[0], paths[1]
	if rerr := client.Rename(from, to); rerr != nil {
		return nil, fmt.Errorf("重命名 %q → %q 失败（会话 %s）：%w", from, to, sess, rerr)
	}
	c.app.manager.InvalidateSftpCache(path.Dir(from))
	c.app.manager.InvalidateSftpCache(path.Dir(to))
	logx.Infof("MCP SFTP 重命名: session=%s from=%s to=%s", sess, from, to)
	return debugsrv.SFTPMutationPayload{OK: true, From: from, To: to}, nil
}

// sftpHostRemove 删除文件 / 目录。
//
// recursive=false 时若目标是目录会明确拒绝（而不是像既有 App.SftpRemove 那样一律递归删除）：
// 「删目录」是本次调试控制面里风险最高的动作，宁可多一轮交互也不做隐式递归。
// 用 Lstat 判断类型：删除符号链接时删的是链接本身，不会递归删掉它指向的目录内容。
func (c *debugController) sftpHostRemove(args map[string]any) (any, error) {
	client, sess, err := c.ctlSftpClient(args)
	if err != nil {
		return nil, err
	}
	paths, err := c.ctlRequireSftpWritePaths(ctlArgStr(args, "path"))
	if err != nil {
		return nil, err
	}
	target := paths[0]
	recursive := ctlArgBool(args, "recursive")
	fi, lerr := client.Lstat(target)
	if lerr != nil {
		return nil, fmt.Errorf("读取远端路径 %q 失败（会话 %s）：%w", target, sess, lerr)
	}
	switch {
	case fi.IsDir() && recursive:
		// 复用既有实现：sshx.Manager.SftpRemove 内部就是「先删子项再删自身」的递归删除。
		if rerr := c.app.SftpRemove(sess, target); rerr != nil {
			return nil, fmt.Errorf("递归删除远端目录 %q 失败：%w", target, rerr)
		}
	case fi.IsDir():
		return nil, fmt.Errorf("%w: 目标 %q 是目录：删除目录必须显式传 recursive=true（会递归删除全部子项，不可恢复）",
			debugsrv.ErrBadInput, target)
	default:
		if rerr := client.Remove(target); rerr != nil {
			return nil, fmt.Errorf("删除远端路径 %q 失败：%w", target, rerr)
		}
		c.app.manager.InvalidateSftpCache(path.Dir(target))
	}
	logx.Infof("MCP SFTP 删除: session=%s path=%s recursive=%v", sess, target, recursive)
	return debugsrv.SFTPMutationPayload{OK: true, Path: target, Recursive: recursive}, nil
}

// sftpHostSyncCwd 把 SFTP 面板的当前目录切到远端路径（推一条既有的 sftp:sync-path 事件）。
//
// 刻意**不**向终端键入 cd：那属于 terminal.input 能力（等于代替用户敲命令），
// 而本工具按规格是「非破坏性」的界面同步动作。
func (c *debugController) sftpHostSyncCwd(args map[string]any) (any, error) {
	sess := ctlSftpSessionID(args)
	if sess == "" {
		return nil, fmt.Errorf("%w: 缺少 sessionId", debugsrv.ErrBadInput)
	}
	paths, err := c.ctlRequireSftpWritePaths(ctlArgStr(args, "remotePath"))
	if err != nil {
		return nil, err
	}
	path := paths[0]
	if serr := c.app.SetSftpPathFromTerminal(sess, path); serr != nil {
		return nil, fmt.Errorf("同步 SFTP 目录失败（会话 %s）：%w", sess, serr)
	}
	logx.Debugf("MCP SFTP 目录同步: session=%s path=%s", sess, path)
	return debugsrv.SFTPMutationPayload{OK: true, Path: path}, nil
}

// sftpHostCancel 取消一条进行中的传输（复用既有 Manager.SftpCancelTransfer）。
func (c *debugController) sftpHostCancel(args map[string]any) (any, error) {
	sess := ctlSftpSessionID(args)
	direction := ctlArgStr(args, "direction")
	name := ctlArgStr(args, "name")
	if sess == "" || direction == "" || name == "" {
		// confirm.commit 路径：args 里只有 transferId（AI confirm.prepare 时登记的形状）。
		if id := ctlArgStr(args, "transferId"); id != "" {
			s, d, n, perr := debugsrv.ParseTransferID(id)
			if perr != nil {
				return nil, perr
			}
			sess, direction, name = s, d, n
		}
	}
	if sess == "" || direction == "" || name == "" {
		return nil, fmt.Errorf("%w: 缺少 sessionId / direction / name（或 transferId）", debugsrv.ErrBadInput)
	}
	// 取消传输没有路径可查白名单，但仍属于远端写类：能力位 + 非空白名单必须满足。
	if err := c.ctlRequireRemoteFSWrite(""); err != nil {
		return nil, err
	}
	if err := c.app.SftpCancelTransfer(sess, direction, name); err != nil {
		return nil, fmt.Errorf("取消传输失败（session=%s direction=%s name=%s）：%w；"+
			"可先用 sftp.transfers 确认它仍在进行中", sess, direction, name, err)
	}
	return debugsrv.SFTPMutationPayload{OK: true, Path: name}, nil
}

// -------- M10：sudo 凭证提权（terminal.sudo）的宿主实现 --------
//
// 分层（照 M8 的 SFTP 写法，不另起炉灶）：
//   - debugsrv/sudo.go：能力位 / 两段式确认 / 临时文件随机命名 / 「一条 shell 执行 + 清理 + 校验」的编排 /
//     输出净化的调用方；
//   - 本文件：**唯一持有明文密码**的一侧 —— 解析「会话 → 服务器节点」，按 `servers.get includeSecrets=true`
//     的同一条宿主路径（App.GetServers → store.List，敏感字段在存储层解密）取保存的密码，
//     用会话**已有的那份 SFTP 客户端**写进 debugsrv 指定的临时文件；净化时把输出里的密码换成 ***。
//
// 密码的硬边界（与 internal/debugsrv/sudo.go 顶部注释一一对应）：
//   - **不回传**：OpSudoStage 的返回值只有 {ok, path, bytes}；OpSudoScrub 只回净化后的文本；
//   - **不落日志**：本节的 logx 只记会话 / 路径 / 字节数，绝不记内容（也不行记前缀）；
//   - **不进审计**：这两个 op 不是 MCP 工具，不经过审计记录；AI 传的参数里本来也没有密码字段；
//   - **不落盘**：临时文件由 debugsrv 侧在同一条 shell 里 rm + ls 校验删除。

// isHostSudoOp 判断 op 是否属于 sudo 凭证提权的宿主操作。
func isHostSudoOp(op string) bool {
	switch strings.TrimSpace(op) {
	case debugsrv.OpSudoStage, debugsrv.OpSudoScrub:
		return true
	}
	return false
}

// handleSudoOp 分发 sudo 宿主 op。
func (c *debugController) handleSudoOp(ctx context.Context, op string, args map[string]any) (any, error) {
	switch op {
	case debugsrv.OpSudoStage:
		return c.sudoHostStage(ctx, args)
	case debugsrv.OpSudoScrub:
		return c.sudoHostScrub(ctx, args)
	}
	return nil, fmt.Errorf("%w: 未知 sudo 操作 %q", debugsrv.ErrBadInput, op)
}

// ctlSudoCredential 解析「会话 → 服务器节点 → 保存的密码」，返回节点与明文密码。
//
// 校验顺序与中文原因（用户明确要求逐条指出拒绝原因）：
//  1. 会话必须存在且不是本机终端：本机终端单独一条文案（没有远端账号，也没有 SFTP 通道）；
//  2. 会话必须对应一台服务器（走前端桥的 session.node，与 tunnels.create 的 sessionId 路径同源）；
//  3. 该服务器必须保存了密码：按 servers.get includeSecrets 的同一条宿主路径取，取不到 / 为空都拒绝。
//
// 调用方（stage / scrub）**都不会**把 password 放进返回值给 debugsrv。
func (c *debugController) ctlSudoCredential(ctx context.Context, sessionID string) (models.ServerNode, string, error) {
	sess := strings.TrimSpace(sessionID)
	if sess == "" {
		return models.ServerNode{}, "", fmt.Errorf("%w: 缺少 sessionId", debugsrv.ErrBadInput)
	}
	// 1) 本机终端：明确拒绝（工具描述里也写明了不支持）。
	if c.app != nil && c.app.local != nil && c.app.local.Has(sess) {
		return models.ServerNode{}, "", errors.New("本机终端不支持 sudo 凭证提权：本机终端没有远端账号，" +
			"也没有可用的 SFTP 通道（本机请直接用 terminal.run / send_input 手动输入）")
	}
	// 2) 会话 → 服务器节点（完整节点只存在于页面标签里）。
	v, err := c.callFrontend(ctx, "session.node", map[string]any{"id": sess})
	if err != nil {
		return models.ServerNode{}, "", fmt.Errorf("会话 %s 没有对应的服务器：sudo 凭证提权只能用于「由已保存服务器打开的 SSH 会话」（%v）",
			sess, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return models.ServerNode{}, "", fmt.Errorf("解析会话 %s 的服务器节点失败：%w", sess, err)
	}
	var node models.ServerNode
	if err := json.Unmarshal(b, &node); err != nil {
		return models.ServerNode{}, "", fmt.Errorf("%w: 前端返回的会话节点不是合法服务器对象", debugsrv.ErrBadInput)
	}
	if node.Host == "" {
		return models.ServerNode{}, "", fmt.Errorf("会话 %s 没有对应的服务器："+
			"sudo 凭证提权需要从凭据库读取该服务器保存的密码（本机终端 / 未绑定服务器的会话都不支持）", sess)
	}
	// 3) 保存的密码：优先按 id 从存储再读一份（= servers.get includeSecrets=true 的宿主实现），
	//    因为会话节点是打开标签那一刻的快照，可能早于用户后来改的密码。
	saved := node
	if node.ID != "" {
		if full, ferr := c.findServer(node.ID); ferr == nil {
			saved = full
		}
	}
	if strings.TrimSpace(saved.Password) == "" {
		name := saved.Name
		if name == "" {
			name = saved.Host
		}
		return saved, "", fmt.Errorf("服务器「%s」没有保存密码：请先在应用里为该服务器保存密码"+
			"（服务器 → 编辑 → 密码），或在该机器上配置免密 sudo 后改用 terminal.run", name)
	}
	return saved, saved.Password, nil
}

// sudoHostStage 把 `<密码>\n` 写进 debugsrv 指定的远端临时文件（用会话已有的 SFTP 客户端）。
//
// 返回值只有 {ok, path, bytes}：不包含任何形式的密码（含长度提示以外的信息）。
func (c *debugController) sudoHostStage(ctx context.Context, args map[string]any) (any, error) {
	sess := ctlSftpSessionID(args)
	target := ctlArgStr(args, "path")
	if target == "" {
		return nil, fmt.Errorf("%w: 缺少 path（sudo 临时文件路径）", debugsrv.ErrBadInput)
	}
	node, password, err := c.ctlSudoCredential(ctx, sess)
	if err != nil {
		return nil, err
	}
	// 白名单复核（与其它 SFTP 写操作共用同一套规则函数）。
	//
	// 这里**不要求 fs.remote.write 能力位**：terminal.sudo 的能力位是 sudo.credential + terminal.input
	//（见 sudo.go 的登记）。白名单在这里的作用是「密码只允许写到白名单前缀内」这一条独立硬边界，
	// 而不是复用远端写权限 —— 否则用户为了用 sudo 就被迫打开通用的远端写能力。
	norm, nerr := debugsrv.NormalizeRemotePath(target)
	if nerr != nil {
		return nil, fmt.Errorf("%w: %v", debugsrv.ErrBadInput, nerr)
	}
	allow := c.app.sftpWriteAllowlist()
	if !debugsrv.SFTPPathAllowed(allow, norm) {
		return nil, fmt.Errorf("sudo 临时文件路径 %q 不在远端写入白名单内（当前白名单：%s）："+
			"请在 设置 → 调试模式 → MCP 能力 里把 /tmp 加进白名单前缀", norm, strings.Join(allow, "、"))
	}
	client, _, err := c.ctlSftpClient(map[string]any{"sessionId": sess})
	if err != nil {
		return nil, err
	}
	payload := []byte(password + "\n")
	f, oerr := client.OpenFile(norm, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if oerr != nil {
		return nil, fmt.Errorf("创建 sudo 临时文件 %q 失败：%s", norm, ctlSftpErrText(oerr))
	}
	n, werr := f.Write(payload)
	cerr := f.Close()
	if werr != nil {
		// 写了一半也可能留下残片：尽力删掉再报错（debugsrv 侧还会再兜底清理一次）。
		_ = client.Remove(norm)
		return nil, fmt.Errorf("写入 sudo 临时文件 %q 失败：%w", norm, werr)
	}
	if cerr != nil {
		_ = client.Remove(norm)
		return nil, fmt.Errorf("关闭 sudo 临时文件 %q 失败：%w", norm, cerr)
	}
	c.app.manager.InvalidateSftpCache(path.Dir(norm))
	// 日志只记会话 / 路径 / 字节数（长度有助于排查「写空了」），绝不记内容。
	logx.Infof("MCP sudo 临时凭证已写入: session=%s path=%s bytes=%d server=%s", sess, norm, n, node.ID)
	return map[string]any{"ok": true, "path": norm, "bytes": n}, nil
}

// sudoHostScrub 把输出里出现的密码替换成 ***（唯一的明文持有者在这里）。
//
// 返回 {"text": 净化后的文本, "scrubbed": bool, "replaced": bool}：
//   - scrubbed=true 表示「已确认返回的文本里不含该密码」（需要时已替换成 ***，包括本来就没出现过）；
//   - scrubbed=false 表示**无法确认**（会话失效 / 没有保存密码 / 替换后仍能匹配到），
//     此时调用方会在返回里明确警告「本段输出未经密码打码」，而不是假装已经安全。
//
// text 参数刻意不过 ctlArgStr（它会 TrimSpace，会改变返回给 AI 的输出）。
func (c *debugController) sudoHostScrub(ctx context.Context, args map[string]any) (any, error) {
	raw, hasText := args["text"]
	text, _ := raw.(string)
	if !hasText {
		return nil, fmt.Errorf("%w: 缺少 text（要净化的输出文本）", debugsrv.ErrBadInput)
	}
	sess := ctlSftpSessionID(args)
	_, password, err := c.ctlSudoCredential(ctx, sess)
	if err != nil || password == "" {
		return map[string]any{"text": text, "scrubbed": false, "replaced": false}, nil
	}
	out := text
	replaced := false
	if strings.Contains(text, password) {
		out = strings.ReplaceAll(text, password, "***")
		replaced = true
	}
	if strings.Contains(out, password) {
		// 理论上不会发生（ReplaceAll 已替换全部出现）；真发生了也如实报告未净化。
		return map[string]any{"text": out, "scrubbed": false, "replaced": replaced}, nil
	}
	return map[string]any{"text": out, "scrubbed": true, "replaced": replaced}, nil
}
