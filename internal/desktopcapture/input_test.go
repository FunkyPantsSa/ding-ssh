package desktopcapture

import (
	"context"
	"errors"
	"testing"
	"unsafe"
)

func TestInputRejectsUnsupportedModesBeforeNativeDelivery(t *testing.T) {
	for _, input := range []Input{{Action: "doubleClick"}, {Action: "click", Button: "middle"}, {Action: "type", Text: "a", Modifiers: []string{"cmd"}}, {Action: "key"}, {Action: "click", Modifiers: []string{"invalid"}}} {
		_, err := SendInput(context.Background(), unsafe.Pointer(new(byte)), "main", input)
		var nativeError *Error
		if !errors.As(err, &nativeError) {
			t.Fatalf("input %+v error=%v", input, err)
		}
		if len(inputSlot) != 0 {
			t.Fatal("unsupported input occupied native slot")
		}
	}
}
