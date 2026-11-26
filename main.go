package main

import (
	"crypto/sha256"
	"ding-ssh/internal/appdir"
	"embed"
	"fmt"
	"os"
	"runtime"
	"time"

	"ding-ssh/internal/apphost"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/updater"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	logx.Debugf("[main] 进程启动: goos=%s goarch=%s", runtime.GOOS, runtime.GOARCH)
	if updater.Helper() {
		logx.Debugf("[main] 以更新器辅助进程模式运行，不启动主窗口")
		return
	}

	boot := apphost.ReadDebugBoot()
	host := apphost.NewApp(boot)
	notifyService := notifications.New()
	dockService := dock.New()

	sessionSvc := apphost.NewSessionService(host)
	sftpSvc := apphost.NewSFTPService(host)
	tunnelSvc := apphost.NewTunnelService(host)
	settingsSvc := apphost.NewSettingsService(host)
	debugSvc := apphost.NewDebugService(host)
	updateSvc := apphost.NewUpdateService(host)

	var window *application.WebviewWindow
	uniqueID := "com.funkypantssa.ding-ssh"
	if appdir.IsDevOverride() {
		uniqueID += fmt.Sprintf(".test-%x", sha256.Sum256([]byte(appdir.ConfigDir())))
	}

	desktop := application.New(application.Options{
		Name:        "ding-ssh",
		Description: "Local-first SSH workstation",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		Windows:     apphost.DebugWindowsOptions(boot),
		ShouldQuit:  func() bool { return apphost.ShouldQuit(host) },
		Services: []application.Service{
			application.NewService(host),
			application.NewService(notifyService),
			application.NewService(dockService),
			application.NewService(sessionSvc),
			application.NewService(sftpSvc),
			application.NewService(tunnelSvc),
			application.NewService(settingsSvc),
			application.NewService(debugSvc),
			application.NewService(updateSvc),
		},
		FileAssociations: []string{".dingpack"},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: uniqueID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				logx.Infof("[main] 收到第二实例启动通知: args=%v dir=%s", data.Args, data.WorkingDir)
				if window != nil {
					window.Show()
					window.Focus()
				}
				if len(data.Args) > 1 {
					host.HandleExternalLaunch(data.Args[1:])
				}
			},
		},
	})

	windowOptions := application.WebviewWindowOptions{
		Name:               "main",
		Title:              "ding-ssh",
		Width:              1280,
		Height:             800,
		MinWidth:           900,
		MinHeight:          600,
		Frameless:          runtime.GOOS == "windows",
		URL:                "/",
		EnableFileDrop:     true,
		UseApplicationMenu: true,
		BackgroundColour:   application.NewRGBA(0, 0, 0, 0),
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
	window = desktop.Window.NewWithOptions(windowOptions)
	apphost.ConfigureDesktop(host, desktop, window, notifyService, dockService)
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		ctx := event.Context()
		if target := ctx.DropTargetDetails(); target != nil && target.ElementID != "" {
			window.EmitEvent("sftp:files-dropped", map[string]any{
				"target": target.ElementID,
				"paths":  ctx.DroppedFiles(),
			})
		}
	})

	if len(os.Args) > 1 {
		go func() {
			time.Sleep(500 * time.Millisecond)
			host.HandleExternalLaunch(os.Args[1:])
		}()
	}

	if err := desktop.Run(); err != nil {
		logx.Errorf("[main] Wails 运行失败: err=%v", err)
		println("Error:", err.Error())
	}
}
