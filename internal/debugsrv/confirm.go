package debugsrv

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 两段式确认（破坏性操作）。
//
// 设计原则（与 caps.go 顶部注释一致，务必分清）：
//   - 能力位 = 「允不允许这类操作」（可持续放行、可反复调用）；
//   - 两段式 = 「不可逆操作前再想一次」（一次性 token、绑定本次参数）。
//
// 因此**只有破坏性 / 不可逆操作**才进下面的清单：删服务器 / 删凭据 / 改隧道 /
// SFTP 写删 / 改设置 / 清日志 / 退出重载 / 开启敏感能力。
// 可逆且高频的操作（终端输入、界面写入、eval）**只受能力位约束**，不要求 token ——
// 否则 send_input 要 token 会让 terminal.run/expect 这类自动化原语不可用，
// eval / UI 写入要 token 会让「改一点看一眼」的迭代循环不可用。
//
// 为什么需要两段式：调试接口 / MCP 的调用方（AI）能一步就把服务器删掉、把 SFTP 文件删掉、
// 把应用退掉。HTTP token 只证明「调用方知道 token」，不等于「用户此刻同意这次删除」。
// 破坏性操作走两步：
//
//	1) confirm.prepare {action, args, summary?} → {token, expiresAt, ...}
//	   —— 把「要做什么 + 具体参数」登记成一次待确认操作，并拿到一次性 token；
//	2) confirm.commit {token}（或写工具本身带 token=…）→ 执行
//	   —— 校验 token 与本次 action+args 完全一致后立即消费掉。
//
// 关键性质（后续三路接入时必须遵守）：
//   - 一次性：Commit 成功后 token 立即失效，重复 commit 一律失败；
//   - 绑参数：token 的哈希覆盖 action 与规范化后的 args，改了参数（哪怕只差一个空格）就失败；
//   - 有期限：默认 TTL 120s，过期即失效；
//   - 读类 / 可逆操作不参与：不需要确认的 action 直接拒绝 prepare（避免把确认当成普通流程）。

// confirmActionNeedsConfirm 判断 action 是否需要两段式确认。
//
// 这份清单是**约定文档**，后续三路按它接入（新增时优先用 RegisterToolConfirm 注册，
// 不要直接改本函数）：
//
//	servers.create/update/delete、credentials.create/update/delete、
//	tunnels.create/update/delete/start/stop、
//	sftp.write/remove/mkdir/rename/upload、
//	terminal.sudo（sudo.credential 能力：用保存的密码提权，等价于交出 root）、
//	settings.update/settings.reset、logs.clear、
//	app.quit、app.reload、capability.enable
//
// 注意 capability.enable 也在列表里：开启敏感能力（如 fs.remote.write / lifecycle / sudo.credential）
// 本身就是破坏性动作 —— 用户明确要求「SFTP 写/删打开需要多次确认」。
// terminal.input / ui.write / eval **不在**列表里（见文件顶部设计原则）。
// terminal.sudo 走 RegisterToolConfirm 注册（见 sudo.go），因此不出现在下面的内置 switch 里。
func confirmActionNeedsConfirm(action string) bool {
	action = strings.TrimSpace(action)
	if _, ok := registeredConfirmAction(action); ok {
		return true
	}
	switch action {
	case
		"servers.create", "servers.update", "servers.delete",
		"credentials.create", "credentials.update", "credentials.delete",
		"tunnels.create", "tunnels.update", "tunnels.delete", "tunnels.start", "tunnels.stop",
		"sftp.write", "sftp.remove", "sftp.mkdir", "sftp.rename", "sftp.upload",
		"settings.update", "settings.reset",
		"logs.clear",
		"app.quit", "app.reload",
		"capability.enable":
		return true
	}
	return false
}

// builtinConfirmActions 内置的「需要确认」动作清单（与 confirmActionNeedsConfirm 的
// switch 一一对应）。单独列出来是为了让 ConfirmableActions() 能输出完整清单
// （注册表里的动作会合并进来）。
var builtinConfirmActions = []string{
	"app.quit", "app.reload",
	"capability.enable",
	"credentials.create", "credentials.delete", "credentials.update",
	"sftp.mkdir", "sftp.remove", "sftp.rename", "sftp.upload", "sftp.write",
	"servers.create", "servers.delete", "servers.update",
	"settings.reset", "settings.update",
	"tunnels.create", "tunnels.delete", "tunnels.start", "tunnels.stop", "tunnels.update",
	"logs.clear",
}

