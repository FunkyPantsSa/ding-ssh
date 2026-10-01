package debugsrv

// 应用控制面（M7）的能力位 / 两段式确认 / 内置命令规则登记。
//
// 为什么单独一个文件：地基（caps.go 顶部注释）明确规定「各域不许改内置表 toolCaps，
// 必须在自己文件的 init() 里用 RegisterToolCap / RegisterToolConfirm 登记」。
// 这里把全部 servers.* / credentials.* / tunnels.* / settings.* / app.* 工具一次登记完，
// appctl_test.go 会重新调用 registerAppctlCaps() 保证用例之间不互相影响
// （RegisterToolCap 幂等，重复登记以最后一次为准），并断言 RegistrationErrors() 为空。
//
// 能力位划分（与任务规格逐条对应）：
//
//	读类（默认脱敏；includeSecrets 时在处理器里额外校验 secrets.read）：
//	  servers.list / servers.get / servers.export / credentials.list /
//	  tunnels.list / settings.schema / app.commands
//	配置写入（config.write + 两段式确认）：
//	  servers.create / servers.update / servers.delete / servers.duplicate /
//	  servers.moveGroup / servers.import / credentials.attach /
//	  tunnels.create / tunnels.update / tunnels.delete / tunnels.start / tunnels.stop /
//	  settings.reset
//	凭据写入（secrets.write，同时要求 AllowSecrets + 两段式确认）：
//	  credentials.create / credentials.update / credentials.delete
//	生命周期（lifecycle + 两段式确认）：
//	  app.quit
//
// 特例：app.command 的权限是**按命令**判定的（见下面的 appctlCommandRule），
// 而框架的能力位 / 确认都是「工具级」的，因此这里对 app.command 只登记 CapRead，
// 真正的判定在 appctl.go 的 appctlAppCommand 里按命令逐条做。

import (
	"fmt"
	"sort"
	"strings"
)

func init() {
	registerAppctlCaps()
}

// appctlToolNames 全部应用控制面工具（顺序 = tools/list 的展示顺序 = 能力位注册顺序）。
var appctlToolNames = []string{
	// A. 服务器
	OpServersList, OpServersGet, OpServersCreate, OpServersUpdate, OpServersDelete,
	OpServersDuplicate, OpServersMoveGroup, OpServersExport, OpServersImport,
	// B. 凭据
	OpCredentialsList, OpCredentialsCreate, OpCredentialsUpdate, OpCredentialsDelete, OpCredentialsAttach,
	// C. 隧道
	OpTunnelsList, OpTunnelsCreate, OpTunnelsUpdate, OpTunnelsDelete, OpTunnelsStart, OpTunnelsStop,
	// D. 设置
	OpSettingsSchema, OpSettingsReset,
	// E. 内置命令
	OpAppCommands, OpAppCommand,
	// F. 生命周期
	OpAppQuit,
}

// appctlToolSet 工具名集合（isAppctlTool 用，避免每次线性扫描）。
var appctlToolSet = func() map[string]bool {
	out := make(map[string]bool, len(appctlToolNames))
	for _, n := range appctlToolNames {
		out[n] = true
	}
	return out
}()

// appctlOps 宿主侧 op 集合（servers.export 没有 op，它在 debugsrv 侧组装，因此不在这里）。
var appctlOps = map[string]bool{
	OpServersList: true,
	OpServersGet:  true, OpServersCreate: true, OpServersUpdate: true, OpServersDelete: true,
	OpServersDuplicate: true, OpServersMoveGroup: true, OpServersImport: true,
	OpCredentialsList: true, OpCredentialsCreate: true, OpCredentialsUpdate: true,
	OpCredentialsDelete: true, OpCredentialsAttach: true,
	OpTunnelsList: true, OpTunnelsCreate: true, OpTunnelsUpdate: true,
	OpTunnelsDelete: true, OpTunnelsStart: true, OpTunnelsStop: true,
	OpSettingsSchema: true, OpSettingsReset: true,
	OpAppCommands: true, OpAppCommand: true,
	OpAppQuit: true,
}

