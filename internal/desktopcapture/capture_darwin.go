//go:build darwin && cgo

package desktopcapture

/*
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdint.h>
#include <stddef.h>
void dingCaptureWindow(uintptr_t window, uint64_t requestID);
*/
import "C"

import (
	"context"
	"sync"
	"unsafe"
)

type captureResult struct {
	frame *Frame
	err   error
}
type captureRequest struct {
	ctx      context.Context
	windowID string
	result   chan captureResult
}

var captureRequests = struct {
	sync.Mutex
	requests map[uint64]*captureRequest
}{requests: make(map[uint64]*captureRequest)}

func ProviderInfo() (string, string, bool) { return "wkwebview-snapshot", "webview", true }

func capturePlatform(ctx context.Context, nativeWindow unsafe.Pointer, windowID string) (*Frame, error) {
	id := requestSequence.Add(1)
	req := &captureRequest{ctx: ctx, windowID: windowID, result: make(chan captureResult, 1)}
	captureRequests.Lock()
	captureRequests.requests[id] = req
	captureRequests.Unlock()
	C.dingCaptureWindow(C.uintptr_t(uintptr(nativeWindow)), C.uint64_t(id))
	select {
	case result := <-req.result:
		return result.frame, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

//export dingCaptureActive
func dingCaptureActive(id C.uint64_t) C.int {
	captureRequests.Lock()
	req := captureRequests.requests[uint64(id)]
	captureRequests.Unlock()
	if req != nil && req.ctx.Err() == nil {
		return 1
	}
	return 0
}

//export dingCaptureComplete
func dingCaptureComplete(id C.uint64_t, bytes unsafe.Pointer, length C.size_t, width, height C.int, viewportWidth, viewportHeight, contentX, contentY, contentWidth, contentHeight C.double, errorCode, errorMessage *C.char) {
	captureRequests.Lock()
	req := captureRequests.requests[uint64(id)]
	delete(captureRequests.requests, uint64(id))
	captureRequests.Unlock()
	if req == nil {
		return
	}
	defer releaseNativeSlot()
	if req.ctx.Err() != nil {
		req.result <- captureResult{err: req.ctx.Err()}
		return
	}
	if errorCode != nil {
		req.result <- captureResult{err: fail(C.GoString(errorCode), C.GoString(errorMessage))}
		return
	}
	if length == 0 || length > MaxPNGBytes || width <= 0 || height <= 0 || int64(width)*int64(height) > MaxPixels {
		req.result <- captureResult{err: fail("CAPTURE_SIZE_LIMIT", "native capture returned an invalid or oversized image")}
		return
	}
	f := &Frame{PNG: C.GoBytes(bytes, C.int(length)), Provider: "wkwebview-snapshot", Surface: "webview", WindowID: req.windowID,
		Width: int(width), Height: int(height), ViewportWidth: float64(viewportWidth), ViewportHeight: float64(viewportHeight), CoordinateSpace: "viewport-css",
		ContentX: float64(contentX), ContentY: float64(contentY), ContentWidth: float64(contentWidth), ContentHeight: float64(contentHeight)}
	if f.ViewportWidth <= 0 || f.ViewportHeight <= 0 {
		req.result <- captureResult{err: fail("VIEWPORT_UNAVAILABLE", "page viewport could not be measured")}
		return
	}
	f.ScaleX = float64(f.Width) / f.ViewportWidth
	f.ScaleY = float64(f.Height) / f.ViewportHeight
	req.result <- captureResult{frame: f}
}
