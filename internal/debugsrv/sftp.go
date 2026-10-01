package debugsrv

// M8：SFTP 文件操作与终端自动化 —— 让 AI 读写远端文件、跑命令并等结果。
//
// # 分层（照既有模式，不另起炉灶）
//
//   - **本文件（debugsrv）**：工具条目与入参校验、路径白名单、体积限额与截断标记、
//     终端输出增量捕获（复用 Hub 的 ssh.output 事件）、传输快照、资源组装；
//   - **宿主（debug.go）**：sftp.* op 落到「会话已有的那份 SFTP 客户端」
//     （internal/sshx 的 Manager.SftpClient → App 的既有 SftpRename/Mkdir/Remove/SftpCancelTransfer），
//     **绝不新建 SSH 连接、不自己写 SFTP 客户端**；
//   - **前端桥**：只有「页面内才知道」的东西才走桥 —— 终端输入沿用既有 op
//     （OpTerminalInput → frontend/src/debug/bridge.ts 的 terminal.input），终端尾部文本用 OpTerminalBuffer。
//
// # 能力位与确认（登记见 sftp_register.go）
//
//	读类（cap 不传 = read，永远允许，但仍全部走审计）：
//	  sftp.list / sftp.stat / sftp.read / sftp.transfers / terminal.expect
//	写类（fs.remote.write + 两段式确认）：
//	  sftp.write / sftp.mkdir / sftp.rename / sftp.remove / sftp.syncCwd / sftp.cancel
//	终端输入（terminal.input，可逆 / 高频，**不需要 token**）：
//	  terminal.run
//
// # 白名单规则（用户明确要求，见 sftpRequireWritePath）
//
//   - sftpWriteAllowlist 为空 ⇒ 一切写 / 删 / 改名 / 同步全部拒绝（中文原因里说明去哪配置）；
//   - 非空时按「路径段边界」前缀匹配（/srv/app 放行 /srv/app 与 /srv/app/x，
//     **不放行** /srv/app-evil）；
//   - 只接受以 / 开头的绝对路径，且拒绝任何含 .. 段的路径（目录穿越）；
//   - sftp.rename 的 from 与 to **都要**过白名单。
//
// 双重校验：宿主侧对同一份白名单再校验一次（见 debug.go 的 ctlRequireWritePaths）——
// 因为 confirm.commit 走的是 Executor → 宿主 op，会绕过本文件的 preflight。
// 两处共用同一套规则函数（NormalizeRemotePath / SFTPPathAllowed 导出给宿主复用）。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---- op / 工具名（工具名即 op 名，与 ui.go / appctl_register.go 同一约定）----

const (
	// A. SFTP 读
	OpSftpList      = "sftp.list"
	OpSftpStat      = "sftp.stat"
	OpSftpRead      = "sftp.read"
	OpSftpTransfers = "sftp.transfers"
	// B. SFTP 写
	OpSftpWrite   = "sftp.write"
	OpSftpMkdir   = "sftp.mkdir"
	OpSftpRename  = "sftp.rename"
	OpSftpRemove  = "sftp.remove"
	OpSftpSyncCwd = "sftp.syncCwd"
	OpSftpCancel  = "sftp.cancel"
	// C. 终端自动化
	OpTerminalRun    = "terminal.run"
	OpTerminalExpect = "terminal.expect"
)

// TerminalOutputTopic 终端输出事件在 Hub 里的 topic。
//
// 后端事件名是 ssh:output:<sessionId>（SSH 会话与本地终端共用这一条通道，
// 见 internal/sshx/manager.go 与 internal/localterm/manager.go），Hub.PublishRaw 拆成
// topic="ssh.output" + sessionId=<id>；M8 直接订阅它拿「输出增量」，不新增推送机制。
const TerminalOutputTopic = "ssh.output"

// TransfersResourceURI 进行中传输的资源 uri。
const TransfersResourceURI = "transfers://current"

// sftpToolNames 全部 M8 工具（顺序 = tools/list 展示顺序 = 能力位注册顺序）。
var sftpToolNames = []string{
	OpSftpList, OpSftpStat, OpSftpRead, OpSftpTransfers,
	OpSftpWrite, OpSftpMkdir, OpSftpRename, OpSftpRemove, OpSftpSyncCwd, OpSftpCancel,
	OpTerminalRun, OpTerminalExpect,
}

// sftpToolSet 工具名集合（分发用，避免每次线性扫描）。
var sftpToolSet = func() map[string]bool {
	out := make(map[string]bool, len(sftpToolNames))
	for _, n := range sftpToolNames {
		out[n] = true
	}
	return out
}()

// IsSftpOp 判断 op 是否属于 M8 的 SFTP 文件操作（sftp.*）。
//
// 宿主（debug.go）用它把 sftp.* 转给 handleSftpOp；terminal.run / terminal.expect
// 完全在 debugsrv 侧实现（需要 Hub），宿主不需要实现 —— 见 IsTerminalAutomationOp。
func IsSftpOp(op string) bool {
	op = strings.TrimSpace(op)
	return strings.HasPrefix(op, "sftp.") && sftpToolSet[op]
}

// IsTerminalAutomationOp 判断工具是否属于本域的终端自动化（terminal.run / terminal.expect / terminal.sudo）。
//
// terminal.sudo 的实现见 sudo.go（它同样订阅 Hub 的输出增量、同样走既有 OpTerminalInput，
// 额外多了「宿主写密码临时文件 + 输出净化」两步），因此和另外两个工具走同一条分发路径。
func IsTerminalAutomationOp(name string) bool {
	name = strings.TrimSpace(name)
	return name == OpTerminalRun || name == OpTerminalExpect || name == OpTerminalSudo
}

// ---- 限额（所有输出都必须体积有界，见 ui.go / appctl.go 的同类常量）----

const (
	// sftpReadDefaultBytes sftp.read 未给 maxBytes 时的默认读取上限。
	sftpReadDefaultBytes = 256 << 10
	// sftpReadMaxBytes 单次 sftp.read 的硬上限（1MB）。
	sftpReadMaxBytes = 1 << 20
	// sftpWriteMaxBytes 单次 sftp.write 的内容上限（1MB）。
	sftpWriteMaxBytes = 1 << 20
	// sftpMaxListEntries 单次 sftp.list 返回的条目上限。
	sftpMaxListEntries = 500
	// sftpMaxTextBytes MCP 文本内容的兜底上限（超过时截断并标注，正常不会触发）。
	sftpMaxTextBytes = 4 << 20
	// sftpMaxOffset 允许的读取偏移上限（防止 strconv 溢出成负数）。
	sftpMaxOffset = 1 << 40
	// sftpCwdSummaryEntries sftp://{id}/cwd 资源里的条目摘要条数。
	sftpCwdSummaryEntries = 20

	// terminalDefTimeoutMS 终端等待的默认超时。
	terminalDefTimeoutMS = 30000
	// terminalMinTimeoutMS / terminalMaxTimeoutMS 超时的合法范围。
	terminalMinTimeoutMS = 200
	terminalMaxTimeoutMS = 600000
	// terminalDefMaxBytes / terminalMaxMaxBytes 终端输出文本的上限（默认 256KB，最大 1MB）。
	terminalDefMaxBytes = 256 << 10
	terminalMaxMaxBytes = 1 << 20
	// terminalIdleMS 「输出静默」的判定窗口：超过这个时间没有新输出即认为命令跑完了。
	// 这是 terminal.run 未给 expect 时的默认值，可用 idleMs 参数覆盖。
	terminalIdleMS = 400
	// terminalMinIdleMS / terminalMaxIdleMS 自定义静默窗口 idleMs 的合法范围。
	terminalMinIdleMS = 50
	terminalMaxIdleMS = 60000
	// terminalFirstIdleMS 首块**真实输出**之前的宽限（SSH 往返延迟 + 提示符回显）。
	// 注意：命令回显不算「真实输出」（见 terminalEchoEnd），否则慢启动的命令会被过早判定为跑完。
	terminalFirstIdleMS = 1500
	// terminalRawLimit 单次捕获的原始字节上限（防止一条命令刷爆内存）。
	terminalRawLimit = 8 << 20
	// echoScanLimit 回显定位的起点扫描上限（字节）：回显一定出现在捕获文本的开头附近，
	// 超出这个范围就不再回溯，避免在大段真实输出上做无谓的比较（见 terminalEchoEndByLength）。
	echoScanLimit = 4096
	// terminalMarkerPrefix 包裹命令时写入的退出码标记前缀。
	terminalMarkerPrefix = "__DING_EXIT__"
	// terminalBufferTailLines 返回里 bufferTail 读取的行数。
	terminalBufferTailLines = 5
	// terminalBufferTailMaxBytes bufferTail 的字节上限。
	terminalBufferTailMaxBytes = 2 << 10
	// terminalBusyMessage 并发调用同一会话时的中文拒绝原因（见 acquireTerminal）。
	terminalBusyMessage = "已被另一个 terminal.run / terminal.expect 占用"
)

