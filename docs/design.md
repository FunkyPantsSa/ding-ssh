本设计方案基于 **Wails (Golang + Vue 3)** 框架构建，兼具 Go 原生高性能网络处理能力与 Vue 现代化现代前端渲染优势，实现跨平台高性能 SSH 客户端。

---

## 1. 技术栈选型

| 层级 | 技术选型 | 说明 |
| --- | --- | --- |
| **桌面客户端框架** | **Wails v2/v3** | Golang 原生绑定 Webview，无需 Node.js 运行时，打包体积小、内存占用低 |
| **前端 UI 框架** | **Vue 3 + Pinia + TailwindCSS** | 声明式组件开发、状态管理与高自由度 CSS 样式处理 |
| **终端渲染引擎** | **xterm.js** | 包含 WebGL 渲染插件、Fit 插件及 Zmodem 插件 |
| **SSH / SFTP 核心** | `golang.org/x/crypto/ssh`<br>

<br>`pkg/sftp` | Golang 官方与社区高并发 SSH/SFTP 协议实现 |
| **配置与历史存储** | **SQLite**（`modernc.org/sqlite`，纯 Go 无 CGO） | 单文件数据库 `os.UserConfigDir()/ding-ssh/ding-ssh.db`（WAL 模式），表：`servers` / `settings` / `credentials` / `groups` / `command_history`；旧版 JSON 数据在首次启动时一次性自动迁移；已抽象 `store.Store` 等接口 |
| **速率限制** | `golang.org/x/time/rate` | 令牌桶限速，支持全局与单任务 Limiter |
| **密钥派生** | `golang.org/x/crypto/argon2` | Argon2id 主密码密钥派生 |
| **系统密钥环** | 各平台原生 Keyring | macOS Keychain / Windows Credential Manager / Linux Secret Service |

### 1.1 存储设计（SQLite）

* **数据库文件**：`os.UserConfigDir()/ding-ssh/ding-ssh.db`（macOS 为 `~/Library/Application Support/ding-ssh/ding-ssh.db`），采用 WAL 日志模式与 `busy_timeout`，兼顾读写并发与稳定性。
* **表结构**：
  * `servers(id, name, grp, host, port, user, auth_type, password, key_path, key_content, bg_image, blur_amount, env_vars)` — 服务器节点，`env_vars` 以 JSON 文本存储；
  * `settings(key, value)` — 应用设置键值表（`logEnabled` / `copyOnSelect` / `theme`）；
  * `credentials(id, name, user, password, auth_type, key_path, key_content)` — 保存的常用凭证（支持密码与私钥两种认证方式）；
  * `groups(name)` — 手动创建的空分组；
  * `command_history(id, server_id, command, executed_at)` — 命令历史记录表（Phase 3 新增）。
* **驱动选型**：`modernc.org/sqlite`（纯 Go 实现，无 CGO，跨平台打包友好）；通过 `database/sql` 访问，`store.Store` / `SettingsStore` / `CredentialStore` / `GroupStore` 接口保持不变，JSON 实现保留为 SQLite 不可用时的兜底。
* **旧数据迁移**：首次启动时若对应表为空且存在旧版 `servers.json` / `settings.json` / `credentials.json` / `groups.json`，自动导入 SQLite；迁移后 JSON 文件保留作备份，不自动删除。

### 1.2 基础架构加固规范 (Cross-Cutting Concerns)

#### 1.2.1 SSH 链路保活与自动重连 (KeepAlive)

针对 `golang.org/x/crypto/ssh` 默认无心跳机制导致的断开问题：

* **心跳 Ticker**：连接成功后启动后台 Goroutine，每 15 秒向服务端发送心跳包：
  ```go
  func() {
      ticker := time.NewTicker(15 * time.Second)
      defer ticker.Stop()
      for range ticker.C {
          _, _, err := sshClient.SendRequest("keepalive@openssh.com", true, nil)
          if err != nil { /* 触发连接断开事件并通知前端 */ }
      }
  }()
  ```
