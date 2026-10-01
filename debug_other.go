//go:build !windows

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// applyDebugBrowserArgs 非 Windows 平台不注入 WebView2 参数（CDP 仅 Windows 有效）。
func applyDebugBrowserArgs(_ *options.App, _ DebugBoot) {}