// ConfirmableActions 返回全部需要两段式确认的动作名（内置 + 注册，字典序去重），
// 供文档 / 测试 / app.permissions 展示。
func ConfirmableActions() []string {
	seen := make(map[string]bool, len(builtinConfirmActions))
	out := make([]string, 0, len(builtinConfirmActions))
	for _, a := range builtinConfirmActions {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, a := range registeredConfirmActions() {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// toolConfirmActions 内置的工具名 → 该工具执行时对应的确认 action。
//
// 为什么需要反向映射：工具名（MCP 对外的名字，如 update_settings）与 action 名
// （审计 / 确认用的稳定名字，如 settings.update）是两套命名，必须有一处显式对应，
// 否则「工具调用时校验 token」就没法知道该比哪个 action。
//
// 后续三路新增写工具时请用 RegisterToolConfirm 注册，不要改这张表。
var toolConfirmActions = map[string]string{
	"update_settings": "settings.update",
	"set_log_options": "settings.update",
	"clear_logs":      "logs.clear",
}

// ToolConfirmAction 返回工具对应的确认 action（第二个返回值为 false 表示无需确认）。
//
// 查找顺序：先注册表、后内置表 —— 注册表让后续三路能在**各自的文件**里登记，
// 不必同时改本文件（避免并行开发时的冲突）。
func ToolConfirmAction(tool string) (string, bool) {
	if a, ok := registeredToolConfirm(tool); ok {
		return a, a != ""
	}
	a, ok := toolConfirmActions[tool]
	return a, ok
}

// ToolConfirmActionByCap 返回某个能力位对应的确认 action（能力 → 动作名 的反向映射）。
//
// 用于自动生成工具描述里的「能力要求：…（需先 confirm.prepare 提供 token=…）」。
// 注意这里是「该能力下最典型的破坏性动作」示意，不代表该能力下所有工具都需要确认
// （例如 terminal.input 能力下的 send_input 就不需要 token）。
func ToolConfirmActionByCap(cap Capability) (string, bool) {
	switch cap {
	case CapConfigWrite:
		return "settings.update", true
	case CapSecretsWrite:
		return "credentials.update", true
	case CapRemoteFSWrite:
		return "sftp.write", true
	case CapSudoCredential:
		// sudo.credential 下只有一个工具（terminal.sudo），动作名与工具名相同。
		return "terminal.sudo", true
	case CapLifecycle:
		return "logs.clear", true
	}
	return "", false
}

// ActionNeedsConfirm 对外暴露「该 action 是否需要确认」（供宿主执行器与 UI 复用）。
func ActionNeedsConfirm(action string) bool { return confirmActionNeedsConfirm(action) }

// ErrConfirmRequired 未提供 token 时返回的哨兵错误（调用方据此拼中文提示）。
var ErrConfirmRequired = errors.New("需要两段式确认")

// PendingConfirm 一次已登记、等待 commit 的确认操作。
type PendingConfirm struct {
	Token     string         `json:"token"`
	Action    string         `json:"action"`
	Args      map[string]any `json:"args"`
	Summary   string         `json:"summary"`
	ExpiresAt int64          `json:"expiresAt"` // 毫秒时间戳
	CreatedAt int64          `json:"createdAt"`
}

type confirmEntry struct {
	pending PendingConfirm
	hash    string
}

// ConfirmManager 管理待确认操作：登记（Prepare）→ 校验并消费（Commit / Take）。
//
// 并发安全；TTL 到期即失效（惰性清理，不需要后台 goroutine）。
type ConfirmManager struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]confirmEntry
}

// DefaultConfirmTTL 默认确认有效期：120 秒（够 AI 把摘要展示给用户并拿到回应）。
const DefaultConfirmTTL = 120 * time.Second

// NewConfirmManager 创建确认管理器；ttl <= 0 时使用 DefaultConfirmTTL。
func NewConfirmManager(ttl time.Duration) *ConfirmManager {
	if ttl <= 0 {
		ttl = DefaultConfirmTTL
	}
	return &ConfirmManager{ttl: ttl, entries: make(map[string]confirmEntry)}
}

// TTL 返回当前有效期（供调用方在提示文案里说明）。
func (m *ConfirmManager) TTL() time.Duration {
	if m == nil {
		return DefaultConfirmTTL
	}
	return m.ttl
}

// Prepare 登记一次待确认操作，返回一次性 token 与过期时间（毫秒）。
//
// summary 是给用户 / AI 展示的中文摘要（可为空，调用方会补一个默认摘要）；
// args 为已规范化的参数（排序键），Commit 时会重新规范化后比对哈希。
func (m *ConfirmManager) Prepare(action string, args map[string]any, summary string) (string, int64) {
	action = strings.TrimSpace(action)
	norm := NormalizeConfirmArgs(args)
	token := newConfirmToken()
	now := time.Now()
	exp := now.Add(m.ttl)
	if strings.TrimSpace(summary) == "" {
		summary = defaultConfirmSummary(action, norm)
	}
	entry := confirmEntry{
		pending: PendingConfirm{
			Token:     token,
			Action:    action,
			Args:      norm,
			Summary:   summary,
			ExpiresAt: exp.UnixMilli(),
			CreatedAt: now.UnixMilli(),
		},
		hash: confirmHash(action, norm),
	}
	m.mu.Lock()
	m.sweepLocked(now)
	m.entries[token] = entry
	m.mu.Unlock()
	return token, entry.pending.ExpiresAt
}

// Commit 校验并消费 token：成功返回 nil，并删除该 token（一次性）。
//
// 失败原因一律是中文，可直接回给 AI：
//   - token 为空 / 未知；
//   - 已过期；
//   - action 不匹配（说明拿 A 的 token 去执行 B）；
//   - args 与登记时不一致（参数被篡改）。
func (m *ConfirmManager) Commit(token, action string, args map[string]any) error {
	_, err := m.Take(token, action, args)
	return err
}

// Take 与 Commit 相同，但同时返回被消费的待确认信息（执行器需要它的 action/args/summary）。
func (m *ConfirmManager) Take(token, action string, args map[string]any) (PendingConfirm, error) {
	token = strings.TrimSpace(token)
	action = strings.TrimSpace(action)
	if token == "" {
		return PendingConfirm{}, errors.New("缺少 token：请先调用 confirm.prepare 获取 token")
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(now)
	entry, ok := m.entries[token]
	if !ok {
		return PendingConfirm{}, errors.New("token 无效、已被使用或已过期：每个 token 只能使用一次且有效期有限，请重新调用 confirm.prepare")
	}
	if now.UnixMilli() > entry.pending.ExpiresAt {
		delete(m.entries, token)
		return PendingConfirm{}, fmt.Errorf("token 已过期：确认有效期 %s，请重新调用 confirm.prepare", m.ttl)
	}
	if entry.pending.Action != action {
		return PendingConfirm{}, fmt.Errorf("token 与当前操作不匹配：该 token 用于 %q，本次操作是 %q（请重新 confirm.prepare）",
			entry.pending.Action, action)
	}
	if got := confirmHash(action, NormalizeConfirmArgs(args)); got != entry.hash {
		return PendingConfirm{}, fmt.Errorf("参数与确认时不一致：token 绑定的是确认时的参数（%s），本次是 %s，改动后必须重新 confirm.prepare",
			NormalizeArgsJSON(entry.pending.Args), NormalizeArgsJSON(args))
	}
	delete(m.entries, token) // 一次性：无论后续执行成功与否都不再可用
	return entry.pending, nil
}

// Peek 查看某个 token 对应的待确认信息（不消费），用于调试 / 测试。
func (m *ConfirmManager) Peek(token string) (PendingConfirm, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[strings.TrimSpace(token)]
	if !ok {
		return PendingConfirm{}, false
	}
	if time.Now().UnixMilli() > entry.pending.ExpiresAt {
		return PendingConfirm{}, false
	}
	return entry.pending, true
}

// PendingCount 返回当前未过期的待确认数量（供 app.health / 测试观察）。
func (m *ConfirmManager) PendingCount() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(time.Now())
	return len(m.entries)
}

// sweepLocked 惰性清理过期项（调用方必须持有锁）。
func (m *ConfirmManager) sweepLocked(now time.Time) {
	for tok, entry := range m.entries {
		if now.UnixMilli() > entry.pending.ExpiresAt {
			delete(m.entries, tok)
		}
	}
}

// NormalizeConfirmArgs 规范化确认参数：排序键、字符串 trim、丢弃 nil / 非有限数字。
//
// 规范化的目的：让「同一组参数」永远得到同一个哈希 —— AI 两次传 JSON 的键顺序、
// 空格可能有微小差异，但语义相同就必须能通过校验；反过来，任何语义变化都必须失败。
func NormalizeConfirmArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		// token 自身不参与哈希：它由 prepare 生成，commit 时才知道，且与「操作内容」无关。
		if k == "token" {
			continue
		}
		if v == nil {
			continue
		}
		out[k] = normalizeConfirmValue(v)
	}
	if len(out) == 0 {
		return map[string]any{}
	}
	return out
}

