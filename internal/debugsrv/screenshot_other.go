//go:build !windows

package debugsrv

import (
	"fmt"
	"runtime"

	"ding-ssh/internal/logx"
)

// captureWindowPNG 非 Windows 平台暂不支持窗口截图。
func captureWindowPNG() ([]byte, error) {
	// 平台能力缺失属「降级」而非异常：记 WARN 说明为什么截图相关工具不可用。
	logx.Warnf("[debugsrv/screenshot] 当前平台不支持窗口截图，降级返回错误: platform=%s", runtime.GOOS)
	return nil, fmt.Errorf("窗口截图目前仅支持 Windows")
}
