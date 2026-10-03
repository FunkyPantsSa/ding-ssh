package debugsrv

// M10：用「应用里保存的凭证」执行 sudo -i（工具 terminal.sudo，能力位 sudo.credential）。
//
// # 为什么需要它
//
// 很多运维动作（改系统配置、装包、重启服务）必须 root。此前 AI 只能让用户在终端里手敲
// 密码，或者用户自己开免密 sudo；本工具让 AI 在**用户显式授权**（能力位 + 两段式确认）后，
// 用**应用里已经保存的那份密码**完成提权，密码本身永远不经过 AI 的手。
//
// # 密码是怎么流动的（本包永远拿不到明文）
//
//  1. 本包生成随机临时文件路径 /tmp/.ding-sudo-<12 位十六进制>，并过既有白名单
//     （sftpRequireWritePath → NormalizeRemotePath / SFTPPathAllowed）；
//  2. 本包调宿主 op OpSudoStage：宿主（debug.go 的 handleSudoOp）解析「会话 → 服务器节点」，
//     按与 servers.get includeSecrets 同一条路径（findServer → App.GetServers → store.List）
//     取出保存的密码，用**会话已有的那份 SFTP 客户端**写入 `<密码>\n`；本包只拿到「写成功了」；
//  3. 本包用终端执行**一条** shell：chmod 600 → sudo -S -p '' -i -- /bin/sh -c '<command>' < 临时文件 →
//     rm -f → ls 校验 → 打印退出码标记。整条用 `;` 串联，无论中途成败都会走到清理；
//  4. 返回前调宿主 op OpSudoScrub，把捕获输出里出现的密码替换成 ***（宿主是唯一持有明文的一侧），
//     返回里的 scrubbed 字段说明「是否已确认返回的文本不含该密码」。
//
// # 清理有两条通道，独立 SFTP 通道才是可靠的（2026-10 修的缺陷）
//
// 现象：`get_hd_smartinfo -d 1 -i 1 | head -45` 这类**永不返回**的命令下，工具超时返回，
// 而当时唯一的兜底清理是「在同一条远端 shell 里再发 `rm -f` + `ls` 校验」—— 它排在用户命令之后，
// 用户命令不返回 ⇒ 兜底行从未执行；随后靠 Ctrl-C 恢复终端时，tty 在 SIGINT 时会**丢弃输入队列**，
// 兜底行同样丢失 ⇒ 目标机残留含明文密码的 /tmp/.ding-sudo-<12hex>。
//
// 修法：清理分两条通道，任一确认删除即 cleanedUp=true。
//
//	通道 A（原 shell）：执行那条 shell 里的 `rm -f` + `ls` 校验（标记出现时 ls 退出码非 0 = 已删除）；
//	通道 B（独立 SFTP，核心）：宿主 op OpSudoCleanup —— 用**该会话已有的 SFTP 客户端** Remove + Stat 复核。
//	        它与终端作业无关（unlink 不受前景进程是否阻塞影响），因此超时/异常场景下也真的能删掉；
//	        通道 B 先做（更快更可靠），只有它没确认删除时才退回通道 A 的终端兜底（尽力而为）。
//
// 事后兜底入口：cleanupOnly（字符串，远端路径）—— 超时/异常之后调用方用上一次返回里的 tempFile
// 再调一次本工具，就能确认残留文件是否已删除（幂等）；它跳过取密码 / stage / 终端，因此即使
// 终端仍被挂起的作业占着也能用。
//
// # 安全硬要求（改这个文件前先读一遍）
//
//  1. **工具参数里永远没有 password 字段**（schema 里也不要出现），也不允许从 command 里透传密码；
//     调用方显式传 password 时直接拒绝（永不接受）；
//  2. 密码不得出现在：审计 args（本工具 args 只有 id / command / timeoutMs / idleMs / cleanupOnly / confirm）、
//     任何 logx 输出、返回值原文（返回前必须净化）、MCP 资源；
//  3. 临时文件必须**随机命名** + **chmod 600** + **用完即删** + **校验删除**（ls 的退出码），
//     删除放在与执行同一条 shell 命令里（`;` 串联 + `|| true` 兜底），并且返回前**总是**再走一次
//     通道 B（独立 SFTP Remove + Stat 复核），标记没出现时同样要先清理再返回；
//  4. 密码**只经由文件重定向**进入 sudo（`sudo -S … < 文件`），绝不进命令行；
//  5. 不支持本机终端、没有保存密码的服务器；白名单不含 /tmp 时直接拒绝（中文说明去配置）；
//  6. 清理入口（cleanupOnly / OpSudoCleanup）只接受本工具自己生成的路径形态
//     ^/tmp/\.ding-sudo-[0-9a-f]{12}$，其它路径一律拒绝 —— 否则它就成了任意删除原语。
//
// # 能力位与确认（登记见本文件 init）
//
//	RegisterToolCap("terminal.sudo", CapSudoCredential, CapTerminalInput)
//	RegisterToolConfirm("terminal.sudo", "terminal.sudo") // 等价于把该机器的 root 交给 AI → 必须两段式
//
// CapSudoCredential 还要求 AllowSecrets=true（密码要从凭据库读出来），缺哪一项由宿主 Gate 明确指出来。
// 并发：与 terminal.run / terminal.expect 共用同一份「同会话串行化」名额（acquireTerminal），不另开一套。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- 工具名 / 宿主 op 名 ----

