# 变更日志（CHANGELOG）

本项目**尚未发布正式版本**，因此本文件不按版本号归档，而是**按开发顺序倒序**记录已完成的、
可对外说明的变更（最新在最前）。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
分类使用：**新增 / 变更 / 修复 / 文档**。

每条尽量写成「现象 → 原因 → 修法」，便于回溯当时的判断依据。

---

## [未发布] 2026-10-01 · M10 · sudo 凭证提权（`terminal.sudo`）与超长命令回显修复

工具面从 88 个扩到 **89 个**（`tools/list` 与 `GET /v1/tools` 的 `count` 一致）。登记口径：**63** 个由各域在能力注册表里登记（`AllRegisteredToolNames()` 的长度 = ui 25 + 应用控制 25 + SFTP/终端 12 + `terminal.sudo` 1），另外 **26** 个内置工具的能力登记在内置表 `toolCaps` 里（`screenshot` 不登记能力位）。数量请以运行时为准，不要写死；新增用例 `TestToolEntryRegistryConsistency` 会在「有条目但没登记能力位」时直接失败。

### 新增

- **能力位 `sudo.credential`（默认关）**：允许 AI 在**当前会话对应的服务器**上，用**应用里保存的密码**执行 `sudo -i`。前置条件是**两个**：能力位本身 + 「允许读取敏感数据」（`secrets.read`，密码只从凭据库读）——缺哪一个都会在中文拒绝原因里明确写出来；后端 `NormalizeDebugSettings` 与前端 `normalizeDebug` 都做了 `!allowSecrets ⇒ capSudoCredential=false` 的兜底（与 `capSecretWrite` 同款），前端「设置 → 调试模式 → MCP 能力」里**开启需二次确认**（`GetCapabilities().requiresConfirm` 已列出 `sudo.credential`）。
- **工具 `terminal.sudo {id, command, timeoutMs?, idleMs?}`**：能力位 `sudo.credential` + `terminal.input`，**每次执行仍需两段式确认**（`confirm.prepare(action="terminal.sudo")`，`ConfirmableActions()` 已包含它；未带 token 时返回中文提示且不执行）。工作流程：先过远端写入白名单（`/tmp` 不在白名单 → 中文拒绝并说明去哪加）→ 生成**随机**临时文件 `/tmp/.ding-sudo-<12 位十六进制>` → 宿主按 `servers.get includeSecrets=true` 的同一条路径（`App.GetServers` → 存储层解密）取出保存的密码，用**会话已有的 SFTP 客户端**写入 `<密码>\n`（返回值只有 `{ok, path, bytes}`）→ 在**同一条 shell 命令**里 `chmod 600`（失败则不执行 sudo）→ `sudo -S -p '' -i -c '<command>' < <临时文件>`（密码**只经由文件重定向**，绝不进命令行）→ `rm -f ... || true` → `ls -d` 校验删除 → 打印退出码标记 → **输出净化**（宿主把输出里出现的密码换成 `***`）→ 返回 `{ok, sudo:true, sessionId, matched, reason, elapsedMs, exitCode?, tempFile, cleanedUp, scrubbed, output, outputBytes, truncated, hint?}`。标记没出现（超时 / 静默 / 流关闭 / 取消）或发送失败时都会再补一次清理并如实报告 `cleanedUp`；密码错误 / 无 sudo 权限 / 缺 TTY / 没有 sudo 命令 / chmod 失败都给中文 `hint`，超时与静默**不是协议错误**。与本机终端、没有保存密码的服务器、以及调用方直接传入密码（**永不接受**，`schema` 里也没有 `password` 字段）都不兼容；并发与 `terminal.run` / `terminal.expect` 共用同一份「同会话串行化」名额。

### 修复

- **`terminal.run` 超长命令的回显定位失准** —— 现象是命令比终端宽度长时，PTY 把回显折行成交错的多段（可见文本里多出换行），旧的「逐字命中 + 取其后第一个换行」定位失败或只命中残片，于是回显残片会参与 `expect` 匹配、`echoSkippedBytes` 也不准（实测发现）；原因是回显被折行后命令文本在可见文本里不再连续，而 `\r` 归一化又让片段被拼接。修法是给回显定位加一个**长度口径**（`terminalEchoEndByLength` / `echoMatchAt`：逐字节与命令比较、比较时**跳过折行插入的换行**，中文按字节数算与显示宽度无关；候选起点扫描上限 4096 字节），与既有换行口径 `terminalEchoEndByNewline` **取较大者**（折行时换行口径低估、不折行时它最精确），两者都定位不到则退回「不跳过」的保守行为；顺带在 `waitTerminal` 里加了「回显已收尾在换行处就不再重复定位」的短路，避免在大段输出上反复做字节比较。

