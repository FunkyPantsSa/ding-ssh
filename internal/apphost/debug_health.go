package apphost

import (
	"context"
	"runtime"
	"time"
)

func (c *debugController) healthView(ctx context.Context) map[string]any {
	view := c.statusView()
	ready := map[string]any{"backendReady": true, "frontendResponsive": false, "checkedAt": time.Now().UnixMilli()}
	if c.bridge != nil {
		// A live, bounded read probes the bridge instead of treating an old
		// successful reply as evidence that the current WebView is ready.
		probeCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		_, err := c.bridge.Call(probeCtx, "ui.revision", nil)
		cancel()
		ready["frontendResponsive"] = err == nil
	}
	if ready["frontendResponsive"] == false {
		ready["retryAfterMs"] = 250
		ready["nextStep"] = "等待应用界面初始化后重新调用 app.health；后端独立任务不依赖前端"
	}
	view["readiness"] = ready
	shell := "sh"
	shells := []string{"sh"}
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shells = []string{"cmd", "powershell", "pwsh"}
	}
	view["execution"] = map[string]any{"recommendedTool": "exec.start", "interactiveTool": "terminal.run", "localDefaultShell": shell, "localShellChoices": shells, "shellAvailabilityCheckedAtExecution": true, "localCwdEnvSupported": true, "remoteShell": "SSH server configured shell"}
	return view
}