// IsAppctlOp 判断 op 是否属于应用控制面（宿主 debug.go 据此把请求转给 handleAppctlOp）。
//
// 为什么不含 OpSettingsUpdate：那是既有的「局部合并设置」op，语义与 settings.reset
// 完全不同，混在一起会让「重置」意外变成「合并」。
func IsAppctlOp(op string) bool {
	return appctlOps[strings.TrimSpace(op)]
}

// registerAppctlCaps 登记全部应用控制面工具的能力位与确认动作（幂等，测试可重复调用）。
func registerAppctlCaps() {
	// ---- 读类（永远允许；includeSecrets 的额外校验在 appctl.go 里做）----
	for _, name := range []string{
		OpServersList, OpServersGet, OpServersExport,
		OpCredentialsList,
		OpTunnelsList,
		OpSettingsSchema,
		OpAppCommands,
	} {
		RegisterToolCap(name, CapRead)
	}

	// ---- 服务器：config.write + 两段式确认 ----
	registerWrite(OpServersCreate, CapConfigWrite, "servers.create")
	registerWrite(OpServersUpdate, CapConfigWrite, "servers.update")
	registerWrite(OpServersDelete, CapConfigWrite, "servers.delete")
	// 复制 = 新建（动作名按规格用 servers.create，token 绑定的是 {id,name}）
	registerWrite(OpServersDuplicate, CapConfigWrite, "servers.create")
	// 移动分组 = 更新（动作名按规格用 servers.update，token 绑定的是 {ids,group}）
	registerWrite(OpServersMoveGroup, CapConfigWrite, "servers.update")
	// 导入 = 新建（replace 模式会先删现有服务器，因此描述里必须写明，见 appctl_export.go 的条目）
	registerWrite(OpServersImport, CapConfigWrite, "servers.create")
	// 凭据绑定到服务器 = 更新服务器
	registerWrite(OpCredentialsAttach, CapConfigWrite, "servers.update")

	// ---- 凭据：secrets.write（还需 AllowSecrets，由 debugGate 校验）+ 两段式确认 ----
	registerWrite(OpCredentialsCreate, CapSecretsWrite, "credentials.create")
	registerWrite(OpCredentialsUpdate, CapSecretsWrite, "credentials.update")
	registerWrite(OpCredentialsDelete, CapSecretsWrite, "credentials.delete")

	// ---- 隧道：config.write + 两段式确认 ----
	registerWrite(OpTunnelsCreate, CapConfigWrite, "tunnels.create")
	registerWrite(OpTunnelsUpdate, CapConfigWrite, "tunnels.update")
	registerWrite(OpTunnelsDelete, CapConfigWrite, "tunnels.delete")
	registerWrite(OpTunnelsStart, CapConfigWrite, "tunnels.start")
	registerWrite(OpTunnelsStop, CapConfigWrite, "tunnels.stop")

	// ---- 设置重置：config.write + 两段式确认（settings.update 保持既有登记，不动）----
	registerWrite(OpSettingsReset, CapConfigWrite, "settings.reset")

	// ---- 内置命令：工具级只登记 read；写类命令的判定见 appctlCommandRule ----
	RegisterToolCap(OpAppCommand, CapRead)
	// 占位键：把 action 名 app.command 挂进确认注册表，使 AI 能
	// confirm.prepare(action=app.command) 拿到 token；因为键不是真实工具，
	// ToolConfirmAction("app.command") 仍为 false（视图类命令不需要 token）。
	RegisterToolConfirm(appctlCommandConfirmKey, appctlCommandConfirmAction)

	// ---- 生命周期：lifecycle + 两段式确认 ----
	registerWrite(OpAppQuit, CapLifecycle, "app.quit")
}

// registerWrite 登记一个「需要能力位 + 两段式确认」的写工具。
func registerWrite(tool string, cap Capability, action string) {
	RegisterToolCap(tool, cap)
	RegisterToolConfirm(tool, action)
}

// ---- 内置命令的权限规则（app.command 的「按命令判定」）----

