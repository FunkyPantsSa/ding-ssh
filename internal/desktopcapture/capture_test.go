package desktopcapture

import (
	"context"
	"errors"
	"testing"
	"unsafe"
)

func TestCaptureRejectsCancelledRequestBeforeNativeDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Capture(ctx, unsafe.Pointer(new(byte)), "main")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("capture error = %v", err)
	}
	if Busy() {
		t.Fatal("cancelled request occupied native capture slot")
	}
}

func TestCaptureRejectsUnavailableNativeWindow(t *testing.T) {
	_, err := Capture(context.Background(), nil, "main")
	var nativeError *Error
	if !errors.As(err, &nativeError) || nativeError.Code != "WINDOW_NOT_READY" {
		t.Fatalf("capture error = %v", err)
	}
	if Busy() {
		t.Fatal("invalid window occupied native capture slot")
	}
}
