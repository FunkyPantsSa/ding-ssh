package apphost

import (
	"context"
	"sync/atomic"

	"ding-ssh/internal/logx"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var appClosePending atomic.Bool
var appCloseApproved atomic.Bool

// BeforeClose cancels native close requests until the themed frontend dialog resolves.
func (a *App) BeforeClose(ctx context.Context) bool {
	if appCloseApproved.CompareAndSwap(true, false) {
		logx.Infof("[apphost] 关闭窗口已确认，放行退出")
		return false
	}
	if a.settings == nil {
		logx.Debugf("[apphost] 关闭请求（设置存储未就绪），直接放行")
		return false
	}
	settings, err := a.settings.Get()
	if err != nil {
		// 读设置失败时沿既有行为继续走确认流程（宁可多问一次，也不悄悄退出）。
		logx.Warnf("[apphost] 关闭请求读取设置失败，按需要确认处理: err=%v", err)
	}
	if err == nil && !settings.ConfirmCloseApp {
		logx.Debugf("[apphost] 关闭请求（未开启关闭确认），直接放行")
		return false
	}
	if appClosePending.CompareAndSwap(false, true) {
		logx.Infof("[apphost] 关闭请求已拦截，等待前端确认")
		wailsruntime.EventsEmit(ctx, "app:confirm-close")
		return true
	}
	logx.Debugf("[apphost] 关闭请求重复到达，确认对话框已在等待")
	return true
}

// ResolveAppClose grants one native close only after an explicit UI confirmation.
func (a *App) ResolveAppClose(confirmed bool) {
	if !appClosePending.CompareAndSwap(true, false) {
		logx.Debugf("[apphost] 关闭确认回包没有对应的待确认请求: confirmed=%v", confirmed)
		return
	}
	if confirmed {
		appCloseApproved.Store(true)
		logx.Infof("[apphost] 用户确认关闭应用")
		wailsruntime.Quit(a.ctx)
		return
	}
	logx.Infof("[apphost] 用户取消关闭应用")
}
