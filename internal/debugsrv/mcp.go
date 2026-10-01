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
	"sync"
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

// mcpTool 一条 MCP 工具条目。
//
// OutputSchema / structured 是 M9 新增的「结构化输出」支持（MCP 2025-06-18）：
//   - OutputSchema 非空时会出现在 tools/list 与 GET /v1/tools 的结果里；
//   - structured 把文本结果投影成 tools/call 的 structuredContent（nil = 不提供）。
//
// 两者必须同时给出（hasStructuredOutput = 两者都非 nil），否则会出现
// 「声明了 outputSchema 却没有 structuredContent」的协议不一致。
//
// 注意：structured 是内部函数，不参与序列化（tools/list 里只有 outputSchema）。
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// OutputSchema 本工具结构化输出的 JSON Schema（可空）。见 outputObj / outProp。
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// structured 文本结果 → structuredContent 的投影函数（不可序列化）。
	structured func(data any) any
}

// outputObj 生成「结构化输出」的 JSON Schema（object 形状）。
//
// 与入参的 obj() 的区别：输出**允许**出现未声明的字段（工具返回里本来就有 note /
// truncated 这类附加信息），因此这里不设 additionalProperties:false ——
// 否则严格校验的客户端会把合法的返回判成不合法。
func outputObj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object"}
	if len(props) > 0 {
		m["properties"] = props
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// outProp 生成输出 schema 的属性：只声明类型（输出字段不需要「怎么填」的说明）。
func outProp(typ string) map[string]any { return map[string]any{"type": typ} }

// structuredIdentity 默认投影：文本内容本身就是结构化 JSON，原样作为 structuredContent。
func structuredIdentity(data any) any { return data }

// structuredTerminals 把 list_terminals 的**数组**结果包成对象。
//
// 为什么要包一层：MCP 的 structuredContent 必须是 JSON 对象，而 list_terminals 的文本
// 内容是数组（既有格式不能改，见 mcpCallTool 的 text()）—— 文本保持数组不变，
// 结构化输出给出 {count, terminals}。
func structuredTerminals(data any) any {
	if arr, ok := data.([]any); ok {
		return map[string]any{"count": len(arr), "terminals": arr}
	}
	return data
}

// 工具条目索引：structuredContent 投影与 /v1/tools 都要按名字查条目，
// 而 mcpTools(s) 每次都会重新组装（含描述拼接），因此这里只对**静态条目表**建一次索引。
var (
	mcpToolIndexOnce sync.Once
	mcpToolIndex     map[string]mcpTool
)

// toolEntryByName 按名字查工具条目（第二个返回值表示是否存在）。
func toolEntryByName(name string) (mcpTool, bool) {
	mcpToolIndexOnce.Do(func() {
		entries := mcpToolEntries()
		mcpToolIndex = make(map[string]mcpTool, len(entries))
		for _, e := range entries {
			mcpToolIndex[e.Name] = e
		}
	})
	e, ok := mcpToolIndex[name]
	return e, ok
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
//
// 描述末尾的「能力要求：…」由能力表 + 注册表自动生成（见 Server.capabilityRequirementText），
// 因此新增工具时只需要用 RegisterToolCap / RegisterToolConfirm 登记，不要手写这句话 ——
// 手写会与真实校验脱节（描述说能调、实际被拒）。
func mcpTools(s *Server) []mcpTool {
	return s.withCapabilityNotes(mcpToolEntries())
}

// mcpToolEntries 全部工具条目（静态表：不含「能力要求」描述行）。
//
// 与 mcpTools 分开的原因：结构化输出（toolEntryByName）与 GET /v1/tools 只需要静态条目，
// 不必每次都重新拼一遍描述文本；能力行由 withCapabilityNotes 按 Server 的能力快照追加。
func mcpToolEntries() []mcpTool {
	list := builtinToolEntries()
	// ---- UI 观测 / 实验域（ui.go 提供条目，集中在此追加）----
	// 25 个 ui.* 工具：观测（读，永远允许）+ 实验（写，需要 ui.write）能力位登记见 ui_register.go。
	list = append(list, uiToolEntries()...)
	// ---- 应用控制面（M7，appctl.go 提供条目，集中在此追加）----
	// 25 个 servers.* / credentials.* / tunnels.* / settings.* / app.* 工具：
	// 能力位与确认登记见 appctl_register.go（读类永远允许，写类 config.write / secrets.write / lifecycle）。
	list = append(list, appctlToolEntries()...)
	// ---- SFTP 与终端自动化（M8，sftp.go 提供条目，集中在此追加）----
	// 12 个 sftp.* / terminal.* 工具：读类永远允许（但远端内容全部走审计），
	// 写类需要 fs.remote.write + 两段式确认 + 路径白名单，terminal.run 只需 terminal.input。
	// 能力位与确认登记见 sftp_register.go。
	list = append(list, sftpToolEntries()...)
	// ---- sudo 凭证提权（M10，sudo.go 提供条目）----
	// 1 个工具：terminal.sudo —— 用应用里保存的密码执行 sudo -i，
	// 需要 sudo.credential + terminal.input（前者还要求 secrets.read）+ 两段式确认；
	// 能力位与确认登记在 sudo.go 的 init 里。
	list = append(list, sudoToolEntries()...)
	return list
}

// builtinToolEntries 地基工具（26 个）的条目表：能力位登记在内置表 toolCaps 里。
//
// 带 OutputSchema / structured 的条目见各条目的注释（M9 的结构化输出清单）。
func builtinToolEntries() []mcpTool {
	return []mcpTool{
		{Name: "app_state", Description: "读取应用状态：视图、标签、会话、隧道、设置与调试模式信息。",
			InputSchema: obj(nil),
			OutputSchema: outputObj(map[string]any{
				"debug": outProp("object"), "sessions": outProp("array"), "tunnels": outProp("array"),
				"settings": outProp("object"), "frontend": outProp("object"),
			}),
			structured: structuredIdentity},
		{Name: "list_terminals", Description: "列出所有终端及其诊断信息：rows/cols、缓冲区行数、baseY/viewportY、回滚是否可达、可见文本与 DOM 几何。",
			InputSchema: obj(nil),
			// 文本内容是数组（既有格式），结构化输出包成 {count, terminals} ——
			// structuredContent 必须是 JSON 对象，见 structuredTerminals。
			OutputSchema: outputObj(map[string]any{"count": outProp("number"), "terminals": outProp("array")}, "count", "terminals"),
			structured:   structuredTerminals},
		{Name: "read_terminal", Description: "读取某个终端缓冲区的文本行（含回滚）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("终端 id（即标签 clientId，见 list_terminals）"),
				"from":  strProp("起始位置：head | tail | 行号（默认 tail）"),
				"lines": intProp("读取行数（默认 200，最多 20000）"),
			}, "id"),
			OutputSchema: outputObj(map[string]any{
				"start": outProp("number"), "total": outProp("number"), "baseY": outProp("number"),
				"viewportY": outProp("number"), "count": outProp("number"),
				"lines": outProp("array"), "text": outProp("string"),
			}, "count", "lines", "text"),
			structured: structuredIdentity},
		{Name: "send_input", Description: "向会话写入数据（等价于在该终端里键入）。高频 / 可逆操作：只需要 terminal.input 能力位，不需要 confirm token。",
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
		{Name: "update_settings", Description: "部分更新应用设置（只传要改的字段），保存后立即生效。" +
			"破坏性 / 不可逆（会持久化到用户配置）：需要 config.write 能力位，且必须先 confirm.prepare 取 token。",
			InputSchema: obj(map[string]any{
				"settings": map[string]any{"type": "object", "description": "要合并进现有设置的字段"},
				"token":    strProp("两段式确认 token（本工具属于破坏性操作，必须先 confirm.prepare 取得）"),
			}, "settings")},
		{Name: "eval_js", Description: "在页面上下文执行 JS（默认关闭，需在设置 → 调试模式中开启「允许执行 JS」）。" +
			"高频 / 可逆（只改当前页面内存状态）：只需要 eval 能力位，不需要 confirm token。",
			InputSchema: obj(map[string]any{
				"js": strProp("要执行的 JavaScript 表达式或语句"),
			}, "js")},
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
		{Name: "clear_logs", Description: "清空日志文件（返回删除数量）。破坏性 / 不可逆（删掉的日志无法找回），需要 token。",
			InputSchema: obj(map[string]any{
				"token": strProp("两段式确认 token（本工具属于破坏性操作，必须先 confirm.prepare 取得）"),
			})},
		{Name: "set_log_options", Description: "调整日志设置以便定位问题：打开文件日志/控制台日志、把级别设为 debug、或打开 MCP/调试 API 调用日志。" +
			"只传要改的字段，保存后立即生效。level=debug 才会记录终端跟踪与更细的调试信息；" +
			"apiLogEnabled 打开后每次 HTTP/MCP 调用都会留一行 [api]（只记 path、不含 query，不会泄露 token）。" +
			"破坏性 / 不可逆（会持久化到用户配置）：需要 config.write 能力位，且必须先 confirm.prepare 取 token。",
			InputSchema: obj(map[string]any{
				"consoleEnabled": boolProp("控制台日志开关"),
				"fileEnabled":    boolProp("文件日志开关（落盘到 list_logs 的 dir）"),
				"level":          strProp("日志级别：debug | info | warn | error（debug 才记录终端跟踪）"),
				"apiLogEnabled":  boolProp("MCP/调试 API 调用日志开关（每次调用一行 [api]）"),
				"token":          strProp("两段式确认 token（本工具属于破坏性操作，必须先 confirm.prepare 取得）"),
			})},
		// ---- 权限 / 审计 / 确认（第四波：MCP 扩展的地基）----
		{Name: "app.permissions", Description: "读取当前能力位快照与每个能力的说明（含风险提示、是否需要两段式确认），" +
			"用于判断「我现在能做什么、要不要先 confirm.prepare」。返回里的 toolCount 是当前工具总数。",
			InputSchema: obj(nil),
			OutputSchema: outputObj(map[string]any{
				"caps": outProp("object"), "descriptions": outProp("object"), "confirmActions": outProp("array"),
				"readAlwaysAllowed": outProp("boolean"), "toolCount": outProp("number"), "note": outProp("string"),
			}, "caps", "toolCount"),
			structured: structuredIdentity},
		{Name: "app.recent_calls", Description: "读取 AI / 调试接口最近的调用审计记录（时间、来源、工具、能力、耗时、结果、已脱敏参数）。" +
			"用于自查「刚才那次操作到底做了什么、失败了还是成功了」；只保留最近 500 条。",
			InputSchema: obj(map[string]any{
				"limit":  intProp("返回条数（默认 50，最多 500）"),
				"cap":    strProp("按能力过滤，如 config.write（可选）"),
				"source": strProp("按来源过滤：mcp | http | ui（可选）"),
			}),
			OutputSchema: outputObj(map[string]any{
				"count": outProp("number"), "records": outProp("array"),
				"limit": outProp("number"), "ringMax": outProp("number"),
			}, "count", "records"),
			structured: structuredIdentity},
		{Name: "app.health", Description: "读取应用运行健康信息：版本、Go 版本、平台、已运行时长、数据/日志目录、调试服务状态（token 已打码）与能力位快照。",
			InputSchema: obj(nil),
			OutputSchema: outputObj(map[string]any{
				"appVersion": outProp("string"), "goVersion": outProp("string"), "os": outProp("string"),
				"arch": outProp("string"), "uptimeSec": outProp("number"), "dataDir": outProp("string"),
				"logDir": outProp("string"), "debug": outProp("object"), "capabilities": outProp("object"),
				"pendingConfirms": outProp("number"),
			}, "appVersion", "debug", "capabilities"),
			structured: structuredIdentity},
		{Name: "confirm.prepare", Description: "为**破坏性 / 不可逆**操作登记一次两段式确认，返回一次性 token 与中文摘要（请把摘要展示给用户后再决定是否 commit）。" +
			"约定：只有不可逆操作必须先 prepare 拿 token（再调 confirm.commit 或把 token 传给对应工具）；" +
			"可逆 / 高频操作（send_input、eval_js、界面写入）只受能力位约束，**不需要** token，" +
			"调用本工具登记它们会被拒绝。可确认的 action 见 initialize.instructions。",
			InputSchema: obj(map[string]any{
				"action":  strProp("动作名，如 servers.delete / sftp.remove / settings.update / logs.clear / app.quit / capability.enable"),
				"args":    map[string]any{"type": "object", "description": "该动作的参数（与真正调用写工具时传入的参数完全一致）"},
				"summary": strProp("给用户看的中文摘要（可选，缺省自动生成）"),
			}, "action")},
		{Name: "confirm.commit", Description: "校验并消费 confirm.prepare 返回的 token，然后执行该动作并返回结果。" +
			"token 一次性、绑定 action 与 args、默认 120 秒过期；token 无效/过期/已用掉/参数被改动都会返回中文错误。",
			InputSchema: obj(map[string]any{
				"token": strProp("confirm.prepare 返回的 token"),
			}, "token")},
	}
}

// confirmableActionsDoc 供 initialize.instructions 展示的可确认动作清单。
//
// 动态生成（不是写死的字符串）：后续三路用 RegisterToolConfirm 注册的动作会自动出现，
// 且与 confirmActionNeedsConfirm 的真实判断永远一致。
// 只列前 8 个 + 总数，保持 instructions 简短；完整清单在 app.permissions 的 confirmActions。
func confirmableActionsDoc() string {
	all := ConfirmableActions()
	shown := all
	more := ""
	if len(all) > 8 {
		shown = all[:8]
		more = fmt.Sprintf("（共 %d 个，完整清单见 app.permissions 的 confirmActions）", len(all))
	}
	return "只有**破坏性 / 不可逆**操作需要两段式确认（先 confirm.prepare 拿 token，" +
		"再 confirm.commit 或把 token 传给对应工具）：" +
		strings.Join(shown, " / ") + more + "。" +
		"可逆 / 高频操作（send_input、eval_js、界面写入）只受能力位约束，**不需要** token —— " +
		"能力位 = 「允不允许这类操作」，两段式 = 「不可逆操作前再想一次」。"
}

// withCapabilityNotes 给每个工具的描述末尾追加一行「能力要求：…」（由能力表 + 注册表生成）。
//
// 写类工具的 token 提示也在这里自动加：是否出现「需先 confirm.prepare」取决于该工具
// 自己的确认登记（内置表 / RegisterToolConfirm），而不是能力位 ——
// send_input 与 update_settings 同属写类，但只有后者需要 token。
func (s *Server) withCapabilityNotes(list []mcpTool) []mcpTool {
	for i := range list {
		list[i].Description = strings.TrimRight(list[i].Description, "\n") + "\n" + s.capabilityRequirementText(list[i].Name)
	}
	return list
}

// capabilitySnapshotText 生成"当前能力快照"的中文说明，用于 initialize.instructions。
//
// AI 需要在第一次握手时就知道「哪些能力开着」，否则会反复撞拒绝；
// 写成一行「开：… / 关：…」比给一个 JSON 更好读（initialize 的 instructions 是纯文本）。
func (s *Server) capabilitySnapshotText() string {
	snap := s.CapabilitySnapshot()
	var on, off []string
	for _, name := range sortedCapNames(snap) {
		c := Capability(name)
		if snap[c] {
			on = append(on, fmt.Sprintf("%s(%s)", CapabilityLabel(c), name))
		} else {
			off = append(off, fmt.Sprintf("%s(%s)", CapabilityLabel(c), name))
		}
	}
	txt := "当前能力快照 —— 开：" + joinOrNone(on) + "；关：" + joinOrNone(off) + "。"
	if len(off) > 0 {
		txt += "被关闭的能力对应的工具会返回中文拒绝原因（含怎么开启），不会报协议错误。"
	}
	return txt
}

// joinOrNone 用「、」连接，空列表返回「无」。
func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "无"
	}
	return strings.Join(items, "、")
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
			// ---- UI 观测域：view / 布局变化 → notifications/resources/updated（映射见 ui.go）----
			if note, isUI := uiResourceUpdatedNotification(ev); isUI {
				b, err := json.Marshal(note)
				if err != nil {
					continue
				}
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				flusher.Flush()
				continue
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
	// ---- M8：SFTP 相关事件 → 本域资源 ----
	// 传输进度（sftp.transfer）与目录变化（sftp.dir-updated）都映射到对应资源，
	// 客户端可据此失效缓存；SFTP 目录同步（sftp.sync-path）同样指向该会话的当前目录资源。
	switch ev.Topic {
	case TransferTopic:
		return TransfersResourceURI
	case "sftp.dir-updated", "sftp.sync-path":
		if ev.SessionID != "" {
			return SftpCwdResourceURI(ev.SessionID)
		}
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
				"发送命令用 send_input（\\r 结尾，不需要确认 token）；看输出用 read_terminal 或 recent_events；" +
				"screenshot 可拿到界面截图。eval_js 需在应用设置里单独开启（同样不需要 token）。\n" +
				"界面观测 / 实验（M5+M6）：ui.layout 看布局骨架与非预期重叠，ui.query / ui.styles / ui.hit / ui.tree 定位元素，" +
				"ui.screenshot / ui.elementShot 看局部截图，ui.snapshot + ui.diff 量化「改前 vs 改后」的像素与指纹差异，" +
				"ui.assert 做自动验收；实验用 ui.setToken / ui.injectCSS / ui.storePatch / ui.navigate / ui.click / ui.type（需要 ui.write 能力位，" +
				"可逆 / 高频，不需要 token；只影响当前运行实例，刷新即还原）。资源：ui://layout / ui://view / ui://console。\n" +
				"应用控制面（M7）：服务器 servers.list / servers.get / servers.create / servers.update / servers.delete / servers.duplicate / " +
				"servers.moveGroup / servers.export / servers.import，凭据 credentials.list（只看元信息）/ create / update / delete / attach，" +
				"隧道 tunnels.list / create / update / delete / start / stop，设置 settings.schema（列出全部字段与默认值）/ settings.reset，" +
				"内置命令 app.commands / app.command，生命周期 app.quit。读类默认脱敏（includeSecrets=true 需要 secrets.read，" +
				"返回里的 plaintextFields 标出明文字段，请勿落盘）；写类需要 config.write / secrets.write / lifecycle，" +
				"且除 app.command 的只读命令外都必须先 confirm.prepare 取 token。\n" +
				"权限：每个工具的描述末尾都有「能力要求」一行；能力位由用户在 设置 → 调试模式 → MCP 能力 控制，" +
				"被关闭的能力会让工具返回 isError 与中文原因（不会报协议错误）。用 app.permissions 查看当前快照，app.health 看运行信息。\n" +
				"SFTP 与终端自动化（M8）：读远端文件用 sftp.list / sftp.stat / sftp.read（encoding=text|base64，默认按 256KB 截断，" +
				"返回 sha256）；写用 sftp.write / sftp.mkdir / sftp.rename / sftp.remove / sftp.syncCwd（需要 fs.remote.write，" +
				"必须先 confirm.prepare 取 token，且路径要命中设置里的白名单前缀、不能含 ..；白名单留空 = 一律拒绝）；" +
				"传输看 sftp.transfers、取消用 sftp.cancel。跑命令用 terminal.run（发送 + 等 expect 命中或等输出静默，返回新增可见文本、" +
				"可选 wrapExitMarker 拿退出码；只需要 terminal.input，不需要 token），纯等待用 terminal.expect。" +
				"注意：同一会话上同一时刻只允许一个 terminal.run / terminal.expect，第二个调用会被直接拒绝（不排队）。" +
				"需要 root 时用 terminal.sudo（在**当前会话对应的服务器**上用**应用里保存的密码**执行 sudo -i：" +
				"需要用户开启 sudo.credential 能力位（另需 secrets.read）+ 每次 confirm.prepare(action=terminal.sudo)；" +
				"密码只经远端随机临时文件（chmod 600、用完即删）喂给 sudo -S，绝不进参数 / 返回值 / 日志 / 审计，" +
				"工具也不接受调用方传入密码；不支持本机终端与没有保存密码的服务器，白名单不含 /tmp 时直接拒绝）。" +
				"资源：sftp://{sessionId}/cwd / transfers://current。\n" +
				s.capabilitySnapshotText() + "\n" +
				"两段式确认：" + confirmableActionsDoc() + "\n" +
				"审计：每次调用都会记录（含脱敏参数），可用 app.recent_calls 自查；应用内「设置 → AI 记录」可实时查看。\n" +
				"工具目录：HTTP GET /v1/tools 可一次拿到全部工具的能力位 / 是否需要 token / 入参 schema / 是否有结构化输出；" +
				"声明了 outputSchema 的工具在 tools/call 里会额外返回 structuredContent（MCP 2025-06-18），" +
				"文本内容 content[0] 保持原样不变。",
		})

	case "ping":
		return ok(map[string]any{})

	case "tools/list":
		return ok(map[string]any{"tools": mcpTools(s)})

	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "tools/call 参数无效", err.Error())
		}
		// 能力位 + 两段式确认 + 审计统一在 mcpCallTool 里处理：
		// 这样「被拒」也会留下审计记录，且拒绝原因以 isError 的形式回给 AI（而不是 JSON-RPC 错误）。
		content, rec, err := s.mcpCallToolAudited(ctx, p.Name, p.Arguments)
		_ = rec
		if err != nil {
			return ok(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
				"isError": true,
			})
		}
		result := map[string]any{"content": content, "isError": false}
		// ---- M9：结构化输出（MCP 2025-06-18）----
		// 只对声明了 outputSchema（+ structured 投影）的工具追加；文本内容照旧保留在 content[0]，
		// 老客户端忽略 structuredContent 即可，向后兼容。
		if sc, has := structuredContentFor(p.Name, content); has {
			result["structuredContent"] = sc
		}
		return ok(result)

	case "resources/list":
		list := []any{
			map[string]any{"uri": "app://state", "name": "应用状态", "mimeType": "application/json"},
			map[string]any{"uri": "app://debug/info", "name": "调试模式信息", "mimeType": "application/json"},
			map[string]any{"uri": "app://servers", "name": "服务器列表（脱敏）", "mimeType": "application/json"},
			map[string]any{"uri": "app://logs", "name": "日志文件与日志设置", "mimeType": "application/json"},
			map[string]any{"uri": "app://logs/tail", "name": "最新日志末尾 200 行", "mimeType": "text/plain"},
			// ---- UI 观测域（ui.go）----
			map[string]any{"uri": "ui://layout", "name": "界面布局骨架（与 ui.layout 同源）", "mimeType": "application/json"},
			map[string]any{"uri": "ui://view", "name": "当前 view / 导航状态", "mimeType": "application/json"},
			map[string]any{"uri": "ui://console", "name": "前端控制台尾部文本", "mimeType": "text/plain"},
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
		// ---- SFTP 与终端自动化域（M8）：sftp://{sessionId}/cwd 与 transfers://current ----
		list = append(list, sftpResourceEntries(s.terminalIDs(ctx))...)
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

// mcpCallToolAudited 是 tools/call 的统一入口，负责三件事（顺序即执行顺序）：
//
//  1. 能力位校验（toolCaps）—— 被拒直接返回中文原因，AI 能读懂要开什么；
//  2. 两段式确认 —— 需要确认的工具若没带 token，提示先 confirm.prepare（**不执行**）；
//  3. 审计 —— 无论成功、被拒还是失败都落一条记录，并通过 debug.audit 事件推给前端。
//
// 返回的 AuditRecord 供调用方（测试 / 调试）观察，正常流程可以忽略。
func (s *Server) mcpCallToolAudited(ctx context.Context, name string, args map[string]any) ([]any, AuditRecord, error) {
	if _, known := ToolCapabilities(name); !known && !builtinToolsWithoutCaps[name] {
		// 未知工具名不进入审计的「能力」统计，但仍记录一条（防止 AI 拼错名字后无从排查）。
		if !isConfirmTool(name) {
			rec := newAuditRecorder(s.opts.Audits, s.opts.Hub, "mcp", name, CapRead, auditSafeArgs(name, args))
			err := fmt.Errorf("未知工具: %s（可用 tools/list 查看全部工具）", name)
			return nil, rec.finish(err, false, ""), err
		}
	}

	primary := primaryCapability(name)
	// 审计参数先过 auditSafeArgs（M7：明文密钥零泄漏）——credentials.create 的 content、
	// servers.import 的 json、confirm.prepare 里嵌套的 args 都必须打码后再进环形缓冲。
	// M8 再叠一层 sftpAuditSafeArgs：sftp.write 的 content / base64 动辄几百 KB，
	// 原样进（只有 500 条的）环形缓冲会把内存与 app.recent_calls 一起淹掉。
	rec := newAuditRecorder(s.opts.Audits, s.opts.Hub, "mcp", name, primary, sftpAuditSafeArgs(name, auditSafeArgs(name, args)))

	// 1) 能力位
	if err := s.checkToolCaps(name, argDetail(args)); err != nil {
		return nil, rec.finish(err, false, ""), err
	}

	// 1.5) 域内预检（M8：远端写路径的白名单 / 体积）—— 必须放在 token 校验**之前**：
	//      一次性 token 被消费后不能再用，若路径会被白名单拒绝就白烧一个 token，
	//      AI 只能重新 confirm.prepare。这里只做不依赖远端 I/O 就能判定的检查。
	if err := s.preflightToolArgs(name, args); err != nil {
		return nil, rec.finish(err, false, ""), err
	}

	// 2) 两段式确认：需要确认的工具必须携带 token（由 confirm.prepare 取得）。
	//    约定：缺 token 时**不执行**，只提示怎么拿 token。
	if action, need := ToolConfirmAction(name); need {
		// token 是既有工具的参数名；M7 的应用控制面工具按规格把同一个东西写成 confirm，
		// 因此这里两个名字都收（token 优先），保证 AI 用任意一个都能通过。
		token, _ := args["token"].(string)
		if strings.TrimSpace(token) == "" {
			token, _ = args["confirm"].(string)
		}
		token = strings.TrimSpace(token)
		if token == "" {
			err := errors.New(confirmRequiredMessage(action))
			return nil, rec.finish(err, false, ""), err
		}
		// token 必须绑定「除 token 外的参数」，与 prepare 时登记的一致。
		if _, err := s.takeConfirm(token, action, withoutToken(args)); err != nil {
			return nil, rec.finish(err, false, ""), err
		}
	}

	// 3) 执行（args 里的 token 字段对业务无意义，但要保留给 handler 的既有行为；
	//    既有 21 个工具的参数结构不允许改动，因此这里只把 token 剔除后再序列化）
	content, err := s.mcpCallTool(ctx, name, args)
	if err != nil {
		return nil, rec.finish(err, false, ""), err
	}
	// 只有成功且确实可撤销的才带撤销提示（失败的操作没有「改动前」可恢复）
	reversible, hint := auditRevertHint(name, args)
	return content, rec.finish(nil, reversible, hint), nil
}

// builtinToolsWithoutCaps 内置工具里**不登记能力位**的例外（读类，永远允许）。
//
// 为什么需要显式列出：screenshot 走的是「窗口截图」而不是调试 op（见 mcpCallTool），
// 因此它既不在内置能力表 toolCaps 里，也不能被当成「未知工具」。
// 单独抽成一份事实来源后，「工具条目 ↔ 能力登记」的一致性检查（register_test.go）与
// 未知工具判定（mcpCallToolAudited）都不必再写死工具名。
var builtinToolsWithoutCaps = map[string]bool{"screenshot": true}

// isConfirmTool 判断是否是「确认类」工具（它们的名字不在 toolCaps 里也算合法）。
func isConfirmTool(name string) bool {
	return name == "confirm.prepare" || name == "confirm.commit"
}

// primaryCapability 返回工具的主要能力位（用于审计记录的 cap 字段；读类工具为空）。
//
// 走 ToolCapabilities（注册表优先、内置表兜底）而不是直接查 toolCaps：
// 各域（ui.* / M7 的 servers.* 等）的能力位登记在自己的文件里，直接查内置表会让
// 这些工具的审计记录丢掉 cap 字段（AI 自查「刚才那次调用动了哪类权限」时就看不出）。
func primaryCapability(tool string) Capability {
	caps, _ := ToolCapabilities(tool)
	for _, c := range caps {
		if c != CapRead {
			return c
		}
	}
	return ""
}

// withoutToken 复制一份参数并剔除确认凭据字段（确认哈希不把 token / confirm 算进去）。
//
// 为什么两个名字都要剔：既有工具用 token，M7 的应用控制面工具按规格用 confirm，
// 两者都是「本次操作已获确认」的一次性凭据，不属于被确认的操作内容。
func withoutToken(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if k == "token" || k == "confirm" {
			continue
		}
		out[k] = v
	}
	return out
}

