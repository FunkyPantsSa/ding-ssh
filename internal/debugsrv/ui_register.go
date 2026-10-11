package debugsrv

// UI 观测 / 实验域的能力位注册。
//
// 为什么单独一个文件：地基（caps.go）明确规定「三方并行开发不许改内置表 toolCaps」，
// 各域必须在**自己的文件**的 init() 里用 RegisterToolCap / RegisterToolConfirm 登记。
// 这里把全部 ui.* 工具一次登记完，ui_test.go 会重新调用 registerUIToolCaps() 以保证
// 用例之间不互相影响（RegisterToolCap 幂等，重复登记以最后一次为准），
// 并断言 RegistrationErrors() 为空 —— 拼错能力名这类问题不会静默通过。
//
// 能力位划分：
//   - 读类（layout/query/styles/tokens/hit/tree/console/revision/screenshot/elementShot/
//     snapshot/diff/assert/store）：注册 CapRead（永远允许，不受任何开关限制）；
//   - 写类（setToken/injectCSS/storePatch/navigate/window/reload/click/type/key/hover/scroll）：
//     注册 CapUIWrite（ui.write）。
//
// 为什么不给写类注册 RegisterToolConfirm：它们都是可逆 / 高频操作（改的是内存里的
// CSS / store / 合成事件，刷新即还原），按地基规则「能力位 = 允不允许这类操作，
// 两段式 = 不可逆操作前再想一次」，它们只受能力位约束。
func init() {
	registerUIToolCaps()
}

// registerUIToolCaps 登记全部 ui.* 工具的能力位（幂等，测试可重复调用）。
func registerUIToolCaps() {
	for _, name := range uiToolNames {
		if uiWriteTools[name] {
			RegisterToolCap(name, CapUIWrite)
			continue
		}
		RegisterToolCap(name, CapRead)
	}
}
