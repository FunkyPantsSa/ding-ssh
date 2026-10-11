package debugsrv

// M7 应用控制面：让 AI 管理服务器 / 凭据 / 隧道 / 设置 / 内置命令 / 生命周期。
//
// # 分层（谁负责什么）
//
//   - **本文件（debugsrv）**：工具条目与入参校验、能力位与两段式确认的**补充判定**
//     （框架只管到「工具级」，写工具的常规确认仍由 caps.go/confirm.go 的注册表负责）、
//     明文密钥脱敏、体积限额与截断标记；
//   - **宿主（debug.go）**：每个 op 落到 app.go 的既有绑定（GetServers / SaveServer /
//     DeleteServer / SaveCredential / StartTunnel / SaveSettings …），**不直接操作 SQLite**；
//   - **前端桥（frontend/src/debug/bridge.ts）**：只有「页面内才知道」的东西才走桥
//     （内置命令面板的命令列表与执行、会话标签对应的服务器节点）。
//
// # 三条硬约束（与任务规格一致）
//
//  1. 读写分权：读类工具注册 CapRead（永远允许）；写类注册 CapConfigWrite / CapSecretsWrite /
//     CapLifecycle，破坏性动作另注册 RegisterToolConfirm（action 名 = 规格给出的「域.动词」）。
//  2. **明文密钥零泄漏**：审计参数过 auditSafeArgs（把 password / content 等键整体替换为 ***）、
//     返回值只回 hasContent / 字段名、错误信息只提字段名不提值；includeSecrets 的结果在
//     description 与返回里都显式警告「含明文，请勿落盘」。
//  3. 体积有界：列表有条数上限、导出有字节上限并带 truncated 标记、文本内容有兜底截断。
//
// 注意：本文件不 import models / store —— 宿主返回值的字段名以 JSON 为准（appctlSecretFields），
// 这样 debugsrv 与宿主的数据结构保持解耦（与 ui.go 的做法一致）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---- 限额（所有输出都必须体积有界，见 ui.go 的同类常量）----

const (
	// appctlMaxListItems 单个列表类工具最多返回的条目数（服务器 / 凭据 / 隧道 / 命令）。
	appctlMaxListItems = 500
	// appctlMaxExportBytes servers.export 的 JSON 文本上限（超出后按整条服务器丢弃并标注）。
	appctlMaxExportBytes = 256 << 10
	// appctlMaxImportBytes servers.import 接受的最大入参长度（防止一次灌进几十 MB）。
	appctlMaxImportBytes = 2 << 20
	// appctlMaxTextBytes 工具文本内容的兜底上限（一般不会触发，触发时带截断标记）。
	appctlMaxTextBytes = 512 << 10
	// appctlMaxSchemaFields settings.schema 最多返回的字段数。
	appctlMaxSchemaFields = 400
	// appctlMaxImportErrors 导入校验失败时最多回报几条具体错误。
	appctlMaxImportErrors = 3
)

// appctlSecretFields 服务器节点 / 凭据里可能出现**明文密钥**的字段名。
//
// 与 audit.go 的 auditSensitiveKeys 保持同一思路：宁可多打码也不漏。
// 注意 content 也在列表里 —— 凭据写入工具用 content 传明文（密码或私钥内容）。
var appctlSecretFields = []string{"password", "keyContent", "privateKey", "passphrase", "secret", "content"}

// appctlText 把结果组装成 MCP 的 content（文本 JSON）。
func appctlText(v any) ([]any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化结果失败: %w", err)
	}
	text := string(b)
	if len(text) > appctlMaxTextBytes {
		text = text[:appctlMaxTextBytes] +
			fmt.Sprintf("\n…（结果超过 %d 字节上限，已截断；请用更小的 limit / 更精确的 id 重试）", appctlMaxTextBytes)
	}
	return []any{map[string]any{"type": "text", "text": text}}, nil
}

// ---- 审计参数脱敏（M7 的「明文密钥零泄漏」闸门，被 mcp.go 调用）----

// auditSafeArgs 返回一份「可以写进审计」的参数副本。
//
// 为什么必须有它：审计记录会进内存环形缓冲，被前端「AI 记录」页签与 app.recent_calls 读出来；
// 而 MCP 的参数是原样带进来的（credentials.create 的 content 就是明文密码 / 私钥），
// audit.go 的敏感键名单里没有 content，`{"content":"明文"}` 会被原样记下 —— 这是真实的泄漏路径。
// 因此这里在**进入审计之前**先把敏感键整体替换成 ***，并递归处理嵌套对象 / 数组
// （confirm.prepare 的 args.args 是嵌套对象，servers.import 的 json 是整段导入数据）。
//
// 只用于审计副本，不影响真正传给执行器的参数。
func auditSafeArgs(tool string, args map[string]any) map[string]any {
	if len(args) == 0 {
		return args
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if appctlSensitiveArgKey(k) {
			out[k] = "***"
			continue
		}
		// servers.import 的 json 是整段导入数据（可能含明文密码），整体折叠成摘要。
		if tool == OpServersImport && (k == "json" || k == "payload") {
			out[k] = appctlSummarizeImportArg(v)
			continue
		}
		out[k] = appctlSanitizeValue(v)
	}
	return out
}

// appctlSummarizeImportArg 把导入数据折叠成「多少字节」的摘要（绝不回显内容）。
func appctlSummarizeImportArg(v any) any {
	s, ok := v.(string)
	if !ok {
		return "***"
	}
	return fmt.Sprintf("<%d 字节的导入数据，已省略>", len(s))
}

// appctlSensitiveArgKey 判断参数名是否属于「值本身可能是明文密钥」的键。
func appctlSensitiveArgKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "password", "passwd", "pwd", "passphrase", "privatekey", "keycontent", "key_content",
		"content", "secret", "apikey", "api_key", "token", "confirm", "authorization":
		return true
	}
	return false
}

// appctlSanitizeValue 递归脱敏：嵌套对象里的敏感键同样打码。
func appctlSanitizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			if appctlSensitiveArgKey(k) {
				out[k] = "***"
				continue
			}
			out[k] = appctlSanitizeValue(vv)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, appctlSanitizeValue(item))
		}
		return out
	default:
		return v
	}
}

// ---- 入参读取与 JSON 归一 ----