const (
	// OpTerminalSudo 工具名（严格按规格：terminal.sudo）。
	OpTerminalSudo = "terminal.sudo"

	// OpSudoStage 宿主 op：解析会话 → 服务器节点 → 保存的密码，并写进指定的远端临时文件。
	//
	// 入参 {"sessionId":…, "path":…}；返回值只回 {"ok":true, "path":…, "bytes":…}。
	// 密码只存在于宿主进程内（凭据库 → 内存 → SFTP），绝不回给 debugsrv / MCP。
	OpSudoStage = "sudo.stage"

	// OpSudoScrub 宿主 op：把输出里出现的「该会话对应的保存密码」替换成 ***。
	//
	// 入参 {"sessionId":…, "text":…}；返回 {"text":…, "scrubbed":bool}。
	// 为什么由宿主做：净化需要明文密码，而明文只在宿主侧；让 debugsrv 拿到密码会立刻破坏上面第 2 条硬要求。
	OpSudoScrub = "sudo.scrub"

	// OpSudoCleanup 宿主 op：用**会话已有的 SFTP 客户端**删除本工具生成的临时文件，并 Stat 复核。
	//
	// 入参 {"sessionId":…, "path":…}；返回 {"removed":bool, "existed":bool, "verified":bool, "reason":string}：
	//   - removed / existed 描述「本次删除是否成功 / 删除前文件是否存在」；
	//   - verified=true 表示**删除后复核确认文件已不存在**（文件本来就不存在时同样为 true，幂等）；
	//   - reason 是中文原因（拿不到 SFTP 客户端 / 删除失败 / 复核失败），绝不包含密码；
	//   - path 必须严格匹配本工具生成的形态（见 IsSudoTempPath），并仍要过远端写入白名单；
	//     拿不到 SFTP 客户端时返回 verified=false + 中文原因，**不退化**成「再发一条终端命令」
	//     （终端可能仍被挂起的作业占着，这正是要走独立通道的原因）。
	//
	// 为什么需要它：见文件顶部「清理有两条通道」—— 命令永不返回时，终端兜底清理会丢，含密码的
	// 临时文件会留在目标机；SFTP 删除与终端作业无关，因此是超时/异常场景下唯一可靠的清理路径。
	OpSudoCleanup = "sudo.cleanup"
)

// ---- 限额与常量 ----

const (
	// sudoTempPrefix 远端临时文件前缀（完整形态 /tmp/.ding-sudo-<12 位十六进制>）。
	sudoTempPrefix = "/tmp/.ding-sudo-"
	// sudoTempRandomBytes 随机后缀字节数（6 字节 → 12 位十六进制，与规格一致）。
	sudoTempRandomBytes = 6
	// sudoMarkerPrefix 退出码标记前缀（真实打印形态：__DING_SUDO_RC__<退出码>__LS__<ls 退出码>__CHMOD__<0|1>）。
	sudoMarkerPrefix = "__DING_SUDO_RC__"
	// sudoCleanMarker 兜底清理的标记前缀（打印形态：__DING_SUDO_CLEAN__<ls 退出码>）。
	sudoCleanMarker = "__DING_SUDO_CLEAN__"
	// sudoCleanupWait 兜底清理的等待上限（标记没出现时用它，避免调用方长时间挂住）。
	sudoCleanupWait = 2 * time.Second
	// sudoCleanupTimeout 独立清理通道（宿主 op OpSudoCleanup）的上下文超时。
	//
	// 它是「返回前必须完成」的一步，因此给它一个有界上下文（调用方已取消时也照样清理）。
	// 说明（不要误以为它能掐断一切）：真正的 SFTP 调用（Remove / Stat）由 pkg/sftp 自己的
	// 连接级超时兜底，不受 ctx 影响；这里约束的是「宿主 op 的分发与实现」，避免异常实现
	// 无限期挂住调用方。取值给到秒级（懒建的 SFTP 客户端可能需要一次子系统握手）。
	sudoCleanupTimeout = 8 * time.Second
)

// sudoTempPathShape 临时文件路径的人类可读形态（用于中文错误与提示，不要写死在多处字符串里）。
const sudoTempPathShape = "/tmp/.ding-sudo-<12 位十六进制>"

// reSudoTempFile 严格匹配本工具生成的临时文件路径。
//
// 为什么必须严格匹配：cleanupOnly 与宿主 op sudo.cleanup 都会收到**调用方指定**的路径，
// 若不锁定形态，它们就成了「任意路径删除原语」。远端写入白名单是按**前缀**匹配的
// （/tmp 会放行 /tmp 下的任何文件），因此这里必须再锁一层「只认本工具自己的文件名」。
var reSudoTempFile = regexp.MustCompile(`^/tmp/\.ding-sudo-[0-9a-f]{12}$`)

// IsSudoTempPath 判断路径是否严格等于本工具生成的临时文件形态（导出给宿主复核用）。
func IsSudoTempPath(p string) bool {
	return reSudoTempFile.MatchString(strings.TrimSpace(p))
}

// reSudoMarker 匹配真实退出码标记。
//
// 刻意要求后面跟数字：命令行回显里是字面量 __DING_SUDO_RC__%s__LS__%s__CHMOD__%s，
// 不会命中；只有 shell 真的执行到最后的 printf 才会出现数字形态。
var reSudoMarker = regexp.MustCompile(sudoMarkerPrefix + `(-?[0-9]{1,3})__LS__([0-9]{1,3})__CHMOD__([01])`)

// reSudoCleanMarker 匹配兜底清理的标记（ls 的退出码：非 0 = 文件确实不在了）。
var reSudoCleanMarker = regexp.MustCompile(sudoCleanMarker + `([0-9]{1,3})`)

// ---- 能力位 / 两段式确认登记（照 caps.go 顶部注释：各域在自己文件的 init 里登记）----

func init() {
	registerSudoCaps()
}

// registerSudoCaps 登记 terminal.sudo 的能力位与确认动作（幂等，测试可重复调用）。
//
// 能力位两个都必要：
//   - CapSudoCredential：本次操作的本质（用保存的密码提权），还要求 AllowSecrets=true；
//   - CapTerminalInput：它仍然是在终端里执行命令（等价于代替用户敲命令）。
func registerSudoCaps() {
	RegisterToolCap(OpTerminalSudo, CapSudoCredential, CapTerminalInput)
	RegisterToolConfirm(OpTerminalSudo, OpTerminalSudo)
}

