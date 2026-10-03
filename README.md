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

- **调试模式**：本机 HTTP 控制面 + SSE 实时事件流 + MCP 端点（89 个工具，按域分组见「MCP 工具总览」），危险能力各自独立开关。
- **能力位 + 两段式确认**：10 个能力位控制「允不允许这类操作」，破坏性 / 不可逆操作另需 `confirm.prepare` → `confirm.commit`；可逆 / 高频操作不需要 token。
- **审计与「AI 记录」**：每次调用都留结构化记录（含脱敏参数、能力位、耗时、可否撤销），应用内可实时查看并撤销设置类改动。
- **UI 观测与实验**：`ui.*` 读取布局 / 样式 / store，做局部截图与「改前 vs 改后」的像素 + 指纹差异量化，写入只影响当前实例、不落库。
- **SFTP 与终端自动化**：`sftp.*` 走白名单 + 两段式确认，`terminal.run` / `terminal.expect` 让 AI 能发命令、等输出、拿退出码；`terminal.sudo` 可在用户授权后（`sudo.credential` + 两段式确认）用**应用里保存的密码**提权执行 `sudo -i`，密码只经随机临时文件喂给 `sudo -S`、用完即删，绝不进参数 / 返回值 / 日志 / 审计。
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

**能力位与监听地址是两回事**：能力位（含上述两个开关）**就地生效** —— 只有影响监听地址的字段（`enabled` / `port` / `bindLan`）变化才会重启调试服务，因此保存设置或勾选能力位**不会**换端口 / token、也**不会**踢掉已经连上的 MCP / HTTP 客户端（详见「能力位与两段式确认」）。

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
| GET | `/v1/tools` | **工具目录**：全部 MCP 工具的 `{name, capability[], confirmAction?, needsToken, description, inputSchema, hasStructuredOutput, outputSchema?}`，与 `tools/list` 同源 |
| GET/POST | `/v1/ui/*` | UI 观测与实验（`ui.layout`、`ui.query`、`ui.screenshot`、`ui.snapshot`、`ui.diff`、`ui.setToken` …）：读类走 GET、需要参数的读类与写类走 POST，写类需要 `ui.write` 能力位 |

鉴权：`Authorization: Bearer <token>` 或 `?token=<token>`。`/v1/tools` 与其他读类接口一样需要带 token（工具目录里含能力位与描述，属于控制面信息，不做免鉴权例外）。

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

端点为 `POST http://127.0.0.1:<port>/mcp`。当前共 **89 个工具**：`tools/list` 与 `GET /v1/tools` 的条数一致。其中 **63** 个由各域在**能力注册表**里登记（即 `AllRegisteredToolNames()` 的长度，见 `internal/debugsrv/*_register.go` 与 `sudo.go`），另外 **26** 个是内置工具（能力登记在内置表 `toolCaps` 里；`screenshot` 走的是「窗口截图」而非调试 op，因此不登记能力位）。**工具数量不要写死**：跑一次 `tools/list` 或 `GET /v1/tools` 取 `count` 即可。

按域分组（描述末尾都会自动带一行「能力要求」，由能力表与注册表生成，不会与真实校验脱节）：

