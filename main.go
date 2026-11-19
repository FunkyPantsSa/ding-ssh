package main

import (
	"embed"
	goruntime "runtime"

	"ding-ssh/internal/apphost"
	"ding-ssh/internal/logfilter"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/updater"

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
	// 日志开关此时尚未从设置里读出（要等 Startup），因此这里只记 debug：
	// 用户在设置页打开日志后，本进程后续的启动步骤仍会正常输出，不会丢启动现场。
	logx.Debugf("[main] 进程启动: goos=%s goarch=%s", goruntime.GOOS, goruntime.GOARCH)
	if updater.Helper() {
		logx.Debugf("[main] 以更新器辅助进程模式运行，不启动主窗口")
		return
	}
	// 调试模式配置要在 wails.Run 之前读取：WebView2 的远程调试端口必须在启动时确定。
	boot := apphost.ReadDebugBoot()
	logx.Debugf("[main] 调试启动配置: debug=%v port=%d bindLAN=%v cdp=%v allowEval=%v",
		boot.Enabled, boot.Port, boot.BindLAN, boot.CDPEnabled, boot.AllowEval)
	app := &App{App: apphost.NewApp(boot)}

	opt := &options.App{
		Title:     "ding-ssh",
		Frameless: goruntime.GOOS == "windows",
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
		Logger:        logfilter.New(),
		OnStartup:     app.Startup,
		OnShutdown:    app.Shutdown,
		OnBeforeClose: app.BeforeClose,
		Bind: []interface{}{
			app,
		},
	}
	apphost.ApplyDebugBrowserArgs(opt, boot)

	logx.Infof("[main] 启动 Wails 主窗口: title=ding-ssh size=1280x800 min=900x600 frameless=%v",
		goruntime.GOOS == "windows")
	err := wails.Run(opt)
	// wails.Run 返回即主循环结束（正常退出或启动失败），两种都是判断「进程还在不在」的关键节点。
	logx.Infof("[main] Wails 主循环退出: err=%v", err)

	if err != nil {
		logx.Errorf("[main] Wails 运行失败: err=%v", err)
		// 保留 println：日志开关默认关闭时，这是用户在终端唯一能看到的启动失败原因。
		println("Error:", err.Error())
	}
}
