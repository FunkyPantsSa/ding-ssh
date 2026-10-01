# ding-ssh

基于 **Wails (Go + Vue 3)** 的跨平台 SSH 客户端。技术设计见 [`docs/design.md`](docs/design.md)，产品需求见 [`docs/prd.md`](docs/prd.md)。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 桌面框架 | Wails v2 (Golang + Webview) |
| 前端 | Vue 3 + Pinia + TailwindCSS + Vite |
| 终端渲染 | @xterm/xterm + @xterm/addon-fit + @xterm/addon-webgl + @xterm/addon-zmodem |
| SSH 核心 | golang.org/x/crypto/ssh |
| SFTP 核心 | github.com/pkg/sftp |
| 速率限制 | golang.org/x/time/rate |
| 配置存储 | SQLite（`modernc.org/sqlite` 纯 Go 驱动，`os.UserConfigDir()/ding-ssh/ding-ssh.db`），旧版 JSON 数据首次启动自动迁移 |

## 当前进度

### Phase 1 ✅ (基础框架与 SSH)
- [x] Wails 项目骨架（前后端分离架构）
- [x] 服务器节点管理（新增 / 编辑 / 删除 / 搜索，密码与私钥认证）
- [x] 多标签 SSH 终端（xterm.js 渲染、PTY 尺寸自适应、输入输出流）
- [x] 连接状态事件与失败重连
- [x] 设置页面（日志开关、选中即复制、鼠标右键行为、左侧导航区段顺序、调试模式、终端主题、保存的凭证）
- [x] 服务器分组管理与折叠
- [x] 右侧 SFTP 面板（目录浏览 / 导航 / 上传下载，底部进度条与传输取消）
- [x] SSH 隧道页（本地端口转发，支持停止 / 重启 / 删除）
- [x] SQLite 存储（WAL 模式，旧版 JSON 自动迁移）
- [x] SSH 连接过程进度实时展示（10s 超时提示）
- [x] Phase 2（部分）: SFTP 文件管理（右键菜单、重命名 / 删除 / 新建文件夹、路径编辑、多选上传）

### Phase 2 ✅ (SFTP 深度联动与高并发引擎)
- [x] Shell <-> SFTP 双向目录同步（OSC 7 解析 + Prompt 备选）
- [x] SWR 目录缓存引擎（sync.Map 内存缓存 + 异步 Revalidate + 增量 Push）
- [x] 并发传输 Worker Pool + 令牌桶限速（golang.org/x/time/rate）
- [x] SSH 链路保活与自动重连（KeepAlive Ticker 15s + 一键重新连接）

### Phase 3 ✅ (终端极客特性与智能补全)
- [x] rz/sz (Zmodem) 协议全自动接管
- [x] Trie + FZF 智能命令提示面板
- [x] GPU 硬件加速渲染（xterm-addon-webgl，自动降级）
- [x] 命令历史记录存储与查询

### Phase 4 ✅ (运维 Dashboard 与配置迁移)
- [x] 静默系统分析看板（SysInfo Dashboard）
- [x] 底部服务器状态栏（CPU / 内存 / 磁盘 / 网卡，可选）
- [x] SSH 隧道高级模式（Remote Forward / Dynamic Forward SOCKS5）
- [x] 数据库敏感字段加密（AES-256-GCM + OS Keyring）
- [x] 配置导出与迁移（.dingpack 加密打包/导入）
- [x] 命令历史清理 / 补全导航热键可配置

### Phase 5 ✅ (CI/CD 与持续交付)
- [x] GitHub Actions 多平台自动化构建矩阵
- [x] 自动化测试（Go `internal` + 前端 typecheck）
- [x] 推送 `v*` 标签自动构建并上传 GitHub Release
- [x] 多平台打包（Windows NSIS/zip，macOS DMG/zip，Linux AppImage/deb/tar.gz）

## 目录结构

