// Package logx 提供受设置页日志开关控制的应用日志。
//
// 输出通道有两个，各自独立（设置页上是两个并列开关）：
//   - 控制台：受 SetEnabled 控制，关闭后终端完全静默（保持历史行为）；
//   - 日志文件：受 SetFileEnabled 控制，落盘到 LogDir()（%AppData%\ding-ssh\logs）。
//
// 因此「关掉控制台 + 打开文件日志」仍会正常落盘，只有两者都关才完全不留痕；
// 每条日志写到所有已开启的通道（见 writeBoth）。任一通道开启才会格式化日志，
// 两路都关时立即返回，避免无谓开销。
//
// 例外是 APILogf（MCP/调试调用日志）：它由 SetAPILogEnabled 独立控制，
// 不受总开关与文件开关影响，便于在关闭常规日志时单独抓取调用记录。
//
// 级别过滤由 SetLevel 控制，默认 info（Debugf 在 info 级别下不输出）。
package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// level 日志级别，数值越大越严重。
// 用未导出类型是为了避免与查询函数 Level() 重名（同一包内类型与函数共用标识符空间），
// 同时把「级别名 → 取值」的映射集中在 String/parseLevel，防止设置页与代码各写一份。
type level int32

const (
	lvDebug level = iota
	lvInfo
	lvWarn
	lvError
)

// String 返回级别名（小写），与 SetLevel 接受的取值、设置页持久化的取值一致。
func (l level) String() string {
	switch l {
	case lvDebug:
		return "debug"
	case lvWarn:
		return "warn"
	case lvError:
		return "error"
	default:
		return "info"
	}
}

// parseLevel 把设置页传来的级别名解析为内部级别。
// 未知取值回落 info：设置页脏数据不应让日志全丢（debug 语义）或全放（error 语义）。
func parseLevel(name string) level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return lvDebug
	case "warn", "warning":
		return lvWarn
	case "error":
		return lvError
	default:
		return lvInfo
	}
}

var (
	// enabled 「运行日志（控制台）」开关，由设置页实时切换。
	// 只影响控制台输出：文件通道由 SetFileEnabled 独立控制。
	enabled atomic.Bool
	// apiLogEnabled MCP/调试调用日志开关，与总开关互相独立，默认关闭：
	// 抓取 API 调用通常只在排查问题时临时开启，不应被常规日志的取舍连带关闭。
	apiLogEnabled atomic.Bool
	// currentLevel 当前最低输出级别。零值不是 info，因此在 init 中显式设置。
	currentLevel atomic.Int32
)

// noiseSubstrings 需要从标准库 log 输出中过滤的无害框架噪音。
var noiseSubstrings = []string{
	// Go net/http 在闲置 keep-alive 连接上收到意外响应时的内部提示，
	// 常见于 Wails 内嵌 WebView 服务器与 WKWebView 连接复用，属框架噪音。
	"Unsolicited response received on idle HTTP channel",
}

// stdLogWriter 将标准库 log 输出按日志开关过滤后转发到控制台与日志文件。
type stdLogWriter struct{}

// Write 实现 io.Writer：两路通道都关闭时全部静默，否则过滤已知噪音后转发。
func (stdLogWriter) Write(p []byte) (int, error) {
	if !outputActive() {
		return len(p), nil
	}
	msg := string(p)
	for _, s := range noiseSubstrings {
		if strings.Contains(msg, s) {
			return len(p), nil
		}
	}
	// 第三方库经标准库 log 打出的内容也一并落盘，便于用户在日志页复现问题。
	writeBoth(msg)
	return len(p), nil
}

// init 接管标准库 log 输出，使第三方库（如 net/http）的内部日志同样
// 受日志开关与噪音过滤控制，避免框架噪音刷屏。
func init() {
	currentLevel.Store(int32(lvInfo))
	log.SetOutput(stdLogWriter{})
	log.SetFlags(log.LstdFlags)
}

// SetEnabled 设置「运行日志（控制台）」开关：true 输出到控制台，false 控制台静默。
// 与文件日志（SetFileEnabled）互相独立，关掉控制台不影响落盘。
func SetEnabled(v bool) {
	enabled.Store(v)
}

// Enabled 返回控制台日志开关状态。
func Enabled() bool {
	return enabled.Load()
}

// SetLevel 设置最低输出级别（debug/info/warn/error，忽略大小写与首尾空白）。
// 非法取值回落 info，避免设置页写入脏数据后日志行为不可预期。
func SetLevel(name string) {
	currentLevel.Store(int32(parseLevel(name)))
}

