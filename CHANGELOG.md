# 变更日志（CHANGELOG）

本项目**尚未发布正式版本**，因此本文件不按版本号归档，而是**按开发顺序倒序**记录已完成的、
可对外说明的变更（最新在最前）。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
分类使用：**新增 / 变更 / 修复 / 文档**。

每条尽量写成「现象 → 原因 → 修法」，便于回溯当时的判断依据。

---

## [未发布] 2026-10-01

### 新增

- **调试模式**：应用内置一个**仅监听本机**（可切换局域网）的 HTTP 控制面 `/v1/*`，供 AI / 自动化调用。
  - 鉴权：每次启动生成随机 token，请求需带 `Authorization: Bearer <token>`（也支持 `?token=`）；
    端口 / token / CDP 端口写在 `%APPDATA%\ding-ssh\debug.json`，便于脚本自动发现。
  - 危险能力各自独立开关：**允许执行 JS**（`/v1/eval`）、**允许读取敏感数据**（服务器列表默认脱敏，不含密码/私钥）。
  - 能力覆盖：状态与设置读写（`/v1/state`、`/v1/settings`）、服务器列表、终端诊断
    （`rows/cols`、缓冲区行数、`baseY/viewportY`、回滚是否可达、可见文本与 DOM 几何）、
    终端读写 / 滚动 / `resize`、会话重连与断开、标签开关、窗口截图（`PrintWindow`）。
  - 实时事件流 `/v1/stream`（SSE，按 topic 订阅）与 `/v1/events`（环形缓冲，免订阅取最近 N 条）。
  - **应用内 MCP**（Streamable HTTP，`/mcp`）：21 个工具 + `app://`、`terminal://` 资源，
    并支持**服务端推送**（`initialize` 返回 `Mcp-Session-Id`，`GET /mcp` 为 SSE 通知通道，
    `DELETE /mcp` 结束会话）。
  - **MCP / 调试 API 的日志查看能力**：新增 5 个工具（`list_logs`、`read_logs`、`export_diagnostics`、
    `clear_logs`、`set_log_options`）、2 个资源（`app://logs`、`app://logs/tail`）与 5 条 `/v1/logs*`
    接口（列表 / 尾部读取 / 清空 / 导出 / 设置），支持按级别与关键字过滤、导出诊断包 zip；
    日志中的 token / 密码 / 私钥在写入前**自动脱敏**，可放心交给 AI 分析。
  - 对应设置页签 **「调试模式」**。
- **MCP stdio 代理 `cmd/ding-ssh-mcp`**：把上面的 HTTP MCP 桥接给只支持 stdio 传输的 MCP 客户端
  （stdin 逐行 JSON-RPC → `POST /mcp`；响应与 SSE 推送写回 stdout，stdout 保持纯 JSON-RPC，日志走 stderr 且 token 打码）。
  地址与 token 按 `-url/-token` → `DING_SSH_DEBUG_URL`/`DING_SSH_DEBUG_TOKEN` → `debug.json` 解析；
  拿到会话后自动挂 `GET /mcp` 接收推送，断线按 1s/2s/5s 退避重连，退出时关闭 SSE 并尽力 `DELETE /mcp`。
- **日志**：应用日志可落盘到 `%APPDATA%\ding-ssh\logs\app-YYYYMMDD.log`，
  单文件 8MB 或跨天轮转为 `app-YYYYMMDD-HHMMSS.log`，目录内只保留最近 20 个（超出删最旧）。
  - 控制台输出与文件输出**两个开关互相独立**，可分别开关；级别可选 `debug/info/warn/error`。
  - **MCP / 调试 API 调用日志**：独立开关，记录调用方法、工具名、状态码与耗时。
  - **日志管理页签**：打开日志文件夹、清理日志（先关句柄再删，清理后继续写入）、
    导出诊断包（`diagnostics-*.zip`，含最近 3 个日志 + `debug.json` + `state.json` + `env.json`）、最近日志预览。
  - **敏感信息自动脱敏**：token / 密码 / 私钥（含 `Authorization: Bearer`、JSON 敏感字段、整段 PEM）在写入前替换为 `***`，固定开启。
  - **按会话跟踪**：终端右键菜单「开始 / 停止跟踪此会话日志」把该会话输出以 `[trace:<id>]` 旁路落盘，便于复现单次连接。
  - 对应设置页签 **「日志」**。

### 修复

- **终端粘贴：Windows 下 `Ctrl+V` 不生效** —— 现象是按下 `Ctrl+V` 后远端报 `-bash: $'\026': command not found`；
  原因是 Wails v2 在 Windows 上固定关闭 WebView2 加速键（`PutAreBrowserAcceleratorKeysEnabled(false)`），
  原生 `paste` 事件不触发，xterm.js 又把 `\x16` 当控制字符发给远端；
  修法是在 `TerminalView.vue` 捕获阶段自行处理剪贴板快捷键（`Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` / `Shift+Insert`、
  有选中时 `Ctrl+Shift+C` / `Cmd+C` 复制，`Ctrl+C` 仍保留 SIGINT），统一走 `term.paste()`，
  并新增「鼠标右键行为」设置（打开选项栏 / 直接粘贴）。
