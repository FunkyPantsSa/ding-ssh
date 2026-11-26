package debugsrv

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
)

const OpToolsSearch = "tools.search"
const toolProfileHeader = "X-Ding-SSH-Tool-Profile"

type toolProfileContextKey struct{}

func init() { registerDiscoveryCaps() }

func registerDiscoveryCaps() { RegisterToolCap(OpToolsSearch, CapRead) }

// Profiles reduce discovery noise only. They never grant authority, and a known
// tool remains callable through the same authorization checks in every profile.
func normalizeToolProfile(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "full", nil
	}
	switch value {
	case "full", "operations", "debug":
		return value, nil
	}
	return "", fmt.Errorf("%w: tool profile 必须为 full、operations 或 debug", ErrBadInput)
}

func requestToolProfile(r *http.Request) (string, bool, error) {
	value := r.Header.Get(toolProfileHeader)
	if value == "" {
		value = r.URL.Query().Get("profile")
	}
	profile, err := normalizeToolProfile(value)
	return profile, strings.TrimSpace(value) != "", err
}

func contextToolProfile(ctx context.Context) string {
	if value, ok := ctx.Value(toolProfileContextKey{}).(string); ok {
		return value
	}
	return "full"
}

var canonicalToolNames = map[string]string{
	"open_tab":           OpSessionsConnect,
	"close_tab":          "sessions.close",
	"disconnect_session": OpSessionsDisconnect,
	"reconnect_session":  OpSessionsReconnect,
	"list_servers":       OpServersSearch,
}

var operationTools = map[string]bool{
	"app.health": true, "app.permissions": true, "app.preflight": true, OpToolsSearch: true,
	OpServersSearch: true, OpServersGet: true,
	OpSessionsConnect: true, OpSessionsGet: true, OpSessionsWait: true,
	OpSessionsReconnect: true, OpSessionsDisconnect: true, "sessions.close": true, "sessions.closeStatus": true,
	OpHostChallenges: true, OpExecStart: true, OpExecPlan: true, OpExecBatch: true,
	OpJobsGet: true, OpJobsWait: true, OpJobsOutput: true, OpJobsCancel: true, OpJobsList: true,
	OpHostsMetrics: true, OpProcessesList: true, OpServicesStatus: true, OpServicesLogs: true, OpServicesRestart: true,
	OpTunnelsHealth: true, OpTunnelsList: true,
	"confirm.prepare": true, "confirm.status": true, "confirm.commit": true,
	"sftp.list": true, "sftp.read": true, "sftp.stat": true,
	"sftp.patch.preview": true, "sftp.patch": true, "sftp.write": true,
	"sftp.upload": true, "sftp.download": true, "sftp.transfers": true, "sftp.cancel": true,
	"sftp.mkdir": true, "sftp.rename": true, "sftp.remove": true,
}

func toolInProfile(name, profile string) bool {
	if profile == "full" {
		return true
	}
	if profile == "operations" {
		return operationTools[name]
	}
	return strings.HasPrefix(name, "ui.") || strings.HasPrefix(name, "terminal.") ||
		strings.HasPrefix(name, "app.") || strings.HasPrefix(name, "confirm.") ||
		name == OpToolsSearch || name == "app_state" || name == "list_terminals" ||
		name == "read_terminal" || name == "send_input" || name == "resize_terminal" ||
		name == "scroll_terminal" || name == "screenshot" || name == "eval_js" ||
		name == "read_logs" || name == "list_logs" || name == "recent_events" ||
		strings.HasPrefix(name, "sessions.")
}

// conditionalToolCapabilities documents argument-dependent grants without
// changing the legacy capability union used by existing catalog clients.
func conditionalToolCapabilities(name string) []map[string]any {
	if name != OpExecStart {
		return nil
	}
	return []map[string]any{
		{"when": "local=true", "requires": []string{string(CapLocalExec)}},
		{"when": "local omitted or false (sessionId targets SSH)", "requires": []string{string(CapTerminalInput)}},
	}
}

