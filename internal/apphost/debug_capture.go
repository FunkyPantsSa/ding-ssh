package apphost

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"ding-ssh/internal/debugsrv"
	"ding-ssh/internal/desktopcapture"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Capture implements debugsrv.CaptureProvider. The caller has already passed
// the UI-inspection policy. Resolve only this application's Wails windows.
func (c *debugController) Capture(ctx context.Context, windowID string) (debugsrv.CaptureFrame, error) {
	return c.captureDebugWindow(ctx, windowID)
}

func (c *debugController) captureDebugWindow(ctx context.Context, windowID string) (debugsrv.CaptureFrame, error) {
	if c.app == nil || c.app.desktop == nil {
		return debugsrv.CaptureFrame{}, fmt.Errorf("WINDOW_NOT_READY: desktop host is unavailable")
	}
	var selected application.Window
	for _, window := range c.app.desktop.Window.GetAll() {
		if windowID != "" && window.Name() != windowID && strconv.FormatUint(uint64(window.ID()), 10) != windowID {
			continue
		}
		if windowID != "" || c.app.window == nil || window.ID() == c.app.window.ID() {
			selected = window
			break
		}
	}
	if selected == nil {
		return debugsrv.CaptureFrame{}, fmt.Errorf("WINDOW_NOT_FOUND: no application window matches %q", windowID)
	}
	if err := ctx.Err(); err != nil {
		return debugsrv.CaptureFrame{}, err
	}
	frame, err := desktopcapture.Capture(ctx, selected.NativeWindow(), selected.Name())
	if err != nil {
		return debugsrv.CaptureFrame{}, err
	}
	return debugsrv.CaptureFrame{PNG: frame.PNG, Provider: frame.Provider, Surface: frame.Surface, WindowID: frame.WindowID,
		PixelWidth: frame.Width, PixelHeight: frame.Height, ScaleX: frame.ScaleX, ScaleY: frame.ScaleY,
		// The returned surface contains only the WebView/client area. Its
		// content rect is in bitmap pixels, not Cocoa bottom-left points.
		ContentWidth: float64(frame.Width), ContentHeight: float64(frame.Height),
		OcclusionPossible: frame.OcclusionPossible, CapturedAt: time.Now().UnixMilli()}, nil
}

func (c *debugController) CaptureCapabilities(ctx context.Context) map[string]any {
	provider, surface, implemented := desktopcapture.ProviderInfo()
	ready := implemented && c.app != nil && c.app.window != nil && c.app.window.NativeWindow() != nil
	visible := ready && c.app.window.IsVisible() && !c.app.window.IsMinimised()
	reason := ""
	if !implemented {
		reason = "CAPTURE_UNSUPPORTED"
	} else if !ready {
		reason = "WINDOW_NOT_READY"
	} else if !visible {
		reason = "WINDOW_NOT_VISIBLE"
	}
	return map[string]any{"implemented": implemented, "available": ready && visible, "provider": provider, "surface": surface,
		"reasonCode": reason, "platform": runtime.GOOS, "maxPixels": desktopcapture.MaxPixels, "maxPNGBytes": desktopcapture.MaxPNGBytes,
		"requiresVisibleWindow": true, "busy": desktopcapture.Busy(), "verified": desktopcapture.LastSuccess() > 0, "lastSuccessAt": desktopcapture.LastSuccess(), "note": "availability describes the adapter; each capture result reports success or a concrete native failure"}
}
