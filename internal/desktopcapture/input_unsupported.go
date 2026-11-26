//go:build !darwin || !cgo

package desktopcapture

import (
	"context"
	"runtime"
	"unsafe"
)

func NativeInputCapabilities() map[string]any {
	return map[string]any{"available": false, "platform": runtime.GOOS, "reasonCode": "NATIVE_INPUT_UNSUPPORTED", "actions": []string{}, "limitations": []string{"native window input adapter is unavailable for this platform/build"}}
}

func inputPlatform(ctx context.Context, window unsafe.Pointer, windowID string, input Input) (*InputResult, error) {
	defer func() { <-inputSlot }()
	return nil, fail("NATIVE_INPUT_UNSUPPORTED", "local native window input is not implemented for this platform/build; no global focus or OS input was attempted")
}