* **自动重连机制**：断线时上报 `ssh:status:{sessionID}` 为 `disconnected`，前端保留终端上下文并在标签页提供「一键重新连接」按钮。

#### 1.2.2 PTY Window Resize 实时协同

* **尺寸同步**：xterm.js 绑定 FitAddon 后，监听窗口及 Split-Pane 缩放事件 `onResize({cols, rows})`。
* **后端通知**：通过 Wails IPC 调用 `ResizeTerminal(sessionID, cols, rows)`，内部触发 Go 端 `ssh.Session.WindowChange(rows, cols)`，确保 vim、htop 等全屏 CLI 工具渲染正常。

#### 1.2.3 数据库敏感字段加密 (Phase 4 安全落地)

* **算法选择**：采用 AES-256-GCM 算法加密 SQLite 中的 `password` 与 `key_content` 字段。
* **密钥派生与存储**：
  * **默认模式**：应用启动时自动调用 OS Native Keyring（macOS Keychain / Windows Credential Manager / Linux Secret Service）生成并存储 32 字节 Master Key。
  * **主密码模式（可选）**：用户可在设置中开启「启动主密码」，使用 Argon2id 算法从主密码派生加密密钥。

---

## 2. 核心架构设计

项目采用 **前后端分离绑定架构**：Go 负责网络通信、SSH 协议栈管理、SFTP 并发传输及系统信息采集；Vue 负责终端渲染、SFTP 交互界面与视觉特效控制。

```
+-------------------------------------------------------------+
|                      Vue 3 Frontend                         |
|  +------------------+  +-----------------+  +------------+  |
|  | Multi-Tab Terminal|  | SFTP Explorer   |  | Dashboard  |  |
|  | (xterm.js + CSS) |  | (Virtual List)  |  | (SysInfo)  |  |
|  +--------+---------+  +--------+--------+  +-----+------+  |
|           |  cmd history |  OSC 7 sync     |  sys metrics  |
|           |  Zmodem      |  SWR cache      |               |
+-----------|--------------|-----------------|---------------+
|           v              v                 v               |
|  +------------------+  +-----------------+  +------------+  |
|  | SSH Session Mgr  |  | Fast SFTP Engine|  | SysInfo    |  |
|  | (KeepAlive/Zmodem)| | (SWR/Parallel)  |  | Collector  |  |
|  +--------+---------+  +--------+--------+  +-----+------+  |
|           |                     |                 |         |
|           +---------------------+-----------------+         |
|                                 v                           |
|                      golang.org/x/crypto/ssh                |
|                       Golang Backend Core                   |
+-------------------------------------------------------------+

```

---

## 3. 关键特性实现方案

### 3.1 同屏终端与 SFTP 及目录同步

* **布局设计**：采用左右/上下可拖拽 Split-Pane 组件，一侧为 xterm.js 终端，另一侧为 SFTP 文件树。
* **双向同步机制**：

#### A. Shell -> SFTP 路径捕获

* **解析优先级**（避免缓冲区中过期 OSC 7 挡住当前目录）：末行 Prompt 路径 → 回显的 `cd` 绝对路径 → 缓冲区中**最后一次** OSC 7。
* **OSC 7**：识别 `\033]7;file://[hostname]/path\007` 或 ST 终止；hostname 可为空（`file:///path`）；取最后一次匹配而非首次。
* **Prompt**：剥离 CSI/OSC 但保留换行后取末行，匹配 `[user@host /path]$`、`user@host:/path$`、`/path$`（`#/%/$` 提示符）。
* **cd 回退**：ECHO 回显的 `cd /abs/path`（须已换行）作为 Prompt 无法识别时的兜底。
* **配置项**：`Settings.terminalToSftpSync`（默认开启）。关闭后终端 cd 不再驱动 SFTP 面板。
* **事件驱动**：Go 端提取到绝对路径后，向前端触发 `sftp:sync-path:{sessionID}`。监听挂在终端组件（会话存活期间始终存在），写入 `tab.sftpPath`，SFTP 面板 watch 后加载；避免面板 `v-if` 拆装导致漏事件。路径无效时前端静默保持当前列表。
* **排查日志**：设置中开启「输出调试日志」后，后端输出 `[INFO] 目录同步 path= src=prompt|cd|osc7`；浏览器控制台输出 `[sftp-sync]` 收包/写入/切换/失败。