| 域 | 数量 | 代表工具 | 能力位 |
| --- | --- | --- | --- |
| 内置（状态 / 终端 / 设置 / 日志 / 权限审计确认） | 26 | `app_state`、`list_terminals`、`read_terminal`、`send_input`、`update_settings`、`list_logs`、`screenshot`、`app.permissions`、`app.health`、`app.recent_calls`、`confirm.prepare` / `confirm.commit` | 读类永远允许；写入按能力位（`terminal.input` / `config.write` / `lifecycle` / `eval`） |
| `ui.*`（界面观测与实验） | 25 | `ui.layout`、`ui.query`、`ui.styles`、`ui.hit`、`ui.tree`、`ui.console`、`ui.screenshot`、`ui.snapshot`、`ui.diff`、`ui.assert`、`ui.setToken`、`ui.injectCSS`、`ui.click`、`ui.type` | 读类 `read`；写类 `ui.write`（可逆 / 高频，不需要 token） |
| 应用控制（服务器 / 凭据 / 隧道 / 设置 / 命令 / 生命周期） | 25 | `servers.list` / `get` / `create` / `update` / `delete` / `duplicate` / `moveGroup` / `export` / `import`、`credentials.*`、`tunnels.*`、`settings.schema` / `settings.reset`、`app.commands` / `app.command`、`app.quit` | `config.write` / `secrets.write` / `lifecycle`；写类基本都要两段式确认 |
| SFTP 与终端自动化 | 13 | `sftp.list` / `stat` / `read` / `transfers` / `write` / `mkdir` / `rename` / `remove` / `syncCwd` / `cancel`、`terminal.run` / `terminal.expect` / `terminal.sudo` | 读类 `read`；写类 `fs.remote.write`；`terminal.run` 只要 `terminal.input`；`terminal.sudo` 要 `sudo.credential` + `terminal.input`（详见「`sudo.credential`：用已保存的凭证提权」） |

`GET /v1/tools` 是「一次看清所有工具」的入口（脚本 / 人盘点用）：每个条目带 `capability[]`（空数组 = 读类，永远允许）、`needsToken` 与 `confirmAction`（是否需要 `confirm.prepare` 取 token）、`inputSchema`、`hasStructuredOutput` 与 `outputSchema`。声明了 `outputSchema` 的 13 个工具（`app_state`、`app.health`、`app.permissions`、`list_terminals`、`read_terminal`、`ui.layout`、`ui.diff`、`ui.query`、`sftp.list`、`servers.list`、`servers.get`、`settings.schema`、`app.recent_calls`）在 `tools/call` 里除文本内容外还会返回 **`structuredContent`**（MCP 2025-06-18）；`content[0]` 的文本格式保持不变，老客户端忽略新字段即可。

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
| `ui://layout` | `application/json` | 界面布局骨架（与 `ui.layout` 同源） |
| `ui://view` | `application/json` | 当前 view / 导航状态 |
| `ui://console` | `text/plain` | 前端控制台环形缓冲的尾部文本 |
| `sftp://{sessionId}/cwd` | `application/json` | 该会话 SFTP 当前目录摘要（前 20 条，全量用 `sftp.list`） |
| `transfers://current` | `application/json` | 近期 / 进行中的 SFTP 传输（由进度事件折叠而来） |

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

### 能力位与两段式确认

调试模式把整个应用暴露给 AI，因此权限被拆成两层，**不要混在一起**：

- **能力位 = 「允不允许这类操作」**：10 个能力，逐个开关，宿主在每次工具调用前校验。读类永远允许（`read`，敏感字段仍受「允许读取敏感数据」控制），其余默认关闭，只有用户在 **设置 → 调试模式 → MCP 能力** 里显式打开才放行：

| 能力 | 默认 | 说明 |
| --- | --- | --- |
| `read` | 开（不可关） | 读状态 / 终端缓冲区 / 日志 / 审计 |
| `terminal.input` | **开** | 向终端写入（等价于代替用户敲命令） |
| `secrets.read` | 关 | 读取含密钥的字段（同「允许读取敏感数据」开关） |
| `eval` | 关 | 在页面上下文执行 JS（同「允许执行 JS」开关） |
| `ui.write` | 关 | 改 CSS / 设计令牌 / store / 合成事件 |
| `config.write` | 关 | 写配置（服务器、隧道、应用设置） |
| `secrets.write` | 关 | 新增 / 修改 / 删除凭据（同时要求 `secrets.read`） |
| `fs.remote.write` | 关 | 远端文件写入（同时要求白名单非空，见下） |
| `sudo.credential` | 关 | **用应用里保存的密码执行 `sudo -i`**（同时要求 `secrets.read`；每次执行仍需两段式确认，见下） |
| `lifecycle` | 关 | 重载页面 / 退出应用 / 清空日志 |

- **两段式确认 = 「不可逆操作前再想一次」**：只有破坏性 / 不可逆动作才需要 `confirm.prepare` → `confirm.commit`（或把 token 作为参数传给写工具）。token 一次性、绑定 `action` + `args`、默认 120 秒过期。

