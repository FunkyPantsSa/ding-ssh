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
| **配置与历史存储** | **SQLite / BoltDB** | 本地加密存储服务器节点信息、凭据、命令历史 |

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

* **入口与交互**：左侧边栏底部导航「设置」，主区域切换为设置页；设置项修改后即时生效。
* **设置持久化**：应用设置独立存储于 `settings.json`（`os.UserConfigDir()/ding-ssh/settings.json`），与服务器列表分离，采用原子写入；后续随存储层统一迁移 SQLite/BoltDB。
* **日志开关（默认关闭）**：
  * 关闭：应用日志与 Wails 框架日志全部静默。
  * 开启：输出应用运行日志（SSH 连接发起 / 建立 / Shell 就绪 / 断开 / 状态变更 / 错误信息）与 Wails 框架日志（过滤已知 `runtime:ready` 噪音），输出到运行终端，便于排查连接等问题。
* **实时生效**：切换开关后立即写入并生效，无需重启应用。
* **选中即复制**：开启后，终端中选中文本会自动复制到剪贴板（`runtime.ClipboardSetText`）。
* **连接进度**：建立 SSH 连接时，后端按阶段上报进度（初始化 / 握手 / PTY / Shell），前端在标签页内实时展示详细过程，超时或失败时保留最后进度与错误原因。
* **终端主题**：设置页可自定义终端背景色 / 文字 / 光标 / 选中颜色、背景图（文件选择 + 模糊强度）、文字阴影开关与强度，保存后所有标签页即时生效。
* **保存的凭证**：设置页可增删常用用户名密码（`credentials.json` 持久化），新建 / 编辑服务器时可直接选择自动填充。
* **服务器分组**：服务器节点支持分组（`ServerNode.Group`），左侧列表按分组聚合展示，未分组在前，分组可折叠。

### 3.8 SFTP 面板（基础版）

* **入口与布局**：会话连接成功后，右侧显示 SFTP 面板（可隐藏 / 重新显示），与终端同屏展示。
* **功能**：目录列表（目录优先）、面包屑导航、上一级、刷新、点击目录进入；文件属性展示大小与修改时间。
* **实现**：SFTP 基于 `github.com/pkg/sftp`，在 SSH 会话上按需懒创建 `sftp.Client`（随会话关闭而释放），通过 `SftpList(sessionID, path)` 绑定读取目录。
* **后续（Phase 2）**：上传 / 下载、进度条、LRU 目录缓存、终端与 SFTP 目录双向联动。

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

// 常用凭证（用户名 + 密码），持久化到 credentials.json
type Credential struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// SFTP 远程目录条目
type SFTPEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
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

1. **Phase 1 (基础框架与 SSH)**：完成 Wails 项目骨架搭建，实现多标签页切换、基础 SSH 终端连接及 xterm.js 渲染；提供设置页（日志开关 / 主题 / 凭证）、服务器分组与右侧 SFTP 目录浏览，便于开发期排查问题。
2. **Phase 2 (SFTP 与同步)**：实现 SFTP 文件管理、高速 LRU 缓存架构及终端与 SFTP 的目录双向联动。
3. **Phase 3 (进阶终端特性)**：集成 rz/sz 协议支持、命令智能匹配框、终端背景图及 CSS 动态模糊阴影特效。
4. **Phase 4 (运维增强与多端构建)**：开发一键系统信息 Dashboard、全平台打包构建 (Windows EXE, macOS DMG, Linux AppImage) 及性能优化。