#### B. SFTP -> Shell 联动（受配置项控制）

* 在 SFTP 面板双击文件夹或点击面包屑跳转时，前端触发 `SendTerminalInput(sessionID, "cd \"" + targetPath + "\"\n")`。
* **配置项**：`Settings.sftpToTerminalSync`（默认开启）。关闭后 SFTP 目录变化不再向终端发送 cd 命令。

#### C. SFTP 拖拽上传

* 通过 Wails `DragAndDrop.EnableFileDrop` 开启原生文件拖放；前端 `OnFileDrop` 回调拿到本地文件绝对路径数组。
* 仅在带 `--wails-drop-target: drop` 标识的 SFTP 文件列表区域松开时触发，逐个上传到当前目录，复用 `SftpUpload` 传输通道与进度上报。
* 拖拽悬停时显示虚线高亮覆盖层作为视觉反馈。

### 3.2 SWR (Stale-While-Revalidate) 目录缓存引擎

为解决深层目录加载延迟，设计两级存储：

```go
type DirCacheNode struct {
    Path      string      `json:"path"`
    Entries   []SFTPEntry `json:"entries"`
    UpdatedAt time.Time   `json:"updatedAt"`
}

type SFTPCacheManager struct {
    tree sync.Map // map[string]*DirCacheNode
}
```

* **读取流**：前端请求 `GetSftpDir(path)` → 优先从 `sync.Map` 内存中获取缓存并立即返回前端渲染。
* **异步 Revalidate**：后台启动 Goroutine 执行 `sftpClient.ReadDir(path)`，比对计算 Diff（新增/删除/修改）。
* **增量 Push**：若有变更，更新内存缓存并通过 `sftp:dir-updated:{sessionID}` 补发更新后的列表，前端仅增量刷新 DOM。

### 3.3 并发传输与限速控制

* **Worker Pool**：配置 `runtime.NumCPU() * 2` 的协程池并发处理多文件批量传输。
* **令牌桶限速**：使用 `golang.org/x/time/rate` 模块，设置全局与单任务 Limiter，并在设置页面开放带宽上限（如 10 MB/s）配置。

### 3.4 rz / sz (Zmodem) 协议全自动接管

* **序列侦测**：前端 xterm.js 引入 `xterm-addon-zmodem`，挂载到 Terminal 数据流管道。
* **握手挂起**：当收到 `**\B00000000000000` (ZMODEM Header) 时，挂起 xterm 的键盘输入。
* **二进制流切换**：
  * **sz (下载)**：捕获远端发送的文件名与大小，弹出系统 SaveFileDialog，Go 侧通过二进制管道接收数据流并驱动进度条。
  * **rz (上传)**：弹出系统 OpenFileDialog，选择文件后 Go 端将二进制流喂给 SSH Session。

### 3.5 Trie + FZF 智能命令提示面板

```
+-------------------------------------------------------+
|  xterm.js Cursor Position                             |
|  $ git che|                                           |
|            +---------------------------------------+  |
|            | > git checkout        (Standard Dict) |  |
|            |   git cherry-pick     (History Match) |  |
|            |   git check-ref-format(Screen Buffer) |  |
|            +---------------------------------------+  |
+-------------------------------------------------------+
```

* **三级词库构建**：
  * **静态字典**：内嵌 500+ 常见 Linux/DevOps CLI 命令树。
  * **历史记录**：从 SQLite `command_history` 表拉取该 ServerNode 下高频执行成功的**整行命令**。
  * **屏幕上下文**：提取 `xterm.buffer.active` 当前可视区域中的**路径 / Pod 名 / 主机名**等 token（非整行命令）。
