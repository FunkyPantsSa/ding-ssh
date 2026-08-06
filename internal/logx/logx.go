// Package logx 提供受设置页日志开关控制的应用日志。
// 开关关闭时所有日志静默，开启时输出到运行终端（stdout）。
package logx

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
)

// 全局日志开关，由设置页实时切换，应用内所有日志统一读取。
var enabled atomic.Bool

// noiseSubstrings 需要从标准库 log 输出中过滤的无害框架噪音。
var noiseSubstrings = []string{
	// Go net/http 在闲置 keep-alive 连接上收到意外响应时的内部提示，
	// 常见于 Wails 内嵌 WebView 服务器与 WKWebView 连接复用，属框架噪音。
	"Unsolicited response received on idle HTTP channel",
}

// stdLogWriter 将标准库 log 输出按日志开关过滤后转发到 stdout。
type stdLogWriter struct{}

// Write 实现 io.Writer：日志关闭时全部静默，开启时过滤已知噪音。
func (stdLogWriter) Write(p []byte) (int, error) {
	if !enabled.Load() {
		return len(p), nil
	}
	msg := string(p)
	for _, s := range noiseSubstrings {
		if strings.Contains(msg, s) {
			return len(p), nil
		}
	}
	return os.Stdout.Write(p)
}

// init 接管标准库 log 输出，使第三方库（如 net/http）的内部日志同样
// 受日志开关与噪音过滤控制，避免框架噪音刷屏。
func init() {
	log.SetOutput(stdLogWriter{})
	log.SetFlags(log.LstdFlags)
}

// SetEnabled 设置日志开关：true 输出日志，false 静默。
func SetEnabled(v bool) {
	enabled.Store(v)
}

// Enabled 返回当前日志开关状态。
func Enabled() bool {
	return enabled.Load()
}

// Debugf 输出 DEBUG 日志。
func Debugf(format string, args ...interface{}) {
	if !Enabled() {
		return
	}
	fmt.Printf("[DEBUG] "+format+"\n", args...)
}

// Infof 输出 INFO 日志。
func Infof(format string, args ...interface{}) {
	if !Enabled() {
		return
	}
	fmt.Printf("[INFO] "+format+"\n", args...)
}

// Errorf 输出 ERROR 日志。
func Errorf(format string, args ...interface{}) {
	if !Enabled() {
		return
	}
	fmt.Printf("[ERROR] "+format+"\n", args...)
}