```
ding-ssh/
├── main.go                    # 应用入口与 Wails 配置
├── app.go                     # Wails 绑定 API（服务器管理 / SSH 会话）
├── internal/
│   ├── models/                # 前后端共享数据结构
│   ├── store/                 # 持久化存储（SQLite + dingpack / 加密迁移）
│   ├── cryptox/               # AES-256-GCM + Argon2id + Keyring
│   ├── sshx/                  # SSH 会话 / SFTP / 隧道 / SysInfo
│   ├── logx/                  # 受日志开关控制的应用日志
│   └── logfilter/             # Wails 日志过滤（噪音 + 开关）
├── scripts/                   # 版本写入与各平台二次打包
├── .github/workflows/         # CI 测试 + Release 多平台构建
└── frontend/
    ├── src/
    │   ├── components/        # ServerList / TerminalView / TunnelPage / SftpPanel / SysInfoPanel / SettingsPage
    │   ├── completion/        # Trie + FZF 智能补全
    │   ├── stores/            # Pinia stores
    │   ├── services/          # ssh / settings / history / zmodem / security / sysinfo
    │   └── types.ts
    └── wailsjs/               # Wails 自动生成绑定（勿手改）
```

## 开发运行

前置要求：Go 1.25+、Node.js 18+、Wails CLI v2.13。

```bash
# 安装 Wails CLI（首次）
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 启动开发模式（热更新前端，实时编译 Go）
wails dev

# 生产构建
wails build
```

产物位于 `build/bin/`。

### Windows 本机环境（已配置）

Go / Node / Wails 都已装好，但本机 PowerShell 策略禁止直接运行 `.ps1`，
且「安装之后才启动」的终端需要重新拼一次 PATH。为此提供两个入口：

```powershell
# 起一个已激活环境的交互式终端，然后正常用 wails
.\dev.cmd

# 或者直接带命令执行
.\dev.cmd wails dev
.\dev.cmd wails build
.\dev.cmd go test ./internal/...
```

`dev-env.ps1` 负责拼 PATH 并设置 Go 模块代理，`dev.cmd` 只是用
`-ExecutionPolicy Bypass` 把它包起来（本机策略禁止运行 `.ps1`）。

本机环境要点：

| 组件 | 位置 / 说明 |
| --- | --- |
| Go | `C:\Program Files\Go`（全局安装，已在 PATH） |
| Wails CLI | `%USERPROFILE%\go\bin\wails.exe`（v2.13.0） |
| Node | `C:\Program Files\nodejs`（v24.19.0 LTS，winget 安装，已在 PATH） |
| npm | 随 Node 安装（v11.17.0） |
| Go 模块代理 | `GOPROXY=https://goproxy.cn,direct`（直连 proxy.golang.org 会超时） |

Go 和 Node 现在都已全局装好，**新开的终端里直接用 `wails` / `go` / `npm` 即可**，
不需要 `dev.cmd`。`dev.cmd` / `dev-env.ps1` 只对「安装之前就已启动的旧终端」有用，
它会为当前会话补齐 PATH（安装器改的是系统 PATH，已在运行的进程不会自动更新）。

## 发布（GitHub Release）

推送语义化版本标签后，Actions 会在 macOS / Windows / Ubuntu 上并行构建，并把安装包挂到该 tag 的 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

| 产物 | 说明 |
| --- | --- |
| `ding-ssh-*-macos-universal.dmg` / `.app.zip` | macOS Universal（未签名，首次请右键打开） |
| `ding-ssh-*-windows-amd64-setup.exe` / `.zip` | Windows x64（NSIS 安装包内嵌 WebView2 bootstrap） |
| `ding-ssh-*-linux-amd64.AppImage` / `.deb` / `.tar.gz` | Linux x64，需 `libgtk-3-0` 与 `libwebkit2gtk-4.1-0` |
| `SHA256SUMS.txt` | 校验和 |

也可在 Actions 里手动运行 **Release** 工作流，仅上传 artifacts、不创建 Release。

## 调试模式（Debug Mode，供 AI / 自动化调用）

在 **设置 → 调试模式** 开启。这是一个**仅监听本机**（可切换局域网）的控制面，让 AI 或脚本可以读取应用/终端状态、
驱动终端、订阅输出，也可以直接以 **MCP** 接入任意 AI 客户端。默认关闭，token 每次启动重新生成。

