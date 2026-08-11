# ding-ssh

基于 **Wails (Go + Vue 3)** 的跨平台 SSH 客户端。设计文档见 [`docs/design.md`](docs/design.md)。

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
- [x] 设置页面（日志开关、选中即复制、终端主题、保存的凭证）
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

### Phase 3 📋 (终端极客特性与智能补全)
- [ ] rz/sz (Zmodem) 协议全自动接管
- [ ] Trie + FZF 智能命令提示面板
- [ ] GPU 硬件加速渲染（xterm-addon-webgl，自动降级）
- [ ] 命令历史记录存储与查询

### Phase 4 📋 (运维 Dashboard 与配置迁移)
- [ ] 静默系统分析看板（SysInfo Dashboard）
- [ ] SSH 隧道高级模式（Remote Forward / Dynamic Forward SOCKS5）
- [ ] 数据库敏感字段加密（AES-256-GCM + OS Keyring）
- [ ] 配置导出与迁移（.dingpack 加密打包/导入）

### Phase 5 📋 (CI/CD 与持续交付)
- [ ] GitHub Actions 多平台自动化构建矩阵
- [ ] 自动化测试与发布流程

## 目录结构

```
ding-ssh/
├── main.go                    # 应用入口与 Wails 配置
├── app.go                     # Wails 绑定 API（服务器管理 / SSH 会话）
├── internal/
│   ├── models/                # 前后端共享数据结构
│   ├── store/                 # 持久化存储（SQLite 主实现 + JSON 兜底/迁移）
│   ├── sshx/                  # SSH 会话管理器与终端会话
│   ├── logx/                  # 受日志开关控制的应用日志
│   └── logfilter/             # Wails 日志过滤（噪音 + 开关）
└── frontend/
    ├── src/
    │   ├── components/        # ServerList / ServerDialog / TabBar / TerminalView / TunnelPage / SftpPanel / SettingsPage
    │   ├── stores/            # Pinia: servers / sessions / settings / ui / credentials / groups
    │   ├── services/          # Wails 绑定与事件封装（ssh / settings / credentials / groups）
    │   └── types.ts           # 前端类型定义
    └── wailsjs/               # Wails 自动生成绑定（勿手改）
```

## 开发运行

前置要求：Go 1.22+、Node.js 18+、Wails CLI。

```bash
# 安装 Wails CLI（首次）
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# 启动开发模式（热更新前端，实时编译 Go）
wails dev

# 生产构建
wails build
```

产物位于 `build/bin/`。

## 已知问题

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

## 后端 API 与事件

### 绑定方法（`app.go`）

**服务器管理：**
- `GetServers` / `SaveServer` / `DeleteServer` / `SelectKeyFile`

**SSH 会话：**
- `Connect` / `Disconnect` / `Write` / `Resize` / `ListSessions`

**SFTP：**
- `SftpList` / `SftpUpload` / `SftpDownload` / `SftpCancelTransfer` / `SftpRename` / `SftpMkdir` / `SftpRemove` / `SelectLocalFiles` / `SelectSavePath`

**SSH 隧道：**
- `StartTunnel` / `StopTunnel` / `RestartTunnel` / `RemoveTunnel` / `ListTunnels`

**Phase 2+ 增量 API：**
- `SetSftpPathFromTerminal(sessionID, path)` — Shell → SFTP 目录同步
- `SyncSftpToTerminal(sessionID, path)` — SFTP → Shell 目录同步
- `StartZmodemUpload(sessionID)` / `StartZmodemDownload(sessionID, fileName)` — Zmodem 传输
- `StartSysInfoCollector(sessionID)` / `StopSysInfoCollector(sessionID)` — 系统监控
- `ExportConfig(passphrase)` / `ImportConfig(data, passphrase)` — 配置迁移

### 事件

- `ssh:output:{sessionId}` — 终端输出，`data` 为 base64 编码字节流
- `ssh:status:{sessionId}` — 会话状态（connected / closed / error / disconnected）
- `tunnel:status` — SSH 隧道状态变更（running / stopped / error）
- `sftp:sync-path:{sessionId}` — SFTP 目录同步
- `sftp:dir-updated:{sessionId}` — SWR 缓存增量更新
- `sysinfo:snapshot:{sessionId}` — 系统信息快照
