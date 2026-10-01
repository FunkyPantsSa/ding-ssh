// Package debugsrv 的「权限内核」：能力位（Capability）与破坏性操作的两段式确认。
//
// # 两条规则，别混在一起
//
//   - **能力位 = 「允不允许这类操作」**：把可控动作切成若干能力（终端输入 / 界面写入 /
//     配置写入 / 凭据写入 / 远端文件写入 / 生命周期 / eval），宿主在每次工具调用前逐个校验。
//     它回答的是「用户授权了这个方向吗」，可持续放行、可反复调用。
//   - **两段式确认 = 「不可逆操作前再想一次」**：只有破坏性 / 不可逆动作（删服务器、清日志、
//     退出应用、开启敏感能力…）才需要 `confirm.prepare` → `confirm.commit`。
//     可逆 / 高频操作（终端输入、界面写入、eval）**只受能力位约束**，不要求 token ——
//     否则「AI 管 SSH」（terminal.run/expect）与「改一点看一眼」的 UI 循环直接不可用。
//
// # 后续三路（UI 观测 / 应用控制 / SFTP）怎么接入
//
// 不要改本文件的 `toolCaps` / `toolConfirmActions` 常量表（三方并行会互相冲突），
// 在**各自的文件**里用 `init()` 注册：
//
//	func init() {
//	    debugsrv.RegisterToolCap("sftp.write", debugsrv.CapRemoteFSWrite)
//	    debugsrv.RegisterToolConfirm("sftp.write", "sftp.write") // 不可逆 → 需要 token
//	    debugsrv.RegisterToolCap("ui.apply_css", debugsrv.CapUIWrite)
//	    // 可逆/高频 → 不注册 confirm，只受能力位约束
//	}
//
// 注册后：`tools/list` 的描述会自动带「能力要求：…（需先 confirm.prepare 提供 token=…）」，
// `tools/call` 前的能力校验与 token 校验自动生效，审计也会自动记账。
// 注册表先于内置表被查询（同名以注册值为准），非法能力名不会静默通过 ——
// 错误会记进 `RegistrationErrors()`，请在测试里断言它为空。
package debugsrv

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// 能力位（权限内核）。
//
// 为什么要有它：调试模式把整个应用（终端、设置、SFTP、生命周期）暴露给 AI，
// 单靠「调试模式总开关」粒度过粗 —— 想让它读状态就得连删除服务器一起放开。
// 因此把可控动作切成若干「能力」，宿主在每次工具调用前逐个校验：
//
//   - read 永远允许（读类工具不受开关限制；敏感字段仍受既有 AllowSecrets 控制）；
//   - eval 沿用既有 AllowEval（不重复造开关）；
//   - 其余能力默认关闭，只有用户在「设置 → 调试模式 → MCP 能力」里显式打开才放行。
//
// 能力位与 models.DebugSettings 的 Cap* 字段一一对应，前端开关 / 调试接口 /
// MCP 工具描述全部以这里的字符串为准，不要另起名字。
type Capability string

