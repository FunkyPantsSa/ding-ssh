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
| **配置与历史存储** | **SQLite**（`modernc.org/sqlite`，纯 Go 无 CGO） | 单文件数据库 `os.UserConfigDir()/ding-ssh/ding-ssh.db`（WAL 模式），表：`servers` / `settings` / `credentials` / `groups`；旧版 JSON 数据在首次启动时一次性自动迁移；已抽象 `store.Store` 等接口，敏感字段加密列入后续阶段 |

### 1.1 存储设计（SQLite）

* **数据库文件**：`os.UserConfigDir()/ding-ssh/ding-ssh.db`（macOS 为 `~/Library/Application Support/ding-ssh/ding-ssh.db`），采用 WAL 日志模式与 `busy_timeout`，兼顾读写并发与稳定性。
* **表结构**：
  * `servers(id, name, grp, host, port, user, auth_type, password, key_path, key_content, bg_image, blur_amount, env_vars)` — 服务器节点，`env_vars` 以 JSON 文本存储；
  * `settings(key, value)` — 应用设置键值表（`logEnabled` / `copyOnSelect` / `theme`）；
  * `credentials(id, name, user, password, auth_type, key_path, key_content)` — 保存的常用凭证（支持密码与私钥两种认证方式）；
  * `groups(name)` — 手动创建的空分组。
* **驱动选型**：`modernc.org/sqlite`（纯 Go 实现，无 CGO，跨平台打包友好）；通过 `database/sql` 访问，`store.Store` / `SettingsStore` / `CredentialStore` / `GroupStore` 接口保持不变，JSON 实现保留为 SQLite 不可用时的兜底。
* **旧数据迁移**：首次启动时若对应表为空且存在旧版 `servers.json` / `settings.json` / `credentials.json` / `groups.json`，自动导入 SQLite；迁移后 JSON 文件保留作备份，不自动删除。
* **后续（Phase 4）**：敏感字段（密码 / 私钥 / 凭证）本地加密存储。

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
+-----------|---------------------|-----------------|---------+
            | Wails IPC           | Wails IPC       | Wails IPC
+-----------|---------------------|-----------------|---------+
|           v                     v                 v         |
|  +------------------+  +-----------------+  +------------+  |
|  | SSH Session Mgr  |  | Fast SFTP Engine|  | SysInfo    |  |
|  | (Zmodem / Shell) |  | (Cache/Parallel)|  | Collector  |  |
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
* **目录同步机制**：
* **Shell -> SFTP**：解析终端输出中的 OSC 7 序列或 Shell Prompt（如通过环境变量设置 `PROMPT_COMMAND` / `PS1` 抛出当前路径），Go 侧捕获 `PWD` 变更并通知 Vue 端 SFTP 自动 `cd`。
* **SFTP -> Shell**：在 SFTP 界面双击文件夹时，后端直接向 Shell 写入 `cd "/path"\n`，实现双向无缝联动。



### 3.2 极速 SFTP 加载引擎

为实现切换/打开目录零等待：

* **内存 LRU 树状缓存**：访问过的目录在 Go 内存中建立 Tree Cache。再次进入时先瞬时呈现缓存内容。
* **后台静默增量校验 (SWR)**：展示缓存的同时，异步发起 `ReadDir` 请求，完成比对后进行 DOM 增量更新。
* **属性并发批处理**：针对海量文件目录，使用 Goroutine 连接池并发读取文件属性，并对图标与类型分析实施延迟懒加载。

### 3.3 rz / sz (Zmodem) 支持

* **协议捕获**：xterm.js 集成 `xterm-addon-zmodem`，实时检测标准输出流中的 Zmodem 逃逸字符序列 (`*  B00000000000000`)。
* **文件传输接管**：捕获握手信号后挂起终端输入，调用 Wails 原生 API 弹出系统文件选择框，由 Go 后端通过二进制管道直接读取/写入，同屏显示全局传输进度条。