### 文档

- `README.md`：「能力位与两段式确认」一节补上 `sudo.credential` 的**设计说明**（开启条件、完整工作流程、边界与「不做的事」、以及用 `sudoers.d` 配置**免密 sudo** 或「密钥 + 受限 sudo 规则」的更安全替代），工具总览与能力位表更新为 89 个工具 / 10 个能力位，「SFTP 与终端自动化」补 `terminal.sudo` 与超长命令回显定位的说明；顺带修正工具登记口径的表述（此前写成「87 个登记在能力注册表」，实际 `AllRegisteredToolNames()` 的长度是 63：各域注册 62 + M10 新增 1，26 个内置工具在内置表 `toolCaps` 里）。

---

## [未发布] 2026-10-01 · MCP 扩展与收尾（能力位 / 确认 / 审计 / ui.* / 应用控制 / SFTP）

工具面从最初的内置 21 个扩展到 **88 个**（`tools/list` 与 `GET /v1/tools` 的 `count` 一致）。登记口径当时写成了「87 个登记在能力注册表里（`AllRegisteredToolNames()` 的长度）」，实际是 **62**（ui 25 + 应用控制 25 + SFTP/终端 12）—— 另外 26 个内置工具的能力登记在内置表 `toolCaps` 里；该表述已在 M10 的文档更新里改正，并补了注册表一致性用例。数量请以运行时为准，不要写死。

### 新增

- **能力位（权限内核）**：把「调试模式总开关」细分成 9 个能力 —— `read`（永远允许）、`terminal.input`（默认开）、`ui.write`、`config.write`、`secrets.read`、`secrets.write`、`fs.remote.write`、`lifecycle`、`eval`（沿用「允许执行 JS」）。宿主在每次工具调用前校验，被拒时返回**中文原因 + 怎么开启**（不是协议错误）。前端在 **设置 → 调试模式 → MCP 能力** 逐项开关。
- **两段式确认**：破坏性 / 不可逆操作必须先 `confirm.prepare`（返回一次性 token，绑定 `action` + `args`，默认 120 秒过期）再 `confirm.commit` 或把 token 交给写工具；**可逆 / 高频操作（`send_input`、`terminal.run/expect`、`eval_js`、全部 `ui.*` 写入）只受能力位约束，不需要 token**。开启敏感能力本身（`capability.enable`）也在需要确认的清单里。
- **审计与「AI 记录」页签**：每次调用（成功 / 被拒 / 失败）都留一条结构化记录（来源 `mcp|http|ui`、工具名、能力位、耗时、结果、**已脱敏**参数、可否撤销），环形缓冲保留 500 条；应用内可过滤查看，设置类改动（`update_settings` / `set_log_options`）可一键**撤销**（保留最近 20 份改动前快照）；AI 侧对应 `app.recent_calls`。
- **UI 观测与实验（25 个 `ui.*` 工具）**：观测用 `ui.layout` / `ui.query` / `ui.styles` / `ui.tokens` / `ui.hit` / `ui.tree` / `ui.console` / `ui.revision` / `ui.screenshot` / `ui.elementShot` / `ui.snapshot` / `ui.diff` / `ui.assert` / `ui.store`；实验用 `ui.setToken` / `ui.injectCSS` / `ui.storePatch` / `ui.navigate` / `ui.window` / `ui.reload` / `ui.click` / `ui.type` / `ui.key` / `ui.hover` / `ui.scroll`（需要 `ui.write`，**只改内存态、不写设置、刷新即还原**）。资源：`ui://layout`、`ui://view`、`ui://console`；`ui.layout` 会给出可见元素之间的**非预期重叠**，`ui.diff` 走**像素 + 指纹双口径**（16×16 分块 + 差异区域 + diff 图 + 指纹 added/removed/changed），快照落盘在日志目录的 `ui-snapshots/`。
- **应用控制面（25 个工具）**：`servers.list/get/create/update/delete/duplicate/moveGroup/export/import`、`credentials.list/create/update/delete/attach`、`tunnels.list/create/update/delete/start/stop`、`settings.schema` / `settings.reset`、`app.commands` / `app.command`、`app.quit`。读类默认脱敏（`includeSecrets=true` 需要 `secrets.read`，返回里用 `plaintextFields` 标出明文），写类需要 `config.write` / `secrets.write` / `lifecycle` 且基本都要两段式确认；`app.command` 的权限**按命令实际影响**判定（只读 / 切换视图类命令不需要能力位与 token，未登记的命令按最保守策略处理）。
- **SFTP 与终端自动化（12 个工具）**：`sftp.list/stat/read/transfers/write/mkdir/rename/remove/syncCwd/cancel` 与 `terminal.run` / `terminal.expect`。远端写入要求路径是绝对路径、不含 `..`，且命中**白名单前缀**（路径段边界匹配；留空 = 一律拒绝），每个写操作都要两段式确认并提示「远端改动不可撤销」；`terminal.run` 支持 `expect` 正则、`wrapExitMarker` 退出码捕获、`idleMs` 静默窗口，`terminal.expect` 纯等待并用 `sinceMarker` 去重；两者共用「同会话串行化」（第二个并发调用直接拒绝，不排队）。
- **工具目录 `GET /v1/tools`**：一次返回全部工具的 `{name, capability[], confirmAction?, needsToken, description, inputSchema, hasStructuredOutput, outputSchema?}`，与 `tools/list` 同源且沿用同一个 Bearer 鉴权；`app.permissions` 同时新增 `toolCount`（当前工具总数，不写死）。
- **MCP 结构化输出（2025-06-18）**：13 个工具（`app_state`、`app.health`、`app.permissions`、`list_terminals`、`read_terminal`、`ui.layout`、`ui.diff`、`ui.query`、`sftp.list`、`servers.list`、`servers.get`、`settings.schema`、`app.recent_calls`）声明 `outputSchema`，并在 `tools/call` 里除文本内容外返回 `structuredContent`；`content[0].text` 保持原有格式，老客户端忽略新字段即可。

