package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
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
	"ding-ssh/internal/store"

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
		Addr:    fmt.Sprintf("%s:%d", host, c.boot.Port),
		Token:   c.boot.Token,
		Handler: c,
		Hub:     c.hub,
		Logf:    logx.Infof,
		APILogf: apiLogf,
	})
	addr, err := srv.Start()
	if err != nil {
		// 端口被占用：回退到系统分配的空闲端口，保证调试模式仍可用
		logx.Errorf("调试服务启动失败（端口 %d）: %v，改用随机端口重试", c.boot.Port, err)
		srv = debugsrv.New(debugsrv.Options{
			Addr:    fmt.Sprintf("%s:0", host),
			Token:   c.boot.Token,
			Handler: c,
			Hub:     c.hub,
			Logf:    logx.Infof,
			APILogf: apiLogf,
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
	var in debugArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("%w: 入参不是合法 JSON", debugsrv.ErrBadInput)
		}
	}

	switch op {
	case debugsrv.OpHealth:
		return c.info(), nil
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
		if err := json.Unmarshal(args, &cur); err != nil {
			return nil, fmt.Errorf("%w: %v", debugsrv.ErrBadInput, err)
		}
		if err := c.app.SaveSettings(cur); err != nil {
			return nil, err
		}
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
		if err := c.app.SetLogOptions(opts); err != nil {
			return nil, err
		}
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
	c := &debugController{app: a, boot: a.debugBoot, bridge: bridge, hub: debugsrv.NewHub()}
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

// reloadDebug 在设置保存后调用：开关/端口变化立即生效（CDP 端口需重启）。
func (a *App) reloadDebug(next models.DebugSettings) {
	next = models.NormalizeDebugSettings(next)
	if a.debug == nil && !next.Enabled {
		return
	}
	changed := a.debugBoot.Enabled != next.Enabled ||
		a.debugBoot.Port != next.Port ||
		a.debugBoot.BindLAN != next.BindLAN ||
		a.debugBoot.AllowEval != next.AllowEval ||
		a.debugBoot.AllowSecrets != next.AllowSecrets ||
		a.debugBoot.CDPEnabled != next.CDPEnabled
	if !changed {
		return
	}
	a.stopDebug()
	boot := DebugBoot{
		Enabled:      next.Enabled,
		Port:         next.Port,
		BindLAN:      next.BindLAN,
		AllowEval:    next.AllowEval,
		AllowSecrets: next.AllowSecrets,
		CDPEnabled:   next.CDPEnabled,
		CDPPort:      a.debugBoot.CDPPort,
		Token:        a.debugBoot.Token,
	}
	if boot.Enabled && boot.Token == "" {
		boot.Token = newDebugToken()
	}
	a.debugBoot = boot
	a.startDebug()
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
