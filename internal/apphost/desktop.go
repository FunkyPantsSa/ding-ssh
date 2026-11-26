package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"time"

	"ding-ssh/internal/i18n"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func ConfigureDesktop(a *App, desktop *application.App, window *application.WebviewWindow, ns *notifications.NotificationService, ds *dock.DockService) {
	a.desktop = desktop
	a.window = window
	a.notifications = ns
	a.dock = ds
	setupTerminalStream(a, desktop)
	setupSystemTray(a, desktop, window)
	setupApplicationMenu(a, desktop)
	setupGlobalShortcut(a, desktop, window)
	setupSystemEvents(a, desktop)

	window.OnWindowEvent(events.Common.WindowFocus, func(event *application.WindowEvent) {
		window.Flash(false)
		if runtime.GOOS == "windows" && a.dock != nil {
			// 连接数量不代表未读消息，清除旧版本留下的任务栏角标。
			_ = a.dock.RemoveBadge()
		}
	})

	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if a.quitting.Load() {
			return
		}
		// 关闭窗口时隐藏至系统托盘，保持后台隧道与连接常驻
		event.Cancel()
		window.Hide()
	})
}

func setupGlobalShortcut(a *App, desktop *application.App, window *application.WebviewWindow) {
	if desktop.GlobalShortcut == nil {
		return
	}
	shortcut := "CmdOrCtrl+OptionOrAlt+S"
	err := desktop.GlobalShortcut.Register(shortcut, func() {
		if window != nil {
			if window.IsVisible() && window.IsFocused() {
				window.Hide()
			} else {
				window.Show()
				window.Focus()
			}
		}
	})
	if err != nil {
		println("[desktop] 注册全局呼出快捷键失败:", shortcut, err.Error())
	}
}

func setupSystemEvents(a *App, desktop *application.App) {
	desktop.Event.OnApplicationEvent(events.Common.SystemDidWake, func(event *application.ApplicationEvent) {
		a.emit("system:did-wake")
	})
	desktop.Event.OnApplicationEvent(events.Common.NetworkChanged, func(event *application.ApplicationEvent) {
		a.emit("system:network-changed")
	})
	desktop.Event.OnApplicationEvent(events.Common.ThemeChanged, func(event *application.ApplicationEvent) {
		isDark := desktop.Env.IsDarkMode()
		a.emit("system:theme-changed", map[string]any{"isDark": isDark})
	})
}

func setupApplicationMenu(a *App, desktop *application.App) {
	menu := desktop.NewMenu()

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}

	// File menu
	fileMenu := menu.AddSubmenu(i18n.T("文件"))
	fileMenu.Add(i18n.T("新建连接")).SetAccelerator("CmdOrCtrl+N").OnClick(func(ctx *application.Context) {
		a.emit("menu:new-connection")
	})
	fileMenu.Add(i18n.T("快速连接")).SetAccelerator("CmdOrCtrl+K").OnClick(func(ctx *application.Context) {
		a.emit("menu:quick-connect")
	})
	fileMenu.AddSeparator()
	fileMenu.Add(i18n.T("导入配置...")).OnClick(func(ctx *application.Context) {
		a.emit("menu:import-config")
	})
	fileMenu.Add(i18n.T("导出配置...")).OnClick(func(ctx *application.Context) {
		a.emit("menu:export-config")
	})
	fileMenu.AddSeparator()
	fileMenu.Add(i18n.T("隐藏窗口")).SetAccelerator("CmdOrCtrl+W").OnClick(func(ctx *application.Context) {
		if a.window != nil {
			a.window.Hide()
		}
	})

	// Edit menu
	menu.AddRole(application.EditMenu)

	// Session menu
	sessionMenu := menu.AddSubmenu(i18n.T("会话"))
	sessionMenu.Add(i18n.T("重连当前会话")).SetAccelerator("CmdOrCtrl+R").OnClick(func(ctx *application.Context) {
		a.emit("menu:reconnect-current")
	})
	sessionMenu.Add(i18n.T("断开当前会话")).OnClick(func(ctx *application.Context) {
		a.emit("menu:disconnect-current")
	})
	sessionMenu.AddSeparator()
	sessionMenu.Add(i18n.T("断开所有连接与隧道")).OnClick(func(ctx *application.Context) {
		a.DisconnectAll()
	})

	// View menu
	menu.AddRole(application.ViewMenu)

	// Window menu
	menu.AddRole(application.WindowMenu)

	// Help menu
	helpMenu := menu.AddSubmenu(i18n.T("帮助"))
	helpMenu.Add(i18n.T("打开日志目录")).OnClick(func(ctx *application.Context) {
		_ = a.OpenLogFolder()
	})
	helpMenu.Add(i18n.T("开发者与诊断")).OnClick(func(ctx *application.Context) {
		a.emit("menu:open-debug")
	})
	helpMenu.AddSeparator()
	helpMenu.Add(i18n.T("检查更新...")).OnClick(func(ctx *application.Context) {
		a.emit("menu:check-updates")
	})

	desktop.Menu.SetApplicationMenu(menu)
}

