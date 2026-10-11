package debugsrv

// M9：terminal.run 的两个体验缺陷回归。
//
// 1) expect 被「命令回显」假命中：PTY 先回显命令行，若 expect 的模式出现在命令里
//   （命令 `echo ===DONE===` + expect `===DONE===`），实测 1ms 就返回 matched=true，
//   而 output 里只有命令行本身。修法：匹配只在回显之后的文本上进行（terminalEchoEnd）。
// 2) 未给 expect 时的「输出静默」判定：命令回显会重置静默窗口，导致
//   「先回显、后静默、最后才吐输出」的命令（如 docker ps）过早返回；现在回显不算真实输出，
//   真实输出出现前保持更宽的宽限（terminalFirstIdleMS），并新增 idleMs 参数。

import (
	"strings"
	"testing"
	"time"
)

// 只有命令回显（命令还没执行完）时，expect 不得命中。
func TestTerminalRunExpectIgnoresCommandEcho(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		// 只回显命令行（真实输出还没来）：这正是线上实测到的场景。
		publishOutput(env.hub, id, "echo ===M9DONE===\r\n")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "echo ===M9DONE===", "expect": "===M9DONE===", "timeoutMs": 300,
	})
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("只有命令回显时不应命中：%v", out)
	}
	if body, _ := out["output"].(string); !strings.Contains(body, "echo ===M9DONE===") {
		t.Fatalf("output 仍应如实包含命令行回显（只是不参与匹配）：%q", body)
	}
	if uiNum(out["echoSkippedBytes"]) <= 0 {
		t.Fatalf("应报告跳过的回显字节数：%v", out)
	}
}

// 回显之后出现真实输出 → 命中；且命中的是真实输出。
func TestTerminalRunExpectMatchesRealOutputAfterEcho(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, "$ echo ===M9DONE===\r\n===M9DONE===\r\n$ ")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "echo ===M9DONE===", "expect": "===M9DONE===", "timeoutMs": 3000,
	})
	if out["matched"] != true || out["reason"] != "matched" {
		t.Fatalf("回显之后的真实输出应命中：%v", out)
	}
}

// 分块到达：先到半截回显（不含换行），再到完整回显 → 全程不得命中。
func TestTerminalRunExpectIgnoresPartialEcho(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, "echo ===M9D")
		publishOutput(env.hub, id, "ONE===\r\n")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "echo ===M9DONE===", "expect": "===M9DONE===", "timeoutMs": 300,
	})
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("分块到达的命令回显也不应命中：%v", out)
	}
}

// 终端不回声（或命令文本无法定位）时不做跳过：真实输出即使只有一个词也能命中。
func TestTerminalRunExpectStillMatchesWithoutEcho(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		// 没有命令行回显，直接就是输出（例如 stty -echo）
		publishOutput(env.hub, id, "READY")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "printf READY", "expect": "READY", "timeoutMs": 3000,
	})
	if out["matched"] != true || out["reason"] != "matched" {
		t.Fatalf("无回显时应正常命中：%v", out)
	}
	if _, has := out["echoSkippedBytes"]; has {
		t.Fatalf("没发现回显时不应报告 echoSkippedBytes：%v", out)
	}
}

// wrapExitMarker 自动等待退出码标记：只有回显时不得命中，真实标记到达才命中。
func TestTerminalRunExitMarkerIgnoresEcho(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, sent string) {
		// 回显包裹后的命令（含字面量 __DING_EXIT__%s），随后才是真实退出码标记
		publishOutput(env.hub, id, sent+"\r\n")
		publishOutput(env.hub, id, "hello\r\n__DING_EXIT__7\r\n$ ")
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "false", "wrapExitMarker": true, "timeoutMs": 3000,
	})
	if out["matched"] != true || out["exitMarker"] != float64(7) {
		t.Fatalf("应命中真实退出码标记：%v", out)
	}
}

