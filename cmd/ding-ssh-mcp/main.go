// Command ding-ssh-mcp 是 ding-ssh 调试模式 MCP 服务的 **stdio 桥**。
//
// 背景：应用内的 MCP 服务走 Streamable HTTP（POST /mcp 收发消息 + GET /mcp 的 SSE 推送通道），
// 但很多 MCP 客户端只支持 stdio 传输。本程序把两者接起来：
//
//	stdin（每行一个 JSON-RPC）  ──▶  POST <url>/mcp  ──▶  应用内调试服务
//	stdout（每行一个 JSON-RPC） ◀──  响应体 / GET <url>/mcp 的 SSE data 行
//
// 协议要点：
//   - stdin 逐行读取（MCP stdio 规范：一行一个 JSON-RPC 消息，换行分隔）；
//   - 含 id 的请求转发后把响应体原样写回 stdout；通知（无 id）只转发、不输出；
//   - initialize 响应头里的 Mcp-Session-Id 会被记住，后续请求都带上；
//   - 拿到会话后启动一个 goroutine 用 GET <url>/mcp 挂 SSE，把 data 行逐行写 stdout，
//     断开后按 1s/2s/5s 退避重连，直到进程退出。
//
// 约定：**stdout 只允许出现 JSON-RPC**，任何日志（含 token）都走 stderr，
// 且日志里的 token 一律打码（见 maskToken）。
//
// 只用标准库：不引入任何依赖，也不改动 go.mod。
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// serverName 打在每条 stderr 日志前的标识，便于区分多个 MCP 桥的日志。
	serverName = "ding-ssh-mcp"

	// envURL / envToken 环境变量名：优先级低于命令行参数、高于 debug.json。
	envURL   = "DING_SSH_DEBUG_URL"
	envToken = "DING_SSH_DEBUG_TOKEN"

	// sessionHeader MCP 会话头（2025-03-26 规范，与 internal/debugsrv 保持一致）。
	sessionHeader = "Mcp-Session-Id"

	// maxLineBytes stdin / SSE 单行上限。MCP 消息可能带较大的 arguments
	// （整段文本、base64 图片），1MB 足够覆盖正常工具调用，超过则报错而不是静默截断。
	maxLineBytes = 1 << 20

	// maxBodyBytes 响应体读取上限，防止服务端异常时把内存吃满。
	maxBodyBytes = 64 << 20

	// requestTimeout 单次 POST /mcp 的超时。截图 / eval 等工具可能偏慢，
	// 因此给较宽松的上限；到点即报错，避免 stdio 侧无限等待。
	requestTimeout = 120 * time.Second

	// verifyTimeout 启动时连通性探测的单次超时。
	verifyTimeout = 10 * time.Second
)

// verifyRetryDelays 启动探测失败后的重试间隔。
// 应用可能刚在别处启动（debug.json 已写出但端口尚未开始监听），
// 直接报错退出对用户不友好；按固定间隔重试几次再放弃。
var verifyRetryDelays = []time.Duration{300 * time.Millisecond, 700 * time.Millisecond, 1500 * time.Millisecond}

// errNotForwarded 表示「请求确定没有被服务端受理」（连不上、token 被拒），
// 只有这类失败才允许重试，避免副作用（如 send_input）被重复执行。
var errNotForwarded = errors.New("请求未送达")

// sseBackoff 是 SSE 断线后的重连退避序列，用完最后一个值后一直沿用它。
var sseBackoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second}

// shutdownOnce 保证清理逻辑（关 SSE、DELETE 会话）只执行一次：
// 正常退出与信号处理两条路径都可能触发。
var shutdownOnce sync.Once

// config 解析出来的目标地址与令牌。
type config struct {
	url       string // 形如 http://127.0.0.1:8765（不含 /mcp）
	token     string
	urlSource string // -v 时说明地址是从哪来的
	tokSource string
}