* **交互设计**：监听键盘输入，利用 Trie 树进行前缀筛选，引入 fzf 算法实现模糊匹配。提示框使用 Teleport 定位到光标物理坐标下侧，按 Tab / Enter 补全。面板条数可在设置中配置（3–30，默认 8）。

### 3.6 GPU 硬件加速渲染

* 引入 `xterm-addon-webgl`：在组件 Mount 后加载 WebGL 插件，若系统不支持 WebGL 则自动降级回 Canvas / DOM 渲染，解决极高刷屏率（如 `cat` 大日志）时的 UI 卡顿。

### 3.7 多标签页与连接管理

* **标签管理器**：管理 `map[string]*Session` 全局会话池，每个会话分配唯一 SessionID（UUID v4）。
* **连接生命周期**：`Connect() → ssh:connecting → progress events → ssh:connected → ssh:output:stream → Disconnect() → ssh:closed`。
* **连接进度**：SSH 连接过程分为 5 步（解析地址、TCP 拨号、密钥交换、认证、会话创建），每步完成向前端推送进度事件，超时 10s 触发 `ssh:error`。不同阶段错误给出不同提示（如「认证失败，请检查密码或私钥」、「连接超时，请检查网络」）。
* **断线自动重连**：断线后前端保留标签页上下文，提供「一键重新连接」按钮。

### 3.8 SSH 隧道管理

* **隧道类型**：
  * **本地端口转发 (Local Forward)**：将本地端口流量通过 SSH 转发到远程目标（`-L` 模式）。
  * **远程端口转发 (Remote Forward)**：将远程服务器端口映射回本地（`-R` 模式，Phase 4 实现）。
  * **动态转发 (Dynamic Forward / SOCKS5)**：在本地启动 SOCKS5 代理服务，将所有 TCP 请求通过 SSH Channel 进行动态转发（Phase 4 实现）。
* **隧道生命周期**：基于已保存的 ServerNode 独立建立隧道连接（不依赖终端会话），支持列表展示、停止、重启、删除。
* **状态监控**：隧道长连接 Goroutine 持续监听，状态变更时推送 `tunnel:status` 事件（running / stopped / error）。

---

## 4. 运维 Dashboard 与配置迁移

### 4.1 静默系统分析看板 (SysInfo Dashboard) 与底部状态栏

```go
type SysInfoSnapshot struct {
    CPUUsage   float64    `json:"cpuUsage"`
    MemUsedMB  uint64     `json:"memUsedMb"`
    MemTotalMB uint64     `json:"memTotalMb"`
    DiskUsage  []DiskInfo `json:"diskUsage"`
    NetIfaces  []NetIface `json:"netIfaces"` // Name / IP / RxMbps / TxMbps（采样差分）
    Uptime     string     `json:"uptime"`
}
```

* **静默通道**：SSH 主连接建立后自动启动独立无 PTY Session 采集器（右栏监控与底栏共用）。
* **稳采集脚本**（每 3s，后台降频 10s；优先 `/proc`，避免依赖 `top` 输出格式）：
  ```bash
  cat /proc/stat ; cat /proc/meminfo ; df -k -P
  cat /proc/net/dev ; ip -o -4 addr ; cat /proc/uptime
  ```
* **解析策略**：分段宽松解析，成功字段照常展示，失败字段显示 `—`。
* **右栏 Dashboard**：CPU/内存折线、磁盘用量条。
* **底部状态栏（ServerStatusBar）**：CPU%、内存%、可选磁盘分区使用率、可选网卡（名称+IP+上下行 Mbps）；磁盘/网卡选择按服务器持久化到 localStorage。
* **异常**：非 Linux / 全无输出 → 状态栏提示；部分失败不阻断其余指标。

### 4.1.1 智能补全交互与历史

