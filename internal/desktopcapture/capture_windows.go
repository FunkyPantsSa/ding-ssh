//go:build windows

package desktopcapture

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"syscall"
	"unsafe"
)

var (
	captureUser32           = syscall.NewLazyDLL("user32.dll")
	captureGDI32            = syscall.NewLazyDLL("gdi32.dll")
	captureIsWindow         = captureUser32.NewProc("IsWindow")
	captureWindowPID        = captureUser32.NewProc("GetWindowThreadProcessId")
	captureIsIconic         = captureUser32.NewProc("IsIconic")
	captureIsVisible        = captureUser32.NewProc("IsWindowVisible")
	captureClientRect       = captureUser32.NewProc("GetClientRect")
	captureGetDC            = captureUser32.NewProc("GetDC")
	captureReleaseDC        = captureUser32.NewProc("ReleaseDC")
	capturePrintWindow      = captureUser32.NewProc("PrintWindow")
	captureCompatibleDC     = captureGDI32.NewProc("CreateCompatibleDC")
	captureDeleteDC         = captureGDI32.NewProc("DeleteDC")
	captureCompatibleBitmap = captureGDI32.NewProc("CreateCompatibleBitmap")
	captureSelectObject     = captureGDI32.NewProc("SelectObject")
	captureDeleteObject     = captureGDI32.NewProc("DeleteObject")
	captureDIBits           = captureGDI32.NewProc("GetDIBits")
)

type windowsRect struct{ Left, Top, Right, Bottom int32 }
type windowsBitmapInfo struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
	Colors                       [1]uint32
}

func ProviderInfo() (string, string, bool) {
	return "win32-printwindow-client", "client-area", true
}

func capturePlatform(ctx context.Context, nativeWindow unsafe.Pointer, windowID string) (*Frame, error) {
	result := make(chan captureResult, 1)
	// PrintWindow is a synchronous OS call without a cancellation API. Keep the
	// global slot until it finishes, even if the caller's deadline expires.
	go func() {
		defer releaseNativeSlot()
		frame, err := captureWindows(ctx, uintptr(nativeWindow), windowID)
		result <- captureResult{frame: frame, err: err}
	}()
	select {
	case res := <-result:
		return res.frame, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type captureResult struct {
	frame *Frame
	err   error
}

func captureWindows(ctx context.Context, hwnd uintptr, windowID string) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if valid, _, _ := captureIsWindow.Call(hwnd); valid == 0 {
		return nil, fail("WINDOW_GONE", "native window no longer exists")
	}
	var pid uint32
	captureWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != uint32(os.Getpid()) {
		return nil, fail("WINDOW_IDENTITY_MISMATCH", "native window does not belong to this process")
	}
	if iconic, _, _ := captureIsIconic.Call(hwnd); iconic != 0 {
		return nil, fail("WINDOW_NOT_VISIBLE", "window is minimized")
	}
	if visible, _, _ := captureIsVisible.Call(hwnd); visible == 0 {
		return nil, fail("WINDOW_NOT_VISIBLE", "window is hidden")
	}
	var rect windowsRect
	if ok, _, err := captureClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); ok == 0 {
		return nil, fmt.Errorf("GetClientRect: %w", err)
	}
	w, h := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 || int64(w)*int64(h) > MaxPixels {
		return nil, fail("CAPTURE_SIZE_LIMIT", "window client bitmap exceeds the capture budget")
	}
	dc, _, err := captureGetDC.Call(hwnd)
	if dc == 0 {
		return nil, fmt.Errorf("GetDC: %w", err)
	}
	defer captureReleaseDC.Call(hwnd, dc)
	mem, _, err := captureCompatibleDC.Call(dc)
	if mem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC: %w", err)
	}
	defer captureDeleteDC.Call(mem)
	bitmap, _, err := captureCompatibleBitmap.Call(dc, uintptr(w), uintptr(h))
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap: %w", err)
	}
	defer captureDeleteObject.Call(bitmap)
	old, _, err := captureSelectObject.Call(mem, bitmap)
	if old == 0 || old == ^uintptr(0) {
		return nil, fmt.Errorf("SelectObject: %w", err)
	}
	defer captureSelectObject.Call(mem, old)
	// PW_CLIENTONLY | PW_RENDERFULLCONTENT. No desktop-copy fallback: it would
	// silently capture a different surface and could include another program.
	if ok, _, err := capturePrintWindow.Call(hwnd, mem, 0x00000001|0x00000002); ok == 0 {
		return nil, fmt.Errorf("PrintWindow: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A DIB bitmap must not remain selected while GetDIBits reads it.
	captureSelectObject.Call(mem, old)
	info := windowsBitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	raw := make([]byte, w*h*4)
	if lines, _, err := captureDIBits.Call(dc, bitmap, 0, uintptr(h), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&info)), 0); lines != uintptr(h) {
		return nil, fmt.Errorf("GetDIBits: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(raw); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = raw[i+2], raw[i+1], raw[i], 255
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, err
	}
	if encoded.Len() > MaxPNGBytes {
		return nil, fail("CAPTURE_SIZE_LIMIT", "window PNG exceeds the encoding budget")
	}
	return &Frame{PNG: encoded.Bytes(), Provider: "win32-printwindow-client", Surface: "client-area", WindowID: windowID,
		Width: w, Height: h, CoordinateSpace: "window-client-pixels", ContentWidth: float64(w), ContentHeight: float64(h)}, nil
}