// sudoWantRegistry 规格表：工具 → 能力位 → 确认 action（供 sudo_test.go 断言，与登记表一一对应）。
var sudoWantRegistry = []struct {
	Tool   string
	Cap    Capability
	Action string
}{
	{OpTerminalSudo, CapSudoCredential, OpTerminalSudo},
	{OpTerminalSudo, CapTerminalInput, OpTerminalSudo},
}

// ---- 工具条目 ----

// sudoToolEntries 返回 terminal.sudo 的条目（描述末尾的「能力要求」行由 mcp.go 自动追加）。
//
// schema 里**没有** password 字段，这是规格里的硬要求：密码只能来自应用里保存的凭证。
func sudoToolEntries() []mcpTool {
	return []mcpTool{
		{Name: OpTerminalSudo, Description: "在**当前会话对应的服务器**上，用**应用里保存的密码**执行 `sudo -S -p '' -i -- /bin/sh -c '<command>'`（提权执行一条命令）。" +
			"适合「必须 root 才能做」的运维动作（改系统配置、装包、重启服务）。" +
			"**密码从哪来**：只从应用里为该服务器保存的密码读取（等同 servers.get includeSecrets 的宿主路径），" +
			"并且只经由远端随机临时文件（/tmp/.ding-sudo-<随机 12 位十六进制>、写入后立即 chmod 600、" +
			"执行时用 `< 文件` 重定向喂给 sudo -S、无论成败都会删除并校验）传给 sudo。" +
			"密码**不会**出现在 MCP 参数 / 返回值 / 日志 / 审计里；工具 schema 里没有 password 字段，**也不接受**调用方传入密码；" +
			"临时文件路径必须命中远端写入白名单（白名单不含 /tmp 时直接拒绝，并提示去 设置 → 调试模式 → MCP 能力 里加）。" +
			"返回 {ok, sudo:true, sessionId, matched, reason, elapsedMs, exitCode?, tempFile, cleanedUp, cleanupAttempted, cleanupVerified, scrubbed, output, outputBytes, truncated, hint?}：" +
			"ok=true 表示流程走完（拿到退出码与清理结果），**命令本身的成败看 exitCode**；" +
			"cleanedUp=true 表示结束时会话已确认临时文件不存在（原 shell 的 ls 校验 **或** 独立清理通道任一确认即可）；" +
			"cleanupAttempted=true 表示已调用独立清理通道（宿主用会话已有的 SFTP 客户端 Remove + Stat 复核，与终端作业无关，" +
			"因此命令挂起/超时时也生效），cleanupVerified=true 表示该通道复核确认文件已不存在；" +
			"cleanedUp=false 时 hint 会给出**残留文件的完整路径**与中文处置建议（请删除并轮换该账号密码）。" +
			"scrubbed=true 表示返回值已确认不含保存的密码（需要时已替换成 ***），" +
			"false 表示净化未生效（此时**请勿**把本段输出落盘 / 写入对话记录）。" +
			"识别常见失败并给中文提示：密码错误（Sorry, try again / incorrect password）→ 保存的密码不正确或该账号无 sudo 权限；" +
			"超时 / 静默 → 中文说明（**不报协议错误**）。" +
			"**超时 / 异常后的事后清理**：用 cleanupOnly 参数（值取上一次返回里的 tempFile）再调一次本工具，即可只做清理与复核、" +
			"不取密码、不写临时文件、不碰终端（因此终端仍被挂起的作业占着也能用），幂等（文件不存在也算成功）。" +
			"不支持：本机终端、没有保存密码的服务器、以及由调用方直接传入密码（永不接受）。" +
			"注意：命令由远端 shell 执行（`sudo -S ... < 文件`、`chmod`、`rm`、`ls` 都是 POSIX 语义），" +
			"Windows OpenSSH 默认 cmd / PowerShell 登录 shell 下不适用。" +
			"并发：与 terminal.run / terminal.expect 共用同一份「同会话串行化」名额，第二个调用被直接拒绝（不排队）；" +
			"cleanupOnly 不占这个名额（它不碰终端）。",
			InputSchema: obj(map[string]any{
				"id":        strProp("终端 id（标签 clientId，见 list_terminals）。必须是 SSH 会话：本机终端会被拒绝"),
				"command":   strProp("要用 root 执行的命令（以 /bin/sh -c 执行，支持 ; / 管道等 shell 语法；不需要也不能传密码）。传 cleanupOnly 时可省略"),
				"timeoutMs": intProp("总超时毫秒（默认 30000，范围 200–600000）"),
				"idleMs":    intProp("可选：输出静默窗口毫秒（50–60000）。省略 = 不启用「静默提前返回」，一直等到退出码标记或 timeoutMs（慢命令建议省略）"),
				"cleanupOnly": strProp("可选：**事后兜底清理入口**，值为远端路径。用于超时 / 异常后清理可能残留的临时文件；" +
					"不接受任意路径，只接受本工具生成的 /tmp/.ding-sudo-<12hex> 形态（其它路径一律中文拒绝）。" +
					"传了它 ⇒ 跳过取密码与 stage，只对该路径执行独立清理（会话已有 SFTP 客户端 Remove + Stat 复核），" +
					"返回 {ok, sudo:false, cleanupOnly:true, sessionId, path, removed, existed, cleanupAttempted, cleanupVerified, cleanedUp, hint?}（幂等：文件不存在也算成功）"),
				"confirm": strProp("两段式确认 token（confirm.prepare，action=terminal.sudo；别名 token 亦可）"),
			}, "id")},
	}
}

// ---- 主流程 ----