// 能力枚举。字符串同时也是对外的 API 名称（前端开关、审计记录的 cap 字段、拒绝原因文案）。
const (
	// CapRead 读取类能力：永远允许，Gate 不会拒绝它（保留常量是为了让工具表能显式标注「读」）。
	CapRead Capability = "read"
	// CapTerminalInput 向终端 / 会话写入数据（等价于在该终端里键入）。
	CapTerminalInput Capability = "terminal.input"
	// CapUIWrite 修改界面（CSS / 设计令牌 / store / 合成事件）。
	CapUIWrite Capability = "ui.write"
	// CapConfigWrite 写入配置（服务器、隧道、应用设置）。
	CapConfigWrite Capability = "config.write"
	// CapSecretsRead 读取含密钥的敏感数据（受既有 AllowSecrets 控制，不是新的开关）。
	CapSecretsRead Capability = "secrets.read"
	// CapSecretsWrite 写入凭据（受 CapSecretWrite + AllowSecrets 双重约束）。
	CapSecretsWrite Capability = "secrets.write"
	// CapRemoteFSWrite 远端文件系统写入（SFTP 写 / 删 / 改名 / 建目录 / 上传）。
	CapRemoteFSWrite Capability = "fs.remote.write"
	// CapSudoCredential 用应用里保存的密码在当前会话对应的服务器上执行 sudo -i（默认关）。
	//
	// 边界（定义本能力时就必须说清，见 sudo.go 与 README）：
	//   - 前置条件：还要求 AllowSecrets=true（密码来自凭据库），缺哪个都会被 Gate 明确指出来；
	//   - 密码只从凭据库读取，只经「随机命名 + chmod 600 + 用完即删」的远端临时文件喂给 sudo -S，
	//     绝不进入 MCP 参数 / 返回值 / 日志 / 审计；工具 schema 里永远没有 password 字段；
	//   - 不支持本机终端、没有保存密码的服务器，也**永不接受**调用方直接传入密码；
	//   - 每次 terminal.sudo 仍需两段式确认（等价于把该机器的 root 权限交给 AI）。
	CapSudoCredential Capability = "sudo.credential"
	// CapLifecycle 应用生命周期（重载页面 / 退出 / 重启）。
	CapLifecycle Capability = "lifecycle"
	// CapEval 在页面上下文执行 JS（沿用既有 AllowEval）。
	CapEval Capability = "eval"
)

// allCapabilities 全部能力位，顺序固定（文档、快照输出、前端列表都用它，避免各处顺序不一致）。
var allCapabilities = []Capability{
	CapRead, CapTerminalInput, CapUIWrite, CapConfigWrite,
	CapSecretsRead, CapSecretsWrite, CapRemoteFSWrite, CapSudoCredential, CapLifecycle, CapEval,
}

// AllCapabilities 返回全部能力位（副本，调用方可安全修改）。
func AllCapabilities() []Capability {
	out := make([]Capability, len(allCapabilities))
	copy(out, allCapabilities)
	return out
}

// capabilityDescriptions 能力的中文说明（面向 AI 与用户界面，直接展示）。
//
// 每条包含：一句话说明 + 风险提示 + 是否需要两段式确认（confirm.prepare → confirm.commit）。
// 注意最后一项是**按能力下最典型的操作**写的；具体到某个工具是否需要 token，
// 以该工具的确认登记（RegisterToolConfirm / toolConfirmActions）为准 ——
// 例如 terminal.input 能力下的 send_input 就不需要 token。
var capabilityDescriptions = map[Capability]string{
	CapRead:          "读取类：读取应用状态、终端缓冲区、日志、审计记录。永远允许（敏感字段仍受「允许读取敏感数据」控制）。",
	CapTerminalInput: "终端输入：向会话写入数据，等价于在该终端里键入命令。风险：可执行任意远端命令；属于可逆 / 高频操作，只受本开关约束，不需要 token。",
	CapUIWrite:       "界面写入：修改 CSS / 设计令牌 / 前端 store / 合成 DOM 事件，用于验证界面改动。风险：可伪装界面状态，默认关闭；可逆 / 高频操作，不需要 token。",
	CapConfigWrite:   "配置写入：修改服务器、隧道与应用设置（含调试设置本身）。风险：可持久化错误配置，默认关闭；设置类改动属于不可逆操作，需两段式确认。",
	CapSecretsRead:   "读取敏感数据：放开服务器 / 设置里可能含密钥字段的读取（同「允许读取敏感数据」开关）。",
	CapSecretsWrite:  "凭据写入：新增 / 修改 / 删除保存的凭据。需同时开启「允许读取敏感数据」与两段式确认。",
	CapRemoteFSWrite: "远端文件写入：SFTP 写入 / 删除 / 改名 / 建目录 / 上传。仅白名单前缀（SFTPWriteAllowlist）内的路径允许；留空 = 禁止任何写入路径。写入 / 删除属不可逆操作，需两段式确认。",
	CapSudoCredential: "sudo 凭证提权：允许 AI 在**当前会话对应的服务器**上，用**应用里保存的密码**执行 sudo -i（等于把该机器的 root 权限交给 AI）。" +
		"前置条件：必须同时开启「允许读取敏感数据」（secrets.read），因为密码只从凭据库读取；缺哪一项都会被明确拒绝。" +
		"密码不会出现在 MCP 参数 / 返回值 / 日志 / 审计里（工具 schema 里也没有 password 字段），也不接受调用方直接传入密码；" +
		"密码只写进远端随机临时文件（/tmp/.ding-sudo-<随机 12 位十六进制>、chmod 600、用完即删并校验），" +
		"临时文件所在路径还必须命中远端写入白名单（白名单不含 /tmp 时直接拒绝）。" +
		"不支持本机终端与没有保存密码的服务器。风险极高，默认关闭，开启需二次确认；每次执行 terminal.sudo 仍需两段式确认。",
	CapLifecycle: "应用生命周期：重载页面 / 退出 / 重启应用。风险：会中断用户当前会话，需两段式确认。",
	CapEval:      "执行 JS：在页面上下文执行任意脚本（等价于完全控制界面）。沿用「允许执行 JS」开关；可逆 / 高频操作，不需要 token。",
}