// argDetail 从参数里取出「写哪个路径 / 哪个对象」，用于能力被拒时的补充提示。
func argDetail(args map[string]any) string {
	for _, k := range []string{"path", "remotePath", "newPath", "remote", "dir", "id", "serverId"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// auditRevertHint 返回审计记录里的「可撤销」信息。
//
// 本波只对设置类改动（update_settings / set_log_options）提供撤销：
// 宿主在改动前存了一份设置快照，UI 点「撤销」即可写回。
// 其余动作（删服务器、清日志、退出应用…）无法可靠回滚，
// 因此返回 false —— 宁可在按钮上显示禁用并说明原因，也不假装支持。
func auditRevertHint(name string, args map[string]any) (bool, string) {
	switch name {
	case "update_settings", "set_log_options":
		return true, "可在「设置 → AI 记录」中点击「撤销」恢复为改动前的设置值"
	default:
		return false, ""
	}
}

// mcpCallTool 执行工具调用，返回 MCP 的 content 数组。
//
// 注意：能力位校验 / 两段式确认 / 审计都在 mcpCallToolAudited 里完成；
// 本函数只负责「工具名 → 调试 op」的翻译，保持与第三波完全一致的既有语义。
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

	// ---- 本波新增：权限 / 审计 / 确认 ----
	case "app.permissions":
		return text(s.permissionsView()), nil
	case "app.recent_calls":
		return text(s.recentCallsView(args)), nil
	case "app.health":
		return call(OpStatus, nil)
	case "confirm.prepare":
		return s.confirmPrepare(args)
	case "confirm.commit":
		return s.confirmCommit(ctx, args)
	}
	// ---- UI 观测 / 实验域：工具名即 op，统一交给 ui.go（含裁剪 / 换算 / 差异 / 落盘 / 图片内容）----
	if isUITool(name) {
		res, err := s.handleUIOp(ctx, name, args)
		if err != nil {
			return nil, err
		}
		return uiMCPContent(res)
	}
	// ---- 应用控制面（M7）：servers.* / credentials.* / tunnels.* / settings.* / app.* ----
	// 统一交给 appctl.go（含明文脱敏、includeSecrets 的 secrets.read 校验、体积限额、
	// app.command 的「按命令」权限判定）。
	if isAppctlTool(name) {
		return s.handleAppctl(ctx, name, args)
	}
	// ---- SFTP 与终端自动化（M8）----
	// sftp.*：入参校验 / 白名单 / 体积裁剪在本包完成，远端 I/O 交给宿主 op
	//（debug.go 用会话已有的 SFTP 客户端执行，不新建连接）。
	if IsSftpOp(name) {
		return s.handleSftpOp(ctx, name, args)
	}
	// terminal.run / terminal.expect：完全在本包实现（订阅 Hub 的 ssh.output 事件拿输出增量，
	// 发送命令仍沿用既有 OpTerminalInput → 前端桥），因此不在宿主新增任何终端逻辑。
	if IsTerminalAutomationOp(name) {
		return s.handleTerminalAutomationOp(ctx, name, args)
	}
	return nil, fmt.Errorf("未知工具: %s", name)
}