// debugInfo 是 %AppData%\ding-ssh\debug.json 的子集（应用写入的调试信息）。
type debugInfo struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// rpcEnvelope 只取分发需要的字段：id 是否出现决定「请求」还是「通知」。
type rpcEnvelope struct {
	ID json.RawMessage `json:"id"`
}

// options 解析后的运行配置。
type options struct {
	url      string
	token    string
	debugRaw string // -debug-json，空表示用默认路径
	verbose  bool
	quiet    bool
	once     bool
	help     bool
}

// logf2 是极小的 stderr 日志器（名字里的 2 只是与 logx 体系区分，这里只用标准库）。
// 所有日志统一经过它，保证「stdout 只有 JSON-RPC」与「token 打码」两条约定不被破坏。
type logf2 struct {
	verbose bool
	mu      sync.Mutex
}

func (l *logf2) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(os.Stderr, "[%s] %s\n", serverName, fmt.Sprintf(format, args...))
}

// infof 普通日志（始终输出到 stderr）。
func (l *logf2) infof(format string, args ...any) { l.printf(format, args...) }

// debugf 细节日志（仅 -v 时输出）。
func (l *logf2) debugf(format string, args ...any) {
	if !l.verbose {
		return
	}
	l.printf(format, args...)
}

// clientMCP 把 stdio 与 HTTP/SSE 两侧串起来。
type clientMCP struct {
	verbose bool
	http    *http.Client
	out     *bufio.Writer
	log     *logf2
	opts    options // 请求失败时用于重读 debug.json（命令行显式值优先）

	// cfg 目标地址与令牌，可被 refreshFromDisk 更新（应用重启后会自动跟上）。
	cfgMu sync.Mutex
	cfg   config

	// 会话 id：initialize 响应头里拿到后由后续请求共用，SSE goroutine 也读它。
	sessMu sync.Mutex
	sessID string

	streamStart sync.Once // SSE 订阅只启动一次
	streamCtx   context.Context
	streamStop  context.CancelFunc
	streamConn  atomic.Pointer[http.Response] // 当前 SSE 连接，退出时用于强制关闭
}

// main 只负责把 run 的退出码交给操作系统 —— 退出前必须关闭 SSE 并尽力 DELETE 会话，
// 这些都在 run 内部完成。
func main() { os.Exit(run()) }

// run 解析参数与目标地址，进入 stdio 循环；返回进程退出码。
func run() int {
	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) || opts.help {
		printUsage(os.Stdout)
		return 0
	}
	if err != nil {
		// flag 包已把错误与用法打到 stderr。
		return 2
	}

	log := &logf2{verbose: opts.verbose && !opts.quiet}

	cfg, err := resolveConfig(opts, os.Getenv, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] 错误：%v\n", serverName, err)
		fmt.Fprintf(os.Stderr, "[%s] 提示：请先在应用内打开「设置 → 调试模式 → 启用」，或显式传入 -url/-token。\n", serverName)
		return 1
	}
	log.infof("目标调试服务：%s", cfg.url)
	log.infof("鉴权 token：%s（来源：%s）", maskToken(cfg.token), cfg.tokSource)
	log.debugf("地址来源：%s", cfg.urlSource)

	c := newClientMCP(cfg, opts, log)

	// 启动探测：避免在地址过期 / token 不匹配时进入 stdio 循环，
	// 让 MCP 客户端只看到「子进程以非 0 退出」这一个明确信号。
	if err := c.verify(opts.once); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] 错误：%v\n", serverName, err)
		return 1
	}
	if opts.once {
		log.infof("探测成功：地址与 token 均可用（-once 模式，直接退出）")
		return 0
	}

	// 被 Ctrl+C / 终止时也要走清理路径（关闭 SSE、尽力 DELETE 会话）。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sigCh
		log.infof("收到信号 %v，正在退出…", s)
		c.shutdown()
		os.Exit(0)
	}()

	code := c.serveStdin(os.Stdin)
	c.shutdown()
	log.infof("stdin 已关闭，退出（退出码 %d）", code)
	return code
}