// CapabilityDescription 返回能力的中文说明；未知能力返回空串。
func CapabilityDescription(cap Capability) string {
	return capabilityDescriptions[cap]
}

// CapabilityDescriptions 返回「能力 → 中文说明」的副本，供 app.permissions / GetCapabilities 直接序列化。
func CapabilityDescriptions() map[string]string {
	out := make(map[string]string, len(capabilityDescriptions))
	for c, d := range capabilityDescriptions {
		out[string(c)] = d
	}
	return out
}

// KnownCapability 判断字符串是否是已知能力名（用于前端开关 / 调试接口入参校验）。
func KnownCapability(name string) (Capability, bool) {
	c := Capability(strings.TrimSpace(name))
	if _, ok := capabilityDescriptions[c]; ok {
		return c, true
	}
	return "", false
}

// CapabilityLabel 返回能力的中文短名（用于拒绝原因与审计列表）。
func CapabilityLabel(c Capability) string {
	switch c {
	case CapRead:
		return "读取"
	case CapTerminalInput:
		return "终端输入"
	case CapUIWrite:
		return "界面写入"
	case CapConfigWrite:
		return "配置写入"
	case CapSecretsRead:
		return "读取敏感数据"
	case CapSecretsWrite:
		return "凭据写入"
	case CapRemoteFSWrite:
		return "远端文件写入"
	case CapSudoCredential:
		return "sudo 凭证提权"
	case CapLifecycle:
		return "应用生命周期"
	case CapEval:
		return "执行 JS"
	}
	return string(c)
}

// Gate 由宿主（应用进程）实现：把能力位映射到 models.DebugSettings 的实际开关。
//
// 约定：
//   - Allow 返回 nil 表示允许；否则返回**中文**拒绝原因，文案要能直接回给 AI
//     （AI 需要看到「为什么被拒 + 怎么开启」，而不是一个布尔值）；
//   - detail 用于补充「写哪个路径 / 哪个对象」这类上下文（可为空），宿主可在原因里回显；
//   - Snapshot 返回当前能力快照，用于 app.permissions 工具与 initialize.instructions。
//
// Gate 必须是并发安全的（HTTP 与 MCP 请求可能同时到达）。
type Gate interface {
	Allow(cap Capability, detail string) error
	Snapshot() map[Capability]bool
}

// denyAllGate 未注入 Gate 时的兜底实现：只允许 read。
//
// 为什么不是「全部允许」：Options.Gate 为 nil 只可能出现在宿主忘记接线的情况下，
// 此时按最保守策略处理（拒绝一切写能力），保证「忘记配置」不会变成安全漏洞。
type denyAllGate struct{}