// reTerminalEscape 终端里需要从「可见文本」中剔除的转义序列：
//
//	CSI：ESC [ 参数 中间字节 终止字节（颜色、光标、清屏…）
//	OSC：ESC ] … BEL 或 ESC \（窗口标题、OSC7 当前目录…）
//	其它两字节 ESC 序列
//
// 说明：这里只做「去掉控制序列」，不做终端模拟 —— 真正的屏幕状态在 xterm 里，
// 需要精确的可见行时用 read_terminal / terminal.buffer。
var reTerminalEscape = regexp.MustCompile(
	`\x1b\[[0-9;?]*[ -/]*[@-~]` + // CSI
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?` + // OSC（未收尾时也吃掉）
		`|\x1b[@-Z\\-_]`) // 其它两字节 ESC 序列

// ---- 宿主返回契约（按 JSON 字段名约定，与 appctl.go 的分层说明一致）----
//
// 宿主（debug.go）构造这些结构体返回；debugsrv 侧统一过 appctlObj 归一成 JSON 形状后再裁剪。
// 好处：宿主不需要知道 debugsrv 的限额策略，debugsrv 也不需要 import sshx / models。

// SFTPEntryInfo 一条远端目录条目。
type SFTPEntryInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	IsSymlink bool   `json:"isSymlink"`
	Size      int64  `json:"size"`
	Mode      string `json:"mode"` // 如 drwxr-xr-x（来自 SFTP 的属性位）
	ModTime   int64  `json:"modTime"`
}

// SFTPListPayload sftp.list 的宿主返回（Path 是解析后的绝对路径，Path="." 时由宿主用 Getwd 解析）。
type SFTPListPayload struct {
	Path    string          `json:"path"`
	Entries []SFTPEntryInfo `json:"entries"`
}

// SFTPStatPayload sftp.stat 的宿主返回。
type SFTPStatPayload struct {
	Path        string `json:"path"`
	IsDir       bool   `json:"isDir"`
	IsSymlink   bool   `json:"isSymlink"`
	Size        int64  `json:"size"`
	Mode        string `json:"mode"`
	ModTime     int64  `json:"modTime"`
	Target      string `json:"target,omitempty"`
	TargetIsDir bool   `json:"targetIsDir,omitempty"`
}

// SFTPReadPayload sftp.read 的宿主返回（Data 走 Go 的 []byte → base64）。
//
// EOF=true 表示「本次已读到文件尾」，据此判断 truncated。
type SFTPReadPayload struct {
	Path  string `json:"path"`
	Data  []byte `json:"data"`
	Size  int64  `json:"size"`
	EOF   bool   `json:"eof"`
	IsDir bool   `json:"isDir"`
}

// SFTPWritePayload sftp.write 的宿主返回。
type SFTPWritePayload struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	Appended bool   `json:"appended,omitempty"`
}

// SFTPMutationPayload sftp.mkdir / rename / remove / syncCwd 的宿主返回。
type SFTPMutationPayload struct {
	OK        bool   `json:"ok"`
	Path      string `json:"path,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Recursive bool   `json:"recursive,omitempty"`
}

// RemoteFSPathScope 由宿主注入：远端写白名单的来源（设置 → 调试模式 → MCP 能力 → 白名单）。
//
// 为什么要单独注入而不是复用 Gate：Gate 一次只能校验一个路径（rename 有两端），
// 而且「白名单为空 / .. 穿越 / 段边界」这套规则必须能在**没有宿主**的单测里被完整覆盖。
// 两者最终读的是同一份设置，语义一致。
type RemoteFSPathScope interface {
	SFTPWriteAllowlist() []string
}

// ---- 路径白名单（用户明确要求的安全边界，导出给宿主复用）----

// NormalizeRemotePath 归一化远端路径：必须以 / 开头，去掉重复斜杠与 . 段，拒绝 .. 段。
//
// 导出给宿主复用（debug.go 的写入路径校验），保证「AI 调 MCP 工具」与
// 「confirm.commit 走执行器」两条路径的规则完全一致。
func NormalizeRemotePath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errors.New("缺少远端路径")
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("远端路径 %q 必须是以 / 开头的绝对路径（白名单按绝对路径前缀匹配）", p)
	}
	segments := strings.Split(p, "/")
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		switch seg {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("远端路径 %q 含 .. 段：拒绝目录穿越（请先自行解析成绝对路径）", p)
		default:
			out = append(out, seg)
		}
	}
	return "/" + strings.Join(out, "/"), nil
}

// SFTPPathAllowed 判断（已归一化的）远端路径是否命中白名单前缀。
//
// 规则与 debug.go 的 pathAllowed 一致，并额外要求路径不能是根目录：
//   - 前缀完全相等，或以「前缀 + /」开头 —— /srv/app 放行 /srv/app 与 /srv/app/x，
//     但**不放行** /srv/app-evil（路径段边界匹配）；
//   - 大小写敏感（远端多为 Linux）；
//   - 空前缀（含把整个 / 写进白名单的情况）不参与匹配，避免「白名单 / 等于放行一切」。
func SFTPPathAllowed(allowlist []string, path string) bool {
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

// sftpAllowlist 当前白名单（宿主未注入时按「空 = 拒绝一切写」处理）。
func (s *Server) sftpAllowlist() []string {
	if s.opts.RemoteFSPaths == nil {
		return nil
	}
	return s.opts.RemoteFSPaths.SFTPWriteAllowlist()
}

// sftpRequireWritePath 校验一个写目标路径，返回归一化后的路径；不通过时返回中文原因。
func (s *Server) sftpRequireWritePath(label, raw string) (string, error) {
	p, err := NormalizeRemotePath(raw)
	if err != nil {
		return "", fmt.Errorf("%s：%w", label, err)
	}
	allow := s.sftpAllowlist()
	if len(allow) == 0 {
		return "", fmt.Errorf("远端写操作被拒绝（%s=%s）：远端写入白名单为空 —— 白名单留空 = 禁止一切 SFTP 写入 / 删除 / 改名 / 目录同步。"+
			"请在 设置 → 调试模式 → MCP 能力 →「远端文件写入」的白名单里填写允许的路径前缀（例如 /srv/app），再重试", label, p)
	}
	if !SFTPPathAllowed(allow, p) {
		return "", fmt.Errorf("远端路径 %q 不在写入白名单内（%s）：白名单按路径段前缀匹配，/srv/app 放行 /srv/app 与 /srv/app/x，"+
			"但**不放行** /srv/app-evil；当前白名单：%s。请在 设置 → 调试模式 → MCP 能力 中调整白名单",
			p, label, strings.Join(allow, "、"))
	}
	return p, nil
}

// sftpWritePathArgs 写类工具里需要过白名单的路径参数（sftp.rename 的 from 与 to 两端都要）。
var sftpWritePathArgs = map[string][]string{
	OpSftpWrite:   {"path"},
	OpSftpMkdir:   {"path"},
	OpSftpRename:  {"from", "to"},
	OpSftpRemove:  {"path"},
	OpSftpSyncCwd: {"remotePath"},
}

// preflightToolArgs 在「能力校验通过之后、token 校验之前」做白名单与体积预检。
//
// 为什么放在这里（mcp.go 的 mcpCallToolAudited 调用）：一次性 token 一旦被消费就不能再用，
// 若路径被白名单拒绝却已经把 token 烧掉，AI 只能重新 confirm.prepare —— 白白多一轮交互。
// 预检只做「不依赖远端 I/O 就能判定」的检查：路径规则 + 白名单 + 内容大小。
func (s *Server) preflightToolArgs(name string, args map[string]any) error {
	keys, ok := sftpWritePathArgs[name]
	if !ok {
		return nil
	}
	for _, k := range keys {
		v := appctlString(args, k)
		if v == "" {
			continue // 缺参由 handler 报（这里只做白名单）
		}
		if _, err := s.sftpRequireWritePath(k, v); err != nil {
			return err
		}
	}
	if name == OpSftpWrite {
		if _, err := sftpWriteContent(args); err != nil {
			return err
		}
	}
	return nil
}

// sftpWriteContent 从入参里取出要写入的字节：
//
//	base64 参数优先；否则 content 按 encoding 解释（默认 text，encoding=base64 时先解码）。
func sftpWriteContent(args map[string]any) ([]byte, error) {
	enc := strings.ToLower(appctlString(args, "encoding"))
	if enc == "" {
		enc = "text"
	}
	if enc != "text" && enc != "base64" {
		return nil, fmt.Errorf("%w: 非法 encoding %q（可用 text | base64）", ErrBadInput, enc)
	}
	if v, has := args["base64"]; has {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%w: base64 必须是字符串", ErrBadInput)
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("%w: base64 解码失败：%v", ErrBadInput, err)
		}
		if len(data) > sftpWriteMaxBytes {
			return nil, fmt.Errorf("写入内容过大：%d 字节超过上限 %d 字节（可分片写入，或用 append=true 追加）", len(data), sftpWriteMaxBytes)
		}
		return data, nil
	}
	v, has := args["content"]
	if !has {
		return nil, fmt.Errorf("%w: 缺少 content / base64（写空文件请显式传 content: \"\"）", ErrBadInput)
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("%w: content 必须是字符串", ErrBadInput)
	}
	data := []byte(s)
	if enc == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("%w: content 不是合法 base64：%v", ErrBadInput, err)
		}
		data = decoded
	}
	if len(data) > sftpWriteMaxBytes {
		return nil, fmt.Errorf("写入内容过大：%d 字节超过上限 %d 字节（可分片写入，或用 append=true 追加）", len(data), sftpWriteMaxBytes)
	}
	return data, nil
}

// sftpAuditSafeArgs 在 auditSafeArgs 的基础上，把「可能极大 / 可能含密」的长内容折叠成摘要。
//
// 为什么需要：sftp.write 的 content 与 base64 动辄几百 KB，原样进审计环形缓冲（500 条）
// 会把内存和 app.recent_calls 一起淹掉；审计只需要知道「写了多少字节」。
// （content 本身已由 appctlSensitiveArgKey 打成 ***，这里补的是 base64 与超长 command。）
func sftpAuditSafeArgs(tool string, args map[string]any) map[string]any {
	if len(args) == 0 {
		return args
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		s, isStr := v.(string)
		switch {
		case isStr && (k == "base64" || k == "content") && len(s) > 128:
			out[k] = fmt.Sprintf("<%d 字节的内容，已省略>", len(s))
		case isStr && k == "command" && len(s) > 1024:
			out[k] = s[:1024] + "…"
		default:
			out[k] = v
		}
	}
	return out
}

// ---- 工具条目（mcpTools 集中追加；能力要求行由 withCapabilityNotes 自动追加）----