**可逆 / 高频操作只受能力位约束，不需要 token**：`send_input`、`terminal.run` / `terminal.expect`、`eval_js`、全部 `ui.*` 写入。否则「AI 管 SSH」与「改一点看一眼」的循环直接不可用。

需要 token 的动作（`ConfirmableActions()`，数量随注册表增长）：`servers.create/update/delete`、`credentials.create/update/delete`、`tunnels.create/update/delete/start/stop`、`sftp.write/mkdir/rename/remove/cancel`、`terminal.sudo`、`settings.update/reset`、`logs.clear`、`app.quit`、`app.command`（只对会改配置的命令）、以及 **`capability.enable`** —— 开启敏感能力本身就是扩权动作，必须走确认。

用 `app.permissions` 可随时查看当前能力快照、每个能力的说明与 `confirmActions`；`tools/list` 里每个工具的 `inputSchema` 与描述末尾的「能力要求」行都写明它需要什么。

#### `sudo.credential`：用已保存的凭证提权（默认关，高风险）

**它解决什么**：很多运维动作（改系统配置、装包、重启服务）必须 root。以前 AI 只能请用户在终端里手敲密码，或让用户自己配免密 sudo；`terminal.sudo` 让 AI 在用户显式授权后，用**应用里已经保存的那份密码**完成提权 —— 密码始终不经过 AI 的手。

**开启条件（缺哪一个都会被中文明确指出来）**：

1. `sudo.credential` 能力位本身打开 —— 设置 → 调试模式 → MCP 能力，**开启时需要二次确认**（与「远端文件写入」「应用生命周期」同一套交互，后端 `GetCapabilities().requiresConfirm` 也会列出它）；
2. 同时开启 `secrets.read`（=「允许读取敏感数据」）—— 密码只从凭据库读，不允许读密钥就不可能用保存的密码提权。任一条不满足时，`terminal.sudo` 返回 `isError` + 中文原因（说清缺哪一项、去哪里开）；
3. 每次调用 `terminal.sudo` **仍然**要两段式确认：先 `confirm.prepare(action="terminal.sudo", args={id, command})` 拿一次性 token，再把 token 传给工具（参数名 `confirm`，别名 `token`）。

**它如何工作（一条完整流程）**：

1. **前置校验**：会话必须是 SSH 会话（本机终端直接拒绝）、必须对应一台**已保存的服务器**、该服务器必须**保存了密码**（按 `servers.get includeSecrets=true` 的同一条宿主路径读取：`App.GetServers` → 存储层解密），并且远端写入白名单必须命中 `/tmp`（否则中文拒绝并说明去哪加）；
2. 本包生成**随机**临时文件路径 `/tmp/.ding-sudo-<随机 12 位十六进制>`，并先后两次过既有白名单校验（写入前 + 写入后复核）；
3. 宿主把 `<密码>\n` 用**会话已有的那份 SFTP 客户端**（不新建 SSH 连接）写进该临时文件 —— 密码只存在于宿主进程内，返回值只有 `{ok, path, bytes}`；
4. **同一个会话里**用终端执行**一条** shell（用 `;` 串起来，任何一步失败都会继续往下走到清理）：
   `chmod 600 <临时文件>` → 失败则**不执行** sudo → `sudo -S -p '' -i -- /bin/sh -c '<command>' < <临时文件>`（密码**只经由文件重定向**进入 sudo，绝不进命令行；`-c` 是内层 `/bin/sh` 的参数，sudo 本身没有 `-c`）→ `rm -f <临时文件> || true` → `ls -d <临时文件>` 校验已删除 → 打印退出码标记 `__DING_SUDO_RC__<退出码>__LS__<ls 退出码>__CHMOD__<0|1>`；