func (denyAllGate) Allow(cap Capability, _ string) error {
	if cap == CapRead {
		return nil
	}
	return fmt.Errorf("调试服务未配置能力校验（Gate 为空），已按最保守策略拒绝「%s」能力：请在应用内启用调试模式后重试", CapabilityLabel(cap))
}

func (denyAllGate) Snapshot() map[Capability]bool {
	out := make(map[Capability]bool, len(allCapabilities))
	for _, c := range allCapabilities {
		out[c] = c == CapRead
	}
	return out
}

// gate 返回实际生效的 Gate（未注入时用兜底实现）。
func (s *Server) gate() Gate {
	if s.opts.Gate != nil {
		return s.opts.Gate
	}
	return denyAllGate{}
}

// allowCap 校验能力位；detail 为补充信息（如 SFTP 路径）。
func (s *Server) allowCap(cap Capability, detail string) error {
	if cap == CapRead {
		return nil
	}
	return s.gate().Allow(cap, detail)
}

// CapabilitySnapshot 返回当前能力快照（用于 initialize.instructions 与 app.permissions）。
func (s *Server) CapabilitySnapshot() map[Capability]bool {
	return s.gate().Snapshot()
}

// capabilityRequirementText 生成工具描述末尾自动追加的一行「能力要求：…」。
//
// 统一由能力表生成（而不是每个工具手写一句话），保证：
//   - 描述与 tools/call 的真实校验永远一致，不会出现「描述说能调、实际被拒」；
//   - 新增工具时只需注册能力位（RegisterToolCap）与确认动作（RegisterToolConfirm）。
//
// 是否出现「需先 confirm.prepare」由 **该工具自己的** 确认登记决定，而不是由能力位推断：
// terminal.input 能力下的 send_input 不需要 token，而 config.write 下的 update_settings 需要。
func (s *Server) capabilityRequirementText(tool string) string {
	caps, _ := ToolCapabilities(tool)
	confirmAction, needConfirm := ToolConfirmAction(tool)
	if len(caps) == 0 {
		base := "能力要求：无（读取类工具，永远允许）。"
		if needConfirm {
			base += fmt.Sprintf("该工具属于破坏性操作：需先 confirm.prepare（action=%s）取得 token=… 再调用。", confirmAction)
		}
		return base
	}
	parts := make([]string, 0, len(caps))
	for _, c := range caps {
		if c == CapRead {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s（%s）", CapabilityLabel(c), c))
	}
	text := "能力要求："
	if len(parts) == 0 {
		text += "无（读取类工具，永远允许）"
	} else {
		text += strings.Join(parts, "；")
	}
	text += "。"
	if needConfirm {
		text += fmt.Sprintf("该工具属于破坏性 / 不可逆操作：需先 confirm.prepare（action=%s）取得 token=… 再调用。", confirmAction)
	} else {
		text += "可逆 / 高频操作，不需要 confirm.prepare（只受能力位约束）。"
	}
	return text
}

// toolCaps 内置的工具名 → 所需能力（读类工具为 nil）。
//
// 这张表是**内置事实来源**：tools/call 前的 Gate 校验、工具描述末尾的「能力要求」行
// 都由它 + 注册表（RegisterToolCap）推导。
//
// 后续三路（UI 观测 / 应用控制 / SFTP）**不要改这张表**：三方并行改同一个 map 会冲突，
// 请在各自的文件里用 init() + RegisterToolCap / RegisterToolConfirm 注册，见包注释。
var toolCaps = map[string][]Capability{
	// 读类（永远允许）
	"app_state":          nil,
	"list_terminals":     nil,
	"read_terminal":      nil,
	"scroll_terminal":    nil,
	"resize_terminal":    nil,
	"open_tab":           nil,
	"close_tab":          nil,
	"reconnect_session":  nil,
	"disconnect_session": nil,
	"list_servers":       nil,
	"get_settings":       nil,
	"screenshot":         nil,
	"recent_events":      nil,
	"list_logs":          nil,
	"read_logs":          nil,
	"export_diagnostics": nil,
	// 本波新增
	// confirm.prepare 只是登记待确认操作，不改变任何状态（真正的放行在 confirm.commit）；
	// confirm.commit 的能力校验在具体动作的执行器里做（见 debug.go 的 debugExecute）。
	"app.permissions":  nil,
	"app.recent_calls": nil,
	"app.health":       nil,
	"confirm.prepare":  nil,
	"confirm.commit":   nil,
	// 写类（需要能力位）
	"send_input":      {CapTerminalInput},
	"update_settings": {CapConfigWrite},
	"set_log_options": {CapConfigWrite},
	"clear_logs":      {CapLifecycle},
	"eval_js":         {CapEval},
}

// ---- 注册表（供后续三路在各自文件里并行接入）----
//
// 同步手段：sync.RWMutex。约定的用法是「init() 期写、运行期只读」，
// 但本包不依赖这个约定 —— 读写都用锁保护，运行期补注册同样安全
// （后续可能有「按需启用某域」之类的动态注册）。
var (
	toolCapRegistryMu   sync.RWMutex
	toolCapRegistry     = map[string][]Capability{}
	toolConfirmRegistry = map[string]string{}

	registrationErrMu sync.Mutex
	registrationErrs  []error
)

// RegisterToolCap 注册某个工具所需的能力位（供各域在自己的文件 init() 里调用）。
//
// 用法：
//
//	func init() {
//	    debugsrv.RegisterToolCap("sftp.write", debugsrv.CapRemoteFSWrite)
//	}
//
// 规则：
//   - 同名重复注册以**最后一次**为准（注册表优先于内置表）；
//   - 能力名必须已登记在 DebugSettings 里（`capabilityField`），否则不写入并记一条错误；
//   - 至少要有一个能力位（读类工具请显式传 CapRead，或干脆不注册 —— 未知工具按读处理）；
//   - 非法注册不会 panic（避免 init 期把整个应用带崩），错误请用 RegistrationErrors() 断言。
func RegisterToolCap(tool string, caps ...Capability) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		recordRegistrationError(fmt.Errorf("RegisterToolCap: 工具名不能为空"))
		return
	}
	if len(caps) == 0 {
		recordRegistrationError(fmt.Errorf("RegisterToolCap(%s): 至少要传一个能力位（读类请传 CapRead）", tool))
		return
	}
	for _, c := range caps {
		if _, ok := capabilityField(c); !ok {
			recordRegistrationError(fmt.Errorf("RegisterToolCap(%s): 未知能力 %q（可用能力见 AllCapabilities）", tool, string(c)))
			return
		}
	}
	cp := make([]Capability, len(caps))
	copy(cp, caps)
	toolCapRegistryMu.Lock()
	toolCapRegistry[tool] = cp
	toolCapRegistryMu.Unlock()
}

