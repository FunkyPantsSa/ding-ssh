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

* **OSC 7 转义序列解析**：在 Go 端读取 PTY 输出流的字节管道中叠加 OSC7 Scanner，识别 `\033]7;file://[hostname]/[path]\007` 模式。
* **Prompt 备选解析**：未开启 OSC 7 时，匹配常见 Shell 提示符（如 `[user@host /path]$`）提取路径。
* **事件驱动**：Go 端提取到绝对路径后，向前端触发 `sftp:sync-path:{sessionID}`，SFTP 面板无感更新到目标目录。

#### B. SFTP -> Shell 联动

* 在 SFTP 面板双击文件夹或点击面包屑跳转时，前端触发 `SendTerminalInput(sessionID, "cd \"" + targetPath + "\"\n")`。

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
  * **历史记录**：从 SQLite `command_history` 表拉取该 ServerNode 下高频执行成功的命令。
  * **屏幕上下文**：提取 `xterm.buffer.active` 当前可视区域中的文件名与路径词汇。
* **交互设计**：监听键盘输入，利用 Trie 树进行前缀筛选，引入 fzf 算法实现模糊匹配。提示框使用 Teleport 定位到光标物理坐标下侧，按 Tab / Enter 补全。

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

### 4.1 静默系统分析看板 (SysInfo Dashboard)

```go
type SysInfoSnapshot struct {
    CPUUsage   float64   `json:"cpuUsage"`
    MemUsedMB  uint64    `json:"memUsedMb"`
    MemTotalMB uint64    `json:"memTotalMb"`
    DiskUsage  []DiskInfo`json:"diskUsage"`
    Uptime     string    `json:"uptime"`
}
```

* **静默通道**：SSH 主连接建立后，在后台打开一个独立的 `ssh.Session`（不分配 PTY）。
* **轻量脚本执行**：每 3 秒循环发送组合分析指令（使用 `LANG=C` 确保输出格式一致）：
  ```bash
  LC_ALL=C top -bn1 | grep "Cpu(s)" ; free -m ; df -h -P
  ```
* **前端呈现**：Go 解析纯文本为 `SysInfoSnapshot` 结构体，推送给前端通过 SVG / Lightweight Charts 渲染 CPU/内存/磁盘动态折线图。

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

通过 GitHub Actions 实现多平台自动化打包，自动化流程如下：

| 目标平台 | 打包产物 | 依赖环境与架构 |
| --- | --- | --- |
| macOS | `.dmg` / `.app` | macOS-latest (Universal Binary: darwin/amd64 + darwin/arm64) |
| Windows | `.exe` (NSIS) / `.zip` | windows-latest (windows/amd64) |
| Linux | `.AppImage` / `.deb` | ubuntu-latest (linux/amd64, 依赖 libgtk-3-dev, libwebkit2gtk-4.0-dev) |

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
    LogEnabled   bool  `json:"logEnabled"`
    CopyOnSelect bool  `json:"copyOnSelect"`
    Theme        Theme `json:"theme"`
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

## 7. 项目开发阶段规划

### Phase 1 ✅ (基础框架与 SSH)
- Wails 项目骨架搭建（前后端分离架构）
- 多标签页切换、基础 SSH 终端连接及 xterm.js 渲染
- 设置页（日志开关 / 主题 / 凭证）
- 服务器分组管理
- 右侧 SFTP 目录浏览 / 上传下载（含传输取消）
- SSH 隧道页（本地端口转发）
- SQLite 存储（含旧版 JSON 数据迁移）

### Phase 2 (SFTP 深度联动与高并发引擎)
- [ ] SFTP 文件管理增强（右键菜单、重命名/删除/新建文件夹、路径编辑、多选上传）
- [x] **已部分完成**：右键菜单、多选上传
- [ ] Shell <-> SFTP 双向目录同步（OSC 7 解析 + Prompt 备选）
- [ ] SWR 目录缓存引擎（sync.Map 内存缓存 + 异步 Revalidate + 增量 Push）
- [ ] 并发传输 Worker Pool（runtime.NumCPU() * 2）
- [ ] 令牌桶限速（golang.org/x/time/rate）

### Phase 3 (终端极客特性与智能补全)
- [ ] rz/sz (Zmodem) 协议全自动接管（xterm-addon-zmodem + 二进制流切换）
- [ ] Trie + FZF 智能命令提示面板（三级词库 + 模糊匹配）
- [ ] GPU 硬件加速渲染（xterm-addon-webgl，自动降级）
- [ ] 命令历史记录存储与查询（SQLite command_history 表）

### Phase 4 (运维 Dashboard 与配置迁移)
- [ ] 静默系统分析看板（SysInfo Dashboard，轻量脚本 + 折线图）
- [ ] SSH 隧道高级模式（Remote Forward / Dynamic Forward SOCKS5）
- [ ] 数据库敏感字段加密（AES-256-GCM + OS Keyring / Argon2id 主密码）
- [ ] 配置导出与迁移（.dingpack 加密打包/导入）
- [ ] 多平台打包构建（Windows EXE, macOS DMG, Linux AppImage）

### Phase 5 (CI/CD 与持续交付)
- [ ] GitHub Actions 多平台自动化构建矩阵
- [ ] 自动化测试集成
- [ ] 发布流程自动化（Release Drafter + 自动上传 artifacts）

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