// parseFlags 解析命令行参数。help 为 true 或返回 flag.ErrHelp 时打印用法。
func parseFlags(argv []string, out io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet(serverName, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { printUsage(out) }
	fs.StringVar(&opts.url, "url", os.Getenv(envURL), "调试服务地址，如 http://127.0.0.1:8765（默认取环境变量 "+envURL+" 或 debug.json）")
	fs.StringVar(&opts.token, "token", os.Getenv(envToken), "调试服务 token（默认取环境变量 "+envToken+" 或 debug.json）")
	fs.StringVar(&opts.debugRaw, "debug-json", "", "debug.json 路径（默认 %AppData%\\ding-ssh\\debug.json）")
	fs.BoolVar(&opts.verbose, "v", false, "打印转发细节日志到 stderr（token 一律打码）")
	fs.BoolVar(&opts.quiet, "quiet", false, "只输出错误日志到 stderr")
	fs.BoolVar(&opts.once, "once", false, "仅探测地址与 token 是否可用，然后退出（不进入 stdio 桥接）")
	fs.BoolVar(&opts.help, "h", false, "显示本帮助")

	if err := fs.Parse(argv); err != nil {
		return opts, err
	}
	if opts.help {
		return opts, flag.ErrHelp
	}
	// 位置参数没有意义：直接报错，避免「参数写错却被静默忽略」。
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("不支持的位置参数: %s", strings.Join(fs.Args(), " "))
	}
	opts.url = strings.TrimSpace(opts.url)
	opts.token = strings.TrimSpace(opts.token)
	opts.debugRaw = strings.TrimSpace(opts.debugRaw)
	return opts, nil
}

// printUsage 打印中文用法说明。
func printUsage(w io.Writer) {
	fmt.Fprint(w, `ding-ssh-mcp —— 把 ding-ssh 的 MCP 服务（Streamable HTTP）桥接给只支持 stdio 的 MCP 客户端

用法：
  ding-ssh-mcp [选项]

选项：
  -url string         调试服务地址，如 http://127.0.0.1:8765（也接受以 /mcp 结尾，会自动去掉）
  -token string       调试服务 token（每次启动应用都会变化）
  -debug-json string  debug.json 路径，默认 %AppData%\ding-ssh\debug.json
  -v                  打印转发细节日志到 stderr（token 一律打码）
  -quiet              只输出错误日志
  -once               仅探测地址与 token 是否可用，然后退出
  -h                  显示本帮助

地址与令牌的解析优先级（从高到低）：
  1. 命令行参数  -url / -token
  2. 环境变量    DING_SSH_DEBUG_URL / DING_SSH_DEBUG_TOKEN
  3. 调试信息文件 %AppData%\ding-ssh\debug.json（应用在 设置 → 调试模式 开启后写入）
  三者都拿不到时，向 stderr 打印中文错误并以退出码 1 结束。

传输与 IO 约定：
  stdin   每行一个 JSON-RPC 消息（MCP stdio 规范）；含 id 的请求会被转发到 POST <url>/mcp，
          无 id 的通知只转发、不产生输出。
  stdout  只输出 JSON-RPC：请求的响应体原样逐行写回，以及 GET <url>/mcp 的 SSE 推送
          （notifications/message）逐行写回。任何日志都不会混进 stdout。
  stderr  日志（启动信息、-v 细节、错误）。

示例：
  ding-ssh-mcp -v
  ding-ssh-mcp -url http://127.0.0.1:8765 -token <token>
  $env:DING_SSH_DEBUG_URL="http://127.0.0.1:8765"; ding-ssh-mcp

MCP 客户端配置（stdio 传输）：
  {
    "mcpServers": {
      "ding-ssh": { "command": "ding-ssh-mcp.exe", "args": ["-v"] }
    }
  }
  开发期也可用 "command": "go", "args": ["run", "./cmd/ding-ssh-mcp"]（在仓库根目录运行）。
`)
}