### 3.4 动态终端特效（背景模糊与文字阴影）

* **背景图与动态模糊**：
* 在 xterm.js DOM 容器底层叠加绝对定位的 `<img>` 或 CSS 背景图层。
* 采用 CSS 属性 `backdrop-filter: blur(12px) brightness(0.8);` 结合 CSS 变量实现动态模糊与调光。


* **文字阴影与高对比度**：
* 配置 xterm.js 开启 `allowProposedApi: true`，结合自定义 CSS 类名：
```css
.xterm-rows {
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.8), 0 0 5px rgba(0, 0, 0, 0.5);
}

```





### 3.5 智能命令提示与匹配

* **数据收集**：建立三级提示词库：
1. Linux 常用标准命令字典；
2. 节点独立历史执行记录（从本地 SQLite 提取）；
3. 当前 SSH Session 上下文自动提取（文件/路径/已输入命令）。


* **匹配算法**：在前端集成 Trie 树前缀匹配与 FZF 模糊匹配算法，输入时弹出的浮动 Panel 自动高亮匹配项，按 `Tab` 或 `Enter` 完成补全。

### 3.6 一键系统信息分析仪表盘

* **数据采集**：SSH 连接建立后，后端静默打开单独的 `ssh.Session`，并行执行轻量级分析脚本：
```bash
echo "===CPU==="; top -bn1 | head -n 5;
echo "===MEM==="; free -m;
echo "===DISK==="; df -h;
echo "===OS==="; uname -a; cat /etc/os-release | grep PRETTY_NAME

```


* **可视化呈报**：Go 端将文本解析为 JSON 结构体发往前端，Vue 使用 Lightweight Charts 或 SVG 渲染 CPU、内存、磁盘占用率仪表盘及系统基本信息看板。

### 3.7 设置页面与日志开关

* **入口与交互**：顶部标题栏右侧主导航「设置」（与「终端 / 隧道」平级），点击后主区域切换为设置页；标题栏左侧展示应用名「ding-ssh」与当前页面副标题，不再占用左侧空间；设置项修改后即时生效。
* **两级菜单布局**：设置页主区域为左右两栏——左侧一级菜单（通用 / 终端主题 / 保存的凭证），右侧为对应二级内容区，菜单切换不卸载组件、不打断 SSH 会话。
* **设置持久化**：应用设置存储于 SQLite `settings` 键值表（`logEnabled` / `copyOnSelect` / `theme`），与服务器列表同库分表，读写即时生效。
* **日志开关（默认关闭）**：
  * 关闭：应用日志与 Wails 框架日志全部静默。
  * 开启：输出应用运行日志（SSH 连接发起 / 建立 / Shell 就绪 / 断开 / 状态变更 / 错误信息）与 Wails 框架日志（过滤已知 `runtime:ready` 噪音），输出到运行终端，便于排查连接等问题。
* **实时生效**：切换开关后立即写入并生效，无需重启应用。
* **选中即复制**：开启后，终端中选中文本会自动复制到剪贴板（`runtime.ClipboardSetText`）。
* **连接进度**：建立 SSH 连接时，后端按阶段上报进度（初始化 / 握手 / PTY / Shell），前端在标签页内实时展示详细过程，超时或失败时保留最后进度与错误原因。
* **终端主题**：设置页可自定义终端背景色 / 文字 / 光标 / 选中颜色、背景图（文件选择 + 模糊强度）、文字阴影开关与强度，保存后所有标签页即时生效。
* **保存的凭证**：设置页可增删常用用户名密码（`credentials.json` 持久化），新建 / 编辑服务器时可直接选择自动填充。
* **服务器分组**：服务器节点支持分组（`ServerNode.Group`），左侧列表按分组聚合展示，未分组在前，分组可折叠；支持手动新建空分组、重命名（同步更新组内服务器）、删除（组内服务器变为未分组），分组清单持久化于 `groups.json`。