// terminal.expect 不发送数据 → 不做回显过滤（pattern 在全部新增文本上匹配）。
func TestTerminalExpectDoesNotFilterEcho(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	// terminal.expect 不发送数据，因此输出要在**订阅之后**到达：用 goroutine 延迟一点发布。
	go func() {
		time.Sleep(50 * time.Millisecond)
		publishOutput(env.hub, "tab-1", "echo FOO\r\n")
	}()
	out := env.callJSON(t, OpTerminalExpect, map[string]any{
		"id": "tab-1", "pattern": "echo FOO", "timeoutMs": 3000,
	})
	if out["matched"] != true {
		t.Fatalf("terminal.expect 不做回显过滤：%v", out)
	}
}

// 未给 expect：命令回显不算「真实输出」，静默窗口必须等到真实输出之后才开始缩短，
// 否则「先回显、后静默、最后才吐输出」的命令会被过早判定为跑完。
func TestTerminalRunIdleWaitsForRealOutput(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		// 只有回显：模拟「命令启动慢、还没吐输出」
		publishOutput(env.hub, id, "docker ps\r\n")
	}
	start := time.Now()
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "docker ps", "timeoutMs": 8000,
	})
	elapsed := time.Since(start)
	if out["reason"] != "idle" {
		t.Fatalf("未给 expect 时应以 idle 结束：%v", out)
	}
	if elapsed < time.Duration(terminalFirstIdleMS)*time.Millisecond {
		t.Fatalf("真实输出未出现时应保持更宽的宽限（>=%dms），实际 %v", terminalFirstIdleMS, elapsed)
	}
}

// idleMs 参数：可自定义静默窗口，越界返回中文错误。
func TestTerminalRunIdleMsParameter(t *testing.T) {
	ensureSFTPRegistrations(t)
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		// 回显 + 真实输出：真实输出出现后静默窗口按 idleMs 收窄
		publishOutput(env.hub, id, "uname -s\r\nLinux\r\n")
	}
	start := time.Now()
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "uname -s", "idleMs": 80, "timeoutMs": 8000,
	})
	elapsed := time.Since(start)
	if out["reason"] != "idle" {
		t.Fatalf("应以 idle 结束：%v", out)
	}
	if uiNum(out["idleMs"]) != 80 {
		t.Fatalf("返回里应如实给出 idleMs：%v", out["idleMs"])
	}
	if elapsed > time.Duration(terminalIdleMS)*time.Millisecond {
		t.Fatalf("idleMs=80 应比默认 %dms 更快返回，实际 %v", terminalIdleMS, elapsed)
	}
	// 越界（过大 / 过小）都要中文报错
	if text := env.callErr(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": "ls", "idleMs": 10,
	}); !strings.Contains(text, "idleMs") && !strings.Contains(text, "越界") {
		t.Fatalf("过小的 idleMs 应返回中文错误：%s", text)
	}
}

// ---- M10：超长命令（折行回显）的回显定位 ----
//
// 实测缺陷：命令比终端宽度长时，PTY 会把回显折行成交错的多段（可见文本里出现额外换行），
// 旧的「按换行逐字定位」找不到整行 → 回显不被跳过 → expect 命中回显残片（或 echoSkippedBytes 失真）。
// 修法：新增长度口径（逐字节比较、比较时跳过换行），与换行口径取更稳的那个（见 terminalEchoEnd）。
//
// 下面四个用例分别覆盖：> 40 列、> 200 列（多次折行）、多行命令、命令含中文（字节数 vs 显示宽度）。

// fakeWrappedEcho 造「被 PTY 折行的命令回显」：每 wrapAt 个字符插一个换行（\r\n → 归一成 \n）。
func fakeWrappedEcho(sent string, wrapAt int) string {
	var sb strings.Builder
	n := 0
	for _, r := range sent {
		sb.WriteRune(r)
		n++
		if wrapAt > 0 && n%wrapAt == 0 {
			sb.WriteString("\r\n")
		}
	}
	sb.WriteString("\r\n")
	return sb.String()
}