// terminalSudo 实现 terminal.sudo。
func (s *Server) terminalSudo(ctx context.Context, args map[string]any) (any, error) {
	id := appctlString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("%w: 缺少 id（终端 id，见 list_terminals）", ErrBadInput)
	}
	// 安全硬要求 1：永不接受调用方传入的密码（schema 里也没有这个字段）。
	if _, has := args["password"]; has {
		return nil, fmt.Errorf("%w: 不接受 password 参数：terminal.sudo 只用应用里为该服务器保存的密码；"+
			"请先在应用里为这台服务器保存密码（服务器 → 编辑 → 密码），再重试", ErrBadInput)
	}
	// 事后兜底入口（cleanupOnly）：只清理指定路径，不取密码、不写临时文件、不碰终端。
	// 因此它放在「command / timeoutMs / Hub」这些主流程前置条件之前 —— 超时之后调用方
	// 很可能只想清掉残留文件，不该被「缺少 command」之类的主流程校验挡住。
	//
	// 判定依据是「键存在且不是 null」：显式传空字符串会被当成 cleanupOnly 模式并明确拒绝
	//（说清要传完整路径），而 JSON null / 缺键走主流程 —— 模型经常给可选字段填 null。
	if raw, has := args["cleanupOnly"]; has && raw != nil {
		return s.sudoCleanupOnly(ctx, id, appctlString(args, "cleanupOnly"))
	}
	command := appctlString(args, "command")
	if command == "" {
		return nil, fmt.Errorf("%w: 缺少 command（要用 root 执行的命令）", ErrBadInput)
	}
	timeoutMS, err := sftpIntArg(args, "timeoutMs", terminalDefTimeoutMS, terminalMinTimeoutMS, terminalMaxTimeoutMS)
	if err != nil {
		return nil, err
	}
	// idleMs：省略 = 0 = 不启用「静默提前返回」（默认等到退出码标记或 timeoutMs）——
	// sudo 下面的命令往往「先静默后吐输出」，默认按静默判定会过早返回、只拿到半截结果。
	idleMS := 0
	if v, has := appctlInt(args, "idleMs"); has {
		if v < terminalMinIdleMS || v > terminalMaxIdleMS {
			return nil, fmt.Errorf("%w: idleMs 取值 %d 越界（允许 %d–%d；省略 = 不启用静默提前返回）",
				ErrBadInput, v, terminalMinIdleMS, terminalMaxIdleMS)
		}
		idleMS = v
	}
	if s.opts.Hub == nil {
		return nil, errors.New("事件流未启用（Hub 为空），无法捕获终端输出")
	}

	// 1) 随机临时文件路径 + 白名单（白名单不含 /tmp 时在这里被拒，且不消耗任何远端资源）。
	tempFile, err := s.sudoTempPath()
	if err != nil {
		return nil, err
	}

	// 2) 占住会话名额（与 terminal.run / terminal.expect 共用，避免互相偷走输出增量）。
	if !s.acquireTerminal(id) {
		return nil, terminalBusyError(id)
	}
	defer s.releaseTerminal(id)

	// 3) 订阅必须在「写入密码 / 发送任何字符」之前：Hub 不缓存历史事件。
	hubID, ch := s.opts.Hub.Subscribe(TerminalOutputTopic)
	defer s.opts.Hub.Unsubscribe(hubID)

	// 4) 宿主把 `<密码>\n` 写进临时文件（本包只拿到「写成功了」，永远不接触明文）。
	if _, err := s.callSFTPHost(ctx, OpSudoStage, map[string]any{"sessionId": id, "path": tempFile}); err != nil {
		return nil, err
	}
	// 写入后复核一次白名单：接下来要把这个路径拼进 shell 命令，必须确认它仍是受控路径。
	if _, err := s.sftpRequireWritePath("sudo 临时文件（写入后复核）", tempFile); err != nil {
		return nil, err
	}

	// 5) 一条 shell 完成：chmod 600 → sudo（密码经文件重定向）→ rm -f → ls 校验 → 退出码标记。
	line := sudoShellLine(tempFile, command)
	sent := line + "\r"
	if _, err := s.callSFTPHost(ctx, OpTerminalInput, map[string]any{"id": id, "data": sent}); err != nil {
		// 发送失败也必须清理：密码文件已经落在远端了。
		// 同样先走独立 SFTP 通道（可靠），只有它没确认删除时才退回终端兜底（尽力而为）。
		cleanup, cleaned := s.sudoCleanupBoth(ctx, ch, id, tempFile, false)
		extra := ""
		if !cleaned {
			extra = "；" + sudoResidualHint(tempFile, cleanup.reason)
		}
		return nil, fmt.Errorf("向终端 %s 发送 sudo 命令失败：%w（已请求删除临时文件，cleanedUp=%v%s）", id, err, cleaned, extra)
	}

	wait := s.waitTerminal(ctx, ch, id, reSudoMarker,
		time.Duration(timeoutMS)*time.Millisecond, idleMS, strings.TrimRight(sent, "\r\n"))

	// 6) 解析标记与清理结果：两条清理通道，任一确认删除即 cleanedUp=true。
	//
	// 通道 A（原 shell）：执行那条 shell 自带 `rm -f` + `ls` 校验，标记出现时 ls 的退出码
	//   就是判据（非 0 = 文件确实不在了）。
	// 通道 B（独立 SFTP，核心修复）：无论成功 / 失败 / 超时，都在返回前调宿主 op OpSudoCleanup，
	//   用会话已有的 SFTP 客户端 Remove + Stat 复核 —— 它与终端作业无关（unlink 不受前景进程
	//   是否阻塞影响），因此命令永不返回时也能真正删掉含密码的临时文件。
	// 顺序：先 B（更快更可靠）再按需退到 A 的终端兜底（挂起时那行可能被 tty 丢弃，只作最后手段）。
	visible := wait.visible
	rc, lsExit, chmodFailed, hasMarker := parseSudoMarker(visible)
	matched := hasMarker
	reason := wait.reason
	if matched {
		reason = "matched"
	}
	shellVerified := matched && lsExit != 0
	cleanup, cleanedUp := s.sudoCleanupBoth(ctx, ch, id, tempFile, shellVerified)

	// 7) 输出净化：先净化再截断（反过来会把跨截断点的密码切成两半，打码就打不中了）。
	//    净化由宿主完成（只有它持有明文密码），本包永远拿不到密码本身。
	scrubbedText, scrubbed := s.sudoScrubOutput(ctx, id, visible)
	out, truncated := truncateStringBytes(scrubbedText, terminalDefMaxBytes)

	res := map[string]any{
		"ok":               matched && !chmodFailed,
		"sudo":             true,
		"sessionId":        id,
		"matched":          matched,
		"reason":           reason,
		"elapsedMs":        wait.elapsed.Milliseconds(),
		"tempFile":         tempFile,
		"cleanedUp":        cleanedUp,
		"cleanupAttempted": cleanup.attempted,
		"cleanupVerified":  cleanup.verified,
		"scrubbed":         scrubbed,
		"output":           out,
		"outputBytes":      len(scrubbedText),
		"truncated":        truncated,
	}
	if matched && !chmodFailed {
		res["exitCode"] = rc
	}
	if idleMS > 0 {
		res["idleMs"] = idleMS
	}
	if hint := sudoHint(sudoHintInput{
		matched: matched, reason: reason, chmodFailed: chmodFailed, exitCode: rc,
		cleanedUp: cleanedUp, timeoutMS: timeoutMS, idleMS: idleMS, output: scrubbedText,
		tempFile: tempFile, cleanupReason: cleanup.reason,
	}); hint != "" {
		res["hint"] = hint
	}
	if !scrubbed {
		res["scrubHint"] = "输出净化未生效（宿主未返回净化结果）：本段输出未经密码打码，请勿落盘 / 勿写入对话记录"
	}
	if len(scrubbedText) > len(out) {
		res["truncatedHint"] = fmt.Sprintf("可见文本共 %d 字节，按 %d 字节上限截断（只保留开头部分）",
			len(scrubbedText), terminalDefMaxBytes)
	}
	return res, nil
}