* **导航模式**：未导航时 Tab/↑↓ 交给终端；自定义热键（默认 Alt+↓）或悬停进入导航后，↑↓ 切换、Tab/Enter 仅插入。
* **历史来源**：本地 SQLite `command_history`（回车时优先从屏上行去 prompt 提取整行，兼容 shell ↑ 召回与 Tab 补全；lineBuf 兜底），**非**远端 `~/.bash_history`。
* **清理**：设置页可一键清空全部本地命令历史（`ClearCommandHistory`）。
* **补全面板**：条数可配置；优先置于光标行上方并按实际 DOM 高度校准，避免遮挡输入行。

### 4.2 SSH 隧道高级模式

在 `internal/sshx/tunnel.go` 基础上扩展：

* **Remote Forward (反向隧道)**：将远程服务器端口映射回本地。
* **Dynamic Forward (SOCKS5 代理)**：在本地启动 SOCKS5 代理服务，将所有 TCP 请求通过 SSH Channel 进行动态转发。

### 4.3 配置导出与迁移 (.dingpack)

* **导出**：生成 JSON 配置文件，并用用户设定的密码加密打包为 `.dingpack`。
* **导入**：支持一键导入，自动合并服务器节点、分组与凭证库，解决多台电脑配置同步问题。

---

## 5. 多平台构建与 CI/CD 规范

### 5.1 CI/CD 构建矩阵 (GitHub Actions)

通过 GitHub Actions 实现多平台自动化打包。工作流：

* **CI**（`.github/workflows/ci.yml`）：`main` / PR 上跑 `go test ./internal/...` 与前端 `vue-tsc` + Vite 构建。
* **Release**（`.github/workflows/release.yml`）：推送 `v*` 标签后先跑同样测试，再在三平台并行 `wails build`，打包后上传 GitHub Release（含 `SHA256SUMS.txt`）。也可 `workflow_dispatch` 仅产出 Actions artifacts，不发版。

| 目标平台 | 打包产物 | 依赖环境与架构 |
| --- | --- | --- |
| macOS | `.dmg` / `.app.zip` | `macos-latest`，`darwin/universal`（amd64 + arm64） |
| Windows | NSIS `setup.exe` / `.zip` | `windows-latest`，`windows/amd64`，WebView2 embed |
| Linux | `.AppImage` / `.deb` / `.tar.gz` | `ubuntu-24.04`，`linux/amd64`，`libgtk-3-dev` + `libwebkit2gtk-4.1-dev`，构建 tag `webkit2_41` |

Linux 运行时依赖 `libgtk-3-0` 与 `libwebkit2gtk-4.1-0`。AppImage 为瘦包，不内嵌 WebKit（系统需已安装上述库）。macOS 默认未签名，用户需右键打开。

本地辅助脚本：`scripts/set-version.py`、`scripts/package-macos.sh`、`scripts/package-linux.sh`、`scripts/package-windows.ps1`。Linux deb 由 `build/linux/nfpm.yaml` 生成。

发版：

```bash
git tag v1.0.0
git push origin v1.0.0
```

---

## 6. 数据结构定义

以下为 `internal/models/models.go` 中定义的前后端共享数据结构的完整集合。

### 6.1 现有数据结构