const (
	// appctlCommandConfirmAction 内置命令的确认 action 名（AI 先 confirm.prepare(action=app.command)）。
	appctlCommandConfirmAction = "app.command"
	// appctlCommandConfirmKey 把上面这个 action 挂进确认注册表用的占位键。
	//
	// 它不是工具（不会出现在 tools/list，也不会被 AllRegisteredToolNames 统计），
	// 存在的唯一目的是让 confirmActionNeedsConfirm("app.command") 为真 ——
	// 否则 AI 无法为「会改配置的命令」准备 token（confirm.prepare 会以「不是破坏性操作」拒绝）。
	appctlCommandConfirmKey = "app.command.rule"
)

// appctlCommandRule 一条内置命令的权限规则。
//
//	cap     需要的额外能力位（空 = 只读 / 切换视图，不需要能力位）
//	confirm 是否需要两段式确认 token
//	note    给 AI 看的中文说明（会出现在 app.command 的返回里）
type appctlCommandRule struct {
	cap     Capability
	confirm bool
	note    string
}

// appctlCommandRules 已登记的内置命令规则。
//
// 判定依据是「命令实际影响」而不是名字：
//   - 命令面板里现有的命令全部只切换视图 / 打开面板 / 建立会话（都不写用户配置、也不断开已有会话），
//     因此都不需要能力位与 token；它们对用户是可见且可逆的（关掉标签 / 切回来即可）；
//   - 将来若加入「改配置 / 断开连接 / 退出」类命令，必须显式登记 config.write 或 lifecycle +
//     confirm=true，否则会落到下面的默认规则（最保守）。
var appctlCommandRules = map[string]appctlCommandRule{
	"workspace": {note: "切换到工作区视图（只读 / 可逆）"},
	"servers":   {note: "切换到服务器管理视图（只读 / 可逆）"},
	"tunnel":    {note: "切换到隧道视图（只读 / 可逆）"},
	"settings":  {note: "切换到设置视图（只读 / 可逆）"},
	"new":       {note: "打开「新建服务器」对话框（只打开面板，不写配置）"},
	"local":     {note: "打开一个本机终端标签（会新建本地会话，可关闭）"},
	"hide-tool": {note: "收起右侧工具侧栏（只读 / 可逆）"},
	"show-sftp": {note: "打开 SFTP 侧栏（只读 / 可逆）"},
	"show-sys":  {note: "打开系统看板侧栏（只读 / 可逆）"},
}

// appctlCommandPrefixRule 前缀规则的条目类型（用具名类型，匿名结构体切片无法省略类型）。
type appctlCommandPrefixRule struct {
	prefix string
	rule   appctlCommandRule
}

// appctlCommandPrefixRules 前缀规则：命令面板里按服务器动态生成的 connect-<serverId>。
var appctlCommandPrefixRules = []appctlCommandPrefixRule{
	{prefix: "connect-", rule: appctlCommandRule{note: "连接指定服务器（会新建一个 SSH 会话标签；不改配置，可关闭标签收回）"}},
}

// appctlCommandRuleFor 返回某条命令的权限规则；未登记的命令按最保守策略处理。
func appctlCommandRuleFor(id string) appctlCommandRule {
	id = strings.TrimSpace(id)
	if r, ok := appctlCommandRules[id]; ok {
		return r
	}
	for _, p := range appctlCommandPrefixRules {
		if strings.HasPrefix(id, p.prefix) {
			return p.rule
		}
	}
	return appctlCommandRule{
		cap:     CapConfigWrite,
		confirm: true,
		note: "未登记的命令：按最保守策略处理（要求 config.write 能力位 + 两段式确认）；" +
			"如确认该命令是只读 / 可逆的，请在 appctl_register.go 的 appctlCommandRules 里登记",
	}
}

// ---- 注册表驱动的工具清单（供测试断言用）----

