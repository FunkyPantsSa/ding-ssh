<div align="center">
  <img src="frontend/icon/ding.png" width="280" alt="ding-ssh：猫咪终端应用图标">
  <h1>ding-ssh</h1>
  <p><strong>把 SSH 终端、SFTP、端口转发与本机 shell 装进同一个桌面工作台。</strong></p>
</div>

[![CI](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml)

一个把 **SSH 终端 / SFTP / 端口转发 / 本机 shell** 装进同一窗口的桌面工作台，并额外内置一套**只监听本机**的控制面（HTTP + SSE + MCP），让脚本与 AI 客户端可以直接读写应用状态、驱动终端。

| | |
| --- | --- |
| 运行平台 | Windows x64 · macOS（Universal）· Linux x64 |
| 技术栈 | Go 1.25 + Wails v2.13 · Vue 3.5 + Pinia + Tailwind v4 + Vite 7 · xterm.js 6 |
| 数据落地 | 本机 SQLite（WAL）+ 系统钥匙串，无云同步、无遥测 |
| 当前分支 | `dev`（持续开发，`main` 为合并线） |
| 变更记录 | [`CHANGELOG.md`](CHANGELOG.md) · [`docs/2.0设计规范.md`](docs/2.0设计规范.md) · [`docs/refactor-2.0/`](docs/refactor-2.0/) |

---

## 目录

- [一、它解决什么问题](#一它解决什么问题)
- [二、能力总览](#二能力总览)
- [三、五分钟上手](#三五分钟上手)
- [四、界面地图](#四界面地图)
- [五、快捷键](#五快捷键)
- [六、从源码构建](#六从源码构建)
- [七、代码地图](#七代码地图)
- [八、数据与安全](#八数据与安全)
- [九、调试模式：给 AI 的本机控制面](#九调试模式给-ai-的本机控制面)
- [十、日志与诊断](#十日志与诊断)
- [十一、已知限制](#十一已知限制)
- [十二、CI 与发布](#十二ci-与发布)
- [十三、UI 2.0 设计系统](#十三ui-20-设计系统)
- [十四、更多文档与许可](#十四更多文档与许可)

---

## 一、它解决什么问题

日常运维常在这几件事之间来回切窗口：连服务器、改配置、传文件、开隧道、看负载。ding-ssh 的判断是——**这些都该在同一个终端窗口里完成**，而且这个窗口应该能被自动化：

1. **本地优先**。服务器清单、凭据、命令历史、设置全部落在本机配置目录，敏感字段用 AES-256-GCM 加密，密钥交给系统钥匙串或你自己设的主密码。没有账号体系，也没有服务端。
2. **终端是中心**。SFTP 面板与 shell 的当前目录双向同步，隧道、本机 shell、分屏都复用同一套标签体系，而不是各开一个对话框。
3. **可被观察、可被驱动**。设置里打开「调试模式」后，应用多出一个本机 HTTP 控制面与 MCP 端点：读布局、读终端缓冲、发命令、等输出、截图对比、导出诊断包——权限按「能力位」逐项放开，破坏性操作还要两段式确认。

它**不是**：跳板机 / 堡垒机（没有服务端审计）、配置管理工具（不批量下发）、也不是通用 SSH 代理（隧道只服务本机端口）。

## 二、能力总览

| 模块 | 你能做什么 | 实现要点 |
| --- | --- | --- |
| **终端** | 多标签会话；左右 / 上下分屏，分隔条可拖；把标签拖到终端区域按落点方向分屏 | xterm.js + WebGL 渲染（已修正 CSS `zoom` 下的画布位图，任意缩放档位都可用；环境不支持 / 上下文丢失时自动降级 Canvas）；5000 行回滚；字号 `Ctrl+=` / `Ctrl+-` / `Ctrl+0` |
| **连接** | 密码或私钥认证（粘贴内容或选文件）；分组、搜索、在线状态测试 | 五步进度（DNS/直连 → TCP → 鉴权 → 分配 PTY → 就绪）带逐步日志；15s 心跳保活；断线按 2/5/10/20/30s 退避自动重连，**重连不清屏** |
| **SFTP** | 浏览、上传（拖拽 / 多选）、下载、重命名、删除、新建目录、路径直编；底部进度条可取消 | 目录读取走 SWR 缓存（`sync.Map` + 异步 revalidate + 增量推送）；传输并发 = CPU 核心 × 2，令牌桶限速（默认不限速）；rz/sz（Zmodem）可接管 |
| **目录联动** | shell 里 `cd` 同步到 SFTP 面板；SFTP 跳转同步回终端 | 优先解析 OSC 7，拿不到时用 prompt 兜底；两个方向可分别开关 |
| **隧道** | 本地转发 / 远程转发 / 动态转发（SOCKS5）；启动、停止、重启、编辑、删除 | 状态变更通过事件回传界面 |
| **本机终端** | 不开 SSH 也能开本机 shell | Windows：PowerShell / cmd；macOS：zsh / bash；Linux：默认 shell（`go-pty`） |
| **命令补全** | 输入时弹出候选，`Tab` / `↑` / `↓` 采纳；本地命令历史参与排序 | Trie + FZF 风格打分；导航热键可自定义（需带修饰键或功能键） |
| **命令面板** | `Ctrl+K` 跳页面、连服务器、开本机终端、进设置 | 最近命令、服务器、操作、导航分组；命令随当前状态生成 |
| **状态看板** | 底部状态栏或右侧面板看远端 CPU / 内存 / 磁盘 / 网络 | 按会话启动采集器，空闲时暂停 |
| **外观** | 7 套预设色板 + 自定义 ANSI 16 色、背景图与模糊、字体、明暗跟随系统 | 预设模式下终端色板随界面明暗切换；缩放 80%–150%；标签栏位置（顶栏 / 左侧）与导航区段顺序可换；左侧导航与右侧面板宽度可拖 |
| **输出着色** | 终端输出按语义自动着色：关 / 语义 / 强调三档 | 纯前端 `Colorizer`，只注入 SGR；ANSI/OSC/CSI 交互序列和无换行提示符立即原样透传 |
| **数据** | `.dingpack` 配置包导出导入（口令加密）；主密码；跨设备搬家 | 服务器上限 500 条，导入可覆盖同 ID |
| **自动化** | 调试模式 HTTP API、SSE 实时事件流、MCP（含 stdio 代理） | 能力位 + 两段式确认 + 审计记录，见[第九节](#九调试模式给-ai-的本机控制面) |

## 三、五分钟上手

**1. 拿到安装包**

- GitHub Release：打 `v*` 标签后自动产出 macOS `.dmg` / `.app.zip`、Windows `setup.exe` / `.zip`、Linux `.AppImage` / `.deb` / `.tar.gz`，附 `SHA256SUMS.txt`。
- CI 产物：`main` / `dev` 每次 push 通过测试后构建三平台，产物名形如 `ding-ssh-dev-windows-amd64-<sha>`。
- macOS 包未签名：首次打开请在 Finder 里右键 →「打开」。

**2. 首次启动**

启用了主密码时会先出现解锁框，未启用则直接进入工作区。想恢复「上次关掉时还开着」的会话，在设置里打开「启动后自动连接上次的服务器」。

**3. 加一台服务器**

左侧导航 → 会话区 → 新建，填主机 / 端口 / 用户，选密码或私钥（私钥可粘贴内容，也可选文件）。分组用于归类，状态测试会给出 TCP 握手延迟与 SSH 端口连通性。服务器右键菜单可「复制 IP」「复制配置（生成 `原名.new` 并直接打开编辑）」。

**4. 打开终端**

双击服务器即开会话。卡住时展开连接进度，看是哪一步慢（DNS、握手、鉴权、PTY）。

**5. 传文件 / 开隧道**

右侧面板切到 SFTP，路径与终端目录自动联动，拖文件进面板即上传。隧道页新建转发，模式选本地转发、远程转发、动态 SOCKS5 或反向 SOCKS5。

#### 动态 SOCKS5

| 隧道模式 | 在哪配置代理 | 流量出口 |
| --- | --- | --- |
| SOCKS5 -D | 本机应用使用 `127.0.0.1:1080` | SSH 服务器 |
| 反向 SOCKS5 -R | SSH 服务器上的应用使用 `127.0.0.1:1080` | 运行 Ding SSH 的本机 |

在「隧道 → 新建隧道」中选择「反向 SOCKS5」，选中服务器并填写服务器代理端口，默认是 `1080`。Ding SSH 从本机连接服务器的 SSH 端口，服务器可以通过内网、VPN 或跳板机访问。服务器须允许远程 TCP 转发（OpenSSH 的 `AllowTcpForwarding remote` 或 `yes`，以及相应的 `PermitListen` 策略）。本机负责连接目标，直接使用本机可达的网络。若两端之间只有服务器能主动连接本机，需要先建立可达路径，或在服务器上使用 SSH 客户端连接本机的 SSH 服务。

在服务器上验证代理，并把目标域名交给代理出口解析。

```bash
curl --proxy socks5h://127.0.0.1:1080 https://example.com
```

- **代理无认证。** 服务器上的其他用户、进程和容器也可能使用这个代理访问本机的回环服务、内网和公网，流量会从本机出口发出。仅对可信服务器启用，用完后停止隧道。
- **限制监听范围。** 应用请求服务器绑定 `127.0.0.1`，并拒绝 SSH 服务报告的非回环来源。OpenSSH 请保持 `GatewayPorts no`，用防火墙阻止外部访问代理端口。`GatewayPorts yes` 会强制扩大监听范围，服务端配置和来源报告必须可信。[OpenSSH sshd_config 手册](https://man.openbsd.org/sshd_config)
- **按应用启用。** SOCKS5 只支持 TCP CONNECT。应用需要显式配置代理。全局接管流量时，要为 SSH 对端保留直连路由，避免隧道路由环路。


**6. 想给 AI 用？**

设置 → 调试模式 → 打开总开关 → 按需勾选能力位；token 与端口写在 `debug.json` 里（见[第九节](#九调试模式给-ai-的本机控制面)）。

## 四、界面地图

```
┌──────────────────────────────────────────────────────────────┐
│ 顶栏：标签页（可选） · 命令面板入口 · 设置                      │
├────────────┬─────────────────────────────────┬───────────────┤
│ 左侧导航    │ 终端区（可左右 / 上下分屏）       │ 右侧工具面板   │
│ · 导航      │  ┌──────────┬──────────┐        │ · SFTP        │
│ · 会话      │  │ tab-1    │ tab-2    │        │ · SysInfo     │
│ · 标签页    │  └──────────┴──────────┘        │ · 传输历史     │
├────────────┴─────────────────────────────────┴───────────────┤
│ 底部状态栏：远端 CPU / 内存 / 磁盘 / 网络（可选）               │
└──────────────────────────────────────────────────────────────┘
```

- **左侧导航**由三个区段组成（导航 / 会话 / 标签页），顺序可在设置里六选一；折叠状态记在 `localStorage`，宽度可拖并同步给浮层定位。
- **标签栏**可放顶栏或左侧导航；标签右键可复制远端 IP。
- **终端区**支持左右 / 上下分屏与拖拽分屏，分隔条可拖动。
- **右侧面板**宽度可拖，内容按需切换。
- **快速连接**是从右侧贴边收起的侧栏，点选即开会话。
- **命令面板**（`Ctrl+K`）是全局入口，`Esc` 关闭。

## 五、快捷键

| 快捷键 | 作用 | 生效位置 |
| --- | --- | --- |
| `Ctrl+K` / `Cmd+K` | 打开 / 关闭命令面板（未解锁时不响应） | 全局 |
| `Ctrl+W` / `Cmd+W` | 关闭当前标签（仅工作区） | 全局 |
| `Ctrl+1` … `Ctrl+9` | 切到第 N 个标签（标签数足够时） | 全局 |
| `Ctrl+B` / `Cmd+B` | 展开 / 收起左侧导航 | 全局 |
| `Ctrl+Shift+B` / `Cmd+Shift+B` | 展开 / 收起右侧工具栏 | 工作区 |
| `Esc` | 关闭命令面板 / 补全面板 / 终端侧栏 | 全局 |
| `↑` `↓` `Enter` | 命令面板内选择与执行 | 命令面板 |
| `Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` / `Shift+Insert` | 粘贴 | 终端 |
| `Ctrl+Shift+C` / `Cmd+C` | 复制选中内容（无选中时不动作） | 终端 |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | 字号放大 / 缩小 / 恢复 13 | 终端 |
| `F11` | 全屏 | 终端 |
| 自定义热键（默认 `Alt+↓`） | 进入补全候选导航 | 终端 |
| `↑` `↓` `Tab` `Enter` | 候选切换 / 采纳插入 | 补全面板 |

粘贴与复制由终端在捕获阶段自行拦截，绕开 WebView2 的加速键限制；`Ctrl+C` 始终是 SIGINT，不会被抢作复制。鼠标右键可配置为「打开选项栏」或「直接粘贴」。补全导航热键支持形如 `Alt+ArrowDown` / `Ctrl+Shift+Space` 的写法，至少需要一个修饰键，或使用功能键、方向键、`Space`/`Tab`/`Escape`/`Enter`。

## 六、从源码构建

**依赖**

| 需要 | 版本 / 说明 |
| --- | --- |
| Go | 1.25+（`go.mod` 声明 `go 1.25.0`） |
| Node.js | 22.12+（仓库 `.nvmrc` 固定 22.23.4；Vite 7 / Vitest 5 的最低要求） |
| Wails CLI | `v2.13.0` |
| Linux 额外 | `libgtk-3-dev`、`libwebkit2gtk-4.1-dev`（构建加 `-tags webkit2_41`） |
| Windows 额外 | WebView2 运行时；打包安装器需 NSIS |

**常用命令**

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0

# 开发模式：前端热更新 + Go 实时编译
wails dev

# 生产构建（产物在 build/bin/）
wails build

# 后端测试
go test ./internal/...

# 前端类型检查（等价于 frontend/package.json 里 build 的第一步）
cd frontend && npm ci && node node_modules/vue-tsc/bin/vue-tsc.js --noEmit
```

Windows 上仓库提供两个辅助入口，只为解决「装完 Go/Node 之前就已打开的终端 PATH 未更新」：

- `scripts/dev/dev-env.ps1`：把 Go（`C:\Program Files\Go\bin`、`%LOCALAPPDATA%\Programs\Go\bin`、项目内 `.tools\go\bin`）、`GOPATH\bin`、Node（`C:\Program Files\nodejs`、`%APPDATA%\npm`）补进当前会话 PATH，必要时设置 `GOPROXY` / `GOSUMDB` 镜像，切回项目根目录并打印 go / node / npm / wails 版本。需 dot-source：`. .\scripts\dev\dev-env.ps1`。
- `scripts/dev/dev.cmd`：本机策略禁止直接跑 `.ps1`，因此用 `-ExecutionPolicy Bypass` 包一层。不带参数打开已激活环境的项目终端，带参数执行单条命令：`.\scripts\dev\dev.cmd wails dev`、`.\scripts\dev\dev.cmd go test ./internal/...`。

**发布用打包脚本**（`scripts/`，由 Release 工作流调用）

| 脚本 | 产物 |
| --- | --- |
| `package-macos.sh` | `ding-ssh-<ver>-macos-universal.dmg` / `.app.zip` |
| `package-windows.ps1` | `ding-ssh-<ver>-windows-amd64.zip` / `-setup.exe` |
| `package-linux.sh` | `.tar.gz` / `.deb`（nfpm，声明 gtk3 + webkit2gtk-4.1 依赖）/ `.AppImage` |
| `set-version.py` | 把版本号写进 `wails.json` 的 `info.productVersion` |
| `debug.ps1` | 调试控制面辅助脚本（端口与 token 自动读 `debug.json`） |

## 七、代码地图

### 1.1.0 架构调整

本次不是换色或局部页面改版，而是一次保持业务能力与原信息架构的前端分层重构：

- **设计系统成为单一来源**：样式从一个巨型 `style.css` 拆成 `tokens.css → semantic.css → components.css`，Tailwind v4 只映射语义令牌；主题引擎只写语义值，终端色板独立在 `terminal.css`。配色由 Material Color Utilities 的 HCT / Tonal Spot 从用户种子动态派生（主导航、二级导航、工作区、卡片、交互、文字与状态各自成组），`uiSchema=4` 负责旧设置的幂等迁移；材质支持 Solid / Soft Glass / Clear Glass，详见 [`docs/theme-system.md`](docs/theme-system.md)。
- **应用外壳与业务解耦**：`App.vue` 只负责组装与全局状态；侧栏、顶栏、解锁页进入 `layouts/`，终端工作区进入 `views/`，命令面板、服务器、设置、终端与隧道各自拆成领域组件。原有「工作区 / 服务器 / 隧道 / 设置」导航保持不变，传输历史仍属于工作区右侧面板。
- **公共交互收口**：表单、对话框、状态与空态收口到 `components/ui/`；主题、快捷键、Toast、设置和终端补全状态下沉到 `composables/`。Dialog 和菜单补齐焦点陷阱、方向键 / Home / End / Esc、焦点恢复及 ARIA 语义。
- **命令与自动化共用契约**：命令面板、`app.commands` 与快捷键来自统一注册表；关键区域使用 `data-ui`，动态项使用业务 id 的 `data-tab-id` / `data-testid`，避免调试工具依赖 Tailwind 类名与 DOM 下标。
- **终端输出管线继续保持字节安全**：补全算法从 `TerminalView` 拆出；语义着色器遇到 ANSI/OSC/CSI 控制序列时原样透传，不再缓存 oh-my-zsh / powerlevel10k 这类无换行提示符，避免光标与输入错行。
- **可验证与可隔离**：新增页面流程、设置迁移、组件可访问性、终端常驻与着色器回归用例；`scripts/verify-ui2.sh` 同时检查类型、构建、Go 测试、迁移守卫和产物体积。`DING_SSH_DEV_CONFIG_DIR` 可把数据库、日志、快照、调试 token 与本地测试密钥全部隔离，不触碰真实系统钥匙串。

```
.
├── main.go                 Wails 入口：窗口参数、frontend/dist 内嵌、启动/关闭钩子
├── internal/
│   ├── apphost/            Wails 宿主实现与同包测试：业务门面、调试控制面、本地路径平台适配
│   ├── appdir/             配置目录单一入口 + `DING_SSH_DEV_CONFIG_DIR` 隔离
│   ├── models/             前后端共享结构（ServerNode、Settings、DebugSettings、事件负载）
│   ├── sshx/               SSH 会话、SFTP 缓存与传输、隧道、OSC 7 解析、SysInfo 采集
│   ├── localterm/          本机 shell（PowerShell / cmd / zsh / bash，走 go-pty）
│   ├── store/              SQLite（WAL）+ JSON 回退、迁移、.dingpack 读写
│   ├── cryptox/            AES-256-GCM 字段加密 + Argon2id 主密码 + 系统钥匙串
│   ├── debugsrv/           调试控制面：路由、能力位、两段式确认、审计、MCP、UI 观测、SFTP/终端自动化
│   ├── logx/               日志落盘、轮转、脱敏、尾部读取
│   └── logfilter/          过滤 Wails 运行时的日志噪音
├── cmd/ding-ssh-mcp/       独立 stdio MCP 代理（给只支持 stdio 的客户端）
├── scripts/dev/            Windows 开发环境激活与命令入口
├── frontend/src/
│   ├── App.vue             应用组装、顶级状态与导航切换
│   ├── layouts/            AppLayout / AppSidebar / AppTopBar / AppUnlock
│   ├── views/              终端工作区（保持终端实例常驻）
│   ├── components/         领域组件 + `ui/` 基础组件
│   ├── composables/        主题 / 快捷键 / Toast / 设置 / 终端补全逻辑
│   ├── stores/             Pinia：servers / sessions / settings / ui / credentials / groups / transfers
│   ├── services/           对 wailsjs 绑定与事件的薄封装
│   ├── completion/         Trie + FZF 打分 + 热键解析
│   ├── theme/              primitive / semantic / component token、迁移、预设与主题引擎
│   ├── panes/layout.ts     分屏布局计算
│   └── debug/              前端侧 UI 观测桥（布局 / 样式 / store / 合成事件）
├── scripts/                三平台打包 + 版本写入 + 调试辅助 + UI 2.0 门禁
├── build/                  Wails 平台资源（图标、Info.plist、nfpm、NSIS 模板）
└── docs/                   2.0 设计规范、重构计划、ADR 与验收记录
```

**前后端契约**：前端通过 Wails Bind 直接调用 `App` 方法（服务器 CRUD、设置读写、SFTP 传输、隧道控制、会话读写、安全与导入导出等），后端通过 `EventsEmit` 把终端输出、会话状态、连接进度、隧道状态推给前端。调试模式下同一份事件还会被旁路投递给 SSE 订阅者与日志追踪——**界面看到的和 AI 看到的是同一股数据流**。

## 八、数据与安全

**文件放在哪**（`os.UserConfigDir()/ding-ssh/`，Windows 即 `%AppData%\ding-ssh\`）

开发/自动化可设置绝对路径环境变量 `DING_SSH_DEV_CONFIG_DIR`，让数据库、旧 JSON、日志、
UI 快照、`debug.json` 和本地测试密钥全部落入隔离目录。该模式不读取或创建真实系统钥匙串条目；
相对路径会被忽略。生产运行不要设置此变量。

| 文件 | 内容 |
| --- | --- |
| `ding-ssh.db` | SQLite 主库（WAL）：`servers` / `settings` / `credentials` / `groups` / `command_history` |
| `settings.json`、`servers.json`、`groups.json` | 旧版 JSON 存储；首次启动会一次性导入 SQLite，之后仅在 SQLite 不可用时回退 |
| `security.json`、`.master.key` | 主密码状态与本地密钥文件（启用主密码后密钥改由 OS Keyring 保管） |
| `logs/` | 日志与 `ui-snapshots/`（见[第十节](#十日志与诊断)） |
| `debug.json` | 调试模式运行时信息：`url` / `lanUrl` / `port` / `token` / CDP 端口 / 开关 / PID，权限 `0600`；服务停止即删除，**token 每次启动重新生成** |

**加密链路**

- 字段级：AES-256-GCM，密文带 `enc1:` 前缀，用于识别与迁移；存量明文敏感字段在启动时自动加密迁移。
- 密钥级：默认交给系统钥匙串（`zalando/go-keyring`）；也可设置主密码，用 Argon2id（3 轮、64 MiB、4 线程）从口令派生 32 字节密钥。解锁前，需要密钥的操作会被 `ensureUnlocked()` 拦住。
- 配置包：`.dingpack` 为口令加密的 JSON 载荷（magic `DINGPACK1`），含服务器、凭据、分组，服务器上限 500 条。

**边界说明**

- 应用不做统计上报与更新检查；除你主动连接的 SSH 主机外，只有调试模式会监听端口，且默认仅 `127.0.0.1`。
- **主机密钥未强制校验**：连接进度日志里会展示服务端主机密钥类型与 `sha256` 指纹，但 `known_hosts` 校验在代码中标注为待办——请勿把它当作中间人防护。
- 日志与审计在写入前自动脱敏 token / 密码 / 私钥（覆盖 `key=value`、`?token=`、JSON 敏感字段、`Authorization: Bearer`、整段 PEM），固定开启、无法关闭；诊断包同样只含脱敏内容。
- 调试模式的能力位**就地生效**：只有影响监听地址的字段（`enabled` / `port` / `bindLan`）变化才会重启服务，因此勾选能力位不会换端口 / token，也不会踢掉已连接的客户端。

## 九、调试模式：给 AI 的本机控制面

默认关闭，关闭时不监听任何端口。开启后在 `127.0.0.1:<port>`（默认 8765，可切局域网）暴露控制面，地址与 token 见「设置 → 调试模式 → 运行状态」，或读 `debug.json`。请求需带 `Authorization: Bearer <token>`（也接受 `?token=`）。

### 权限模型：能力位 + 两段式确认

两条规则各管一件事，不要混：

- **能力位**回答「这类操作允不允许」。读类永远允许；其余默认关闭，在「设置 → 调试模式 → MCP 能力」里逐项打开。
- **两段式确认**回答「不可逆操作前再想一次」。只有破坏性动作需要 `confirm.prepare` → `confirm.commit`；可逆 / 高频动作（终端输入、界面写入、eval）只受能力位约束——否则「AI 管 SSH」和「改一点看一眼」的循环直接不可用。

| 能力位 | 默认 | 说明 |
| --- | --- | --- |
| `read` | 开（不可关） | 读状态 / 终端缓冲 / 日志 / 审计 |
| `terminal.input` | **开** | 向终端写入（等价于代替用户敲命令） |
| `secrets.read` | 关 | 读含密钥的字段（同「允许读取敏感数据」开关） |
| `eval` | 关 | 页面上下文执行 JS（同「允许执行 JS」开关） |
| `ui.write` | 关 | 改 CSS / 设计令牌 / store / 合成事件 |
| `config.write` | 关 | 写配置（服务器、隧道、应用设置） |
| `secrets.write` | 关 | 增删改凭据（同时要求 `secrets.read`） |
| `fs.remote.write` | 关 | 远端文件写入（白名单非空才生效） |
| `sudo.credential` | 关 | 用应用里保存的密码执行 `sudo -i`（同时要求 `secrets.read`） |
| `lifecycle` | 关 | 重载页面 / 退出应用 / 清空日志 |

需要 token 的动作（`ConfirmableActions()`，随注册表增长）：`servers.create|update|delete`、`credentials.create|update|delete`、`tunnels.create|update|delete|start|stop`、`sftp.write|mkdir|rename|remove|upload`、`terminal.sudo`、`settings.update|reset`、`logs.clear`、`app.quit|reload`、`app.command`（仅对会改配置的命令）、以及 `capability.enable`——开启敏感能力本身就是扩权动作。

token 一次性、绑定 `action` + `args`、默认 120 秒过期。一次写操作长这样：

```
tools/call confirm.prepare  {"action":"servers.delete","args":{"id":"srv-1"}}
→ {"token":"cfm_...","expiresInMs":120000,"summary":"..."}
tools/call servers.delete   {"id":"srv-1","token":"cfm_..."}
```

HTTP 调用方把 token 放 `?confirmToken=...`；两个入口共用同一个确认管理器，token 可互换。`app.permissions` 可随时查看当前能力快照与各动作说明。

### HTTP 端点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/health` | 运行状态、端口与开关 |
| GET | `/v1/state` | 视图 / 标签 / 会话 / 隧道 / 设置与调试信息快照 |
| GET | `/v1/settings` · PUT | 读设置；部分更新（只传要改的字段），立即生效 |
| GET | `/v1/servers` | 服务器列表（默认脱敏，不含密码 / 私钥） |
| GET | `/v1/sessions` | 后端会话列表 |
| GET | `/v1/terminals` | 终端诊断：`rows/cols`、缓冲行数、`baseY/viewportY`、回滚可达性、可见文本与 DOM 几何 |
| GET | `/v1/terminals/{id}/buffer?from=tail&lines=200` | 读终端缓冲（含回滚） |
| POST | `/v1/terminals/{id}/input` | 写入会话，`{data}` 或 `{base64}` |
| POST | `/v1/terminals/{id}/scroll` | `{to: top\|bottom\|line\|lines, value}` |
| POST | `/v1/terminals/{id}/resize` | `{cols, rows}`，只改模拟器尺寸、不改 PTY |
| POST | `/v1/sessions/{id}/reconnect` · `/disconnect` | 重连（保留缓冲区）/ 断开（不关标签） |
| POST | `/v1/tabs` · DELETE `/v1/tabs/{id}` | 开标签（`{serverId}` / `{node}` / `{local:true}`）/ 关标签 |
| POST | `/v1/eval` | 页面上下文执行 JS（需 `eval`） |
| GET | `/v1/screenshot` | 窗口 PNG 截图（窗口需可见） |
| GET | `/v1/events?limit=&topics=` | 最近事件（环形缓冲，默认 50 条、上限 300） |
| GET | `/v1/stream?topics=ssh.output,ssh.status` | SSE 实时事件流，按 topic 订阅 |
| GET | `/v1/logs` · `/v1/logs/tail` | 日志目录与保留策略；尾部读取（可按级别 / 关键字 / 文件过滤） |
| POST | `/v1/logs/clear` · `/export` · `/options` | 清日志（`lifecycle` + token）/ 导出诊断包 / 调日志开关 |
| GET | `/v1/tools` | **工具目录**：`{name, capability[], confirmAction?, needsToken, description, inputSchema, hasStructuredOutput, outputSchema?}`，与 `tools/list` 同源 |
| GET/POST | `/v1/ui/*` | UI 观测与实验：读类走 GET，带参读类与写类走 POST，写类需 `ui.write` |
| * | `/mcp` | MCP（Streamable HTTP） |

`/v1/tools` 与其它读接口一样要带 token：工具目录本身属于控制面信息，不做免鉴权例外。

辅助脚本（端口与 token 自动读 `debug.json`）：

```powershell
.\scripts\debug.ps1 health                  # 调试服务状态
.\scripts\debug.ps1 state                   # 应用状态快照
.\scripts\debug.ps1 terminals               # 终端诊断
.\scripts\debug.ps1 servers / settings      # 服务器（脱敏）/ 设置
.\scripts\debug.ps1 buffer <id> [lines]     # 读终端缓冲
.\scripts\debug.ps1 input <id> "echo hi`r"  # 向会话写入
.\scripts\debug.ps1 scroll <id> <to> [val]  # 滚动终端
.\scripts\debug.ps1 resize <id> <cols> <rows>
.\scripts\debug.ps1 open <serverId> [-Local] / close <id>
.\scripts\debug.ps1 reconnect <id> / disconnect <id>
.\scripts\debug.ps1 eval "<js>"             # 需开启 eval 能力位
.\scripts\debug.ps1 events [limit] [topics] # 最近事件
.\scripts\debug.ps1 stream [topics]         # 订阅 SSE（Ctrl+C 退出）
.\scripts\debug.ps1 screenshot [out.png]    # 窗口截图
.\scripts\debug.ps1 mcp tools|resources|call <tool> [json]
```

### MCP 工具面

端点为 `POST http://127.0.0.1:<port>/mcp`。当前共 **89 个工具**：**63** 个由各域在能力注册表登记（`AllRegisteredToolNames()`），**26** 个是内置工具（能力登记在内置表 `toolCaps`；`screenshot` 走窗口截图，不登记能力位）。**数量不要写死**——跑一次 `tools/list` 或 `GET /v1/tools` 取 `count` 即可。

| 域 | 数量 | 代表工具 | 能力位 |
| --- | --- | --- | --- |
| 内置（状态 / 终端 / 设置 / 日志 / 权限确认） | 26 | `app_state`、`list_terminals`、`read_terminal`、`send_input`、`update_settings`、`list_logs`、`screenshot`、`app.permissions`、`app.recent_calls`、`confirm.prepare` / `confirm.commit` | 读类永远允许；写入按 `terminal.input` / `config.write` / `lifecycle` / `eval` |
| `ui.*`（界面观测与实验） | 25 | `ui.layout`、`ui.query`、`ui.styles`、`ui.tree`、`ui.console`、`ui.screenshot`、`ui.diff`、`ui.assert`、`ui.setToken`、`ui.injectCSS`、`ui.click`、`ui.type` | 读类 `read`；写类 `ui.write`（可逆 / 高频，不需 token） |
| 应用控制（服务器 / 凭据 / 隧道 / 设置 / 命令 / 生命周期） | 25 | `servers.*`（含 `duplicate` / `moveGroup` / `export` / `import`）、`credentials.*`、`tunnels.*`、`settings.schema` / `settings.reset`、`app.commands` / `app.command`、`app.quit` | `config.write` / `secrets.write` / `lifecycle`，写类基本都要确认 |
| SFTP 与终端自动化 | 13 | `sftp.list`、`sftp.stat`、`sftp.read`、`sftp.transfers`、`sftp.write`、`sftp.mkdir`、`sftp.rename`、`sftp.remove`、`sftp.syncCwd`、`sftp.cancel`、`terminal.run`、`terminal.expect`、`terminal.sudo` | 读类 `read`；写类 `fs.remote.write`；`terminal.run` 只要 `terminal.input`；`terminal.sudo` 要 `sudo.credential` + `terminal.input` |

声明了 `outputSchema` 的工具在 `tools/call` 里除文本内容外还返回 **`structuredContent`**（MCP 2025-06-18），`content[0]` 的文本格式保持不变，老客户端忽略新字段即可。

**MCP 资源**（便于客户端缓存与直接读取）：

| URI | 类型 | 内容 |
| --- | --- | --- |
| `app://state` · `app://debug/info` · `app://servers` · `app://logs` | JSON | 状态、调试信息、服务器（脱敏）、日志设置 |
| `app://logs/tail` · `terminal://{id}/buffer` · `ui://console` | text | 日志尾部、终端缓冲、前端控制台尾部 |
| `ui://layout` · `ui://view` | JSON | 布局骨架、当前 view 与导航状态 |
| `sftp://{sessionId}/cwd` · `transfers://current` | JSON | 会话当前目录摘要（前 20 条）、近期传输 |

**服务端推送**：`POST /mcp` 的 `initialize` 在响应头返回 `Mcp-Session-Id`；`GET /mcp`（需带该头）是 SSE 推送通道；`DELETE /mcp` 结束会话。推送形如 `notifications/message`，`logger` 为 `ding-ssh/<topic>`（如 `ssh.output`、`ssh.status`、`tunnel.status`），`data.uri` 指向对应资源便于失效缓存；每 20s 发一行 `: ping` 保活；`?topics=` 可在 `initialize` 时指定订阅范围。

```bash
# 1) initialize：用 -D 打印响应头，取回 Mcp-Session-Id
curl -s -D - -X POST http://127.0.0.1:8765/mcp \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'

# 2) 挂上推送通道（-N 关闭缓冲，Ctrl+C 退出）
curl -N -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp

# 3) 结束会话
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp
```

HTTP 客户端配置：

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

**stdio 客户端**用仓库自带的代理（把 stdin 的 JSON-RPC 转发到 `POST <url>/mcp`，响应与推送写回 stdout，日志一律走 stderr 且打码）：

```bash
go build -o ding-ssh-mcp ./cmd/ding-ssh-mcp
./ding-ssh-mcp -v
```

| 参数 / 环境变量 | 说明 |
| --- | --- |
| `-url` · `DING_SSH_DEBUG_URL` | 调试服务地址，如 `http://127.0.0.1:8765`（以 `/mcp` 结尾也可） |
| `-token` · `DING_SSH_DEBUG_TOKEN` | 调试服务 token |
| `-debug-json` | `debug.json` 路径，默认在配置目录下 |
| `-v` / `-quiet` / `-once` / `-h` | 打印转发细节 / 只输出错误 / 仅探测可用性 / 中文用法 |

解析优先级：`-url`/`-token` → 环境变量 → `debug.json`。拿到会话 id 后自动挂 `GET /mcp` 接收推送，断线按 1s / 2s / 5s 退避重连；退出前关闭 SSE 并尽力 `DELETE /mcp`；应用重启换端口或 token 后，代理会在请求失败时重读 `debug.json` 跟上（显式传参时不覆盖）。

### 终端与 SFTP 自动化

- **`sftp.*`** 复用会话已有的 SFTP 连接，不新建连接。读：`sftp.list` / `sftp.stat`（Lstat 语义，符号链接给 target）/ `sftp.read`（`encoding=text|base64`，默认 256KB 截断并返回 `sha256`）/ `sftp.transfers`。写：`sftp.write`（`append=true` 追加）/ `mkdir` / `rename` / `remove` / `syncCwd`，传输可用 `sftp.cancel` 取消。
- **白名单是硬边界**：写 / 删 / 改名 / 建目录必须是绝对路径、不含 `..`，且命中「远端写入白名单」的路径段前缀（`/srv/app` 放行 `/srv/app` 与 `/srv/app/x`，不放行 `/srv/app-evil`）。**白名单留空 = 一律拒绝**。每个写操作都要 `fs.remote.write` + token，远端改动不可撤销、无法通过审计回滚；写完成后自动让 SFTP 面板缓存失效。
- **`terminal.run`**：发送命令（自动补 `\r`），给了 `expect` 就等它命中，未给则等输出静默（`idleMs` 默认 400ms，首段真实输出前保持 1500ms 宽限）。`expect` 只在「命令回显之后」的文本上匹配；超长命令折行回显也能按长度口径定位。`wrapExitMarker=true` 会把命令包成 `command; printf '\n__DING_EXIT__%s\n' "$?"` 以拿退出码（POSIX 语义）。
- **`terminal.expect`**：纯等待，不发数据；`sinceMarker` 可跳过已看过的内容。
- **`terminal.sudo`**：用应用里保存的密码执行 `sudo -i`。密码经随机临时文件（`/tmp/.ding-sudo-<12hex>`、`chmod 600`）喂给 `sudo -S`，绝不进参数 / 返回值 / 日志 / 审计；收尾走**双通道清理**（原 shell 的 `rm`+`ls` 校验 + 独立 SFTP 的 `Remove`+`Stat` 复核），因此命令挂起 / 超时也能真的删掉；失败时 `hint` 给出残留文件完整路径与处置建议。返回里 `cleanedUp` / `cleanupAttempted` / `cleanupVerified` 如实说明清理结果，`scrubbed` 表示输出已确认不含密码。可选 `cleanupOnly` 只接受本工具生成的路径形态，是超时后的事后清理入口（幂等，不碰终端）。
- **同会话串行化**：同一会话上同一时刻只允许一个 `terminal.run` / `terminal.expect` / `terminal.sudo`，第二个调用被**直接拒绝**（不排队），避免互相偷走输出增量。

> **更安全的替代**：优先给 AI 配**免密 sudo**（只放开具体命令，例如 `/etc/sudoers.d/ding-ai` 里 `deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx`），这样 `terminal.run` 就能完成同样的工作，而**不必**打开 `sudo.credential`。该能力的价值只在「机器没配免密、且必须立刻提权」时体现。

### 审计与 UI 实验

- **AI 记录**（设置页签）：每次调用留结构化记录（时间、来源 `mcp|http|ui`、工具名、能力位、耗时、结果、脱敏参数、可否撤销），无论成功、被拒还是失败；环形缓冲保留最近 500 条。设置类改动（`update_settings` / `set_log_options`）提供「撤销」按钮（保留最近 20 条改动前快照）。AI 侧可用 `app.recent_calls` 自查。
- **UI 观测**：`ui.layout`（布局骨架 + 非预期重叠）、`ui.query` / `ui.styles` / `ui.tokens` / `ui.hit` / `ui.tree`、`ui.console`、`ui.revision`（判断 DOM 认知是否失效）、`ui.screenshot` / `ui.elementShot`、`ui.snapshot` / `ui.diff` / `ui.assert`。
- **UI 实验**（需 `ui.write`）：`ui.setToken`、`ui.injectCSS`、`ui.storePatch`、`ui.navigate`、`ui.window`、`ui.reload`、`ui.click` / `ui.type` / `ui.key` / `ui.hover` / `ui.scroll`。写入只作用于当前运行实例（内存态），不写设置、不落库，刷新即还原。
- **稳定选择器**：自动化优先使用 `[data-ui="topbar"]`、`sidebar`、`terminal`、`context-panel`、`statusbar`、`command-palette`、`settings` 等语义标识；动态标签用 `data-tab-id`，服务器与命令项用业务 id 的 `data-testid`。不要依赖 Tailwind 类名或数组下标。
- **`ui.diff` 双口径**：像素侧 16×16 分块比较（`threshold` 控通道差），合并相邻变化块为 `regions[]`（最多 20 个），返回 `changedRatio` / `changedPixels` 与叠加红色的 diff 图；指纹侧 `changed[].fields[]` 只报真正变化的字段，其中 `styles` 细化到 CSS 属性级。快照落在 `logs/ui-snapshots/`（文件名有白名单校验）。

## 十、日志与诊断

日志目录为 `<os.UserConfigDir()>/ding-ssh/logs`（Windows `%AppData%\ding-ssh\logs`，macOS `~/Library/Application Support/ding-ssh/logs`，Linux 通常为 `~/.config/ding-ssh/logs`）：

- 当前文件 `app-YYYYMMDD.log`；单文件超过 **8 MB** 或跨天时轮转为 `app-YYYYMMDD-HHMMSS.log`；
- 目录内最多保留 **20** 个文件，超出删最旧，正在写入的文件永不删除；
- 尾部读取默认 200 行，单次最多 4 MB。

| 设置项 | 说明 |
| --- | --- |
| 运行日志（控制台）/ 写入日志文件 | 两个开关互相独立 |
| 日志级别 | `debug` / `info` / `warn` / `error`，默认 `info` |
| 记录 MCP / 调试 API 调用 | 独立开关（默认关），每次调用留一行 `[api]`，含方法、路径、状态码、耗时、响应字节数；只记 path 不含 query，不泄露 token |
| 打开日志文件夹 / 清理日志 | 清理只删 `app-*.log`，先关句柄再删，之后按原开关重建 |
| 导出诊断包 | 生成 `diagnostics-YYYYMMDD-HHMMSS.zip`：最近 3 个日志 + `debug.json` + `state.json` + `env.json`（单个条目失败不影响其余） |

终端右键菜单还有「开始 / 停止跟踪此会话日志」：开启后该会话输出以 `[trace:<id>]` 旁路进日志（去 ANSI、单条超 2000 字符截断），用于复现「某次连接到底发生了什么」；这类细粒度跟踪需要日志级别为 `debug`。

## 十一、已知限制

**WebGL 与界面缩放（已修复）**：缩放通过 `.app-shell { zoom }` 实现，而 `@xterm/addon-webgl` 用 ResizeObserver 的 `devicePixelContentBoxSize` 给 canvas 定尺寸——Chromium 下该值**含 CSS zoom**（zoom=1.25 时位图 990×553、渲染器网格只有 792×442），而 `_setCanvasDevicePixelDimensions()` 只改位图、不重设 GL viewport / resolution uniform / 顶点归一化，于是整幅网格被画进超大位图的左下角：缓冲区与 DOM 几何都对，只有像素错位，表现为「顶部前几行看不到、缓冲区已在顶部无法上滚」，`refresh()` / `clearTextureAtlas()` 都纠不回来。

修法见 `frontend/src/utils/xterm-webgl-zoom.ts`：zoom ≠ 1 时丢弃观察器上报的（含 zoom 的）尺寸，位图保持 `handleResize()` 按未缩放网格算出的尺寸；每次 `fit()` / 缩放切换后再自检「位图 == 网格」，不一致就重跑 `handleResize()` 把 viewport / uniform / 顶点一起补齐。因此现在**任意缩放档位都启用 WebGL**；只有补丁打不上（addon 私有 API 变化）且缩放 ≠ 100% 时才回退 Canvas，上下文丢失 / 环境不支持时同样降级并提示。排查时看 `[webgl] enabled …` 日志行，含 `uiScale / dpr / grid / bitmap / css / rect / cols×rows / viewport / baseY` 快照。

**`wails dev` 的 `runtime:ready` 噪音**：用浏览器打开 dev 地址时，后端会打印两条 `Unknown message from front end: runtime:ready`。这是 Wails v2.13.0 的框架问题（devserver 未拦截 runtime 的内部消息），不影响功能，生产构建与桌面窗口都不会出现；本项目用 `internal/logfilter` 精确过滤这两条。

**其他**

- **主机密钥未强制校验**：只展示指纹，`known_hosts` 校验仍是待办。
- **CDP 不可用**：WebView2 远程调试端口在 Wails v2.13 下无法从应用侧注入（框架会覆盖 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`），因此页面级能力由 `/v1/eval` 承担，截图走原生窗口捕获（受窗口遮挡影响）。
- **`/v1/terminals/{id}/resize` 只改模拟器尺寸**，不改远端 PTY，仅用于复现布局 / 渲染问题。
- **事件通道满会丢事件**：`/v1/stream`、`GET /mcp`（SSE）与终端自动化共用同一个 Hub 通道，订阅者消费不及时时该订阅者丢事件（Hub 不排队、不阻塞发布方）。`terminal.run` / `terminal.expect` 因此可能漏掉部分输出（看 `outputBytes` / `droppedOutput` 自查），需要完整历史时用 `read_terminal` 读缓冲。
- **`ui.reload` 会重挂整个前端**：终端重连、DOM 状态与快照指纹全部失效（`ui.revision` 自增），进行中的 `ui.*` 会话需重新定位元素；要保留现场先 `ui.snapshot`。
- **`ui.*` 依赖已构建的前端**：非 `wails dev` 场景需先 `npm run build`（或 `wails build`），否则页面里没有调试桥，`ui.*` 会返回「未知操作」这类中文错误（后端仍正常，只是页面内能力不可用）。
- **`wrapExitMarker` 是 POSIX 语义**：Windows 本地终端（cmd / PowerShell）拿不到退出码，请改用 `read_terminal` 或自行 `echo %ERRORLEVEL%` / `$LASTEXITCODE`。
- **macOS 包未签名 / 未公证**，首次打开需右键「打开」。
- **局域网监听会扩大暴露面**：`bindLan` 打开后同网段都可达（token 仍有效），不用时请关掉。

## 十二、CI 与发布

| 工作流 | 触发 | 做什么 |
| --- | --- | --- |
| `ci.yml` | push 到 `main` / `dev` / `master`，以及任意 PR | Go `internal` 测试 + 前端 `vue-tsc` 类型检查；`main` / `dev` push 额外并行构建三平台并上传 artifact（不创建 Release） |
| `release.yml` | 推送 `v*` 标签或手动触发 | 先跑测试，再在三平台构建 + 打包，汇总 `SHA256SUMS.txt` 并创建 Release（手动触发只上传 artifact） |

未发版时版本号为 `0.0.0-dev.<sha>`，由工作流注入前端「关于」页。

```bash
git tag v1.0.0
git push origin v1.0.0
```

## 十三、UI 2.0 设计系统

> UI 2.0 已按 `docs/refactor-2.0/` 的 P0–P4 拆分实施。设计令牌、组件基座、应用外壳、业务页、命令与快捷键、稳定 DOM 标识及性能门禁均已进入代码；跨平台人工冒烟与发布签名按发布清单执行。

**为什么不是 Material Design 3**：MD3 移动优先、tonal 抬升、低密度，与本应用的高密度桌面工作台
（服务器列表 / SFTP 文件表 / 状态栏）冲突；且 MD3 不改渲染管线，既不提升显示效果，也不提升性能。

**规范选型（已评估，按适配度）**

| 规范 | 作用 | 结论 |
| --- | --- | --- |
| W3C Design Tokens (DTCG) | 设计令牌标准化格式，单一来源 | 作为统一骨架采用 |
| IBM Carbon | 企业级、高密度、density 档位、暗色成熟 | 最佳借鉴对象（借架构，不搬组件） |
| WCAG 2.2 AA + WAI-ARIA APG | 无障碍与键盘交互 | 必选基线 |
| Microsoft Fluent 2 | 桌面密度模型 | 次级参考 |
| Material Design 3 | 移动优先 | 不采用 |
| Apple HIG | 仅 macOS 壳层 | 不作全平台统一规范 |

**统一蓝图**

- **身份层**：Grid Desk，保留现有审美，写成明文原则，不再凭感觉；
- **令牌层**：现有 117 个 CSS 变量重组织为 `primitive → semantic → component` 三层，7 套预设从 semantic 派生；
- **密度层**：借鉴 Carbon 思路，`compact`（SFTP / 列表 / 状态栏）与 `comfortable`（设置 / 命令面板）两档；
- **基线**：WCAG 2.2 AA + WAI-ARIA APG（键盘优先）+ `prefers-reduced-motion`；
- **动效**：补全 duration / easing 状态动效表。

**当前实现**

1. **token 分层**：`theme/tokens.css` → `semantic.css` → `components.css`，主题引擎只写语义值；动态配色架构、迁移与验收矩阵见 [`docs/theme-system.md`](docs/theme-system.md)。
2. **无障碍基线**：命令面板具备 dialog/listbox 语义、循环键盘导航、焦点保护与输入法保护。
3. **自动化契约**：关键区域使用 `data-ui`，动态项使用业务 id，`ui.layout` 只依赖稳定语义选择器。
4. **资源治理**：字体支持可配置字体及对应许可；`scripts/verify-ui2.sh` 检查 CSS、JS、字体和 dist 预算。

## 十四、更多文档与许可

- [`CHANGELOG.md`](CHANGELOG.md)：按开发顺序倒序记录已完成变更（现象 → 原因 → 修法）。
- [`docs/2.0设计规范.md`](docs/2.0设计规范.md)：UI 2.0 的视觉原则、交互规则与组件规范。
- [`docs/refactor-2.0/`](docs/refactor-2.0/)：架构评审、分阶段改造计划、ADR、选择器契约与验收记录。

仓库未声明项目级许可证（Linux 打包元数据里填的是 MIT，正式授权请以仓库后续补充的 `LICENSE` 为准）。第三方依赖与内嵌字体遵循各自原始许可，字体目录内附 `OFL.txt`。

## 1.1.1 更新

- 侧边栏支持固定展开和悬停预览：点击收起后保留图标栏，在 Logo 上停留 500 毫秒临时展开，移出整体侧栏 250 毫秒后关闭；点击展开控制可恢复固定展开。预览覆盖内容，不改变终端尺寸，菜单、拖拽和键盘操作期间暂缓关闭。
- 主页使用与侧栏一致的毛玻璃底层，服务器列表与分组使用更轻的模糊；设置、隧道卡片及弹窗同步覆盖。关闭毛玻璃恢复纯色背景。
- 修复横向表单按钮受挤压后中文竖排的问题，窄窗口下输入框合理收缩、工具栏与操作行可分行。
- 新增 SSH 主机信任校验、最多五级跳板机、SSH Config 快照导入与本机 Agent 认证，详见下节。

验证：前端类型检查、构建与 40 个测试通过；Go 内部测试、关键模块竞态检查、静态检查及 macOS 打包通过；Windows/Linux SSH 包交叉编译通过。完整桌面跨平台及真实远端联测尚未执行。当前完整 UI2 校验仍存在字体资源预算超限，不能视为全项通过。

### SSH 主机信任、跳板机与 OpenSSH 导入

- 首次手动连接会显示主机类型和 SHA256 指纹，核对后选择“信任并连接”。主机密钥变化会中止连接并显示内置确认弹窗，未经明确授权不会覆盖；撤销密钥始终拒绝。取消不会保存信任。
- “设置 → 安全 → SSH 主机信任管理”可查看、复制指纹和删除应用记录。应用保存的密钥发生合法轮换时，可在内置变化提示中勾选已核对指纹后选择“覆盖当前密钥并连接”；撤销记录不能覆盖；只读来源的旧密钥经明确核对后可通过应用独立信任记录替代，源文件不变。用户 `~/.ssh/known_hosts` 及服务器指定的额外信任源只读。
- 编辑服务器可添加最多 5 级已保存跳板机并调整顺序。各跳独立认证和校验身份；被引用服务器不能直接删除。终端、SFTP、状态采集和三类隧道使用统一连接链，不回退直连。
- 认证新增 SSH Agent 和自动选择。Linux/macOS 使用 `SSH_AUTH_SOCK`，Windows 使用原生 OpenSSH Agent 命名管道，也可设置 `IdentityAgent`。支持查看公钥指纹；不启用 Agent Forwarding。
- 服务器工具栏的“导入 SSH Config”提供预览、跳过、创建副本、覆盖及字段差异。导入生成快照，不回写源文件；请同时选择跳板依赖。SQLite 导入使用事务，JSON 回退使用原子替换。
- 自动模式优先复用 Agent 公钥签名，支持多个 IdentityFile；`IdentitiesOnly` 限定身份。加密身份文件需要密码短语时显示密码输入框，临时参数不写入存储或配置包。
- 数据库自动新增 `ssh_options` 列，旧 ID、分组、密码及私钥字段保留；JSON 和 `.dingpack` 保留新增 SSH 配置。应用信任文件独立存储，不进入配置包。

导入支持 Host 多别名、默认/通配符/否定规则、首值优先、HostName、User、Port、IdentityFile、IdentityAgent、IdentitiesOnly、ProxyJump、HostKeyAlias、UserKnownHostsFile，以及有深度/循环限制的 Include。ProxyJump 当前要求引用同一配置中的明确 Host 别名，暂不支持内联 `user@host:port`。不执行 ProxyCommand、LocalCommand 或任何 shell 指令；不支持的关键字标记为人工检查，Match 整体拒绝。CanonicalizeHostname 等高级配置不宣称兼容。Include 相对路径按当前用户 `~/.ssh` 展开。

未知主机的手动确认采用“中止首次握手 → 显式确认 → 重新连接”，确认前不会发送登录密码。无界面调用及自动重连不批准新信任。Windows/Linux SSH 包已提供平台适配；完整桌面端交互和远端环境仍需在对应系统实测。

### 主机信任与 Windows 标题栏修复

首次信任和密钥变化均使用 ding-ssh 内置弹窗，支持完整指纹展示和复制。后端签发有效期五分钟的一次性请求，确认时重新检查撤销及记录变化；应用自身的普通单主机记录可明确覆盖；只读来源的旧记录通过应用独立信任记录替代，不回写用户 OpenSSH 文件。信任管理支持按 IP、主机名及指纹搜索，每页十条并限制列表高度。

Windows 改为应用自绘标题栏，提供拖动、双击最大化、最小化、还原和关闭按钮；顶部使用与 macOS 应用顶栏相同的主题材质，关闭毛玻璃后为纯色，避免系统原生 Acrylic 标题栏独立显示。系统窗口控制形态仍保留各平台习惯。Windows 运行时交互需在 Windows 实机复核。

终端渲染会按实际背景校验正文颜色，并启用最低 4.5:1 对比度保护，避免浅色主题下白色或亮色输出不可读。该调整作用于显示，不修改远端输出、复制内容或服务器配置。

Windows 自绘标题栏的拖动区与右侧窗口按钮分离；外框不做前端圆角裁切，由系统处理窗口轮廓。浅色终端关闭文字阴影，无背景图/玻璃时采用不透明渲染，Windows 正文字重提高以改善可读性。关闭毛玻璃的浅色界面采用明亮纯色底，避免大片灰色底。

快速连接浮层使用独立明亮底色，避免背景遮罩透过面板造成发暗；首页在空终端页占满内容区域，并支持窄窗口、缩放后的自适应布局和纵向滚动。
