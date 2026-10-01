//go:build !windows

package debugsrv

import "fmt"

// captureWindowPNG 非 Windows 平台暂不支持窗口截图。
func captureWindowPNG() ([]byte, error) {
	return nil, fmt.Errorf("窗口截图目前仅支持 Windows")
}
