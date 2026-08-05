// Package logfilter 提供 Wails 日志过滤：
//   - 日志开关（设置页）关闭时全部静默；
//   - 开启时输出日志，但屏蔽已知无害的框架噪音。
package logfilter

import (
	"strings"

	"ding-ssh/internal/logx"

	"github.com/wailsapp/wails/v2/pkg/logger"
)

// ignoredSubstrings 是需要过滤的已知无害日志片段。
//
// Wails v2.13.0 dev 模式中，浏览器通过 WebSocket IPC 加载页面时，注入的
// runtime bundle 会发送内部消息 "runtime:ready"。桌面窗口会在原生 IPC
// 层拦截该消息，但 devserver 的 WebSocket 处理器没有拦截，导致
// dispatcher 打出两条错误的 ERR 日志：
//
//	ERR | process message error: runtime:ready -> Unknown message from front end: runtime:ready
//	ERR | Unknown message from front end: runtime:ready
//
// 这是 Wails 上游已知问题，不影响任何功能（生产构建与桌面窗口均不出现）。
// 待上游修复后可删除本包。
var ignoredSubstrings = []string{
	"Unknown message from front end: runtime:ready",
}

// Logger 包装 Wails 默认日志器，按日志开关与噪音过滤后转发。
type Logger struct {
	base    logger.Logger
	enabled func() bool
}

// New 创建一个跟随应用日志开关的 Logger。
func New() *Logger {
	return NewWithBase(logger.NewDefaultLogger())
}

// NewWithBase 使用自定义底层日志器创建 Logger，便于测试与扩展。
func NewWithBase(base logger.Logger) *Logger {
	return &Logger{base: base, enabled: logx.Enabled}
}

// suppressed 判断消息是否应被丢弃：日志关闭或命中已知噪音。
func (l *Logger) suppressed(message string) bool {
	if l.enabled != nil && !l.enabled() {
		return true
	}
	for _, s := range ignoredSubstrings {
		if strings.Contains(message, s) {
			return true
		}
	}
	return false
}

// Print 原样输出消息，被抑制时丢弃。
func (l *Logger) Print(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Print(message)
}

// Trace 原样输出 TRACE 日志，被抑制时丢弃。
func (l *Logger) Trace(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Trace(message)
}

// Debug 原样输出 DEBUG 日志，被抑制时丢弃。
func (l *Logger) Debug(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Debug(message)
}

// Info 原样输出 INFO 日志，被抑制时丢弃。
func (l *Logger) Info(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Info(message)
}

// Warning 原样输出 WARNING 日志，被抑制时丢弃。
func (l *Logger) Warning(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Warning(message)
}

// Error 原样输出 ERROR 日志，被抑制时丢弃。
func (l *Logger) Error(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Error(message)
}

// Fatal 原样输出 FATAL 日志，被抑制时丢弃。
func (l *Logger) Fatal(message string) {
	if l.suppressed(message) {
		return
	}
	l.base.Fatal(message)
}