5. **清理有两条通道（任一确认删除即 `cleanedUp=true`）**：
   - **通道 A（原 shell）**：执行那条 shell 自带的 `rm -f` + `ls -d` 校验 —— 退出码标记里带了 `ls` 的退出码（非 0 = 文件确实不在了）；
   - **通道 B（独立 SFTP，核心）**：无论成功 / 失败 / 超时，返回前都调一次宿主 op `sudo.cleanup` —— 用**该会话已有的那份 SFTP 客户端** `Remove` 临时文件，并再 `Stat` 一次复核，返回 `{removed, existed, verified}`（幂等：文件本来就不存在时 `verified=true`）。它与终端作业无关（unlink 不受前景进程是否阻塞影响），因此**命令挂起 / 超时时也真的能删掉**；
   - 顺序是先 B 后 A：独立通道没确认删除时，才退回再发一条 `rm -f` + `ls`（尽力而为的最后手段）；
6. **输出净化**：返回前把捕获输出里出现的密码替换成 `***`（净化由宿主做 —— 它才是唯一持有明文的一侧），返回里的 `scrubbed=true` 表示已确认返回文本不含该密码；`false` 时会在返回里明确警告「本段输出未经打码，请勿落盘」；
7. 返回结构与常见失败提示：`{ok, sudo:true, sessionId, matched, reason, elapsedMs, exitCode?, tempFile, cleanedUp, cleanupAttempted, cleanupVerified, scrubbed, output, outputBytes, truncated, hint?}`。`ok=true` 只表示**流程走完**（拿到退出码与清理结果），命令本身的成败看 `exitCode`；`cleanedUp=true` 表示**结束时会话已确认临时文件不存在**（通道 A 或通道 B 任一确认即可），`cleanupAttempted` / `cleanupVerified` 如实说明独立通道是否被调用、是否复核确认删除；`Sorry, try again` / `incorrect password` → 「保存的密码不正确或该账号无 sudo 权限（请核对后重试，或改配免密 sudo）」；超时 / 静默**不是协议错误**（返回 `matched=false` + 已捕获输出 + 中文说明）。

**超时 / 异常时的清理保证（2026-10 修的缺陷）**：

- **修的是什么**：原先唯一的兜底清理是「在同一条远端 shell 里再发 `rm -f` + `ls` 校验」，排在用户命令之后。命令**永不返回**时（实测：`get_hd_smartinfo -d 1 -i 1 | head -45` 阻塞不打印）那行从未执行；随后靠 Ctrl-C 恢复终端时，tty 在 SIGINT 时会**丢弃输入队列**，兜底行同样丢失 ⇒ 目标机残留 `/tmp/.ding-sudo-<12hex>`（**含明文密码**）。
- **现在怎么做**：收尾固定走**独立 SFTP 通道**（宿主 op `sudo.cleanup`，复用会话已有的 SFTP 客户端，不新建连接、不走终端、不走 shell），`Remove` 后再 `Stat` 复核，且**先清理再返回**（不等调用方善后）。超时批次因此也可以是 `cleanedUp=true`。
- **`cleanupAttempted` / `cleanupVerified` 的语义**：前者 = 是否调用了独立通道（成功路径也会调用，用于如实报告）；后者 = 该通道是否复核确认文件已不存在。`cleanedUp` = 通道 A 的 `ls` 校验 **或** 通道 B 的 `cleanupVerified`（两者取或）。
- **清理失败时**：`cleanedUp=false`，并且 `hint` 会给出**残留文件的完整路径**与中文处置建议（「请立即在目标机执行 `rm -f /tmp/.ding-sudo-xxxx` 删除，并轮换该账号密码」）。
- **事后兜底入口 `cleanupOnly`**：`terminal.sudo{id, cleanupOnly:"/tmp/.ding-sudo-xxxx"}` —— 传了它 ⇒ **跳过取密码与写临时文件**，只对该路径做一次独立清理与复核，返回 `{ok, sudo:false, cleanupOnly:true, sessionId, path, removed, existed, cleanupAttempted, cleanupVerified, cleanedUp, hint?}`（幂等：文件不存在也算成功）。它不碰终端（因此终端仍被挂起的作业占着也能用）、不占「同会话串行化」名额，但仍要 `sudo.credential` + 两段式确认（action 仍是 `terminal.sudo`）。
- **只接受本工具生成的路径形态**：`cleanupOnly` 与 `sudo.cleanup` 都严格要求 `^/tmp/\.ding-sudo-[0-9a-f]{12}$`，其它路径（`/etc/passwd`、`/tmp/.ding-sudo-XYZ`、相对路径、含 `..` …）一律中文拒绝，并仍要过既有远端写入白名单 —— 它不是任意删除原语。
- **SFTP 不可用时**：返回 `cleanupVerified=false` + 中文原因，**不会**退化成「再发一条终端命令」（终端可能仍被挂起的作业占着），此时 `cleanedUp` 可能为 `false`，请按 `hint` 里的残留路径人工处理。

