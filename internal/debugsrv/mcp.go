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
	"runtime"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
)

const (
	mcpProtocolVersion = "2025-06-18"
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
	Annotations map[string]any `json:"annotations,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
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
	if props == nil {
		props = map[string]any{}
	}
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
	list := s.withCapabilityNotes(mcpToolEntries())
	for i := range list {
		caps, _ := ToolCapabilities(list[i].Name)
		readOnly := true
		for _, c := range caps {
			if c != CapRead && c != CapTerminalRead && c != CapRemoteFSRead && c != CapUIInspect {
				readOnly = false
			}
		}
		switch list[i].Name {
		case "confirm.prepare", "confirm.commit", OpJobsCancel, OpSftpCancel:
			readOnly = false
		}
		_, destructive := ToolConfirmAction(list[i].Name)
		list[i].Annotations = map[string]any{"readOnlyHint": readOnly, "destructiveHint": destructive, "idempotentHint": readOnly, "openWorldHint": true}
		switch list[i].Name {
		case "screenshot", OpUIScreenshot, OpUIElementShot, OpUISnapshot:
			supported := runtime.GOOS == "windows"
			list[i].Meta = map[string]any{"platformSupported": supported, "supportedPlatforms": []string{"windows"}}
			list[i].Description += "\n窗口截图目前仅支持 Windows。"
			if !supported {
				list[i].Description += "当前平台不支持该工具。"
				list[i].Meta["unavailableReason"] = "当前平台尚未实现窗口截图"
			}
		}
		decorateToolDiscovery(&list[i])
	}
	return list
}

// mcpToolEntries 全部工具条目（静态表：不含「能力要求」描述行）。
//
// 与 mcpTools 分开的原因：结构化输出（toolEntryByName）与 GET /v1/tools 只需要静态条目，
// 不必每次都重新拼一遍描述文本；能力行由 withCapabilityNotes 按 Server 的能力快照追加。
func mcpToolEntries() []mcpTool {
	list := builtinToolEntries()
	// ---- UI 观测 / 实验域（ui.go 提供条目，集中在此追加）----
	// UI 观测与操作使用独立能力位；登记见 ui_register.go。
	list = append(list, uiToolEntries()...)
	// ---- 应用控制面（M7，appctl.go 提供条目，集中在此追加）----
	// 25 个 servers.* / credentials.* / tunnels.* / settings.* / app.* 工具：
	// 能力位与确认登记见 appctl_register.go。
	list = append(list, appctlToolEntries()...)
	// ---- SFTP 与终端自动化（M8，sftp.go 提供条目，集中在此追加）----
	// SFTP 与终端读取分别需要 fs.remote.read / terminal.read，全部经过审计；
	// 写类需要 fs.remote.write + 两段式确认 + 路径白名单，terminal.run 只需 terminal.input。
	// 能力位与确认登记见 sftp_register.go。
	list = append(list, sftpToolEntries()...)
	// ---- sudo 凭证提权（M10，sudo.go 提供条目）----
	// 1 个工具：terminal.sudo —— 用应用里保存的密码执行 sudo -i，
	// 需要 sudo.credential + terminal.input + credentials.use 及独立用户审批；
	// 能力位与确认登记在 sudo.go 的 init 里。
	list = append(list, sudoToolEntries()...)
	list = append(list, jobsToolEntries()...)
	list = append(list, preflightToolEntries()...)
	list = append(list, discoveryToolEntries()...)
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
		{Name: "send_input", Description: "向会话写入数据，等价于在终端键入，可执行任意命令并产生不可逆后果。需要 terminal.input 及目标会话授权；已授权范围内无需单次 token。不会自动识别命令是否安全。",
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
		{Name: "list_servers", Description: "搜索和分页列出已保存的服务器；默认脱敏，查询不匹配密码或私钥。",
			InputSchema: obj(map[string]any{
				"query":  strProp("名称/地址/用户/分组的大小写不敏感子串"),
				"group":  strProp("精确分组路径"),
				"offset": intProp("从 0 起的偏移"),
				"limit":  intProp("每页 1–500，默认 500"),
			})},
		{Name: "get_settings", Description: "读取应用设置（受「允许读取敏感数据」开关影响）。",
			InputSchema: obj(nil)},
		{Name: "update_settings", Description: "部分更新应用设置（只传要改的字段），保存后立即生效。" +
			"破坏性 / 不可逆（会持久化到用户配置）：需要 config.write 能力位，且必须先 confirm.prepare 取 token。",
			InputSchema: obj(map[string]any{
				"settings": map[string]any{"type": "object", "description": "要合并进现有设置的字段"},
				"token":    strProp("两段式确认 token（本工具属于破坏性操作，必须先 confirm.prepare 取得）"),
			}, "settings")},
		{Name: "eval_js", Description: "在页面上下文执行任意 JS；可能调用应用桥接接口并产生持久化或外部副作用。默认关闭，仅限本机可信所有者，同时需要 eval 与 automation.trusted。该能力接近完整应用控制权，需用户在应用内显式授权；不以单次 token 限制任意 JS。",
			InputSchema: obj(map[string]any{
				"js": strProp("要执行的 JavaScript 表达式或语句"),
			}, "js")},
		{Name: "screenshot", Description: "抓取应用窗口截图，返回 PNG 图片内容（受遮挡影响，窗口需可见）。",
			InputSchema: obj(nil)},
		{Name: "recent_events", Description: "读取最近的后端事件（会话输出/状态/进度/隧道）。提供 after 时按事件 seq 游标继续读取，并返回 nextCursor/gap/hasMore；最多保留近期 300 条，gap=true 表示历史已丢失，需读取当前资源重新同步。",
			InputSchema: obj(map[string]any{
				"limit":  intProp("返回条数（默认 50，最多 300）"),
				"after":  intProp("可选，读取 seq 大于此游标的事件；0 表示从现存历史起点开始"),
				"topics": strProp("可选，逗号分隔的 topic 过滤，如 ssh.output,ssh.status"),
			})},
		// 日志相关：先 list_logs 看日志在哪、有多少，再 read_logs 看具体内容，
		// 需要用户配合时 export_diagnostics 打包发给你。
		{Name: "list_logs", Description: "列出日志文件与当前日志设置（目录、大小、开关、级别、保留策略）。用于排查问题时先看日志在哪、有多少。",
			InputSchema: obj(nil)},
		{Name: "read_logs", Description: "读取应用日志（默认最新文件，按需过滤），用于问题定位。日志可能含终端跟踪内容，需 terminal.read；其中的已知 token/密码/私钥会打码。" +
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
		{Name: "confirm.prepare", Description: "请求独立的用户审批；本调用等待用户在可信系统窗口批准，批准后才返回一次性 token、operationId 和服务器生成的摘要。" +
			"MCP 提供的 summary 不能替代用户审批，也不会作为可信摘要。" +
			"无需单次确认的工具会被拒绝。可确认的 action 见 initialize.instructions。",
			InputSchema: obj(map[string]any{
				"action":  strProp("动作名，如 servers.delete / sftp.remove / settings.update / logs.clear / app.quit"),
				"tool":    strProp("精确工具名；建议提供，confirm.commit 将执行该工具并重新校验权限"),
				"args":    map[string]any{"type": "object", "description": "该动作的参数（与真正调用写工具时传入的参数完全一致）"},
				"summary": strProp("兼容旧客户端；审批摘要由服务器生成，本字段不参与授权"),
			}, "action")},
		{Name: "confirm.commit", Description: "校验并消费 confirm.prepare 返回的 token，然后执行该动作并返回结果。" +
			"token 一次性、绑定 action 与 args、默认 120 秒过期；token 无效/过期/已用掉/参数被改动都会返回中文错误。",
			InputSchema: obj(map[string]any{
				"token": strProp("confirm.prepare 返回的 token"),
			}, "token")},
		{Name: "confirm.status", Description: "按 operationId 查询本客户端最近的审批状态：approved / expired / consumed。状态不表示业务操作已经成功；不返回确认 token。最多保留 128 条，终态保留不超过一小时。prepare 等待原生审批，当前没有可轮询的 pending 状态。",
			InputSchema:  obj(map[string]any{"operationId": strProp("confirm.prepare 返回的 operationId")}, "operationId"),
			OutputSchema: outputObj(map[string]any{"operationId": outProp("string"), "status": outProp("string"), "expiresAt": outProp("number")}, "operationId", "status", "expiresAt"),
			structured:   structuredIdentity},
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
	return "以下工具要求独立用户审批：先 confirm.prepare，用户在可信系统窗口批准后才返回 token，" +
		"再 confirm.commit 或把 token 传给对应工具：" +
		strings.Join(shown, " / ") + more + "。" +
		"其余工具仍受能力、调用者身份与目标范围限制。终端输入可执行任意命令；任意 JS 和 DOM 操作仅供本机可信自动化使用。MCP 不能修改自身权限。"
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
	id            string
	topics        map[string]bool
	createdAt     time.Time
	ch            <-chan Event
	hubID         int
	version       string
	initialized   bool
	lastUsed      time.Time
	streaming     bool
	subscriptions map[string]bool
	loggingLevel  int
	owner         string
	toolProfile   string
}

// mcpSessionHeader MCP 会话头（2025-03-26 规范）。
const mcpSessionHeader = "Mcp-Session-Id"

// maskSessionID 给 MCP 会话 id 打码后写日志：会话 id 等价于推送通道的凭据，
// 拿到它就能接管事件订阅，因此日志里只保留前 4 位与长度（见 docs/logging.md §6）。
func maskSessionID(id string) string {
	if id == "" {
		return "(无)"
	}
	if len(id) <= 4 {
		return fmt.Sprintf("****(len=%d)", len(id))
	}
	return fmt.Sprintf("%s****(len=%d)", id[:4], len(id))
}

// mcpContentBytes 统计一次工具结果的文本 / 图片内容字节数，用于成功日志（只记大小，不记内容）。
func mcpContentBytes(content []any) int {
	total := 0
	for _, item := range content {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := m["text"].(string); ok {
			total += len(text)
		}
		if data, ok := m["data"].(string); ok {
			total += len(data)
		}
	}
	return total
}

func (s *Server) mcpNewSession(topics []string) (*mcpSession, error) {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	s.sweepSessionsLocked(time.Now())
	if len(s.mcpSessions) >= mcpMaxSessions {
		return nil, errors.New("MCP 会话数量达到上限，请结束空闲会话后重试")
	}
	id := newSessionID()
	var hubID int
	var ch <-chan Event
	if s.opts.Hub != nil {
		hubID, ch = s.opts.Hub.Subscribe(topics...)
	}
	set := map[string]bool{}
	for _, t := range topics {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	sess := &mcpSession{id: id, topics: set, createdAt: time.Now(), lastUsed: time.Now(), ch: ch, hubID: hubID, version: mcpProtocolVersion, subscriptions: map[string]bool{}}
	s.mcpSessions[id] = sess
	return sess, nil
}

func (s *Server) mcpGetSession(id string) *mcpSession {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	s.sweepSessionsLocked(time.Now())
	sess := s.mcpSessions[id]
	if sess != nil {
		sess.lastUsed = time.Now()
	}
	return sess
}

func (s *Server) sweepSessionsLocked(now time.Time) {
	for id, sess := range s.mcpSessions {
		if !sess.streaming && now.Sub(sess.lastUsed) > mcpSessionTTL {
			delete(s.mcpSessions, id)
			if s.opts.Hub != nil {
				s.opts.Hub.Unsubscribe(sess.hubID)
			}
		}
	}
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

func (s *Server) closeProtocolSessions() {
	s.mcpMu.Lock()
	for id, sess := range s.mcpSessions {
		delete(s.mcpSessions, id)
		if s.opts.Hub != nil {
			s.opts.Hub.Unsubscribe(sess.hubID)
		}
	}
	s.mcpMu.Unlock()
	s.rpcMu.Lock()
	for _, cancel := range s.rpcActive {
		cancel()
	}
	s.rpcMu.Unlock()
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
		sess := s.mcpGetSession(id)
		if id == "" || sess == nil || sess.owner != "" && sess.owner != CallerID(r.Context()) || !s.mcpDropSession(id) {
			logx.Warnf("[debugsrv/mcp] 结束会话失败: method=DELETE reason=未知或无会话")
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知或无会话"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		logx.Warnf("[debugsrv/mcp] 不支持的 HTTP 方法: method=%s path=/mcp", r.Method)
		w.Header().Set("Allow", "POST, GET, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "仅支持 POST / GET / DELETE"})
	}
}

func mcpSessionIDFrom(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(mcpSessionHeader)); v != "" {
		return v
	}
	return ""
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
	ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if ct != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "Content-Type 必须为 application/json"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpBodyLimit+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "读取请求体失败"})
		return
	}
	if len(body) > mcpBodyLimit {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "请求体超过 8MB 上限"})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErrorBody{Code: -32700, Message: "JSON 解析失败"}})
		return
	}
	if err := validateRPC(req); err != nil {
		writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErrorBody{Code: -32600, Message: err.Error()}})
		return
	}
	ctx := r.Context()
	profile, explicitProfile, profileErr := requestToolProfile(r)
	if profileErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": profileErr.Error()})
		return
	}
	sid := mcpSessionIDFrom(r)
	var sess *mcpSession
	if sid != "" {
		sess = s.mcpGetSession(sid)
		if sess == nil || sess.owner != "" && sess.owner != CallerID(ctx) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知或已过期会话"})
			return
		}
		s.mcpMu.Lock()
		v, ready, sessionProfile := sess.version, sess.initialized, sess.toolProfile
		s.mcpMu.Unlock()
		if sessionProfile != "" {
			if explicitProfile && profile != sessionProfile {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tool profile 与已初始化会话不一致；切换 profile 请建立新 MCP 会话"})
				return
			}
			profile = sessionProfile
		}
		if hv := r.Header.Get("MCP-Protocol-Version"); hv != "" && hv != v {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "协议版本与已协商版本不一致"})
			return
		}
		if !ready && req.Method != "notifications/initialized" && req.Method != "ping" && req.Method != "notifications/cancelled" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请先发送 notifications/initialized"})
			return
		}
		ctx = context.WithValue(ctx, protocolContextKey{}, v)
		ctx = context.WithValue(ctx, sessionContextKey{}, sess)
	}
	ctx = context.WithValue(ctx, toolProfileContextKey{}, profile)
	if hv := r.Header.Get("MCP-Protocol-Version"); hv != "" && !protocolSupported(hv) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不支持的 MCP-Protocol-Version"})
		return
	}
	if sid == "" && r.Header.Get("MCP-Protocol-Version") != "" {
		ctx = context.WithValue(ctx, protocolContextKey{}, r.Header.Get("MCP-Protocol-Version"))
	}
	if len(req.ID) == 0 {
		switch req.Method {
		case "notifications/initialized":
			if sess != nil {
				s.mcpMu.Lock()
				sess.initialized = true
				s.mcpMu.Unlock()
			}
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(req.Params, &p) == nil {
				s.rpcMu.Lock()
				cancel := s.rpcActive[CallerID(ctx)+"/"+sid+"/"+rpcRequestIDKey(p.RequestID)]
				s.rpcMu.Unlock()
				if cancel != nil {
					cancel()
				}
			}
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if req.Method == "tools/call" {
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) == nil {
			if p.Arguments == nil {
				p.Arguments = map[string]any{}
			}
			if entry, ok := toolEntryByName(p.Name); ok {
				if err := validateToolSchema(entry.InputSchema, p.Arguments, "arguments"); err != nil {
					writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcErrorBody{Code: -32602, Message: err.Error()}})
					return
				}
			}
		}
	}
	key := CallerID(ctx) + "/" + sid + "/" + rpcRequestIDKey(req.ID)
	ctx, cancel := context.WithCancel(ctx)
	s.rpcMu.Lock()
	if _, exists := s.rpcActive[key]; exists {
		s.rpcMu.Unlock()
		cancel()
		writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcErrorBody{Code: -32600, Message: "请求 id 已在执行"}})
		return
	}
	s.rpcActive[key] = cancel
	s.rpcMu.Unlock()
	defer func() { cancel(); s.rpcMu.Lock(); delete(s.rpcActive, key); s.rpcMu.Unlock() }()
	res := s.mcpDispatch(ctx, req)
	if req.Method == "initialize" && res.Error == nil {
		var topics []string
		if v := strings.TrimSpace(r.URL.Query().Get("topics")); v != "" {
			topics = strings.Split(v, ",")
		}
		next, err := s.mcpNewSession(topics)
		if err != nil {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": err.Error()})
			return
		}
		if result, ok := res.Result.(map[string]any); ok {
			if v, ok := result["protocolVersion"].(string); ok {
				next.version = v
			}
		}
		next.owner = CallerID(ctx)
		next.toolProfile = profile
		w.Header().Set(mcpSessionHeader, next.id)
	}
	writeJSON(w, http.StatusOK, res)
}

// mcpStream 建立 SSE 推送通道：把后端事件转成 JSON-RPC 通知推给客户端。
func (s *Server) mcpStream(w http.ResponseWriter, r *http.Request) {
	sess := s.mcpGetSession(mcpSessionIDFrom(r))
	if sess == nil || sess.owner != "" && sess.owner != CallerID(r.Context()) {
		logx.Warnf("[debugsrv/mcp] 拒绝建立推送通道: reason=未知或无会话")
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "未知或无会话：请先用 initialize 获取 " + mcpSessionHeader})
		return
	}
	s.mcpMu.Lock()
	if hv := r.Header.Get("MCP-Protocol-Version"); hv != "" && hv != sess.version {
		s.mcpMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "协议版本与已协商版本不一致"})
		return
	}
	if sess.streaming {
		s.mcpMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"error": "该会话已有活动推送连接"})
		return
	}
	sess.streaming = true
	s.mcpMu.Unlock()
	defer func() { s.mcpMu.Lock(); sess.streaming = false; sess.lastUsed = time.Now(); s.mcpMu.Unlock() }()
	flusher, ok := w.(http.Flusher)
	if !ok {
		logx.Errorf("[debugsrv/mcp] 无法建立推送通道: reason=当前环境不支持 SSE")
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

	start := time.Now()
	events := 0 // 本次连接已推送的通知数，断开时汇总
	logx.Infof("[debugsrv/mcp] 推送通道已建立: session=%s topics=%d", maskSessionID(sess.id), len(sess.topics))

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev, open := <-sess.ch:
			if !open {
				logx.Infof("[debugsrv/mcp] 推送通道关闭: session=%s events=%d elapsed=%s",
					maskSessionID(sess.id), events, time.Since(start))
				return
			}
			if !s.authorizeEvent(r.Context(), ev) {
				continue
			}
			uri := eventResourceURI(ev)
			if uiNote, isUI := uiResourceUpdatedNotification(ev); isUI {
				if params, ok := uiNote["params"].(map[string]any); ok {
					uri, _ = params["uri"].(string)
				}
			}
			s.mcpMu.Lock()
			subscribed := sess.subscriptions[uri]
			logLevel := sess.loggingLevel
			s.mcpMu.Unlock()
			if uri != "" && subscribed {
				note := map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated", "params": map[string]any{"uri": uri, "_meta": map[string]any{"seq": ev.Seq, "dropped": ev.Dropped}}}
				b, _ := json.Marshal(note)
				if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", ev.Seq, b); err != nil {
					return
				}
				flusher.Flush()
				events++
			}
			if ev.Topic == "ssh.status" {
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/list_changed"})
				if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", b); err != nil {
					return
				}
				flusher.Flush()
			}
			if logLevel > 1 {
				continue
			}

			note := map[string]any{
				"jsonrpc": "2.0",
				"method":  "notifications/message",
				"params": map[string]any{
					"level":  "info",
					"logger": "ding-ssh/" + ev.Topic,
					"data": map[string]any{
						"topic": ev.Topic,
						"seq":   ev.Seq, "dropped": ev.Dropped,
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
				logx.Debugf("[debugsrv/mcp] 通知序列化失败，已跳过: topic=%s err=%v", ev.Topic, err)
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", ev.Seq, b); err != nil {
				return
			}
			flusher.Flush()
			events++
			// 逐事件推送是热路径：采样限流，只记 topic 与字节数，不记事件内容。
			logx.DebugSampled(streamEventLog, "[debugsrv/stream] 推送 MCP 通知: topic=%s bytes=%d", ev.Topic, len(b))
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			logx.Infof("[debugsrv/mcp] 推送通道客户端断开: session=%s events=%d elapsed=%s",
				maskSessionID(sess.id), events, time.Since(start))
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
		logx.Infof("[debugsrv/mcp] 客户端初始化: requested=%q negotiated=%s server=%s", p.ProtocolVersion, version, mcpServerVersion)
		return ok(map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools":     map[string]any{"listChanged": false},
				"resources": map[string]any{"subscribe": true, "listChanged": true},
				"logging":   map[string]any{},
			},
			"serverInfo": map[string]any{"name": mcpServerName, "version": mcpServerVersion},
			"instructions": "ding-ssh 控制面。先用 app.health 检查环境，app.preflight(tool,args) 一次预检具体操作的权限与约束；tools.search 查找工具和推荐工作流。目录 profile=" + contextToolProfile(ctx) + "，只调整工具发现，不改变授权。使用 X-Ding-SSH-Tool-Profile: operations 可获得精简运维目录；默认 full 保持兼容。新连接默认观察模式，各类终端/远端文件/UI 读取分别受能力控制。" +
				"独立运维命令优先 exec.start，然后用 jobs.get/wait/output 查询；任务超时或连接丢失不能假定远端命令已停止。交互才用 terminal.run/expect；静默不等于执行完成。" +
				"持久化变更先 confirm.prepare（建议提供精确 tool/action/args），用户在独立系统窗口批准后才返回一次性 token，再调用原工具或 confirm.commit；MCP 不能修改自身授权。" +
				"credentials.use 允许应用内部使用保存凭据，secrets.read 另行控制明文导出；两者互不隐含。" +
				"SFTP 写路径必须命中全局及客户端范围，符号链接另校验；条件写入用 expectedHash，修改前先 sftp.patch.preview。任意 shell 可绕过 SFTP 工具路径策略，受限账号隔离请在远端配置。" +
				"ui.click/type/key/storePatch 与 eval_js 属于完全信任自动化，可能触发持久化宿主操作；不能承诺刷新还原。只读观测 ui.layout/query/styles/tree/console 要 ui.inspect。" +
				"服务端事件带 seq/gap，丢失时重新读取状态。HTTP GET /v1/tools 与 tools/list 同源。" + s.capabilitySnapshotText() + "两段式确认：" + confirmableActionsDoc(),
		})

	case "ping":
		logx.Debugf("[debugsrv/mcp] 收到 ping")
		return ok(map[string]any{})

	case "tools/list":
		tools := toolsForProfile(s, contextToolProfile(ctx))
		if protocolVersion(ctx) != "2025-06-18" {
			for i := range tools {
				tools[i].OutputSchema = nil
			}
		}
		logx.Debugf("[debugsrv/mcp] 返回工具清单: count=%d", len(tools))
		return ok(map[string]any{"tools": tools})

	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			logx.Warnf("[debugsrv/mcp] tools/call 参数无效: err=%v", err)
			return fail(-32602, "tools/call 参数无效", err.Error())
		}
		// 只记参数键名与 params 字节数，绝不记参数值正文（可能含终端输入 / 凭据）。
		if logx.DebugEnabled() {
			logx.Debugf("[debugsrv/mcp] 工具调用: tool=%s argKeys=%v paramsBytes=%d",
				p.Name, appctlSortedKeys(p.Arguments), len(req.Params))
		}
		// 能力位 + 两段式确认 + 审计统一在 mcpCallTool 里处理：
		// 这样「被拒」也会留下审计记录，且拒绝原因以 isError 的形式回给 AI（而不是 JSON-RPC 错误）。
		content, rec, err := s.mcpCallToolAudited(ctx, p.Name, p.Arguments)
		_ = rec
		if err != nil {
			return ok(map[string]any{
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
				"isError": true,
				"_meta":   toolErrorMeta(err),
			})
		}
		result := map[string]any{"content": content, "isError": false}
		logx.Debugf("[debugsrv/mcp] 工具返回: tool=%s items=%d bytes=%d", p.Name, len(content), mcpContentBytes(content))
		// ---- M9：结构化输出（MCP 2025-06-18）----
		// 只对声明了 outputSchema（+ structured 投影）的工具追加；文本内容照旧保留在 content[0]，
		// 老客户端忽略 structuredContent 即可，向后兼容。
		if sc, has := structuredContentFor(p.Name, content); has && protocolVersion(ctx) == "2025-06-18" {
			result["structuredContent"] = sc
		}
		return ok(result)

	case "resources/templates/list":
		return ok(map[string]any{"resourceTemplates": []any{
			map[string]any{"uriTemplate": "terminal://{id}/buffer", "name": "终端缓冲区", "mimeType": "text/plain"},
			map[string]any{"uriTemplate": "sftp://{sessionId}/cwd", "name": "SFTP 当前目录", "mimeType": "application/json"},
		}})
	case "resources/subscribe", "resources/unsubscribe":
		sess, _ := ctx.Value(sessionContextKey{}).(*mcpSession)
		if sess == nil {
			return fail(-32602, "资源订阅需要有效 MCP 会话", "")
		}
		var p struct {
			URI string `json:"uri"`
		}
		if json.Unmarshal(req.Params, &p) != nil || !validResourceURI(p.URI) {
			return fail(-32602, "资源 URI 无效", "")
		}
		if req.Method == "resources/subscribe" {
			if err := s.authorizeResource(ctx, p.URI); err != nil {
				return fail(-32000, "资源订阅被拒绝", err.Error())
			}
		}
		s.mcpMu.Lock()
		if req.Method == "resources/subscribe" {
			sess.subscriptions[p.URI] = true
		} else {
			delete(sess.subscriptions, p.URI)
		}
		s.mcpMu.Unlock()
		return ok(map[string]any{})
	case "logging/setLevel":
		sess, _ := ctx.Value(sessionContextKey{}).(*mcpSession)
		if sess == nil {
			return fail(-32602, "日志订阅需要有效 MCP 会话", "")
		}
		var p struct {
			Level string `json:"level"`
		}
		_ = json.Unmarshal(req.Params, &p)
		rank, known := map[string]int{"debug": 0, "info": 1, "notice": 2, "warning": 3, "error": 4, "critical": 5, "alert": 6, "emergency": 7}[p.Level]
		if !known {
			return fail(-32602, "未知日志级别", "")
		}
		s.mcpMu.Lock()
		sess.loggingLevel = rank
		s.mcpMu.Unlock()
		return ok(map[string]any{})
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
		logx.Debugf("[debugsrv/mcp] 返回资源清单: count=%d", len(list))
		return ok(map[string]any{"resources": list})

	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			logx.Warnf("[debugsrv/mcp] resources/read 参数无效: err=%v", err)
			return fail(-32602, "resources/read 参数无效", err.Error())
		}
		text, err := s.mcpReadResource(ctx, p.URI)
		if err != nil {
			logx.Warnf("[debugsrv/mcp] 读取资源失败: uri=%s err=%v", p.URI, err)
			return fail(-32602, err.Error(), p.URI)
		}
		logx.Debugf("[debugsrv/mcp] 读取资源完成: uri=%s bytes=%d", p.URI, len(text))
		return ok(map[string]any{"contents": []any{map[string]any{
			"uri": p.URI, "mimeType": mcpResourceMimeType(p.URI), "text": text,
		}}})

	case "prompts/list":
		logx.Debugf("[debugsrv/mcp] 返回提示词清单: count=0")
		return ok(map[string]any{"prompts": []any{}})
	}
	logx.Warnf("[debugsrv/mcp] 不支持的 JSON-RPC 方法: method=%s", req.Method)
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
	start := time.Now()
	if _, known := ToolCapabilities(name); !known && !builtinToolsWithoutCaps[name] {
		// 未知工具名不进入审计的「能力」统计，但仍记录一条（防止 AI 拼错名字后无从排查）。
		if !isConfirmTool(name) {
			logx.Warnf("[debugsrv/mcp] 未知工具调用: tool=%s", name)
			rec := newAuditRecorder(s.opts.Audits, s.opts.Hub, "mcp", name, CapRead, auditSafeArgs(name, args))
			rec.ClientID = CallerID(ctx)
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
	rec.ClientID = CallerID(ctx)
	rec.OperationID = "call_" + newSessionID()

	// Identity scopes and argument-dependent capabilities are checked together,
	// before confirmation consumption.
	if err := s.authorizeInvocation(ctx, name, args); err != nil {
		return nil, rec.finish(err, false, ""), err
	}

	// 1.5) 域内预检（M8：远端写路径的白名单 / 体积）—— 必须放在 token 校验**之前**：
	//      一次性 token 被消费后不能再用，若路径会被白名单拒绝就白烧一个 token，
	//      AI 只能重新 confirm.prepare。这里只做不依赖远端 I/O 就能判定的检查。
	if err := s.preflightToolArgs(name, args); err != nil {
		return nil, rec.finish(err, false, ""), err
	}

	// 1.6) sudo 域预检（M10 收尾）：cleanupOnly 是「超时 / 异常后的事后清理入口」，
	//      它的路径必须先过「形态 + 白名单」校验再消费一次性 token ——
	//      被拒时不烧 token，AI 不必重新 confirm.prepare（理由与上面的 M8 预检相同）。
	if err := s.preflightSudoArgs(name, args); err != nil {
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
			logx.Warnf("[debugsrv/confirm] 破坏性工具缺少确认 token: tool=%s action=%s", name, action)
			err := errors.New(confirmRequiredMessage(action))
			return nil, rec.finish(err, false, ""), err
		}
		if s.opts.Confirms != nil {
			if p, ok := s.opts.Confirms.Peek(token); ok && p.Tool != "" && p.Tool != name {
				logx.Warnf("[debugsrv/confirm] 确认 token 与工具不匹配: tool=%s tokenTool=%s action=%s", name, p.Tool, action)
				err := fmt.Errorf("token 绑定工具 %s，不能用于 %s", p.Tool, name)
				return nil, rec.finish(err, false, ""), err
			}
		}
		// token 必须绑定「除 token 外的参数」，与 prepare 时登记的一致。
		// 校验失败时只记工具与 action：Take 的错误文案里含规范化参数 JSON，
		// 其中可能有凭据 / 设置正文，不能整段写进日志。
		if err := s.checkConfirmCaller(ctx, token); err != nil {
			return nil, rec.finish(err, false, ""), err
		}
		if _, err := s.takeConfirm(token, action, withoutToken(args)); err != nil {
			// 具体失败原因（无效 / 过期 / 参数不一致）由 ConfirmManager.Take 记 WARN，
			// 这里只补工具上下文，避免同一次失败留下两条同级日志。
			logx.Debugf("[debugsrv/confirm] 确认 token 校验失败: tool=%s action=%s", name, action)
			return nil, rec.finish(err, false, ""), err
		}
	}

	// 3) 执行（args 里的 token 字段对业务无意义，但要保留给 handler 的既有行为；
	//    既有 21 个工具的参数结构不允许改动，因此这里只把 token 剔除后再序列化）
	content, err := s.mcpCallTool(ctx, name, args)
	if err != nil {
		logx.Errorf("[debugsrv/mcp] 工具调用失败: tool=%s elapsed=%s err=%v", name, time.Since(start), err)
		return nil, rec.finish(err, false, ""), err
	}
	logx.Debugf("[debugsrv/mcp] 工具调用成功: tool=%s elapsed=%s items=%d bytes=%d",
		name, time.Since(start), len(content), mcpContentBytes(content))
	// 只有成功且确实可撤销的才带撤销提示（失败的操作没有「改动前」可恢复）
	reversible, hint := auditRevertHint(name, args)
	if reversible {
		// 「撤销」入口在前端 AI 记录页：后端只负责把记录标记成可撤销并给出提示。
		logx.Debugf("[debugsrv/audit] 工具调用可撤销，已附加撤销提示: tool=%s", name)
	}
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
	return name == "confirm.prepare" || name == "confirm.commit" || name == "confirm.status"
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
	case OpAppPreflight:
		return s.handlePreflight(ctx, args)
	case OpToolsSearch:
		v, err := s.searchTools(args)
		if err != nil {
			return nil, err
		}
		return text(v), nil
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
		v, err := s.appctlServersList(ctx, h, args)
		if err != nil {
			return nil, err
		}
		return text(v), nil
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
		if _, has := args["after"]; has {
			after, err := jobArgInt(args, "after", 0, 0, int(^uint(0)>>1))
			if err != nil {
				return nil, err
			}
			return text(s.eventPageContext(ctx, uint64(after), limit, topics...)), nil
		}
		events := s.opts.Hub.Recent(limit, topics...)
		filtered := events[:0]
		for _, ev := range events {
			if s.authorizeEvent(ctx, ev) {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
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
		start := time.Now()
		png, err := captureWindowPNG()
		if err != nil {
			return nil, err
		}
		logx.Debugf("[debugsrv/mcp] 截图工具完成: bytes=%d elapsed=%s", len(png), time.Since(start))
		return []any{map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(png),
			"mimeType": "image/png",
		}}, nil

	// ---- 本波新增：权限 / 审计 / 确认 ----
	case "app.permissions":
		return text(s.permissionsView(ctx)), nil
	case "app.recent_calls":
		return text(s.recentCallsView(args)), nil
	case "app.health":
		return call(OpStatus, nil)
	case "confirm.prepare":
		return s.confirmPrepareContext(ctx, args)
	case "confirm.commit":
		return s.confirmCommit(ctx, args)
	case "confirm.status":
		v, err := s.confirmationStatus(ctx, appctlString(args, "operationId"))
		if err != nil {
			return nil, err
		}
		return text(v), nil
	}
	if isJobsTool(name) {
		return s.handleJobsTool(ctx, name, args)
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
	profile, _, err := requestToolProfile(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	tools := toolsForProfile(s, profile)
	out := make([]any, 0, len(tools))
	structuredCount := 0
	for _, t := range tools {
		entry := toolCatalogEntry(t)
		if entry["hasStructuredOutput"] == true {
			structuredCount++
		}
		out = append(out, entry)
	}
	logx.Debugf("[debugsrv/mcp] 返回工具目录: count=%d structured=%d", len(out), structuredCount)
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":         profile,
		"count":           len(out),
		"structuredCount": structuredCount,
		"tools":           out,
		"note": "与 MCP tools/list 同源。capability 为空数组表示仅需基础 read，但仍受身份和目标范围限制；" +
			"needsToken=true 的工具必须先 confirm.prepare 并通过独立用户审批才会获得 token（见 confirmAction）；" +
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
	if conditions := conditionalToolCapabilities(t.Name); len(conditions) > 0 {
		entry["conditionalCapabilities"] = conditions
		entry["capabilityMode"] = "conditional"
		entry["capabilitySemantics"] = "The legacy capability list is a conservative union; use conditionalCapabilities for argument-specific requirements."
	}
	if len(t.Meta) > 0 {
		entry["_meta"] = t.Meta
	}
	if needToken {
		entry["confirmAction"] = action
	}
	entry["platformSupported"] = true
	switch t.Name {
	case "screenshot", OpUIScreenshot, OpUIElementShot, OpUISnapshot:
		entry["platformSupported"] = runtime.GOOS == "windows"
		if runtime.GOOS != "windows" {
			entry["unavailableReason"] = "当前平台尚未实现窗口截图"
		}
	}
	if hasStructured {
		entry["outputSchema"] = t.OutputSchema
	}
	return entry
}

// permissionsView 组装 app.permissions 的返回：能力快照 + 说明 + 白名单 + 工具能力表。
func (s *Server) permissionsView(ctx context.Context) map[string]any {
	snap := s.CapabilitySnapshot()
	caps := make(map[string]bool, len(snap))
	global := make(map[string]bool, len(snap))
	p, restricted := ctx.Value(callerKey{}).(ClientPolicy)
	restricted = restricted && p.ID != "local-owner"
	enabled := 0
	for _, n := range sortedCapNames(snap) {
		global[n] = snap[Capability(n)]
		effective := global[n]
		if restricted && Capability(n) != CapRead {
			effective = effective && hasString(p.Capabilities, n) && s.opts.Clients.Active(p.ID)
		}
		caps[n] = effective
		if effective {
			enabled++
		}
	}
	logx.Debugf("[debugsrv/mcp] 返回权限快照: caps=%d enabled=%d", len(caps), enabled)
	return map[string]any{
		"caps":                  caps,
		"effectiveCapabilities": caps,
		"globalCapabilities":    global,
		"clientId":              CallerID(ctx),
		"scopeLimited":          restricted && !p.AllTargets,
		"descriptions":          CapabilityDescriptions(),
		"confirmActions":        ConfirmableActions(),
		"readAlwaysAllowed":     true,
		// toolCount：当前工具总数（mcpTools 的实际长度，不写死数字）。
		// 用途：AI / 脚本判断「目录是否完整」，以及 M9 起 GET /v1/tools 的条数对照。
		"toolCount": len(mcpTools(s)),
		"note": "caps / effectiveCapabilities 是全局开关与当前身份授权的交集；globalCapabilities 仅表示全局开关。拥有能力仍须满足服务器/会话、路径范围和有效期。基础 read 不包含终端、远端文件、界面内容或明文凭据读取。" +
			"权限只能由用户在应用内修改；MCP 无权扩展自身授权。需确认的操作由独立用户审批产生一次性 token。完整工具目录见 HTTP GET /v1/tools。",
	}
}

// recentCallsView 组装 app.recent_calls 的返回（审计记录）。
func (s *Server) recentCallsView(args map[string]any) map[string]any {
	if s.opts.Audits == nil {
		logx.Warnf("[debugsrv/audit] 读取最近调用失败: reason=审计未启用")
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
	logx.Debugf("[debugsrv/audit] 读取最近调用: limit=%d cap=%s source=%s count=%d", limit, capName, source, len(records))
	return map[string]any{
		"count":   len(records),
		"records": records,
		"limit":   limit,
		"ringMax": AuditRingLimit,
	}
}

// confirmPrepare 实现 confirm.prepare：登记一次待确认操作并返回一次性 token。
func (s *Server) confirmPrepare(args map[string]any) ([]any, error) {
	return s.confirmPrepareContext(context.Background(), args)
}
func (s *Server) confirmPrepareContext(ctx context.Context, args map[string]any) ([]any, error) {
	text := func(v any) []any {
		b, _ := json.MarshalIndent(v, "", "  ")
		return []any{map[string]any{"type": "text", "text": string(b)}}
	}
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	if action == "" {
		logx.Warnf("[debugsrv/confirm] confirm.prepare 缺少 action")
		return nil, errors.New("缺少 action：请传入要确认的动作名（见 initialize.instructions 的清单）")
	}
	if !confirmActionNeedsConfirm(action) {
		logx.Warnf("[debugsrv/confirm] confirm.prepare 被拒: action=%s reason=该动作不需要确认", action)
		return nil, fmt.Errorf("%q 不是破坏性操作，不需要两段式确认：读取类与查询类操作可以直接调用对应工具（或 /v1 接口）", action)
	}
	if s.opts.Confirms == nil {
		logx.Warnf("[debugsrv/confirm] confirm.prepare 被拒: action=%s reason=确认管理器未启用", action)
		return nil, errors.New("确认管理器未启用：请先在应用内开启调试模式")
	}
	tool, _ := args["tool"].(string)
	tool = strings.TrimSpace(tool)
	if tool != "" {
		if a, need := ToolConfirmAction(tool); !need || a != action {
			logx.Warnf("[debugsrv/confirm] confirm.prepare 被拒: tool=%s action=%s reason=工具与动作不匹配", tool, action)
			return nil, fmt.Errorf("tool %q 与 action %q 不匹配", tool, action)
		}
	}
	inner, _ := args["args"].(map[string]any)
	summary, _ := args["summary"].(string)
	_ = summary // Approval summaries are generated by the server.
	operationID, token, expiresAt, err := s.approveOperation(ctx, action, tool, inner)
	if err != nil {
		return nil, err
	}
	pending, _ := s.opts.Confirms.Peek(token)
	return text(map[string]any{
		"token":       token,
		"operationId": operationID,
		"status":      "approved",
		"expiresAt":   expiresAt,
		"tool":        tool,
		"action":      action,
		"args":        SafeProjection(pending.Args),
		"summary":     pending.Summary,
		"hint": "用户已通过独立审批通道批准；使用 confirm.commit {token} 执行，" +
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
		logx.Warnf("[debugsrv/confirm] confirm.commit 缺少 token")
		return nil, errors.New("缺少 token：请先调用 confirm.prepare 获取 token")
	}
	if s.opts.Confirms == nil {
		logx.Warnf("[debugsrv/confirm] confirm.commit 被拒: reason=确认管理器未启用")
		return nil, errors.New("确认管理器未启用：请先在应用内开启调试模式")
	}
	if err := s.checkConfirmCaller(ctx, token); err != nil {
		return nil, err
	}
	pending, ok := s.opts.Confirms.Peek(token)
	if !ok {
		logx.Warnf("[debugsrv/confirm] confirm.commit 被拒: reason=token 无效或已过期")
		return nil, errors.New("token 无效或已过期：请重新调用 confirm.prepare")
	}
	if err := rejectSecurityPatch(pending.Action, pending.Args); err != nil {
		return nil, err
	}
	if pending.Tool == "" {
		if err := s.authorizeInvocation(ctx, pending.Action, pending.Args); err != nil {
			return nil, err
		}
	}
	logx.Debugf("[debugsrv/confirm] confirm.commit 执行: action=%s tool=%s", pending.Action, pending.Tool)
	if pending.Tool != "" {
		if action, needed := ToolConfirmAction(pending.Tool); !needed || action != pending.Action {
			return nil, errors.New("确认动作与工具策略不一致，请重新准备操作")
		}
		callArgs := make(map[string]any, len(pending.Args)+1)
		for k, v := range pending.Args {
			callArgs[k] = v
		}
		callArgs["token"] = token
		result, _, err := s.mcpCallToolAudited(ctx, pending.Tool, callArgs)
		return result, err
	}
	// These actions are shared by multiple tools or lack a generic executor.
	// Reject before consuming the token; callers can still invoke the exact tool.
	switch pending.Action {
	case "servers.create", "servers.update", "sftp.write", "terminal.sudo", "app.reload", "sftp.upload":
		logx.Warnf("[debugsrv/confirm] confirm.commit 被拒: action=%s reason=该动作需要精确工具", pending.Action)
		return nil, fmt.Errorf("动作 %s 需要精确工具：请直接调用原工具并携带 token，或重新 prepare 时提供 tool", pending.Action)
	}
	if cap, ok := CapabilityOfAction(pending.Action); ok {
		if err := s.allowCap(cap, pending.Action); err != nil {
			return nil, err
		}
	}
	if s.opts.Executor == nil {
		logx.Warnf("[debugsrv/confirm] confirm.commit 被拒: action=%s reason=宿主未实现动作执行器", pending.Action)
		return nil, errors.New("宿主未实现动作执行器（Executor）：该动作暂时只能通过应用界面执行")
	}
	// 先用登记时的参数校验并消费 token，避免「执行成功但 token 没被用掉」导致可重放。
	// 失败原因可能含规范化参数 JSON（内有凭据），因此只记 action，不记 err 文本。
	if _, err := s.takeConfirm(token, pending.Action, pending.Args); err != nil {
		// 原因由 Take 记 WARN，这里只留 DEBUG 上下文（避免重复）。
		logx.Debugf("[debugsrv/confirm] confirm.commit 校验失败: action=%s", pending.Action)
		return nil, err
	}
	start := time.Now()
	result, err := s.opts.Executor.ExecuteAction(ctx, pending.Action, pending.Args)
	if err != nil {
		logx.Errorf("[debugsrv/confirm] 已确认动作执行失败: action=%s elapsed=%s err=%v", pending.Action, time.Since(start), err)
		return nil, fmt.Errorf("动作 %s 执行失败：%w", pending.Action, err)
	}
	logx.Infof("[debugsrv/confirm] 已确认动作执行成功: action=%s elapsed=%s", pending.Action, time.Since(start))
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

// authorizeResource applies the same identity and capability checks to reads
// and subscriptions. A transport session never grants access to its resources.
func (s *Server) authorizeResource(ctx context.Context, uri string) error {
	if !validResourceURI(uri) {
		return fmt.Errorf("未知资源: %s", uri)
	}
	tool := "app_state"
	args := map[string]any{}
	switch {
	case strings.HasPrefix(uri, "terminal://"):
		tool = "read_terminal"
		args["id"] = strings.TrimSuffix(strings.TrimPrefix(uri, "terminal://"), "/buffer")
	case strings.HasPrefix(uri, "sftp://"):
		tool = "sftp.list"
		args["sessionId"] = strings.TrimSuffix(strings.TrimPrefix(uri, "sftp://"), "/cwd")
		// The resource follows the panel's current directory. Resolve that
		// directory for the policy check rather than inventing an unrelated root.
		if IsRestrictedCaller(ctx) {
			cwd, err := s.sessionSftpPath(ctx, appctlString(args, "sessionId"))
			if err != nil || cwd == "" {
				return errors.New("无法确定 SFTP 当前目录，请使用 sftp.list 指定获授权的绝对路径")
			}
			args["path"] = cwd
		}
	case strings.HasPrefix(uri, "ui://"):
		tool = OpUILayout
	case uri == "app://servers":
		tool = "list_servers"
	case strings.HasPrefix(uri, "app://logs"):
		tool = "read_logs"
	case uri == TransfersResourceURI:
		tool = "sftp.transfers"
	}
	return s.authorizeInvocation(ctx, tool, args)
}

// mcpReadResource 读取资源内容（JSON 文本）。
func (s *Server) mcpReadResource(ctx context.Context, uri string) (string, error) {
	if err := s.authorizeResource(ctx, uri); err != nil {
		return "", err
	}

	h := s.opts.Handler
	if h == nil {
		logx.Warnf("[debugsrv/mcp] 读取资源失败: uri=%s reason=调试服务未就绪", uri)
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
		return render(s.appctlServersList(ctx, h, nil))
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
