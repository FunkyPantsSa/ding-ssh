//go:build windows

package main

import (
	"ding-ssh/internal/logx"

	"github.com/wailsapp/wails/v2/pkg/options"
)

// applyDebugBrowserArgs Windows 下暂时无能为力：
// Wails v2.13 在创建 WebView2 环境前会执行 preventEnvAndRegistryOverrides()，
// 直接覆盖 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS，而 windows.Options 又没有暴露
// AdditionalBrowserArgs 字段 —— 因此无法从应用侧注入 --remote-debugging-port。
//
// 替代方案（能力等价）：
//   - 调试 API 的 /v1/eval 可在页面上下文执行任意 JS（读 DOM、xterm 内部状态）；
//   - 终端读写/滚动/重连由前端桥直接完成；
//   - 截图计划在 M2 用原生窗口捕获（PrintWindow/BitBlt）实现。
//
// 若确实需要 CDP，可对 Wails 打补丁（go.mod replace）后再在此处注入。
func applyDebugBrowserArgs(_ *options.App, boot DebugBoot) {
	if boot.Enabled && boot.CDPEnabled {
		logx.Infof("提示：WebView2 远程调试（CDP）在 Wails v2.13 下无法注入，已改用调试 API（/v1/eval）")
	}
}