// normalizeConfirmValue 单值规范化：字符串去首尾空白；数字统一为 float64 的规范文本；数组保持顺序。
func normalizeConfirmValue(v any) any {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return normalizeNumber(t)
	case int:
		return normalizeNumber(float64(t))
	case int64:
		return normalizeNumber(float64(t))
	case bool:
		return t
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, normalizeConfirmValue(item))
		}
		return out
	case map[string]any:
		return NormalizeConfirmArgs(t)
	default:
		return t
	}
}

// normalizeNumber 把数字统一成「不带多余小数位」的规范形式：
// JSON 解码得到的始终是 float64，1 与 1.0 必须视为同一个值。
// 返回 json.Number 而不是 string，保证值在规范化后的 JSON 里仍然是数字。
func normalizeNumber(f float64) any {
	return json.Number(strconv.FormatFloat(f, 'f', -1, 64))
}

// NormalizeArgsJSON 把参数序列化成唯一的规范化 JSON 文本（键已排序），
// 用于确认哈希与审计记录里的 args 字段。
func NormalizeArgsJSON(args map[string]any) string {
	norm := NormalizeConfirmArgs(args)
	if len(norm) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(norm))
	for k := range norm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(norm[k])
		if err != nil {
			vb = []byte(`"?"`)
		}
		sb.Write(kb)
		sb.WriteByte(':')
		sb.Write(vb)
	}
	sb.WriteByte('}')
	return sb.String()
}

