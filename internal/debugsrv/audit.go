package debugsrv

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"ding-ssh/internal/logx"
)

// 审计（AI 调用了什么、结果如何）。
//
// 与既有 [api] 调用日志的关系：两者并存、各管一半。
//   - [api] 日志落盘（internal/logx），面向「事后翻日志排查」，只有一行文本；
//   - AuditLog 在内存里保留最近 auditRingLimit 条结构化记录，面向「应用内实时查看 +
//     AI 自查（app.recent_calls）」，并且多出能力位、耗时、可撤销提示等字段。
//
// 两条硬约束：
//  1. **绝不落明文密钥**：先 logx.Redact，再对已知敏感键（password / keyContent / token …）
//     做一次 *** 替换（见 RedactArgsJSON）；
//  2. 每条记录同时通过事件总线发一条 debug.audit 事件，供前端实时追加（不必轮询）。

// AuditRecord 一条审计记录。
type AuditRecord struct {
	ID         string `json:"id"`              // 形如 a12（进程内自增）
	TS         int64  `json:"ts"`              // 毫秒时间戳
	Source     string `json:"source"`          // "mcp" | "http" | "ui"
	Tool       string `json:"tool"`            // 工具名 / HTTP 操作名
	Cap        string `json:"cap"`             // 主要能力位（读类为空）
	Args       string `json:"args"`            // 已脱敏的规范化参数 JSON
	OK         bool   `json:"ok"`              // 是否成功
	Error      string `json:"error,omitempty"` // 失败原因（中文，已脱敏）
	DurationMS int64  `json:"durationMs"`      // 耗时
	Reversible bool   `json:"reversible"`      // 是否可撤销
	// RevertHint 可撤销时的中文提示（后续 UI 用它生成「撤销」按钮的说明）。
	RevertHint string `json:"revertHint,omitempty"`
}

// auditRingLimit 环形缓冲容量（超出后丢弃最旧记录）。
const auditRingLimit = 500

// AuditLog 审计环形缓冲（并发安全，容量固定，不落盘）。
type AuditLog struct {
	mu   sync.Mutex
	seq  int
	ring []AuditRecord
}

// NewAuditLog 创建审计日志。
func NewAuditLog() *AuditLog {
	return &AuditLog{ring: make([]AuditRecord, 0, auditRingLimit)}
}

// Append 追加一条记录：补齐 ID / 时间戳 / 脱敏，返回补齐后的记录（调用方需要 ID 时使用）。
func (a *AuditLog) Append(rec AuditRecord) AuditRecord {
	if a == nil {
		return rec
	}
	if rec.TS == 0 {
		rec.TS = time.Now().UnixMilli()
	}
	rec.Args = RedactArgsJSON(rec.Args)
	rec.Error = logx.Redact(rec.Error)
	a.mu.Lock()
	a.seq++
	rec.ID = fmt.Sprintf("a%d", a.seq)
	a.ring = append(a.ring, rec)
	if len(a.ring) > auditRingLimit {
		a.ring = a.ring[len(a.ring)-auditRingLimit:]
	}
	a.mu.Unlock()
	return rec
}

// Recent 返回最近的记录（最新的在前，便于前端直接渲染列表）。
//
// limit <= 0 时取 50，最大 500；cap 与 source 为空表示不过滤（精确匹配，
// 与前端过滤器 / app.recent_calls 的参数语义一致）。
func (a *AuditLog) Recent(limit int, cap, source string) []AuditRecord {
	if a == nil {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > auditRingLimit {
		limit = auditRingLimit
	}
	cap = strings.TrimSpace(cap)
	source = strings.TrimSpace(source)
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]AuditRecord, 0, limit)
	for i := len(a.ring) - 1; i >= 0 && len(out) < limit; i-- {
		rec := a.ring[i]
		if cap != "" && rec.Cap != cap {
			continue
		}
		if source != "" && rec.Source != source {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// Len 返回当前记录数（用于测试与 app.health）。
func (a *AuditLog) Len() int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.ring)
}

// Cap 环形缓冲容量（导出给前端提示「只保留最近 N 条」）。
const AuditRingLimit = auditRingLimit

// 敏感键：即使 logx.Redact 没覆盖（例如自定义命名的字段），这里也要把值换成 ***。
//
// 与 logx 的 redact.go 保持同一份名单（password / keyContent / token 等），
// 区别是这里作用在「已序列化但可能没被 JSON 正则命中的 JSON 文本」上，做一次兜底。
var auditSensitiveKeys = []string{"password", "keyContent", "token", "privateKey", "passphrase", "secret", "apiKey", "key_content"}

// reEscapedSecretValue 匹配「JSON 字符串里被转义的引号」形式：\"password\":\"xxx\"。
// 审计参数是被 json.Marshal 过的字符串，值里可能带转义引号；两种形式都要打码。
var reEscapedSecretValue = regexp.MustCompile(`(?i)(\\?"(?:password|passwd|pwd|passphrase|keyContent|privateKey|secret|apiKey|token)\\?"\s*:\s*\\?")[^"\\]*(\\?")`)

