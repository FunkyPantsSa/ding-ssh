// Package desktopcapture captures pixels from the application's own native
// window. It does not locate windows by title or alter focus/visibility.
package desktopcapture

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	MaxPixels      = 32 * 1024 * 1024
	MaxPNGBytes    = 32 * 1024 * 1024
	captureTimeout = 8 * time.Second
)

// Frame describes the actual surface that was captured. ScaleX/Y map viewport
// CSS pixels to bitmap pixels only when CoordinateSpace is viewport-css.
type Frame struct {
	PNG                                             []byte
	Provider, Surface, WindowID                     string
	Width, Height                                   int
	ScaleX, ScaleY                                  float64
	ViewportWidth, ViewportHeight                   float64
	CoordinateSpace                                 string
	ContentX, ContentY, ContentWidth, ContentHeight float64
	OcclusionPossible                               bool
}

type Error struct{ Code, Message string }

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func fail(code, message string) error { return &Error{Code: code, Message: message} }

// One native capture at a time. A caller timeout does not release this slot:
// native WebView callbacks cannot necessarily be cancelled, so releasing it
// early would permit an unbounded queue of stalled operations.
var nativeSlot = make(chan struct{}, 1)
var requestSequence atomic.Uint64
var lastSuccessfulCapture atomic.Int64

func Capture(ctx context.Context, nativeWindow unsafe.Pointer, windowID string) (*Frame, error) {
	if ctx == nil {
		return nil, errors.New("capture context is required")
	}
	if nativeWindow == nil {
		return nil, fail("WINDOW_NOT_READY", "native window is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case nativeSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("capture unavailable or busy: %w", ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		releaseNativeSlot()
		return nil, err
	}
	frame, err := capturePlatform(ctx, nativeWindow, windowID)
	if err == nil {
		lastSuccessfulCapture.Store(time.Now().UnixMilli())
	}
	return frame, err
}

func releaseNativeSlot() { <-nativeSlot }

func Busy() bool { return len(nativeSlot) > 0 }

func LastSuccess() int64 { return lastSuccessfulCapture.Load() }