```go
// 服务器节点
type ServerNode struct {
    ID         string            `json:"id"`
    Name       string            `json:"name"`
    Group      string            `json:"group"`
    Host       string            `json:"host"`
    Port       int               `json:"port"`
    User       string            `json:"user"`
    AuthType   string            `json:"authType"` // password | privateKey
    Password   string            `json:"password,omitempty"`
    KeyPath    string            `json:"keyPath,omitempty"`
    KeyContent string            `json:"keyContent,omitempty"`
    BgImage    string            `json:"bgImage"`
    BlurAmount int               `json:"blurAmount"`
    EnvVars    map[string]string `json:"envVars"`
}

// 连接结果
type ConnectResult struct {
    SessionID string `json:"sessionId"`
    Server    string `json:"server"`
}

// 会话状态与信息
type SessionStatus string
const (
    StatusConnecting SessionStatus = "connecting"
    StatusConnected  SessionStatus = "connected"
    StatusClosed     SessionStatus = "closed"
    StatusError      SessionStatus = "error"
)
type SessionInfo struct {
    SessionID  string        `json:"sessionId"`
    ServerName string        `json:"serverName"`
    Host       string        `json:"host"`
    User       string        `json:"user"`
    Status     SessionStatus `json:"status"`
    CreatedAt  int64         `json:"createdAt"`
}

// 事件结构
type OutputEvent struct { SessionID string `json:"sessionId"`; Data string `json:"data"` }
type StatusEvent struct { SessionID string `json:"sessionId"`; Status SessionStatus `json:"status"`; Message string `json:"message,omitempty"` }
type ProgressEvent struct { SessionID string `json:"sessionId"`; Step string `json:"step"` }

// 设置与主题
type Settings struct {
    LogEnabled            bool   `json:"logEnabled"`
    CopyOnSelect          bool   `json:"copyOnSelect"`
    WebGLEnabled          bool   `json:"webGLEnabled"`
    CompletionEnabled     bool   `json:"completionEnabled"`
    CompletionNavHotkey   string `json:"completionNavHotkey"`
    CompletionPanelLimit  int    `json:"completionPanelLimit"`
    SftpToTerminalSync    bool   `json:"sftpToTerminalSync"`  // 默认开启
    TerminalToSftpSync    bool   `json:"terminalToSftpSync"`  // 默认开启
    UIScale               int    `json:"uiScale"`              // 80–150，默认 100
    Theme                 Theme  `json:"theme"`
}
type Theme struct {
    Background string `json:"background"`
    Foreground string `json:"foreground"`
    Cursor     string `json:"cursor"`
    Selection  string `json:"selection"`
    BgImage    string `json:"bgImage"`
    BlurAmount int    `json:"blurAmount"`
    TextShadow bool   `json:"textShadow"`
    ShadowBlur int    `json:"shadowBlur"`
}

// 凭证与隧道
type Credential struct { /* ... 同 models.go */ }
type TunnelInfo struct {
    ID         string `json:"id"`
    Name       string `json:"name"`
    ServerID   string `json:"serverId"`
    ServerName string `json:"serverName"`
    LocalPort  int    `json:"localPort"`
    RemoteHost string `json:"remoteHost"`
    RemotePort int    `json:"remotePort"`
    Status     string `json:"status"` // running | stopped | error
    Message    string `json:"message,omitempty"`
    StartedAt  int64  `json:"startedAt"`
}
type TunnelStatusEvent struct { ID string `json:"id"`; Status string `json:"status"`; Message string `json:"message,omitempty"` }

// SFTP 相关
type SFTPEntry struct {
    Name    string `json:"name"`
    Path    string `json:"path"`
    IsDir   bool   `json:"isDir"`
    Size    int64  `json:"size"`
    ModTime int64  `json:"modTime"`
}
type SFTPTransferEvent struct {
    SessionID   string `json:"sessionId"`
    Direction   string `json:"direction"` // upload | download
    Name        string `json:"name"`
    Transferred int64  `json:"transferred"`
    Total       int64  `json:"total"`
    Done        bool   `json:"done"`
    Error       string `json:"error,omitempty"`
}
type DirSyncEvent struct {
    SessionID   string `json:"sessionId"`
    CurrentPath string `json:"currentPath"`
    Source      string `json:"source"` // "terminal" or "sftp"
}
```

### 6.2 增量数据结构 (Phase 2-4)

