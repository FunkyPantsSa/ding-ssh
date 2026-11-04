package main

import (
	"embed"

	"ding-ssh/internal/apphost"
	"ding-ssh/internal/logfilter"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// App 是绑定给 Wails 的根包门面。实现位于 internal/apphost，
// 这里仅保留嵌入以维持前端稳定的 wailsjs/go/main/App 命名空间。
type App struct {
	*apphost.App
}

func main() {
	// 调试模式配置要在 wails.Run 之前读取：WebView2 的远程调试端口必须在启动时确定。
	boot := apphost.ReadDebugBoot()
	app := &App{App: apphost.NewApp(boot)}

	opt := &options.App{
		Title:     "ding-ssh",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 0},
		Mac: &mac.Options{
			// Keep the themed content behind the native controls without the extra
			// toolbar height added by HiddenInset; this matches the 28px web titlebar.
			TitleBar:             mac.TitleBarHidden(),
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
		Windows: &windows.Options{WebviewIsTransparent: true, WindowIsTranslucent: true, BackdropType: windows.Acrylic},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		Logger:     logfilter.New(),
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		Bind: []interface{}{
			app,
		},
	}
	apphost.ApplyDebugBrowserArgs(opt, boot)

	err := wails.Run(opt)

	if err != nil {
		println("Error:", err.Error())
	}
}
