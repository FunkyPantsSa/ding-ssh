// 日志脱敏：把日志里可能出现的凭据替换为 ***，避免调用日志 / 诊断包泄露密钥。
//
// 覆盖的场景（与设置页「日志」页的说明保持一致）：
//   - token=xxx（URL 查询串 / key=value 形式，含 access_token）
//   - "password":"xxx" 等 JSON 字段（password / keyContent / privateKey / secret ...）
//   - Authorization: Bearer xxx
//   - -----BEGIN ... PRIVATE KEY----- 整段（含被截断的只有 BEGIN 行的情况）
//
// 脱敏只做「明显是凭据」的替换，不做全文加密：日志的可读性同样是排查问题的前提。
package logx

import (
	"regexp"
	"strings"
)

// redacted 替换后的占位符。
const redacted = "***"

var (
	// rePrivateKeyBlock PEM 私钥整段（BEGIN…END，跨行匹配）。
	// 结尾用 (?:END|$) 兜住「日志只截到 BEGIN 段」的情况，避免把后续内容全吃掉后又漏出密钥。
	rePrivateKeyBlock = regexp.MustCompile(`(?s)-----BEGIN[^\n]*PRIVATE KEY-----.*?(?:-----END[^\n]*PRIVATE KEY-----|$)`)
	// rePrivateKeyHeader 仅剩 BEGIN 行时的兜底（例如日志行被截断）。
	rePrivateKeyHeader = regexp.MustCompile(`-----BEGIN[^\n]*PRIVATE KEY-----`)
	// reBearer Authorization 头里的 Bearer 凭据。
	reBearer = regexp.MustCompile(`(?i)(Authorization\s*:\s*Bearer\s+)\S+`)
	// reJSONSecret JSON 字符串字段：保留键名，只把值换成 ***。
	reJSONSecret = regexp.MustCompile(`(?i)("(?:password|passwd|pwd|passphrase|keyContent|privateKey|secret|apiKey|token)"\s*:\s*")[^"]*(")`)
	// reAssignSecret key=value / key: value 形式的敏感赋值（含 access_token）。
	reAssignSecret = regexp.MustCompile(`(?i)([?&\s"']?(?:access_)?(?:token|password|passwd|pwd|secret|apiKey)\s*[=:]\s*)([^\s&"',;)}\]]+)`)
)

// sensitiveMarkers 快速路径的标记表：只有命中其中之一才会进入慢路径跑正则。
//
// 为什么需要它：终端逐行落盘是热路径，而绝大多数日志行不含凭据。
// 先做一次标记预筛（字符串搜索），命中才进入正则慢路径，
// 普通日志行的开销因此比跑 5 条正则低一个数量级。
//
// 该表必须覆盖所有正则会命中的关键字，否则会漏掉本该打码的行，
// 因此与上面的正则一一对应：
//   - reJSONSecret 的键名：password/passwd/pwd/passphrase/keyContent/privateKey/secret/apiKey/token
//   - reAssignSecret 的键名：access_token/token/password/passwd/pwd/secret/apiKey
//   - reBearer 的 Authorization / Bearer
//   - PEM 私钥的 BEGIN 与 PRIVATE KEY
//
// 全部按小写比较，因此大小写混写的 keyContent/privateKey/apiKey 也能命中。
var sensitiveMarkers = []string{
	"token",
	"password", "passwd", "pwd", "passphrase",
	"secret", "apikey", "keycontent", "privatekey", "private key",
	"bearer", "authorization",
	"-----begin",
}

// Redact 返回脱敏后的字符串；不含敏感内容时原样返回。
//
// 实现是「快速路径 + 命中才慢路径」：先用 hasSensitiveMarker 做一次大小写无关的
// 标记预筛（普通日志行在这里就原样返回，完全不碰正则），只有命中标记的字符串
// 才会依次跑 5 条正则做实际替换。
//
// 该函数是无副作用的纯文本处理，可安全用于并发日志调用；替换保留上下文与键名，
// 因此脱敏后的日志依然可以直接搜索关键字（如 token=***）。
// 注意私钥整段正则会跨行匹配（只有 BEGIN 行时会一路吃到文本末尾，含结尾换行），
// 需要保持「一行仍是一行」的调用方请用输出管道里的 redactLine。
// 替换是幂等的：对已脱敏文本再次 Redact 不会产生 *** 套娃，
// 所以「调用方先脱敏 + 输出管道再脱敏」叠加是安全的。
func Redact(s string) string {
	if s == "" || !hasSensitiveMarker(s) {
		return s
	}
	// 顺序固定：先整段私钥（跨行），再逐项替换其余模式。
	out := rePrivateKeyBlock.ReplaceAllString(s, redacted)
	out = rePrivateKeyHeader.ReplaceAllString(out, redacted)
	out = reBearer.ReplaceAllString(out, "${1}"+redacted)
	out = reJSONSecret.ReplaceAllString(out, "${1}"+redacted+"${2}")
	out = reAssignSecret.ReplaceAllString(out, "${1}"+redacted)
	return out
}

// hasSensitiveMarker 做大小写无关的标记预筛，即 Redact 的快速路径。
// 返回 false 表示「没有任何正则会命中」，可以直接原样返回。
//
// 实现用 strings.ToLower + strings.Contains：Contains 走的是 SIMD 汇编，
// 实测比手写的逐字节折叠比较更快（含中文的行会因此多一次 80B 的小分配，
// 但相对慢路径的正则开销可以忽略）。参考值：普通行 ~0.5µs，命中标记的慢路径 ~7.6µs。
func hasSensitiveMarker(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	for _, m := range sensitiveMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
