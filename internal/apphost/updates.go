package apphost

import (
	"context"
	"ding-ssh/internal/i18n"
	"ding-ssh/internal/logx"
	"ding-ssh/internal/updater"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

type updateState struct {
	sync.Mutex
	manifest       *updater.Manifest
	source, target string
	busy, restart  bool
}
type UpdateInfo struct {
	Manifest  updater.Manifest `json:"manifest"`
	Available bool             `json:"available"`
	Current   string           `json:"current"`
}

// updateProgressLog 下载进度回调是高频路径：按时间限流，只记已收 / 总字节数。
var updateProgressLog = logx.NewSampler(0, 2*time.Second)

func (a *App) CheckUpdate(channel string) (UpdateInfo, error) {
	start := time.Now()
	logx.Debugf("[apphost] 检查更新开始: channel=%q", channel)
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	m, e := updater.Check(ctx, channel)
	if e != nil {
		logx.Errorf("[apphost] 检查更新失败: channel=%q err=%v elapsed=%s", channel, e, time.Since(start))
		return UpdateInfo{}, e
	}
	current := currentUpdateCommit()
	if current == "" {
		logx.Errorf("[apphost] 检查更新失败（无法识别当前构建提交）: channel=%q", channel)
		return UpdateInfo{}, fmt.Errorf("%s", i18n.T("无法识别当前构建提交，请安装带版本信息的构建"))
	}
	a.updates.Lock()
	defer a.updates.Unlock()
	if a.updates.busy || a.updates.source != "" {
		logx.Warnf("[apphost] 检查更新被拒（已有更新下载或等待安装）: channel=%q", channel)
		return UpdateInfo{}, fmt.Errorf("%s", i18n.T("已有更新下载或等待安装"))
	}
	a.updates.manifest = &m
	logx.Infof("[apphost] 检查更新完成: channel=%q current=%s latest=%s available=%v elapsed=%s",
		channel, current, m.Commit, m.Commit != current, time.Since(start))
	return UpdateInfo{m, m.Commit != current, current}, nil
}
func (a *App) DownloadUpdate(restart bool) error {
	start := time.Now()
	logx.Debugf("[apphost] 下载更新开始: restart=%v", restart)
	a.updates.Lock()
	if a.updates.busy || a.updates.source != "" || a.updates.manifest == nil {
		a.updates.Unlock()
		logx.Warnf("[apphost] 下载更新被拒（未检查更新或已有任务）: busy=%v pendingInstall=%v hasManifest=%v",
			a.updates.busy, a.updates.source != "", a.updates.manifest != nil)
		return fmt.Errorf("%s", i18n.T("请先检查更新，或等待当前更新完成"))
	}
	m := *a.updates.manifest
	a.updates.busy = true
	a.updates.Unlock()
	defer func() { a.updates.Lock(); a.updates.busy = false; a.updates.Unlock() }()
	target, e := updater.Target()
	if e != nil {
		logx.Errorf("[apphost] 下载更新失败（解析安装目标）: err=%v", e)
		return e
	}
	logx.Infof("[apphost] 更新下载中: channel=%q commit=%s target=%q", m.Channel, m.Commit, target)
	var last time.Time
	file, e := updater.Download(a.ctx, m, func(n, total int64) {
		if time.Since(last) > 250*time.Millisecond {
			last = time.Now()
			logx.DebugSampled(updateProgressLog, "[apphost] 更新下载进度: received=%d total=%d", n, total)
			a.emit("update:progress", map[string]int64{"received": n, "total": total})
		}
	})
	if e != nil {
		logx.Errorf("[apphost] 下载更新失败: channel=%q commit=%s err=%v elapsed=%s",
			m.Channel, m.Commit, e, time.Since(start))
		return e
	}
	logx.Debugf("[apphost] 更新包已下载: file=%q", file)
	source, e := updater.Prepare(file)
	if e != nil {
		if rerr := os.RemoveAll(filepath.Dir(file)); rerr != nil {
			logx.Warnf("[apphost] 清理更新临时目录失败: dir=%q err=%v", filepath.Dir(file), rerr)
		} else {
			logx.Warnf("[apphost] 更新准备失败，已清理临时目录: file=%q err=%v", file, e)
		}
		logx.Errorf("[apphost] 下载更新失败（准备安装包）: file=%q err=%v", file, e)
		return e
	}
	a.updates.Lock()
	a.updates.source = source
	a.updates.target = target
	a.updates.restart = restart
	a.updates.Unlock()
	logx.Infof("[apphost] 更新已准备安装: channel=%q commit=%s source=%q target=%q restart=%v elapsed=%s",
		m.Channel, m.Commit, source, target, restart, time.Since(start))
	return nil
}
func (a *App) InstallUpdateNow() error {
	a.updates.Lock()
	ready := a.updates.source != ""
	if ready {
		a.updates.restart = true
	}
	source := a.updates.source
	a.updates.Unlock()
	if !ready {
		logx.Warnf("[apphost] 立即安装更新被拒（尚未下载完成）")
		return fmt.Errorf("%s", i18n.T("更新尚未下载完成"))
	}
	logx.Infof("[apphost] 立即安装更新并退出: source=%q", source)
	a.requestQuit()
	return nil
}
func (a *App) installPendingUpdate() {
	a.updates.Lock()
	defer a.updates.Unlock()
	if a.updates.source != "" {
		logx.Infof("[apphost] 退出前启动更新安装: source=%q target=%q restart=%v",
			a.updates.source, a.updates.target, a.updates.restart)
		if e := updater.Launch(a.updates.source, a.updates.target, a.updates.restart); e != nil {
			logx.Errorf("[apphost] 启动更新安装失败: source=%q target=%q err=%v",
				a.updates.source, a.updates.target, e)
			if werr := os.WriteFile(filepath.Join(a.dataDir, "update-error.log"), []byte(e.Error()), 0600); werr != nil {
				logx.Warnf("[apphost] 写入更新错误记录失败: dir=%q err=%v", a.dataDir, werr)
			}
		}
	}
}

func (a *App) GetUpdateChannel() string {
	logx.Debugf("[apphost] 读取更新通道: channel=%q", updater.BuildChannel)
	return updater.BuildChannel
}

func currentUpdateCommit() string {
	current := updater.BuildCommit
	if b, ok := debug.ReadBuildInfo(); ok && current == "" {
		for _, s := range b.Settings {
			if s.Key == "vcs.revision" {
				current = s.Value
			}
		}
	}
	return current
}
