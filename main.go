package main

import (
	"embed"
	"runtime"

	"ding-ssh/internal/apphost"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/updater"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	logx.Debugf("[main] 进程启动: goos=%s goarch=%s", runtime.GOOS, runtime.GOARCH)
	if updater.Helper() {
		logx.Debugf("[main] 以更新器辅助进程模式运行，不启动主窗口")
		return
	}

	host := apphost.NewApp(apphost.ReadDebugBoot())
	desktop := application.New(application.Options{
		Name:        "ding-ssh",
		Description: "Local-first SSH workstation",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		ShouldQuit:  func() bool { return apphost.ShouldQuit(host) },
		Services:    []application.Service{application.NewService(host)},
	})

	windowOptions := application.WebviewWindowOptions{
		Name:             "main",
		Title:            "ding-ssh",
		Width:            1280,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        runtime.GOOS == "windows",
		URL:              "/",
		EnableFileDrop:   true,
		BackgroundColour: application.NewRGB(15, 23, 42),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHidden,
			Backdrop: application.MacBackdropTranslucent,
		},
		Windows: application.WindowsWindow{
			BackdropType: application.Acrylic,
		},
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		windowOptions.BackgroundType = application.BackgroundTypeTranslucent
	}
	window := desktop.Window.NewWithOptions(windowOptions)
	apphost.ConfigureDesktop(host, desktop, window)
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		ctx := event.Context()
		if target := ctx.DropTargetDetails(); target != nil && target.ElementID != "" {
			window.EmitEvent("sftp:files-dropped", map[string]any{
				"target": target.ElementID,
				"paths":  ctx.DroppedFiles(),
			})
		}
	})

	if err := desktop.Run(); err != nil {
		logx.Errorf("[main] Wails 运行失败: err=%v", err)
		println("Error:", err.Error())
	}
}