// RegisterToolConfirm 注册「该工具执行前必须先两段式确认」。
//
// 用法（只有破坏性 / 不可逆操作才注册）：
//
//	func init() {
//	    debugsrv.RegisterToolConfirm("sftp.remove", "sftp.remove")
//	    // 可逆 / 高频操作（如 apply_css）不要注册 —— 它们只受能力位约束
//	}
//
// action 是审计与 token 绑定的稳定名字，建议用「域.动词」形式（见 ConfirmableActions）。
// 传入空 action 表示显式取消该工具的确认要求（覆盖内置表）。
func RegisterToolConfirm(tool, action string) {
	tool = strings.TrimSpace(tool)
	action = strings.TrimSpace(action)
	if tool == "" {
		recordRegistrationError(fmt.Errorf("RegisterToolConfirm: 工具名不能为空"))
		return
	}
	if action == "" {
		recordRegistrationError(fmt.Errorf("RegisterToolConfirm(%s): action 不能为空（如需取消确认请用 UnregisterToolConfirm）", tool))
		return
	}
	if !strings.Contains(action, ".") {
		recordRegistrationError(fmt.Errorf("RegisterToolConfirm(%s): action %q 应形如 \"域.动词\"（如 sftp.remove）", tool, action))
		return
	}
	toolCapRegistryMu.Lock()
	toolConfirmRegistry[tool] = action
	toolCapRegistryMu.Unlock()
}