```go
// 命令历史记录（数据库 command_history 表）
type CommandHistory struct {
    ID         int64  `db:"id"`
    ServerID   string `db:"server_id"`
    Command    string `db:"command"`
    ExecutedAt int64  `db:"executed_at"`
}

// 系统信息快照（SysInfo Dashboard）
type SysInfoSnapshot struct {
    CPUUsage   float64    `json:"cpuUsage"`
    MemUsedMB  uint64     `json:"memUsedMb"`
    MemTotalMB uint64     `json:"memTotalMb"`
    DiskUsage  []DiskInfo `json:"diskUsage"`
    Uptime     string     `json:"uptime"`
}

type DiskInfo struct {
    MountPoint string `json:"mountPoint"`
    TotalGB    uint64 `json:"totalGb"`
    UsedGB     uint64 `json:"usedGb"`
    UsagePct   float64`json:"usagePct"`
}

// SFTP 目录缓存节点
type DirCacheNode struct {
    Path      string      `json:"path"`
    Entries   []SFTPEntry `json:"entries"`
    UpdatedAt time.Time   `json:"updatedAt"`
}
```

### 6.3 增量 Wails Bind API 抽象

```go
type AppBridge struct{}

// SFTP & Shell Sync API
func (a *AppBridge) SetSftpPathFromTerminal(sessionID string, path string) error
func (a *AppBridge) SyncSftpToTerminal(sessionID string, path string) error

// Zmodem Flow API
func (a *AppBridge) StartZmodemUpload(sessionID string) error
func (a *AppBridge) StartZmodemDownload(sessionID string, fileName string) error

// System Dashboard API
func (a *AppBridge) StartSysInfoCollector(sessionID string) error
func (a *AppBridge) StopSysInfoCollector(sessionID string) error

// Import / Export API
func (a *AppBridge) ExportConfig(passphrase string) ([]byte, error)
func (a *AppBridge) ImportConfig(data []byte, passphrase string) error
```

---

## 6.5 界面缩放

* **配置项**：`Settings.uiScale`（百分比，范围 80–150，默认 100），在设置页「通用」区以步进按钮调节。
* **实现**：在 `App.vue` 根容器 `.app-shell` 上应用 `style="zoom: uiScale/100"`，利用 CSS `zoom` 整体等比缩放界面（含布局与字号），Wails 的 WebKit / WebView2 均原生支持。
* **目的**：适配不同物理尺寸的屏幕与分辨率，保证显示正常、合理、美观。

---

## 7. 项目开发阶段规划

### Phase 1 ✅ (基础框架与 SSH)
- Wails 项目骨架搭建（前后端分离架构）
- 多标签页切换、基础 SSH 终端连接及 xterm.js 渲染
- 设置页（日志开关 / 主题 / 凭证）
- 服务器分组管理
- 右侧 SFTP 目录浏览 / 上传下载（含传输取消）
- SSH 隧道页（本地端口转发）
- SQLite 存储（含旧版 JSON 数据迁移）

### Phase 2 ✅ (SFTP 深度联动与高并发引擎)
> 进度以代码与 README 为准；产品需求见 [`prd.md`](prd.md)。

- [x] SFTP 文件管理增强（右键菜单、重命名/删除/新建文件夹、路径编辑、多选上传）
- [x] Shell <-> SFTP 双向目录同步（OSC 7 解析 + Prompt 备选）
- [x] SWR 目录缓存引擎（sync.Map 内存缓存 + 异步 Revalidate + 增量 Push）
- [x] 并发传输 Worker Pool（runtime.NumCPU() * 2）
- [x] 令牌桶限速（golang.org/x/time/rate）
- [x] SSH 链路保活与自动重连（KeepAlive Ticker 15s + 一键重新连接）

### Phase 3 ✅ (终端极客特性与智能补全)
- [x] rz/sz (Zmodem) 协议全自动接管（xterm + zmodem.js Sentry + 二进制流切换）
- [x] Trie + FZF 智能命令提示面板（三级词库 + 模糊匹配）
- [x] GPU 硬件加速渲染（xterm-addon-webgl，自动降级）
- [x] 命令历史记录存储与查询（SQLite command_history 表）

