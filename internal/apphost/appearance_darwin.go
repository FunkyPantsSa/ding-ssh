//go:build darwin && cgo

package apphost

/*
#cgo LDFLAGS: -framework Cocoa
void dingSetAppearance(int tone);
*/
import "C"

func syncNativeAppearance(tone string) {
	value := 0
	if tone == "light" {
		value = 1
	}
	if tone == "dark" {
		value = 2
	}
	C.dingSetAppearance(C.int(value))
}
