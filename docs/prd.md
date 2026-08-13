# ding-ssh 产品需求（PRD）v1.0

| 字段 | 内容 |
| --- | --- |
| 文档版本 | 1.2.0 |
| 更新时间 | 2026-08-13 |
| 关联文档 | [`design.md`](design.md)（技术设计）、[`README.md`](../README.md) |
| 说明 | 由原「整体产品规划」与「需求文档」合并精简；进度以代码与 README 为准 |

---

## 1. 定位与用户

**一句话**：本地优先、终端与文件同屏、可扩展隧道与监控的跨平台 SSH 工作台。

| 角色 | 核心诉求 | 频率 |
| --- | --- | --- |
| 后端 / 全栈 | 快速连机、看日志、传文件 | 每日 |
| SRE / 运维 | 多机并行、隧道、资源监控 | 每日高密度 |
| 独立开发者 | 轻量、主题、配置可迁移 | 每周 |

**推荐信息架构**：IDE 三栏（左服务器 / 中终端 / 右 SFTP·看板），与现有实现一致。

```
ding-ssh
├── 工作区（终端）
│   ├── 服务器列表 · 会话标签 · 终端画布
│   ├── 右侧工具栏（SFTP | SysInfo）
│   └── 底部状态栏（CPU / MEM / DISK / NET）
├── 隧道（Local / Remote / Dynamic SOCKS5）
└── 设置（通用 · 主题 · 凭证 · 安全 · 导入导出）
```

---

## 2. 目标与指标

1. 「连机 → 操作 → 传文件 → 转发 → 观察」收敛到单一应用。
2. Phase 3–5 已交付：补全 / WebGL / Zmodem / 加密 / SysInfo / dingpack / 多平台 CI 发布。
3. 当前基线可用于 GitHub Release 分发。

| 指标 | 目标 | 口径 |
| --- | --- | --- |
| 连接成功率 | ≥ 97% | connected / 终态 |
| 连接 P95 | ≤ 3s（局域网） | Progress 五步总时长 |
| 补全采纳率 | ≥ 25% | 采纳 / 展示 |
| 配置导入成功率 | ≥ 95% | 成功 / 尝试 |
| 崩溃率 | ≤ 0.5% 会话 | 异常退出 / 总会话 |

埋点命名：`模块_页面_元素_事件`（可先落本地匿名表）。

---

## 3. 核心场景

| ID | 场景 | 优先级 | 状态 |
| --- | --- | --- | --- |
| S1 | 连接服务器开终端 | P0 | ✅ |
| S2 | 终端与 SFTP 同屏传文件 | P0 | ✅ |
| S3 | 路径双向同步 | P0 | ✅ |
| S4 | 本地端口转发 | P0 | ✅ |
| S5 | 断线一键重连 | P0 | ✅ |
| S6 | 命令智能补全 | P0 | ✅ |
| S7 | rz/sz（Zmodem） | P1 | ✅ |
| S8 | SysInfo 看板 / 底栏 | P1 | ✅ |
| S9 | 导出/导入 dingpack | P1 | ✅ |
| S10 | 主密码 / Keyring 解锁 | P0 | ✅ |
| S11 | CI/CD 多平台发布 | P1 | ✅ |

---

## 4. 功能需求摘要

### 4.1 基线（已有）

| 模块 | 规则要点 |
| --- | --- |
| 服务器 / 会话 | AuthType = password \| privateKey；Port 默认 22；五步进度；超时 10s；语义化错误 |
| SFTP / 同步 | SWR 缓存；OSC7 + Prompt；传输可取消；限速；虚拟列表 |
| 隧道 Local | 独立于终端会话；running / stopped / error |

### 4.2 Phase 3（已有）

| 模块 | 规则要点 |
| --- | --- |
| 智能补全 | 历史频次 > 屏幕上下文 > 静态字典；最多可配置条数；Tab/Enter 采纳；大段粘贴抑制 |
| 命令历史 | 按 server_id；上限约 2000；写库失败静默；设置页可清空本地历史 |
| WebGL | 优先 WebGL，失败降级 Canvas/DOM；可强制关闭 |
| Zmodem | 挂起键盘；sz SaveDialog / rz OpenDialog；失败降级 SFTP；超时恢复 |

### 4.3 Phase 4（已有）

| 模块 | 规则要点 |
| --- | --- |
| SysInfo | 独立无 PTY；约 3s 采集（后台 10s）；底栏摘要 + 右栏折线；磁盘/网卡可选并按服务器记忆 |
| 敏感字段加密 | AES-256-GCM；Keyring 或 Argon2id 主密码；旧明文一次性迁移 |
| dingpack | 密码加密打包；导入同 ID 覆盖询问 |
| 隧道高级 | Remote Forward / Dynamic SOCKS5 |

### 4.4 Phase 5（已有）

| 模块 | 规则要点 |
| --- | --- |
| 多平台打包 | macOS Universal DMG / `.app.zip`；Windows NSIS + zip；Linux AppImage / deb / tar.gz |
| CI/CD | 推送 `v*` 标签触发 GitHub Actions 矩阵构建，测试通过后上传 Release artifacts 与 SHA256 |

技术实现细节见 [`design.md`](design.md)。

---

## 5. 交互与验收

- 布局：顶栏导航；左栏服务器树；中栏 Tab + 终端；右栏 SFTP | SysInfo。
- 侧栏 160–480 可拖拽；右键菜单 Esc / 点击外部关闭。
- 动效参考：标签切换 ~160ms；路径同步脉冲 ~600ms；补全面板 ~120ms。
- 验收：补全面板不严重遮挡输入行；同步反馈可感知；图标线性统一、无 emoji。

### 异常主路径

```
连接中 ──超时/认证失败──> error
       ──成功──> connected ──keepalive 失败──> disconnected ──一键重连──> connecting
Zmodem 失败 ──> 恢复输入 + 建议 SFTP
导入密码错误 ──> 不修改本地数据
```

---

## 6. 非功能

| 项 | 要求 |
| --- | --- |
| 权限 | 单机单用户；导出/清除凭证需二次确认 |
| 性能 | 激活终端 WebGL 目标 60fps；SFTP 虚拟滚动；目录缓存命中快 |
| 安全 | password / key_content AES-256-GCM；日志默认不输出密码 |
| 兼容 | macOS / Windows / Linux；无 CGO SQLite；WebGL 不可用降级 |
| 可维护 | `store` 接口保持；JSON 仅兜底 |

---

## 7. 风险（持续关注）

| 风险 | 预案 |
| --- | --- |
| OSC7/Prompt 路径误解析 | 同步开关 + 手动锁定路径 |
| Zmodem 与 xterm 流冲突 | 超时恢复；降级 SFTP |
| Keyring 平台差异 | 降级主密码并明确提示 |
| SysInfo 非 Linux | 空状态 + 原因；部分字段失败不阻断其余 |

---

## 8. 埋点（可选落地）

| 事件 | 用途 |
| --- | --- |
| `session_workspace_connect_result` | 连接成功率 / 耗时 |
| `sftp_workspace_transfer_result` | 传输完成率 |
| `complete_terminal_panel_accept` | 补全采纳率 |
| `settings_migrate_import_result` | 导入成功率 |
| `sysinfo_workspace_collector_error` | 采集错误分布 |