func appctlString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func appctlBool(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

// appctlInt 读取整数入参；JSON 里数字统一是 float64，这里做一次收敛（小数直接截断）。
func appctlInt(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	return 0, false
}

func appctlMap(args map[string]any, key string) map[string]any {
	if v, ok := args[key].(map[string]any); ok {
		return v
	}
	return nil
}

// appctlStringList 读取字符串数组（也接受单个字符串，按一个元素处理）。
func appctlStringList(args map[string]any, key string) ([]string, bool) {
	switch v := args[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, true
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			out = append(out, strings.TrimSpace(s))
		}
		return out, true
	case string:
		return []string{strings.TrimSpace(v)}, true
	}
	return nil, false
}

// appctlJSON 把任意返回值归一成「JSON 形状」的 any（map[string]any / []any / 标量）。
//
// 为什么需要：宿主可能返回 models.ServerNode 这类具体结构体，而脱敏 / 裁剪必须按键名操作；
// 归一化后再处理，两个世界（结构体与 JSON）就不会各写一份逻辑。
func appctlJSON(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func appctlObj(v any) map[string]any {
	j := appctlJSON(v)
	if m, ok := j.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func appctlObjSlice(v any) []map[string]any {
	j := appctlJSON(v)
	arr, ok := j.([]any)
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

func appctlSortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---- 明文脱敏（返回值侧）----

// appctlNodeSecretFieldsPresent 返回该对象里**有值**的敏感字段名（升序，用于提示哪些字段是明文）。
func appctlNodeSecretFieldsPresent(obj map[string]any) []string {
	out := make([]string, 0, len(appctlSecretFields))
	for _, f := range appctlSecretFields {
		if s, ok := obj[f].(string); ok && s != "" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// appctlRedactSecrets 复制一份对象并把明文敏感字段置空，同时返回被脱敏的字段名。
func appctlRedactSecrets(obj map[string]any) (map[string]any, []string) {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		out[k] = v
	}
	names := make([]string, 0, len(appctlSecretFields))
	for _, f := range appctlSecretFields {
		if s, ok := out[f].(string); ok && s != "" {
			out[f] = ""
			names = append(names, f)
		}
	}
	sort.Strings(names)
	return out, names
}

// appctlMergeFieldNames 把若干次脱敏得到的字段名合并去重。
func appctlMergeFieldNames(dst map[string]bool, names []string) {
	for _, n := range names {
		dst[n] = true
	}
}

func appctlFieldNameList(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// appctlRedactedNode 对单个节点对象做「按需脱敏」，并组装提示字段。
//
// 返回：处理后的对象 + 追加到响应的说明字段（redacted / redactedFields / plaintextFields / note）。
func appctlNodeSecretView(obj map[string]any, includeSecrets bool) (map[string]any, []string, []string) {
	if includeSecrets {
		return obj, nil, appctlNodeSecretFieldsPresent(obj)
	}
	redacted, names := appctlRedactSecrets(obj)
	return redacted, names, nil
}

// ---- 宿主调用 ----

// appctlCall 调宿主的 op（args 为 nil 时不传参数体）。
func appctlCall(ctx context.Context, h Handler, op string, args map[string]any) (any, error) {
	var raw json.RawMessage
	if len(args) > 0 {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("序列化入参失败: %w", err)
		}
		raw = b
	}
	return h.Handle(ctx, op, raw)
}

// appctlAllowSecrets 校验「是否允许读取明文密钥」（secrets.read = 设置里的「允许读取敏感数据」）。
func (s *Server) appctlAllowSecrets(detail string) error {
	return s.allowCap(CapSecretsRead, detail)
}

// ---- 入口 ----

// isAppctlTool 判断工具名是否属于应用控制面（mcp.go 的分发用它把请求交给本文件处理）。
func isAppctlTool(name string) bool {
	return appctlToolSet[name]
}

// handleAppctl 执行一次应用控制面工具调用，返回 MCP 的 content 数组。
//
// 能力位与两段式确认的**框架级**校验已经在 mcpCallToolAudited 里完成（读表见 appctl_register.go）；
// 这里只处理框架管不到的补充规则：
//   - includeSecrets 需要 secrets.read；
//   - app.command 的「按命令」能力位与确认（框架的确认是工具级的，粒度不够）。
func (s *Server) handleAppctl(ctx context.Context, name string, args map[string]any) ([]any, error) {
	if s.opts.Handler == nil {
		return nil, errors.New("调试服务未就绪：宿主 Handler 为空")
	}
	res, err := s.appctlDispatch(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return appctlText(res)
}

// appctlDispatch 把工具名翻译成宿主 op，并对结果做脱敏 / 限额。
func (s *Server) appctlDispatch(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	h := s.opts.Handler
	switch name {
	// ---- A. 服务器 ----
	case OpServersList:
		return s.appctlServersList(ctx, h, args)
	case OpServersGet:
		return s.appctlServersGet(ctx, h, args)
	case OpServersCreate:
		return s.appctlServersCreate(ctx, h, args)
	case OpServersUpdate:
		return s.appctlServersUpdate(ctx, h, args)
	case OpServersDelete:
		return s.appctlServersDelete(ctx, h, args)
	case OpServersDuplicate:
		return s.appctlServersDuplicate(ctx, h, args)
	case OpServersMoveGroup:
		return s.appctlServersMoveGroup(ctx, h, args)
	case OpServersExport:
		return s.appctlServersExport(ctx, h, args)
	case OpServersImport:
		return s.appctlServersImport(ctx, h, args)
	// ---- B. 凭据 ----
	case OpCredentialsList:
		return s.appctlCredentialsList(ctx, h)
	case OpCredentialsCreate:
		return s.appctlCredentialsCreate(ctx, h, args)
	case OpCredentialsUpdate:
		return s.appctlCredentialsUpdate(ctx, h, args)
	case OpCredentialsDelete:
		return s.appctlCredentialsDelete(ctx, h, args)
	case OpCredentialsAttach:
		return s.appctlCredentialsAttach(ctx, h, args)
	// ---- C. 隧道 ----
	case OpTunnelsList:
		return s.appctlTunnelsList(ctx, h)
	case OpTunnelsCreate:
		return s.appctlTunnelsCreate(ctx, h, args)
	case OpTunnelsUpdate:
		return s.appctlTunnelsUpdate(ctx, h, args)
	case OpTunnelsDelete:
		return s.appctlTunnelsDelete(ctx, h, args)
	case OpTunnelsStart:
		return s.appctlTunnelsStartStop(ctx, h, args, OpTunnelsStart)
	case OpTunnelsStop:
		return s.appctlTunnelsStartStop(ctx, h, args, OpTunnelsStop)
	// ---- D. 设置 ----
	case OpSettingsSchema:
		return s.appctlSettingsSchema(ctx, h, args)
	case OpSettingsReset:
		return s.appctlSettingsReset(ctx, h, args)
	// ---- E. 内置命令 ----
	case OpAppCommands:
		return s.appctlAppCommands(ctx, h)
	case OpAppCommand:
		return s.appctlAppCommand(ctx, h, args)
	// ---- F. 生命周期 ----
	case OpAppQuit:
		res, err := appctlCall(ctx, h, OpAppQuit, nil)
		if err != nil {
			return nil, err
		}
		out := appctlObj(res)
		out["quitting"] = true
		out["note"] = "应用即将退出：所有 SSH 会话、本机终端与隧道都会被断开。"
		return out, nil
	}
	return nil, fmt.Errorf("未知的应用控制面工具: %s", name)
}

// ---- A. 服务器 ----

// appctlServersList 列出服务器（默认脱敏；includeSecrets=true 需要 secrets.read）。
func (s *Server) appctlServersList(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	include := appctlBool(args, "includeSecrets")
	if include {
		if err := s.appctlAllowSecrets("servers.list includeSecrets=true"); err != nil {
			return nil, err
		}
	}
	raw, err := appctlCall(ctx, h, OpServersList, nil)
	if err != nil {
		return nil, err
	}
	obj := appctlObj(raw)
	items := appctlObjSlice(obj["servers"])
	fields := map[string]bool{}
	out := make([]any, 0, len(items))
	for _, it := range items {
		view, redactedNames, plainNames := appctlNodeSecretView(it, include)
		appctlMergeFieldNames(fields, redactedNames)
		appctlMergeFieldNames(fields, plainNames)
		out = append(out, view)
	}
	total := len(items)
	truncated := false
	if total > appctlMaxListItems {
		out = out[:appctlMaxListItems]
		truncated = true
	}
	res := map[string]any{
		"count":    len(out),
		"total":    total,
		"servers":  out,
		"redacted": !include,
	}
	if truncated {
		res["truncated"] = true
		res["limit"] = appctlMaxListItems
		res["truncatedNote"] = fmt.Sprintf("服务器数量超过 %d 条上限，已只返回前 %d 条。", appctlMaxListItems, appctlMaxListItems)
	}
	if include {
		res["plaintextFields"] = appctlFieldNameList(fields)
		res["note"] = "返回中含**明文**敏感字段（见 plaintextFields）：请勿落盘、勿写入对话记录，用完即弃。"
	} else {
		res["redactedFields"] = appctlFieldNameList(fields)
	}
	return res, nil
}

// appctlServersGet 读取单个服务器。
func (s *Server) appctlServersGet(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（服务器 id，可用 servers.list 查看）", ErrBadInput)
	}
	include := appctlBool(args, "includeSecrets")
	if include {
		if err := s.appctlAllowSecrets("servers.get includeSecrets=true（id=" + id + "）"); err != nil {
			return nil, err
		}
	}
	raw, err := appctlCall(ctx, h, OpServersGet, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	node := appctlObj(appctlObj(raw)["server"])
	if len(node) == 0 {
		return nil, fmt.Errorf("%w: 未找到服务器 %s", ErrNotFound, id)
	}
	view, redactedNames, plainNames := appctlNodeSecretView(node, include)
	res := map[string]any{
		"server":         view,
		"includeSecrets": include,
	}
	if include {
		res["plaintextFields"] = plainNames
		res["note"] = "返回中含**明文**敏感字段（见 plaintextFields）：请勿落盘、勿写入对话记录，用完即弃。"
	} else {
		res["redactedFields"] = redactedNames
	}
	return res, nil
}

// appctlServersCreate 新增服务器（ID 由宿主生成；传入的 node.id 会被忽略）。
func (s *Server) appctlServersCreate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	node := appctlMap(args, "node")
	if len(node) == 0 {
		return nil, fmt.Errorf("%w: 缺少 node（服务器对象：至少要有 host 与 user）", ErrBadInput)
	}
	if err := appctlValidateServerNode(node, true); err != nil {
		return nil, err
	}
	// 新建语义：不允许带 id（否则会意外覆盖既有服务器）。
	clean := make(map[string]any, len(node))
	for k, v := range node {
		if k == "id" {
			continue
		}
		clean[k] = v
	}
	raw, err := appctlCall(ctx, h, OpServersCreate, map[string]any{"node": clean})
	if err != nil {
		return nil, err
	}
	saved := appctlObj(appctlObj(raw)["server"])
	view, redactedNames, _ := appctlNodeSecretView(saved, false)
	return map[string]any{
		"ok":             true,
		"server":         view,
		"redactedFields": redactedNames,
		"note":           "已创建并保存；服务器里的密码 / 私钥不会回显。",
	}, nil
}

// appctlServersUpdate 局部更新服务器：只覆盖 node 里给出的非空字段，其余保持原值。
func (s *Server) appctlServersUpdate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（服务器 id，可用 servers.list 查看）", ErrBadInput)
	}
	node := appctlMap(args, "node")
	if len(node) == 0 {
		return nil, fmt.Errorf("%w: 缺少 node（要修改的字段；空值 / 0 视为不修改）", ErrBadInput)
	}
	// 局部更新：host / user 可以不给（空值 = 保持原值），只校验给出的值是否合法。
	if err := appctlValidateServerNode(node, false); err != nil {
		return nil, err
	}
	raw, err := appctlCall(ctx, h, OpServersUpdate, map[string]any{"id": id, "node": node})
	if err != nil {
		return nil, err
	}
	saved := appctlObj(appctlObj(raw)["server"])
	view, redactedNames, _ := appctlNodeSecretView(saved, false)
	return map[string]any{
		"ok":             true,
		"server":         view,
		"redactedFields": redactedNames,
		"note":           "只覆盖了 node 里给出的非空字段；未给出的字段保持原值（含未提供的密码 / 私钥）。",
	}, nil
}

// appctlServersDelete 删除服务器。
func (s *Server) appctlServersDelete(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（服务器 id，可用 servers.list 查看）", ErrBadInput)
	}
	raw, err := appctlCall(ctx, h, OpServersDelete, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	out["note"] = "服务器已删除（不可恢复；已打开的会话不受影响，但下次无法再据此连接）。"
	return out, nil
}

// appctlServersDuplicate 复制一台服务器（新 ID；可指定新名字）。
func (s *Server) appctlServersDuplicate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（要复制的服务器 id）", ErrBadInput)
	}
	callArgs := map[string]any{"id": id}
	if name := appctlString(args, "name"); name != "" {
		callArgs["name"] = name
	}
	raw, err := appctlCall(ctx, h, OpServersDuplicate, callArgs)
	if err != nil {
		return nil, err
	}
	saved := appctlObj(appctlObj(raw)["server"])
	view, redactedNames, _ := appctlNodeSecretView(saved, false)
	return map[string]any{
		"ok":             true,
		"server":         view,
		"redactedFields": redactedNames,
		"note":           "已复制为新服务器（新 id）；原服务器的密码 / 私钥一并复制，但不会回显。",
	}, nil
}

