package apphost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
)

var runtimeIdentity = struct {
	sync.Once
	id    string
	build map[string]any
}{}

func debugIdentity() (string, map[string]any) {
	runtimeIdentity.Do(func() {
		runtimeIdentity.id = uuid.NewString()
		runtimeIdentity.build = map[string]any{"appVersion": appVersion(), "goVersion": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, v := range info.Settings {
				if v.Key == "vcs.revision" {
					runtimeIdentity.build["gitCommit"] = v.Value
				}
				if v.Key == "vcs.modified" {
					runtimeIdentity.build["dirty"] = v.Value == "true"
				}
			}
		}
		if exe, err := os.Executable(); err == nil {
			if f, err := os.Open(exe); err == nil {
				h := sha256.New()
				_, e := io.Copy(h, f)
				_ = f.Close()
				if e == nil {
					runtimeIdentity.build["binarySHA256"] = hex.EncodeToString(h.Sum(nil))
				}
			}
		}
	})
	build := make(map[string]any, len(runtimeIdentity.build))
	for k, v := range runtimeIdentity.build {
		build[k] = v
	}
	return runtimeIdentity.id, build
}

func (c *debugController) recordFrontendHeartbeat(payload string) {
	if len(payload) > 8192 {
		return
	}
	var v struct {
		WindowID   string            `json:"windowId"`
		PageEpoch  string            `json:"pageEpoch"`
		Ready      bool              `json:"ready"`
		Visibility string            `json:"visibility"`
		Sequences  map[string]uint64 `json:"sequences"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil || v.WindowID != "main" || len(v.PageEpoch) > 100 {
		return
	}
	c.mu.Lock()
	c.frontendHeartbeat = map[string]any{"receivedAt": time.Now().UnixMilli(), "pageEpoch": v.PageEpoch, "ready": v.Ready, "visibility": v.Visibility, "sequences": v.Sequences}
	c.mu.Unlock()
}

func (c *debugController) runtimeObservation(ctx context.Context) map[string]any {
	id, build := debugIdentity()
	out := map[string]any{"instanceId": id, "build": build, "pid": os.Getpid(), "observedAt": time.Now().UnixMilli(), "backend": map[string]any{"state": "healthy", "source": "go-runtime", "metrics": c.app.GetRuntimeStatus()}}
	c.mu.Lock()
	beat := map[string]any{}
	for k, v := range c.frontendHeartbeat {
		beat[k] = v
	}
	c.mu.Unlock()
	state := "unknown"
	if at, ok := beat["receivedAt"].(int64); ok {
		age := time.Now().UnixMilli() - at
		beat["ageMs"] = age
		state = "healthy"
		if age > 3000 {
			state = "degraded"
		}
		if age > 10000 {
			state = "unhealthy"
		}
		if beat["visibility"] != "visible" && age > 3000 {
			state = "unknown"
		}
	}
	beat["state"] = state
	out["frontendHeartbeat"] = beat
	out["browserDebug"] = c.BrowserDebugCapabilities(ctx)
	if c.bridge != nil {
		out["bridge"] = c.bridge.Stats()
	}
	if c.app.manager != nil {
		out["sessions"] = c.app.manager.List()
		out["tunnels"] = c.app.manager.ListTunnels()
	}
	if c.app.window != nil {
		out["window"] = map[string]any{"windowId": "main", "visible": c.app.window.IsVisible(), "focused": c.app.window.IsFocused(), "minimized": c.app.window.IsMinimised()}
	}
	return out
}

// RuntimeObservation keeps process diagnostics available even when JavaScript
// or the frontend request bridge no longer responds.
func (c *debugController) RuntimeObservation(ctx context.Context) map[string]any {
	return c.runtimeObservation(ctx)
}

var _ interface {
	RuntimeObservation(context.Context) map[string]any
} = (*debugController)(nil)