// resolveConfig 按 命令行参数 → 环境变量 → debug.json 的优先级解析目标地址与 token。
// bootstrap 为 true（启动时的自动重读）时跳过命令行参数，只重新读环境变量与 debug.json，
// 避免覆盖用户显式指定的地址；为 false 时按完整优先级解析。
func resolveConfig(opts options, getenv func(string) string, bootstrap bool) (config, error) {
	var cfg config

	// 1) 命令行参数。flag 的默认值就是环境变量，因此这里无法区分两者，
	//    统一标注为 -url/-token；真正来自 debug.json 的情况会在下面另行标注。
	if !bootstrap {
		if opts.url != "" {
			cfg.url, cfg.urlSource = opts.url, "命令行参数 -url 或环境变量 "+envURL
		}
		if opts.token != "" {
			cfg.token, cfg.tokSource = opts.token, "命令行参数 -token 或环境变量 "+envToken
		}
	}

	// 2) 环境变量。
	if cfg.url == "" {
		if v := strings.TrimSpace(getenv(envURL)); v != "" {
			cfg.url, cfg.urlSource = v, "环境变量 "+envURL
		}
	}
	if cfg.token == "" {
		if v := strings.TrimSpace(getenv(envToken)); v != "" {
			cfg.token, cfg.tokSource = v, "环境变量 "+envToken
		}
	}

	// 3) debug.json。
	path := opts.debugRaw
	if path == "" {
		path = defaultDebugInfoPath()
	}
	if cfg.url == "" || cfg.token == "" {
		info, err := readDebugInfo(path)
		switch {
		case err == nil:
			if cfg.url == "" && strings.TrimSpace(info.URL) != "" {
				cfg.url, cfg.urlSource = strings.TrimSpace(info.URL), "debug.json"
			}
			if cfg.token == "" && strings.TrimSpace(info.Token) != "" {
				cfg.token, cfg.tokSource = strings.TrimSpace(info.Token), "debug.json"
			}
		case errors.Is(err, os.ErrNotExist):
			// 文件不存在：交给下面的缺项检查给出统一提示。
		default:
			return cfg, fmt.Errorf("读取调试信息文件失败（%s）：%v", path, err)
		}
	}

	var missing []string
	if cfg.url == "" {
		missing = append(missing, "调试服务地址（-url / "+envURL+" / debug.json 中的 url）")
	}
	if cfg.token == "" {
		missing = append(missing, "调试 token（-token / "+envToken+" / debug.json 中的 token）")
	}
	if len(missing) > 0 {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("未找到调试信息文件 %s，且无法确定 %s", path, strings.Join(missing, "、"))
		}
		return cfg, fmt.Errorf("无法确定 %s", strings.Join(missing, "、"))
	}

	cfg.url = normalizeBaseURL(cfg.url)
	if err := validateBaseURL(cfg.url); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// defaultDebugInfoPath 返回 %AppData%\ding-ssh\debug.json
// （与 debug.go 的 DebugInfoPath 保持一致的布局）。
func defaultDebugInfoPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "ding-ssh", "debug.json")
	}
	return filepath.Join(dir, "ding-ssh", "debug.json")
}

// readDebugInfo 解析调试信息文件。
func readDebugInfo(path string) (debugInfo, error) {
	var info debugInfo
	b, err := os.ReadFile(path)
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return info, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return info, nil
}

// normalizeBaseURL 容错处理：允许用户直接粘贴 .../mcp 或带结尾 / 的地址。
func normalizeBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/mcp")
	return strings.TrimRight(u, "/")
}

// validateBaseURL 校验地址可用：必须是 http/https 且带主机名。
func validateBaseURL(raw string) error {
	if raw == "" {
		return errors.New("调试服务地址为空")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("调试服务地址必须以 http:// 或 https:// 开头：%s", raw)
	}
	// 借 http.NewRequest 让标准库给出准确的解析错误。
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return fmt.Errorf("调试服务地址无效：%v", err)
	}
	if req.URL.Host == "" {
		return fmt.Errorf("调试服务地址缺少主机名：%s", raw)
	}
	return nil
}