// structuredContentFor 从工具结果里取出结构化内容（MCP 2025-06-18 的 structuredContent）。
//
// 规则：
//   - 只有「声明了 outputSchema 且带 structured 投影」的工具才会返回（见 mcpTool 的说明）；
//   - 投影的输入是 content[0].text 解析回来的 JSON —— 文本内容必须继续存在且不变
//     （老客户端与既有测试都依赖它），structuredContent 只是同一份数据的结构化副本；
//   - 文本不是合法 JSON、或投影后不是 JSON 对象时，宁可不给结构化输出，也不猜。
func structuredContentFor(tool string, content []any) (any, bool) {
	entry, ok := toolEntryByName(tool)
	if !ok || entry.structured == nil || len(content) == 0 {
		return nil, false
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		return nil, false
	}
	text, ok := first["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil, false
	}
	var data any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil, false
	}
	out := entry.structured(data)
	if out == nil {
		return nil, false
	}
	// MCP 规范里 structuredContent 是 JSON 对象；投影写错（返回数组等）时直接不给，
	// 避免把不合规的形状回给客户端。
	if _, isObj := out.(map[string]any); !isObj {
		return nil, false
	}
	return out, true
}

// toolsHTTP 处理 GET /v1/tools：返回全部工具的目录。
//
// 与 MCP 的 tools/list 同源（同一份工具条目表 + 同一份能力登记），一次请求就能看清
// 「有哪些工具、各要什么能力、哪些必须先 confirm.prepare、有没有结构化输出」，
// 供脚本 / 人快速盘点，不必逐个调 tools/list 再拼接。
//
// 鉴权：仍走同一个 Bearer token（auth 中间件把整张 mux 包住）——工具目录里有描述与
// 能力位，属于控制面信息，不做「读类免鉴权」的例外。
func (s *Server) toolsHTTP(w http.ResponseWriter, r *http.Request) {
	tools := mcpTools(s)
	out := make([]any, 0, len(tools))
	structuredCount := 0
	for _, t := range tools {
		entry := toolCatalogEntry(t)
		if entry["hasStructuredOutput"] == true {
			structuredCount++
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":           len(out),
		"structuredCount": structuredCount,
		"tools":           out,
		"note": "与 MCP tools/list 同源。capability 为空数组表示读类（永远允许）；" +
			"needsToken=true 的工具必须先 confirm.prepare 取 token（见 confirmAction）；" +
			"hasStructuredOutput=true 的工具在 tools/call 里会额外返回 structuredContent（MCP 2025-06-18），" +
			"其形状见 outputSchema。",
	})
}