**边界与「不做的事」**：

- **不接受调用方传入的密码**：工具的 `inputSchema` 里**没有** `password` 字段，硬塞 `password` 参数会被直接拒绝（永不接受），也不支持从 `command` 里透传密码；
- **不用于本机终端**：本机终端没有远端账号、也没有 SFTP 通道 → 中文拒绝；
- **不在远端持久保留任何凭证**：临时文件随机命名 + `chmod 600` + 用完即删 + **双通道清理/校验**（原 shell 的 `ls` 校验 + 独立 SFTP 的 `Remove`/`Stat` 复核，见上文「超时 / 异常时的清理保证」）；不写 shell history、不改 sudoers、不落任何用户配置；
- **密码不进审计 / 日志 / 返回值 / MCP 资源**：本工具的审计 args 只有 `id` / `command` / `timeoutMs` / `idleMs` / `cleanupOnly`（token 本身也会被审计脱敏成 `***`）；宿主的日志只记会话 / 路径 / 字节数；返回值在净化之后才会序列化；
- **白名单不含 `/tmp` 时直接拒绝**：临时文件路径同样受「远端写入白名单」约束（只按路径段前缀匹配），拒绝时给出中文说明与配置位置。

**更安全的替代方案**（强烈建议优先考虑）：不要让 AI 拿着可复用的登录密码去提权，而是在该机器上配置**免密 sudo**（只放开具体命令），或者「密钥登录 + 受限 sudo 规则」：

```sudoers
# /etc/sudoers.d/ding-ai —— 只允许重启服务，且不需要密码（visudo -c 校验后再启用）
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx, /usr/bin/systemctl reload nginx
```

```sudoers
# 或者：保留密码，但限定可执行的命令范围（此时仍可用 terminal.sudo，只是把破坏面收窄）
deploy ALL=(root) /usr/bin/systemctl restart nginx, /usr/bin/journalctl -u nginx
```

配好免密 sudo 后，`terminal.run` 就能完成同样的工作，而**不需要**打开 `sudo.credential` 能力 —— 本能力的价值只在「该机器没配免密、且必须立刻提权」时体现。另外：`sudo.credential` 只影响 AI 侧；如果这台机器的密码与登录密码相同，请优先考虑改用密钥登录 + 独立提权口令。


### 审计与「AI 记录」页签

每次 AI / 调试接口调用都会留一条**结构化审计记录**（时间、来源 `mcp|http|ui`、工具名、能力位、耗时、结果、已脱敏参数、是否可撤销与撤销提示），无论成功、被拒还是失败。环形缓冲保留最近 500 条：

- 应用内：**设置 → AI 记录** 页签实时查看，可按能力 / 来源过滤；设置类改动（`update_settings` / `set_log_options`）还提供**「撤销」**按钮（宿主保留了改动前的设置快照，只保留最近 20 条，撤销后快照作废）；
- AI 侧：`app.recent_calls` 自查「刚才那次操作到底做了什么」；
- 明文密钥（`credentials.create` 的 `content`、`servers.import` 的 `json` 等）在进审计缓冲**之前**就被替换为 `***`，不会落进环形缓冲。

### UI 观测与实验

25 个 `ui.*` 工具让 AI「看见」界面并做可量化实验（实现见 `internal/debugsrv/ui.go` + `frontend/src/debug/ui.ts`）：