// appctlServersMoveGroup 批量移动服务器到某个分组（group 为空 = 移出分组）。
func (s *Server) appctlServersMoveGroup(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	ids, ok := appctlStringList(args, "ids")
	if !ok || len(ids) == 0 {
		return nil, fmt.Errorf("%w: 缺少 ids（要移动的服务器 id 数组）", ErrBadInput)
	}
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("%w: ids 里没有有效的服务器 id", ErrBadInput)
	}
	group := appctlString(args, "group")
	raw, err := appctlCall(ctx, h, OpServersMoveGroup, map[string]any{"ids": clean, "group": group})
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	if group == "" {
		out["note"] = "已把这些服务器移出分组（变为未分组）。"
	} else {
		out["note"] = "已把这些服务器移动到分组 " + group + "（分组不存在时会自动出现在分组列表里）。"
	}
	return out, nil
}

// appctlServersExport 导出服务器为 JSON 文本（默认脱敏；includeSecrets 需要 secrets.read）。
//
// 体积策略：按「整条服务器」累积到字节预算，超出的部分丢弃并回 truncated / droppedCount，
// 保证返回的仍然是一份**合法 JSON**（不在这里做字符串截断，半截 JSON 对 AI 毫无价值）。
func (s *Server) appctlServersExport(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	include := appctlBool(args, "includeSecrets")
	if include {
		if err := s.appctlAllowSecrets("servers.export includeSecrets=true"); err != nil {
			return nil, err
		}
	}
	raw, err := appctlCall(ctx, h, OpServersList, nil)
	if err != nil {
		return nil, err
	}
	items := appctlObjSlice(appctlObj(raw)["servers"])
	fields := map[string]bool{}
	envelope := map[string]any{
		"format":     "ding-ssh.servers",
		"version":    1,
		"exportedAt": time.Now().UnixMilli(),
		"redacted":   !include,
	}
	base, _ := json.Marshal(envelope)
	budget := appctlMaxExportBytes - len(base) - 256 // 256 字节给 count / truncated 等字段留余量
	kept := make([]any, 0, len(items))
	used := 0
	for _, it := range items {
		view, redactedNames, plainNames := appctlNodeSecretView(it, include)
		appctlMergeFieldNames(fields, redactedNames)
		appctlMergeFieldNames(fields, plainNames)
		b, err := json.Marshal(view)
		if err != nil {
			continue
		}
		if used+len(b)+2 > budget {
			break
		}
		kept = append(kept, view)
		used += len(b) + 2
	}
	doc := make(map[string]any, len(envelope)+4)
	for k, v := range envelope {
		doc[k] = v
	}
	doc["count"] = len(kept)
	doc["total"] = len(items)
	if len(kept) < len(items) {
		doc["truncated"] = true
		doc["droppedCount"] = len(items) - len(kept)
		doc["truncatedNote"] = fmt.Sprintf("导出内容超过 %d 字节上限，已按整条服务器丢弃 %d 条；"+
			"请改用 servers.get 逐条导出，或减少服务器数量后重试。", appctlMaxExportBytes, len(items)-len(kept))
	}
	doc["servers"] = kept
	text, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化导出内容失败: %w", err)
	}
	res := map[string]any{
		"json":     string(text),
		"count":    len(kept),
		"total":    len(items),
		"bytes":    len(text),
		"redacted": !include,
	}
	if len(kept) < len(items) {
		res["truncated"] = true
	}
	if include {
		res["plaintextFields"] = appctlFieldNameList(fields)
		res["note"] = "导出内容中含**明文**敏感字段（见 plaintextFields）：请勿落盘、勿写入对话记录，用完即弃。"
	} else {
		res["redactedFields"] = appctlFieldNameList(fields)
		res["note"] = "导出内容已脱敏（明文密钥字段被清空）；如需含明文请传 includeSecrets=true（需要 secrets.read 能力位）。"
	}
	return res, nil
}