### 3.8 SFTP 面板

* **入口与布局**：会话连接成功后，右侧显示 SFTP 面板（可隐藏 / 重新显示），与终端同屏展示。
* **目录浏览**：目录列表（目录优先）、面包屑导航、上一级、刷新；**单击选中、双击进入目录**，文件双击下载到本地；文件属性展示大小与修改时间；目录切换时的「加载中」提示固定在列表底部，不遮挡浏览。
* **工具栏布局**：面板顶部标题栏右侧为「＋ 新建」（新建文件夹）与「⬆ 上传」操作（带文字标签，与导航区分）；下方独立导航行放置「上一级 / 刷新 / 面包屑」，避免上传按钮与上一级按钮视觉混淆。
* **上传 / 下载**：标题栏「上传」通过系统多选对话框一次选择多个本地文件，逐个上传到当前目录（每个文件独立进度条）；文件行悬停出现「下载」，经系统保存对话框选择本地路径。
* **文件管理（Phase 2 先行落地）**：
  * **右键菜单**：目录行右键提供「进入目录 / 重命名 / 删除」，文件行右键提供「下载到本地 / 重命名 / 删除」。
  * **重命名**：行内联编辑输入新名称，回车确认、Esc 取消，调用 `SftpRename(sessionID, oldPath, newPath)`。
  * **删除**：行内二次确认（「删除…？」确认 / 取消），调用 `SftpRemove(sessionID, path)`；**目录递归删除**（先删子项再删自身），避免非空目录删除失败。
  * **新建文件夹**：标题栏「＋ 新建」在列表顶部展开名称输入行，调用 `SftpMkdir(sessionID, path)` 在当前目录创建。
  * **路径编辑**：点击面包屑区域切换为完整路径输入框，支持手动输入任意远程路径并回车跳转。
* **传输进度与取消**：传输过程中后端通过 `sftp:transfer:{sessionID}` 事件（限频 100ms）上报字节进度，进度条展示在 SFTP 面板最底部；进行中的传输显示「取消」按钮，调用 `SftpCancelTransfer` 取消后上报 `{error: "已取消"}` 事件并清理不完整文件；失败保留错误信息可手动关闭，成功后自动刷新目录。
* **实现**：SFTP 基于 `github.com/pkg/sftp`，在 SSH 会话上按需懒创建 `sftp.Client`（随会话关闭而释放）；流式拷贝（64KB 缓冲）并由 `SftpUpload / SftpDownload` 绑定驱动，失败时清理不完整文件。传输注册到会话级取消表（`Manager` 维护），`SftpCancelTransfer(sessionID, direction, name)` 通过 `context.CancelFunc` 通知传输协程在下一轮拷贝前停止并上报「已取消」。文件管理绑定：`SftpRename / SftpMkdir / SftpRemove`（目录递归删除）、`SelectLocalFiles`（系统多选文件对话框）。
* **后续（Phase 2）**：LRU 目录缓存、终端与 SFTP 目录双向联动、多文件并发传输。

### 3.9 SSH 隧道（本地端口转发）

* **入口与交互**：顶部标题栏右侧主导航新增「隧道」一级入口（与「终端 / 设置」平级），主区域切换为独立隧道管理页；隧道基于已保存的服务器节点建立，无需重复录入认证信息。
* **创建隧道**：选择跳板服务器，填写隧道名称（自动填充）、本地监听端口、远程目标主机与端口，点击「创建并启动」立即生效；本地监听固定绑定 `127.0.0.1`。
* **隧道列表**：展示名称、跳板服务器、转发关系（`127.0.0.1:本地端口 → 远程主机:远程端口`）与状态徽标（运行中 / 已停止 / 异常）；运行中可「停止」，停止/异常可「启动」重启，「删除」停止并移除条目。
* **状态事件**：后端通过 `tunnel:status` 事件上报状态变更（running / stopped / error 及错误信息），前端实时更新列表。
* **实现**：`internal/sshx/tunnel.go` 复用 SSH 连接配置与 10s 超时拨号逻辑；每隧道一个 SSH 连接 + 本地 TCP 监听，连接级双向 `io.Copy` 转发；应用退出（`CloseAll`）时统一停止全部隧道。
* **后续（Phase 2）**：隧道配置持久化、远程端口转发（反向隧道）、动态端口（SOCKS）。

