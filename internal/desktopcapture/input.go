package desktopcapture

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"
)

// Input sends an event to the application's own native window. This is not
// global OS automation and does not change foreground focus or ask for system
// accessibility permission. Coordinates are WebView viewport CSS pixels.
type Input struct {
	Action    string `json:"action"`
	X, Y      float64
	Key       string   `json:"key,omitempty"`
	Text      string   `json:"text,omitempty"`
	Button    string   `json:"button,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type InputResult struct {
	WindowID     string   `json:"windowId"`
	InputMode    string   `json:"inputMode"`
	Provider     string   `json:"provider"`
	Delivered    bool     `json:"delivered"`
	TrustedEvent string   `json:"trustedEvent"`
	Limitations  []string `json:"limitations"`
}

var inputSlot = make(chan struct{}, 1)
var inputSequence atomic.Uint64

func SendInput(ctx context.Context, window unsafe.Pointer, windowID string, input Input) (*InputResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("native input context is required")
	}
	if window == nil {
		return nil, fail("WINDOW_NOT_READY", "native window is unavailable")
	}
	if input.Action != "click" && input.Action != "key" && input.Action != "type" {
		return nil, fail("NATIVE_ACTION_UNSUPPORTED", "native input supports click, key and type")
	}
	if len(input.Text) > 4096 {
		return nil, fail("INPUT_SIZE_LIMIT", "native text exceeds 4096 bytes")
	}
	if input.Action == "click" && input.Button != "" && input.Button != "left" && input.Button != "right" {
		return nil, fail("NATIVE_BUTTON_UNSUPPORTED", "native click supports left and right buttons")
	}
	if input.Action == "key" && (input.Key == "" || len(input.Key) > 32) {
		return nil, fail("NATIVE_KEY_UNSUPPORTED", "native key must be a character or supported named key")
	}
	if input.Action == "type" && (input.Text == "" || len(input.Modifiers) != 0) {
		return nil, fail("NATIVE_TEXT_UNSUPPORTED", "native text requires nonempty text without key modifiers")
	}
	for _, mod := range input.Modifiers {
		switch mod {
		case "shift", "ctrl", "control", "alt", "option", "meta", "cmd", "command":
		default:
			return nil, fail("NATIVE_MODIFIER_UNSUPPORTED", "unsupported native input modifier")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case inputSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-inputSlot
		return nil, err
	}
	return inputPlatform(ctx, window, windowID, input)
}