// ---- 清理（两条通道） ----

// sudoCleanupResult 独立清理通道（OpSudoCleanup）的结果。
type sudoCleanupResult struct {
	// attempted：是否真的调了宿主 op（用于返回里的 cleanupAttempted，如实报告）。
	attempted bool
	// verified：宿主复核确认文件已不存在（幂等：本来就不存在时同样是 true）。
	verified bool
	// removed / existed：本次删除是否成功 / 删除前文件是否存在（如实报告，便于排查）。
	removed bool
	existed bool
	// reason：未确认删除时的中文原因（绝不包含密码）。
	reason string
}

// sudoCleanupViaHost 走独立 SFTP 通道删除临时文件（宿主 op OpSudoCleanup）。
//
// 这是超时/异常场景下唯一可靠的清理路径：它不经过终端、不排队、不受前景作业阻塞影响。
// 拿不到客户端 / 删除失败时如实返回 verified=false + 中文原因，**不退化**成终端命令。
func (s *Server) sudoCleanupViaHost(ctx context.Context, id, path string) sudoCleanupResult {
	res := sudoCleanupResult{attempted: true}
	callCtx, cancel := sudoCleanupContext(ctx)
	defer cancel()
	m, err := s.callSFTPHost(callCtx, OpSudoCleanup, map[string]any{"sessionId": id, "path": path})
	if err != nil {
		res.reason = err.Error()
		return res
	}
	res.removed = hostBool(m, "removed")
	res.existed = hostBool(m, "existed")
	res.verified = hostBool(m, "verified")
	res.reason = hostStr(m, "reason")
	if !res.verified && strings.TrimSpace(res.reason) == "" {
		res.reason = "宿主未确认删除（返回里 verified=false）"
	}
	return res
}

// sudoCleanupContext 派生独立清理用的上下文。
//
// 为什么不用原 ctx：调用方取消（reason=canceled）时**仍然**必须清理 —— 密码文件不会因为
// 调用方不等了就变得不重要。因此剥掉取消信号（保留值），再叠一个有界超时（见 sudoCleanupTimeout）。
func sudoCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), sudoCleanupTimeout)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), sudoCleanupTimeout)
}

// sudoCleanupBoth 依次走「独立 SFTP 通道」与「终端兜底通道」，返回清理结果与「是否已确认删除」。
//
// 语义（与返回里的 cleanedUp 一一对应）= 两条通道取或：shellVerified 是执行那条 shell 自带的
// ls 校验结果（标记出现时才有意义），cleanup.verified 是独立 SFTP 通道的复核结果。
// 终端兜底只在两者都没确认删除时才发（节省一次往返，也避免向可能被挂起作业占着的终端再写字符）；
// 它只是**最后手段**：命令挂起时那行 rm 可能被 tty 丢弃（这正是本次修复的缺陷成因）。
func (s *Server) sudoCleanupBoth(ctx context.Context, ch <-chan Event, id, tempFile string, shellVerified bool) (sudoCleanupResult, bool) {
	cleanup := s.sudoCleanupViaHost(ctx, id, tempFile)
	if cleanup.verified || shellVerified {
		return cleanup, true
	}
	if s.sudoCleanupAttempt(ctx, ch, id, tempFile) {
		return cleanup, true
	}
	return cleanup, false
}

// sudoRequireCleanupPath 校验「清理目标路径」，返回通过校验的路径。
//
// 两道校验缺一不可：
//  1. 形态：必须严格匹配本工具生成的 /tmp/.ding-sudo-<12 位十六进制>（见 IsSudoTempPath）——
//     cleanupOnly / OpSudoCleanup 都是删除动作，不锁形态就是任意删除原语；
//  2. 白名单：仍要过既有远端写入白名单（与 sftp.remove 同一套规则函数）。
func (s *Server) sudoRequireCleanupPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("%w: cleanupOnly 为空：请传入本工具生成的临时文件路径（形态 %s）", ErrBadInput, sudoTempPathShape)
	}
	if !IsSudoTempPath(p) {
		return "", fmt.Errorf("%w: 拒绝清理路径 %q：只接受本工具生成的临时文件（严格匹配 %s），"+
			"不接受任意路径 —— 这是超时/异常后的清理入口，不是删除原语；"+
			"请把上一次返回里的 tempFile 字段原样传进来", ErrBadInput, p, sudoTempPathShape)
	}
	norm, err := s.sftpRequireWritePath("sudo 临时文件清理", p)
	if err != nil {
		return "", err
	}
	if norm != p {
		return "", fmt.Errorf("%w: 清理路径 %q 归一化后变成 %q：拒绝使用", ErrBadInput, p, norm)
	}
	return p, nil
}