// appctlServersImport 导入服务器（mode=merge 合并 / replace 先删除现有服务器再导入）。
func (s *Server) appctlServersImport(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	payload := appctlString(args, "json")
	if payload == "" {
		return nil, fmt.Errorf("%w: 缺少 json（servers.export 的导出文本，或形如 {\"servers\":[...]} / [...] 的 JSON）", ErrBadInput)
	}
	if len(payload) > appctlMaxImportBytes {
		return nil, fmt.Errorf("%w: 导入数据过大（%d 字节 > %d 字节上限）", ErrBadInput, len(payload), appctlMaxImportBytes)
	}
	servers, err := appctlParseImportPayload(payload)
	if err != nil {
		return nil, err
	}
	mode := appctlString(args, "mode")
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		return nil, fmt.Errorf("%w: 非法 mode %q（只支持 merge | replace）", ErrBadInput, mode)
	}
	raw, err := appctlCall(ctx, h, OpServersImport, map[string]any{"servers": servers, "mode": mode})
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	out["mode"] = mode
	if mode == "replace" {
		out["note"] = "replace 模式：导入前已**删除**全部现有服务器（不可恢复），随后写入本次数据。"
	} else {
		out["note"] = "merge 模式：按 id 覆盖同 id 的服务器，其余新增；未出现在本次数据里的服务器保持不变。"
	}
	return out, nil
}