// newClientMCP 构造桥接器。
func newClientMCP(cfg config, opts options, log *logf2) *clientMCP {
	ctx, cancel := context.WithCancel(context.Background())
	return &clientMCP{
		cfg:     cfg,
		opts:    opts,
		verbose: opts.verbose && !opts.quiet,
		// 只设连接类超时，不设 Client.Timeout：SSE 是长连接，整体超时会把推送通道掐断。
		http: &http.Client{
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout: 10 * time.Second,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		out:        bufio.NewWriter(os.Stdout),
		log:        log,
		streamCtx:  ctx,
		streamStop: cancel,
	}
}

// target 读取当前目标地址与令牌（可能被 refreshFromDisk 更新过）。
func (c *clientMCP) target() config {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	return c.cfg
}

// verify 在进入 stdio 循环前探测地址与 token 是否可用。
// 做法是发一次真的 MCP ping：token 不对会得到 401，应用没开则连接被拒。
// once 为 true（-once 模式）时不重试，让调用方自己决定下一步。
func (c *clientMCP) verify(once bool) error {
	body := []byte(`{"jsonrpc":"2.0","id":"ding-ssh-mcp-verify","method":"ping"}`)
	attempts := 1
	if !once {
		attempts = len(verifyRetryDelays) + 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(verifyRetryDelays[i-1])
		}
		ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
		err := c.post(ctx, body)
		cancel()
		if err == nil {
			c.log.debugf("探测成功：POST %s/mcp", c.cfg.url)
			return nil
		}
		lastErr = err
		c.log.debugf("第 %d 次探测失败：%v", i+1, err)
		// 401 之类的鉴权错误重试没有意义，直接返回。
		if !isRetryable(err) {
			break
		}
	}
	return fmt.Errorf("无法连接调试服务 %s：%v（请确认应用正在运行且「设置 → 调试模式」已启用；token 每次启动都会变化，必要时重新读取 debug.json）", c.cfg.url, lastErr)
}

// isRetryable 判断探测失败是否值得重试：
// 连接被拒（应用刚启动 / 端口尚未监听）、连接被重置或 EOF（服务端立刻关闭）都可以再试。
func isRetryable(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "forcibly closed")
}

// refreshFromDisk 在请求失败时重读环境变量与 debug.json。
//
// 用途：应用重启后端口与 token 都会变，而已配置好的 MCP 客户端不会重启子进程。
// 只要地址/令牌不是命令行显式指定的，就尝试自动跟上新值，一次成功后续就能继续用。
// 返回 true 表示配置发生了变化。
func (c *clientMCP) refreshFromDisk() bool {
	next, err := resolveConfig(c.opts, os.Getenv, true)
	if err != nil {
		c.log.debugf("重读调试信息失败（忽略）：%v", err)
		return false
	}
	c.cfgMu.Lock()
	changed := next.url != c.cfg.url || next.token != c.cfg.token
	if changed {
		c.cfg = next
	}
	c.cfgMu.Unlock()
	if changed {
		c.log.infof("调试服务信息已更新（来源：%s / %s）：%s", next.urlSource, next.tokSource, next.url)
	}
	return changed
}

// serveStdin 逐行读取 stdin 并转发；返回进程退出码。
// stdin 关闭（MCP 客户端退出）视为正常结束，返回 0。
func (c *clientMCP) serveStdin(in io.Reader) int {
	sc := bufio.NewScanner(in)
	// 默认 64KB 对 MCP 消息太小（稍大的参数就会被截断报错），放大到 1MB。
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		// 复制一份：Scanner 的缓冲区会被下一次 Scan 复用。
		body := make([]byte, len(line))
		copy(body, line)
		c.handleLine(body)
	}
	if err := sc.Err(); err != nil {
		c.log.infof("读取 stdin 失败：%v", err)
		return 1
	}
	return 0
}

