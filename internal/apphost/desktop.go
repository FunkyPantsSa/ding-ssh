package apphost

import (
	"context"
	"errors"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func ConfigureDesktop(a *App, desktop *application.App, window *application.WebviewWindow) {
	a.desktop = desktop
	a.window = window
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if a.needsCloseConfirmation() {
			event.Cancel()
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
		return errors.New("剪贴板写入失败")
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
		return "", errors.New("可信用户审批不可用")
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
				message += "\n\n如果系统只显示“是 / 否”，“是”表示“" + label + "”，“否 / No”表示拒绝。"
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