// sftpToolEntries 返回 M8 全部工具条目（名称 / 描述 / 入参 schema）。
func sftpToolEntries() []mcpTool {
	tokenProp := strProp("两段式确认 token（confirm.prepare 取得；别名 token 亦可）")
	whitelistNote := "路径必须是绝对路径、不能含 .. 段，且必须命中「远端文件写入」白名单前缀；" +
		"白名单留空 = 一切写入 / 删除 / 改名被拒绝（中文原因里会说明去哪配置）。"
	return []mcpTool{
		// ---- A. 读 ----
		{Name: OpSftpList, Description: "列出远端目录（走会话已有的 SFTP 连接，不新建连接）。" +
			"返回 {path, count, entries[{name, path, isDir, isSymlink, size, mode, modTime}], truncated}；" +
			"最多 500 条（超出时 truncated=true）。path 省略 = 远端 home（用 SFTP 的当前目录解析）。",
			InputSchema: obj(map[string]any{
				"sessionId": strProp("SSH 会话 id（见 list_terminals 的 clientId / sessions）"),
				"path":      strProp("远端目录路径（默认 . = home）"),
			}, "sessionId"),
			// M9 结构化输出：形状见 sftpList 的返回。
			OutputSchema: outputObj(map[string]any{
				"sessionId": outProp("string"), "path": outProp("string"), "count": outProp("number"),
				"entries": outProp("array"), "truncated": outProp("boolean"), "limit": outProp("number"),
			}, "path", "count", "entries"),
			structured: structuredIdentity},
		{Name: OpSftpStat, Description: "读取远端单个路径的属性（Lstat 语义）：{path, isDir, size, mode, modTime, isSymlink, target?}。" +
			"isSymlink=true 时会给出 target（ReadLink 结果）与 targetIsDir。",
			InputSchema: obj(map[string]any{
				"sessionId": strProp("SSH 会话 id"),
				"path":      strProp("远端路径（文件或目录）"),
			}, "sessionId", "path")},
		{Name: OpSftpRead, Description: "读取远端文件内容并返回 sha256。" +
			"encoding=text（默认）返回 content（UTF-8 文本，按 maxBytes 截断并标 truncated=true）；" +
			"encoding=base64 返回 base64（二进制 / 非 UTF-8 用这个）。" +
			"返回 {path, bytes, size, offset, sha256, encoding, content|base64, truncated}。" +
			"maxBytes 默认 256KB、上限 1MB；大文件请用 offset 分片读（sha256 只覆盖本次返回的字节）。",
			InputSchema: obj(map[string]any{
				"sessionId": strProp("SSH 会话 id"),
				"path":      strProp("远端文件路径"),
				"encoding":  strProp("text | base64（默认 text）"),
				"maxBytes":  intProp("单次读取字节上限（默认 262144，最大 1048576）"),
				"offset":    intProp("起始偏移（默认 0，用于分片读大文件）"),
			}, "sessionId", "path")},
		{Name: OpSftpTransfers, Description: "列出当前 / 近期的 SFTP 传输（由既有的 sftp:transfer:<sessionId> 进度事件折叠而来，" +
			"只保留最近 300 条事件覆盖到的传输）：{id, sessionId, direction, path, bytes, total, state, startedAt}。" +
			"state: running | done | error | cancelled。id 可直接传给 sftp.cancel。",
			InputSchema: obj(map[string]any{
				"sessionId": strProp("只看某个会话的传输（可选）"),
				"limit":     intProp("返回条数（默认 50，最多 200）"),
			})},
		// ---- B. 写（全部需要 fs.remote.write + 两段式确认 + 白名单）----
		{Name: OpSftpWrite, Description: "写入远端文件（覆盖写；append=true 时追加）。" + whitelistNote +
			"content 是文本（encoding=text，默认）或 base64 字符串（encoding=base64）；二进制请直接用 base64 参数。" +
			"createDirs=true 时先递归创建父目录。单次内容上限 1MB。",
			InputSchema: obj(map[string]any{
				"sessionId":  strProp("SSH 会话 id"),
				"path":       strProp("远端目标路径"),
				"content":    strProp("要写入的内容（文本或 base64 字符串）"),
				"base64":     strProp("二进制内容（base64，与 content 二选一）"),
				"encoding":   strProp("content 的解释方式：text（默认）| base64"),
				"createDirs": boolProp("true = 先递归创建父目录"),
				"append":     boolProp("true = 追加而非覆盖"),
				"confirm":    tokenProp,
			}, "sessionId", "path")},
		{Name: OpSftpMkdir, Description: "创建远端目录（recursive=true 时等价于 mkdir -p）。" + whitelistNote,
			InputSchema: obj(map[string]any{
				"sessionId": strProp("SSH 会话 id"),
				"path":      strProp("要创建的目录"),
				"recursive": boolProp("true = 递归创建（缺省 false，父目录不存在会失败）"),
				"confirm":   tokenProp,
			}, "sessionId", "path")},
		{Name: OpSftpRename, Description: "重命名 / 移动远端文件或目录。**from 与 to 都要过白名单**。" + whitelistNote,
			InputSchema: obj(map[string]any{
				"sessionId": strProp("SSH 会话 id"),
				"from":      strProp("原路径"),
				"to":        strProp("新路径"),
				"confirm":   tokenProp,
			}, "sessionId", "from", "to")},
		{Name: OpSftpRemove, Description: "删除远端文件或目录（不可恢复）。recursive=false 时若目标是目录会被拒绝（中文提示改用 recursive=true）。" + whitelistNote,
			InputSchema: obj(map[string]any{
				"sessionId": strProp("SSH 会话 id"),
				"path":      strProp("要删除的路径"),
				"recursive": boolProp("true = 递归删除目录（沿用既有 removeRemote 实现）"),
				"confirm":   tokenProp,
			}, "sessionId", "path")},
		{Name: OpSftpSyncCwd, Description: "把 SFTP 面板的当前目录切换到 remotePath（非破坏性；**不会向终端键入任何字符**，" +
			"只推一条既有的 sftp:sync-path 事件）。remotePath 省略时使用该会话当前的面板目录。" + whitelistNote,
			InputSchema: obj(map[string]any{
				"sessionId":  strProp("SSH 会话 id"),
				"remotePath": strProp("要同步到的远端绝对路径（省略 = 保持当前面板目录）"),
				"confirm":    tokenProp,
			}, "sessionId")},
		{Name: OpSftpCancel, Description: "取消一条正在进行的 SFTP 传输（transferId 见 sftp.transfers 的 id，" +
			"形如 <sessionId>|<upload|download>|<文件名>）。需要 fs.remote.write 能力位与两段式确认。",
			InputSchema: obj(map[string]any{
				"transferId": strProp("传输 id（sftp.transfers 返回）"),
				"confirm":    tokenProp,
			}, "transferId")},
		// ---- C. 终端自动化 ----
		{Name: OpTerminalRun, Description: "在终端里跑一条命令并等结果（AI 管 SSH 的主力工具）。" +
			"发送 command（缺 \\r 自动补）→ 若给了 expect 正则则等它命中；未给 expect 时等到输出静默" +
			"（默认 idleMs=400ms 无新输出；**首块真实输出之前**最多等 1500ms）→ 返回" +
			"{matched, pattern?, exitMarker?, reason, elapsedMs, output, outputBytes, truncated, idleMs?, echoSkippedBytes?, bufferTail}。" +
			"output 是从发送前到现在**新增的可见文本**（已去掉 ANSI 转义，按 maxBytes 截断，默认 256KB / 上限 1MB），其中包含命令行回显 —— 回显只是显示出来，不参与 expect 匹配。" +
			"**expect 只在「命令回显之后」的文本上匹配**（PTY 会先回显命令行）：因此命令里含 `echo ===DONE===` + expect=`===DONE===` 这种写法不会命中回显；" +
			"更稳妥的写法是让标记在远端运行时才展开，例如 `D=__MCP_$(echo DONE)__; <你的命令>; echo $D` + expect=\"__MCP_DONE__\"（命令回显里不含该模式），" +
			"返回里的 echoSkippedBytes 会如实给出跳过的回显字节数。" +
			"**未给 expect 时按「输出静默」判定结束**：对「启动慢、先静默后吐输出」的命令（如 docker ps）可能过早返回、只拿到回显 —— " +
			"这类场景请两步走：先 terminal.run（不要 expect）拿初始输出，再用 terminal.expect 等结果；或加大 idleMs（50–60000）。" +
			"wrapExitMarker=true 会把命令包成 `command; printf '\\n__DING_EXIT__%s\\n' \"$?\"` 以拿到退出码（返回里 wrapped=true 如实说明命令被包裹过）；" +
			"此时若省略 expect，本工具会**自动等到退出码标记出现**再返回；手写 expect 时请用 `__DING_EXIT__[0-9]`" +
			"（回显里是字面量 `__DING_EXIT__%s`，没有数字）。" +
			"expect 超时**不报协议错误**：返回 matched=false + 已捕获输出 + 中文说明。" +
			"并发：同一会话上同一时刻只允许一个 terminal.run / terminal.expect，第二个调用会被**直接拒绝**（不排队），" +
			"避免两个调用互相偷走对方的输出增量。",
			InputSchema: obj(map[string]any{
				"id":             strProp("终端 id（标签 clientId，见 list_terminals）"),
				"command":        strProp("要执行的命令（不要带结尾的 \\r，会自动补）"),
				"expect":         strProp("等待命中的 Go 正则（可选；命中即返回。只在命令回显之后的输出上匹配）"),
				"timeoutMs":      intProp("总超时毫秒（默认 30000，范围 200–600000）"),
				"idleMs":         intProp("未给 expect 时的输出静默窗口毫秒（默认 400，范围 50–60000；给 expect 时忽略）"),
				"wrapExitMarker": boolProp("true = 用 printf 包裹命令以捕获退出码（会改动实际执行的命令行）"),
				"maxBytes":       intProp("output 的字节上限（默认 262144，最大 1048576）"),
			}, "id", "command")},
		{Name: OpTerminalExpect, Description: "纯等待（**不发送任何数据**）：等终端输出里出现 pattern 正则。" +
			"返回 {matched, pattern, reason, elapsedMs, output, outputBytes, truncated, marker}。" +
			"output 是本次调用期间新增的可见文本；sinceMarker 传上一次返回的 marker 可跳过已看过的内容（避免重复输出）。" +
			"本工具不发送命令，因此**没有回显可跳过**（不做回显过滤）：pattern 会在本次等待期间收到的全部新增文本上匹配。" +
			"超时返回 matched=false + 已捕获输出（不是协议错误）。与 terminal.run 共用同一套「同会话串行化」规则。",
			InputSchema: obj(map[string]any{
				"id":          strProp("终端 id"),
				"pattern":     strProp("等待命中的 Go 正则"),
				"timeoutMs":   intProp("超时毫秒（默认 30000，范围 200–600000）"),
				"sinceMarker": strProp("上一次返回的 marker：跳过它之前的内容（可选）"),
			}, "id", "pattern")},
	}
}