// preflightSudoArgs 在「能力校验之后、token 校验之前」做 sudo 域的预检（mcp.go 调用）。
//
// 目前只有一条：cleanupOnly 的路径形态 + 白名单。放在这里的理由与 M8 的 preflightToolArgs
// 相同 —— 一次性 token 被消费后不能再用，会被拒绝的路径不该白烧一个 token
// （cleanupOnly 是超时后的补救入口，更不该多耗一轮 confirm.prepare）。
func (s *Server) preflightSudoArgs(name string, args map[string]any) error {
	if name != OpTerminalSudo {
		return nil
	}
	// 与 terminalSudo 的判定保持一致：键存在且不是 null 才算走了 cleanupOnly 模式。
	if raw, has := args["cleanupOnly"]; !has || raw == nil {
		return nil
	}
	_, err := s.sudoRequireCleanupPath(appctlString(args, "cleanupOnly"))
	return err
}

// sudoCleanupOnly 实现 terminal.sudo 的 cleanupOnly 分支：只清理，不提权。
//
// 用途：超时 / 异常（命令挂起、终端被挂起的作业占着、上一次调用直接超时）之后，调用方用上一次
// 返回里的 tempFile 再调一次本工具，即可确认残留文件是否已删除；幂等（文件不存在也算成功）。
// 不取密码、不写临时文件、不向终端写任何字符，因此也不占「同会话串行化」名额。
//
// 返回 {ok, sudo:false, cleanupOnly:true, sessionId, path, removed, existed,
// cleanupAttempted, cleanupVerified, cleanedUp, hint?}：ok/cleanedUp = cleanupVerified。
func (s *Server) sudoCleanupOnly(ctx context.Context, id, rawPath string) (any, error) {
	path, err := s.sudoRequireCleanupPath(rawPath)
	if err != nil {
		return nil, err
	}
	cleanup := s.sudoCleanupViaHost(ctx, id, path)
	res := map[string]any{
		"ok":               cleanup.verified,
		"sudo":             false,
		"cleanupOnly":      true,
		"sessionId":        id,
		"path":             path,
		"removed":          cleanup.removed,
		"existed":          cleanup.existed,
		"cleanupAttempted": cleanup.attempted,
		"cleanupVerified":  cleanup.verified,
		"cleanedUp":        cleanup.verified,
	}
	if !cleanup.verified {
		res["hint"] = sudoResidualHint(path, cleanup.reason)
	}
	return res, nil
}

// ---- 临时文件路径 ----

// sudoTempPath 生成随机临时文件路径，并过既有远端写入白名单。
//
// 随机性要求（安全硬要求 3）：每次调用的文件名都不同，避免被预测 / 被其它本地用户预先创建；
// 6 字节 crypto/rand → 12 位十六进制，与规格里的 /tmp/.ding-sudo-<12 位十六进制> 一致。
func (s *Server) sudoTempPath() (string, error) {
	buf := make([]byte, sudoTempRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 sudo 临时文件名失败（随机源不可用）：%v", err)
	}
	raw := sudoTempPrefix + hex.EncodeToString(buf)
	p, err := s.sftpRequireWritePath("sudo 临时文件", raw)
	if err != nil {
		return "", fmt.Errorf("sudo 凭证提权被拒绝：临时文件 %s 未通过远端写入白名单校验。%w；"+
			"本工具必须把密码写进 %s<随机 12 位十六进制> 的临时文件（写完立即 chmod 600、用完即删），"+
			"因此请在 设置 → 调试模式 → MCP 能力 →「远端文件写入」白名单里加上 /tmp 前缀后重试",
			raw, err, sudoTempPrefix)
	}
	// 兜底：即使白名单规则日后被放宽，也只允许使用本工具自己的前缀（防止拼进 shell 的路径被改写）。
	if p != raw || !strings.HasPrefix(p, sudoTempPrefix) {
		return "", fmt.Errorf("sudo 临时文件路径 %q 不在受控前缀 %s 下：拒绝使用", p, sudoTempPrefix)
	}
	return p, nil
}

