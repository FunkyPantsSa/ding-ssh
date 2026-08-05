// Package logx 提供受设置页日志开关控制的应用日志。
// 开关关闭时所有日志静默，开启时输出到运行终端（stdout）。
package logx

import (
	"fmt"
	"sync/atomic"
)

// 全局日志开关，由设置页实时切换，应用内所有日志统一读取。
var enabled atomic.Bool

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