// handleLine 处理一行 JSON-RPC：请求（有 id）等响应、通知（无 id）不等响应。
func (c *clientMCP) handleLine(body []byte) {
	var env rpcEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		// 不是合法 JSON 就必须自己回一个 JSON-RPC 错误，
		// 否则客户端会一直等这条消息的响应。
		c.log.infof("stdin 收到非法 JSON，已回错误响应：%v", err)
		c.writeLine(parseErrorResponse())
		return
	}

	method := peekMethod(body)
	if !hasID(env.ID) {
		c.log.debugf("转发通知：%s", method)
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		err := c.post(ctx, body) // 通知不等待响应体
		cancel()
		if err != nil {
			// 通知出错无处回报，只能记日志；顺带试着跟上应用重启后的新端口/token。
			c.log.infof("转发通知失败（%s）：%v", method, err)
			c.refreshFromDisk()
		}
		return
	}

	c.log.debugf("转发请求：%s（id=%s）", method, string(env.ID))
	respBody, err := c.doRequest(body)
	if err != nil {
		c.log.infof("请求失败（%s）：%v", method, err)
		// 仅在「请求肯定没被服务端执行」时重读 debug.json 并重试一次：
		// 应用重启后端口/token 会变，而已配置好的 MCP 客户端不会重启本子进程。
		// 其它失败（HTTP 5xx、读响应体失败等）不重试，避免工具调用被重复执行。
		if errors.Is(err, errNotForwarded) && c.refreshFromDisk() {
			if retryBody, retryErr := c.doRequest(body); retryErr == nil {
				c.writeLine(retryBody)
				c.log.debugf("重试成功：%s", method)
				return
			} else {
				err = retryErr
				c.log.infof("重试失败（%s）：%v", method, retryErr)
			}
		}
		c.writeLine(errorResponse(env.ID, err))
		return
	}
	c.writeLine(respBody)
	c.log.debugf("已回写响应：%s（%d 字节）", method, len(respBody))
}

// doRequest 转发一个请求并返回响应体（原样字节，服务端保证是单行 JSON）。
func (c *clientMCP) doRequest(body []byte) ([]byte, error) {
	cfg := c.target()
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.url+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	if id := c.sessionID(); id != "" {
		req.Header.Set(sessionHeader, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// 连不上：请求肯定没被执行，允许上层重试。
		return nil, fmt.Errorf("%w：请求 %s/mcp 失败：%v", errNotForwarded, cfg.url, err)
	}
	defer resp.Body.Close()

	// initialize：记住会话 id，并启动 SSE 推送通道。
	if sid := strings.TrimSpace(resp.Header.Get(sessionHeader)); sid != "" {
		c.setSessionID(sid)
		c.log.infof("已建立 MCP 会话：%s", maskToken(sid))
		c.startStream()
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		// 响应头已到达但读体失败：服务端可能已经开始执行，不能重试，避免副作用重复。
		return nil, fmt.Errorf("读取响应体失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			// 鉴权失败说明调用没被受理，可以放心重试。
			return nil, fmt.Errorf("%w：鉴权失败（HTTP %d），token 不正确或已过期", errNotForwarded, resp.StatusCode)
		}
		return nil, fmt.Errorf("服务端返回 HTTP %d：%s", resp.StatusCode, truncate(strings.TrimSpace(string(data)), 300))
	}
	line := normalizeJSONLine(data)
	if len(line) == 0 {
		return nil, errors.New("服务端返回空响应体")
	}
	return line, nil
}

// post 发一个请求并丢弃响应体（用于通知与启动探测）。
// 只校验状态码，不解析内容。
func (c *clientMCP) post(ctx context.Context, body []byte) error {
	cfg := c.target()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.url+"/mcp", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	if id := c.sessionID(); id != "" {
		req.Header.Set(sessionHeader, id)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w：鉴权失败（HTTP %d），token 不正确或已过期", errNotForwarded, resp.StatusCode)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// 通知正常返回 202，探测（ping）返回 200，都算成功。
		return nil
	default:
		return fmt.Errorf("服务端返回 HTTP %d", resp.StatusCode)
	}
}

// startStream 启动 SSE 订阅 goroutine（只启动一次）。
func (c *clientMCP) startStream() {
	c.streamStart.Do(func() {
		go c.streamLoop()
	})
}

// streamLoop 订阅 GET <url>/mcp（SSE）：把 data 行的 JSON 逐行写 stdout。
// 断开后按 1s/2s/5s 退避重连，直到进程退出。
func (c *clientMCP) streamLoop() {
	for attempt := 0; ; attempt++ {
		if c.streamCtx.Err() != nil {
			return
		}
		err := c.streamOnce()
		if c.streamCtx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("服务端关闭了推送通道")
		}
		delay := sseBackoff[imin(attempt, len(sseBackoff)-1)]
		c.log.debugf("SSE 断开（%v），%s 后重连", err, delay)
		if !sleepCtx(c.streamCtx, delay) {
			return
		}
	}
}