### 修复

- **切换能力位不再重启调试服务** —— 现象是保存设置 / 勾选能力位后正在连的 MCP / HTTP 客户端掉线，且端口为自动（`port=0`）时 `url` 直接变成另一个随机端口；原因是 `reloadDebug` 只要**任何**调试设置字段变化就 `stop + start` 调试服务；修法是只在**影响监听地址**的三个字段（`enabled` / `port` / `bindLan`）变化时重启，其余（能力位、`allowEval`、`allowSecrets`、CDP、日志相关）只更新内存兜底快照 —— 能力位本来就地生效（Gate 每次调用实时读设置）。`SaveSettings` 与 `SetCapability` 两条路径都汇入这一个函数，因此一处修复覆盖两处；测试断言切换能力位后底层 `debugsrv.Server` 实例、监听地址与 token 都不变。
- **`terminal.run` 的 `expect` 不再被「命令回显」假命中** —— 现象是命令里含 `echo ===DONE===`、`expect="===DONE==="` 时 1ms 就返回 `matched=true`，输出里只有被回显的命令行；原因是 PTY 先回显命令行，而匹配直接在累积文本上做；修法是匹配前先定位**本次发送命令的回显**（`terminalEchoEnd`：只取命令第一行，找到它之后回显行的结束位置并跳过），只在回显之后的文本上匹配，返回里新增 `echoSkippedBytes` 如实说明跳过了多少字节；终端不回声或无法定位回显时不做跳过，行为与之前一致。描述里补充推荐写法：让标记在远端运行时展开（`D=__MCP_$(echo DONE)__; …; echo $D` + `expect="__MCP_DONE__"`）。
- **`terminal.run` 未给 `expect` 时不再把回显当「真实输出」** —— 现象是「先回显、后静默、最后才吐输出」的命令（如 `docker ps`）在 ~400ms 就被判定跑完，只拿到回显；原因是首个输出块（其实是回显）把静默窗口从 1500ms 宽限重置成 400ms；修法是只有出现**回显之外的真实输出**才把窗口收窄到 `idleMs`，并要求真实输出前保持更宽的宽限；同时新增 `idleMs` 参数（默认 400，范围 50–60000）并在返回里回显。
- **`ui.diff` 的 `styles` 指纹细化到 CSS 属性级** —— 现象是 `changed[].fields[]` 里 `styles` 整份 `from`/`to` 丢回来，AI 还得自己做一遍 diff；修法是只报**真正变化**的属性（`{name:"styles", styles:{属性:{from,to}}}`，没有属性变化就不出现该条目），其余字段（`rect` / `classes` / `zIndex` / `visible` / `text`）形状不变。
- **`sftp.read` 对 `/etc/hostname` 报错的排查结论：不是缺陷** —— 完整错误是 `打开远端文件 "/etc/hostname" 失败：远端路径不存在（file does not exist）`（`isError=true`）；同会话 `sftp.list /etc` 的 147 条里确实没有 `hostname`（该 Arch 主机没生成这个文件），`/etc/os-release`（符号链接）、`/etc/passwd`、无换行结尾的文件、0 字节文件读取均正常，目录路径会返回「是目录，请用 sftp.list」。调用方需要注意：错误是 `isError=true` + 文本，不要当 JSON 解析。
- **`sftp.write` 写不出 0 字节文件** —— 现象是 `sftp.write{path:"/tmp/x", content:""}` 返回 `缺少写入内容（content / base64）`，而 `sftpWriteContent` 的中文提示里明确写了「写空文件请显式传 content: ""」；原因是宿主按 base64 的**值**是否为空判断有没有内容，而 debugsrv 侧总是把内容编码成 base64 传下来（空内容编码后正好是空串），于是「空内容」与「没给内容」被混为一谈；修法是改成按**键是否存在**判定（base64 优先、其次 content，空字符串是合法内容），并补了参数解释规则的回归用例（`sftp_write_test.go`）。