// sudoShellLine 拼出「执行 + 清理 + 校验」的那一条 shell 命令。
//
// 为什么必须是一条：密码文件在远端存在期间是敏感数据，清理绝不能依赖另一次 MCP 调用
// （调用方可能超时 / 断开）。整条用 `;` 串联，chmod / sudo / rm / ls 任何一步失败都会继续往下走：
//
//	__ding_sudo_chmod=0; chmod 600 <f> || __ding_sudo_chmod=1;
//	if [ "$__ding_sudo_chmod" = "0" ]; then sudo -S -p '' -i -- /bin/sh -c '<command>' < <f>; __ding_sudo_rc=$?;
//	else __ding_sudo_rc=-1; fi;
//	rm -f <f> || true; ls -d <f> >/dev/null 2>&1;
//	printf '\n__DING_SUDO_RC__%s__LS__%s__CHMOD__%s\n' "$__ding_sudo_rc" "$?" "$__ding_sudo_chmod"
//
// chmod 失败时**不执行** sudo（宁可不提权，也不让密码文件以更宽的权限留在磁盘上）；
// ls 的退出码就是「文件是否还在」的判据（非 0 = 已删除）。
//
// 提权命令为什么是 `-i -- /bin/sh -c '<command>'`（这里踩过两个真实的坑，改动前请先读这段）：
//
//	坑 1：sudo **没有 `-c` 选项**（`-c` 是 su/sh 的用法）。早先这里拼的是
//	      `sudo -S -p '' -i -c '<command>'`，sudo 把 `-c` 当非法选项，直接打印 usage 并以 1 退出 ——
//	      表现是「exitCode=1 + output 是一段 usage」，而**不是**密码错误（sudo 在读密码前就退出了），
//	      排查时极易误判成「保存的密码不对」。
//	坑 2：`sudo -s` / `sudo -i` 接受命令时，会把**每个位置参数重新加引号**再拼成一条命令串交给登录
//	      shell。因此把整条复合命令作为**一个**参数传进去会被包成一个命令名：单字命令（`id`）能跑，
//	      多字命令直接失败（`id; uname -srmo; whoami: command not found`，exit 127）。
//
// 2026-10 在真机（QNAP，Linux 5.10.60-qnap aarch64，sudo 1.9.12p2）实测：
//
//	sudo -n -i -c id                       → usage（坑 1：-c 非法）
//	sudo -i -- id                          → uid=0(admin)（单字命令可以跑）
//	sudo -i -- 'id -u'                     → -sh: id -u: command not found（坑 2）
//	sudo -i -- /bin/sh -c 'id; uname -srmo; whoami' → 正常输出（本次采用的最终形式）
//
// 所以最终形式是 `-i -- /bin/sh -c '<command>'`：`--` 结束 sudo 的选项解析；`/bin/sh` 与 `-c` 是
// sudo 的两个位置参数（sudo 重新加引号不会改变它们去掉引号后的取值）；复合命令整体作为第三个参数，
// 由 sudo 转交给登录 shell，再由内层 `sh -c` 执行 —— 含空格 / 分号 / 引号的命令因此都能原样跑通，
// 同时保留 `-i` 的登录环境（PATH 里带 sbin 等）。若将来遇到不支持 `--` 的老 sudo，
// 可退化为把命令直接交给 `-i`，但**不要**再用 `-c`。
func sudoShellLine(tempFile, command string) string {
	f := shellSingleQuote(tempFile)
	return fmt.Sprintf(
		"__ding_sudo_chmod=0; chmod 600 %s 2>/dev/null || __ding_sudo_chmod=1; "+
			"if [ \"$__ding_sudo_chmod\" = \"0\" ]; then sudo -S -p '' -i -- /bin/sh -c %s < %s; __ding_sudo_rc=$?; "+
			"else __ding_sudo_rc=-1; fi; "+
			"rm -f %s || true; ls -d %s >/dev/null 2>&1; "+
			"printf '\\n%s%%s__LS__%%s__CHMOD__%%s\\n' \"$__ding_sudo_rc\" \"$?\" \"$__ding_sudo_chmod\"",
		f, shellSingleQuote(command), f, f, f, sudoMarkerPrefix)
}

// sudoCleanupLine 兜底清理命令：标记没出现时再删一次并校验（同一会话、同一条 shell）。
func sudoCleanupLine(tempFile string) string {
	f := shellSingleQuote(tempFile)
	return fmt.Sprintf("rm -f %s || true; ls -d %s >/dev/null 2>&1; printf '\\n%s%%s\\n' \"$?\"",
		f, f, sudoCleanMarker)
}

// sudoCleanupAttempt 终端兜底清理：再发一条 `rm -f` + `ls` 校验并等一会儿标记，返回「已确认不存在」。
//
// 这是**最后手段**，只在独立 SFTP 通道（OpSudoCleanup）也没确认删除时才调用（见 sudoCleanupBoth）：
// 命令挂起时这行会排在挂起的作业之后，恢复终端（Ctrl-C）时 tty 还会丢弃输入队列 —— 也就是
// 本次修复的那个缺陷场景，因此它**不能**作为唯一依赖。它仍有价值：SFTP 不可用（例如远端没开
// sftp 子系统）而终端正常时，它是唯一的补救通道。
func (s *Server) sudoCleanupAttempt(ctx context.Context, ch <-chan Event, id, tempFile string) bool {
	line := sudoCleanupLine(tempFile)
	if _, err := s.callSFTPHost(ctx, OpTerminalInput, map[string]any{"id": id, "data": line + "\r"}); err != nil {
		return false
	}
	wait := s.waitTerminal(ctx, ch, id, reSudoCleanMarker, sudoCleanupWait, 0, strings.TrimRight(line, "\r\n"))
	return sudoCleanupOK(wait.visible)
}

// shellSingleQuote 把字符串包进单引号（内部的单引号用「反斜杠 + 单引号」序列替换），
// 保证原样传给远端 shell，不会被 shell 解释。
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parseSudoMarker 解析退出码标记（取最后一次出现的那条）。
//
// 返回值：sudo 的退出码、ls 的退出码（非 0 = 临时文件确实已删除）、chmod 是否失败、是否找到标记。
func parseSudoMarker(visible string) (rc int, lsExit int, chmodFailed bool, ok bool) {
	all := reSudoMarker.FindAllStringSubmatch(visible, -1)
	if len(all) == 0 {
		return 0, 0, false, false
	}
	m := all[len(all)-1]
	rc, err1 := strconv.Atoi(m[1])
	ls, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false, false
	}
	return rc, ls, m[3] == "1", true
}

// sudoCleanupOK 从兜底清理的输出里判断「临时文件是否已确认删除」。
func sudoCleanupOK(visible string) bool {
	all := reSudoCleanMarker.FindAllStringSubmatch(visible, -1)
	if len(all) == 0 {
		return false
	}
	code, err := strconv.Atoi(all[len(all)-1][1])
	if err != nil {
		return false
	}
	return code != 0 // ls 非 0 = 文件不存在
}

// ---- 输出净化 ----

