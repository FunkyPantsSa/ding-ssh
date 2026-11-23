//go:build !darwin || !cgo

package apphost

import "ding-ssh/internal/logx"

func syncNativeAppearance(tone string) {
	logx.Debugf("[apphost] 当前平台无需同步原生外观，已跳过: tone=%s", tone)
}