### 变更

- **命令面板与 `app.commands` 同源**：命令定义抽到 `frontend/src/commands.ts`（`buildCommands(ctx)` / 类型 / 影响范围与能力判定），`App.vue` 的命令面板与 `debug/bridge.ts` 都从这里取，不再各维护一份清单（id 与行为不变）；`app.command` 的执行走同一份 `run` 闭包，不会再出现「面板里有、AI 报未知命令」。
- `initialize.instructions` 补充工具目录、结构化输出与回显相关说明。
- 测试与文档中的工具数量改为**注册表驱动**（`AllRegisteredToolNames()` / 工具条目表），不再写死数字；跨域的测试助手（`ensureAllRegistrations` / `toolsListFromMCP` / `toolNamesOf` / `assertRegistryToolsListed`）归位到 `internal/debugsrv/registry_test.go`。

### 文档

- `README.md`：新增「能力位与两段式确认」「审计与『AI 记录』页签」「UI 观测与实验」「SFTP 与终端自动化」四节，把「MCP（Streamable HTTP）」一节改成**按域分组的工具总览**（内置 26 / `ui.*` 25 / 应用控制 25 / SFTP 与终端 12，共 88）并补上 `GET /v1/tools` 与 `structuredContent` 的说明；HTTP 接口表补 `/v1/tools` 与 `/v1/ui/*`，资源表补 `ui://*`、`sftp://{id}/cwd`、`transfers://current`；「已知问题 → 其他限制」补上 Hub 通道满会丢事件、`ui.reload` 会重挂前端、非 dev 构建需先构建前端否则 `ui.*` 报未知操作、`wrapExitMarker` 的 POSIX 语义、`sftp.write` 不接受空内容。

---

## [未发布] 2026-10-01

### 新增

- **调试模式**：应用内置一个**仅监听本机**（可切换局域网）的 HTTP 控制面 `/v1/*`，供 AI / 自动化调用。
  - 鉴权：每次启动生成随机 token，请求需带 `Authorization: Bearer <token>`（也支持 `?token=`）；
    端口 / token / CDP 端口写在 `%APPDATA%\ding-ssh\debug.json`，便于脚本自动发现。
  - 危险能力各自独立开关：**允许执行 JS**（`/v1/eval`）、**允许读取敏感数据**（服务器列表默认脱敏，不含密码/私钥）。
  - 能力覆盖：状态与设置读写（`/v1/state`、`/v1/settings`）、服务器列表、终端诊断
    （`rows/cols`、缓冲区行数、`baseY/viewportY`、回滚是否可达、可见文本与 DOM 几何）、
    终端读写 / 滚动 / `resize`、会话重连与断开、标签开关、窗口截图（`PrintWindow`）。
  - 实时事件流 `/v1/stream`（SSE，按 topic 订阅）与 `/v1/events`（环形缓冲，免订阅取最近 N 条）。
  - **应用内 MCP**（Streamable HTTP，`/mcp`）：21 个工具 + `app://`、`terminal://` 资源，
    并支持**服务端推送**（`initialize` 返回 `Mcp-Session-Id`，`GET /mcp` 为 SSE 通知通道，
    `DELETE /mcp` 结束会话）。
  - **MCP / 调试 API 的日志查看能力**：新增 5 个工具（`list_logs`、`read_logs`、`export_diagnostics`、
    `clear_logs`、`set_log_options`）、2 个资源（`app://logs`、`app://logs/tail`）与 5 条 `/v1/logs*`
    接口（列表 / 尾部读取 / 清空 / 导出 / 设置），支持按级别与关键字过滤、导出诊断包 zip；
    日志中的 token / 密码 / 私钥在写入前**自动脱敏**，可放心交给 AI 分析。
  - 对应设置页签 **「调试模式」**。