// sudoScrubOutput 调宿主把输出里出现的密码替换成 ***，返回净化后的文本与「是否已确认不含密码」。
//
// scrubbed=true 的语义是「可以安全记录这段输出」：宿主要么替换过，要么确认输出里本来就没有密码。
// 拿不到净化结果时**按未净化如实报告**（返回 scrubbed=false），由调用方在返回里明确警告，
// 而不是假装已经打码 —— 后者会让调用方误以为输出是安全的。
func (s *Server) sudoScrubOutput(ctx context.Context, sessionID, text string) (string, bool) {
	if strings.TrimSpace(text) == "" || s.opts.Handler == nil {
		return text, false
	}
	raw, err := json.Marshal(map[string]any{"sessionId": sessionID, "text": text})
	if err != nil {
		return text, false
	}
	v, err := s.opts.Handler.Handle(ctx, OpSudoScrub, raw)
	if err != nil {
		return text, false
	}
	m := appctlObj(v)
	out, isStr := m["text"].(string)
	if !isStr {
		return text, false
	}
	return out, hostBool(m, "scrubbed")
}

// ---- 中文提示 ----

// sudoHintInput sudoHint 的输入（把判定需要的事实集中在一处，便于测试直接构造）。
type sudoHintInput struct {
	matched     bool
	reason      string
	chmodFailed bool
	exitCode    int
	cleanedUp   bool
	timeoutMS   int
	idleMS      int
	output      string
	// tempFile 本次使用的远端临时文件路径：清理未确认时用它给出「残留路径 + 处置建议」。
	tempFile string
	// cleanupReason 独立清理通道未确认删除时的中文原因（verified=false 时非空）。
	cleanupReason string
}

// sudoResidualHint 生成「临时文件可能残留」的中文警告：必须给出**完整残留路径**与处置建议。
//
// 这是安全关键提示（文件里是明文密码），因此即使调用方不读其它字段也必须能看懂怎么办。
func sudoResidualHint(path, reason string) string {
	path = strings.TrimSpace(path)
	msg := fmt.Sprintf("警告：临时文件可能仍残留在目标机 %s（**该文件含明文密码**）。"+
		"请立即在目标机上执行 rm -f %s 删除，并轮换该账号密码；"+
		"删除后可用 terminal.sudo{id, cleanupOnly:\"%s\"} 复查（幂等：文件不存在也算成功）",
		path, path, path)
	if r := strings.TrimSpace(reason); r != "" {
		msg += "。独立清理通道未确认删除的原因：" + r
	}
	return msg
}

// sudoHint 生成中文提示：清理未确认时**先**给出残留路径与处置建议（安全优先），再接既有判定。
//
// 原则与 terminal.run 一致：超时 / 静默都**不是协议错误**，返回里必须能读懂怎么办。
func sudoHint(in sudoHintInput) string {
	base := sudoBaseHint(in)
	if in.cleanedUp || strings.TrimSpace(in.tempFile) == "" {
		return base
	}
	warn := sudoResidualHint(in.tempFile, in.cleanupReason)
	if base == "" {
		return warn
	}
	return warn + "\n" + base
}

// sudoBaseHint 既有判定：先报「为什么没拿到退出码 / 为什么没执行」，再报常见失败原因。
func sudoBaseHint(in sudoHintInput) string {
	switch {
	case in.chmodFailed:
		return "临时文件 chmod 600 失败：已放弃执行 sudo 并删除临时文件（远端可能不支持 chmod，或 /tmp 不可写）。" +
			"请检查该服务器的权限 / 文件系统，或改用免密 sudo"
	case !in.matched && in.reason == "timeout":
		return fmt.Sprintf("sudo 在 %dms 内超时：没有拿到退出码标记（**不是协议错误**）：命令可能仍在执行，或卡在交互式提示。"+
			"已尝试清理临时文件（cleanedUp=%v）；可加大 timeoutMs，或在该机器上配置免密 sudo 后改用 terminal.run",
			in.timeoutMS, in.cleanedUp)
	case !in.matched && in.reason == "idle":
		return fmt.Sprintf("输出静默超过 idleMs=%dms：命令可能卡在交互式输入（例如远端要求 TTY）（**不是协议错误**）。"+
			"已尝试清理临时文件（cleanedUp=%v）；可加大 idleMs / timeoutMs，或省略 idleMs 一直等到退出码标记",
			in.idleMS, in.cleanedUp)
	case !in.matched && in.reason == "closed":
		return "事件流已关闭（会话可能已断开）：返回的是断开前捕获到的输出；已尝试清理临时文件（独立 SFTP 通道 + 终端兜底）"
	case !in.matched && in.reason == "canceled":
		return "调用方取消了请求：返回的是取消前捕获到的输出；已尝试清理临时文件（独立 SFTP 通道 + 终端兜底）"
	}
	lower := strings.ToLower(in.output)
	switch {
	case strings.Contains(in.output, "Sorry, try again") || strings.Contains(lower, "incorrect password"):
		return "保存的密码不正确或该账号无 sudo 权限（请核对后重试，或改配免密 sudo）"
	case strings.Contains(lower, "a terminal is required to read the password"),
		strings.Contains(lower, "no tty present"),
		strings.Contains(lower, "a password is required"):
		return "远端 sudo 无法从标准输入读到密码（要求 TTY）：请确认该账号可以用密码 sudo，或在该机器上配置免密 sudo"
	case strings.Contains(lower, "not in the sudoers file"),
		strings.Contains(lower, "is not allowed to execute"):
		return "该账号没有 sudo 权限（不在 sudoers 里）：请让管理员授权，或改用有权限的账号"
	case in.exitCode == 127:
		return "远端没有 sudo 命令（退出码 127）：请改用免密 sudo / root 账号，或先安装 sudo"
	case in.exitCode != 0:
		return fmt.Sprintf("命令以退出码 %d 失败：完整输出见 output（这**不是**权限 / 密码问题也会走到这里）", in.exitCode)
	}
	if in.matched {
		return fmt.Sprintf("sudo 已执行（exitCode=%d）；临时文件已清理并校验（cleanedUp=%v）",
			in.exitCode, in.cleanedUp)
	}
	return ""
}