// toolCatalogEntry 组装工具目录里的一条记录（/v1/tools 用）。
func toolCatalogEntry(t mcpTool) map[string]any {
	action, needToken := ToolConfirmAction(t.Name)
	hasStructured := t.OutputSchema != nil && t.structured != nil
	entry := map[string]any{
		"name":                t.Name,
		"capability":          ToolCapabilityNames(t.Name),
		"needsToken":          needToken,
		"description":         t.Description,
		"inputSchema":         t.InputSchema,
		"hasStructuredOutput": hasStructured,
	}
	if needToken {
		entry["confirmAction"] = action
	}
	if hasStructured {
		entry["outputSchema"] = t.OutputSchema
	}
	return entry
}

// permissionsView 组装 app.permissions 的返回：能力快照 + 说明 + 白名单 + 工具能力表。
func (s *Server) permissionsView() map[string]any {
	snap := s.CapabilitySnapshot()
	caps := make(map[string]bool, len(snap))
	for _, n := range sortedCapNames(snap) {
		caps[n] = snap[Capability(n)]
	}
	return map[string]any{
		"caps":              caps,
		"descriptions":      CapabilityDescriptions(),
		"confirmActions":    ConfirmableActions(),
		"readAlwaysAllowed": true,
		// toolCount：当前工具总数（mcpTools 的实际长度，不写死数字）。
		// 用途：AI / 脚本判断「目录是否完整」，以及 M9 起 GET /v1/tools 的条数对照。
		"toolCount": len(mcpTools(s)),
		"note": "read 能力永远允许（敏感字段仍受 AllowSecrets 控制）；eval 沿用「允许执行 JS」开关；" +
			"写类工具被拒时会返回中文原因，按提示在 设置 → 调试模式 → MCP 能力 中开启对应能力，" +
			"或用 capability.enable（需两段式确认）开启。完整工具目录见 HTTP GET /v1/tools。",
	}
}

