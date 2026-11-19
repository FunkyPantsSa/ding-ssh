//go:build !windows

package apphost

import (
	"ding-ssh/internal/logx"

	"github.com/wailsapp/wails/v2/pkg/options"
)

// ApplyDebugBrowserArgs 非 Windows 平台不注入 WebView2 参数（CDP 仅 Windows 有效）。
func ApplyDebugBrowserArgs(_ *options.App, _ DebugBoot) {
	logx.Debugf("[apphost] 非 Windows 平台跳过 WebView2 调试参数注入")
}
