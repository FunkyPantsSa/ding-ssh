# ding-ssh

[![CI](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml)

Windows / macOS / Linux 上的 SSH 工作台：多标签终端 + SFTP + 隧道 + 本机终端，并内置一套仅供本机访问的 AI / 自动化调试控制面（HTTP + SSE + MCP）。

## 项目简介

ding-ssh 是基于 **Wails v2（Go + Vue 3）** 的桌面 SSH 客户端，面向需要同时管理多台服务器、又想保留完整终端体验的开发者与运维人员。

与常见的 SSH 客户端相比，它把几件事做在同一个窗口里：

- **多标签终端**：xterm.js 渲染，支持分屏，标签页位置可放在顶栏或左侧导航；
- **SFTP 与终端目录联动**：一侧浏览/传输文件，另一侧 shell 的 `cd` 会同步过去，反之亦然；
- **隧道管理**：本地 / 远程 / 动态（SOCKS5）三种端口转发；
- **本机终端**：不开 SSH 也能在同一套标签体系里用 PowerShell / cmd / bash；
- **命令面板**：`Ctrl+K` 统一入口，跳页面、连服务器、开本地终端；
- **可配置 UI 布局**：左侧导航区段顺序、标签栏位置、界面缩放、主题与字体；
- **内置调试模式与 MCP**：开启后本机多一个 `/v1/*` 控制面与 `/mcp` 端点，AI 客户端可直接读状态、驱动终端、订阅输出，并有完整的日志与诊断包体系。

技术栈：Wails v2.13 桌面框架；Vue 3 + Pinia + TailwindCSS v4 + Vite 前端；`@xterm/xterm` + `addon-fit` + `addon-webgl` 终端渲染；`golang.org/x/crypto/ssh` 与 `github.com/pkg/sftp` 作为 SSH/SFTP 核心；SQLite（`modernc.org/sqlite` 纯 Go 驱动）保存配置。

## 发展情况

项目从最小可用做起：先后完成 Wails 前后端分离骨架、服务器节点管理与密码/私钥认证、多标签 PTY 终端、连接状态事件与失败重连，以及基于 SQLite（WAL 模式，首次启动自动导入旧版 JSON）的持久化层。随后补齐 SFTP 面板与 SSH 隧道页，并把两端联动起来——shell 的目录变化（OSC 7 解析，prompt 作为备选）会推给 SFTP，SFTP 的跳转也能回到终端；目录读取走 SWR 缓存（`sync.Map` 内存缓存 + 异步 revalidate + 增量推送），传输走并发 worker pool 并配合令牌桶限速，会话靠 15s 心跳维持、断开后自动重连。这一阶段还加进了 rz/sz（Zmodem）接管、Trie + FZF 智能命令补全与本地命令历史、基于 WebGL 的 GPU 渲染（不支持时自动降级），以及 SysInfo 看板与底部服务器状态栏。

功能面铺开后，重心转向界面与交互：设计系统重写为 **Grid Desk**（统一色板、间距、层级），左侧导航成为主入口；**标签页位置**可在顶栏与左侧导航之间切换，**导航区段顺序**支持导航/会话/标签页六种排列；快速连接收敛为贴边的侧边栏；终端分屏（左右/上下，可拖拽分隔条，也支持把标签拖到终端区域按落点方向分屏）落地。同期修掉了一批实际使用中暴露的问题：登录横幅顶部丢行（输出监听注册晚于连接建立）、重连清屏擦掉横幅、终端回滚不可达、以及 Windows 下 `Ctrl+V` 因 WebView2 加速键被关闭而失效。安全侧则加入 AES-256-GCM + OS Keyring 的字段加密与主密码、`.dingpack` 配置导出导入。

最近一轮聚焦「可观测性与可自动化」：新增**调试模式**——一个默认关闭、仅监听本机（可切局域网）的 HTTP 控制面，覆盖状态与设置读写、服务器列表、终端读写/滚动/resize、会话重连与断开、标签开关、窗口截图、`/v1/events` 环形缓冲与 `/v1/stream` SSE 实时流，并提供 **/mcp**（Streamable HTTP）供任意 MCP 客户端接入，含服务端推送与会话管理；同时新增 **stdio 代理 `cmd/ding-ssh-mcp`** 以服务只支持 stdio 传输的客户端。配套的**日志体系**一并落地：日志可落盘并按 8MB 或跨天轮转、保留 20 个文件，控制台与文件两个开关互相独立，支持 `debug/info/warn/error` 级别、MCP / 调试 API 调用日志、按会话跟踪终端输出、导出诊断包，以及写入前对 token / 密码 / 私钥的自动脱敏。

目前项目按 `dev` 分支持续开发，功能面已经覆盖终端、SFTP、隧道、本机终端、隧道与 AI 控制面；质量保障由 GitHub Actions 承担——每次 push 与 PR 跑 Go `internal` 测试与前端 typecheck，`main` / `dev` 分支的 push 额外产出三平台构建物，打 `v*` 标签则自动创建 Release。后续方向仍是补齐终端与运维细节（例如 known_hosts 主机密钥校验已在 SSH 会话中标记为待办），并让调试模式/MCP 的工具面继续覆盖更多应用内部能力。

## 主要特性

**终端**

- 多标签会话，支持左右/上下分屏与拖拽标签按落点方向分屏。
- xterm.js + WebGL 硬件加速；**界面缩放 ≠ 100% 时自动回退 Canvas 渲染**（原因见「已知问题」）。
- 5000 行回滚缓冲，可向上翻阅历史输出。
- 字号可调（`Ctrl+=` / `Ctrl+-` / `Ctrl+0`），字体与终端主题可换。

**粘贴与鼠标**

- `Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` / `Shift+Insert` 粘贴、`Ctrl+Shift+C` / `Cmd+C` 复制：由终端在捕获阶段自行拦截，绕开 Windows WebView2 加速键被关闭的限制；`Ctrl+C` 仍保留为 SIGINT。
- 鼠标右键行为可配置：打开选项栏（复制/粘贴/全选/清屏/全屏/跟踪日志）或直接粘贴。

**SFTP**

- 浏览远程目录、上传（含拖拽上传到当前目录）、下载、重命名、删除、新建文件夹、路径编辑、多选上传，底部进度条可取消传输。
- 与终端 `cwd` **双向同步**：shell 中 `cd` 会同步 SFTP 面板，SFTP 面板跳转也能同步回终端，两项可分别开关。

**连接与会话**

- 密码或私钥认证（私钥可粘贴内容或选文件），支持保存常用凭证。
- 服务器分组、搜索与在线状态测试（TCP 延迟 + SSH 端口连通性）。
- 快速连接面板，贴边收纳，点选即开会话。
- 五步连接过程进度（DNS/直连 → TCP 握手 → SSH 鉴权 → 分配 PTY → 会话就绪）与详细日志，失败时展开排查。
- 断线自动重连（2s/5s/10s/20s/30s 退避）与手动重连；**隐式重连保留缓冲区**，只追加一条分隔行，不清屏。
- 15s SSH 心跳保活，防止 NAT / 防火墙掐断空闲连接。

**隧道**

- 本地端口转发（local）、远程端口转发（remote）、动态转发（dynamic，SOCKS5）。
- 隧道可停止、重启、编辑、删除，状态变更通过事件回传界面。

**本机终端**

- 可不连 SSH 直接开本机 shell：Windows 用 PowerShell 或 cmd，macOS 用 zsh 或 bash，Linux 用默认 shell。

**界面**

- 多套预设主题 + 自定义色板；终端 ANSI 16 色、背景图与文字阴影可调。
- 界面缩放（80%–150%）。
- 标签栏位置（顶栏 / 左侧导航）与左侧导航区段顺序（导航 · 会话 · 标签页，共六种排列）可配置。
- 命令面板 `Ctrl+K`。
- 底部状态栏展示远端 CPU / 内存 / 磁盘 / 网络（可选，也可用右侧 SysInfo 看板）。

**AI / 自动化**

- **调试模式**：本机 HTTP 控制面 + SSE 实时事件流 + MCP 端点，危险能力（执行 JS、读取敏感数据）各自独立开关。
- **日志体系**：脱敏落盘、调用日志、按会话跟踪、诊断包导出，并通过 MCP 工具暴露给 AI 客户端。

## 快捷键与交互

以下为代码中实际绑定的快捷键：

| 快捷键 | 作用 | 生效位置 |
| --- | --- | --- |
| `Ctrl+K` / `Cmd+K` | 打开或关闭命令面板（未解锁时不响应） | 全局 |
| `Ctrl+W` / `Cmd+W` | 关闭当前标签（仅工作区） | 全局 |
| `Ctrl+1` … `Ctrl+9` | 切换到第 N 个标签（标签数足够时） | 全局 |
| `Esc` | 关闭命令面板、补全面板或终端侧栏 | 全局 |
| `↑` / `↓` / `Enter` | 命令面板内上下选择与执行 | 命令面板 |
| `Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` | 粘贴到终端 | 终端 |
| `Shift+Insert` | 粘贴到终端 | 终端 |
| `Ctrl+Shift+C` / `Cmd+C` | 复制选中内容（无选中时不动作） | 终端 |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | 字号放大 / 缩小 / 恢复 13 | 终端 |
| `F11` | 切换全屏 | 终端 |
| `Alt+↓`（可配置） | 进入补全候选导航 | 终端 |
| `↑` / `↓` / `Tab` / `Enter`（导航中） | 切换候选 / 采纳插入 | 终端 |
| `Esc` | 关闭补全面板 | 终端 |

补全导航热键可在设置中自定义（形如 `Alt+ArrowDown` / `Ctrl+Shift+Space`，至少需要一个修饰键，或使用功能键、方向键、`Space`/`Tab`/`Escape`/`Enter`）。

终端交互补充：选中即复制可开关；SFTP 面板支持拖拽文件到面板上传；标签可拖拽到终端区域按落点方向分屏。

## 下载与安装

两种来源：

1. **GitHub Release**：由 `v*` 标签触发，见下方「版本与变更记录」。
2. **Actions 构建产物**：`main` 与 `dev` 分支的 push 会触发 CI 工作流，在 Go 测试与前端 typecheck 通过后并行构建三平台，产物名为
   `ding-ssh-<分支>-<平台>-<完整 sha>`（例如 `ding-ssh-dev-windows-amd64-<sha>`），在对应 workflow run 页面下载。

Release 产物：

| 平台 | 文件 |
| --- | --- |
| Windows x64 | `ding-ssh-<版本>-windows-amd64-setup.exe`（NSIS，内嵌 WebView2 bootstrap）、`ding-ssh-<版本>-windows-amd64.zip` |
| macOS (Universal) | `ding-ssh-<版本>-macos-universal.dmg`、`ding-ssh-<版本>-macos-universal.app.zip` |
| Linux x64 | `ding-ssh-<版本>-linux-amd64.AppImage`、`ding-ssh_<版本>_amd64.deb`、`ding-ssh-<版本>-linux-amd64.tar.gz` |
| 全部 | `SHA256SUMS.txt` 校验和 |

平台注意事项：

- **Windows**：需要 WebView2 运行时；NSIS 安装包已内嵌 bootstrap，运行时会自动补装（CI 构建使用 `-webview2 embed`）。
- **macOS**：未签名，首次打开请在 Finder 中右键 → 打开。
- **Linux**：需要 `libgtk-3-0` 与 `libwebkit2gtk-4.1-0`（`.deb` 会声明依赖）。

## 从源码构建

前置依赖：

- **Go 1.25**（`go.mod` 声明 `go 1.25.0`）
- **Node.js**（CI 使用 22；仅用 `npm`，仓库带 `frontend/package-lock.json`）
- **Wails CLI v2.13**

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0

# 开发模式（前端热更新 + Go 实时编译）
wails dev

# 生产构建（产物在 build/bin/）
wails build

# 后端测试
go test ./...

# 前端类型检查（等价于 frontend/package.json 的 build 首步）
cd frontend && npm ci && node node_modules/vue-tsc/bin/vue-tsc.js --noEmit
```

仓库根目录另有两个 Windows 辅助入口，只为解决「安装 Go/Node 之前就已启动的终端」PATH 未更新的问题：

- `dev-env.ps1`：把 Go（`C:\Program Files\Go\bin`、`%LOCALAPPDATA%\Programs\Go\bin`、项目内 `.tools\go\bin`）、`go env GOPATH\bin`、Node（`C:\Program Files\nodejs`、`%APPDATA%\npm`）补进当前会话 PATH，必要时设置 `GOPROXY=https://goproxy.cn,direct` 与 `GOSUMDB=sum.golang.google.cn`，最后打印 go / node / npm / wails 版本。需 dot-source 后使用：`. .\dev-env.ps1`。
- `dev.cmd`：本机策略禁止直接运行 `.ps1`，因此它用 `powershell -ExecutionPolicy Bypass` 包一层。不带参数时打开一个已激活环境的交互式终端，带参数时执行单条命令，例如 `.\dev.cmd wails dev`、`.\dev.cmd go test ./internal/...`。

Go 与 Node 已在系统 PATH 中时，直接用 `wails` / `go` / `npm` 即可，无需这两个入口。

## 架构与目录结构

Wails 负责窗口与 asset 服务，Go 侧 `App` 结构体作为绑定 API 暴露给前端；`internal/*` 是纯 Go 的业务与基础设施包，彼此不依赖前端；`frontend/` 是 Vue 3 单页应用，通过 Wails 自动生成的绑定（`frontend/wailsjs`）调用 Go。调试模式在 Go 侧由 `internal/debugsrv` 提供 HTTP/MCP 服务端，只有前端才知道的部分（xterm 行列、缓冲区、DOM 几何）通过一条前端桥（`frontend/src/debug/bridge.ts` 与注册表 `registry.ts`）回传。

```
ding-ssh/
├── main.go                    # 入口：Wails 配置、内嵌 frontend/dist、启动前读取调试模式开关
├── app.go                     # Wails 绑定 API（服务器 / 会话 / SFTP / 隧道 / 设置 / 安全 / 配置迁移）
├── debug.go                   # 调试模式：token、debug.json、HTTP op 实现、诊断包导出
├── debug_windows.go           # Windows：CDP 注入的说明与降级（构建标签 windows）
├── debug_other.go             # 非 Windows：空实现
├── internal/
│   ├── models/                # 前后端共享数据结构与设置默认值/规范化
│   ├── store/                 # SQLite（WAL）存储、旧版 JSON 迁移、凭证、分组、命令历史、.dingpack
│   ├── cryptox/               # AES-256-GCM 字段加密、Argon2id 主密码、OS Keyring 保管
│   ├── sshx/                  # SSH 会话、SFTP 与传输、隧道、OSC 7 目录同步、SysInfo
│   ├── localterm/             # 本机终端：按平台选择与启动 shell
│   ├── logx/                  # 日志级别、控制台/文件双通道、轮转、脱敏
│   └── logfilter/             # Wails 自定义 Logger：精确过滤 runtime:ready 噪音
├── cmd/ding-ssh-mcp/          # MCP stdio 代理（HTTP /mcp ↔ stdin/stdout）
├── scripts/                   # set-version.py、三平台打包脚本、debug.ps1 调试辅助
├── .github/workflows/         # ci.yml（测试 + 分支产物）、release.yml（tag 发布）
└── frontend/
    ├── src/
    │   ├── components/        # ServerList / TerminalView / SftpPanel / TunnelPage / SettingsPage / SplitView …
    │   ├── stores/            # Pinia：servers / sessions / settings / ui / groups / credentials
    │   ├── services/          # ssh / settings / history / zmodem / security / sysinfo / groups / credentials
    │   ├── completion/        # Trie + FZF 补全与热键解析
    │   ├── theme/             # 主题预设与生成
    │   ├── debug/             # 调试模式前端桥与终端注册表
    │   ├── panes/             # 分屏布局计算
    │   └── utils/
    └── wailsjs/               # Wails 自动生成绑定（勿手改）
```

## 数据与安全

**数据目录**（`os.UserConfigDir()`，Windows 下即 `%AppData%`）：`%AppData%\ding-ssh\`

| 内容 | 说明 |
| --- | --- |
| `ding-ssh.db` | SQLite 配置库（WAL 模式）：服务器、设置、凭证、分组、命令历史；首次启动会从旧版 `servers.json` / `settings.json` 一次性导入 |
| `logs/` | 日志文件（见「日志」一节） |
| `debug.json` | 调试模式运行时信息（`url` / `lanUrl` / `port` / `token` / CDP 端口 / 开关 / PID 等），权限 `0600`；调试服务停止时会删除，**token 每次启动重新生成** |
| `security.json`、`.master.key` | 主密码状态与本地密钥文件（启用主密码时，密钥改由 OS Keyring 保管） |

**凭据加密**：服务器与凭证中的密码/私钥字段以 AES-256-GCM 加密后入库；密钥由 OS Keyring（`zalando/go-keyring`）保管，也可改用主密码（Argon2id 派生）。启用主密码后，应用启动需要先解锁才能加载设置与服务器。历史明文数据会在启动时自动迁移为密文。

**主机密钥**：当前实现会在 SSH 鉴权阶段读取并展示服务端主机密钥类型与 `sha256` 指纹（连接进度日志中可见），但**尚未强制校验** —— 会话代码中标注了接入 `known_hosts` 的待办。请勿把它当作中间人攻击防护。

**日志脱敏**：token、密码、私钥在写入日志前自动替换为 `***`，固定开启、无法关闭。覆盖 `key=value` / `?token=` 形式的查询串与键值、JSON 敏感字段（`password` / `keyContent` / `privateKey` / `secret` / `apiKey` / `token` 等）、`Authorization: Bearer`、以及整段 PEM 私钥。诊断包只包含脱敏后的内容。

**调试模式**：默认关闭；关闭时不监听任何端口。开启后仅绑定 `127.0.0.1`，需显式打开「允许局域网访问」才绑定 `0.0.0.0`。每次启动生成随机 token，请求需带 `Authorization: Bearer <token>`（也支持 `?token=`）。「允许执行 JS」（`/v1/eval`）与「允许读取敏感数据」是两个独立开关，默认都关闭。修改开关即时生效，无需重启。

## 调试模式与 MCP

在 **设置 → 调试模式** 开启。服务地址与 token 见该页「运行状态」，或读取 `%APPDATA%\ding-ssh\debug.json`。

### HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/health` | 运行状态、端口与开关 |
| GET | `/v1/state` | 视图 / 标签 / 会话 / 隧道 / 设置与调试模式信息快照 |
| GET | `/v1/settings` | 读取应用设置 |
| PUT | `/v1/settings` | 部分更新设置（只传要改的字段），保存后立即生效 |
| GET | `/v1/servers` | 服务器列表（默认脱敏，不含密码/私钥） |
| GET | `/v1/sessions` | 后端会话列表 |
| GET | `/v1/terminals` | 终端诊断：`rows/cols`、缓冲区行数、`baseY/viewportY`、回滚是否可达、可见文本与 DOM 几何 |
| GET | `/v1/terminals/{id}/buffer?from=tail&lines=200` | 读取终端缓冲区文本（含回滚） |
| POST | `/v1/terminals/{id}/input` | 写入会话，`{data}` 或 `{base64}` |
| POST | `/v1/terminals/{id}/scroll` | `{to: top\|bottom\|line\|lines, value}` |
| POST | `/v1/terminals/{id}/resize` | `{cols, rows}`，只改模拟器尺寸、不改 PTY（用于复现渲染问题） |
| POST | `/v1/sessions/{id}/reconnect` | 重连（保留缓冲区，不清屏） |
| POST | `/v1/sessions/{id}/disconnect` | 断开（不关标签） |
| POST | `/v1/tabs` | 打开标签，`{serverId}` 或 `{node}` 或 `{local:true}` |
| DELETE | `/v1/tabs/{id}` | 关闭标签 |
| POST | `/v1/eval` | 在页面上下文执行 JS（需开启「允许执行 JS」） |
| GET | `/v1/screenshot` | 窗口 PNG 截图（需窗口可见） |
| GET | `/v1/events?limit=&topics=` | 最近的后端事件（环形缓冲，默认 50 条，上限 300） |
| GET | `/v1/stream?topics=ssh.output,ssh.status` | SSE 实时事件流，按 topic 订阅 |
| GET | `/v1/logs` | 日志目录、开关、级别、总大小、文件列表与保留策略 |
| GET | `/v1/logs/tail?lines=&level=&keyword=&file=` | 读取日志尾部，可按级别与关键字过滤；`file` 必须是日志目录内的文件名 |
| POST | `/v1/logs/clear` | 清空日志 |
| POST | `/v1/logs/export` | 导出诊断包 zip（返回路径） |
| POST | `/v1/logs/options` | `{consoleEnabled?, fileEnabled?, level?, apiLogEnabled?}` 调整日志设置 |

鉴权：`Authorization: Bearer <token>` 或 `?token=<token>`。

辅助脚本 `scripts/debug.ps1`（端口与 token 自动从 `debug.json` 读取）常用命令：

```powershell
.\scripts\debug.ps1 health                 # 调试服务状态
.\scripts\debug.ps1 state                  # 应用状态快照
.\scripts\debug.ps1 terminals              # 终端诊断
.\scripts\debug.ps1 buffer <id> [lines]    # 读终端缓冲区
.\scripts\debug.ps1 input <id> "echo hi`r" # 向会话写入
.\scripts\debug.ps1 events [limit] [topics]# 最近事件
.\scripts\debug.ps1 screenshot [out.png]   # 窗口截图
.\scripts\debug.ps1 mcp tools              # 列出 MCP 工具
.\scripts\debug.ps1 mcp call <tool> [json] # 调用 MCP 工具
```

### MCP（Streamable HTTP）

端点为 `POST http://127.0.0.1:<port>/mcp`，共 **21 个工具**：

`app_state`、`list_terminals`、`read_terminal`、`send_input`、`scroll_terminal`、`resize_terminal`、`open_tab`、`close_tab`、`reconnect_session`、`disconnect_session`、`list_servers`、`get_settings`、`update_settings`、`eval_js`、`screenshot`、`recent_events`、`list_logs`、`read_logs`、`export_diagnostics`、`clear_logs`、`set_log_options`。

其中 5 个日志工具用于排查：`list_logs` 先看日志在哪、有多少；`read_logs` 读取文本并按 `level`（取该级别及以上，`level=warn` 同时含 WARN 与 ERROR）或大小写不敏感的关键字过滤，行数默认 200、上限 5000；`set_log_options` 可临时把级别调成 `debug`、打开文件/控制台日志或 MCP/调试 API 调用日志；`export_diagnostics` 导出诊断包 zip 并返回路径；`clear_logs` 清空日志。

资源：

| URI | 类型 | 内容 |
| --- | --- | --- |
| `app://state` | `application/json` | 应用状态快照 |
| `app://debug/info` | `application/json` | 调试模式信息 |
| `app://servers` | `application/json` | 服务器列表（脱敏） |
| `app://logs` | `application/json` | 日志文件与日志设置 |
| `app://logs/tail` | `text/plain` | 最新日志末尾 200 行 |
| `terminal://{id}/buffer` | `text/plain` | 指定终端的缓冲区文本 |

HTTP 客户端配置示例：

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

### MCP 服务端推送（SSE）

除请求/响应外，`/mcp` 还提供服务端推送：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/mcp` | JSON-RPC 请求；`initialize` 成功时在**响应头**返回 `Mcp-Session-Id` |
| GET | `/mcp` | 推送通道（SSE），必须带 `Mcp-Session-Id` |
| DELETE | `/mcp` | 结束会话（带 `Mcp-Session-Id`），随后推送通道失效 |

- 推送消息形如 `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","logger":"ding-ssh/<topic>","data":{...}}}`，`logger` 为 `ding-ssh/` 加事件 topic（如 `ssh.output`、`ssh.status`、`tunnel.status`）；
- `data.uri` 指向该事件对应的资源，便于客户端失效缓存：终端输出 → `terminal://{id}/buffer`，隧道事件 → `app://state`；
- 服务端每 20s 发送一行 `: ping` 注释保活（SSE 注释行，可忽略）；
- 推送订阅的 topic 可以在 `initialize` 时用 `?topics=` 指定，留空表示全部；
- 会话由 `initialize` 创建、`DELETE` 结束；应用重启后旧会话失效，需重新 `initialize`。

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

只支持 stdio 传输的 MCP 客户端可用内置代理：它把 stdin 的 JSON-RPC 转发到 `POST <url>/mcp`，把响应与 SSE 推送写回 stdout，对客户端而言就是一个普通的 stdio MCP 服务器。stdout 保持纯 JSON-RPC，日志一律走 stderr 且 token / 会话 id 打码。

```bash
# 自行编译（产物在仓库根目录）
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

也可以不编译，直接让客户端跑源码（工作目录需为仓库根）：

```json
{ "mcpServers": { "ding-ssh": { "command": "go", "args": ["run", "./cmd/ding-ssh-mcp"] } } }
```

地址与 token 的解析优先级：`-url` / `-token` → 环境变量 `DING_SSH_DEBUG_URL` / `DING_SSH_DEBUG_TOKEN` → `%APPDATA%\ding-ssh\debug.json`。

| 参数 / 环境变量 | 说明 |
| --- | --- |
| `-url` · `DING_SSH_DEBUG_URL` | 调试服务地址，如 `http://127.0.0.1:8765`（以 `/mcp` 结尾也可，会自动去掉） |
| `-token` · `DING_SSH_DEBUG_TOKEN` | 调试服务 token |
| `-debug-json` | `debug.json` 路径，默认 `%APPDATA%\ding-ssh\debug.json` |
| `-v` / `-quiet` | 打印转发细节 / 只输出错误 |
| `-once` | 仅探测地址与 token 是否可用，然后退出 |
| `-h` | 中文用法说明 |

行为说明：

- 拿到 `Mcp-Session-Id` 后自动挂 `GET /mcp` 接收 `notifications/message`，断线按 1s / 2s / 5s 退避重连；
- 进程退出前关闭 SSE 连接，并尽力 `DELETE /mcp` 结束会话；
- 应用重启导致端口或 token 变化时，代理会在请求失败后重读 `debug.json` 跟上（用 `-url` / `-token` 显式指定时不覆盖）。

> 关于 CDP：WebView2 的远程调试在 Wails v2.13 下无法从应用侧注入（框架会覆盖 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`，且 `windows.Options` 未暴露该字段）。因此页面级能力由 `/v1/eval` 承担，截图由原生窗口捕获提供。

## 日志

日志目录 `%APPDATA%\ding-ssh\logs`（Linux / macOS 为 `~/.config/ding-ssh/logs`）：

- 当前文件 `app-YYYYMMDD.log`（本地日期）；
- 单文件超过 **8MB** 或跨天时轮转为 `app-YYYYMMDD-HHMMSS.log`（同秒重复轮转追加序号）；
- 目录内最多保留 **20** 个文件，超出删除最旧的，正在写入的文件永不删除。

**设置 → 日志** 提供的能力：

| 能力 | 说明 |
| --- | --- |
| 运行日志（控制台）/ 写入日志文件 | 两个开关互相独立：关掉控制台不影响落盘，反之亦然 |
| 日志级别 | `debug` / `info` / `warn` / `error`，只输出不低于该级别的日志（默认 `info`） |
| 记录 MCP / 调试 API 调用 | 独立开关（默认关闭），每次 HTTP / MCP 调用留一行 `[api]`，含方法、路径、MCP 方法/工具名、状态码、耗时与响应字节数；只记 path、不含 query，不会泄露 token。SSE 长连接只记开始与结束两行 |
| 打开日志文件夹 / 清理日志 | 清理只删 `app-*.log`（先关句柄再删，清理后按原开关重建继续写入） |
| 最近日志预览 | 只读当前文件尾部（默认 200 行），打开页签或点刷新时读一次，不做轮询 |
| 导出诊断包 | 在日志目录生成 `diagnostics-YYYYMMDD-HHMMSS.zip` |

诊断包内容：最近 **3** 个日志文件 + `debug.json`（调试模式运行时才有）+ `state.json`（会话、隧道、设置与终端诊断）+ `env.json`（系统、架构、Go 版本、应用版本、日志目录、日志级别、生成时间）。单个条目写入失败不中断其余条目。

终端右键菜单还有 **「开始 / 停止跟踪此会话日志」**：开启后该会话的**输出**会以 `[trace:<id>]` 旁路一份到日志（去掉 ANSI 转义，单条超过 2000 字符截断），用于复现「某次连接到底发生了什么」；被跟踪的会话列表在「日志」页签中可见。注意只有日志级别为 `debug` 时才会记录这类细粒度跟踪信息。

安全：token / 密码 / 私钥在写入日志前自动打码（`token=***`、`"password":"***"`、`Authorization: Bearer ***`、整段 PEM 私钥），固定开启、无法关闭；诊断包同样只含脱敏内容。

## 已知问题

### WebGL 与界面缩放（`uiScale ≠ 100` 时自动回退 Canvas）

**现象**：界面缩放不是 100% 时，终端顶部前几行看不到，且因为缓冲区已在顶部而无法向上滚动。
**原因**：界面缩放通过 `.app-shell { zoom }` 实现，而 `@xterm/addon-webgl` 在存在 CSS `zoom` 的祖先时会把画布内容整体上移数行——缓冲区与 DOM 几何都正确，只是画出来的像素错位，`clearTextureAtlas()+refresh()` 也无法纠正。
**规避**：仅在 `uiScale = 100` 时启用 WebGL，其余情况自动回退 Canvas 渲染；设置里的 WebGL 开关保持可用，切换界面缩放时会自动重新评估。WebGL 上下文丢失或环境不支持时同样降级 Canvas 并弹出提示。

### `wails dev` 下的 `runtime:ready` 日志噪音

在 `wails dev` 下若用浏览器打开 dev 地址（终端提示的 `http://localhost:34115`），后端会打印两条 `Unknown message from front end: runtime:ready`。这是 Wails v2.13.0 的框架问题：浏览器通过 WebSocket IPC 加载页面时注入的 runtime bundle 会发送内部消息 `runtime:ready`，而 devserver 未像桌面窗口那样拦截。该消息不影响功能，生产构建与桌面窗口都不会出现。本项目通过 `internal/logfilter` 自定义 Logger 精确过滤这两条（仅匹配该字符串，其余日志原样输出）；待上游修复后可删除该包。

### Windows 下 `Ctrl+V` 曾不生效（已修复，保留说明）

Wails v2 在 Windows 上固定调用 `PutAreBrowserAcceleratorKeysEnabled(false)`，WebView2 因此不再执行浏览器层面的编辑加速键：`Ctrl+V` 既不触发原生 `paste` 事件，xterm.js 又把它当控制字符，把 `\x16` 发给远端（表现为 `-bash: $'\026': command not found`）。现在终端在捕获阶段自行处理剪贴板快捷键并统一走 `term.paste()`，行为与 xterm 原生一致。

### 其他限制

- **主机密钥未强制校验**：仅展示指纹，`known_hosts` 校验在代码中标注为待办（见「数据与安全」）。
- **CDP 不可用**：WebView2 远程调试端口无法在 Wails v2.13 下注入，页面级能力改用 `/v1/eval`，截图用原生窗口捕获（受窗口遮挡影响，窗口需可见）。
- **`/v1/terminals/{id}/resize` 只改模拟器尺寸**，不改远端 PTY，仅用于复现布局/渲染问题。

## 版本与变更记录

完整变更记录见 [`CHANGELOG.md`](CHANGELOG.md)——本项目尚未发布正式版本，该文件按开发顺序倒序记录已完成的、可对外说明的变更（现象 → 原因 → 修法）。

- 项目在 `dev` 分支持续开发，未发版时构建产物的版本号为 `0.0.0-dev.<sha>`；
- 推送 `v*` 标签会触发 **Release** 工作流，在 macOS / Windows / Ubuntu 上并行构建并创建同名 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

该工作流也支持手动触发（`workflow_dispatch`），此时只上传 artifacts、不创建 Release。

## 许可证

仓库中未声明项目级许可证。第三方依赖与内嵌字体各自遵循其原始许可（字体资源目录内含 `OFL.txt`）。
