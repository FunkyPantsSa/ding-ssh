package apphost

import (
	"context"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"sync/atomic"
)

var appClosePending atomic.Bool
var appCloseApproved atomic.Bool

// BeforeClose cancels native close requests until the themed frontend dialog resolves.
func (a *App) BeforeClose(ctx context.Context) bool {
	if appCloseApproved.CompareAndSwap(true, false) {
		return false
	}
	if a.settings == nil {
		return false
	}
	settings, err := a.settings.Get()
	if err == nil && !settings.ConfirmCloseApp {
		return false
	}
	if appClosePending.CompareAndSwap(false, true) {
		wailsruntime.EventsEmit(ctx, "app:confirm-close")
	}
	return true
}

// ResolveAppClose grants one native close only after an explicit UI confirmation.
func (a *App) ResolveAppClose(confirmed bool) {
	if !appClosePending.CompareAndSwap(true, false) {
		return
	}
	if confirmed {
		appCloseApproved.Store(true)
		wailsruntime.Quit(a.ctx)
	}
}