// ---- 分发（mcp.go 的 mcpCallTool 调用）----

// handleSftpOp 处理 sftp.* 工具：校验入参 → 调宿主 op → 裁剪 / 标记 → 组装 MCP 文本。
func (s *Server) handleSftpOp(ctx context.Context, name string, args map[string]any) ([]any, error) {
	var (
		out any
		err error
	)
	switch name {
	case OpSftpList:
		out, err = s.sftpList(ctx, args)
	case OpSftpStat:
		out, err = s.sftpStat(ctx, args)
	case OpSftpRead:
		out, err = s.sftpRead(ctx, args)
	case OpSftpTransfers:
		out, err = s.sftpTransfers(args), nil
	case OpSftpWrite:
		out, err = s.sftpWrite(ctx, args)
	case OpSftpMkdir:
		out, err = s.sftpMkdir(ctx, args)
	case OpSftpRename:
		out, err = s.sftpRename(ctx, args)
	case OpSftpRemove:
		out, err = s.sftpRemove(ctx, args)
	case OpSftpSyncCwd:
		out, err = s.sftpSyncCwd(ctx, args)
	case OpSftpCancel:
		out, err = s.sftpCancel(ctx, args)
	default:
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	if err != nil {
		return nil, err
	}
	return sftpMCPText(out)
}

// handleTerminalAutomationOp 处理 terminal.run / terminal.expect / terminal.sudo。
func (s *Server) handleTerminalAutomationOp(ctx context.Context, name string, args map[string]any) ([]any, error) {
	var (
		out any
		err error
	)
	switch name {
	case OpTerminalRun:
		out, err = s.terminalRun(ctx, args)
	case OpTerminalExpect:
		out, err = s.terminalExpect(ctx, args)
	case OpTerminalSudo:
		// sudo 凭证提权（M10）：实现在 sudo.go，与上面两个工具共用同一份会话串行化名额。
		out, err = s.terminalSudo(ctx, args)
	default:
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	if err != nil {
		return nil, err
	}
	return sftpMCPText(out)
}

// sftpMCPText 把本域的返回组装成 MCP 的文本内容（体积兜底 + 截断说明）。
func sftpMCPText(v any) ([]any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化结果失败: %w", err)
	}
	text := string(b)
	if len(text) > sftpMaxTextBytes {
		text = text[:sftpMaxTextBytes] +
			fmt.Sprintf("\n…（结果超过 %d 字节上限，已截断：请用更小的 maxBytes / limit，或分片读取）", sftpMaxTextBytes)
	}
	return []any{map[string]any{"type": "text", "text": text}}, nil
}

// ---- 入参读取 ----

// sftpSessionArg 读取会话 id（主名 sessionId，兼容 id）。
func sftpSessionArg(args map[string]any) (string, error) {
	sess := appctlString(args, "sessionId")
	if sess == "" {
		sess = appctlString(args, "id")
	}
	if sess == "" {
		return "", fmt.Errorf("%w: 缺少 sessionId（SSH 会话 id，见 list_terminals）", ErrBadInput)
	}
	return sess, nil
}

// sftpIntArg 读取整数入参并收敛到 [min, max]（给了非法值时报中文错）。
func sftpIntArg(args map[string]any, key string, def, min, max int) (int, error) {
	v, has := appctlInt(args, key)
	if !has {
		return def, nil
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%w: %s 取值 %d 越界（允许范围 %d–%d）", ErrBadInput, key, v, min, max)
	}
	return v, nil
}

// callSFTPHost 调宿主 op 并把返回值归一成 JSON 形状的 map。
func (s *Server) callSFTPHost(ctx context.Context, op string, args map[string]any) (map[string]any, error) {
	if s.opts.Handler == nil {
		return nil, errors.New("调试服务未就绪")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("序列化 %s 入参失败: %w", op, err)
	}
	v, err := s.opts.Handler.Handle(ctx, op, raw)
	if err != nil {
		return nil, err
	}
	return appctlObj(v), nil
}

// hostInt64 从宿主返回的 JSON map 里读整数（JSON 数字统一是 float64）。
func hostInt64(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

// hostStr 从宿主返回的 JSON map 里读字符串。
func hostStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// hostBool 从宿主返回的 JSON map 里读布尔。
func hostBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// truncateStringBytes 按字节截断字符串，并保证不切断 UTF-8 字符。
func truncateStringBytes(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// ---- A. 读 ----

// A1 sftp.list
func (s *Server) sftpList(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	path := appctlString(args, "path")
	if path == "" {
		path = "."
	}
	// 多要一条用于判断 truncated。
	limit := sftpMaxListEntries + 1
	raw, err := s.callSFTPHost(ctx, OpSftpList, map[string]any{
		"sessionId": sess, "path": path, "limit": limit,
	})
	if err != nil {
		return nil, err
	}
	entriesRaw, _ := raw["entries"].([]any)
	entries := make([]map[string]any, 0, len(entriesRaw))
	for _, item := range entriesRaw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entries = append(entries, map[string]any{
			"name":      hostStr(m, "name"),
			"path":      hostStr(m, "path"),
			"isDir":     hostBool(m, "isDir"),
			"isSymlink": hostBool(m, "isSymlink"),
			"size":      hostInt64(m, "size"),
			"mode":      hostStr(m, "mode"),
			"modTime":   hostInt64(m, "modTime"),
		})
	}
	// 目录在前、名字升序：输出稳定，便于 AI 比对两次调用。
	sort.SliceStable(entries, func(i, j int) bool {
		di, dj := entries[i]["isDir"].(bool), entries[j]["isDir"].(bool)
		if di != dj {
			return di
		}
		return entries[i]["name"].(string) < entries[j]["name"].(string)
	})
	truncated := len(entries) > sftpMaxListEntries
	if truncated {
		entries = entries[:sftpMaxListEntries]
	}
	resolved := hostStr(raw, "path")
	if resolved == "" {
		resolved = path
	}
	return map[string]any{
		"sessionId": sess,
		"path":      resolved,
		"count":     len(entries),
		"entries":   entries,
		"truncated": truncated,
		"limit":     sftpMaxListEntries,
	}, nil
}

// A2 sftp.stat
func (s *Server) sftpStat(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	path := appctlString(args, "path")
	if path == "" {
		return nil, fmt.Errorf("%w: 缺少 path", ErrBadInput)
	}
	raw, err := s.callSFTPHost(ctx, OpSftpStat, map[string]any{"sessionId": sess, "path": path})
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"sessionId": sess,
		"path":      hostStr(raw, "path"),
		"isDir":     hostBool(raw, "isDir"),
		"isSymlink": hostBool(raw, "isSymlink"),
		"size":      hostInt64(raw, "size"),
		"mode":      hostStr(raw, "mode"),
		"modTime":   hostInt64(raw, "modTime"),
	}
	if out["path"] == "" {
		out["path"] = path
	}
	if target := hostStr(raw, "target"); target != "" {
		out["target"] = target
		out["targetIsDir"] = hostBool(raw, "targetIsDir")
	}
	return out, nil
}

// A3 sftp.read
func (s *Server) sftpRead(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	path := appctlString(args, "path")
	if path == "" {
		return nil, fmt.Errorf("%w: 缺少 path", ErrBadInput)
	}
	encoding := strings.ToLower(appctlString(args, "encoding"))
	if encoding == "" {
		encoding = "text"
	}
	if encoding != "text" && encoding != "base64" {
		return nil, fmt.Errorf("%w: 非法 encoding %q（可用 text | base64）", ErrBadInput, encoding)
	}
	maxBytes, err := sftpIntArg(args, "maxBytes", sftpReadDefaultBytes, 1, sftpReadMaxBytes)
	if err != nil {
		return nil, err
	}
	offset, hasOffset := appctlInt(args, "offset")
	if !hasOffset {
		offset = 0
	}
	if offset < 0 || offset > sftpMaxOffset {
		return nil, fmt.Errorf("%w: offset 取值 %d 越界（允许 0–%d）", ErrBadInput, offset, sftpMaxOffset)
	}
	raw, err := s.callSFTPHost(ctx, OpSftpRead, map[string]any{
		"sessionId": sess, "path": path, "offset": offset, "maxBytes": maxBytes,
	})
	if err != nil {
		return nil, err
	}
	if hostBool(raw, "isDir") {
		return nil, fmt.Errorf("远端路径 %q 是目录，不能用 sftp.read 读取：请用 sftp.list 列目录", path)
	}
	dataB64 := hostStr(raw, "data")
	data, decErr := base64.StdEncoding.DecodeString(dataB64)
	if decErr != nil {
		return nil, fmt.Errorf("读取结果解码失败（宿主返回的 data 不是合法 base64）: %v", decErr)
	}
	sum := sha256.Sum256(data)
	resolved := hostStr(raw, "path")
	if resolved == "" {
		resolved = path
	}
	out := map[string]any{
		"sessionId": sess,
		"path":      resolved,
		"bytes":     len(data),
		"size":      hostInt64(raw, "size"),
		"offset":    offset,
		"sha256":    hex.EncodeToString(sum[:]),
		"encoding":  encoding,
		"truncated": !hostBool(raw, "eof"),
	}
	if encoding == "base64" {
		out["base64"] = base64.StdEncoding.EncodeToString(data)
	} else {
		out["content"] = string(data)
	}
	return out, nil
}

// A4 sftp.transfers：把 Hub 里的 sftp.transfer 进度事件折叠成「每条传输的最新状态」。
//
// 为什么不新增接口：既有进度事件（internal/sshx 的 sftp:transfer:<sessionId>）已经带
// direction / name / transferred / total / done / error，Hub 又保留最近 300 条，
// 足够还原「进行中 / 刚结束」的传输列表；M8 只是把它读出来，不再新开一条推送通道。
func (s *Server) sftpTransfers(args map[string]any) any {
	sess := appctlString(args, "sessionId")
	limit := 50
	if v, has := appctlInt(args, "limit"); has && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}
	items := s.transferSnapshots(sess)
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{
		"count":     len(items),
		"transfers": items,
		"limit":     limit,
		"note": "数据来自既有的 sftp:transfer 进度事件环形缓冲（最近 300 条事件），" +
			"因此只能看到「近期」传输；id 形如 <sessionId>|<direction>|<文件名>，可直接传给 sftp.cancel。",
	}
}