### Phase 4 ✅ (运维 Dashboard 与配置迁移)
- [x] 静默系统分析看板（SysInfo Dashboard，轻量脚本 + 折线图）
- [x] 底部服务器状态栏（CPU / MEM / DISK / NET，磁盘与网卡可选）
- [x] SSH 隧道高级模式（Remote Forward / Dynamic Forward SOCKS5）
- [x] 数据库敏感字段加密（AES-256-GCM + OS Keyring / Argon2id 主密码）
- [x] 配置导出与迁移（.dingpack 加密打包/导入）
- [x] 命令历史清理与补全导航热键可配置

### Phase 5 ✅ (CI/CD 与持续交付)
- [x] GitHub Actions 多平台自动化构建矩阵
- [x] 自动化测试集成（Go `internal` 单测 + 前端 typecheck）
- [x] 发布流程自动化（`v*` 标签 → 三平台产物 + GitHub Release + SHA256）
- [x] 多平台打包构建（Windows NSIS/zip，macOS DMG/zip，Linux AppImage/deb/tar.gz）

---

## 8. 新增/变更特性记录

### 8.1 终端主题配置

* **设置页入口**：设置页 → 终端主题（二级菜单）。
* **可配置项**：
  * 背景色（hex 颜色选择器 + 文本输入）
  * 文字颜色（hex 颜色选择器 + 文本输入）
  * 光标颜色（hex 颜色选择器 + 文本输入）
  * 选中背景色（文本输入，支持 `rgba()`）
  * 背景图（文件选择器 + 清除按钮）
  * 背景模糊强度（滑块 0-30px）
  * 文字阴影开关（ToggleSwitch）及阴影强度（滑块 0-10px）
* **交互逻辑**：
  * 修改后需点击「保存主题」按钮，调用 `settings.setTheme()` 更新 Pinia store 并持久化到 SQLite。
  * 所有已打开的 TerminalView 实例通过 `watch` 监听 `settings.theme` 变化，自动调用 `applyTheme()` 刷新 xterm.js 主题。
  * 设置加载完成后（`settings.loaded`）额外触发一次主题应用，确保时序正确。
  * 「恢复默认」按钮重置表单并立即保存到 store。
  * 切换至主题页签时自动从 store 刷新表单，避免 `v-show` 导致表单数据过期。

### 8.2 SVG 图标系统

* **组件位置**：`src/components/Icon.vue`
* **设计规范**：
  * 基于 16x16 viewBox 的手绘 SVG 图标。
  * 统一使用 `stroke="currentColor"`、`fill="none"`、`stroke-width="1.5"`、`stroke-linecap="round"`、`stroke-linejoin="round"` 确保视觉一致性。
  * 通过 `name` prop 选择图标，`size` prop 控制尺寸（默认 16px），`extraClass` prop 传递额外 CSS 类。
* **可用图标列表**：`terminal`、`tunnel`、`settings`、`folder`、`file`、`upload`、`download`、`close`、`plus`、`key`、`refresh`、`up`、`gear`、`search`
* **使用方式**：`<Icon name="terminal" :size="14" class="mr-1" />`

### 8.3 右键菜单交互

* **标签页右键菜单（TabBar）**：
  * 右键点击标签页弹出：切换到此标签、关闭标签、关闭其他标签、关闭全部标签。
  * 菜单通过 `@contextmenu.prevent` 触发，`@click.stop` 阻止冒泡。
  * 点击菜单外部任意位置通过 `window.addEventListener('click', closeTabMenu)` 关闭菜单。
  * 组件 unmount 时 `onBeforeUnmount` 移除事件监听。

* **终端右键菜单（TerminalView）**：
  * 右键点击终端区域弹出：复制、粘贴、全选、清除屏幕、放大/缩小、全屏。
  * 通过 `window.addEventListener('click', closeMenu)` 关闭菜单。
  * 快捷键辅助：`Ctrl+=` 放大、`Ctrl+-` 缩小、`Ctrl+0` 重置字号、`F11` 全屏。
