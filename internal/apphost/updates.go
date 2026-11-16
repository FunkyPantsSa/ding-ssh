package apphost

import (
	"context"
	"ding-ssh/internal/updater"
	"fmt"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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

func (a *App) CheckUpdate(channel string) (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	m, e := updater.Check(ctx, channel)
	if e != nil {
		return UpdateInfo{}, e
	}
	current := currentUpdateCommit()
	if current == "" {
		return UpdateInfo{}, fmt.Errorf("无法识别当前构建提交，请安装带版本信息的构建")
	}
	a.updates.Lock()
	defer a.updates.Unlock()
	if a.updates.busy || a.updates.source != "" {
		return UpdateInfo{}, fmt.Errorf("已有更新下载或等待安装")
	}
	a.updates.manifest = &m
	return UpdateInfo{m, m.Commit != current, current}, nil
}
func (a *App) DownloadUpdate(restart bool) error {
	a.updates.Lock()
	if a.updates.busy || a.updates.source != "" || a.updates.manifest == nil {
		a.updates.Unlock()
		return fmt.Errorf("请先检查更新，或等待当前更新完成")
	}
	m := *a.updates.manifest
	a.updates.busy = true
	a.updates.Unlock()
	defer func() { a.updates.Lock(); a.updates.busy = false; a.updates.Unlock() }()
	target, e := updater.Target()
	if e != nil {
		return e
	}
	var last time.Time
	file, e := updater.Download(a.ctx, m, func(n, total int64) {
		if time.Since(last) > 250*time.Millisecond {
			last = time.Now()
			wailsruntime.EventsEmit(a.ctx, "update:progress", map[string]int64{"received": n, "total": total})
		}
	})
	if e != nil {
		return e
	}
	source, e := updater.Prepare(file)
	if e != nil {
		os.RemoveAll(filepath.Dir(file))
		return e
	}
	a.updates.Lock()
	a.updates.source = source
	a.updates.target = target
	a.updates.restart = restart
	a.updates.Unlock()
	return nil
}
func (a *App) InstallUpdateNow() error {
	a.updates.Lock()
	ready := a.updates.source != ""
	if ready {
		a.updates.restart = true
	}
	a.updates.Unlock()
	if !ready {
		return fmt.Errorf("更新尚未下载完成")
	}
	wailsruntime.Quit(a.ctx)
	return nil
}
func (a *App) installPendingUpdate() {
	a.updates.Lock()
	defer a.updates.Unlock()
	if a.updates.source != "" {
		if e := updater.Launch(a.updates.source, a.updates.target, a.updates.restart); e != nil {
			os.WriteFile(filepath.Join(a.dataDir, "update-error.log"), []byte(e.Error()), 0600)
		}
	}
}

func (a *App) GetUpdateChannel() string { return updater.BuildChannel }

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