### 3.10 终端交互增强（快捷键与右键菜单）

* **右键菜单**：终端区域右键弹出菜单，提供「复制 / 粘贴 / 全选 / 清除屏幕 / 放大 / 缩小 / 全屏」操作；粘贴读取系统剪贴板通过 `Write` 绑定写入 SSH 会话。
* **快捷键**：
  * `Ctrl+=` / `Ctrl+-`：终端字体缩放，范围 8-32px，缩放后自动 `fit` 重算 PTY 尺寸。
  * `F11`：终端区全屏/退出全屏。
  * `Ctrl+0`：重置字体大小为默认 13px。
  * `Cmd+W`（macOS）/ `Ctrl+W`：关闭当前标签页（仅工作区视图）。
  * `Cmd+1~9`：切换到第 N 个标签页。
* **实现**：`TerminalView.vue` 内通过 `keydown` 事件监听 + `contextmenu` 事件，右键菜单使用 `Teleport` 浮层，点击外部自动关闭。

### 3.11 标签页增强

* **右键菜单**：标签页右键弹出菜单，提供「切换到此标签 / 关闭标签 / 关闭其他标签 / 关闭全部标签」操作。
* **全局快捷键**：`App.vue` 监听全局 `keydown` 事件，`Cmd+W` 关闭当前标签、`Cmd+1~9` 切换标签。
* **实现**：`TabBar.vue` 内 `@contextmenu.prevent` 触发自定义浮层，通过 `Teleport` 渲染到 `body`。

### 3.12 视觉一致性与可维护性

* **SVG 图标系统**：创建 `Icon.vue` 通用组件，内置 terminal / tunnel / settings / folder / file / upload / download / close / plus / key / refresh / up / gear / search 等 14 种 SVG 图标；替换全应用 emoji 图标（导航栏、SFTP 面板、设置菜单、对话框等），消除跨平台渲染差异。
* **加载骨架屏**：`ServerList.vue` 加载中显示 4 行脉冲动画骨架屏，替换空白区域。
* **空状态美化**：终端空状态添加图标 + 描述文字，替代纯文字提示。
* **侧栏宽度可拖拽**：`App.vue` 左侧服务器列表栏添加拖拽手柄，宽度范围 160-400px，拖拽时 `cursor: col-resize`。
* **aria-label 可访问性**：全应用图标按钮补充 `aria-label` 属性。

### 3.13 小细节优化

* **分组折叠持久化**：`ServerList.vue` 的分组折叠状态通过 `localStorage`（键 `sftp-collapsed`）持久化，重新打开应用后恢复。
* **路径编辑**：SFTP 面板点击面包屑区域切换为完整路径输入框，支持手动输入任意远程路径回车跳转。
* **双击操作**：SFTP 文件行双击：目录进入、文件下载到本地。
* **选中高亮**：SFTP 文件行单击选中高亮（`bg-sky-500/15`），与右键菜单联动。

---
---

## 4. 数据结构与接口示例 (Golang)