func setupSystemTray(a *App, desktop *application.App, window *application.WebviewWindow) {
	tray := desktop.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	} else {
		tray.SetDarkModeIcon(icons.SystrayDark)
		tray.SetIcon(icons.SystrayLight)
	}
	tray.SetTooltip("ding-ssh")

	tray.OnClick(func() {
		if window != nil {
			if window.IsVisible() {
				window.Hide()
			} else {
				window.Show()
				window.Focus()
			}
		}
	})

	menu := desktop.NewMenu()
	menu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
		if window != nil {
			window.Show()
			window.Focus()
		}
	})
	menu.AddSeparator()

	menu.Add("断开所有连接与隧道").OnClick(func(ctx *application.Context) {
		a.DisconnectAll()
	})
	menu.AddSeparator()

	menu.Add("退出 ding-ssh").OnClick(func(ctx *application.Context) {
		a.quit()
	})

	tray.SetMenu(menu)
	a.tray = tray
}

type terminalControlMessage struct {
	Action    string `json:"action"` // "attach", "resize", "input", "input_interactive"
	SessionID string `json:"sessionId,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Data      string `json:"data,omitempty"`
	Command   string `json:"command,omitempty"`
}

func setupTerminalStream(a *App, desktop *application.App) {
	desktop.HandleStream("terminal", func(c *application.StreamConn) {
		defer c.Close()
		var currentSessionID string
		var unsub func()
		defer func() {
			if unsub != nil {
				unsub()
			}
		}()

		for {
			frame, err := c.Receive()
			if err != nil {
				return
			}
			if len(frame) == 0 {
				continue
			}

			if frame[0] == '{' {
				var msg terminalControlMessage
				if err := json.Unmarshal(frame, &msg); err == nil && msg.Action != "" {
					switch msg.Action {
					case "attach":
						if unsub != nil {
							unsub()
							unsub = nil
						}
						currentSessionID = msg.SessionID
						if currentSessionID != "" {
							unsub = a.RegisterTerminalStream(currentSessionID, func(data []byte) {
								_ = c.Send(data)
							})
						}
					case "resize":
						sid := msg.SessionID
						if sid == "" {
							sid = currentSessionID
						}
						if sid != "" && msg.Cols > 0 && msg.Rows > 0 {
							_ = a.Resize(sid, msg.Cols, msg.Rows)
						}
					case "input_interactive":
						sid := msg.SessionID
						if sid == "" {
							sid = currentSessionID
						}
						if sid != "" {
							_ = a.WriteRawInteractive(sid, []byte(msg.Data), msg.Command)
						}
					case "input":
						sid := msg.SessionID
						if sid == "" {
							sid = currentSessionID
						}
						if sid != "" {
							_ = a.WriteRaw(sid, []byte(msg.Data))
						}
					}
					continue
				}
			}

			if currentSessionID != "" {
				_ = a.WriteRaw(currentSessionID, frame)
			}
		}
	})
}

func ShouldQuit(a *App) bool {
	return !a.needsCloseConfirmation()
}

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(ctx)
	return nil
}

func (a *App) ServiceShutdown() error {
	a.shutdown()
	return nil
}

func (a *App) emit(name string, data ...any) {
	if a.desktop != nil {
		a.desktop.Event.Emit(name, data...)
	}
}

func (a *App) requestQuit() {
	if a.desktop != nil && ShouldQuit(a) {
		a.quit()
	}
}

func (a *App) quit() {
	if a.desktop != nil && a.quitting.CompareAndSwap(false, true) {
		a.desktop.Quit()
	}
}

func (a *App) openFile(options application.OpenFileDialogOptions) (string, error) {
	options.Window = a.window
	options.CanChooseFiles = true
	return a.desktop.Dialog.OpenFileWithOptions(&options).PromptForSingleSelection()
}

func (a *App) openFiles(options application.OpenFileDialogOptions) ([]string, error) {
	options.Window = a.window
	options.CanChooseFiles = true
	return a.desktop.Dialog.OpenFileWithOptions(&options).PromptForMultipleSelection()
}

func (a *App) openDirectory(options application.OpenFileDialogOptions) (string, error) {
	options.Window = a.window
	options.CanChooseDirectories = true
	options.CanCreateDirectories = true
	return a.desktop.Dialog.OpenFileWithOptions(&options).PromptForSingleSelection()
}

func (a *App) saveFile(options application.SaveFileDialogOptions) (string, error) {
	options.Window = a.window
	options.CanCreateDirectories = true
	return a.desktop.Dialog.SaveFileWithOptions(&options).PromptForSingleSelection()
}

// CopyText exposes the native clipboard result so the UI can distinguish
// a failed write from a successful copy.
func (a *App) CopyText(text string) error {
	if a.desktop == nil || !a.desktop.Clipboard.SetText(text) {
		return errors.New(i18n.T("剪贴板写入失败"))
	}
	return nil
}

type approvalDialogOptions struct {
	Title, Message              string
	Buttons                     []string
	DefaultButton, CancelButton string
}

func (a *App) promptApproval(options approvalDialogOptions) (string, error) {
	if a.ctx == nil || a.window == nil || a.ctx.Err() != nil || a.window.NativeWindow() == nil {
		return "", errors.New(i18n.T("可信用户审批不可用"))
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	result := make(chan string, 1)
	message := options.Message
	if runtime.GOOS == "windows" {
		// Wails can fall back to a native Yes/No box. It detects the
		// default No button by its caption and maps Yes to the first
		// non-cancel choice. Explain that exact choice in the fallback.
		for _, label := range options.Buttons {
			if label != options.CancelButton {
				message += fmt.Sprintf(i18n.T("\n\n如果系统只显示“是 / 否”，“是”表示“%s”，“否 / No”表示拒绝。"), label)
				break
			}
		}
	}
	dialog := a.desktop.Dialog.Question().SetTitle(options.Title).SetMessage(message).AttachToWindow(a.window)
	for _, label := range options.Buttons {
		caption := label
		if runtime.GOOS == "windows" && label == options.CancelButton {
			caption = "No"
		}
		button := dialog.AddButton(caption).OnClick(func() {
			select {
			case result <- label:
			default:
			}
		})
		// AppKit gives a button only one key equivalent. Keep Escape
		// bound to rejection rather than assigning Return to that button.
		if label == options.DefaultButton && !(runtime.GOOS == "darwin" && label == options.CancelButton) {
			dialog.SetDefaultButton(button)
		}
		if label == options.CancelButton {
			dialog.SetCancelButton(button)
		}
	}
	dialog.Show()
	select {
	case choice := <-result:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return choice, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
