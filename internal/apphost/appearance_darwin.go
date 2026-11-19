//go:build darwin && cgo

package apphost

/*
#cgo LDFLAGS: -framework Cocoa
void dingSetAppearance(int tone);
*/
import "C"

import "ding-ssh/internal/logx"

func syncNativeAppearance(tone string) {
	value := 0
	if tone == "light" {
		value = 1
	}
	if tone == "dark" {
		value = 2
	}
	C.dingSetAppearance(C.int(value))
	logx.Debugf("[apphost] 同步 macOS 原生外观: tone=%s value=%d", tone, value)
}