```go
// 服务器节点定义
type ServerNode struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Group      string            `json:"group"` // 分组名，空为未分组
	Host       string            `json:"host"`
	Port       int               `json:"port"`
	User       string            `json:"user"`
	AuthType   string            `json:"authType"` // password | privateKey
	Password   string            `json:"password,omitempty"`
	KeyPath    string            `json:"keyPath,omitempty"`
	KeyContent string            `json:"keyContent,omitempty"` // 直接粘贴的私钥内容，优先于 KeyPath
	BgImage    string            `json:"bgImage"`
	BlurAmount int               `json:"blurAmount"`
	EnvVars    map[string]string `json:"envVars"`
}

// SSH 连接（TCP 拨号 + 握手）默认 10 秒超时，超时报「SSH 连接超时（10 秒）」；
// 连接过程通过 ssh:progress:{sessionId} 事件逐步上报（初始化/握手/PTY/Shell），前端实时展示。

// SSH 连接过程进度事件
type ProgressEvent struct {
	SessionID string `json:"sessionId"`
	Step      string `json:"step"`
}

// 应用设置（持久化到 settings.json）
type Settings struct {
	LogEnabled   bool  `json:"logEnabled"`   // 是否输出调试日志（默认关闭）
	CopyOnSelect bool  `json:"copyOnSelect"` // 终端选中内容自动复制到剪贴板
	Theme        Theme `json:"theme"`        // 终端主题
}

// 终端主题：颜色、背景图、模糊与文字阴影
type Theme struct {
	Background string `json:"background"` // 终端背景色（hex）
	Foreground string `json:"foreground"` // 文字颜色（hex）
	Cursor     string `json:"cursor"`     // 光标颜色（hex）
	Selection  string `json:"selection"`  // 选中背景色
	BgImage    string `json:"bgImage"`    // 背景图路径，空为不启用
	BlurAmount int    `json:"blurAmount"` // 背景模糊强度(px)
	TextShadow bool   `json:"textShadow"` // 文字阴影开关
	ShadowBlur int    `json:"shadowBlur"` // 阴影强度(px)
}

// 常用凭证，持久化到 credentials.json / SQLite（支持密码与私钥两种认证方式）
type Credential struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	AuthType   string `json:"authType"` // password | privateKey
	KeyPath    string `json:"keyPath,omitempty"`
	KeyContent string `json:"keyContent,omitempty"`
}

// SFTP 远程目录条目
type SFTPEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
}

// SSH 隧道摘要信息（本地端口转发）
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

// SSH 隧道状态事件（事件名 tunnel:status）
type TunnelStatusEvent struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// SFTP 传输进度事件（取消时 Error 为「已取消」，Done 为 true）
type SFTPTransferEvent struct {
	SessionID   string `json:"sessionId"`
	Direction   string `json:"direction"` // upload | download
	Name        string `json:"name"`
	Transferred int64  `json:"transferred"`
	Total       int64  `json:"total"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// 终端与SFTP同步事件
type DirSyncEvent struct {
	SessionID string `json:"sessionId"`
	CurrentPath string `json:"currentPath"`
	Source     string `json:"source"` // "terminal" or "sftp"
}

```

---

## 5. 项目开发阶段规划

1. **Phase 1 (基础框架与 SSH)**：完成 Wails 项目骨架搭建，实现多标签页切换、基础 SSH 终端连接及 xterm.js 渲染；提供设置页（两级菜单：日志开关 / 主题 / 凭证）、服务器分组、右侧 SFTP 目录浏览 / 上传下载（含传输取消）、独立 SSH 隧道页（本地端口转发）与 SQLite 存储（含旧版 JSON 数据迁移），便于开发期排查问题。
2. **Phase 2 (SFTP 与同步)**：实现 SFTP 文件管理（右键菜单、重命名/删除/新建文件夹、路径编辑、多选上传）、高速 LRU 缓存架构及终端与 SFTP 的目录双向联动。
3. **Phase 3 (进阶终端特性)**：集成 rz/sz 协议支持、命令智能匹配框、终端交互增强（快捷键与右键菜单）、终端背景图及 CSS 动态模糊阴影特效。
4. **Phase 4 (运维增强与多端构建)**：开发一键系统信息 Dashboard、全平台打包构建 (Windows EXE, macOS DMG, Linux AppImage) 及性能优化。
