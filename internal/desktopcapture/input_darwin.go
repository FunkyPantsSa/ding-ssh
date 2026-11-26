//go:build darwin && cgo

package desktopcapture

/*
#include <stdint.h>
#include <stdlib.h>
void dingInputWindow(uintptr_t window, uint64_t requestID, const char *payload);
*/
import "C"

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"unsafe"
)

type inputRequest struct {
	ctx      context.Context
	windowID string
	started  atomic.Bool
	result   chan error
}

var inputRequests = struct {
	sync.Mutex
	requests map[uint64]*inputRequest
}{requests: make(map[uint64]*inputRequest)}

func NativeInputCapabilities() map[string]any {
	return map[string]any{"available": true, "provider": "appkit-nsevent", "inputMode": "native-window-events", "actions": []string{"click", "key", "type"}, "trustedEvent": "unverified", "requiresFocusedWindow": true, "requiresAccessibilityPermission": false,
		"limitations": []string{"local native window delivery; no global OS injection", "no system shortcuts, native-menu or IME coverage", "actual delivery and effects must be verified after each action"}}
}

func inputPlatform(ctx context.Context, window unsafe.Pointer, windowID string, input Input) (*InputResult, error) {
	data, err := json.Marshal(map[string]any{"action": input.Action, "x": input.X, "y": input.Y, "key": input.Key, "text": input.Text, "button": input.Button, "modifiers": input.Modifiers})
	if err != nil {
		<-inputSlot
		return nil, err
	}
	id := inputSequence.Add(1)
	req := &inputRequest{ctx: ctx, windowID: windowID, result: make(chan error, 1)}
	inputRequests.Lock()
	inputRequests.requests[id] = req
	inputRequests.Unlock()
	payload := C.CString(string(data))
	defer C.free(unsafe.Pointer(payload))
	C.dingInputWindow(C.uintptr_t(uintptr(window)), C.uint64_t(id), payload)
	select {
	case err := <-req.result:
		if err != nil {
			return nil, err
		}
		return &InputResult{WindowID: windowID, InputMode: "native-window-events", Provider: "appkit-nsevent", Delivered: true, TrustedEvent: "unverified", Limitations: []string{"local native window delivery; no global OS input", "requires the target window to be focused", "does not validate IME, system shortcuts, native menus or user-gesture privilege"}}, nil
	case <-ctx.Done():
		if req.started.Load() {
			return nil, fail("NATIVE_ACTION_OUTCOME_UNKNOWN", "deadline expired after native dispatch began; inspect state before retrying")
		}
		return nil, ctx.Err()
	}
}

//export dingInputActive
func dingInputActive(id C.uint64_t, start C.int) C.int {
	inputRequests.Lock()
	req := inputRequests.requests[uint64(id)]
	inputRequests.Unlock()
	if req == nil || req.ctx.Err() != nil {
		return 0
	}
	if start != 0 {
		req.started.Store(true)
	}
	return 1
}

//export dingInputComplete
func dingInputComplete(id C.uint64_t, code, message *C.char) {
	inputRequests.Lock()
	req := inputRequests.requests[uint64(id)]
	delete(inputRequests.requests, uint64(id))
	inputRequests.Unlock()
	if req == nil {
		return
	}
	defer func() { <-inputSlot }()
	var err error
	if code != nil {
		err = fail(C.GoString(code), C.GoString(message))
	}
	req.result <- err
}