// 命令超过 40 列（终端宽度）→ 回显被折成两段：回显里的模式不得命中，真实输出才命中。
func TestTerminalRunEchoSkipLongCommand40Cols(t *testing.T) {
	ensureSFTPRegistrations(t)
	// 60+ 列的命令，模式 ===W40=== 在折行点之前；后面全是无害的注释填充。
	command := "echo ===W40===; : " + strings.Repeat("#", 50)
	if len(command) <= 40 {
		t.Fatalf("用例需要命令长度 > 40，实际 %d", len(command))
	}

	// 1) 只有折行回显（命令还没输出结果）→ 不得命中
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, fakeWrappedEcho(command, 40))
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===W40===", "timeoutMs": 300,
	})
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("折行的命令回显不得命中：%v", out)
	}
	if uiNum(out["echoSkippedBytes"]) < float64(len(command)) {
		t.Fatalf("应跳过整段回显（至少 %d 字节），实际 %v", len(command), out["echoSkippedBytes"])
	}

	// 2) 折行回显之后出现真实输出 → 命中真实输出
	env2 := newSFTPEnv(t)
	env2.h.onInput = func(id, _ string) {
		publishOutput(env2.hub, id, fakeWrappedEcho(command, 40))
		publishOutput(env2.hub, id, "===W40===REAL\r\n")
	}
	out2 := env2.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===W40===REAL", "timeoutMs": 3000,
	})
	if out2["matched"] != true || out2["reason"] != "matched" {
		t.Fatalf("回显之后的真实输出应命中：%v", out2)
	}
}

// 命令超过 200 列（多次折行）→ 回显被折成多段：同样不得命中回显。
func TestTerminalRunEchoSkipLongCommand200Cols(t *testing.T) {
	ensureSFTPRegistrations(t)
	// 首段是模式，随后是 400 个字符的填充（远超 200 列，会产生多次折行）。
	command := "echo ===W200===; : " + strings.Repeat("y", 400)
	if len(command) <= 200 {
		t.Fatalf("用例需要命令长度 > 200，实际 %d", len(command))
	}
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, fakeWrappedEcho(command, 200))
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===W200===", "timeoutMs": 300,
	})
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("多次折行的回显不得命中：%v", out)
	}
	if uiNum(out["echoSkippedBytes"]) < float64(len(command)) {
		t.Fatalf("应跳过整段回显（至少 %d 字节），实际 %v", len(command), out["echoSkippedBytes"])
	}
}

// 多行命令：只按**第一行**定位回显（后续行的回显会与第一行的真实输出交错，不能一起吞掉）。
func TestTerminalRunEchoSkipMultilineCommand(t *testing.T) {
	ensureSFTPRegistrations(t)
	line1 := "echo ===MLFIRST===; : " + strings.Repeat("z", 30) // 首行 > 40 列，会被折行
	command := line1 + "\necho ===MLSECOND==="

	// 1) 只有首行的折行回显 → 模式（出现在首行里）不得命中
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, fakeWrappedEcho(line1, 40))
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===MLFIRST===", "timeoutMs": 300,
	})
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("多行命令的首行回显（折行）不得命中：%v", out)
	}
	if uiNum(out["echoSkippedBytes"]) < float64(len(line1)) {
		t.Fatalf("应跳过首行回显（至少 %d 字节），实际 %v", len(line1), out["echoSkippedBytes"])
	}

	// 2) 首行回显 + 首行真实输出 → 命中真实输出
	env2 := newSFTPEnv(t)
	env2.h.onInput = func(id, _ string) {
		publishOutput(env2.hub, id, fakeWrappedEcho(line1, 40))
		publishOutput(env2.hub, id, "===MLFIRST===\r\n")
	}
	out2 := env2.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===MLFIRST===", "timeoutMs": 3000,
	})
	if out2["matched"] != true || out2["reason"] != "matched" {
		t.Fatalf("首行的真实输出应命中：%v", out2)
	}
}

