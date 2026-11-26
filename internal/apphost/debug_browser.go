package apphost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var configuredDebugBrowserPorts sync.Map

// DebugWindowsOptions must be passed to application.Options.Windows before
// application.New. WebView2 creates one shared browser environment, so its
// remote-debugging arguments cannot be enabled after startup.
func DebugWindowsOptions(boot DebugBoot) application.WindowsOptions {
	options := debugWindowsOptionsForPlatform(boot, runtime.GOOS)
	if len(options.AdditionalBrowserArgs) != 0 {
		configuredDebugBrowserPorts.Store(boot.CDPPort, true)
	}
	return options
}

func debugWindowsOptionsForPlatform(boot DebugBoot, platform string) application.WindowsOptions {
	if platform != "windows" || !boot.Enabled || !boot.CDPEnabled || boot.CDPPort <= 0 || boot.CDPPort > 65535 {
		return application.WindowsOptions{}
	}
	return application.WindowsOptions{AdditionalBrowserArgs: []string{
		fmt.Sprintf("--remote-debugging-port=%d", boot.CDPPort),
		"--remote-debugging-address=127.0.0.1",
	}}
}

// BrowserDebugCapabilities verifies the actual localhost endpoint. Requested,
// configured and reachable are intentionally separate facts. A reachable shared
// WebView2 endpoint is not enough to assert that a target matches this window.
func (c *debugController) BrowserDebugCapabilities(ctx context.Context) map[string]any {
	out := map[string]any{"requested": c.boot.CDPEnabled, "configured": false, "reachable": false, "targetMatched": false, "ready": false, "platform": runtime.GOOS}
	if !c.boot.CDPEnabled {
		out["reasonCode"] = "CDP_DISABLED"
		return out
	}
	if runtime.GOOS != "windows" {
		out["reasonCode"] = "CDP_PLATFORM_UNSUPPORTED"
		return out
	}
	_, configured := configuredDebugBrowserPorts.Load(c.boot.CDPPort)
	if !c.boot.Enabled || c.boot.CDPPort <= 0 || c.boot.CDPPort > 65535 || !configured {
		out["reasonCode"] = "CDP_NOT_CONFIGURED"
		return out
	}
	// The main application must call DebugWindowsOptions; the port probe below
	// validates whether WebView2 actually accepted and exposed those arguments.
	out["configured"] = true
	out["port"] = c.boot.CDPPort
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", c.boot.CDPPort)
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		out["reasonCode"] = "CDP_PROBE_FAILED"
		return out
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 350 * time.Millisecond}).DialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		out["reasonCode"] = "CDP_UNREACHABLE"
		return out
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		out["reasonCode"] = "CDP_PROBE_FAILED"
		return out
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		out["reasonCode"] = "CDP_INVALID_RESPONSE"
		return out
	}
	var version struct {
		Browser         string `json:"Browser"`
		ProtocolVersion string `json:"Protocol-Version"`
		WebSocket       string `json:"webSocketDebuggerUrl"`
	}
	if json.Unmarshal(data, &version) != nil || version.Browser == "" || !strings.HasPrefix(version.WebSocket, "ws://") {
		out["reasonCode"] = "CDP_INVALID_RESPONSE"
		return out
	}
	out["reachable"] = true
	out["browser"] = version.Browser
	out["protocolVersion"] = version.ProtocolVersion
	out["reasonCode"] = "CDP_TARGET_UNVERIFIED"
	out["note"] = "endpoint responds, but this instance/window target has not been correlated; do not attach by first target"
	return out
}