// recentCallsView 组装 app.recent_calls 的返回（审计记录）。
func (s *Server) recentCallsView(args map[string]any) map[string]any {
	if s.opts.Audits == nil {
		return map[string]any{"count": 0, "records": []any{}, "error": "审计未启用"}
	}
	limit := 50
	if v, has := args["limit"]; has {
		if f, isNum := v.(float64); isNum {
			limit = int(f)
		}
	}
	capName, _ := args["cap"].(string)
	source, _ := args["source"].(string)
	records := s.opts.Audits.Recent(limit, capName, source)
	return map[string]any{
		"count":   len(records),
		"records": records,
		"limit":   limit,
		"ringMax": AuditRingLimit,
	}
}

// confirmPrepare 实现 confirm.prepare：登记一次待确认操作并返回一次性 token。
func (s *Server) confirmPrepare(args map[string]any) ([]any, error) {
	text := func(v any) []any {
		b, _ := json.MarshalIndent(v, "", "  ")
		return []any{map[string]any{"type": "text", "text": string(b)}}
	}
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	if action == "" {
		return nil, errors.New("缺少 action：请传入要确认的动作名（见 initialize.instructions 的清单）")
	}
	if !confirmActionNeedsConfirm(action) {
		return nil, fmt.Errorf("%q 不是破坏性操作，不需要两段式确认：读取类与查询类操作可以直接调用对应工具（或 /v1 接口）", action)
	}
	if s.opts.Confirms == nil {
		return nil, errors.New("确认管理器未启用：请先在应用内开启调试模式")
	}
	inner, _ := args["args"].(map[string]any)
	summary, _ := args["summary"].(string)
	token, expiresAt := s.opts.Confirms.Prepare(action, inner, summary)
	pending, _ := s.opts.Confirms.Peek(token)
	return text(map[string]any{
		"token":     token,
		"expiresAt": expiresAt,
		"action":    action,
		"args":      pending.Args,
		"summary":   pending.Summary,
		"hint": "请先把 summary 展示给用户确认；确认后用 confirm.commit {token} 执行，" +
			"或把 token 作为 token 参数传给对应写工具。token 一次性、绑定 action+args，默认 120 秒过期。",
	}), nil
}

