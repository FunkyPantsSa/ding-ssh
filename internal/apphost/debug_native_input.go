package apphost

import (
	"context"
	"fmt"
	"strconv"

	"ding-ssh/internal/desktopcapture"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (c *debugController) NativeInputCapabilities(ctx context.Context) map[string]any {
	out := desktopcapture.NativeInputCapabilities()
	out["hostReady"] = c.app != nil && c.app.window != nil && c.app.window.NativeWindow() != nil
	return out
}

// NativeInput is an internal host adapter; it is not a Wails-bound method.
// debugsrv must apply UI-write capability checks and actionability checks first.
func (c *debugController) NativeInput(ctx context.Context, windowID string, input desktopcapture.Input) (*desktopcapture.InputResult, error) {
	if c.app == nil || c.app.desktop == nil {
		return nil, fmt.Errorf("WINDOW_NOT_READY: desktop host is unavailable")
	}
	if windowID == "" {
		windowID = "main"
	}
	var selected application.Window
	for _, window := range c.app.desktop.Window.GetAll() {
		if window.Name() == windowID || strconv.FormatUint(uint64(window.ID()), 10) == windowID {
			selected = window
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("WINDOW_NOT_FOUND: no application window matches %q", windowID)
	}
	return desktopcapture.SendInput(ctx, selected.NativeWindow(), selected.Name(), input)
}