- **观测（读）**：`ui.layout`（布局骨架 + 非预期重叠）、`ui.query` / `ui.styles` / `ui.tokens` / `ui.hit` / `ui.tree` 定位元素与样式、`ui.console` 读前端控制台、`ui.revision` 判断「DOM 认知是否已失效」、`ui.screenshot` / `ui.elementShot` 局部截图、`ui.snapshot` 存档、`ui.diff` 量化差异、`ui.assert` 自动验收；
- **实验（写，需要 `ui.write`）**：`ui.setToken`、`ui.injectCSS`、`ui.storePatch`、`ui.navigate`、`ui.window`、`ui.reload`、`ui.click`、`ui.type`、`ui.key`、`ui.hover`、`ui.scroll`。这些写入**只作用于当前运行实例（内存态），不写设置、不落库**，刷新或 `ui.reload` 后还原。

`ui.diff` 是**像素 + 指纹双口径**：

- 像素口径：16×16 分块比较（`threshold` 控制通道差阈值），合并相邻变化块成 `regions[]`（最多 20 个），返回 `changedRatio` / `changedPixels`，并把原图叠半透明红的 **diff 图**作为图片内容返回、同时存盘；两次快照尺寸不同则按最小公共区域裁剪并在结果里说明；
- 指纹口径：`changed[].fields[]` 只报**真正变化**的字段，其中 **`styles` 细化到 CSS 属性级**（形如 `{name:"styles", styles:{"border-radius":{from,to}}}`，只列值真的变了的属性；一个属性都没变就不出现该条目），其余字段 `rect` / `classes` / `zIndex` / `visible` / `text` 保持 `{name, from, to}` 形状；
- 两次快照之间若发生过刷新 / HMR（`revision` 变化），会显式提示「DOM 差异可能不可比」，像素差异仍可参考。

快照落在日志目录的 `ui-snapshots/`（`<name>.png` / `<name>.json`），文件名有白名单校验（防目录穿越）。

### SFTP 与终端自动化

**SFTP**（`sftp.*`，走会话已有的 SFTP 连接，不新建连接）：读用 `sftp.list` / `sftp.stat`（Lstat 语义，符号链接给出 target）/ `sftp.read`（`encoding=text|base64`，默认按 256KB 截断，返回 `sha256`）/ `sftp.transfers`；写用 `sftp.write`（覆盖写，`append=true` 追加）/ `sftp.mkdir` / `sftp.rename` / `sftp.remove` / `sftp.syncCwd`，传输可用 `sftp.cancel` 取消。

- **白名单是硬边界**：写 / 删 / 改名 / 建目录的路径都必须是**绝对路径、不含 `..` 段**，且命中 **设置 → 调试模式 → MCP 能力 → 远端写入白名单** 的前缀（按路径段边界匹配：`/srv/app` 放行 `/srv/app` 与 `/srv/app/x`，不放行 `/srv/app-evil`）。**白名单留空 = 一律拒绝**。
- **每个写操作都要两段式确认**（`fs.remote.write` + token），且返回里会写明「远端改动不可撤销、无法通过审计回滚」。
- 远端写完成后会自动让 SFTP 面板缓存失效，因此 AI 写文件不会让用户看到旧目录内容。

**终端自动化**：

