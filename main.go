package main

import (
	"embed"

	"ding-ssh/internal/logfilter"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 调试模式配置要在 wails.Run 之前读取：WebView2 的远程调试端口必须在启动时确定。
	boot := readDebugBoot()
	app := NewApp(boot)

	opt := &options.App{
		Title:     "ding-ssh",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 1},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		Logger:    logfilter.New(),
		OnStartup: app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	}
	applyDebugBrowserArgs(opt, boot)

	err := wails.Run(opt)

	if err != nil {
		println("Error:", err.Error())
	}
}