// AllRegisteredToolNames 返回注册表（RegisterToolCap）里登记过能力位的全部工具名（升序）。
//
// 用途：把「工具总数被硬编码」的断言改成**注册表驱动** ——
// 遍历这里的名字断言「每个已注册工具都出现在 tools/list 且都有能力登记」，
// 后续再新增工具时测试不需要改任何数字。
//
// 注意：只统计登记过能力位的工具（toolCapRegistry 的键）。
// 为了让某个 action 进入「可确认清单」而用 RegisterToolConfirm 挂的占位键
// （例如 appctlCommandConfirmKey）不是工具，不会出现在这里。
func AllRegisteredToolNames() []string {
	toolCapRegistryMu.RLock()
	defer toolCapRegistryMu.RUnlock()
	out := make([]string, 0, len(toolCapRegistry))
	for name := range toolCapRegistry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ---- 工具条目（由 mcp.go 集中追加，见 mcpTools）----

// appctlToolEntries 返回应用控制面全部工具条目（名称 / 描述 / 入参 schema）。
//
// 描述末尾的「能力要求：…」由 mcp.go 的 withCapabilityNotes 自动追加（含是否需要 token），
// 因此这里**不要**手写能力行 —— 手写会与真实校验脱节。
// 这里只写「规格里要求讲清楚」的东西：脱敏默认值、明文警告、replace 会删除、重启 / 风险提示。
func appctlToolEntries() []mcpTool {
	secretWarn := "includeSecrets=true 时会返回**明文**密钥（需要 secrets.read 能力位）：" +
		"这类结果请勿落盘、勿写入对话记录，用完即弃；返回里的 plaintextFields 会标出哪些字段是明文。"
	// 写工具统一带 token 参数：既有工具用 token，规格里写作 confirm，因此两个名字都收。
	tokenProp := strProp("两段式确认 token（confirm.prepare 取得；别名 confirm 亦可）")
	return []mcpTool{
		// ---- A. 服务器 ----
		{Name: OpServersList, Description: "列出已保存的服务器（默认脱敏：password / keyContent 被清空，redactedFields 标出被清空的字段）。" + secretWarn,
			InputSchema: obj(map[string]any{
				"includeSecrets": boolProp("true 表示返回明文密钥（默认 false）"),
			}),
			// M9 结构化输出：形状见 handleAppctlOp 的 OpServersList / OpServersGet / settings.schema。
			OutputSchema: outputObj(map[string]any{
				"count": outProp("number"), "servers": outProp("array"),
				"redacted": outProp("boolean"), "plaintextFields": outProp("array"),
			}, "count", "servers"),
			structured: structuredIdentity},
		{Name: OpServersGet, Description: "读取单台服务器的完整配置（含分组、端口、认证方式、环境变量）。默认脱敏。" + secretWarn,
			InputSchema: obj(map[string]any{
				"id":             strProp("服务器 id（见 servers.list）"),
				"includeSecrets": boolProp("true 表示返回明文密钥（默认 false）"),
			}, "id"),
			OutputSchema: outputObj(map[string]any{
				"server": outProp("object"), "redactedFields": outProp("array"), "plaintextFields": outProp("array"),
			}, "server"),
			structured: structuredIdentity},
		{Name: OpServersCreate, Description: "新增一台服务器并保存（id 由应用生成；node.id 会被忽略，避免误覆盖既有服务器）。" +
			"node 至少要有 host 与 user；port 缺省 22，authType 缺省 password（password | privateKey）。" +
			"保存会立即持久化到用户配置并触发界面刷新。",
			InputSchema: obj(map[string]any{
				"node":  map[string]any{"type": "object", "description": "服务器对象：{name, group, host, port, user, authType, password, keyPath, keyContent, bgImage, blurAmount, envVars}"},
				"token": tokenProp,
			}, "node")},
		{Name: OpServersUpdate, Description: "局部更新一台服务器：**只覆盖 node 里给出的非空字段**（空字符串 / 0 视为不修改，因此无法用它清空字段）。" +
			"未给出的敏感字段（密码 / 私钥）保持原值。",
			InputSchema: obj(map[string]any{
				"id":    strProp("服务器 id"),
				"node":  map[string]any{"type": "object", "description": "要修改的字段（非空才覆盖）"},
				"token": tokenProp,
			}, "id", "node")},
		{Name: OpServersDelete, Description: "删除一台服务器（不可恢复）。已打开的会话不受影响，但之后无法再据此连接。",
			InputSchema: obj(map[string]any{
				"id":    strProp("服务器 id"),
				"token": tokenProp,
			}, "id")},
		{Name: OpServersDuplicate, Description: "复制一台服务器为新条目（新 id；name 缺省时在原名字后加「副本」）。密码 / 私钥一并复制且不会回显。",
			InputSchema: obj(map[string]any{
				"id":    strProp("要复制的服务器 id"),
				"name":  strProp("新服务器名字（可选）"),
				"token": tokenProp,
			}, "id")},
		{Name: OpServersMoveGroup, Description: "把一批服务器移动到指定分组（group 传空字符串 = 移出分组，变为未分组）。返回实际移动成功的 id 与未找到的 id。",
			InputSchema: obj(map[string]any{
				"ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "服务器 id 列表"},
				"group": strProp("目标分组名（空字符串表示移出分组）"),
				"token": tokenProp,
			}, "ids")},
		{Name: OpServersExport, Description: "导出全部服务器为 JSON 文本（结果在返回的 json 字段里，可直接喂给 servers.import）。默认脱敏。" + secretWarn +
			"内容超过 256KB 时按整条服务器丢弃，并给出 truncated / droppedCount。",
			InputSchema: obj(map[string]any{
				"includeSecrets": boolProp("true 表示导出含明文密钥（默认 false）"),
			})},
		{Name: OpServersImport, Description: "导入服务器数据：json 传 servers.export 的输出，或形如 {\"servers\":[...]} / [...] 的 JSON。" +
			"mode=merge（默认）按 id 覆盖同 id 的服务器、其余新增；**mode=replace 会先删除全部现有服务器再导入（不可恢复）**。",
			InputSchema: obj(map[string]any{
				"json":  strProp("要导入的 JSON 文本（上限 2MB）"),
				"mode":  strProp("merge | replace（默认 merge）"),
				"token": tokenProp,
			}, "json")},
		// ---- B. 凭据 ----
		{Name: OpCredentialsList, Description: "列出已保存的凭据**元信息**（id / name / type / user / keyPath / hasContent）：" +
			"绝不返回密码或私钥内容。凭据模型里没有创建时间字段，因此没有 createdAt。",
			InputSchema: obj(nil)},
		{Name: OpCredentialsCreate, Description: "新增一条凭据。content 是明文（type=password 时是密码，type=privateKey 时是私钥内容）：" +
			"**该参数不会出现在审计 / 日志 / 返回值里**（审计参数里会被替换为 ***），返回值只回 {id,name,type,hasContent}。" +
			"需要 secrets.write 能力位（同时要求「允许读取敏感数据」已开启）。",
			InputSchema: obj(map[string]any{
				"name":    strProp("凭据名称，如「生产 root」"),
				"type":    strProp("password | privateKey"),
				"content": strProp("明文内容（密码或私钥文本）；不会回显"),
				"user":    strProp("用户名（可选）"),
				"token":   tokenProp,
			}, "name", "type", "content")},
		{Name: OpCredentialsUpdate, Description: "修改凭据的名称 / 用户名 / 明文内容（content 不传 = 保持原值）。明文同样不会出现在审计、日志与返回值里。",
			InputSchema: obj(map[string]any{
				"id":      strProp("凭据 id（见 credentials.list）"),
				"name":    strProp("新名称（可选）"),
				"user":    strProp("新用户名（可选）"),
				"content": strProp("新的明文内容（可选；不会回显）"),
				"token":   tokenProp,
			}, "id")},
		{Name: OpCredentialsDelete, Description: "删除一条凭据（不可恢复）。已使用它绑定过的服务器节点不受影响（字段是复制过去的）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("凭据 id"),
				"token": tokenProp,
			}, "id")},
		{Name: OpCredentialsAttach, Description: "把某条凭据的用户名 / 认证方式 / 密码（或私钥）复制到指定服务器节点并保存。" +
			"返回只回被写入的字段名（appliedFields），不回显明文；需要 config.write 能力位。",
			InputSchema: obj(map[string]any{
				"serverId":     strProp("目标服务器 id"),
				"credentialId": strProp("凭据 id（见 credentials.list）"),
				"token":        tokenProp,
			}, "serverId", "credentialId")},
		// ---- C. 隧道 ----
		{Name: OpTunnelsList, Description: "列出当前全部 SSH 隧道（id / name / mode / 端口 / 状态）。隧道只存在于当前运行实例（不落库）。",
			InputSchema: obj(nil)},
		{Name: OpTunnelsCreate, Description: "新建并启动一条 SSH 隧道（复用 internal/sshx 的隧道实现）。" +
			"type=local（-L）：listenPort=本地监听端口，targetHost/targetPort=远端目标；" +
			"type=remote（-R）：listenHost/listenPort=远端监听地址与端口，targetPort=本地目标端口（当前实现里本地目标固定 127.0.0.1）；" +
			"type=dynamic（-D）：listenPort=本地 SOCKS5 端口，不需要 target*。" +
			"listenHost 在 local / dynamic 下当前固定为 127.0.0.1（传入会被忽略）。" +
			"serverId 用已保存的服务器，sessionId 用某个已打开会话的服务器节点（二选一）。",
			InputSchema: obj(map[string]any{
				"serverId":   strProp("已保存的服务器 id（与 sessionId 二选一）"),
				"sessionId":  strProp("已打开会话的 id（与 serverId 二选一）"),
				"type":       strProp("local | remote | dynamic"),
				"name":       strProp("隧道名（可选，缺省自动生成）"),
				"listenHost": strProp("监听地址（remote 模式 = 远端监听地址；可选）"),
				"listenPort": intProp("监听端口（local: 本地；remote: 远端；dynamic: 本地 SOCKS5）"),
				"targetHost": strProp("目标主机（local 模式，缺省 127.0.0.1）"),
				"targetPort": intProp("目标端口（local: 远端目标端口；remote: 本地目标端口）"),
				"token":      tokenProp,
			}, "type", "listenPort")},
		{Name: OpTunnelsUpdate, Description: "修改隧道配置并保存；原本在运行的隧道会按新配置重启。必须用 serverId / sessionId 指明用哪条 SSH 连接。",
			InputSchema: obj(map[string]any{
				"id":         strProp("隧道 id（见 tunnels.list）"),
				"serverId":   strProp("已保存的服务器 id（与 sessionId 二选一）"),
				"sessionId":  strProp("已打开会话的 id（与 serverId 二选一）"),
				"type":       strProp("local | remote | dynamic"),
				"name":       strProp("隧道名（可选）"),
				"listenHost": strProp("监听地址（可选）"),
				"listenPort": intProp("监听端口"),
				"targetHost": strProp("目标主机（local 模式，可选）"),
				"targetPort": intProp("目标端口"),
				"token":      tokenProp,
			}, "id", "type", "listenPort")},
		{Name: OpTunnelsDelete, Description: "停止并移除一条隧道（该隧道不落库，移除后配置无法恢复）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("隧道 id"),
				"token": tokenProp,
			}, "id")},
		{Name: OpTunnelsStart, Description: "启动一条已停止的隧道（会新建一条 SSH 连接）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("隧道 id"),
				"token": tokenProp,
			}, "id")},
		{Name: OpTunnelsStop, Description: "停止一条隧道（条目保留，可用 tunnels.start 重新启动）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("隧道 id"),
				"token": tokenProp,
			}, "id")},
		// ---- D. 设置 ----
		{Name: OpSettingsSchema, Description: "返回**全部**设置字段的元信息：{path, type, default, current, enum?, restartRequired?, risky?, note?}。" +
			"字段清单由宿主的默认设置树递归生成（覆盖全部设置项，含 theme.* / appearance.* / fonts.* / debug.* 等嵌套字段；" +
			"对象型字段也会出现，便于整块重置）。restartRequired=true 表示改动要重启应用才生效；" +
			"risky=true 表示改动会削弱安全边界或影响调试通道本身。",
			InputSchema: obj(map[string]any{
				"includeSecrets": boolProp("true 表示连敏感字段（如本机 Shell 路径）的当前值一起返回（默认 false）"),
			}),
			OutputSchema: outputObj(map[string]any{
				"count": outProp("number"), "fields": outProp("array"), "notes": outProp("object"),
				"truncated": outProp("boolean"), "limit": outProp("number"), "redactedPaths": outProp("array"),
			}, "count", "fields"),
			structured: structuredIdentity},
		{Name: OpSettingsReset, Description: "把设置恢复默认值：paths 省略 = **整体恢复默认**（会刻意保留调试模式的总开关 / 端口 / 绑定与「允许执行 JS」「允许读取敏感数据」，" +
			"避免把控制面自己关掉导致失联）；paths=[\"uiScale\",\"theme.background\"] = 只重置这些字段。" +
			"会立即持久化；非法字段名会返回中文错误（可用 settings.schema 查全部字段）。",
			InputSchema: obj(map[string]any{
				"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "要重置的字段路径（省略 = 整体恢复默认）"},
				"token": tokenProp,
			})},
		// ---- E. 内置命令 ----
		{Name: OpAppCommands, Description: "列出命令面板里的内置命令：{id, title, section?, hotkey?}（与界面上的命令面板同源）。" +
			"connect-<serverId> 这类动态命令会随已保存的服务器变化。",
			InputSchema: obj(nil)},
		{Name: OpAppCommand, Description: "执行一条内置命令（id 见 app.commands）。权限**按命令实际影响**判定：" +
			"只读 / 切换视图类命令（workspace / servers / tunnel / settings / new / local / hide-tool / show-sftp / show-sys / connect-*）" +
			"不需要能力位、不需要 token；会改配置或断开连接的命令要求 config.write，且必须先 confirm.prepare（action=app.command）取 token；" +
			"**未登记的命令一律按最保守策略**（config.write + 两段式确认）。命令是页面里的动作，不可撤销（刷新或手动改回即可）。",
			InputSchema: obj(map[string]any{
				"id":    strProp("命令 id（见 app.commands）"),
				"args":  map[string]any{"type": "object", "description": "命令参数（可选，当前内置命令都不需要）"},
				"token": tokenProp,
			}, "id")},
		// ---- F. 生命周期 ----
		{Name: OpAppQuit, Description: "退出应用（等价于点关闭窗口，由 Go 侧 Wails runtime 执行）。" +
			"**会断开所有 SSH 会话 / 本机终端与隧道**，未保存的终端缓冲区内容会丢失；退出后调试服务与 MCP 连接一并失效。",
			InputSchema: obj(map[string]any{
				"token": tokenProp,
			})},
	}
}

// appctlToolEntryNames 返回工具条目的名字（顺序清单，供排查「登记了但没进 tools/list」）。
func appctlToolEntryNames() []string {
	entries := appctlToolEntries()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

// appctlCheckToolEntriesRegistered 自检：每个登记过能力位的工具都必须有工具条目
// （即出现在 tools/list 里），反之每个条目也必须登记过能力位。
//
// 不做 panic（避免影响运行期），返回中文错误由调用方（测试 / 诊断）处理。
func appctlCheckToolEntriesRegistered() error {
	entries := map[string]bool{}
	for _, n := range appctlToolEntryNames() {
		entries[n] = true
	}
	for _, n := range appctlToolEntryNames() {
		if _, ok := ToolCapabilities(n); !ok {
			return fmt.Errorf("工具 %s 有工具条目但没登记能力位（RegisterToolCap）", n)
		}
	}
	for _, n := range appctlToolNames {
		if !entries[n] {
			return fmt.Errorf("工具 %s 登记了能力位但没有工具条目（不会出现在 tools/list）", n)
		}
	}
	return nil
}
