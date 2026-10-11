package debugsrv

// 工具清单一致性用例（注册表驱动）。
//
// 注：文件名沿用最初的临时探针名（zz_temp_count_test.go）—— 本工作区对文件删除 / 改名有约束，
// 内容已改为长期回归用例：数出来的东西变成了一条会失败的断言。
//
// 它守住的不变量：`mcpToolEntries()` 里的每个工具都必须有**能力登记**，
// 且只能来自两处之一 —— 各域自己的注册表（RegisterToolCap，见 AllRegisteredToolNames）
// 或内置信表 `toolCaps`。这样后续再加工具时，「有条目但忘登记能力位」会被当场抓住
//（此前只能靠 tools/list 的遍历用例间接发现）。
//
// 同时反向断言：注册表里的工具都必须有工具条目（否则登记了却不会出现在 tools/list 里）。
// 总数刻意不写死：只断言「条目数 >= 内置表条数」这类结构性下限，数量以运行时为准。

import "testing"

func TestToolEntryRegistryConsistency(t *testing.T) {
	ensureAllRegistrations(t)
	registered := map[string]bool{}
	for _, n := range AllRegisteredToolNames() {
		registered[n] = true
	}
	entries := mcpToolEntries()
	if len(entries) == 0 {
		t.Fatalf("工具条目表为空（mcpToolEntries）")
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
		if _, ok := ToolCapabilities(e.Name); !ok {
			t.Fatalf("工具 %s 有工具条目，但既不在能力注册表也不在内置 toolCaps 里", e.Name)
		}
		if registered[e.Name] {
			continue
		}
		if _, builtin := toolCaps[e.Name]; !builtin {
			t.Fatalf("工具 %s 既不在注册表（RegisterToolCap）也不在内置能力表 toolCaps 里", e.Name)
		}
	}
	for _, n := range AllRegisteredToolNames() {
		if !names[n] {
			t.Fatalf("注册表里的工具 %s 没有工具条目（不会出现在 tools/list）", n)
		}
	}
	if len(entries) < len(toolCaps) {
		t.Fatalf("工具条目数 %d 少于内置能力表条数 %d（内置工具有条目缺失）", len(entries), len(toolCaps))
	}
	// 规格锚点：M10 新增的 terminal.sudo 必须是「注册表登记 + 有条目」的那一类。
	if !registered[OpTerminalSudo] || !names[OpTerminalSudo] {
		t.Fatalf("terminal.sudo 应同时出现在注册表与工具条目里：registered=%v listed=%v",
			registered[OpTerminalSudo], names[OpTerminalSudo])
	}
}