// UnregisterToolConfirm 取消某个工具的确认要求（覆盖内置表），例如某个动作后续被判定为可逆。
func UnregisterToolConfirm(tool string) {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		recordRegistrationError(fmt.Errorf("UnregisterToolConfirm: 工具名不能为空"))
		return
	}
	toolCapRegistryMu.Lock()
	// 空字符串 = 显式「不需要确认」（见 ToolConfirmAction 的约定）
	toolConfirmRegistry[tool] = ""
	toolCapRegistryMu.Unlock()
}

// RegistrationErrors 返回注册期收集到的错误（按发生顺序）。
//
// 请在测试里断言它为空：
//
//	if errs := debugsrv.RegistrationErrors(); len(errs) > 0 { t.Fatalf("%v", errs) }
func RegistrationErrors() []error {
	registrationErrMu.Lock()
	defer registrationErrMu.Unlock()
	out := make([]error, len(registrationErrs))
	copy(out, registrationErrs)
	return out
}

// ResetRegistrationsForTest 清空注册表与错误（仅供测试使用，运行期不要调用）。
func ResetRegistrationsForTest() {
	toolCapRegistryMu.Lock()
	toolCapRegistry = map[string][]Capability{}
	toolConfirmRegistry = map[string]string{}
	toolCapRegistryMu.Unlock()
	registrationErrMu.Lock()
	registrationErrs = nil
	registrationErrMu.Unlock()
}

func recordRegistrationError(err error) {
	registrationErrMu.Lock()
	registrationErrs = append(registrationErrs, err)
	registrationErrMu.Unlock()
}

// registeredToolCap 查注册表（第二个返回值表示是否命中）。
func registeredToolCap(tool string) ([]Capability, bool) {
	toolCapRegistryMu.RLock()
	defer toolCapRegistryMu.RUnlock()
	caps, ok := toolCapRegistry[tool]
	if !ok {
		return nil, false
	}
	out := make([]Capability, len(caps))
	copy(out, caps)
	return out, true
}

// registeredToolConfirm 查注册表的确认登记：
// 返回 (action, true) 表示命中（action 可能为空字符串 = 显式取消确认）。
func registeredToolConfirm(tool string) (string, bool) {
	toolCapRegistryMu.RLock()
	defer toolCapRegistryMu.RUnlock()
	a, ok := toolConfirmRegistry[tool]
	return a, ok
}

// registeredConfirmAction 判断某个 action 是否来自注册表（用于 confirmActionNeedsConfirm）。
func registeredConfirmAction(action string) (string, bool) {
	if action == "" {
		return "", false
	}
	toolCapRegistryMu.RLock()
	defer toolCapRegistryMu.RUnlock()
	for _, a := range toolConfirmRegistry {
		if a == action {
			return a, true
		}
	}
	return "", false
}