// streamOnce 建立一次 SSE 连接并读到断开为止。
func (c *clientMCP) streamOnce() error {
	id := c.sessionID()
	if id == "" {
		return errors.New("没有会话 id，暂不订阅推送")
	}
	cfg := c.target()
	req, err := http.NewRequestWithContext(c.streamCtx, http.MethodGet, cfg.url+"/mcp", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	req.Header.Set(sessionHeader, id)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	c.streamConn.Store(resp)
	defer func() {
		c.streamConn.Store(nil)
		resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		msg := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
			// 会话被服务端丢弃（例如应用重启），或 token 已变化：
			// 清掉会话 id 并重读调试信息，让客户端重新 initialize，
			// 而不是无限重连一个死会话。
			c.log.infof("推送通道返回 %d（会话已失效），等待客户端重新 initialize：%s", resp.StatusCode, truncate(msg, 200))
			c.setSessionID("")
			c.refreshFromDisk()
			return errors.New("MCP 会话已失效")
		}
		return fmt.Errorf("推送通道 HTTP %d：%s", resp.StatusCode, truncate(msg, 200))
	}

	c.log.infof("已挂上推送通道（GET %s/mcp）", cfg.url)
	return c.readSSE(resp.Body)
}

// readSSE 解析 SSE 流：把 data 字段拼起来逐行写 stdout。
// 事件名（event:）不影响协议，MCP 通知都放在 data 里。
func (c *clientMCP) readSSE(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		payload := strings.TrimSpace(strings.Join(data, "\n"))
		data = data[:0]
		if payload == "" {
			return
		}
		// 只把合法 JSON 写进 stdout：stdout 是纯 JSON-RPC 通道，
		// 任何非 JSON 内容都会让客户端解析失败。
		if !json.Valid([]byte(payload)) {
			c.log.infof("推送内容不是合法 JSON，已丢弃：%s", truncate(payload, 200))
			return
		}
		c.writeLine([]byte(payload))
		c.log.debugf("推送已转发：%s", truncate(payload, 200))
	}

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			flush() // 空行表示一个事件结束
		case strings.HasPrefix(line, ":"):
			// 注释行（服务端心跳 ": ping"），忽略。
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			// event: / id: / retry: 等字段对 stdio 桥没有意义。
		}
	}
	flush()
	return sc.Err()
}

// writeLine 向 stdout 写一行并立即冲刷（MCP 客户端按行读取，缓冲会卡住交互）。
func (c *clientMCP) writeLine(b []byte) {
	if len(bytes.TrimSpace(b)) == 0 {
		return
	}
	if _, err := c.out.Write(b); err != nil {
		c.log.infof("写 stdout 失败：%v", err)
		return
	}
	if err := c.out.WriteByte('\n'); err != nil {
		c.log.infof("写 stdout 失败：%v", err)
		return
	}
	if err := c.out.Flush(); err != nil {
		c.log.infof("冲刷 stdout 失败：%v", err)
	}
}

// sessionID 读取当前会话 id。
func (c *clientMCP) sessionID() string {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	return c.sessID
}

// setSessionID 更新会话 id。
func (c *clientMCP) setSessionID(id string) {
	c.sessMu.Lock()
	c.sessID = id
	c.sessMu.Unlock()
}