- **登录横幅顶部丢行** —— 现象是连接成功后横幅（`/etc/motd`、fastfetch 等）开头少几行；
  原因是输出监听注册晚于连接建立，首段输出在订阅前就丢了；修法是**先注册输出监听再发起连接**。
- **重连导致横幅开头消失** —— 现象是重连后清屏把刚输出的横幅擦掉；
  原因是并发/隐式重连路径重复执行了 `term.reset()`；修法是加连接重入保护，且隐式重连保留缓冲区（只追加分隔行），
  仅在用户显式重连 / 新会话时才清屏。
- **终端回滚不可达（顶部行看不到且滚不动）** —— 现象是缓冲区明明有历史却无法上滚；
  原因是 `.xterm-viewport` 与内容区没有对齐，滚动区域超出可视范围；修法是给视口上下内缩对齐内容区
  （分屏下用小一些的内缩）。
- **WebGL 与界面缩放错位** —— 现象是 `uiScale ≠ 100%` 时终端顶部缺行且无法上滚，
  但缓冲区与 DOM 几何都正确；原因是 `@xterm/addon-webgl` 在存在 CSS `zoom` 的祖先下会把画布内容整体上移数行，
  `clearTextureAtlas()+refresh()` 也无法纠正；修法是**仅在 `uiScale = 100` 时启用 WebGL，其余自动回退 Canvas**
  （设置里的 WebGL 开关保持可用，切换缩放时自动重新评估）。
- **快速连接面板关闭卡顿** —— 现象是点选服务器后面板「僵一下」再滑走；
  原因是关闭动画与终端创建挤在同一帧；修法是**先收面板、下一帧再创建终端**，
  并把面板改为从左侧导航右边界出入。

### 变更

- 左侧导航的**区段顺序可配置**（导航 / 会话 / 标签页 六种排列），标签页可放在左侧导航或顶部。
- 设置页新增 **「调试模式」** 与 **「日志」** 两个页签。

### 文档

- `README.md` 补充调试模式的 MCP 服务端推送（SSE）与 stdio 代理接入方式，新增「日志（Logs）」一节，
  并同步 MCP 工具清单（21 个，含 5 个日志工具）、`app://logs` 资源与 `/v1/logs*` 接口表，
  以及更新「已知问题」中的 WebGL 与界面缩放说明。

---

## 更早的特性与修复

以下条目按**开发阶段倒序**列出（最新在最前），内容与 `README.md` 的「当前进度」一致。

### 界面与终端交互（2026-09）

- **终端分屏**：左右 / 上下分屏，可拖拽分隔条调整比例，也支持把标签拖到终端区域按落点方向分屏；
  分屏组在标签栏合并为一个条目，可展开子菜单、拖到空白处取消分屏。
- **设计系统重写为 Grid Desk**：统一色板 / 间距 / 层级，左侧导航作为主入口，标签页位置可配置。
- **快速连接入口收敛**：新增快速连接侧边栏，箭头贴边、空闲态降低存在感。
- **「关于」页**：展示版本号、提交号与构建时间（编译期注入）。
- **会话断开会话内容保留可回看**：断开后保留终端画面，顶部横幅提示，可滚屏 / 复制。
- **凭证编辑与查看复制**；标签支持复制（复制当前标签终端）。
- **连接分步日志**：连接过程的实时状态与详细日志，失败时展开面板。
- **本机终端**：支持按平台选择 Shell（PowerShell / cmd / bash 等）。

### Phase 1–5（2026-08 ～ 2026-09）

- **Phase 1**：Wails 前后端分离骨架；服务器节点管理（新增 / 编辑 / 删除 / 搜索，密码与私钥认证）；
  多标签 SSH 终端（PTY 尺寸自适应、输入输出流、连接状态事件与失败重连）；设置页；服务器分组管理；
  右侧 SFTP 面板（浏览 / 导航 / 上传下载 / 进度与取消）；SSH 隧道页（本地端口转发）；SQLite 存储（WAL + 旧版 JSON 自动迁移）。
- **Phase 2**：Shell ↔ SFTP 双向目录同步（OSC 7 解析 + Prompt 备选）；SWR 目录缓存引擎
  （`sync.Map` 内存缓存 + 异步 Revalidate + 增量推送）；并发传输 Worker Pool 与令牌桶限速；SSH 保活与自动重连。
- **Phase 3**：rz/sz（Zmodem）全自动接管；Trie + FZF 智能命令提示；GPU 硬件加速渲染（xterm WebGL，自动降级）；
  命令历史存储与查询。
- **Phase 4**：SysInfo 静默系统分析看板与底部服务器状态栏；SSH 隧道高级模式（Remote Forward / Dynamic SOCKS5）；
  数据库敏感字段加密（AES-256-GCM + OS Keyring）；`.dingpack` 配置导出与导入；命令历史清理与补全热键可配置。
- **Phase 5**：GitHub Actions 多平台构建矩阵与自动化测试（Go `internal` + 前端 typecheck）；
  推送 `v*` 标签自动创建 Release；Windows（NSIS/zip）、macOS（DMG/zip）、Linux（AppImage/deb/tar.gz）打包。