// RedactArgsJSON 脱敏审计参数：
//  1. 先跑 logx.Redact（与日志同一套规则，覆盖 token=xxx、Bearer、PEM 私钥等）；
//  2. 再用本地的敏感键正则做一次替换（覆盖 JSON 形式与转义引号形式）；
//  3. 最后对每个敏感键做一次「兜底扫描」：只要键名出现，就把紧随其后的字符串值换成 ***。
//
// 第三步是「宁可多打码也不漏」的保险丝：前两步依赖正则的书写细节，
// 而审计记录一旦写进内存就会被 UI / AI 读到，漏一个密码的代价远大于过度打码。
func RedactArgsJSON(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	out := logx.Redact(s)
	out = reEscapedSecretValue.ReplaceAllString(out, "${1}***${2}")
	return scrubJSONSecrets(out)
}

// scrubJSONSecrets 逐键扫描 JSON 文本，把敏感键的字符串值替换为 ***。
// 不做完整 JSON 解析：审计参数可能是任意（甚至被截断的）文本，解析失败时仍需打码。
func scrubJSONSecrets(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	i := 0
	for i < len(s) {
		colon, ok := matchSensitiveKeyAt(s, i)
		if !ok {
			sb.WriteByte(s[i])
			i++
			continue
		}
		// 先原样保留 "key":（键名与冒号之间的空白也不动）。
		sb.WriteString(s[i : colon+1])
		j := colon + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		if j < len(s) && s[j] == '"' {
			sb.WriteString(`"***"`)
			j++
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					j++
					break
				}
				j++
			}
			i = j
			continue
		}
		// 非字符串值（数字 / 对象 / 数组）：不处理，继续往后扫描。
		i = colon + 1
	}
	return sb.String()
}

// matchSensitiveKeyAt 判断 s[i:] 是否正好以「敏感键名 + 引号 + 冒号」开头，
// 返回冒号所在下标（调用方据此把值整体替换掉）。
func matchSensitiveKeyAt(s string, i int) (int, bool) {
	// 键名前必须是 JSON 结构分隔符（或带转义的反斜杠），避免把正文里的 "password" 误判成键。
	if i > 0 && s[i-1] != '{' && s[i-1] != ',' && s[i-1] != ' ' && s[i-1] != '\\' {
		return 0, false
	}
	// 允许 \" 转义前缀（审计参数被 Marshal 成字符串后嵌在 JSON 里）
	start := i
	switch {
	case strings.HasPrefix(s[start:], `\"`):
		start += 2
	case s[start] == '"':
		start++
	default:
		return 0, false
	}
	rest := s[start:]
	lower := strings.ToLower(rest)
	for _, k := range auditSensitiveKeys {
		if !strings.HasPrefix(lower, strings.ToLower(k)) {
			continue
		}
		after := strings.TrimLeft(rest[len(k):], `\`)
		if !strings.HasPrefix(after, `"`) {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(after[1:]), ":") {
			continue
		}
		if colon := strings.Index(after, ":"); colon >= 0 {
			return start + len(k) + colon, true
		}
	}
	return 0, false
}

// ---- 记录的构造与发布 ----

// auditRecorder 在一次调用内累积审计字段，调用结束时由 finish 生成记录。
//
// 为什么要有它：MCP 的 tools/call 需要「能力校验失败」「参数缺失」「执行失败」等
// 多种提前返回路径都留下审计记录；用一个小结构体承载上下文，比在每个分支里手写
// Append 更不容易漏记。
type auditRecorder struct {
	log        *AuditLog
	hub        *Hub
	Source     string
	Tool       string
	Cap        Capability
	Args       map[string]any
	Reversible bool
	RevertHint string
	started    time.Time
}

func newAuditRecorder(log *AuditLog, hub *Hub, source, tool string, capCap Capability, args map[string]any) *auditRecorder {
	return &auditRecorder{log: log, hub: hub, Source: source, Tool: tool, Cap: capCap, Args: args, started: time.Now()}
}

// finish 生成并落地审计记录；err 为 nil 表示成功。
func (r *auditRecorder) finish(err error, reversible bool, revertHint string) AuditRecord {
	if r == nil {
		return AuditRecord{}
	}
	rec := AuditRecord{
		TS:         r.started.UnixMilli(),
		Source:     r.Source,
		Tool:       r.Tool,
		Cap:        string(r.Cap),
		Args:       NormalizeArgsJSON(r.Args),
		OK:         err == nil,
		DurationMS: time.Since(r.started).Milliseconds(),
		Reversible: reversible,
		RevertHint: revertHint,
	}
	if err != nil {
		rec.Error = err.Error()
	}
	out := AuditRecord{}
	if r.log != nil {
		out = r.log.Append(rec)
	} else {
		out = rec
	}
	publishAudit(r.hub, out)
	return out
}

// publishAudit 通过事件总线发一条 debug.audit 事件（前端 EventsOn('debug.audit') 实时追加）。
func publishAudit(hub *Hub, rec AuditRecord) {
	if hub == nil || rec.Tool == "" {
		return
	}
	hub.Publish(Event{
		Topic: AuditTopic,
		Name:  "debug:audit",
		Data:  mustJSON(rec),
		TS:    rec.TS,
	})
}

// AuditTopic 审计事件 topic（前端与 SSE 订阅者共用）。
const AuditTopic = "debug.audit"

// mustJSON 序列化失败时返回 null（审计事件不值得让调用方处理错误）。
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// AuditSourceValues 返回合法的来源取值（供过滤器与文档展示）。
func AuditSourceValues() []string {
	return []string{"mcp", "http", "ui"}
}
