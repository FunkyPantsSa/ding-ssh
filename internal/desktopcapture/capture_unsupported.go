//go:build (!darwin && !windows) || (darwin && !cgo)

package desktopcapture

import (
	"context"
	"runtime"
	"unsafe"
)

func ProviderInfo() (string, string, bool) { return "", "", false }

func capturePlatform(ctx context.Context, nativeWindow unsafe.Pointer, windowID string) (*Frame, error) {
	defer releaseNativeSlot()
	return nil, fail("CAPTURE_UNSUPPORTED", "native pixel capture is not implemented for "+runtime.GOOS+" with the current build configuration")
}