// transferSnapshot 一条传输的当前状态。
type transferSnapshot struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Direction string `json:"direction"`
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	Total     int64  `json:"total,omitempty"`
	State     string `json:"state"`
	StartedAt int64  `json:"startedAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Error     string `json:"error,omitempty"`
}

// transferSnapshots 折叠 Hub 里的传输事件（最新的在前）。
func (s *Server) transferSnapshots(sessionID string) []transferSnapshot {
	if s.opts.Hub == nil {
		return []transferSnapshot{}
	}
	events := s.opts.Hub.Recent(hubRecentLimit, TransferTopic)
	byID := make(map[string]*transferSnapshot, len(events))
	order := make([]string, 0, len(events))
	for _, ev := range events {
		var p struct {
			SessionID   string `json:"sessionId"`
			Direction   string `json:"direction"`
			Name        string `json:"name"`
			Transferred int64  `json:"transferred"`
			Total       int64  `json:"total"`
			Done        bool   `json:"done"`
			Error       string `json:"error"`
		}
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			continue
		}
		if p.SessionID == "" {
			p.SessionID = ev.SessionID
		}
		if sessionID != "" && p.SessionID != sessionID {
			continue
		}
		id := transferID(p.SessionID, p.Direction, p.Name)
		item, ok := byID[id]
		if !ok {
			item = &transferSnapshot{
				ID: id, SessionID: p.SessionID, Direction: p.Direction, Path: p.Name,
				StartedAt: ev.TS, State: "running",
			}
			byID[id] = item
			order = append(order, id)
		}
		item.Bytes = p.Transferred
		item.Total = p.Total
		item.UpdatedAt = ev.TS
		switch {
		case !p.Done:
			item.State = "running"
		case p.Error == "已取消":
			item.State = "cancelled"
			item.Error = p.Error
		case p.Error != "":
			item.State = "error"
			item.Error = p.Error
		default:
			item.State = "done"
		}
	}
	out := make([]transferSnapshot, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}

// TransferTopic 传输进度事件在 Hub 里的 topic（sftp:transfer:<sessionId>）。
const TransferTopic = "sftp.transfer"

// transferID 传输 id：<sessionId>|<direction>|<name>（文件名可能含 |，解析时用 SplitN）。
func transferID(sessionID, direction, name string) string {
	return sessionID + "|" + direction + "|" + name
}

// ParseTransferID 解析 sftp.cancel 的 transferId（导出给宿主复用：
// confirm.commit 走执行器时 args 里只有 transferId，需要在那里再解析一次）。
func ParseTransferID(id string) (sessionID, direction, name string, err error) {
	parts := strings.SplitN(strings.TrimSpace(id), "|", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("%w: 非法 transferId %q（应为 <sessionId>|<upload|download>|<文件名>，见 sftp.transfers）", ErrBadInput, id)
	}
	if parts[1] != "upload" && parts[1] != "download" {
		return "", "", "", fmt.Errorf("%w: 非法 direction %q（只支持 upload | download）", ErrBadInput, parts[1])
	}
	return parts[0], parts[1], parts[2], nil
}

// ---- B. 写（全部先过白名单，再交给宿主执行）----

// sftpWrite 写入远端文件。
func (s *Server) sftpWrite(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	if appctlString(args, "path") == "" {
		return nil, fmt.Errorf("%w: 缺少 path", ErrBadInput)
	}
	path, err := s.sftpRequireWritePath("path", appctlString(args, "path"))
	if err != nil {
		return nil, err
	}
	data, err := sftpWriteContent(args)
	if err != nil {
		return nil, err
	}
	raw, err := s.callSFTPHost(ctx, OpSftpWrite, map[string]any{
		"sessionId":  sess,
		"path":       path,
		"base64":     base64.StdEncoding.EncodeToString(data),
		"createDirs": appctlBool(args, "createDirs"),
		"append":     appctlBool(args, "append"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"sessionId": sess,
		"path":      firstNonEmpty(hostStr(raw, "path"), path),
		"bytes":     len(data),
		"append":    appctlBool(args, "append"),
		"note":      "写入成功（远端改动不可撤销，无法通过审计「撤销」回滚）",
	}, nil
}

// sftpMkdir 创建远端目录。
func (s *Server) sftpMkdir(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	if appctlString(args, "path") == "" {
		return nil, fmt.Errorf("%w: 缺少 path", ErrBadInput)
	}
	path, err := s.sftpRequireWritePath("path", appctlString(args, "path"))
	if err != nil {
		return nil, err
	}
	recursive := appctlBool(args, "recursive")
	raw, err := s.callSFTPHost(ctx, OpSftpMkdir, map[string]any{
		"sessionId": sess, "path": path, "recursive": recursive,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"sessionId": sess,
		"path":      firstNonEmpty(hostStr(raw, "path"), path),
		"recursive": recursive,
	}, nil
}

// sftpRename 重命名 / 移动远端路径（from 与 to 都要过白名单）。
func (s *Server) sftpRename(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	if appctlString(args, "from") == "" || appctlString(args, "to") == "" {
		return nil, fmt.Errorf("%w: 缺少 from / to", ErrBadInput)
	}
	from, err := s.sftpRequireWritePath("from", appctlString(args, "from"))
	if err != nil {
		return nil, err
	}
	to, err := s.sftpRequireWritePath("to", appctlString(args, "to"))
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, fmt.Errorf("%w: from 与 to 相同（%s），无需重命名", ErrBadInput, from)
	}
	raw, err := s.callSFTPHost(ctx, OpSftpRename, map[string]any{"sessionId": sess, "from": from, "to": to})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"sessionId": sess,
		"from":      firstNonEmpty(hostStr(raw, "from"), from),
		"to":        firstNonEmpty(hostStr(raw, "to"), to),
	}, nil
}

// sftpRemove 删除远端文件或目录。
func (s *Server) sftpRemove(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	if appctlString(args, "path") == "" {
		return nil, fmt.Errorf("%w: 缺少 path", ErrBadInput)
	}
	path, err := s.sftpRequireWritePath("path", appctlString(args, "path"))
	if err != nil {
		return nil, err
	}
	recursive := appctlBool(args, "recursive")
	raw, err := s.callSFTPHost(ctx, OpSftpRemove, map[string]any{
		"sessionId": sess, "path": path, "recursive": recursive,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"sessionId": sess,
		"path":      firstNonEmpty(hostStr(raw, "path"), path),
		"recursive": recursive,
		"note":      "删除不可恢复（无法通过审计「撤销」回滚）",
	}, nil
}

// sftpSyncCwd 把 SFTP 面板的当前目录切到 remotePath（非破坏性，但同属写类：需要能力位 + 白名单 + 确认）。
func (s *Server) sftpSyncCwd(ctx context.Context, args map[string]any) (any, error) {
	sess, err := sftpSessionArg(args)
	if err != nil {
		return nil, err
	}
	target := appctlString(args, "remotePath")
	if target == "" {
		// 省略时用该会话当前的面板目录（页面内事实，只有前端桥知道）。
		cur, cerr := s.sessionSftpPath(ctx, sess)
		if cerr != nil || cur == "" {
			return nil, fmt.Errorf("%w: 缺少 remotePath（省略时需要用会话当前的面板目录，但当前会话没有已同步的 SFTP 路径）", ErrBadInput)
		}
		target = cur
	}
	path, err := s.sftpRequireWritePath("remotePath", target)
	if err != nil {
		return nil, err
	}
	raw, err := s.callSFTPHost(ctx, OpSftpSyncCwd, map[string]any{"sessionId": sess, "remotePath": path})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"sessionId": sess,
		"path":      firstNonEmpty(hostStr(raw, "path"), path),
		"note":      "只切换 SFTP 面板的当前目录（推了一条 sftp:sync-path 事件），没有向终端键入任何字符",
	}, nil
}

// sftpCancel 取消一条正在进行的传输。
func (s *Server) sftpCancel(ctx context.Context, args map[string]any) (any, error) {
	id := appctlString(args, "transferId")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 transferId（见 sftp.transfers）", ErrBadInput)
	}
	sess, direction, name, err := ParseTransferID(id)
	if err != nil {
		return nil, err
	}
	if _, err := s.callSFTPHost(ctx, OpSftpCancel, map[string]any{
		"sessionId": sess, "direction": direction, "name": name,
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":         true,
		"transferId": id,
		"sessionId":  sess,
		"direction":  direction,
		"name":       name,
		"note":       "已请求取消：进行中的传输会在下一个数据块处停止（既有实现会删除未完成的本地 / 远端文件）",
	}, nil
}

// sessionSftpPath 读取某个会话在前端的面板目录（页面内事实，走既有的 tabs 桥）。
func (s *Server) sessionSftpPath(ctx context.Context, sessionID string) (string, error) {
	if s.opts.Handler == nil {
		return "", errors.New("调试服务未就绪")
	}
	v, err := s.opts.Handler.Handle(ctx, OpSessions, nil)
	if err != nil {
		return "", err
	}
	root := appctlObj(v)
	tabs, _ := root["tabs"].([]any)
	for _, item := range tabs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if hostStr(m, "clientId") == sessionID || hostStr(m, "sessionId") == sessionID {
			if p := hostStr(m, "sftpPath"); p != "" {
				return p, nil
			}
		}
	}
	return "", nil
}

// ---- C. 终端自动化 ----

// acquireTerminal 非阻塞地占住一个会话的「终端等待」名额。
//
// 语义（工具描述里也写清了）：同一会话上同一时刻只允许一个 terminal.run / terminal.expect，
// 第二个调用**直接拒绝**而不是排队 —— 两个调用同时消费同一份输出增量时，
// 「从发送前到结束之间新增的文本」对任何一方都不再成立（会互相偷走对方的输出），
// 明确失败比给出不可信的结果更好。
func (s *Server) acquireTerminal(id string) bool {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	if s.termBusy == nil {
		s.termBusy = make(map[string]bool)
	}
	if s.termBusy[id] {
		return false
	}
	s.termBusy[id] = true
	return true
}

// releaseTerminal 释放名额。
func (s *Server) releaseTerminal(id string) {
	s.termMu.Lock()
	if s.termBusy != nil {
		delete(s.termBusy, id)
	}
	s.termMu.Unlock()
}

// terminalBusyError 并发冲突时的中文原因。
func terminalBusyError(id string) error {
	return fmt.Errorf("终端 %s %s：同一会话上只允许一个 terminal.run / terminal.expect（并发调用直接拒绝，不排队）。"+
		"请等上一次调用返回后重试，或改用另一个会话（原因：两个调用同时消费同一份输出增量会互相偷走对方的文本）",
		id, terminalBusyMessage)
}

// terminalArgs 终端自动化工具解析后的公共入参。
type terminalArgs struct {
	sessionID string
	pattern   *regexp.Regexp
	timeout   time.Duration
	maxBytes  int
}

// parseTerminalArgs 解析 id / 超时 / 输出上限 / 正则。
func parseTerminalArgs(args map[string]any, pattern string, needPattern bool) (terminalArgs, error) {
	out := terminalArgs{}
	id := appctlString(args, "id")
	if id == "" {
		return out, fmt.Errorf("%w: 缺少 id（终端 id，见 list_terminals）", ErrBadInput)
	}
	out.sessionID = id
	timeoutMS, err := sftpIntArg(args, "timeoutMs", terminalDefTimeoutMS, terminalMinTimeoutMS, terminalMaxTimeoutMS)
	if err != nil {
		return out, err
	}
	out.timeout = time.Duration(timeoutMS) * time.Millisecond
	maxBytes, err := sftpIntArg(args, "maxBytes", terminalDefMaxBytes, 1024, terminalMaxMaxBytes)
	if err != nil {
		return out, err
	}
	out.maxBytes = maxBytes
	if needPattern {
		if strings.TrimSpace(pattern) == "" {
			return out, fmt.Errorf("%w: 缺少 pattern（要等待的正则）", ErrBadInput)
		}
		re, cerr := regexp.Compile(pattern)
		if cerr != nil {
			return out, fmt.Errorf("%w: pattern 不是合法正则：%v", ErrBadInput, cerr)
		}
		out.pattern = re
	}
	return out, nil
}

// terminalRun 实现 terminal.run。
func (s *Server) terminalRun(ctx context.Context, args map[string]any) (any, error) {
	command := appctlString(args, "command")
	if command == "" {
		return nil, fmt.Errorf("%w: 缺少 command", ErrBadInput)
	}
	expect := appctlString(args, "expect")
	ta, err := parseTerminalArgs(args, expect, false)
	if err != nil {
		return nil, err
	}
	if expect != "" {
		re, cerr := regexp.Compile(expect)
		if cerr != nil {
			return nil, fmt.Errorf("%w: expect 不是合法正则：%v", ErrBadInput, cerr)
		}
		ta.pattern = re
	}
	if s.opts.Hub == nil {
		return nil, errors.New("事件流未启用（Hub 为空），无法捕获终端输出")
	}
	// wrapExitMarker 的语义是「我要退出码」：未给 expect 时自动等到退出码标记出现，
	// 而不是靠「输出静默」猜命令是否跑完。
	// 注意匹配 __DING_EXIT__[0-9]+ 而不是 __DING_EXIT__：后者会先命中**命令行回显**
	//（回显里是字面量 __DING_EXIT__%s，没有数字）。
	if wrap := appctlBool(args, "wrapExitMarker"); wrap && ta.pattern == nil {
		ta.pattern = reExitMarkerCode
	}
	if !s.acquireTerminal(ta.sessionID) {
		return nil, terminalBusyError(ta.sessionID)
	}
	defer s.releaseTerminal(ta.sessionID)

	// 订阅必须在发送**之前**：Hub 不缓存历史事件，订阅晚了会丢掉开头的输出。
	hubID, ch := s.opts.Hub.Subscribe(TerminalOutputTopic)
	defer s.opts.Hub.Unsubscribe(hubID)

	wrap := appctlBool(args, "wrapExitMarker")
	sent := command
	if wrap {
		sent = wrapExitCommand(command)
	}
	if !strings.HasSuffix(sent, "\r") {
		sent += "\r"
	}
	if _, err := s.callSFTPHost(ctx, OpTerminalInput, map[string]any{"id": ta.sessionID, "data": sent}); err != nil {
		return nil, fmt.Errorf("向终端 %s 发送命令失败：%w", ta.sessionID, err)
	}

	// idleMs：自定义「输出静默」窗口（只在没有 expect / wrapExitMarker 时生效）。
	idleMS, err := sftpIntArg(args, "idleMs", terminalIdleMS, terminalMinIdleMS, terminalMaxIdleMS)
	if err != nil {
		return nil, err
	}
	idle := 0
	if ta.pattern == nil {
		idle = idleMS
	}
	// echoProbe 是「刚刚发出去的这行命令」：PTY 会先把它回显回来，
	// 匹配时必须跳过这段回显，否则 expect 命中命令本身而不是真实输出（见 terminalEchoEnd）。
	echoProbe := strings.TrimRight(sent, "\r\n")
	wait := s.waitTerminal(ctx, ch, ta.sessionID, ta.pattern, ta.timeout, idle, echoProbe)
	visible := wait.visible
	outputBytes := len(visible)
	out, truncated := truncateStringBytes(visible, ta.maxBytes)

	matched := ta.pattern != nil && wait.matched
	res := map[string]any{
		"sessionId":   ta.sessionID,
		"matched":     matched,
		"reason":      wait.reason,
		"elapsedMs":   wait.elapsed.Milliseconds(),
		"output":      out,
		"outputBytes": outputBytes,
		"truncated":   truncated,
		"wrapped":     wrap,
		"bufferTail":  s.terminalBufferTail(ctx, ta.sessionID),
	}
	if wait.echoEnd > 0 {
		// 如实说明「匹配跳过了多少字节的命令回显」：AI 据此能判断 expect 是不是被回显干扰过。
		res["echoSkippedBytes"] = wait.echoEnd
	}
	if ta.pattern == nil {
		res["idleMs"] = idleMS
	}
	if expect != "" {
		res["pattern"] = expect
	}
	if wrap {
		if code, ok := extractExitMarker(visible); ok {
			res["exitMarker"] = code
		} else {
			res["exitMarker"] = nil
		}
		res["wrappedCommand"] = sent
		res["note"] = "命令被包裹过（wrapExitMarker=true）：实际执行的是上面的 wrappedCommand，退出码由 printf 标记捕获。"
	}
	switch {
	case matched:
		res["hint"] = "expect 已命中（已跳过命令回显：命中的是回显之后的输出）。"
	case wait.reason == "idle":
		res["hint"] = fmt.Sprintf("未给 expect：等到输出静默（%dms 无新输出）即返回；若命令还没跑完（例如先静默后吐输出），"+
			"可加大 idleMs，或改用 terminal.expect 继续等。", idleMS)
	case wait.reason == "timeout":
		res["hint"] = fmt.Sprintf("expect 在 %dms 内未命中（不是协议错误）：已返回捕获到的输出；命令可能仍在执行 —— "+
			"可加大 timeoutMs、改成更宽松的正则，或先用 terminal.expect 继续等。", ta.timeout.Milliseconds())
	case wait.reason == "closed":
		res["hint"] = "事件流已关闭（会话可能已断开），返回的是断开前捕获到的输出。"
	case wait.reason == "canceled":
		res["hint"] = "调用方取消了请求，返回的是取消前捕获到的输出。"
	}
	if outputBytes > len(out) {
		res["truncatedHint"] = fmt.Sprintf("可见文本共 %d 字节，按 maxBytes=%d 截断（只保留开头部分）", outputBytes, ta.maxBytes)
	}
	if wait.dropped {
		res["droppedOutput"] = true
		res["droppedHint"] = fmt.Sprintf("输出量超过单次捕获上限（%d 字节），超出部分的输出已被丢弃；"+
			"建议改用更精确的命令，或分多次 terminal.expect 读取", terminalRawLimit)
	}
	return res, nil
}

// terminalExpect 实现 terminal.expect（不发送任何数据）。
func (s *Server) terminalExpect(ctx context.Context, args map[string]any) (any, error) {
	pattern := appctlString(args, "pattern")
	ta, err := parseTerminalArgs(args, pattern, true)
	if err != nil {
		return nil, err
	}
	if s.opts.Hub == nil {
		return nil, errors.New("事件流未启用（Hub 为空），无法捕获终端输出")
	}
	if !s.acquireTerminal(ta.sessionID) {
		return nil, terminalBusyError(ta.sessionID)
	}
	defer s.releaseTerminal(ta.sessionID)

	hubID, ch := s.opts.Hub.Subscribe(TerminalOutputTopic)
	defer s.opts.Hub.Unsubscribe(hubID)

	wait := s.waitTerminal(ctx, ch, ta.sessionID, ta.pattern, ta.timeout, 0, "")
	visible := wait.visible
	if marker := appctlString(args, "sinceMarker"); marker != "" {
		if idx := strings.LastIndex(visible, marker); idx >= 0 {
			visible = visible[idx+len(marker):]
		}
	}
	outputBytes := len(visible)
	out, truncated := truncateStringBytes(visible, ta.maxBytes)
	res := map[string]any{
		"sessionId":   ta.sessionID,
		"matched":     wait.matched,
		"pattern":     pattern,
		"reason":      wait.reason,
		"elapsedMs":   wait.elapsed.Milliseconds(),
		"output":      out,
		"outputBytes": outputBytes,
		"truncated":   truncated,
		// marker 是本段输出的尾部片段，下次调用用 sinceMarker 传回来即可跳过已看过的内容。
		"marker": tailString(visible, 120),
	}
	if wait.matched {
		res["hint"] = "pattern 已命中。"
	} else {
		res["hint"] = fmt.Sprintf("pattern 在 %dms 内未命中（不是协议错误）：已返回本次等待期间捕获的输出。"+
			"可加大 timeoutMs 或放宽正则后重试。", ta.timeout.Milliseconds())
	}
	return res, nil
}

// waitTerminalResult 一次终端等待的结果。
type waitTerminalResult struct {
	matched bool
	reason  string // matched | idle | timeout | closed | canceled
	elapsed time.Duration
	visible string
	// dropped=true 表示原始捕获量超过上限，后续输出被丢弃（内存保护）。
	dropped bool
	// echoEnd 匹配时跳过的「命令回显」字节数（0 = 未发现回显，没跳过）。
	echoEnd int
}

// waitTerminal 从 Hub 事件里累积「本次调用期间的新增可见文本」，直到：
//   - 正则命中（reason=matched）；
//   - 输出静默（idleWindow > 0 且超过 idleWindow 无新输出，reason=idle）；
//   - 超时（reason=timeout）；
//   - 事件流关闭 / 调用方取消。
//
// echoCmd 是本次发送的命令行（terminal.expect 传空）：PTY 会先回显命令本身，
// 若 expect 的模式出现在命令里，匹配会命中回显而不是真实输出（M9 修的缺陷），
// 因此匹配只在「回显之后」的文本上进行 —— 见 terminalEchoEnd 的判定规则。
//
// 首块**真实输出**之前用更宽的宽限（terminalFirstIdleMS）：SSH 往返 + 远端执行需要时间，
// 直接用 idleWindow 会在慢链路上误判成「已经结束」。命令回显不算真实输出，
// 否则「先回显、后静默、最后才吐输出」的命令（如 docker ps）会在 400ms 就被判成跑完。
func (s *Server) waitTerminal(
	ctx context.Context, ch <-chan Event, id string, re *regexp.Regexp,
	timeout time.Duration, idleWindow int, echoCmd string,
) waitTerminalResult {
	start := time.Now()
	text := &terminalText{limit: terminalRawLimit}
	var (
		echoEnd     int  // 已发现的回显结束位置（单调不减）
		echoDone    bool // 回显结束位置是否已「收尾在换行处」（= 不会再增长，不必重复定位）
		realStarted bool // 是否已出现「回显之外」的真实输出
	)
	finish := func(reason string, matched bool) waitTerminalResult {
		return waitTerminalResult{
			matched: matched, reason: reason, elapsed: time.Since(start),
			visible: text.visible(), dropped: text.truncatedRaw(), echoEnd: echoEnd,
		}
	}
	// match 只在「命令回显之后」的文本上匹配正则；顺带维护 realStarted
	//（未给 expect 的 idle 模式也要靠它区分「只有回显」与「已有真实输出」）。
	//
	// 回显定位只在「还没收尾」时做：terminalEchoEnd 是 O(命令长度 × 扫描上限) 的比较
	//（见 terminalEchoEndByLength），一旦定位结果收尾在换行处就不会再变，重复计算只是浪费。
	match := func() bool {
		body := text.visible()
		if !echoDone {
			if skip := terminalEchoEnd(body, echoCmd); skip > echoEnd {
				echoEnd = skip
			}
			if echoEnd > 0 && echoEnd <= len(body) && body[echoEnd-1] == '\n' {
				echoDone = true
			}
		}
		body = body[echoEnd:]
		if strings.TrimSpace(body) != "" {
			realStarted = true
		}
		return re != nil && re.MatchString(body)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	var idleC <-chan time.Time
	var idleTimer *time.Timer
	if idleWindow > 0 {
		idleTimer = time.NewTimer(time.Duration(terminalFirstIdleMS) * time.Millisecond)
		idleC = idleTimer.C
		defer idleTimer.Stop()
	}
	if match() {
		return finish("matched", true)
	}
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return finish("closed", false)
			}
			if ev.Topic != TerminalOutputTopic || ev.SessionID != id {
				continue
			}
			chunk := decodeOutputChunk(ev)
			if len(chunk) == 0 {
				continue
			}
			text.push(chunk)
			matched := match()
			if idleTimer != nil {
				// 真实输出出现前保持更宽的宽限（命令回显不重置成短窗口）。
				window := idleWindow
				if !realStarted {
					window = maxInt(idleWindow, terminalFirstIdleMS)
				}
				resetTimer(idleTimer, time.Duration(window)*time.Millisecond)
			}
			if matched {
				return finish("matched", true)
			}
		case <-deadline.C:
			return finish("timeout", false)
		case <-idleC:
			return finish("idle", false)
		case <-ctx.Done():
			return finish("canceled", false)
		}
	}
}

// terminalEchoEnd 返回「本次发送命令的回显」在累积可见文本里的结束位置（0 = 尚未发现回显）。
//
// 为什么需要它：PTY 会先把命令行本身回显出来（ECHO）。若 expect 的模式出现在命令里
// （例如命令 `echo ===DONE===` + expect `===DONE===`），匹配就会命中**回显**，
// 命令还没执行就返回 matched=true（M9 修的真实缺陷：1ms 返回、输出只有命令行本身）。
//
// 两个口径一起用，取更稳的那个（M10 修的长命令缺陷）：
//
//  1. **长度口径（terminalEchoEndByLength）**：逐字节比较命令文本，比较时**跳过换行**，
//     因此命令比终端宽度长、PTY 把回显折行成交错多段时也能定位准。
//     依据：PTY 回显的是**同一串字节**，可见文本里属于回显的字节序列与命令逐字节一致
//     （中文按字节比较，与显示宽度无关）；折行只往里插换行，不改变其它字节。
//  2. **换行口径（terminalEchoEndByNewline）**：既有做法 —— 在文本里找命令第一行的**逐字**出现，
//     再取其后第一个换行。命令没被折行时它最精确，因此仍然保留。
//
// 为什么取两者中较大者：折行会让逐字命中失败或只命中残片（换行口径低估 → 回显残片会参与
// expect 匹配，正是实测到的现象），而长度口径给出的是「整段回显结束」的位置；反过来长度口径
// 找不到可信回显（例如终端不回声）时换行口径仍然可用。两个口径的取值都停在换行边界
// （或停在「回显还没收完」的估算位置），因此取较大者不会越过整行回显。
//
// 兜底：终端不回声（stty -echo）、回显被重绘打散（字节不再连续）或可见文本比命令还短时，
// 两个口径都返回 0 = 不跳过，行为与 M9 之前一致 —— 不会因为跳过逻辑而漏掉真实输出。
//
// 多行命令只按**第一行**定位（沿用既有语义）：多行命令的后续行回显会与第一行的真实输出交错，
// 把整段都算成回显会把真实输出一起吞掉。
func terminalEchoEnd(visible, command string) int {
	if visible == "" || command == "" {
		return 0
	}
	line := command
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return 0
	}
	byNewline := terminalEchoEndByNewline(visible, line)
	byLength := terminalEchoEndByLength(visible, line)
	if byLength > byNewline {
		return byLength
	}
	return byNewline
}

// terminalEchoEndByNewline 换行口径：命令第一行逐字出现在文本里，取其后的第一个换行。
//
// 其后暂时没有换行时返回「命中位置 + 命令长度」（把命令行文本本身算作回显），
// 这样即使终端只回显 \r 不回显 \n，也不会把命令本身匹配上，而其后的真实输出仍能匹配。
func terminalEchoEndByNewline(visible, line string) int {
	idx := strings.Index(visible, line)
	if idx < 0 {
		return 0
	}
	end := idx + len(line)
	if nl := strings.IndexByte(visible[end:], '\n'); nl >= 0 {
		return end + nl + 1
	}
	return end
}

// terminalEchoEndByLength 长度口径：在可见文本里定位「（可能被折行的）命令回显」的结束位置。
//
// 与换行口径的区别：这里**允许回显被折行**——比较时跳过可见文本里多出来的换行（PTY 折行 /
// readline 重绘插入的 \r\n 归一化后的 \n），因此命令长度超过终端宽度时也能定位准。
//
// 实现：按「候选起点」逐个尝试（起点可以是行首，也可以是提示符之后的行内位置），
// 逐字节与命令比较、比较时跳过换行；命中整行后取其后的第一个换行收尾。
// 起点扫描上限 echoScanLimit 字节：回显一定出现在捕获的开头附近，超出这个范围不再回溯，
// 避免在一大段真实输出上做无谓的二次扫描（超大输出是常态，这里不能退化成 O(n·m)）。
//
// 返回 0 表示「没有找到可信的回显」：此时**不跳过**（宁可少跳过一点，也不要误吞真实输出）。
func terminalEchoEndByLength(visible, line string) int {
	limit := len(visible)
	if limit > echoScanLimit {
		limit = echoScanLimit
	}
	for start := 0; start < limit; start++ {
		if visible[start] != line[0] {
			continue
		}
		if end := echoMatchAt(visible, start, line); end > 0 {
			return end
		}
	}
	return 0
}

// echoMatchAt 判断 visible[start:] 是否以「折行后的 line」开头（比较时跳过换行）。
//
// 命中返回回显的结束位置（其后第一个换行之后；其后暂时没有换行则停在最后一个匹配字节之后），
// 未命中返回 0。字节比较（不做 rune 归一化）：中文等多字节字符在回显里是同一串字节，
// 按字节比较既准确又与「显示宽度」无关。
func echoMatchAt(visible string, start int, line string) int {
	count := 0
	end := 0
	for i := start; i < len(visible) && count < len(line); i++ {
		c := visible[i]
		if c == '\n' {
			continue // 折行 / 重绘插入的换行不属于命令文本
		}
		if c != line[count] {
			return 0
		}
		count++
		end = i + 1
	}
	if count < len(line) {
		return 0 // 回显还没收完
	}
	if nl := strings.IndexByte(visible[end:], '\n'); nl >= 0 {
		return end + nl + 1
	}
	return end
}

// resetTimer 重置一个已启动的 Timer（先安全排空可能已触发的信号）。
func resetTimer(t *time.Timer, d time.Duration) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// decodeOutputChunk 解码 ssh.output 事件的 payload（{"sessionId":…,"data":"<base64>"}）。
func decodeOutputChunk(ev Event) []byte {
	if len(ev.Data) == 0 {
		return nil
	}
	var p struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(ev.Data, &p); err != nil || p.Data == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return nil
	}
	return b
}

// terminalText 增量累积终端可见文本：
//
//   - 逐块去掉 ANSI 转义序列（跨块的半个转义序列会被留到下一块，避免把 ESC 残片当文本）；
//   - \r\n 归一成 \n，孤立 \r（进度条回写）直接丢弃；
//   - 超过 limit 字节后停止累积并标记 dropped（返回里的 truncated 由 maxBytes 决定，
//     dropped 只用于极端刷屏场景下的内存保护）。
type terminalText struct {
	carry   string
	sb      strings.Builder
	rawLen  int
	limit   int
	dropped bool
}

// push 追加一块原始输出。
func (t *terminalText) push(chunk []byte) {
	if t.dropped {
		return
	}
	t.rawLen += len(chunk)
	if t.rawLen > t.limit {
		t.dropped = true
		return
	}
	s := t.carry + string(chunk)
	t.carry = ""
	if keep := incompleteEscapeTail(s); keep > 0 {
		t.carry = s[len(s)-keep:]
		s = s[:len(s)-keep]
	}
	t.sb.WriteString(cleanTerminalChunk(s))
}

// visible 当前可见文本。
func (t *terminalText) visible() string { return t.sb.String() }

// truncatedRaw 是否因为超过原始字节上限而丢掉了后续输出。
func (t *terminalText) truncatedRaw() bool { return t.dropped }

// cleanTerminalChunk 去掉一块文本里的转义序列并归一化换行。
func cleanTerminalChunk(s string) string {
	if s == "" {
		return ""
	}
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = reTerminalEscape.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r", "")
	}
	return s
}

// incompleteEscapeTail 返回 s 末尾「未完成的转义序列」长度（0 = 结尾是完整文本）。
//
// 判断依据：从最后一个 ESC 开始看，CSI 要等到终止字节（@-~），OSC 要等到 BEL 或 ESC \。
func incompleteEscapeTail(s string) int {
	idx := strings.LastIndexByte(s, 0x1b)
	if idx < 0 {
		return 0
	}
	tail := s[idx:]
	if len(tail) == 1 {
		return 1
	}
	switch tail[1] {
	case '[':
		for i := 2; i < len(tail); i++ {
			if tail[i] >= '@' && tail[i] <= '~' {
				return 0
			}
		}
		return len(tail)
	case ']':
		for i := 2; i < len(tail); i++ {
			if tail[i] == 0x07 {
				return 0
			}
			if tail[i] == 0x1b {
				if i+1 >= len(tail) {
					return len(tail) - i
				}
				if tail[i+1] == '\\' {
					return 0
				}
			}
		}
		return len(tail)
	}
	return 0
}

// wrapExitCommand 把命令包成「执行 + 打印退出码标记」。
//
// 只在 wrapExitMarker=true（AI 显式要求）时才会改动用户命令；返回里会带
// wrapped=true + wrappedCommand，如实说明实际执行了什么。
func wrapExitCommand(command string) string {
	return command + "; printf '\\n" + terminalMarkerPrefix + "%s\\n' \"$?\""
}

// reExitMarker 匹配退出码标记（取最后一次出现的）。
var reExitMarker = regexp.MustCompile(terminalMarkerPrefix + `([0-9]{1,3})`)

// reExitMarkerCode 判断「退出码标记是否已经出现」（wrapExitMarker 且未给 expect 时的默认等待目标）。
//
// 刻意要求后面跟数字：命令行回显里是字面量 __DING_EXIT__%s，不会命中；
// 只有 printf 真的执行完才会出现 __DING_EXIT__<数字>。
var reExitMarkerCode = regexp.MustCompile(terminalMarkerPrefix + `[0-9]+`)

// extractExitMarker 从可见文本里解析退出码（没有则 ok=false）。
func extractExitMarker(visible string) (int, bool) {
	all := reExitMarker.FindAllStringSubmatch(visible, -1)
	if len(all) == 0 {
		return 0, false
	}
	code, err := strconv.Atoi(all[len(all)-1][1])
	if err != nil {
		return 0, false
	}
	return code, true
}

// terminalBufferTail 读取终端缓冲区的尾部文本（页面内事实，走既有 OpTerminalBuffer op）。
//
// 用途：AI 判断「屏幕上现在是什么状态」（例如命令跑完了但 expect 写错了）。
// 实现细节：xterm 缓冲区尾部通常是若干空行（提示符下方的空白行），因此多读几行、
// 只保留非空行里的最后 terminalBufferTailLines 行，结果才有信息量。
// 拿不到时返回空串，不影响主结果。
func (s *Server) terminalBufferTail(ctx context.Context, id string) string {
	if s.opts.Handler == nil {
		return ""
	}
	raw, err := json.Marshal(map[string]any{
		"id": id, "from": "tail", "lines": terminalBufferTailLines * 4,
	})
	if err != nil {
		return ""
	}
	v, err := s.opts.Handler.Handle(ctx, OpTerminalBuffer, raw)
	if err != nil {
		return ""
	}
	m := appctlObj(v)
	txt := hostStr(m, "text")
	lines := strings.Split(txt, "\n")
	kept := make([]string, 0, terminalBufferTailLines)
	for i := len(lines) - 1; i >= 0 && len(kept) < terminalBufferTailLines; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		kept = append([]string{lines[i]}, kept...)
	}
	out, _ := truncateStringBytes(strings.Join(kept, "\n"), terminalBufferTailMaxBytes)
	return out
}

// ---- 资源（resources/list 与 resources/read 用）----

// terminalIDs 取当前全部终端 id（resources/list 用；页面不在时返回空切片）。
//
// 走既有 OpTerminals op（与 mcp.go 的 resources/list 同一来源），失败不影响其它资源。
func (s *Server) terminalIDs(ctx context.Context) []string {
	if s.opts.Handler == nil {
		return nil
	}
	v, err := s.opts.Handler.Handle(ctx, OpTerminals, nil)
	if err != nil || v == nil {
		return nil
	}
	return terminalIDsOf(v)
}

// terminalIDsOf 从 OpTerminals 的返回里抽出终端 id（替身 Handler 可能返回
// []any / []map[string]any / 别的形状，一律过一遍 JSON 归一）。
func terminalIDsOf(v any) []string {
	j := appctlJSON(v)
	arr, ok := j.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		m, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if id := hostStr(m, "clientId"); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// sftpResourceEntries 返回本域的资源条目（sftp://{id}/cwd 与 transfers://current）。
func sftpResourceEntries(terminalIDs []string) []any {
	out := []any{
		map[string]any{"uri": TransfersResourceURI, "name": "近期 SFTP 传输（进度事件快照）", "mimeType": "application/json"},
	}
	for _, id := range terminalIDs {
		if id == "" {
			continue
		}
		out = append(out, map[string]any{
			"uri":      SftpCwdResourceURI(id),
			"name":     "SFTP 当前目录 " + id,
			"mimeType": "application/json",
		})
	}
	return out
}

// SftpCwdResourceURI 会话的 SFTP 当前目录资源 uri。
func SftpCwdResourceURI(sessionID string) string { return "sftp://" + sessionID + "/cwd" }

// mcpReadSFTPResource 读取本域资源。
func (s *Server) mcpReadSFTPResource(ctx context.Context, uri string) (string, error) {
	switch {
	case uri == TransfersResourceURI:
		return sftpResourceJSON(s.sftpTransfers(map[string]any{"limit": 50}))
	case strings.HasPrefix(uri, "sftp://"):
		rest := strings.TrimPrefix(uri, "sftp://")
		id := strings.TrimSuffix(rest, "/cwd")
		if id == "" || id == rest {
			return "", fmt.Errorf("资源 uri 应为 sftp://{sessionId}/cwd，实际 %q", uri)
		}
		summary, err := s.sftpCwdSummary(ctx, id)
		if err != nil {
			return "", err
		}
		return sftpResourceJSON(summary)
	}
	return "", fmt.Errorf("未知资源: %s", uri)
}

// sftpResourceJSON 把资源内容序列化成 JSON 文本。
func sftpResourceJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// sftpCwdSummary 组装「当前 SFTP 路径 + 条目摘要」。
func (s *Server) sftpCwdSummary(ctx context.Context, sessionID string) (map[string]any, error) {
	path, err := s.sessionSftpPath(ctx, sessionID)
	if err != nil || path == "" {
		path = "."
	}
	listed, err := s.sftpList(ctx, map[string]any{"sessionId": sessionID, "path": path})
	if err != nil {
		return map[string]any{
			"sessionId": sessionID, "path": path, "error": err.Error(),
			"note": "读取目录失败（会话可能已断开，或不是 SSH 会话）",
		}, nil
	}
	m, _ := listed.(map[string]any)
	entries, _ := m["entries"].([]map[string]any)
	names := make([]any, 0, sftpCwdSummaryEntries)
	for i, e := range entries {
		if i >= sftpCwdSummaryEntries {
			break
		}
		names = append(names, map[string]any{
			"name": e["name"], "isDir": e["isDir"], "isSymlink": e["isSymlink"], "size": e["size"],
		})
	}
	return map[string]any{
		"sessionId":     sessionID,
		"path":          m["path"],
		"count":         m["count"],
		"entries":       names,
		"truncated":     true, // entries 只是摘要（最多 sftpCwdSummaryEntries 条）
		"fullEntryTool": OpSftpList,
	}, nil
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// tailString 返回字符串末尾最多 n 字节（不切断 UTF-8 字符）。
func tailString(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}
