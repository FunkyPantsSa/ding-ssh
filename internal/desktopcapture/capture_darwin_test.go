//go:build darwin && cgo

package desktopcapture

import (
	"context"
	"errors"
	"testing"
)

func TestNativeCompletionReleasesCancelledRequestExactlyOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	req := &captureRequest{ctx: ctx, windowID: "main", result: make(chan captureResult, 1)}
	nativeSlot <- struct{}{}
	captureRequests.Lock()
	captureRequests.requests[90000001] = req
	captureRequests.Unlock()
	cancel()
	// Completion may arrive after the caller returned. It still releases the
	// native slot and registry entry; duplicate native delivery is harmless.
	dingCaptureComplete(90000001, nil, 0, 0, 0, 0, 0, 0, 0, 0, 0, nil, nil)
	if Busy() {
		t.Fatal("native callback did not release capture slot")
	}
	if result := <-req.result; !errors.Is(result.err, context.Canceled) {
		t.Fatalf("result error = %v", result.err)
	}
	dingCaptureComplete(90000001, nil, 0, 0, 0, 0, 0, 0, 0, 0, 0, nil, nil)
	captureRequests.Lock()
	remaining := len(captureRequests.requests)
	captureRequests.Unlock()
	if remaining != 0 {
		t.Fatalf("pending native callbacks = %d", remaining)
	}
}