- **MCP stdio 代理 `cmd/ding-ssh-mcp`**：把上面的 HTTP MCP 桥接给只支持 stdio 传输的 MCP 客户端
  （stdin 逐行 JSON-RPC → `POST /mcp`；响应与 SSE 推送写回 stdout，stdout 保持纯 JSON-RPC，日志走 stderr 且 token 打码）。
  地址与 token 按 `-url/-token` → `DING_SSH_DEBUG_URL`/`DING_SSH_DEBUG_TOKEN` → `debug.json` 解析；
  拿到会话后自动挂 `GET /mcp` 接收推送，断线按 1s/2s/5s 退避重连，退出时关闭 SSE 并尽力 `DELETE /mcp`。
- **日志**：应用日志可落盘到 `%APPDATA%\ding-ssh\logs\app-YYYYMMDD.log`，
  单文件 8MB 或跨天轮转为 `app-YYYYMMDD-HHMMSS.log`，目录内只保留最近 20 个（超出删最旧）。
  - 控制台输出与文件输出**两个开关互相独立**，可分别开关；级别可选 `debug/info/warn/error`。
  - **MCP / 调试 API 调用日志**：独立开关，记录调用方法、工具名、状态码与耗时。
  - **日志管理页签**：打开日志文件夹、清理日志（先关句柄再删，清理后继续写入）、
    导出诊断包（`diagnostics-*.zip`，含最近 3 个日志 + `debug.json` + `state.json` + `env.json`）、最近日志预览。
  - **敏感信息自动脱敏**：token / 密码 / 私钥（含 `Authorization: Bearer`、JSON 敏感字段、整段 PEM）在写入前替换为 `***`，固定开启。
  - **按会话跟踪**：终端右键菜单「开始 / 停止跟踪此会话日志」把该会话输出以 `[trace:<id>]` 旁路落盘，便于复现单次连接。
  - 对应设置页签 **「日志」**。

### 修复

- **终端粘贴：Windows 下 `Ctrl+V` 不生效** —— 现象是按下 `Ctrl+V` 后远端报 `-bash: $'\026': command not found`；
  原因是 Wails v2 在 Windows 上固定关闭 WebView2 加速键（`PutAreBrowserAcceleratorKeysEnabled(false)`），
  原生 `paste` 事件不触发，xterm.js 又把 `\x16` 当控制字符发给远端；
  修法是在 `TerminalView.vue` 捕获阶段自行处理剪贴板快捷键（`Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` / `Shift+Insert`、
  有选中时 `Ctrl+Shift+C` / `Cmd+C` 复制，`Ctrl+C` 仍保留 SIGINT），统一走 `term.paste()`，
  并新增「鼠标右键行为」设置（打开选项栏 / 直接粘贴）。
- **登录横幅顶部丢行** —— 现象是连接成功后横幅（`/etc/motd`、fastfetch 等）开头少几行；
  原因是输出监听注册晚于连接建立，首段输出在订阅前就丢了；修法是**先注册输出监听再发起连接**。
- **重连导致横幅开头消失** —— 现象是重连后清屏把刚输出的横幅擦掉；
  原因是并发/隐式重连路径重复执行了 `term.reset()`；修法是加连接重入保护，且隐式重连保留缓冲区（只追加分隔行），
  仅在用户显式重连 / 新会话时才清屏。
- **终端回滚不可达（顶部行看不到且滚不动）** —— 现象是缓冲区明明有历史却无法上滚；
  原因是 `.xterm-viewport` 与内容区没有对齐，滚动区域超出可视范围；修法是给视口上下内缩对齐内容区
  （分屏下用小一些的内缩）。