// shutdown 关闭 SSE 连接、尽力 DELETE 会话，并冲刷 stdout。
// 正常退出与信号处理两条路径都可能触发，因此用 sync.Once 保护。
func (c *clientMCP) shutdown() {
	shutdownOnce.Do(func() {
		c.streamStop()
		if resp := c.streamConn.Load(); resp != nil {
			// 关闭底层响应体，让阻塞中的 SSE 读取立刻返回。
			_ = resp.Body.Close()
		}
		if id := c.sessionID(); id != "" {
			c.deleteSession(id)
		} else {
			c.log.debugf("没有会话 id，跳过 DELETE /mcp")
		}
		_ = c.out.Flush()
	})
}

// deleteSession 尽力结束会话：失败只记日志，不影响退出码。
func (c *clientMCP) deleteSession(id string) {
	cfg := c.target()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, cfg.url+"/mcp", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	req.Header.Set(sessionHeader, id)
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.debugf("DELETE /mcp 失败（可忽略）：%v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	c.log.debugf("已结束会话（DELETE /mcp → HTTP %d）", resp.StatusCode)
}

// hasID 判断 JSON-RPC 消息是否带 id（决定「请求」还是「通知」）。
// 显式 null 按通知处理，与 internal/debugsrv 的判断保持一致。
func hasID(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

// peekMethod 取 method 字段，仅用于日志；解析失败时返回 "?"。
func peekMethod(body []byte) string {
	var m struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &m); err != nil || m.Method == "" {
		return "?"
	}
	return m.Method
}

// normalizeJSONLine 把响应体压成一行：先去掉 CR，再把 JSON 字符串之外的换行
// 换成空格。debugsrv 的 writeJSON 用 Encoder 输出，正常只有结尾一个换行，
// 这里只是兜底，保证 stdout 的「一行一个消息」不被破坏。
func normalizeJSONLine(data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte{'\r'}, nil)
	trimmed := bytes.TrimSpace(data)
	if !bytes.ContainsAny(trimmed, "\n") {
		return trimmed
	}
	// 有换行的多半是缩进 JSON：JSON 字符串里不会出现裸换行（会被转义成 \n），
	// 因此直接把换行当分隔符替换为空格是安全的。
	fields := bytes.Fields(trimmed)
	return bytes.Join(fields, []byte(" "))
}

// maskToken 给 token / 会话 id 打码：只保留前 6 位，其余用 * 代替并标注长度。
// 短 token 至少保留 2 位，避免出现「保留的前缀就是全部内容」。
func maskToken(tok string) string {
	if tok == "" {
		return "(无)"
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

// writeJSONMessage 组装一条 JSON-RPC 消息（响应或错误）。
func writeJSONMessage(id json.RawMessage, result any, errObj map[string]any) []byte {
	msg := map[string]any{"jsonrpc": "2.0"}
	if hasID(id) {
		msg["id"] = id
	} else {
		msg["id"] = nil
	}
	if errObj != nil {
		msg["error"] = errObj
	} else {
		msg["result"] = result
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"内部错误"}}`)
	}
	return b
}

// errorResponse 把桥接层错误翻译成 JSON-RPC 错误响应，让客户端不至于一直等待。
func errorResponse(id json.RawMessage, err error) []byte {
	return writeJSONMessage(id, nil, map[string]any{
		"code":    -32001,
		"message": "ding-ssh-mcp 转发失败",
		"data":    err.Error(),
	})
}

// parseErrorResponse 对非法 JSON 的 stdin 行返回 -32700。
func parseErrorResponse() []byte {
	return writeJSONMessage(nil, nil, map[string]any{
		"code":    -32700,
		"message": "JSON 解析失败",
	})
}

// sleepCtx 可被取消的 sleep；返回 false 表示上下文已结束。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// truncate 截断过长的日志内容（按字节，日志可读性优先）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// imin 取较小值（避免与 Go 1.21+ 的内置 min 在阅读上混淆）。
func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