- 端口 / token / CDP 端口写在 `%APPDATA%\ding-ssh\debug.json`（Windows）或 `~/.config/ding-ssh/debug.json`
- 危险能力各自独立开关：**允许执行 JS**（`/v1/eval`）、**允许读取敏感数据**（含密码/私钥的字段）
- 建议只用 `127.0.0.1`；开局域网时务必配合 token
- MCP 支持**服务端推送**（`GET /mcp` 的 SSE 通道），只支持 stdio 的客户端可用 `cmd/ding-ssh-mcp` 代理接入

### HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/health` | 运行状态、端口、开关 |
| GET | `/v1/state` | 视图 / 标签 / 会话 / 隧道 / 设置快照 |
| GET | `/v1/settings` · PUT `/v1/settings` | 读设置 / 部分更新（保存后立即生效） |
| GET | `/v1/servers` | 服务器列表（默认脱敏） |
| GET | `/v1/terminals` | 终端诊断：`rows/cols`、`bufferLength`、`baseY/viewportY`、`hiddenAbove`、`atTop/atBottom`、`scrollable`、`visibleText`、DOM 几何 |
| GET | `/v1/terminals/{id}/buffer?from=tail&lines=200` | 读缓冲区文本（含回滚） |
| POST | `/v1/terminals/{id}/input` | `{data}` 或 `{base64}` 写入会话 |
| POST | `/v1/terminals/{id}/scroll` | `{to: top\|bottom\|line\|lines, value}` |
| POST | `/v1/terminals/{id}/resize` | `{cols, rows}`（只改模拟器尺寸，用于复现渲染问题） |
| POST | `/v1/sessions/{id}/disconnect` · `/reconnect` | 断开 / 重连 |
| POST | `/v1/tabs` · DELETE `/v1/tabs/{id}` | 打开（`{serverId}` 或 `{local:true}`）/ 关闭标签 |
| POST | `/v1/eval` | 在页面上下文执行 JS（需开关） |
| GET | `/v1/screenshot` | 窗口 PNG 截图（需窗口可见） |
| GET | `/v1/events?limit=&topics=` | 最近的后端事件（环形缓冲） |
| GET | `/v1/stream?topics=ssh.output,ssh.status` | SSE 实时事件流 |
| GET | `/v1/logs` | 日志目录、开关、级别、总大小、文件列表、保留策略 |
| GET | `/v1/logs/tail?lines=&level=&keyword=&file=` | 读取日志尾部（可按级别/关键字过滤；`file` 必须是日志目录内的文件名） |
| POST | `/v1/logs/clear` · `/v1/logs/export` | 清空日志 / 导出诊断包 zip（返回路径） |
| POST | `/v1/logs/options` | `{consoleEnabled?,fileEnabled?,level?,apiLogEnabled?}` 调整日志设置 |

鉴权：`Authorization: Bearer <token>`（也支持 `?token=`）。辅助脚本：
`.\scripts\debug.ps1 terminals`、`.\scripts\debug.ps1 buffer <id>`、`.\scripts\debug.ps1 screenshot` 等。

### MCP 接入（Streamable HTTP）

端点即 `POST http://127.0.0.1:<port>/mcp`，共 **21 个工具**，覆盖上表能力：`app_state`、`list_terminals`、
`read_terminal`、`send_input`、`scroll_terminal`、`resize_terminal`、`open_tab`、`close_tab`、
`reconnect_session`、`disconnect_session`、`list_servers`、`get_settings`、`update_settings`、`eval_js`、
`screenshot`、`recent_events`、`list_logs`、`read_logs`、`export_diagnostics`、`clear_logs`、`set_log_options`。

其中 5 个日志工具用于排查问题：`list_logs` 先看日志在哪、有多少（目录、开关、级别、总大小、文件列表与保留策略），
`read_logs` 拉取日志文本并可按级别（`level=warn` 同时含 WARN 与 ERROR）或关键字（大小写不敏感）过滤，
`set_log_options` 可临时把级别调成 `debug`（debug 才记录终端跟踪等细粒度信息）、打开文件/控制台日志
或 MCP / 调试 API 调用日志，`export_diagnostics` 导出诊断包 zip（返回路径，便于让用户直接发给你），
`clear_logs` 清空日志。日志中的 **token / 密码 / 私钥在写入前已自动打码**，可放心交给 AI 分析。

