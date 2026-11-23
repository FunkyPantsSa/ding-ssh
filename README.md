简体中文 | [English](README.en.md)

<div align="center">
  <img src="frontend/icon/ding.png" width="280" alt="ding-ssh：猫咪终端应用图标">
  <h1>ding-ssh</h1>
  <p><strong>把 SSH 终端、SFTP、端口转发与本机 shell 装进同一个桌面工作台，并给它们一个可被脚本和 AI 驱动的本机控制面。</strong></p>
</div>

[![CI](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml)

ding-ssh 是一个本地优先的桌面 SSH 工作台：终端是中心，SFTP、隧道、本机 shell 与分屏共用同一套标签体系；设置里打开调试模式后，应用还会在回环地址上提供一个 HTTP + SSE + MCP 控制面，让脚本和 AI 客户端能够读取状态、操作终端、执行任务和修改文件。

| | |
| --- | --- |
| 运行平台 | Windows x64 · macOS（Apple Silicon / Intel 独立包）· Linux x64 |
| 技术栈 | Go 1.26 + Wails v3.1 · Vue 3.5 + Pinia 4 + Tailwind v4 + Vite 7 + xterm.js 6 |
| 数据落地 | 本机 SQLite（WAL）+ 系统钥匙串；无账号、无云同步、无遥测 |
| 控制面 | 默认关闭，仅本机监听；当前 `dev` 提供 **124 个 MCP 工具**（以 `tools/list` / `GET /v1/tools` 为准） |
| 分支与版本 | `dev` 持续开发，`main` 为合并线；已发布 1.2.1，未发布改动见 [`CHANGELOG.md`](CHANGELOG.md) |
| 开源许可 | [MIT 许可证](LICENSE) |
| 相关文档 | [MCP 任务与文件接口](docs/MCP任务与文件接口.md) · [MCP 改造实施与验证记录](docs/MCP改造实施与验证记录.md) · [日志规范](docs/logging.md) |

设置 → 通用 → 语言支持简体中文和 English，默认中文，选择保存在本机。切换语言原地更新界面，不重载页面、不关闭终端会话；应用界面、说明、连接与传输提示及原生文件选择标题均提供英文，用户内容和远端终端输出保持原样。

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
- [十三、界面与设计系统](#十三界面与设计系统)
- [十四、更多文档与许可](#十四更多文档与许可)

---

## 一、它解决什么问题

日常运维常在这几件事之间来回切窗口：连服务器、改配置、传文件、开隧道、看负载。ding-ssh 的判断是——**这些都该在同一个终端窗口里完成**，而且这个窗口应该能被自动化，同时不把本机安全边界交给调用方自觉：

1. **本地优先**。服务器清单、凭据、命令历史、设置全部落在本机配置目录，敏感字段用 AES-256-GCM 加密，密钥交给系统钥匙串或你自己设的主密码。没有账号体系，也没有服务端。
2. **终端是中心**。SFTP 面板与 shell 的当前目录双向同步，隧道、本机 shell、分屏都复用同一套标签体系，而不是各开一个对话框。
3. **可被观察、可被驱动，但授权是明确的**。设置里打开「调试模式」后，应用多出一个本机 HTTP 控制面与 MCP 端点：读布局、读终端缓冲、发命令、等输出、执行独立任务、修改远端文件、导出诊断包。读取、连接、执行、配置、凭据和界面分别授权；每客户端可签发独立 token 限定目标与路径；配置、文件与生命周期变更还要经过**独立于 WebView 的系统原生审批**，批准后拿到的执行凭据一次性、绑定完整参数、120 秒过期。

它**不是**：跳板机 / 堡垒机（没有服务端审计）、配置管理工具（不批量下发）、也不是通用 SSH 代理（隧道只服务本机或指定对端）。调试控制面也不是公网远程接入方案——没有 TLS / OAuth，远程使用请通过 SSH 端口转发。

## 二、能力总览

| 模块 | 你能做什么 | 实现要点 |
| --- | --- | --- |
| **终端** | 多标签会话；左右 / 上下分屏，分隔条可拖；把标签拖到终端区域按落点方向分屏；终端内搜索 | xterm.js + WebGL（已修正 CSS `zoom` 下的画布位图，任意缩放档位可用；不支持或上下文丢失时降级 Canvas）；5000 行回滚；`Ctrl/Cmd+F` 搜索当前缓冲区 |
| **连接** | 密码、私钥、SSH Agent 或自动选择认证；连接进度；断线重连；主机指纹信任与变更拦截 | 五步进度（DNS/直连 → TCP → 鉴权 → 分配 PTY → 就绪）带逐步日志；15s 心跳；2/5/10/20/30s 退避重连，重连不清屏 |
| **分组与检索** | 文件夹式多层分组（如 `生产环境/华东/杭州`）；父组选中含全部子孙；拖服务器入组；搜索与面包屑 | 路径即身份；重命名级联；启动时幂等规范化旧分组 |
| **跳板与堡垒机** | 最多 5 级已保存跳板机；JumpServer 堡垒机模式 | 各跳独立认证；终端、SFTP、状态采集和三类隧道共用连接链，不回退直连 |
| **SSH Config 导入** | 预览、跳过、创建副本、覆盖及字段差异 | 生成只读快照，不回写源文件；支持常用 Host 规则与 Include，不执行 `ProxyCommand` / `LocalCommand` 等 shell 指令 |
| **SFTP** | 浏览、上传（拖拽 / 多选）、下载、重命名、删除、新建目录、路径直编、目录下载；底部进度条可取消 | 目录读取走 SWR 缓存（`sync.Map` + 异步 revalidate + 增量推送）；传输并发 = CPU 核心 × 2；令牌桶限速（默认不限速）；rz/sz（Zmodem）可接管 |
| **目录联动** | shell 里 `cd` 同步到 SFTP 面板；SFTP 跳转同步回终端 | 优先解析 OSC 7，拿不到时用 prompt 兜底；两个方向可分别开关 |
| **隧道** | 本地转发 / 远程转发 / 动态 SOCKS5 / 反向 SOCKS5；启动、停止、重启、编辑、删除 | 状态变更通过事件回传界面；隧道健康可由 MCP 读取 |
| **本机终端** | 不开 SSH 也能开本机 shell | Windows：PowerShell / cmd；macOS：zsh / bash；Linux：默认 shell（`go-pty`） |
| **补全与命令面板** | 输入时弹出候选，`Tab` / `↑` / `↓` 采纳；本地命令历史参与排序；`Ctrl/Cmd+K` 全局命令面板 | Trie + FZF 风格打分；导航热键可自定义；命令面板聚合最近命令、服务器、操作与导航 |
| **运行状态** | 底部状态栏或右侧面板看远端 CPU / 内存 / 磁盘 / 网络；按会话切换「单次 / 实时」 | 按会话启动采集器；实时刷新默认关闭，空闲时暂停 |
| **外观** | 10 套预设色板 + 自定义 ANSI 16 色、背景图与模糊、字体、明暗跟随系统；缩放 80%–150% | HCT / Tonal Spot 从种子派生语义色；Solid / Soft Glass / Clear Glass 三种材质；标签栏位置与导航区段顺序可换；左右面板宽度可拖 |
| **输出着色** | 终端输出按语义自动着色：关 / 语义 / 强调三档 | 纯前端 `Colorizer`，只注入 SGR；ANSI/OSC/CSI 交互序列和无换行提示符立即原样透传 |
| **双语** | 简体中文 / English 原地切换 | 界面、说明、系统提示与原生文件选择标题；用户内容与远端输出不变 |
| **数据与更新** | `.dingpack` 配置包导出导入、主密码、跨设备搬家；`main` / `dev` 更新通道 | 口令加密配置包（最多 500 台服务器）；SHA-256 校验；可选立即更新或退出时安装 |
| **自动化** | 本机 HTTP API、SSE 实时事件流、MCP（含 stdio 代理） | 能力位 + 客户端范围 + 原生审批 + 持久审计，见[第九节](#九调试模式给-ai-的本机控制面) |

## 三、五分钟上手

### 1. 拿到安装包

- GitHub Release：打 `v*` 标签后自动产出 macOS `.dmg` / `.app.zip`、Windows `setup.exe` / `.zip`、Linux `.AppImage` / `.deb` / `.tar.gz`，附 `SHA256SUMS.txt`。
- CI 产物：`main` / `dev` 每次 push 通过测试后构建三平台，产物名形如 `ding-ssh-dev-windows-amd64-<sha>`；分支构建同时更新 `main-latest` / `dev-latest` 预发布中的 `update.json`。
- macOS 包使用临时签名，未公证：首次打开请在 Finder 里右键 →「打开」。

Windows 安装程序默认安装到 `C:\Program Files\FunkyPantsSa\ding-ssh`，可在目录选择页改到其他位置，并在系统「已安装的应用」中提供卸载入口。应用内更新在目录可写时直接安装，权限不足时单独通过 UAC 请求管理员权限，仅由安装助手替换程序；重启由普通权限助手执行，避免主程序继承管理员权限。取消授权会保留旧版本。配置保存在独立的 `%APPDATA%\ding-ssh` 中。旧版需先手动安装一次包含此更新助手的构建，或以管理员身份运行旧版完成这次更新。

### 2. 首次启动

启用了主密码时会先出现解锁框，未启用则直接进入工作区。在「设置 → 通用」里可以按需打开：

- 开机自启（默认关闭，使用当前用户的 LaunchAgent / Run 注册表项 / XDG autostart，不需要管理员权限）；
- 启动后自动连接上次的服务器；
- 关闭标签页 / 退出应用前确认；
- 平滑过渡动画（默认关闭，遵循系统「减少动态效果」偏好）。

### 3. 加一台服务器

左侧导航 → 会话区 → 新建，填主机 / 端口 / 用户，选择密码、私钥（可粘贴内容或选文件）、SSH Agent 或自动选择。分组支持文件夹式多层嵌套：左栏树形展开、选中父组自动包含子孙服务器、可拖服务器入组、右键新建子组，重命名父组会级联更新。

服务器右键菜单可「复制 IP」「复制配置（生成 `原名.new` 并直接打开编辑）」；工具栏的「导入 SSH Config」提供预览、跳过、创建副本、覆盖与字段差异，导入结果保存为快照，不改写 `~/.ssh/config`。

首次连接会显示主机类型和 SHA256 指纹，核对后选择「信任并连接」。主机密钥变化会中止连接并要求明确确认；撤销记录始终拒绝。应用自己的信任记录独立存储，用户 `~/.ssh/known_hosts` 及服务器指定的额外信任源只读。

### 4. 打开终端

双击服务器即开会话。卡住时展开连接进度，看是哪一步慢（DNS、握手、鉴权、PTY）。断线后会按退避策略自动重连并保留屏幕内容；需要手动重连时右键标签即可。

需要自动填写 `sudo -i` / `sudo -s` 的密码时，在「服务器 → 编辑」开启「sudo 自动填写密码」，保存后重新连接。识别到标准中英文密码提示后，当前终端的光标旁会弹出小框，1 秒后自动提交一次已保存的登录密码；可立即提交、改为手动输入，或按 Esc 取消。密码错误后只提供手动输入，私钥口令不会当作登录密码使用。终端里再次 SSH、`su`、进入 tmux / screen / 容器或多行粘贴后暂停自动填写，重新连接后恢复。此功能独立于调试接口的 `terminal.sudo`，不会开启任何 MCP 能力。

`Cmd/Ctrl+F` 搜索终端输出，包含仍保留的回滚内容；支持区分大小写、上一个/下一个匹配，搜索不会向远端发送按键。

### 5. 传文件 / 开隧道

右侧面板切到 SFTP，路径与终端目录自动联动，拖文件进面板即上传。隧道页新建转发，模式选本地转发、远程转发、动态 SOCKS5 或反向 SOCKS5。

#### 动态 SOCKS5

| 隧道模式 | 在哪配置代理 | 流量出口 |
| --- | --- | --- |
| SOCKS5 -D | 本机应用使用 `127.0.0.1:1080` | SSH 服务器 |
| 反向 SOCKS5 -R | SSH 服务器上的应用使用 `127.0.0.1:1080` | 运行 Ding SSH 的本机 |

在「隧道 → 新建隧道」中选择「反向 SOCKS5」，选中服务器并填写服务器代理端口，默认 `1080`。Ding SSH 从本机连接服务器的 SSH 端口，服务器可以通过内网、VPN 或跳板机访问；服务器须允许远程 TCP 转发（OpenSSH 的 `AllowTcpForwarding remote` / `yes` 及相应 `PermitListen` 策略）。本机负责连接目标，直接使用本机可达的网络。

在服务器上验证代理，并把目标域名交给代理出口解析：

```bash
curl --proxy socks5h://127.0.0.1:1080 https://example.com
```

- **代理无认证。** 服务器上的其他用户、进程和容器也可能使用这个代理访问本机的回环服务、内网和公网。仅对可信服务器启用，用完后停止隧道。
- **限制监听范围。** 应用请求服务器绑定 `127.0.0.1`，并拒绝 SSH 服务报告的非回环来源。OpenSSH 请保持 `GatewayPorts no`，用防火墙阻止外部访问代理端口。
- **按应用启用。** SOCKS5 只支持 TCP CONNECT；全局接管流量时，要为 SSH 对端保留直连路由，避免隧道路由环路。

### 6. 想给 AI 用？

设置 → 调试模式 → 打开总开关。首次启用只开放基础元信息，终端 / 文件 / 界面读取和执行能力默认关闭；按需选择「观察」「运维」「完全信任的开发调试」预设，或逐项勾选能力位。

给单个自动化客户端授权时，在同一页填写客户端名称、服务器 / 会话 ID、读写路径和有效期，创建时会弹出系统原生确认窗口。token 只显示一次；最多 32 个客户端，最多 24 小时有效（界面默认 1 小时），应用重启后全部失效，也可随时撤销。详见[第九节](#九调试模式给-ai-的本机控制面)。

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

- **左侧导航**由三个区段组成（导航 / 会话 / 标签页），顺序可在设置里六选一；折叠状态记在 `localStorage`，宽度可拖并同步给浮层定位。收起后保留图标栏，悬停 Logo 500 毫秒临时展开预览，不改变终端尺寸。
- **标签栏**可放顶栏或左侧导航；标签右键可复制远端 IP、重连或关闭。
- **终端区**支持左右 / 上下分屏与拖拽分屏，分隔条可拖动。
- **右侧面板**宽度可拖，内容按需切换。
- **快速连接**是从右侧贴边收起的侧栏，点选即开会话。
- **命令面板**（`Ctrl/Cmd+K`）是全局入口，`Esc` 关闭。

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
| `Cmd+F` / `Ctrl+F` | 搜索终端输出；Enter / Shift+Enter 跳转，Esc 关闭 | 终端 |
| 自定义热键（默认 `Alt+↓`） | 进入补全候选导航 | 终端 |
| `↑` `↓` `Tab` `Enter` | 候选切换 / 采纳插入 | 补全面板 |

粘贴与复制由终端在捕获阶段自行拦截，绕开 WebView2 的加速键限制；`Ctrl+C` 始终是 SIGINT，不会被抢作复制。鼠标右键可配置为「打开选项栏」或「直接粘贴」。补全导航热键支持形如 `Alt+ArrowDown` / `Ctrl+Shift+Space` 的写法，至少需要一个修饰键，或使用功能键、方向键、`Space` / `Tab` / `Escape` / `Enter`。

## 六、从源码构建

**依赖**

| 需要 | 版本 / 说明 |
| --- | --- |
| Go | 1.26.10+（工具链由 `go.mod` 指定） |
| Node.js | 22.12+（仓库 `.nvmrc` 固定 22.23.4；Vite 7 / Vitest 5 的最低要求） |
| Wails CLI | `v3.1.0` |
| Linux 额外 | `libgtk-3-dev`、`libwebkit2gtk-4.1-dev`（构建加 `-tags gtk3`） |
| macOS | macOS 12+，构建需要 Xcode Command Line Tools |
| Windows 额外 | WebView2 运行时；打包安装器需 NSIS |

**常用命令**

```bash
# 安装 Wails CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
# Linux（GTK3 / WebKitGTK 4.1）
GOFLAGS=-tags=gtk3 go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0

# 开发模式：前端热更新 + Go 实时编译
npm --prefix frontend ci
wails3 dev

# 生产构建（产物在 build/bin/）
wails3 build

# Windows 安装包：安装目录可选，受保护目录更新时由助手请求 UAC 授权
wails3 task package GOOS=windows ARCH=amd64

# 后端测试（Linux 加上 -tags gtk3）
go test ./internal/...

# 前端测试、类型检查与构建
cd frontend && npm ci && npm test && npm run build

# 完整 UI 门禁：类型检查、构建、前端测试、Go 测试与体积预算
bash scripts/verify-ui2.sh
```

本机执行真实 PTY 用例（如 `internal/localterm`）需要允许创建伪终端的完整权限；在受限沙箱里这些用例会以「创建本地 PTY 失败」跳过环境能力，CI 的 Linux runner 不受影响。

Windows 上仓库提供两个辅助入口，只为解决「装完 Go/Node 之前就已打开的终端 PATH 未更新」：

- `scripts/dev/dev-env.ps1`：把 Go（`C:\Program Files\Go\bin`、`%LOCALAPPDATA%\Programs\Go\bin`、项目内 `.tools\go\bin`）、`GOPATH\bin`、Node（`C:\Program Files\nodejs`、`%APPDATA%\npm`）补进当前会话 PATH，必要时设置 `GOPROXY` / `GOSUMDB` 镜像，切回项目根目录并打印版本。需 dot-source：`. .\scripts\dev\dev-env.ps1`。
- `scripts/dev/dev.cmd`：本机策略禁止直接跑 `.ps1`，因此用 `-ExecutionPolicy Bypass` 包一层。不带参数打开已激活环境的项目终端，带参数执行单条命令：`.\scripts\dev\dev.cmd wails3 dev`、`.\scripts\dev\dev.cmd go test ./internal/...`。

**发布用打包脚本**（`scripts/`，由 Release 工作流调用）

| 脚本 | 产物 |
| --- | --- |
| `package-macos.sh` | `ding-ssh-<ver>-macos-<arch>.dmg` / `.app.zip`（`arm64` 为 M 系列，`amd64` 为 Intel） |
| `package-windows.ps1` | `ding-ssh-<ver>-windows-amd64.zip` / `-setup.exe` |
| `package-linux.sh` | `.tar.gz` / `.deb`（nfpm，声明 gtk3 + webkit2gtk-4.1 依赖）/ `.AppImage` |
| `set-version.py` | 写入 macOS 与 Windows 原生产物的版本元数据 |
| `update-manifest.py` | 生成分支更新通道的 `update.json` |
| `debug.ps1` | 调试控制面辅助脚本（端口与 token 自动读 `debug.json`） |

## 七、代码地图

```
.
├── main.go                 Wails 入口：窗口参数、frontend/dist 内嵌、启动/关闭钩子
├── internal/
│   ├── apphost/            Wails 宿主：业务门面、调试控制面、原生审批、客户端策略、任务与文件接口
│   ├── appdir/             配置目录单一入口 + `DING_SSH_DEV_CONFIG_DIR` 隔离
│   ├── models/             前后端共享结构（ServerNode、Settings、DebugSettings、事件负载）
│   ├── sshx/               SSH 会话、独立 exec、SFTP 缓存/安全写入/传输、隧道、OSC 7、SysInfo
│   ├── localterm/          本机 shell（PowerShell / cmd / zsh / bash，走 go-pty）
│   ├── store/              SQLite（WAL）+ JSON 回退、迁移、.dingpack 读写
│   ├── cryptox/            AES-256-GCM 字段加密 + Argon2id 主密码 + 系统钥匙串
│   ├── debugsrv/           控制面内核：HTTP/MCP/SSE、能力位、客户端策略、确认、任务、SFTP/UI
│   ├── logx/               日志落盘、轮转、脱敏、尾部读取
│   ├── i18n/               中英文案与语言切换
│   ├── sshconfig/          OpenSSH Config 解析与导入
│   ├── autostart/          各平台开机自启
│   └── updater/            分支更新通道、下载校验与安装
├── cmd/ding-ssh-mcp/       独立 stdio MCP 代理（给只支持 stdio 的客户端）
├── frontend/src/
│   ├── App.vue             应用组装、顶级状态与导航切换
│   ├── layouts/            AppLayout / AppSidebar / AppTopBar / AppUnlock
│   ├── views/              终端工作区（保持终端实例常驻）与首页
│   ├── components/         领域组件 + `ui/` 基础组件
│   ├── composables/        主题 / 快捷键 / Toast / 设置 / 终端补全逻辑
│   ├── stores/             Pinia：servers / sessions / settings / ui / credentials / groups / transfers
│   ├── services/           对 Wails v3 绑定与事件的薄封装
│   ├── completion/         Trie + FZF 打分 + 热键解析
│   ├── theme/              token、语义色、主题引擎、预设、字体、材质
│   ├── panes/layout.ts     分屏布局计算
│   └── debug/              前端侧 UI 观测桥（布局 / 样式 / store / 合成事件）
├── scripts/                三平台打包、版本写入、调试辅助、UI 门禁、MCP 审计脚本
├── build/                  Wails 平台资源（图标、Info.plist、nfpm、NSIS 模板）
└── docs/                   MCP 设计与接口、日志规范、分组设计、测试与验证记录
```

**前后端契约**：前端通过 Wails Bind 直接调用 `App` 方法（服务器 CRUD、设置读写、SFTP 传输、隧道控制、会话读写、安全与导入导出等），后端通过 `EventsEmit` 把终端输出、会话状态、连接进度、隧道状态推给前端。调试模式下同一份事件还会被旁路投递给 SSE 订阅者与 MCP 推送通道——**界面看到的和 AI 看到的是同一股数据流**，并带递增 `seq` 与缺口标记。

## 八、数据与安全

**文件放在哪**（`os.UserConfigDir()/ding-ssh/`，Windows 即 `%AppData%\ding-ssh\`）

开发 / 自动化可设置绝对路径环境变量 `DING_SSH_DEV_CONFIG_DIR`，让数据库、旧 JSON、日志、UI 快照、`debug.json`、任务记录和本地测试密钥全部落入隔离目录。该模式不读取或创建真实系统钥匙串条目；相对路径会被忽略。生产运行不要设置此变量。

| 文件 | 内容 |
| --- | --- |
| `ding-ssh.db` | SQLite 主库（WAL）：`servers` / `settings` / `credentials` / `groups` / `command_history` |
| `settings.json`、`servers.json`、`groups.json` | 旧版 JSON 存储；首次启动一次性导入 SQLite，之后仅在 SQLite 不可用时回退 |
| `security.json`、`.master.key` | 主密码状态与本地密钥文件（启用主密码后密钥改由 OS Keyring 保管） |
| `logs/` | 运行日志（见[第十节](#十日志与诊断)） |
| `logs/ui-snapshots/` | UI 快照，0600 权限；最多 64 组 / 64 MiB，超过 24 小时的在下次采集时清理 |
| `debug.json` | 调试运行时信息：`url` / `lanUrl` / `port` / `token` / 开关 / PID，权限 `0600`；服务停止即删除，**token 每次启动重新生成** |
| `mcp-jobs/` | 独立执行任务的摘要与有界输出；目录 0700、文件 0600，原子替换，不保存命令正文与环境 |
| `mcp-audit.jsonl` | 可选的持久审计（默认关闭）：只含客户端、工具、动作、时间、结果等固定元数据，不保存参数、输出或错误正文 |

**加密链路**

- 字段级：AES-256-GCM，密文带 `enc1:` 前缀，用于识别与迁移；存量明文敏感字段在启动时自动加密迁移。
- 密钥级：默认交给系统钥匙串（`zalando/go-keyring`）；也可设置主密码，用 Argon2id（3 轮、64 MiB、4 线程）从口令派生 32 字节密钥。解锁前，需要密钥的操作会被 `ensureUnlocked()` 拦住。
- 配置包：`.dingpack` 为口令加密的 JSON 载荷（magic `DINGPACK1`），含服务器、凭据、分组，服务器上限 500 条。

**主机信任**

首次连接需明确确认 SHA256 指纹，主机密钥变化会中止连接；应用独立信任记录与只读 OpenSSH 来源分开处理。撤销记录始终拒绝，合法轮换需用户在可信界面核对后覆盖。无界面调用与自动重连不会批准新信任，确认前不会发送登录密码。

**边界说明**

- 应用不做统计上报，也不要求账号；自动更新检查可在更新面板关闭；在线字体首次使用可能连接字体源。调试端口仅在开启调试模式时监听。
- 日志与审计在写入前自动脱敏 token / 密码 / 私钥（覆盖 `key=value`、`?token=`、JSON 敏感字段、`Authorization: Bearer`、整段 PEM），固定开启、无法关闭。
- 诊断包默认只含安全投影：调试信息、状态快照、环境信息；原始日志、终端内容、认证 token、密码和私钥不进入导出。
- 调试模式的能力位**就地生效**：只有影响监听地址的字段（`enabled` / `port` / `bindLan`）变化才会重启服务，因此勾选能力位不会换端口 / token，也不会踢掉已连接的客户端。
- 局域网监听会扩大暴露面：`bindLan` 打开后同网段都可达（token 仍有效），不用时请关掉；远程使用时优先通过 SSH 转发。

## 九、调试模式：给 AI 的本机控制面

### 开启与连接

默认关闭，关闭时不监听任何端口。开启后在 `127.0.0.1:<port>`（默认 8765，端口填 0 时自动挑空闲端口；可切局域网）暴露控制面。地址与 token 见「设置 → 调试模式 → 运行状态」，或读 `debug.json`。

- 认证只接受 `Authorization: Bearer <token>`；query token 会被拒绝，避免进入浏览器历史、代理日志和截图。
- 执行确认只接受 `X-Ding-Confirm-Token` 请求头，不接受 query。
- 服务会校验 Host 与 Origin，拒绝 DNS rebinding / 跨源浏览器请求；普通 HTTP 并发上限 64、SSE 上限 128，超出返回 429；`/mcp` 请求体上限 8 MiB，请求头上限 64 KiB。
- MCP 工具目录和 HTTP 读接口同样需要 token，不做免鉴权例外。

### 权限模型：能力位、客户端范围与原生审批

首次启用只开放基础元信息。终端、远端文件、界面读取和所有执行能力默认关闭；旧配置保留已明确保存的能力，不自动开启新增能力。设置页提供三个预设，扩大权限需要系统原生窗口批准：

| 预设 | 打开的能力 | 说明 |
| --- | --- | --- |
| **观察** | `terminal.read`、`fs.remote.read`、`ui.inspect` | 只读终端、远端文件和界面，适合排障与演示 |
| **运维** | 观察项 + `session.connect`、`session.disconnect`、`credentials.use`、`terminal.input`、`transfer.cancel`、`config.write`、`fs.remote.write` | 允许运行远端命令与改配置；文件写入仍受白名单和单次审批约束 |
| **完全信任的开发调试** | 在现有能力上增加 `automation.trusted`、`eval`、`ui.write`、`ui.inspect`、`terminal.read` | 允许任意 DOM 交互与页面 JS；不是沙箱，只应给本机可信自动化 |

| 能力位 | 默认 | 作用 |
| --- | --- | --- |
| `read` | 开（不可关） | 安全的基本元信息；不包含终端、文件、界面或敏感字段 |
| `terminal.read` | 关 | 读取已授权会话的终端缓冲区与输出 |
| `fs.remote.read` | 关 | 读取已授权会话的远端目录与文件 |
| `ui.inspect` | 关 | 读布局、DOM、控制台、样式与截图 |
| `session.connect` | 关 | 建立连接、打开标签；使用保存凭据还需 `credentials.use` |
| `session.disconnect` | 关 | 断开或重连会话，可能中断正在运行的作业 |
| `credentials.use` | 关 | 应用内部使用保存的凭据，不能导出明文 |
| `terminal.input` | 关 | 输入终端或独立执行远端命令，可能产生任意副作用 |
| `exec.local` | 关 | 额外允许本机命令执行；属于完全信任能力 |
| `ui.write` | 关 | 界面实验与导航；任意 DOM 交互另需 `automation.trusted` |
| `automation.trusted` | 关 | 完全信任的界面交互，能触发持久化业务变更 |
| `eval` | 关 | 页面上下文执行 JS；另需「允许执行 JS」与 `automation.trusted` |
| `config.write` | 关 | 服务器、隧道与应用设置写入（调试授权只能由用户管理） |
| `secrets.read` | 关 | 显式读取 / 导出明文凭据，与内部使用、写入分别授权 |
| `secrets.write` | 关 | 新增 / 修改 / 删除保存的凭据 |
| `fs.remote.write` | 关 | 远端文件写入；另需非空白名单和客户端路径范围 |
| `transfer.cancel` | 关 | 取消有权操作的传输 |
| `sudo.credential` | 关 | 用保存密码执行 sudo；另需 `credentials.use` 和单次批准 |
| `lifecycle` | 关 | 重载、退出、清日志等，需单次批准 |

**远端写入白名单是硬边界**：留空 = 一律拒绝；命中前缀的绝对路径才允许写入 / 删除（`/srv/app` 放行 `/srv/app` 与 `/srv/app/x`，不放行 `/srv/app-evil`）。宿主还会核验 canonical 路径、拒绝写入最终符号链接，但普通 SFTP 无法完全消除远端路径与替换之间的竞态——强隔离请使用受限远端账号、chroot 或受控代理。

### 客户端授权

设置页可为每个自动化客户端创建独立 token：

- 限定客户端名称、能力集合、服务器 ID、会话 ID、可读路径、可写路径和到期时间；
- 有效期最长 24 小时，界面默认 1 小时；最多 32 个客户端，token 仅在创建时显示一次；
- 授权只保存在内存，应用重启后全部失效，可随时撤销；
- 受限客户端**不能**导出明文凭据、执行本机命令、运行任意 JS 或使用完全信任的界面自动化；
- 服务器与会话分别授权；传输里的 `sessionId` 不代表授权身份；全局能力是上限，客户端 token 不能扩大它；
- 客户端授权只能通过本机设置页创建，MCP 工具目录中没有创建授权的工具。

### 独立审批与执行确认

配置、文件与生命周期等已登记工具先调用 `confirm.prepare`。服务端核对实际目标和完整参数，再弹出**独立于 WebView 的系统原生审批窗口**；未批准不发执行凭据。

```json
{"name":"confirm.prepare","arguments":{"action":"servers.delete","tool":"servers.delete","args":{"id":"srv-1"}}}
```

- 批准后返回 `operationId`、`status: approved`、一次性 `token` 和服务器生成的脱敏摘要；可用 `confirm.status` 查询状态，不会再次返回 token。
- 支持「允许一次」，或十分钟内允许**同一调用方、同一动作与工具、相同完整参数**。
- 执行凭据一次性、绑定客户端身份 / 工具 / 动作 / 参数，120 秒过期；撤销授权或权限变化会使其失效。
- 随后调用目标工具并携带原始参数和 token（MCP 用 `token` 或 `confirm` 字段，HTTP 用 `X-Ding-Confirm-Token`）。
- 任意终端命令按已授予的执行权限持续放行，不逐条弹窗；这等价于把命令执行能力交给调用方，请按目标范围授权。
- MCP 无权通过 `capability.enable`、设置更新、设置重置或 store patch 扩大自身权限；调试开关与客户端授权只能由本机用户操作。

`app.permissions` 可随时查看能力快照、各工具的能力要求和可确认动作清单。

### HTTP 端点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/health` | 运行状态、版本、端口与开关 |
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
| POST | `/v1/eval` | 页面上下文执行 JS（需 `eval` + `automation.trusted` + 「允许执行 JS」） |
| GET | `/v1/screenshot` | 窗口 PNG 截图（**当前仅 Windows**，窗口需可见） |
| GET | `/v1/events?limit=&after=&topics=` | 最近事件（环形缓冲，默认 50 条、上限 300；`after` 按 seq 游标读取） |
| GET | `/v1/stream?topics=ssh.output,ssh.status` | SSE 实时事件流，按 topic 订阅 |
| GET | `/v1/logs` · `/v1/logs/tail` | 日志目录与保留策略；尾部读取（可按级别 / 关键字 / 文件过滤） |
| POST | `/v1/logs/clear` · `/export` · `/options` | 清日志（`lifecycle` + token）/ 导出安全诊断包 / 调日志开关 |
| GET | `/v1/tools` | **工具目录**：`{name, capability[], confirmAction?, needsToken, description, inputSchema, annotations, hasStructuredOutput, outputSchema?}`，与 `tools/list` 同源 |
| GET/POST | `/v1/ui/*` | UI 观测与实验：读类需 `ui.inspect`；写类需 `ui.write`，任意交互另需 `automation.trusted` |
| * | `/mcp` | MCP（Streamable HTTP：POST 请求 / GET 推送 / DELETE 结束） |

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
.\scripts\debug.ps1 screenshot [out.png]    # 窗口截图（Windows）
.\scripts\debug.ps1 mcp tools|resources|call <tool> [json]
```

### MCP 工具面

端点为 `POST http://127.0.0.1:<port>/mcp`。当前共 **124 个工具**，按域组织：

| 域 | 数量 | 代表能力 |
| --- | --- | --- |
| 内置基础 | 27 | 状态、终端读取/输入/滚动/尺寸、标签、服务器列表、设置、日志、截图、事件、权限、审计、`confirm.*` |
| UI 与工作区 | 28 | 布局、DOM、样式、控制台、断言、快照/差异、实验、标签激活、面板、分屏 |
| 应用控制 | 31 | 分组、服务器 CRUD / 复制 / 移动 / 导入导出、连接测试、凭据、隧道、设置、命令、退出 |
| 文件与交互终端 | 17 | `sftp.list/stat/read/download/upload/patch`、原子/条件写、取消传输、`terminal.run/expect/sudo` |
| 任务与运维 | 21 | `exec.start/plan/batch`、`jobs.*`、会话连接/等待、`servers.search`、主机指纹 challenge、指标、进程、隧道健康、systemd 服务 |

新增接口的参数与运行保证见 [MCP 任务与文件接口](docs/MCP任务与文件接口.md)。`terminal.run` 的退出码标记每次随机，返回 `completionKnown`；没有本次退出码时不声明命令完成。

**协议**：支持协商 `2024-11-05`、`2025-03-26`、`2025-06-18`；严格验证 JSON-RPC envelope、`Content-Type`、参数类型与 schema，只对 2025-06-18 输出 `structuredContent/outputSchema`。工具带 `readOnlyHint` / `destructiveHint` / `idempotentHint` 注解（仅提示，不替代服务端授权）。

**会话与事件**：每个 MCP 会话只允许一个活动 SSE 连接（重复连接返回 409）；最多 128 个会话，30 分钟空闲回收；事件带递增 `seq`、`after` 游标与 `gap` 标记，发生缺口应重新读取状态或任务输出，而不是假设事件完整。

**stdio 代理**：支持最多 32 个并行请求和取消；单次请求超时 11 分钟；只在确认请求未被受理时重试，已发送的副作用请求不会自动重放。

### 任务、文件与运维接口

- **独立执行**：`exec.start` 使用现有 SSH 连接另开非 PTY exec 通道，不向用户终端输入，也不改变用户终端的目录与环境。支持 `raw` / `posix` 模式、`cwd/env`、超时与输出预算、幂等键；本机执行必须显式 `local=true` 且另需 `exec.local`。进程内最多 8 个未完成任务、保留 64 个；完成记录保留一小时；每任务最多 1 MiB / 4096 个输出分片。没有退出确认时返回 `unknown`，应用重启后未完成任务标为 `unknown` 且不自动重放。
- **任务查询**：`jobs.get/wait/output/cancel/list`；`jobs.output` 按 `cursor` 分页读取 stdout / stderr，跨 UTF-8 边界请使用 `base64`；`hasMore=false` 只表示当前页已读完，不表示运行中的任务以后不会再有输出。
- **多机计划**：`exec.plan` 冻结 1–8 个已连接 SSH 会话及完整参数，返回 `planId`、`dryRun` 和 120 秒有效期；`exec.batch` 以 1–4 并发提交，每目标返回独立 `job`，同一计划只能提交一次。跨主机操作没有事务回滚承诺。
- **运维观测**：`hosts.metrics`、`processes.list`、`tunnels.health`；Linux 上 `services.status/logs` 使用固定 `systemctl show` 与 journald 读取，`services.restart` 是写操作，需要针对 `sessionId/unit/idempotencyKey` 的原生批准并异步返回任务。不自动 sudo，缺少 procps / systemd / 权限时通过 `available/error` 说明。
- **文件读取**：`sftp.list` 目录分页（每页最多 500）、`sftp.read` / `sftp.download` 分片读取（每片默认 256 KiB、最多 1 MiB，返回 `sha256/nextOffset/truncated`），不读写任意本机路径。
- **文件修改**：`sftp.patch.preview` 只预览精确文本替换（不写远端），`sftp.patch` 提交时必须带预览返回的 `expectedHash` 和批准 token；提交前重新读取并核对整体哈希，通过同目录私有临时文件与 POSIX 原子 rename 替换。`sftp.upload` 使用 base64（解码后最多 1 MiB），`sftp.write` 支持文本或 base64、`append`、`expectedHash`（含 `missing`）。覆盖写先暂存再原子替换，目标已存在且服务器不支持原子替换时拒绝；基础 mode / owner 元数据尽力保留，ACL / xattr 不保证。
- **交互终端**：`terminal.run/expect/sudo` 共用用户终端；同一会话同一时刻只允许一个，第二个调用直接拒绝。`terminal.run` 的退出码标记每次随机，`completionKnown` 为假时不得当作命令已结束。`terminal.sudo` 只从凭据库取密码，经随机临时文件（`/tmp/.ding-sudo-<12hex>`、`chmod 600`）喂给 `sudo -S`，密码不进参数 / 返回值 / 日志 / 审计；收尾走原 shell 与独立 SFTP 双通道清理，并返回 `cleanedUp/cleanupVerified`。
- **面板与传输**：`sftp.syncCwd` 只切换文件面板（需 `ui.write`），不向终端键入 `cd`；`sftp.cancel` 只需 `transfer.cancel`，取消是请求，在下一数据块检查点生效，不保证远端临时文件立即消失。

> **更安全的提权替代**：优先给 AI 配免密 sudo（只放开具体命令，例如 `/etc/sudoers.d/ding-ai` 里 `deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx`），这样 `terminal.run` 就能完成同样的工作，而**不必**打开 `sudo.credential`。该能力只在「机器没配免密、且必须立刻提权」时体现价值。

### MCP 资源与推送

| URI | 类型 | 内容 |
| --- | --- | --- |
| `app://state` · `app://debug/info` · `app://servers` · `app://logs` | JSON | 状态、调试信息、服务器（脱敏）、日志设置 |
| `app://logs/tail` · `terminal://{id}/buffer` · `ui://console` | text | 日志尾部、终端缓冲、前端控制台安全元信息 |
| `ui://layout` · `ui://view` | JSON | 布局骨架、当前 view 与导航状态 |
| `sftp://{sessionId}/cwd` · `transfers://current` | JSON | 会话当前目录摘要（前 20 条）、近期传输 |

资源模板为 `terminal://{id}/buffer` 与 `sftp://{sessionId}/cwd`，支持 `resources/subscribe` / `unsubscribe` 与 `resources/updated` 通知。服务端推送形如 `notifications/message`，`logger` 为 `ding-ssh/<topic>`（如 `ssh.output`、`ssh.status`、`tunnel.status`），每 20 秒发一行 `: ping` 保活；`?topics=` 可在 `initialize` 时指定订阅范围。

```bash
# 1) initialize：用 -D 打印响应头，取回 Mcp-Session-Id
curl -s -D - -X POST http://127.0.0.1:8765/mcp \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'

# 2) 挂上推送通道（-N 关闭缓冲，Ctrl+C 退出）
curl -N -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp

# 3) 结束会话
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp
```

**HTTP 客户端配置**：

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
| `-token` · `DING_SSH_DEBUG_TOKEN` | 调试服务 token（也接受每客户端 `dsc_` token） |
| `-debug-json` | `debug.json` 路径，默认在配置目录下 |
| `-v` / `-quiet` / `-once` / `-h` | 打印转发细节 / 只输出错误 / 仅探测可用性 / 中文用法 |

解析优先级：`-url`/`-token` → 环境变量 → `debug.json`。拿到会话 id 后自动挂 `GET /mcp` 接收推送，断线按 1s / 2s / 5s 退避重连；退出前关闭 SSE 并尽力 `DELETE /mcp`；应用重启换端口或 token 后，代理会在请求失败时重读 `debug.json` 跟上（显式传参时不覆盖）。

### 审计与 UI 观测

- **AI 记录**（设置页签）：每次调用留结构化记录（时间、来源 `mcp|http|ui`、工具名、能力位、耗时、结果、脱敏参数、可否撤销），无论成功、被拒还是失败；环形缓冲保留最近 500 条。设置类改动（`update_settings` / `set_log_options`）提供「撤销」按钮（保留最近 20 条改动前快照）。AI 侧可用 `app.recent_calls` 自查。
- **持久审计**：可选开启，只记录客户端、工具、动作、时间、结果等固定元数据，0600 权限，两个 2 MiB 文件轮转；不保存参数、命令、输出或错误正文。
- **UI 观测**：`ui.layout`（布局骨架 + 非预期重叠）、`ui.query` / `ui.styles` / `ui.tokens` / `ui.hit` / `ui.tree`、`ui.console`、`ui.revision`（判断 DOM 认知是否失效）、`ui.screenshot` / `ui.elementShot`、`ui.snapshot` / `ui.diff` / `ui.assert`。
- **隐式数据保护**：`ui.tree` 的 props 只给字段名、类型和规模；DOM 文本排除隐藏及敏感控件。`ui.console` 只保留时间、级别、来源及参数类型/规模，不导出日志正文、异常消息或调用栈；原生控制台输出保持不变。
- **UI 实验**（需 `ui.write`）：`ui.setToken`、`ui.injectCSS`、`ui.storePatch`、`ui.navigate`、`ui.window`、`ui.reload`、`ui.click` / `ui.type` / `ui.key` / `ui.hover` / `ui.scroll`。写入只作用于当前运行实例（内存态），不写设置、不落库，刷新即还原；但合成 DOM 事件可能触发真实业务操作，因此任意交互还要求 `automation.trusted`。
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
| 导出诊断包 | 生成 `diagnostics-YYYYMMDD-HHMMSS.zip`：安全投影后的 `debug.json`、`state.json`、`env.json`；默认不包含原始日志与终端内容，单个条目失败不影响其余 |

终端右键菜单还有「开始 / 停止跟踪此会话日志」：开启后该会话输出以 `[trace:<id>]` 旁路进日志（去 ANSI、单条超 2000 字符截断），用于复现「某次连接到底发生了什么」；这类细粒度跟踪需要日志级别为 `debug`。

日志规范、级别语义与采样策略见 [`docs/logging.md`](docs/logging.md)。

## 十一、已知限制

**窗口截图仅 Windows**：`/v1/screenshot`、`screenshot`、`ui.screenshot`、`ui.elementShot` 和 `ui.snapshot` 的像素捕获目前通过 Windows GDI 实现；macOS / Linux 会明确返回不支持原因，工具目录也会标记 `platformSupported=false`。本仓库没有宣称跨平台像素截图通过验证。

**WebGL 与界面缩放（已修复）**：缩放通过 `.app-shell { zoom }` 实现，而 `@xterm/addon-webgl` 用含 CSS zoom 的 `devicePixelContentBoxSize` 给 canvas 定尺寸，导致位图与渲染网格不一致（顶部几行看不到、缓冲区已在顶部无法上滚）。修法见 `frontend/src/utils/xterm-webgl-zoom.ts`：zoom ≠ 1 时丢弃观察器上报的含 zoom 尺寸，并在 `fit()` / 缩放切换后自检「位图 == 网格」，不一致就重跑 `handleResize()` 补齐 viewport / uniform / 顶点。现在任意缩放档位都启用 WebGL；补丁打不上且缩放 ≠ 100% 时回退 Canvas。排查看 `[webgl] enabled …` 日志行。


**其他**

- **CDP 不可用**：当前版本通过 `/v1/eval` 提供页面调试，Windows 屏幕截图使用原生窗口捕获。
- **`/v1/terminals/{id}/resize` 只改模拟器尺寸**，不改远端 PTY，仅用于复现布局 / 渲染问题。
- **事件通道满会丢事件**：`/v1/stream`、`GET /mcp`（SSE）与终端自动化共用同一个 Hub 通道，订阅者消费不及时时该订阅者丢事件（Hub 不排队、不阻塞发布方）。事件带 `seq` 与 `gap`，发生缺口应重新读取状态；`terminal.run` / `terminal.expect` 还可能漏掉部分输出，需要完整历史时用 `read_terminal` 读缓冲。
- **`ui.reload` 会重挂整个前端**：终端重连、DOM 状态与快照指纹全部失效（`ui.revision` 自增），进行中的 `ui.*` 会话需重新定位元素；要保留现场先 `ui.snapshot`。
- **`ui.*` 依赖已构建的前端**：非 `wails3 dev` 场景需先 `npm run build`（或 `wails3 build`），否则页面里没有调试桥，`ui.*` 会返回「未知操作」这类中文错误（后端仍正常，只是页面内能力不可用）。
- **`wrapExitMarker` / `terminal.run` 的退出码是 POSIX 语义**：Windows 本地终端（cmd / PowerShell）拿不到退出码，请改用 `read_terminal` 或自行 `echo %ERRORLEVEL%` / `$LASTEXITCODE`。
- **`confirm.prepare` 是同步等待**：当前没有异步审批收件箱，`confirm.status` 只查询批准后的执行凭据状态，不表示业务操作已成功；限时批准精确到完整参数，不提供通配命令授权。
- **远端执行与取消有边界**：关闭 SSH channel 不保证远端整个进程树已终止，没有退出确认就返回 `unknown`；应用重启不保证任务继续运行，也不会自动重放命令。每个 SSH transport 最多 8 个未实际结束的独立通道操作，远端不响应时会占用槽位直到操作结束或连接关闭。
- **文件写入不是文件系统沙箱**：普通 SFTP 无法彻底消除远端其他进程造成的检查/写入竞态；追加写非原子，ACL / xattr 不保证保留。任意 shell 执行也能绕过 SFTP 白名单；强隔离依赖远端账号、chroot 或受控代理。
- **完全信任能力不是沙箱**：`terminal.input`、`exec.local`、`ui.write` + `automation.trusted`、`eval` 都能产生持久化或外部副作用；运维预设允许远端任意命令。只应给本机可信自动化使用。
- **远程接入仍是本机方案**：当前没有 TLS / OAuth，`bindLan` 只扩大明文 HTTP 的暴露面；远程使用请通过 SSH 端口转发。
- **macOS 包使用临时签名，未公证**，首次打开需右键「打开」。
- **局域网监听会扩大暴露面**：`bindLan` 打开后同网段都可达（token 仍有效），不用时请关掉。

## 十二、CI 与发布

| 工作流 | 触发 | 做什么 |
| --- | --- | --- |
| `ci.yml` | push 到 `main` / `master` / `dev`，以及任意 PR | Go `internal` 测试 + 前端 `npm run build`（类型检查与构建）；`main` / `dev` push 额外并行构建三平台、上传 artifact，并把 `update.json` 发布到 `main-latest` / `dev-latest` 预发布（不创建正式 Release） |
| `release.yml` | 推送 `v*` 标签或手动触发 | 先跑测试，再在三平台构建 + 打包，汇总 `SHA256SUMS.txt` 并创建 Release（手动触发只上传 artifact） |

- 分支构建版本号形如 `0.0.0-dev.<sha>`，并通过 `-ldflags` 注入 `BuildChannel` / `BuildCommit`；未发版构建不会误判为正式版。
- 分支更新通道按 commit SHA 识别更新；历史提交的下载包使用独立文件名，避免正在下载时被覆盖。
- 更新下载优先使用 `HTTPS_PROXY` / `HTTP_PROXY`（遵循 `NO_PROXY`），其次读取 macOS / Windows 手动系统代理或 Linux GNOME 手动代理；PAC 自动代理尚不支持。

```bash
git tag v1.2.1
git push origin v1.2.1
```

## 十三、界面与设计系统

- **令牌分层**：`theme/tokens.css` → `semantic.css` → `components.css`，Tailwind v4 只映射语义令牌；主题引擎只写语义值，终端色板独立在 `terminal.css`。
- **动态配色**：Material Color Utilities 的 HCT / Tonal Spot 从用户种子派生主导航、二级导航、工作区、卡片、交互、文字与状态；提供 10 套预设、自定义 ANSI 16 色和 Solid / Soft Glass / Clear Glass 三种材质。
- **无障碍**：核心弹窗、菜单与命令面板具备焦点陷阱、方向键 / Home / End / Esc、焦点恢复与 ARIA 语义，并遵循 `prefers-reduced-motion`。
- **自动化契约**：关键区域使用 `data-ui`，动态项使用业务 id 的 `data-tab-id` / `data-testid`；`ui.layout` 只依赖稳定语义选择器。
- **资源治理**：内置 Geist Sans、Geist Mono + Noto Sans SC 的 WOFF2 离线字体；JetBrains Mono、Fira Code、Maple、Sarasa 在选择后下载并缓存，失败可重试并回退到内置字体。`scripts/verify-ui2.sh` 检查类型、构建、测试与 CSS / JS / dist 体积预算，字体原始许可随应用提供。

## 十四、更多文档与许可

- [`CHANGELOG.md`](CHANGELOG.md)：按版本与开发顺序倒序记录变更（现象 → 原因 → 修法）。
- [`docs/MCP任务与文件接口.md`](docs/MCP任务与文件接口.md)：任务、文件、运维接口的参数、限制与示例。
- [`docs/MCP改造实施与验证记录.md`](docs/MCP改造实施与验证记录.md)：本轮 MCP 改造的实施范围、验证结果与明确边界。
- [`docs/MCP能力设计评审与演进方案.md`](docs/MCP能力设计评审与演进方案.md)：改造前的设计评审与演进判断（历史文档）。
- [`docs/logging.md`](docs/logging.md)：日志级别语义、格式规范、脱敏与采样约定。
- [`docs/分组嵌套设计方案.md`](docs/分组嵌套设计方案.md)：文件夹式分组的路径模型与级联规则。
- [`docs/testing/`](docs/testing/) · [`docs/validation/mcp-2026-11-22.json`](docs/validation/mcp-2026-11-22.json)：测试记录与脱敏的联调计数。
- [`scripts/audit/`](scripts/audit/)：`mcp_security_smoke.py`、`live_smoke.py`、`mcp_sdk_compat.mjs`，用于在隔离配置实例上复跑安全与 MCP 兼容性联调。

本项目采用 [MIT 许可证](LICENSE) 开源。第三方依赖与内嵌字体遵循各自原始许可，字体目录内附 `OFL.txt`，更多字体许可详见 `frontend/public/licenses/fonts/`。