// appctlParseImportPayload 解析导入数据：支持导出文档（{"servers":[...]}）与裸数组（[...]）。
func appctlParseImportPayload(payload string) ([]any, error) {
	var doc any
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return nil, fmt.Errorf("%w: 导入数据不是合法 JSON: %v", ErrBadInput, err)
	}
	var arr []any
	switch t := doc.(type) {
	case []any:
		arr = t
	case map[string]any:
		inner, ok := t["servers"]
		if !ok {
			return nil, fmt.Errorf("%w: 导入数据里没有 servers 字段（应为 servers.export 的输出或裸数组）", ErrBadInput)
		}
		arr, ok = inner.([]any)
		if !ok {
			return nil, fmt.Errorf("%w: 导入数据的 servers 字段不是数组", ErrBadInput)
		}
	default:
		return nil, fmt.Errorf("%w: 导入数据应为对象或数组", ErrBadInput)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("%w: 导入数据里没有任何服务器", ErrBadInput)
	}
	if len(arr) > appctlMaxListItems {
		return nil, fmt.Errorf("%w: 导入条目过多（%d > %d）", ErrBadInput, len(arr), appctlMaxListItems)
	}
	bad := make([]string, 0, appctlMaxImportErrors)
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			if len(bad) < appctlMaxImportErrors {
				bad = append(bad, fmt.Sprintf("第 %d 条不是对象", i+1))
			}
			continue
		}
		if err := appctlValidateServerNode(m, true); err != nil {
			if len(bad) < appctlMaxImportErrors {
				bad = append(bad, fmt.Sprintf("第 %d 条：%v", i+1, err))
			}
		}
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("%w: 导入数据校验失败（%s）", ErrBadInput, strings.Join(bad, "；"))
	}
	return arr, nil
}

// appctlValidateServerNode 校验服务器对象的关键字段（只校验**会在 SSH 连接里用到**的那几个）。
//
// requireCore=true（新建 / 导入）：host 与 user 必填；
// requireCore=false（局部更新）：空值 = 不修改，因此只校验「给出来的值」是否合法。
// 缺省字段交给宿主补默认值（port=22 / authType=password）。
func appctlValidateServerNode(node map[string]any, requireCore bool) error {
	if requireCore && appctlString(node, "host") == "" {
		return fmt.Errorf("%w: node.host 不能为空", ErrBadInput)
	}
	if requireCore && appctlString(node, "user") == "" {
		return fmt.Errorf("%w: node.user 不能为空", ErrBadInput)
	}
	if v, has := node["port"]; has && v != nil {
		port, ok := appctlInt(node, "port")
		if !ok {
			return fmt.Errorf("%w: node.port 必须是整数", ErrBadInput)
		}
		if port != 0 && (port < 1 || port > 65535) {
			return fmt.Errorf("%w: node.port 越界（%d，合法范围 1-65535）", ErrBadInput, port)
		}
	}
	if auth := appctlString(node, "authType"); auth != "" && auth != "password" && auth != "privateKey" {
		return fmt.Errorf("%w: 非法 node.authType %q（只支持 password | privateKey）", ErrBadInput, auth)
	}
	return nil
}

// ---- B. 凭据 ----

// appctlCredentialsList 列出凭据**元信息**（绝不返回明文）。
//
// 说明：models.Credential 里没有创建时间字段，因此这里返回的是仓库实际有的元信息
// （name / type / user / keyPath / hasContent），并把「为什么没有 createdAt」写在 note 里。
func (s *Server) appctlCredentialsList(ctx context.Context, h Handler) (map[string]any, error) {
	raw, err := appctlCall(ctx, h, OpCredentialsList, nil)
	if err != nil {
		return nil, err
	}
	items := appctlObjSlice(appctlObj(raw)["credentials"])
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, appctlCredentialMeta(it))
	}
	total := len(out)
	truncated := false
	if total > appctlMaxListItems {
		out = out[:appctlMaxListItems]
		truncated = true
	}
	res := map[string]any{
		"count":          len(out),
		"total":          total,
		"credentials":    out,
		"note":           "只返回元信息：不含密码 / 私钥内容（明文永不通过本工具返回）。凭据模型里没有创建时间字段，因此没有 createdAt。",
		"redactedFields": []string{"password", "keyContent"},
	}
	if truncated {
		res["truncated"] = true
		res["limit"] = appctlMaxListItems
	}
	return res, nil
}

// appctlCredentialMeta 把一条凭据投影成元信息（白名单式，新增敏感字段也不会漏出去）。
//
// 兼容两种输入形状：宿主给元信息（键 type / hasContent）或直接给凭据对象
// （键 authType / password / keyContent）—— 两种都能安全投影，绝不会带出明文。
func appctlCredentialMeta(c map[string]any) map[string]any {
	typ := appctlString(c, "type")
	if typ == "" {
		typ = appctlString(c, "authType")
	}
	hasContent := false
	if v, ok := c["hasContent"].(bool); ok {
		hasContent = v
	} else {
		hasContent = appctlString(c, "password") != "" || appctlString(c, "keyContent") != ""
	}
	meta := map[string]any{
		"id":         appctlString(c, "id"),
		"name":       appctlString(c, "name"),
		"type":       typ,
		"user":       appctlString(c, "user"),
		"hasContent": hasContent,
	}
	if p := appctlString(c, "keyPath"); p != "" {
		meta["keyPath"] = p
	}
	return meta
}

// appctlCredentialsCreate 新增凭据（明文只进不出：返回值只回 {id,name,hasContent}）。
func (s *Server) appctlCredentialsCreate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	name := appctlString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("%w: 缺少 name（凭据名称，如「生产 root」）", ErrBadInput)
	}
	typ := appctlString(args, "type")
	if typ != "password" && typ != "privateKey" {
		return nil, fmt.Errorf("%w: 非法 type %q（只支持 password | privateKey）", ErrBadInput, typ)
	}
	content := appctlString(args, "content")
	if content == "" {
		return nil, fmt.Errorf("%w: 缺少 content（%s 的明文内容；本参数不会出现在审计 / 日志 / 返回值里）", ErrBadInput, typ)
	}
	callArgs := map[string]any{"name": name, "type": typ, "content": content}
	if user := appctlString(args, "user"); user != "" {
		callArgs["user"] = user
	}
	raw, err := appctlCall(ctx, h, OpCredentialsCreate, callArgs)
	if err != nil {
		return nil, err
	}
	cred := appctlObj(appctlObj(raw)["credential"])
	return map[string]any{
		"ok":         true,
		"id":         appctlString(cred, "id"),
		"name":       appctlString(cred, "name"),
		"type":       appctlString(cred, "authType"),
		"hasContent": true,
		"note":       "凭据已保存；明文内容不会通过 MCP 返回（审计参数同样已打码）。",
	}, nil
}

