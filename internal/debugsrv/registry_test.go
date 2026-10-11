package debugsrv

// M9 收尾：把原先散落在 appctl_test.go / ui_test.go / sftp_test.go 的**注册与清单断言助手**
// 归到这一个文件，避免同一个助手在多处复制（改一处漏一处会让「注册表驱动」的断言失真）。
//
// 这些助手的共同点：它们只依赖包级注册表（RegisterToolCap 注册的 toolCapRegistry），
// 与具体某个域无关，因此放在 registry_test.go 而不是某个域的测试文件里。
//
// 注意：同一包内的其它用例会调用 ResetRegistrationsForTest（见 register_test.go 的 resetRegistry），
// 因此每个用到注册表的用例都必须先调 ensure* 重新登记一遍。

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
)

// ---- 各域的能力位登记（幂等，可重复调用）----

// ensureUIRegistrations 保证 ui.* 的能力位已登记。
func ensureUIRegistrations(t *testing.T) {
	t.Helper()
	before := len(RegistrationErrors())
	registerUIToolCaps()
	errs := RegistrationErrors()
	if len(errs) > before {
		t.Fatalf("登记 ui.* 能力位时报错：%v", errs[before:])
	}
}

// ensureAppctlRegistrations 保证 M7（应用控制面）的能力位已登记。
func ensureAppctlRegistrations(t *testing.T) {
	t.Helper()
	before := len(RegistrationErrors())
	registerAppctlCaps()
	errs := RegistrationErrors()
	if len(errs) > before {
		t.Fatalf("登记应用控制面能力位时报错：%v", errs[before:])
	}
}

// ensureSFTPRegistrations 保证 M8（SFTP + 终端自动化）的能力位已登记。
func ensureSFTPRegistrations(t *testing.T) {
	t.Helper()
	before := len(RegistrationErrors())
	registerSFTPCaps()
	errs := RegistrationErrors()
	if len(errs) > before {
		t.Fatalf("登记 M8 能力位时报错：%v", errs[before:])
	}
}

// ensureAllRegistrations 重新登记全部「用注册表而不是内置表」的域（ui.* / 应用控制面 / SFTP）。
//
// 为什么四个域都要登记：注册表驱动的断言（assertRegistryToolsListed）遍历
// AllRegisteredToolNames()，只要某个域被 ResetRegistrationsForTest 清掉就会漏掉该域的工具。
func ensureAllRegistrations(t *testing.T) {
	t.Helper()
	ensureUIRegistrations(t)
	ensureAppctlRegistrations(t)
	ensureSFTPRegistrations(t)
}

// ---- tools/list 与注册表的断言助手 ----

// toolsListFromMCP 走一次 tools/list（不经过网络），返回「名字集合」与原始条目。
func toolsListFromMCP(t *testing.T, s *Server) (map[string]bool, []mcpTool) {
	t.Helper()
	resp := s.mcpDispatch(context.Background(), rpcRequest{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/list",
	})
	if resp.Error != nil {
		t.Fatalf("tools/list 失败：%+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var out struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 tools/list 失败：%v", err)
	}
	return toolNamesOf(out.Tools), out.Tools
}

// toolNamesOf 把 tools/list 的条目转成「名字 → true」集合（注册表驱动断言用）。
func toolNamesOf(tools []mcpTool) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, tool := range tools {
		out[tool.Name] = true
	}
	return out
}

// assertRegistryToolsListed 注册表驱动的核心断言：
// 每个登记过能力位的工具都必须出现在 tools/list，并且都能查到能力登记。
//
// 这是「工具总数不写死」的关键：新增工具时只要登记了能力位，这条断言就会自动覆盖它。
func assertRegistryToolsListed(t *testing.T, listed map[string]bool) {
	t.Helper()
	ensureAllRegistrations(t)
	names := AllRegisteredToolNames()
	if len(names) == 0 {
		t.Fatalf("AllRegisteredToolNames 为空（注册表被清空了？）")
	}
	if !sort.SliceIsSorted(names, func(i, j int) bool { return names[i] < names[j] }) {
		t.Fatalf("AllRegisteredToolNames 应升序：%v", names)
	}
	for _, name := range names {
		if !listed[name] {
			t.Fatalf("注册表里的工具 %s 没有出现在 tools/list（登记了能力位却不可见）", name)
		}
		if _, ok := ToolCapabilities(name); !ok {
			t.Fatalf("工具 %s 没有能力登记", name)
		}
	}
}