// confirmCommit 实现 confirm.commit：校验并消费 token，然后由宿主（Executor）执行动作。
func (s *Server) confirmCommit(ctx context.Context, args map[string]any) ([]any, error) {
	text := func(v any) []any {
		b, _ := json.MarshalIndent(v, "", "  ")
		return []any{map[string]any{"type": "text", "text": string(b)}}
	}
	token, _ := args["token"].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("缺少 token：请先调用 confirm.prepare 获取 token")
	}
	if s.opts.Confirms == nil {
		return nil, errors.New("确认管理器未启用：请先在应用内开启调试模式")
	}
	pending, ok := s.opts.Confirms.Peek(token)
	if !ok {
		return nil, errors.New("token 无效或已过期：请重新调用 confirm.prepare")
	}
	if s.opts.Executor == nil {
		return nil, errors.New("宿主未实现动作执行器（Executor）：该动作暂时只能通过应用界面执行")
	}
	// 先用登记时的参数校验并消费 token，避免「执行成功但 token 没被用掉」导致可重放。
	if _, err := s.opts.Confirms.Take(token, pending.Action, pending.Args); err != nil {
		return nil, err
	}
	result, err := s.opts.Executor.ExecuteAction(ctx, pending.Action, pending.Args)
	if err != nil {
		return nil, fmt.Errorf("动作 %s 执行失败：%w", pending.Action, err)
	}
	return text(map[string]any{
		"ok":      true,
		"action":  pending.Action,
		"args":    pending.Args,
		"summary": pending.Summary,
		"result":  result,
	}), nil
}

// mcpResourceMimeType 返回资源在 resources/read 响应里的 MIME 类型。
// 纯文本资源（日志尾部）用 text/plain；其余沿用既有行为（application/json）。
func mcpResourceMimeType(uri string) string {
	if uri == "app://logs/tail" || uri == "ui://console" {
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
	case strings.HasPrefix(uri, "ui://"):
		// UI 观测域资源（ui://layout / ui://view / ui://console）
		return s.mcpReadUIResource(ctx, uri)
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
	case uri == TransfersResourceURI, strings.HasPrefix(uri, "sftp://"):
		// ---- SFTP 与终端自动化域（M8）：transfers://current 与 sftp://{id}/cwd ----
		return s.mcpReadSFTPResource(ctx, uri)
	}
	return "", fmt.Errorf("未知资源: %s", uri)
}