// appctlCredentialsUpdate 修改凭据的名称 / 内容（内容不传表示保持原值）。
func (s *Server) appctlCredentialsUpdate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（凭据 id，可用 credentials.list 查看）", ErrBadInput)
	}
	name := appctlString(args, "name")
	content := appctlString(args, "content")
	user := appctlString(args, "user")
	if name == "" && content == "" && user == "" {
		return nil, fmt.Errorf("%w: 至少要给 name / content / user 之一", ErrBadInput)
	}
	callArgs := map[string]any{"id": id}
	if name != "" {
		callArgs["name"] = name
	}
	if content != "" {
		callArgs["content"] = content
	}
	if user != "" {
		callArgs["user"] = user
	}
	raw, err := appctlCall(ctx, h, OpCredentialsUpdate, callArgs)
	if err != nil {
		return nil, err
	}
	cred := appctlObj(appctlObj(raw)["credential"])
	hasContent := true
	if v, has := cred["hasContent"].(bool); has {
		hasContent = v
	}
	return map[string]any{
		"ok":         true,
		"id":         appctlString(cred, "id"),
		"name":       appctlString(cred, "name"),
		"type":       appctlString(cred, "authType"),
		"hasContent": hasContent,
		"note":       "凭据已更新；明文内容不会通过 MCP 返回。",
	}, nil
}

// appctlCredentialsDelete 删除凭据。
func (s *Server) appctlCredentialsDelete(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（凭据 id，可用 credentials.list 查看）", ErrBadInput)
	}
	raw, err := appctlCall(ctx, h, OpCredentialsDelete, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	out["note"] = "凭据已删除（不可恢复）；已使用它的服务器节点不受影响（它们的字段是复制出来的）。"
	return out, nil
}

// appctlCredentialsAttach 把凭据应用到某台服务器（等于把用户名 / 密码 / 私钥写进该节点）。
func (s *Server) appctlCredentialsAttach(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	serverID := appctlString(args, "serverId")
	if serverID == "" {
		return nil, fmt.Errorf("%w: 缺少 serverId（目标服务器 id）", ErrBadInput)
	}
	credID := appctlString(args, "credentialId")
	if credID == "" {
		return nil, fmt.Errorf("%w: 缺少 credentialId（凭据 id，可用 credentials.list 查看）", ErrBadInput)
	}
	raw, err := appctlCall(ctx, h, OpCredentialsAttach, map[string]any{"serverId": serverID, "credentialId": credID})
	if err != nil {
		return nil, err
	}
	obj := appctlObj(raw)
	node := appctlObj(obj["server"])
	view, redactedNames, _ := appctlNodeSecretView(node, false)
	res := map[string]any{
		"ok":             true,
		"serverId":       serverID,
		"credentialId":   credID,
		"appliedFields":  obj["fields"],
		"server":         view,
		"redactedFields": redactedNames,
		"note":           "凭据内容已复制到服务器节点；密码 / 私钥明文不会回显。",
	}
	return res, nil
}

// ---- C. 隧道 ----

// appctlTunnelsList 列出隧道。
func (s *Server) appctlTunnelsList(ctx context.Context, h Handler) (map[string]any, error) {
	raw, err := appctlCall(ctx, h, OpTunnelsList, nil)
	if err != nil {
		return nil, err
	}
	obj := appctlObj(raw)
	items := appctlObjSlice(obj["tunnels"])
	total := len(items)
	truncated := false
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	if total > appctlMaxListItems {
		// 隧道条目本身很小，这里只是为了「任何列表都有上限」的一致性。
		out = out[:appctlMaxListItems]
		truncated = true
	}
	res := map[string]any{"count": len(out), "total": total, "tunnels": out}
	if truncated {
		res["truncated"] = true
		res["limit"] = appctlMaxListItems
	}
	res["note"] = "隧道只存在于当前运行实例（不落库）：应用退出后全部消失。"
	return res, nil
}

// appctlTunnelsCreate 新建隧道（serverId / sessionId 二选一，由宿主解析成完整节点）。
func (s *Server) appctlTunnelsCreate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	callArgs, err := s.appctlTunnelArgs(args)
	if err != nil {
		return nil, err
	}
	raw, err := appctlCall(ctx, h, OpTunnelsCreate, callArgs)
	if err != nil {
		return nil, err
	}
	return appctlTunnelResult(raw, "隧道已创建并启动（start 失败时会在返回里给出中文原因）。"), nil
}

// appctlTunnelsUpdate 修改隧道（需要 serverId / sessionId 以确定用哪个 SSH 连接）。
func (s *Server) appctlTunnelsUpdate(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（隧道 id，可用 tunnels.list 查看）", ErrBadInput)
	}
	callArgs, err := s.appctlTunnelArgs(args)
	if err != nil {
		return nil, err
	}
	callArgs["id"] = id
	raw, err := appctlCall(ctx, h, OpTunnelsUpdate, callArgs)
	if err != nil {
		return nil, err
	}
	return appctlTunnelResult(raw, "隧道配置已更新（原本在运行的隧道已按新配置重启）。"), nil
}

// appctlTunnelsDelete 删除（并停止）隧道。
func (s *Server) appctlTunnelsDelete(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（隧道 id，可用 tunnels.list 查看）", ErrBadInput)
	}
	raw, err := appctlCall(ctx, h, OpTunnelsDelete, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	out["note"] = "隧道已停止并移除（该隧道不落库，移除后无法恢复配置）。"
	return out, nil
}

// appctlTunnelsStartStop 启动 / 停止隧道。
func (s *Server) appctlTunnelsStartStop(ctx context.Context, h Handler, args map[string]any, op string) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（隧道 id，可用 tunnels.list 查看）", ErrBadInput)
	}
	raw, err := appctlCall(ctx, h, op, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	note := "隧道已启动（会新建一条 SSH 连接）。"
	if op == OpTunnelsStop {
		note = "隧道已停止（条目保留，可用 tunnels.start 重新启动）。"
	}
	return appctlTunnelResult(raw, note), nil
}

// appctlTunnelResult 组装隧道类工具的返回。
func appctlTunnelResult(raw any, note string) map[string]any {
	obj := appctlObj(raw)
	res := map[string]any{"ok": true, "note": note}
	if t, has := obj["tunnel"]; has {
		res["tunnel"] = t
	}
	for k, v := range obj {
		if _, exists := res[k]; !exists {
			res[k] = v
		}
	}
	return res
}