资源含 `app://state`、`app://debug/info`、`app://servers`、`app://logs`（`application/json`，日志文件与日志设置）、
`app://logs/tail`（`text/plain`，最新日志末 200 行）、`terminal://{id}/buffer`。

```json
{
  "mcpServers": {
    "ding-ssh": {
      "type": "http",
      "url": "http://127.0.0.1:8765/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

> 客户端若不支持 Streamable HTTP，请改用下一节的 stdio 代理。
> WebView2 的远程调试（CDP）在 Wails v2.13 下无法注入（框架会覆盖 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`，
> 且 `windows.Options` 未暴露该字段），因此页面级能力由 `/v1/eval` 承担，截图由原生窗口捕获提供。

### MCP 服务端推送（SSE）

除「请求 / 响应」外，MCP 端点还提供**服务端推送**：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/mcp` | JSON-RPC 请求；`initialize` 成功时在**响应头**返回 `Mcp-Session-Id` |
| GET | `/mcp` | 推送通道（SSE），必须带 `Mcp-Session-Id`；推送 `notifications/message` |
| DELETE | `/mcp` | 结束会话（带 `Mcp-Session-Id`），随后推送通道失效 |

- 推送内容形如 `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","logger":"ding-ssh/<topic>","data":{...}}}`，
  其中 `logger` 是 `ding-ssh/` + 事件 topic（如 `ssh.output`、`ssh.status`、`tunnel`）；
- `data.uri` 会指向该事件对应的资源，便于客户端失效缓存：终端输出 → `terminal://{id}/buffer`，隧道等应用级事件 → `app://state`；
- 服务端每 20s 发一行 `: ping` 注释保活（SSE 注释行，可忽略）；
- 会话由 `initialize` 创建，`DELETE` 结束；应用重启后旧会话即失效，需重新 `initialize`。

```bash
# 1) initialize：用 -D 打印响应头，取回 Mcp-Session-Id
curl -s -D - -X POST http://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer <token>" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'

# 2) 挂上推送通道（-N 关闭缓冲，Ctrl+C 退出）
curl -N -H "Authorization: Bearer <token>" -H "Mcp-Session-Id: <id>" http://127.0.0.1:<port>/mcp

# 3) 结束会话
curl -X DELETE -H "Authorization: Bearer <token>" -H "Mcp-Session-Id: <id>" http://127.0.0.1:<port>/mcp
```

### stdio 客户端接入（`cmd/ding-ssh-mcp`）

只支持 **stdio** 传输的 MCP 客户端可用内置代理 `cmd/ding-ssh-mcp`：它把 stdin 的 JSON-RPC
转发到 `POST <url>/mcp`，把响应与 SSE 推送写回 stdout，对客户端来说就是一个普通的 stdio MCP 服务器。

```bash
# 自行编译（产物在仓库根目录；也可放到 PATH 里）
go build -o ding-ssh-mcp.exe ./cmd/ding-ssh-mcp
```

```json
{
  "mcpServers": {
    "ding-ssh": {
      "command": "E:\\Project\\ding-ssh\\ding-ssh-mcp.exe",
      "args": ["-v"]
    }
  }
}
```

地址与令牌的解析优先级：`-url`/`-token` → 环境变量 `DING_SSH_DEBUG_URL`/`DING_SSH_DEBUG_TOKEN`
→ `%APPDATA%\ding-ssh\debug.json`（应用开启调试模式后写入，**token 每次启动都会变化**）。

| 参数 / 环境变量 | 说明 |
| --- | --- |
| `-url` · `DING_SSH_DEBUG_URL` | 调试服务地址，如 `http://127.0.0.1:8765`（也接受 `/mcp` 结尾，会自动去掉） |
| `-token` · `DING_SSH_DEBUG_TOKEN` | 调试服务 token |
| `-debug-json` | `debug.json` 路径，默认 `%APPDATA%\ding-ssh\debug.json` |
| `-v` / `-quiet` | 打印转发细节 / 只输出错误 |
| `-once` | 仅探测地址与 token 是否可用，然后退出（排查配置时用） |
| `-h` | 中文用法说明 |