func toolsForProfile(s *Server, profile string) []mcpTool {
	all := mcpTools(s)
	if profile == "full" {
		return all
	}
	filtered := make([]mcpTool, 0, len(all))
	for _, tool := range all {
		if toolInProfile(tool.Name, profile) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func decorateToolDiscovery(tool *mcpTool) {
	if tool.Meta == nil {
		tool.Meta = map[string]any{}
	}
	category := strings.SplitN(tool.Name, ".", 2)[0]
	if !strings.Contains(tool.Name, ".") {
		category = "legacy"
	}
	tool.Meta["category"] = category
	tool.Meta["recommended"] = operationTools[tool.Name]
	profiles := []string{"full"}
	for _, profile := range []string{"operations", "debug"} {
		if toolInProfile(tool.Name, profile) {
			profiles = append(profiles, profile)
		}
	}
	tool.Meta["profiles"] = profiles
	if canonical := canonicalToolNames[tool.Name]; canonical != "" {
		tool.Meta["canonicalName"] = canonical
		tool.Meta["legacyAlias"] = true
		tool.Description = "兼容入口；新流程优先使用 " + canonical + "。" + tool.Description
	}
	if tool.Name == OpTerminalRun || tool.Name == OpTerminalExpect {
		tool.Meta["executionKind"] = "interactive-shared-pty"
		tool.Meta["recommendedForCommands"] = OpExecStart
		tool.Description = "交互式终端入口。普通命令执行优先 exec.start，再用 jobs.wait/output 获取退出码与结果。" + tool.Description
	}
	if tool.Name == OpExecStart {
		tool.Meta["executionKind"] = "independent-job"
		tool.Meta["capabilityMode"] = "conditional"
		tool.Meta["capabilitySemantics"] = "The legacy capability list is a conservative union; use conditionalCapabilities for argument-specific requirements."
		tool.Meta["conditionalCapabilities"] = conditionalToolCapabilities(tool.Name)
		tool.Description = "普通命令执行的推荐入口。" + tool.Description
	}
}

func discoveryToolEntries() []mcpTool {
	return []mcpTool{{
		Name:        OpToolsSearch,
		Description: "按名称或描述发现工具及推荐工作流。默认检索完整目录；profile 可选 operations（日常运维）、debug（界面和交互调试）、full（全部）。只调整发现范围，不改变权限。includeSchema=true 返回可直接调用的输入输出 schema。",
		InputSchema: obj(map[string]any{
			"query":         strProp("名称或描述中的关键字，大小写不敏感"),
			"profile":       map[string]any{"type": "string", "enum": []string{"full", "operations", "debug"}},
			"limit":         intProp("最大结果数，默认20，范围1–200"),
			"offset":        intProp("分页偏移，默认0"),
			"includeSchema": boolProp("是否包含完整 schema，默认 false"),
		}),
		OutputSchema: outputObj(nil), structured: structuredIdentity,
	}}
}

func (s *Server) searchTools(args map[string]any) (map[string]any, error) {
	profile, err := normalizeToolProfile(appctlString(args, "profile"))
	if err != nil {
		return nil, err
	}
	limit, err := jobArgInt(args, "limit", 20, 1, 200)
	if err != nil {
		return nil, err
	}
	offset, err := jobArgInt(args, "offset", 0, 0, 1000000)
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(appctlString(args, "query")))
	items := []map[string]any{}
	for _, tool := range toolsForProfile(s, profile) {
		if query != "" && !strings.Contains(strings.ToLower(tool.Name+" "+tool.Description), query) {
			continue
		}
		entry := toolCatalogEntry(tool)
		if !appctlBool(args, "includeSchema") {
			delete(entry, "inputSchema")
			delete(entry, "outputSchema")
		}
		items = append(items, entry)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i]["name"].(string), items[j]["name"].(string)
		if operationTools[a] != operationTools[b] {
			return operationTools[a]
		}
		return a < b
	})
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	localShell, commandMode := "sh", "POSIX shell command"
	localShells := []string{"sh"}
	if runtime.GOOS == "windows" {
		localShell, commandMode = "cmd", "Windows cmd syntax by default; use shell=powershell or shell=pwsh for PowerShell syntax and UTF-8 output"
		localShells = []string{"cmd", "powershell", "pwsh"}
	}
	result := map[string]any{
		"profile": profile, "count": end - offset, "total": total, "offset": offset, "tools": items[offset:end],
		"profiles": []string{"operations", "debug", "full"},
		"platform": map[string]any{"os": runtime.GOOS, "localExecShell": localShell, "localShells": localShells, "localCommandSyntax": commandMode, "localCwdEnv": "native cwd/env supported with local=true; shell selection is local-only"},
		"workflows": map[string]any{
			"execute":     []string{"app.preflight", "exec.start", "jobs.wait", "jobs.output"},
			"session":     []string{"servers.search", "app.preflight", "sessions.connect", "sessions.close"},
			"interactive": []string{"sessions.connect", "terminal.run", "terminal.expect"},
		},
		"note": "目录分组只减少发现噪音，不授予权限；未列出的已知工具仍经过同一授权校验。操作前用 app.preflight 检查具体参数。",
	}
	if end < total {
		result["nextOffset"] = end
	}
	return result, nil
}