// appctlTunnelArgs 校验并整理隧道入参（字段名以 internal/sshx 的隧道 API 为准）。
//
// 模式与端口语义（与 internal/sshx.manager 的 normalizeTunnelConfig 对齐）：
//   - local（-L，本地转发）：listenPort = 本地监听端口；targetHost/targetPort = 远端目标；
//   - remote（-R，远程转发）：listenHost/listenPort = 远端监听地址与端口；targetPort = 本地目标端口
//     （**当前实现里本地目标固定为 127.0.0.1**，targetHost 会被忽略并回显提示）；
//   - dynamic（-D，SOCKS5）：listenPort = 本地 SOCKS5 端口；不需要 target*。
//
// listenHost 说明：local / dynamic 模式的本地监听地址在当前实现里固定 127.0.0.1，
// 传入其它值会被忽略（返回里会带上 listenHostNote）。
func (s *Server) appctlTunnelArgs(args map[string]any) (map[string]any, error) {
	serverID := appctlString(args, "serverId")
	sessionID := appctlString(args, "sessionId")
	if serverID == "" && sessionID == "" {
		return nil, fmt.Errorf("%w: 需要 serverId 或 sessionId（用哪条 SSH 连接建隧道）", ErrBadInput)
	}
	typ := appctlString(args, "type")
	switch typ {
	case "local", "remote", "dynamic":
	default:
		return nil, fmt.Errorf("%w: 缺少 / 非法 type %q（只支持 local | remote | dynamic）", ErrBadInput, typ)
	}
	port, ok := appctlInt(args, "listenPort")
	if !ok || port <= 0 {
		return nil, fmt.Errorf("%w: 缺少 listenPort（local: 本地监听端口；remote: 远端监听端口；dynamic: 本地 SOCKS5 端口）", ErrBadInput)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("%w: listenPort 越界（%d，合法范围 1-65535）", ErrBadInput, port)
	}
	out := map[string]any{
		"type":       typ,
		"listenPort": port,
	}
	if serverID != "" {
		out["serverId"] = serverID
	}
	if sessionID != "" {
		out["sessionId"] = sessionID
	}
	if name := appctlString(args, "name"); name != "" {
		out["name"] = name
	}
	if host := appctlString(args, "listenHost"); host != "" {
		out["listenHost"] = host
	}
	if host := appctlString(args, "targetHost"); host != "" {
		out["targetHost"] = host
	}
	if p, has := appctlInt(args, "targetPort"); has {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("%w: targetPort 越界（%d，合法范围 1-65535）", ErrBadInput, p)
		}
		out["targetPort"] = p
	}
	switch typ {
	case "local":
		if _, has := out["targetPort"]; !has {
			return nil, fmt.Errorf("%w: local 模式需要 targetPort（远端目标端口）", ErrBadInput)
		}
	case "remote":
		if _, has := out["targetPort"]; !has {
			return nil, fmt.Errorf("%w: remote 模式需要 targetPort（本地目标端口）", ErrBadInput)
		}
	}
	return out, nil
}

// ---- D. 设置 ----

// appctlSettingsSchema 返回**全部**设置字段的元信息（path / type / default / current / enum…）。
//
// 覆盖性由实现保证：字段清单是从宿主给的 defaults JSON 树递归走出来的，
// 因此「新增设置字段忘了写进 schema」在结构上不可能发生（只可能缺 enum / risky 这类注解）。
func (s *Server) appctlSettingsSchema(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	include := appctlBool(args, "includeSecrets")
	if include {
		if err := s.appctlAllowSecrets("settings.schema includeSecrets=true"); err != nil {
			return nil, err
		}
	}
	raw, err := appctlCall(ctx, h, OpSettingsSchema, map[string]any{"includeSecrets": include})
	if err != nil {
		return nil, err
	}
	obj := appctlObj(raw)
	defaults := appctlObj(obj["defaults"])
	current := appctlObj(obj["current"])
	meta := appctlObj(obj["meta"])
	if len(defaults) == 0 {
		return nil, errors.New("宿主未返回设置字段清单（defaults 为空）：settings.schema 暂不可用")
	}
	fields := make([]any, 0, 64)
	truncated := false
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case map[string]any:
			if prefix != "" {
				fields = append(fields, appctlSchemaEntry(prefix, "object", nil, appctlLookup(current, prefix), meta, false))
			}
			for _, k := range appctlSortedKeys(t) {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, t[k])
			}
		default:
			fields = append(fields, appctlSchemaEntry(prefix, appctlValueType(v), v, appctlLookup(current, prefix), meta, appctlLookupFound(current, prefix)))
		}
	}
	walk("", defaults)
	if len(fields) > appctlMaxSchemaFields {
		fields = fields[:appctlMaxSchemaFields]
		truncated = true
	}
	res := map[string]any{
		"count":  len(fields),
		"fields": fields,
		"notes":  obj["notes"],
	}
	if truncated {
		res["truncated"] = true
		res["limit"] = appctlMaxSchemaFields
	}
	if paths, ok := obj["redactedPaths"].([]any); ok && len(paths) > 0 {
		res["redactedPaths"] = paths
	}
	res["note"] = "字段清单由宿主的默认设置树递归生成，覆盖全部设置项；" +
		"restartRequired=true 表示改动要重启应用才生效，risky=true 表示改动会削弱安全边界或影响调试通道本身。" +
		"可用 settings.reset {paths:[...]} 只重置其中若干字段。"
	return res, nil
}

// appctlSchemaEntry 组装一条字段元信息。
func appctlSchemaEntry(path, typ string, def, cur any, meta map[string]any, hasCurrent bool) map[string]any {
	entry := map[string]any{
		"path":            path,
		"type":            typ,
		"default":         def,
		"restartRequired": false,
		"risky":           false,
	}
	if hasCurrent {
		entry["current"] = cur
	} else {
		entry["current"] = nil
	}
	if m, ok := meta[path].(map[string]any); ok {
		if enum, has := m["enum"]; has {
			entry["enum"] = enum
		}
		if v, has := m["restartRequired"].(bool); has {
			entry["restartRequired"] = v
		}
		if v, has := m["risky"].(bool); has {
			entry["risky"] = v
		}
		if note := appctlString(m, "note"); note != "" {
			entry["note"] = note
		}
	}
	return entry
}