- **stdout 是纯 JSON-RPC**（响应体原样写回、SSE 推送逐行写回），日志一律走 **stderr**，且 token/会话 id 会打码；
- 拿到 `Mcp-Session-Id` 后自动挂 `GET /mcp` 接收 `notifications/message`，断线按 1s/2s/5s 退避重连；
- 进程退出前会关闭 SSE 连接并尽力 `DELETE /mcp` 结束会话；
- 应用重启后端口/token 变化时，代理会重读 `debug.json` 自动跟上（用 `-url`/`-token` 显式指定时不覆盖）。

开发期也可以不编译，直接让客户端跑源码（工作目录需为仓库根）：

```json
{ "mcpServers": { "ding-ssh": { "command": "go", "args": ["run", "./cmd/ding-ssh-mcp"] } } }
```

## 日志（Logs）

日志目录 `%APPDATA%\ding-ssh\logs`（Linux/macOS 为 `~/.config/ding-ssh/logs`），
当前文件 `app-YYYYMMDD.log`（本地日期），单文件超过 **8MB** 或跨天时轮转为
`app-YYYYMMDD-HHMMSS.log`，目录内最多保留 **20** 个文件（超出删最旧）。

**设置 → 日志** 提供：

| 能力 | 说明 |
| --- | --- |
| 控制台输出 / 文件输出 | 两个开关互相独立：关掉控制台不影响落盘，反之亦然 |
| 日志级别 | `debug` / `info` / `warn` / `error`，只输出不低于该级别的日志 |
| MCP / 调试 API 调用日志 | 单独开关（默认关闭），记录每次 HTTP / MCP 调用的方法、工具名、状态码与耗时，核对 AI / 自动化到底做了什么 |
| 打开日志文件夹 / 清理日志 | 清理只删 `app-*.log`（正在写入的文件先关闭再重建），清理后日志继续正常记录 |
| 最近日志预览 | 只读当前文件尾部（默认 200 行），打开页签或点刷新时读一次，不做轮询 |
| 导出诊断包 | 在日志目录生成 `diagnostics-YYYYMMDD-HHMMSS.zip`：最近 3 个日志文件 + `debug.json` + `state.json`（会话/隧道/设置/终端诊断）+ `env.json`（系统、架构、Go 版本、应用版本、日志级别） |

安全：**token、密码、私钥等敏感信息在写入日志前会自动打码**（`token=***`、`"password":"***"`、
`Authorization: Bearer ***`、整段 PEM 私钥），固定开启、无法关闭；诊断包同样只含脱敏后的内容。

终端右键菜单还有 **「开始 / 停止跟踪此会话日志」**：开启后该会话的**输出**会以 `[trace:<id>]` 旁路一份到日志
（去掉 ANSI 转义，单条超过 2000 字符截断），用于复现「某次连接到底发生了什么」；不需要时记得关掉，
被跟踪的会话列表在「日志」页签里也能看到。

## 已知问题

### WebGL 与界面缩放（`uiScale ≠ 100` 时自动回退 Canvas）

`@xterm/addon-webgl` 在存在 CSS `zoom` 的祖先（本项目的「界面缩放」，`uiScale ≠ 100`）时，
会把画布内容整体上移数行：缓冲区与 DOM 几何都正确，但**画出来的像素错位**，表现为终端顶部
前几行看不到、且因为缓冲区已在顶部而**无法向上滚动**；缩放回 100% 或在别的尺寸下又可能刚好正常。
实测（调试模式 A/B/C 对照）：`zoom=1` 正常、`zoom=0.9` 错位、`clearTextureAtlas()+refresh()` 无法纠正。

因此现在**仅在 `uiScale = 100` 时启用 WebGL**，其余情况自动回退 Canvas 渲染；
设置里的「WebGL 硬件加速」开关保持可用（切换界面缩放时会自动重新评估）。

### Wails dev 模式日志噪音（`runtime:ready`）

在 `wails dev` 下，若用浏览器打开 dev 地址（终端提示的 `http://localhost:34115`），
后端会打印两条 `ERR | ... Unknown message from front end: runtime:ready`。