// 命令含中文：显示宽度 ≈ 2 倍字符数、字节数 = 3 倍字符数，两个口径都不等于「终端列数」。
// 长度口径按**字节**比较，因此折行回显一样能被完整跳过。
func TestTerminalRunEchoSkipChineseCommand(t *testing.T) {
	ensureSFTPRegistrations(t)
	// 模式放在命令最后：任何「按显示宽度提前收尾」的实现都会把模式留在回显残片里 → 假命中。
	command := "echo 这是一条很长的中文命令用来验证字节长度与显示宽度的差异 ===CNMARK==="
	if len(command) <= 40 {
		t.Fatalf("用例需要命令字节数 > 40，实际 %d", len(command))
	}
	env := newSFTPEnv(t)
	env.h.onInput = func(id, _ string) {
		publishOutput(env.hub, id, fakeWrappedEcho(command, 40))
	}
	out := env.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===CNMARK===", "timeoutMs": 300,
	})
	if out["matched"] != false || out["reason"] != "timeout" {
		t.Fatalf("中文长命令的折行回显不得命中：%v", out)
	}
	if uiNum(out["echoSkippedBytes"]) < float64(len(command)) {
		t.Fatalf("字节口径应跳过整段回显（至少 %d 字节），实际 %v", len(command), out["echoSkippedBytes"])
	}

	// 真实输出里的中文模式仍要能命中
	env2 := newSFTPEnv(t)
	env2.h.onInput = func(id, _ string) {
		publishOutput(env2.hub, id, fakeWrappedEcho(command, 40))
		publishOutput(env2.hub, id, "===CNMARK===已执行\r\n")
	}
	out2 := env2.callJSON(t, OpTerminalRun, map[string]any{
		"id": "tab-1", "command": command, "expect": "===CNMARK===已执行", "timeoutMs": 3000,
	})
	if out2["matched"] != true {
		t.Fatalf("回显之后的真实输出（含中文）应命中：%v", out2)
	}
}

// 单元口径：terminalEchoEnd 的两个口径分别覆盖「逐字命中」与「折行命中」。
func TestTerminalEchoEndCandidates(t *testing.T) {
	// 逐字命中（无折行）：与 M9 行为一致
	if got := terminalEchoEnd("echo abc\nOUT\n", "echo abc"); got != len("echo abc\n") {
		t.Fatalf("逐字命中的回显结束位置 = %d，期望 %d", got, len("echo abc\n"))
	}
	// 折行命中：可见文本里插入了换行，逐字口径失效，长度口径必须给出同一结果
	wrapped := "echo abc\ndef\nOUT\n"
	if got := terminalEchoEndByNewline(wrapped, "echo abcdef"); got != 0 {
		t.Fatalf("折行时逐字口径应定位失败，实际 %d", got)
	}
	if got := terminalEchoEndByLength(wrapped, "echo abcdef"); got != len("echo abc\ndef\n") {
		t.Fatalf("折行时长度口径 = %d，期望 %d", got, len("echo abc\ndef\n"))
	}
	if got := terminalEchoEnd(wrapped, "echo abcdef"); got != len("echo abc\ndef\n") {
		t.Fatalf("折行时 terminalEchoEnd = %d，期望 %d", got, len("echo abc\ndef\n"))
	}
	// 终端不回声（可见文本是真实输出）→ 不做跳过
	if got := terminalEchoEnd("READY\n", "printf READY"); got != 0 {
		t.Fatalf("无回显时不应跳过，实际 %d", got)
	}
	// 回显还没收完（只有前缀）→ 不跳过
	if got := terminalEchoEnd("echo abc", "echo abcdef"); got != 0 {
		t.Fatalf("回显未收完时不应跳过，实际 %d", got)
	}
	// 多行命令只按第一行定位
	echoCmd := "echo first\necho second"
	if got := terminalEchoEnd("echo first\nOUT\n", echoCmd); got != len("echo first\n") {
		t.Fatalf("多行命令应按第一行定位 = %d，期望 %d", got, len("echo first\n"))
	}
}