// appctlValueType 把 JSON 值映射成对 AI 友好的类型名。
func appctlValueType(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case string:
		return "string"
	case float64:
		if t == float64(int64(t)) {
			return "int"
		}
		return "number"
	case []any:
		allString := true
		for _, item := range t {
			if _, ok := item.(string); !ok {
				allString = false
				break
			}
		}
		if allString {
			return "stringArray"
		}
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// appctlLookup 按点分路径在 JSON 对象里取值（不存在返回 nil）。
func appctlLookup(root map[string]any, path string) any {
	v, _ := appctlLookupValue(root, path)
	return v
}

// appctlLookupFound 与 appctlLookup 相同，但同时返回「路径是否存在」。
func appctlLookupFound(root map[string]any, path string) bool {
	_, ok := appctlLookupValue(root, path)
	return ok
}

func appctlLookupValue(root map[string]any, path string) (any, bool) {
	if path == "" {
		return root, true
	}
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

// appctlSettingsReset 重置设置：paths 为空 = 整体恢复默认；给了 paths = 只重置这些字段。
//
// 注意 paths 必须是**非空**数组或直接省略：显式传空数组会返回中文错误，
// 而不是被当成「整体恢复默认」—— 这个方向上的歧义代价太大（会把用户设置全清掉）。
func (s *Server) appctlSettingsReset(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	paths := []string{}
	if v, has := args["paths"]; has && v != nil {
		list, ok := appctlStringList(args, "paths")
		if !ok {
			return nil, fmt.Errorf("%w: paths 必须是字符串数组（如 [\"uiScale\",\"theme.background\"]）", ErrBadInput)
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("%w: paths 不能是空数组：省略 paths 表示整体恢复默认；"+
				"只想重置若干字段请给出至少一个字段路径（可用 settings.schema 查看）", ErrBadInput)
		}
		for _, p := range list {
			if p == "" {
				return nil, fmt.Errorf("%w: paths 里存在空字符串（字段路径如 uiScale / theme.background）", ErrBadInput)
			}
			paths = append(paths, p)
		}
	}
	raw, err := appctlCall(ctx, h, OpSettingsReset, map[string]any{"paths": paths})
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	if len(paths) == 0 {
		out["note"] = "已把设置整体恢复为默认值（调试模式的总开关 / 端口 / 绑定与「允许执行 JS」「允许读取敏感数据」被刻意保留，" +
			"避免把控制面自己关掉导致失联）。"
	} else {
		out["note"] = "已把指定字段重置为默认值，其余设置不变。"
	}
	return out, nil
}

// ---- E. 内置命令 ----

// appctlAppCommands 通过前端桥列出命令面板里的命令。
func (s *Server) appctlAppCommands(ctx context.Context, h Handler) (map[string]any, error) {
	raw, err := appctlCall(ctx, h, OpAppCommands, nil)
	if err != nil {
		return nil, err
	}
	obj := appctlObj(raw)
	items := appctlObjSlice(obj["commands"])
	out := make([]any, 0, len(items))
	for _, it := range items {
		cmd := map[string]any{"id": appctlString(it, "id"), "title": appctlString(it, "title")}
		if sec := appctlString(it, "section"); sec != "" {
			cmd["section"] = sec
		}
		if hk := appctlString(it, "hotkey"); hk != "" {
			cmd["hotkey"] = hk
		}
		if note := appctlString(it, "note"); note != "" {
			cmd["note"] = note
		}
		out = append(out, cmd)
	}
	total := len(out)
	truncated := false
	if total > appctlMaxListItems {
		out = out[:appctlMaxListItems]
		truncated = true
	}
	res := map[string]any{"count": len(out), "total": total, "commands": out}
	if truncated {
		res["truncated"] = true
		res["limit"] = appctlMaxListItems
	}
	res["note"] = "命令清单来自前端的命令面板（与界面里显示的是同一套定义）；" +
		"connect-<serverId> 这类动态命令会随已保存的服务器变化。"
	return res, nil
}

// appctlAppCommand 执行一条内置命令。
//
// 权限按**命令实际影响**判定（见 appctl_register.go 的 appctlCommandRule）：
//   - 只读 / 切换视图类 → 不要求能力位、不需要 token；
//   - 会改配置或断开的命令 → 要求 config.write，且必须带两段式确认 token；
//   - **未登记的命令一律按最保守策略**：config.write + token。
//
// 注意框架的确认是「工具级」的，而这里要「按命令」判定，因此本工具的确认在下面自行校验，
// token 的 action 名固定为 app.command（AI 先 confirm.prepare(action=app.command, args={id:…})）。
func (s *Server) appctlAppCommand(ctx context.Context, h Handler, args map[string]any) (map[string]any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（命令 id，可用 app.commands 查看）", ErrBadInput)
	}
	rule := appctlCommandRuleFor(id)
	if rule.cap != "" {
		if err := s.allowCap(rule.cap, "命令 "+id); err != nil {
			return nil, err
		}
	}
	if rule.confirm {
		token := appctlTokenArg(args)
		if token == "" {
			return nil, fmt.Errorf("%w: 命令 %q 属于会改动配置 / 连接状态的命令，需要两段式确认："+
				"请先 confirm.prepare（action=%s, args={\"id\":%q}）取得 token 再调用"+
				"（只读 / 切换视图类命令不需要 token）。",
				ErrConfirmRequired, id, appctlCommandConfirmAction, id)
		}
		if _, err := s.takeConfirm(token, appctlCommandConfirmAction, withoutToken(args)); err != nil {
			return nil, err
		}
	}
	callArgs := map[string]any{"id": id}
	if v, has := args["args"]; has && v != nil {
		callArgs["args"] = v
	}
	raw, err := appctlCall(ctx, h, OpAppCommand, callArgs)
	if err != nil {
		return nil, err
	}
	out := appctlObj(raw)
	out["ok"] = true
	out["id"] = id
	if rule.note != "" {
		out["rule"] = rule.note
	}
	// 命令是「页面里的动作」，一律不可撤销（刷新 / 手动改回即可，但没有通用回滚）。
	out["reversible"] = false
	return out, nil
}

// appctlTokenArg 读取两段式确认 token：主名 token（与既有工具一致），confirm 为别名
// （规格里的参数名写作 confirm）。
func appctlTokenArg(args map[string]any) string {
	if tok := appctlString(args, "token"); tok != "" {
		return tok
	}
	return appctlString(args, "confirm")
}

// ---- F. 生命周期 ----

// (app.quit 在 appctlDispatch 里直接透传给宿主；真正的 wailsruntime.Quit 由宿主执行，
//  这样 debugsrv 不依赖 Wails。)
