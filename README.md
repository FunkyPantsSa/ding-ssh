# ding-ssh

基于 **Wails (Go + Vue 3)** 的跨平台 SSH 客户端。设计文档见 [`docs/design.md`](docs/design.md)。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 桌面框架 | Wails v2 (Golang + Webview) |
| 前端 | Vue 3 + Pinia + TailwindCSS + Vite |
| 终端渲染 | @xterm/xterm + @xterm/addon-fit |
| SSH 核心 | golang.org/x/crypto/ssh |
| 配置存储 | JSON 文件（`os.UserConfigDir()/ding-ssh/servers.json`），预留接口可替换 SQLite/BoltDB |

## 当前进度（Phase 1）

- [x] Wails 项目骨架（前后端分离架构）
- [x] 服务器节点管理（新增 / 编辑 / 删除 / 搜索，密码与私钥认证）
- [x] 多标签 SSH 终端（xterm.js 渲染、PTY 尺寸自适应、输入输出流）
- [x] 连接状态事件与失败重连
- [x] 设置页面（日志开关、选中即复制、终端主题、保存的凭证）
- [x] 服务器分组管理与折叠
- [x] 右侧 SFTP 面板（目录浏览 / 导航，`github.com/pkg/sftp`）
- [x] SSH 连接过程进度实时展示（10s 超时提示）
- [ ] Phase 2: SFTP 文件管理、目录双向联动、LRU 缓存
- [ ] Phase 3: rz/sz (Zmodem)、命令智能补全、终端背景特效
- [ ] Phase 4: 系统信息仪表盘、多平台打包

## 目录结构

```
ding-ssh/
├── main.go                    # 应用入口与 Wails 配置
├── app.go                     # Wails 绑定 API（服务器管理 / SSH 会话）
├── internal/
│   ├── models/                # 前后端共享数据结构
│   ├── store/                 # 服务器配置持久化（JSON，含 settings.json）
│   ├── sshx/                  # SSH 会话管理器与终端会话
│   ├── logx/                  # 受日志开关控制的应用日志
│   └── logfilter/             # Wails 日志过滤（噪音 + 开关）
└── frontend/
    ├── src/
    │   ├── components/        # ServerList / ServerDialog / TabBar / TerminalView
    │   ├── stores/            # Pinia: servers / sessions
    │   ├── services/          # Wails 绑定与事件封装（ssh / settings）
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

绑定方法（`app.go`）：

- 服务器：`GetServers` / `SaveServer` / `DeleteServer` / `SelectKeyFile`
- 会话：`Connect` / `Disconnect` / `Write` / `Resize` / `ListSessions`

事件（按会话粒度，避免多标签互相干扰）：

- `ssh:output:{sessionId}` — 终端输出，`data` 为 base64 编码字节流
- `ssh:status:{sessionId}` — 会话状态（connected / closed / error）