- **WebGL 与界面缩放错位** —— 现象是 `uiScale ≠ 100%` 时终端顶部缺行且无法上滚，
  但缓冲区与 DOM 几何都正确；原因是 `@xterm/addon-webgl` 在存在 CSS `zoom` 的祖先下会把画布内容整体上移数行，
  `clearTextureAtlas()+refresh()` 也无法纠正；修法是**仅在 `uiScale = 100` 时启用 WebGL，其余自动回退 Canvas**
  （设置里的 WebGL 开关保持可用，切换缩放时自动重新评估）。
- **快速连接面板关闭卡顿** —— 现象是点选服务器后面板「僵一下」再滑走；
  原因是关闭动画与终端创建挤在同一帧；修法是**先收面板、下一帧再创建终端**，
  并把面板改为从左侧导航右边界出入。

### 变更

- 左侧导航的**区段顺序可配置**（导航 / 会话 / 标签页 六种排列），标签页可放在左侧导航或顶部。
- 设置页新增 **「调试模式」** 与 **「日志」** 两个页签。

### 文档

- `README.md` 补充调试模式的 MCP 服务端推送（SSE）与 stdio 代理接入方式，新增「日志（Logs）」一节，
  并同步 MCP 工具清单（21 个，含 5 个日志工具）、`app://logs` 资源与 `/v1/logs*` 接口表，
  以及更新「已知问题」中的 WebGL 与界面缩放说明。

---

## 更早的特性与修复

以下条目按**开发阶段倒序**列出（最新在最前），内容与 `README.md` 的「当前进度」一致。

### 界面与终端交互（2026-09）

- **终端分屏**：左右 / 上下分屏，可拖拽分隔条调整比例，也支持把标签拖到终端区域按落点方向分屏；
  分屏组在标签栏合并为一个条目，可展开子菜单、拖到空白处取消分屏。
- **设计系统重写为 Grid Desk**：统一色板 / 间距 / 层级，左侧导航作为主入口，标签页位置可配置。
- **快速连接入口收敛**：新增快速连接侧边栏，箭头贴边、空闲态降低存在感。
- **「关于」页**：展示版本号、提交号与构建时间（编译期注入）。
- **会话断开会话内容保留可回看**：断开后保留终端画面，顶部横幅提示，可滚屏 / 复制。
- **凭证编辑与查看复制**；标签支持复制（复制当前标签终端）。
- **连接分步日志**：连接过程的实时状态与详细日志，失败时展开面板。
- **本机终端**：支持按平台选择 Shell（PowerShell / cmd / bash 等）。

### Phase 1–5（2026-08 ～ 2026-09）

- **Phase 1**：Wails 前后端分离骨架；服务器节点管理（新增 / 编辑 / 删除 / 搜索，密码与私钥认证）；
  多标签 SSH 终端（PTY 尺寸自适应、输入输出流、连接状态事件与失败重连）；设置页；服务器分组管理；
  右侧 SFTP 面板（浏览 / 导航 / 上传下载 / 进度与取消）；SSH 隧道页（本地端口转发）；SQLite 存储（WAL + 旧版 JSON 自动迁移）。
- **Phase 2**：Shell ↔ SFTP 双向目录同步（OSC 7 解析 + Prompt 备选）；SWR 目录缓存引擎
  （`sync.Map` 内存缓存 + 异步 Revalidate + 增量推送）；并发传输 Worker Pool 与令牌桶限速；SSH 保活与自动重连。
- **Phase 3**：rz/sz（Zmodem）全自动接管；Trie + FZF 智能命令提示；GPU 硬件加速渲染（xterm WebGL，自动降级）；
  命令历史存储与查询。
- **Phase 4**：SysInfo 静默系统分析看板与底部服务器状态栏；SSH 隧道高级模式（Remote Forward / Dynamic SOCKS5）；
  数据库敏感字段加密（AES-256-GCM + OS Keyring）；`.dingpack` 配置导出与导入；命令历史清理与补全热键可配置。
- **Phase 5**：GitHub Actions 多平台构建矩阵与自动化测试（Go `internal` + 前端 typecheck）；
  推送 `v*` 标签自动创建 Release；Windows（NSIS/zip）、macOS（DMG/zip）、Linux（AppImage/deb/tar.gz）打包。