// Level 返回当前最低输出级别名（小写）。
func Level() string {
	return level(currentLevel.Load()).String()
}

// SetAPILogEnabled 开关调用日志（MCP/调试用），默认关闭。
func SetAPILogEnabled(v bool) error {
	apiLogEnabled.Store(v)
	return nil
}

// APILogEnabled 返回调用日志开关状态。
func APILogEnabled() bool {
	return apiLogEnabled.Load()
}

// outputActive 判断是否还有任一输出通道开启。
// 控制台只看 SetEnabled，文件只看 SetFileEnabled，两者互不牵连
// （「关掉控制台 + 打开文件日志」必须仍能落盘）；都关时上层直接返回，
// 连格式化都不做，做到完全不留痕。
func outputActive() bool {
	return enabled.Load() || FileEnabled()
}

// levelEnabled 判断某级别是否达到当前最低输出级别。
func levelEnabled(lv level) bool {
	return lv >= level(currentLevel.Load())
}

// logf 统一输出入口：先判通道与级别，再一次性写出（控制台 + 文件）。
func logf(lv level, prefix, format string, args ...interface{}) {
	if !outputActive() || !levelEnabled(lv) {
		return
	}
	writeBoth(prefix + fmt.Sprintf(format, args...) + "\n")
}

// writeBoth 把一行（已含换行）日志写到所有已开启的通道：
// 控制台受 SetEnabled 控制，文件受 SetFileEnabled 控制，两路各自独立判定。
// 写出前统一过一次凭据脱敏（redactLine）：token/密码/私钥一律打码，
// 第三方库经标准库 log 进来的内容也走这里，同样被打码。
// 顺序固定为控制台在前：即使文件写入失败，终端也能完整看到日志。
// 写入错误一律忽略——终端/磁盘不可写属于宿主环境问题，不应影响业务逻辑。
func writeBoth(line string) {
	line = redactLine(line)
	if enabled.Load() {
		_, _ = os.Stdout.WriteString(line)
	}
	writeFile(line)
}

// redactLine 对一行日志脱敏，并保证行尾换行不被吃掉。
//
// 为什么要单独保留换行：Redact 的私钥整段正则在「只有 BEGIN 行、没有 END」时
// 会一路匹配到文本末尾（含结尾的 \n），直接替换会把换行一起换成 ***，
// 导致该行与下一行粘连、日志文件的行结构被破坏。这里把换行摘出来最后补回，
// 从而在不改动 Redact 行为的前提下保证「一行仍是一行」。
//
// 未发生替换时直接返回原串（不额外拷贝），不给热路径增加每行一次分配。
func redactLine(line string) string {
	if !strings.HasSuffix(line, "\n") {
		return Redact(line)
	}
	body := line[:len(line)-1] // 仅切片，不分配
	if out := Redact(body); out != body {
		return out + "\n"
	}
	return line
}

// Debugf 输出 DEBUG 日志（默认 info 级别下不输出，需 SetLevel("debug")）。
func Debugf(format string, args ...interface{}) {
	logf(lvDebug, "[DEBUG] ", format, args...)
}

// Infof 输出 INFO 日志。
func Infof(format string, args ...interface{}) {
	logf(lvInfo, "[INFO] ", format, args...)
}

// Warnf 输出 WARN 日志。
func Warnf(format string, args ...interface{}) {
	logf(lvWarn, "[WARN] ", format, args...)
}

// Errorf 输出 ERROR 日志。
func Errorf(format string, args ...interface{}) {
	logf(lvError, "[ERROR] ", format, args...)
}

// APILogf 输出调用日志（MCP/调试接口用），前缀 [api]，仅当 SetAPILogEnabled(true) 时输出。
// 该通道独立于总开关：调试模式下即使常规日志关闭，也能单独抓取 API 调用，
// 否则「打开调用日志却什么都看不到」会让人以为功能失效。
// 是否落盘仍遵循 SetFileEnabled（与普通日志共用同一条文件通道）。
func APILogf(format string, args ...interface{}) {
	if !APILogEnabled() {
		return
	}
	// 调用方（如 debug.go 的 apiLogf）通常已脱敏过一次；这里再脱一次是防御性的，
	// Redact 幂等，不会产生 *** 套娃（也不会破坏已打码的行）。
	line := redactLine("[api] " + fmt.Sprintf(format, args...) + "\n")
	_, _ = os.Stdout.WriteString(line)
	writeFile(line)
}