这是 Wails v2.13.0 的已知框架问题：浏览器通过 WebSocket IPC 加载页面时，注入的
runtime bundle 会发送内部消息 `runtime:ready`，而 devserver 未像桌面窗口那样拦截它。
该消息不影响任何功能，生产构建与桌面窗口均不会出现。

本项目通过 `internal/logfilter` 自定义 Logger 精确过滤这两条噪音（仅匹配
`Unknown message from front end: runtime:ready`，其余日志原样输出）。
日志输出同时受设置页「日志开关」控制：开启时输出（仍过滤该噪音），关闭时全部静默。
待 Wails 上游修复后可删除该包。

### Windows 终端粘贴：Ctrl+V 不生效（Wails 关闭了 WebView2 加速键）

Wails v2 在 Windows 上固定调用 `PutAreBrowserAcceleratorKeysEnabled(false)`
（见 `wails/v2/internal/frontend/desktop/windows/frontend.go`），
WebView2 因此不再执行浏览器层面的编辑加速键：按下 Ctrl+V 既不会触发原生 `paste` 事件，
xterm.js 又会把它按控制字符处理，把 `\x16`（`^V`）原样发给远端，表现为
`-bash: $'\026': command not found`。

修复方式：终端不再依赖浏览器加速键，而是在 `TerminalView.vue` 的捕获阶段自行处理剪贴板快捷键：

| 快捷键 | 行为 |
| --- | --- |
| `Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` | 读取剪贴板（优先 Wails `ClipboardGetText`，回退 `navigator.clipboard`）并粘贴 |
| `Shift+Insert` | 同上 |
| `Ctrl+Shift+C` / `Cmd+C` | 有选中内容时复制（`Ctrl+C` 仍保留为 SIGINT） |

粘贴统一走 `term.paste()`，与 xterm 原生行为一致（`\r?\n → \r`，shell 开启 bracketed paste 时自动加括号），
因此 vim / tmux / zsh 能识别为一次粘贴而不是逐行执行。

设置页「通用 → 鼠标右键行为」可切换右键是「打开选项栏」还是「直接粘贴」；
两种模式下菜单与粘贴操作都会把键盘焦点交回终端，避免操作后无法继续输入。

## 后端 API 与事件

### 绑定方法（`app.go`）

**服务器管理：**
- `GetServers` / `SaveServer` / `DeleteServer` / `SelectKeyFile`

**SSH 会话：**
- `Connect` / `Disconnect` / `Write` / `Resize` / `ListSessions`

**SFTP：**
- `SftpList` / `SftpUpload` / `SftpDownload` / `SftpCancelTransfer` / `SftpRename` / `SftpMkdir` / `SftpRemove` / `SelectLocalFiles` / `SelectSavePath`

**SSH 隧道：**
- `StartTunnel(node, name, mode, localPort, remoteHost, remotePort)` — mode: `local` | `remote` | `dynamic`
- `StopTunnel` / `RestartTunnel` / `RemoveTunnel` / `ListTunnels`

**Phase 2+ 增量 API：**
- `SetSftpPathFromTerminal(sessionID, path)` — Shell → SFTP 目录同步
- `SyncSftpToTerminal(sessionID, path)` — SFTP → Shell 目录同步
- `StartSysInfoCollector(sessionID)` / `StopSysInfoCollector(sessionID)` / `SetSysInfoIdle` — 系统监控
- `ClearCommandHistory(serverID)` — 清理本地命令历史（空字符串=全部）
- `ExportConfig(passphrase)` / `ImportConfig(passphrase, overwrite)` — .dingpack 配置迁移

**安全：**
- `GetSecurityStatus` / `UnlockWithMasterPassword` / `EnableMasterPassword` / `DisableMasterPassword` / `ChangeMasterPassword`

### 事件

- `ssh:output:{sessionId}` — 终端输出，`data` 为 base64 编码字节流
- `ssh:status:{sessionId}` — 会话状态（connected / closed / error / disconnected）
- `tunnel:status` — SSH 隧道状态变更（running / stopped / error）
- `sftp:sync-path:{sessionId}` — SFTP 目录同步
- `sftp:dir-updated:{sessionId}` — SWR 缓存增量更新
- `sysinfo:snapshot:{sessionId}` — 系统信息快照