// confirmHash 计算 action + 规范化参数 的 SHA-256（十六进制）。
func confirmHash(action string, norm map[string]any) string {
	sum := sha256.Sum256([]byte(action + "\x00" + NormalizeArgsJSON(norm)))
	return hex.EncodeToString(sum[:])
}

// newConfirmToken 生成一次性 token（16 字节随机数的十六进制）。
func newConfirmToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("confirm-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// SettingsPatchArgs 把调用参数归一化成「设置补丁」形状（{"settings": {...}}）。
//
// 为什么需要：审计记录与设置撤销快照必须用同一个键，而同一个语义有两种调用形状：
//   - MCP 的 update_settings：{"settings": {...}}（还可能有 token）
//   - HTTP 的 PUT /v1/settings：直接一份设置补丁 {"uiScale": 120}
//
// 归一到前者后，两侧算出的键一致（撤销功能与 confirm 绑定都依赖它）。
func SettingsPatchArgs(args map[string]any) map[string]any {
	if inner, ok := args["settings"].(map[string]any); ok {
		return map[string]any{"settings": inner}
	}
	if raw, ok := args["settings"].(json.RawMessage); ok {
		var inner map[string]any
		if err := json.Unmarshal(raw, &inner); err == nil {
			return map[string]any{"settings": inner}
		}
	}
	// 没有 settings 字段：把整份参数当成设置补丁（HTTP 直接 PUT 的形状）。
	return map[string]any{"settings": args}
}

// defaultConfirmSummary 未提供 summary 时的中文默认摘要。
func defaultConfirmSummary(action string, args map[string]any) string {
	if len(args) == 0 {
		return fmt.Sprintf("即将执行破坏性操作 %s（无参数）", action)
	}
	return fmt.Sprintf("即将执行破坏性操作 %s，参数：%s", action, NormalizeArgsJSON(args))
}