// registeredConfirmActions 返回注册表里的全部动作名（去重，供 ConfirmableActions 合并）。
func registeredConfirmActions() []string {
	toolCapRegistryMu.RLock()
	defer toolCapRegistryMu.RUnlock()
	seen := make(map[string]bool, len(toolConfirmRegistry))
	out := make([]string, 0, len(toolConfirmRegistry))
	for _, a := range toolConfirmRegistry {
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// ToolCapabilities 返回某个工具所需的能力（未知工具返回 nil + false）。
//
// 查找顺序：先注册表、后内置表 —— 同名以注册值为准（后续三路可覆盖内置工具的登记）。
func ToolCapabilities(tool string) ([]Capability, bool) {
	if caps, ok := registeredToolCap(tool); ok {
		return caps, true
	}
	caps, ok := toolCaps[tool]
	return caps, ok
}

// ToolCapabilityNames 返回工具所需能力的字符串列表（JSON 友好，供调试接口 / 审计输出）。
func ToolCapabilityNames(tool string) []string {
	caps, _ := ToolCapabilities(tool)
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}

// checkToolCaps 在 tools/call 执行前逐个校验能力位，返回第一个拒绝原因。
func (s *Server) checkToolCaps(tool string, detail string) error {
	caps, _ := ToolCapabilities(tool)
	for _, c := range caps {
		if err := s.allowCap(c, detail); err != nil {
			return err
		}
	}
	return nil
}

// capabilityField 校验能力名是否已登记（能映射到 models.DebugSettings 的某个字段）。
//
// 存在的意义：让 RegisterToolCap 能拒绝拼错的能力名（例如 "fs.remote.write " 带空格、
// 或 "ui.writes"），而不是让它静默变成一个永远不会被放行 / 永远被放行的幽灵能力。
func capabilityField(c Capability) (string, bool) {
	switch c {
	case CapRead:
		return "", true // 读类不对应开关
	case CapTerminalInput:
		return "capTerminalInput", true
	case CapUIWrite:
		return "capUiWrite", true
	case CapConfigWrite:
		return "capConfigWrite", true
	case CapSecretsRead:
		return "allowSecrets", true
	case CapSecretsWrite:
		return "capSecretWrite", true
	case CapRemoteFSWrite:
		return "capRemoteFsWrite", true
	case CapSudoCredential:
		return "capSudoCredential", true
	case CapLifecycle:
		return "capLifecycle", true
	case CapEval:
		return "allowEval", true
	}
	return "", false
}

// CapabilityOfAction 把确认动作名（如 "settings.update" / "servers.delete"）映射回能力位。
//
// 约定：动作名与能力名共用同一套字符串（settings.update → config.write 例外，见下表），
// 便于审计记录里 cap 与 action 互相对应；返回 false 表示该动作没有对应能力（读类 / 未知）。
func CapabilityOfAction(action string) (Capability, bool) {
	switch strings.TrimSpace(action) {
	case string(CapTerminalInput):
		return CapTerminalInput, true
	case string(CapUIWrite):
		return CapUIWrite, true
	case "servers.create", "servers.update", "servers.delete",
		"tunnels.create", "tunnels.update", "tunnels.delete", "tunnels.start", "tunnels.stop",
		"settings.update", "settings.reset":
		return CapConfigWrite, true
	case "credentials.create", "credentials.update", "credentials.delete":
		return CapSecretsWrite, true
	case "sftp.write", "sftp.remove", "sftp.mkdir", "sftp.rename", "sftp.upload":
		return CapRemoteFSWrite, true
	case "terminal.sudo":
		// sudo 凭证提权的确认动作名与工具名相同（登记见 sudo.go 的 init）。
		return CapSudoCredential, true
	case string(CapLifecycle), "logs.clear", "app.quit", "app.reload", "capability.enable":
		return CapLifecycle, true
	case string(CapEval):
		return CapEval, true
	}
	return "", false
}

// sortedCapNames 返回快照里的能力名（按 allCapabilities 顺序，便于阅读与测试断言）。
func sortedCapNames(snap map[Capability]bool) []string {
	out := make([]string, 0, len(snap))
	for _, c := range allCapabilities {
		if _, ok := snap[c]; ok {
			out = append(out, string(c))
		}
	}
	// 兜底：Gate 返回了表外的能力也要出现在输出里（否则调用方会以为它不存在）
	var extra []string
	for c := range snap {
		known := false
		for _, k := range allCapabilities {
			if k == c {
				known = true
				break
			}
		}
		if !known {
			extra = append(extra, string(c))
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}