- `terminal.run`：发送 `command`（自动补 `\r`），给了 `expect` 正则就等它命中，未给 `expect` 时等**输出静默**（`idleMs`，默认 400ms；首段**真实输出**之前保持 1500ms 宽限）后返回 `{matched, pattern?, exitMarker?, reason, elapsedMs, output, outputBytes, truncated, idleMs?, echoSkippedBytes?, bufferTail}`。
  - **`expect` 只在「命令回显之后」的文本上匹配**：PTY 会先把命令行回显回来，命令里含 `echo ===DONE===` + `expect="===DONE==="` 这种写法不会命中回显（`echoSkippedBytes` 如实给出跳过的回显字节数）。更稳妥的写法是让标记在远端运行时才展开：`D=__MCP_$(echo DONE)__; <你的命令>; echo $D` + `expect="__MCP_DONE__"`。
  - **超长命令的折行回显也能定位准**：命令比终端宽度长时 PTY 会把回显折成交错的多段（可见文本里多出换行），旧实现按「逐字 + 换行」定位会失准；现在改为**长度口径为主**（逐字节比较命令文本、比较时跳过折行插入的换行，中文按字节数算，与显示宽度无关），并与换行口径**取更稳的那个**（`terminalEchoEnd` / `terminalEchoEndByLength`），两者都定位不到时退回「不跳过」的保守行为。
  - 「启动慢、先静默后吐输出」的命令（如 `docker ps`）在**未给 `expect`** 时可能过早返回、只拿到回显；这类场景请两步走：先 `terminal.run`（不要 expect）拿初始输出，再用 `terminal.expect` 等结果，或加大 `idleMs`。
  - `wrapExitMarker=true` 会把命令包成 `command; printf '\n__DING_EXIT__%s\n' "$?"` 以捕获退出码（返回里 `wrapped=true` 与 `wrappedCommand` 如实说明命令被改过）；这是 **POSIX shell** 语义，在 Windows 本地终端（cmd / PowerShell）上拿不到退出码。
  - `expect` 超时**不是协议错误**：返回 `matched=false` + 已捕获输出 + 中文说明。
- `terminal.expect`：纯等待（**不发送任何数据**），`pattern` 正则命中即返回；`sinceMarker` 可跳过上一次已看过的内容。它没有「回显」可跳过，因此 pattern 在本次等待收到的全部新增文本上匹配。
- `terminal.sudo`：用**应用里保存的密码**在当前会话对应的服务器上执行 `sudo -i`（需要 `sudo.credential` + `terminal.input`，且每次都要 `confirm.prepare(action="terminal.sudo")`）。密码只经随机临时文件喂给 `sudo -S`、用完即删（**双通道清理**：原 shell 的 `rm`/`ls` 校验 + 独立 SFTP 的 `Remove`/`Stat` 复核，超时/挂起时后者才是可靠的），绝不进参数 / 返回值 / 日志 / 审计；`idleMs` **省略时不启用「静默提前返回」**（一直等到退出码标记或 `timeoutMs`，避免慢命令被误判成卡住），显式给出（50–60000）时按 `terminal.run` 的语义用；可选参数 `cleanupOnly`（只接受本工具生成的 `/tmp/.ding-sudo-<12hex>` 形态）是**超时/异常后的事后清理入口**：只做清理与复核，不取密码、不写临时文件、不碰终端。完整流程、边界与更安全的替代方案见上文「能力位与两段式确认」里的 `sudo.credential` 小节。
- **同会话串行化**：同一会话上同一时刻只允许一个 `terminal.run` / `terminal.expect` / `terminal.sudo`，第二个调用被**直接拒绝**（不排队），避免两个调用互相偷走对方的输出增量。

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
- **事件通道满会丢事件**：`/v1/stream`、`GET /mcp`（SSE）与终端自动化共用同一个 Hub 通道，订阅者消费不及时（或阻塞太久）时**该订阅者会丢事件**（Hub 不排队、不阻塞发布方）；`terminal.run` / `terminal.expect` 因此可能漏掉部分输出（返回里的 `outputBytes` 与 `droppedOutput` 用于自查），需要完整历史时用 `read_terminal` 读缓冲区。
- **`ui.reload` 会重挂整个前端**：页面重载后终端重连、DOM 状态与快照指纹全部失效（`ui.revision` 自增），正在进行的 `ui.*` 会话需要重新定位元素；需要保留现场请先 `ui.snapshot`。
- **`ui.*` 依赖前端已构建的调试桥**：非 `wails dev` 场景下必须先把前端产物构建好（`npm run build` 或 `wails build`），否则 `frontend/src/debug/ui.ts` 的桥不在页面里，`ui.*` 会返回「未知操作」这类中文错误（后端此时仍能正常工作，只是页面内能力不可用）。
- **`wrapExitMarker` 是 POSIX 语义**：在 Windows 本地终端（cmd / PowerShell）上包裹命令无法拿到退出码，请用 `read_terminal` 或自行 `echo %ERRORLEVEL%` / `$LASTEXITCODE`。

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
