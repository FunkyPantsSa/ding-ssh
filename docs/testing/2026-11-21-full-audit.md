# ding-ssh 全量扫描、功能测试与修复报告

日期：2026-11-21（Asia/Shanghai）
后续复查：第二轮新增 5 类问题及当前回归结果见 [第二轮报告](2026-11-21-round2-audit.md)。下文保留第一轮的历史快照。
起始版本：`1510b92`，分支 `dev`。测试对象为该版本、开始审计时已有的未提交 sudo 助手功能，以及本轮修复合并后的工作区。

## 1. 结果与交付内容

本轮记录并修复了 **19 组可复现缺陷**，包含会话生命周期、传输、终端输入、加密存储、配置包和调试接口。修复后已执行全量 Go 测试、race 检测、前端单元与组件测试、类型检查、生产构建，以及运行中应用的真实本机终端冒烟测试。

**功能回归通过；资源体积门禁仍未通过。不能据此宣称“全部质量门禁通过”或“软件不存在其他 bug”。** 本轮没有通过减少离线中文字符覆盖、删除字体选项或放宽阈值来掩盖资源超标。详情见第 8 节。

交付文件：

| 文件 | 内容 |
| --- | --- |
| `docs/testing/2026-11-21-full-audit.md` | 本报告：范围、用例构造、复现、根因、修复与复测结果 |
| `docs/testing/2026-11-21-test-cases.csv` | 859 条测试结果记录，包含 Go 父测试与子测试、前端用例及真实应用检查项 |
| `docs/testing/2026-11-21-summary.json` | 脱敏后的数量、覆盖率、构建及门禁结果 |
| `scripts/audit/live_smoke.py` | 可重复运行的真实应用冒烟脚本 |
| 各模块新增的 `*_test.go`、`*.test.ts` | 与缺陷一起提交的长期回归测试 |

CSV 中的 Go 父测试和子测试分别占行，**859 行不代表 859 个相互独立的顶层场景**。下表采用可核对的不同统计口径，不把父子用例叠加后冒充顶层测试数量。

| 检查 | 实际结果 |
| --- | --- |
| 初始 `go test ./... -count=1` | 通过；说明原有正常路径测试未暴露后续构造出的缺陷 |
| 初始前端测试 | 61 个文件，291 个测试通过 |
| 最终 Go 全量测试，显式开启两个手动协议探针 | **421 个顶层测试通过**；连同子测试共 503 条通过记录，0 失败，0 跳过 |
| 最终前端测试 | **64 个文件，306 个测试通过**，0 失败 |
| 真实运行应用冒烟 | **49 项通过，1 项跳过，0 失败** |
| `go test -race ./internal/... -count=1` | 通过；新增并发场景未再报告 data race |
| `go vet ./...` | 通过 |
| `npm run build` | Vue/TypeScript 类型检查与 Vite 生产构建通过 |
| `wails build -platform darwin/arm64 -o ding-ssh-audit` | macOS ARM64 原生编译、打包和本地自签名通过 |
| Windows AMD64、Linux AMD64 internal 包交叉编译 | 通过，**没有在相应操作系统执行测试二进制** |
| 选定 internal 包语句覆盖率 | 53.2%，范围与限制见第 7 节 |
| `bash scripts/verify-ui2.sh` | 前置构建和测试通过；字体与包体资源门禁失败 |
| `git diff --check` | 通过 |

## 2. 环境、隔离与扫描范围

- 本机：macOS ARM64；Go 1.25.0，Node 22.23.4，npm；Wails 开发应用已经由用户开启调试模式。
- 范围：`internal` 下后端模块、Vue 组件和页面、composables、stores、services、终端工具、调试桥、生成的 Wails 绑定、构建及 CI 脚本。`node_modules`、Go 依赖缓存和生成的 `dist` 不作为业务源码逐项修复对象。
- 方法：源码清单与调用路径检查、静态类型/Go vet、既有用例全量执行、失败路径与并发用例补充、真实 TCP SSH 与 SFTP 协议夹具、运行中应用的 HTTP/MCP → 前端 → 本机 PTY 验证。
- 原工作区已含 sudo 助手等未提交内容。审计保留了这些内容并纳入回归；它们不被计为本轮新发现的 19 组缺陷。最终提交包含这些必要文件，以保持功能与 Wails 绑定一致。
- SSH 主机密钥临时生成，使用 loopback 随机端口；所有协议测试使用合成账号和密码。未连接用户保存的真实服务器。
- SQLite、文件传输、导入导出、密钥元数据测试使用临时目录；系统钥匙串测试使用内存 keyring，未用真实钥匙串进行主密码切换。
- 真实应用测试只创建并关闭脚本自己的本机标签，恢复原页面、设置分区、侧栏和原有活动标签；没有修改真实服务器配置、密码或调试能力开关。
- 调试 token 只在脚本进程内使用。提交的文档与结果不含实际 token、保存的主机信息、私钥、用户密码或完整应用日志。

## 3. 功能覆盖矩阵

“真实应用”指当前运行的 Wails 窗口及其实际前端状态；“协议集成”指本地构造服务端，经真实编解码和网络通道执行；“组件/单元”不能替代原生平台或外部服务器兼容性验证。

| 功能 | 构造与检查 | 测试层级与主要证据 | 结果 |
| --- | --- | --- | --- |
| 应用首页、服务器页、隧道页 | 导航后读取实际前端状态与布局条目 | 真实应用；`live_smoke.py` | 通过 |
| 11 个设置分区 | general、hotkeys、connectionInfo、theme、credentials、security、debug、logs、audit、migrate、about；断言对应分区存在且可见 | 真实应用 | 通过 |
| 本机终端 | 真实 PTY、输入输出、默认尺寸、resize、非法 ID/base64、断开后拒绝输入、重连、CloseAll | `localterm/manager_test.go`；真实应用 | 通过 |
| 本机 shell 快速退出与并发替换 | 临时 shell 启动即退出，重复创建；12 次同 ID 并发替换，核对关闭数量 | 本机 PTY 集成、重复测试 | 修复 B15 后通过 |
| SSH 交互通道 | 合成 SSH 服务端支持 PTY/shell/window-change；Unicode 字节往返；拒绝重复连接；并发连接和关闭；远端 shell 立即退出 | `terminal_integration_test.go`，含 race | 修复 B17、B18 后通过 |
| SSH 网络连通性 | 可达/不可达地址、超时处理 | `connectivity_test.go` | 通过 |
| 多级跳板 | 两级真实 SSH 跳板到目标，关闭后检查上游连接回收 | `transport_test.go` | 通过 |
| 密码、私钥、agent 配置 | 认证参数解析、agent 过滤与真实签名、SSHOptions 持久化 | `agent_test.go`、`ssh_options_test.go`、前端安全服务测试 | 通过；不代表所有外部 agent 实现均已实测 |
| 主机信任 | 未知主机在发送密码前被拒绝、变更密钥、hash/revoked、用户批准与替换、只读 known_hosts 来源 | `hostkeys_test.go`、`trust_challenge_test.go`、`hostTrust.test.ts` | 通过 |
| SSH config 导入 | Host/Include 等解析、跳板依赖预览与导入、失败时不产生部分配置 | `sshconfig/config_test.go`、`apphost/sshimport_test.go` | 通过 |
| 服务器与分组管理 | SQLite/JSON 持久化、嵌套分组、隐式祖先、重命名、删除保留/递归策略、拖动/展开/搜索组件流程 | `store/group*test.go`、`apphost/group_test.go`、ServerList/ServerDialog 测试 | 通过 |
| SFTP 列目录/新建/重命名/删除 | 临时目录下创建中文和重音文件名，列目录检查大小，重命名后递归删除 | 真实 SFTP 协议；`audit_integration_test.go` | 通过 |
| SFTP 文件上传与下载 | 135KB 以上、含 0/127/128/255 字节及中文内容，完整往返比较；不存在源文件与不存在会话 | 真实 SFTP 协议 | 通过 |
| SFTP 文件夹传输 | 多层目录、累计字节数、符号链接跳过、取消、根目录限制相关路径 | `transfer_folder_test.go` | 通过 |
| 覆盖写入安全 | 临时文件提交前原文件不变；完成后内容更新且权限保留；删除 symlink 不删除目标 | `sftp_atomic_test.go` | 通过 |
| SFTP 缓存 | 相同路径不同会话隔离，失效后旧刷新不能复活缓存，目录变更重新读取 | `sftp_cache_test.go` | 通过 |
| 传输进度与取消 | 实时进度、任务 ID 稳定、同名并行任务 ID 不同、只取消指定任务、部分字节取消仍计数、短写及部分写入伴随错误 | `transfer_progress_test.go` | 修复 B19 后通过 |
| 底层传输限速与并发组件 | 64KiB 分片大于 burst、3B/s、500 次并发修改限速、并发池增大/缩小、等待取消、二进制拷贝；这是底层 API 验证，当前 UI 未暴露调节入口 | `transfer_limiter_test.go`，含 race | 修复 B03–B05 后通过 |
| 本地转发 | 实际 HTTP 数据穿过 SSH direct-tcpip；loopback/全网监听、更新与重启保留监听配置 | `tunnel_listen_test.go` | 通过 |
| 动态 SOCKS5 | 标准 SOCKS5 客户端握手，经 SSH 访问临时 HTTP 服务；不支持的 BIND 命令返回失败码 | `audit_integration_test.go` | 修复 B16 后通过 |
| 远程转发 | 合成 sshd 接受 tcpip-forward，再打开真实 forwarded-tcpip 通道；访问本地 HTTP 目标并核对数据 | 同上 | 通过 |
| 反向动态 SOCKS5 | forwarded-tcpip 上做 SOCKS 握手并传 HTTP；拒绝非回环来源 | `tunnel_reverse_test.go` | 通过 |
| 隧道页面管理 | 新建、编辑、停止、重启、删除等前端流程和后端入参校验 | TunnelPage/useTunnelManagement/tunnel_update 测试 | 通过 |
| 系统指标面板 | 配置禁止时不采集、实时开关按会话隔离、刷新、网卡筛选、文件系统分类、top 缺失/单位/最后一帧解析 | `sysinfo_*test.go`、`top_test.go`、SysInfoPanel 等 | 通过；数据解析使用样本和夹具 |
| 终端目录同步 | OSC7/OSC3008、相对 cd 仅消费一次、流式拆包、目录存在性校验、chroot/SFTP 视图降级、JumpServer 隔离 | `osc7_test.go`、`session_dir_test.go`、`jumpserver_test.go` | 通过 |
| 标签、分屏与侧栏 | 真实两终端分屏/恢复、SFTP/指标/传输侧栏切换；组件测试覆盖布局与关闭确认 | 真实应用 + TerminalWorkspace/useWorkspaceLayout 测试 | 通过 |
| 终端字符、粘贴与搜索 | ASCII、Latin-1、中文、emoji、控制字符、全部 256 个原始字节、大段粘贴、搜索快捷键、选区复制保护 | terminal-input 等测试 + 真实 PTY 的十六进制验证 | 修复 B07 后通过 |
| 终端渲染辅助 | 语义着色、缩放、WebGL atlas、字体加载与回退、右键菜单、连接覆盖层 | 前端相关 utils/theme/component 测试 | 通过；不等价于所有 GPU 实机兼容性验证 |
| 历史与补全 | ANSI/方向键/输出噪音/敏感模式过滤、连续去重、中文、LIKE 通配符按字面查找、按服务器隔离、清空、2000 条滚动上限 | `store/audit_flows_test.go`、fzf/补全 composable 测试 | 通过 |
| sudo 助手 | 只响应明确提交的 sudo -i/-s，分块/彩色/中文提示，保存密码只尝试一次，重试手输，过期与一次性令牌，会话隔离，服务器配置变化，禁用嵌套目的地及多行粘贴，控制字符拒绝和错误脱敏 | 既有工作区的 sshx/apphost/composable/popover 测试 | 通过；未连接生产主机运行真实 sudo |
| sudo 临时文件清理 | 路径格式与白名单、无 SFTP 不降级执行任意命令、真实 SFTP Remove/Lstat 幂等复核 | `sudo_cleanup_test.go`、`debugsrv/sudo_test.go` | 通过 |
| 主密码与字段加密 | 开启→重启锁定→错误密码→解锁→更换→关闭→重启后解密；四类字段迁移；启用/更换/关闭失败回滚 | `vault_test.go`、`security_rotation_test.go`、`crypto_migrate_test.go` | 修复 B09–B12 后通过 |
| 配置包 | 服务器/凭证/中文分组完整加密往返、磁盘文件读写、空密码、错误密码、篡改、截断、500 上限和未知版本 | `store/audit_flows_test.go` | 修复 B14 后通过 |
| 更新 | 检查/忽略提醒/切换通道/忙碌态、存储不可用；HTTP 失败、不安全资产、安装计划与 hash 变更、替换失败回滚、保留备份、是否重启 | updates 前端测试、`internal/updater/*test.go` | 通过；未替换用户正在运行的真实应用 |
| 主题、语言与字体 | token 单一来源、语义色、Material 引擎、配置迁移、自定义/远程字体缓存和终端字体、语言字典 | 前端 theme/i18n 测试，后端 i18n 测试 | 功能测试通过；资源预算另列失败 |
| 日志、审计与调试权限 | 日志开关/脱敏/采样、MCP 与 HTTP 工具目录一致、鉴权、能力开关、确认令牌、审计脱敏与限流、UI 工具契约、终端回显匹配 | logx/debugsrv 全套测试 + 真实应用只读查询 | 通过 |
| 配置隔离和调试热重载 | 临时配置目录、重载失败保留服务、发现文件使用客户端地址、测试不能覆盖真实发现文件 | appdir/debug_reload/debug_discovery 测试 | 修复 B01、B02 后通过 |
| 自启动与平台服务 | 平台路径/注册配置生成等安全可测路径 | `autostart_test.go`、交叉编译 | 通过；未改变当前用户系统自启动状态 |

## 4. 缺陷清单、复现和修复

优先级说明：P1 为数据完整性、安全状态或严重阻塞；P2 为功能错误；P3 为诊断与协议反馈问题。这里的级别用于本轮排序，不表示已完成独立安全评级。

### B01 · P1：调试重载测试覆盖真实 debug.json

- 复现：运行 `internal/apphost` 的 debug reload 测试，测试虽使用临时设置文件，但发现文件仍由全局真实配置目录决定，曾使运行中应用的发现文件被覆盖或删除。
- 根因：测试夹具没有隔离 `appdir.ConfigDir()` 的所有副作用。
- 修复：`newReloadTestApp` 在创建实例前设置 `appdir.EnvDevConfigDir` 为 `t.TempDir()`；发现文件与设置一并进入隔离目录。
- 复测：apphost 全套、race 和真实应用发现/连接均通过。测试结束后仍可使用真实发现文件进行冒烟。
- 文件：`internal/apphost/debug_reload_test.go`。

### B02 · P2：LAN 模式发布不可移植的客户端 URL

- 复现：启用 BindLAN 时，`DebugInfo.url` 与 SSE 提示使用 `http://0.0.0.0:8765`；该地址是监听通配地址，部分客户端/代理无法把它作为目标地址使用。本次最初请求即失败。
- 修复：面向本机客户端的 `url` 和 `streamHint` 始终发布 `127.0.0.1`；保留原有 LAN 监听和独立 LAN 地址信息。
- 复测：`TestDebugDiscoveryAlwaysUsesDialableLoopbackURL` 同时覆盖 LAN 开/关；真实 loopback HTTP/MCP 正常。
- 文件：`internal/apphost/debug.go`、`debug_discovery_test.go`。

### B03 · P1：正常传输块超过限速器 burst，低速率 burst 为 0

- 复现：128KiB/s 限速配置产生 32KiB burst，传输常规 64KiB 块时 `WaitN` 直接报超过 burst；3B/s 时整除后 burst 为 0。
- 修复：正速率 burst 至少为 1；等待按稳定 limiter 的 burst 拆块，不要求调用方缩小传输缓冲区。
- 复测：`TestTransferLimiterAcceptsChunksLargerThanBurst`、`TestTransferLimiterSmallRatesHaveNonzeroBurst`、真实 SFTP 二进制往返通过。
- 文件：`internal/sshx/transfer.go`、`transfer_limiter_test.go`。

### B04 · P1：传输限速热调整存在 data race

- 复现：同时循环 `SetRateLimit` 与 `Wait`，race 检测报告对限速状态的无同步访问。
- 修复：锁内读取 enabled/limiter 快照；重配置创建新的 limiter，避免等待过程中 burst 又被缩小。
- 复测：500 次并发重配置用例和最终全包 race 通过。
- 文件：同 B03。

### B05 · P1：改变并发数后正在运行的传输无法释放槽位

- 复现：取得一个 worker 槽后调整并发数；原实现替换承载令牌的 channel，Release 面对新 channel 而阻塞。
- 修复：改为锁保护 active/limit，通过变化通知唤醒等待者，调整上限不丢弃在途任务的占用数。
- 复测：增大后释放、缩小后等待所有既有任务退出、已取消的 Acquire、完整拷贝均通过。
- 文件：`transfer.go`、`transfer_limiter_test.go`。

### B06 · P2：系统信息缓存为合法但错误结构 JSON 时异常

- 复现：把 snapshot 存储设为 `null`、数组、数字、字符串或损坏 JSON，再调用读取/缓存；原实现对 `null` 等结构操作会抛错，后续持久化不可靠。
- 修复：从 unknown 验证为非数组对象，仅保留对象类型的快照条目；异常存储回退空缓存。
- 复测：上述五类输入、缓存写入/删除、服务器地址变化淘汰旧缓存和暂时采集失败保留旧信息通过。
- 文件：`frontend/src/services/serverSnapshot.ts`、`serverSnapshot.test.ts`。

### B07 · P1：Latin-1 范围字符被当作原始字节发送

- 复现：原 `btoa('é')` 不会抛错，却发送 E9，而不是 UTF-8 的 C3 A9；真实终端曾因此破坏带引号命令输入。中文因为走异常兜底，反而不一定暴露同一问题。
- 修复：普通 xterm 文本统一先经 TextEncoder 编成 UTF-8；调试 base64 输入显式标记为已编码原始字节，避免二次转码。大段输入分块构造 base64 源串。
- 复测：ASCII、é/ü/£/±、中文、emoji、控制字符、大段粘贴、256 个原始字节单元测试通过；调试桥验证文本/二进制标志；真实 shell 的 `od` 返回 C3 A9 及正确中文 UTF-8。
- 文件：`terminal-input.ts`、`terminal-input.test.ts`、`TerminalView.vue`、debug registry/bridge 及测试。

### B08 · P2：调试接口不能断开本机终端

- 复现：对真实本机终端调用 `disconnect_session`，原实现仅走 SSH manager，返回“会话不存在”。
- 修复：调用现有 `App.Disconnect` 统一入口，由其分发到本机或 SSH 管理器。
- 复测：`TestDebugDisconnectRoutesLocalSession` 使用真实 PTY；真实应用断开后状态 closed，重连后再次执行命令成功。
- 文件：`internal/apphost/debug.go`、`security_rotation_test.go`。

### B09 · P1：隔离配置的主密码切换访问共享钥匙串且密钥未正确持久化

- 复现：在开发目录覆盖模式下，用内存 keyring 放置与隔离配置无关的 sentinel；开启主密码会删除它，关闭主密码会写共享 keyring，隔离 vault 重开后却从本地文件得到另一把 key。
- 修复：Vault 打开时捕获隔离状态；隔离实例在打开、状态查询、启用和禁用主密码路径均不访问系统 keyring，关闭主密码后的 key 写入隔离目录。
- 复测：sentinel 在开启/关闭后保持不变；重开 key 一致；完整生命周期解密成功。测试使用 mock，不接触真实系统钥匙串。
- 文件：`internal/cryptox/vault.go`、`vault_test.go`。

### B10 · P1：密钥迁移中途失败造成混合密文或元数据先行变更

- 复现 A：服务器字段有效、稍后处理的凭证密文损坏。原实现逐条 UPDATE，凭证失败时服务器字段已经换成新 key，而其他字段仍是旧 key。
- 复现 B：主密码状态/元数据先保存，字段重加密随后报错；或元数据磁盘写入失败但内存状态已改变。原 key、持久化元数据和数据库可能不一致。
- 修复：`ReencryptAllSecrets` 将服务器与凭证 password/key_content 放入同一 SQLite 事务，检查迭代错误并统一提交。Vault 增加迁移回调，在失败时恢复旧元数据，仅在元数据与迁移成功后发布新活动 key；宿主不再先换 key 再执行独立迁移。
- 复测：启用/更换/关闭 × 成功/损坏凭证失败六组宿主集成；每组均重新打开 Vault，按正确密码解锁并解密服务器、凭证的两类字段；磁盘写元数据失败也验证原内存状态与 key 不变。
- 边界：该修复针对返回错误的回滚。**文件系统元数据和 SQLite 之间尚没有跨存储崩溃恢复日志，不承诺在任意指令点断电都能原子恢复**，见第 9 节。
- 文件：`cryptox/vault.go`、`store/crypto_migrate.go`、`apphost/app.go` 及三个对应测试文件。

### B11 · P1：缺失密码校验器时错误密码可被标为已解锁

- 复现：开启主密码后删除 `VerifierB64`，重新打开并输入任意非空密码；原代码把“校验器为空”解释为跳过验证，报告解锁成功。注意：这不意味着错误 key 能解密已有 AES 密文，但会形成错误安全状态，并可能引出后续数据破坏。
- 修复：读取主密码元数据和使用密码操作时校验 salt 长度与 verifier 结构；损坏元数据直接返回错误。
- 复测：缺失 verifier、无 salt、非法 salt 均拒绝；正确密码/错误密码生命周期仍通过。
- 文件：`vault.go`、`vault_test.go`。

### B12 · P1：损坏的已有文件密钥被自动覆盖

- 复现：把已有 `.master.key` 改成无效内容再 OpenVault；原实现生成随机 key 覆盖原文件，掩盖故障并破坏恢复可能性。
- 修复：只有文件不存在时才创建新 key；已有文件内容损坏或读取出现其他错误时返回错误，保留原文件。
- 复测：OpenVault 必须报错，损坏文件字节必须完全不变。
- 文件：`vault.go`、`vault_test.go`。

### B13 · P2：更新偏好存储异常会从模块导入路径抛出

- 复现：模拟 localStorage.getItem 的 SecurityError 与 setItem 的 QuotaExceededError；原 `updates` 模块在导入时直接失败，设置写入也抛异常。
- 修复：安全读取并采用默认值，安全写入失败后保留本次会话内设置；启动检查同样使用安全读取。
- 复测：受限存储下模块可导入、可修改通道、可关闭启动检查、可注册启动监听；现有更新提醒流程保持通过。
- 文件：`frontend/src/services/updates.ts`、`updates.storage.test.ts`。

### B14 · P2：配置包导入未校验版本和服务器数量上限

- 复现：构造经过正常加密、magic 正确但 version=999 的包，或 version=1 且包含 501 个服务器的包；原解析均接受，绕过导出端的限制。
- 修复：解析入口只接受支持的版本 1，并对服务器数量执行同一 500 上限。
- 复测：两种违规包均拒绝；中文服务器/分组、凭证往返、文件读写、错误密码、篡改、截断仍正常。
- 文件：`store/dingpack.go`、`audit_flows_test.go`。

### B15 · P2：本机 shell 早退与同 ID 替换的生命周期竞争

- 复现：临时 shell 启动立即退出，重复运行时出现进程已退出但管理器仍保留连接项；源码中读写/Wait 协程在注册之前已经启动。并发同 ID Connect 还可能发生替换互相干扰。
- 修复：先注册并发出 connected，再启动读取和退出观察；连接/CloseAll 生命周期串行化；移除时核对 Session 身份，旧实例不能移除新实例。
- 复测：快速退出循环、同 ID 12 次并发替换、关闭计数、完整 PTY 生命周期通过；单独覆盖率快速退出测试重复 30 次通过，整包重复 10 次通过。高负载下进程退出等待窗口设为 5 秒，避免用过窄时限误判调度延迟。
- 文件：`localterm/manager.go`、`session.go`、`manager_test.go`。

### B16 · P3：SOCKS5 不支持的命令缺少协议错误响应

- 复现：完成无认证握手后发送 BIND 命令，原实现直接关闭 TCP，客户端只能看到 reset。
- 修复：完整消费请求后返回 0x07（命令不支持）；未知地址类型返回 0x08，再结束通道。
- 复测：真实 SOCKS5 CONNECT HTTP 往返不受影响，BIND 得到明确失败响应。
- 文件：`sshx/tunnel.go`、`audit_integration_test.go`。

### B17 · P2：SSH shell 在注册前退出，遗留“已连接”会话

- 复现：合成 sshd 接受 shell 后立刻发送 exit-status 并关闭通道，循环连接；原 newSession 退出回调可能先执行，随后 Manager 又把已关闭实例注册进去。
- 修复：在管理器与 Session 锁保护下检查 closed/closing，并在同一临界区完成注册；已经结束的连接返回“会话已关闭”。
- 复测：80 次立即退出循环；和正常 SSH 字符往返、并发连接/关闭合并使用 race 重复运行 5 次通过；最终全量通过。
- 文件：`sshx/manager.go`、`terminal_integration_test.go`、协议夹具 `transport_test.go`。

### B18 · P1：SSH 清理日志在锁外读取 sessions map

- 复现：实际 SSH 通道同时建立/关闭，race 报告 Connect/map 写与 `Manager.remove` 的锁外 `len(m.sessions)` 冲突。
- 修复：在持锁期间获取剩余数量，日志只使用快照。
- 复测：同 B17 的真实通道并发测试和最终所有 internal 包 race 通过。
- 文件：`sshx/manager.go`、`terminal_integration_test.go`。

### B19 · P1：传输忽略短写并错误上报成功或进度

- 复现：构造只接收 4 字节块中前 2 字节的 Writer。返回 `(2, nil)` 时，文件和文件夹流式复制原实现都把 4 字节计入进度并报告成功；返回 `(2, io.ErrClosedPipe)` 时，又把已写出的 2 字节漏记为 0。
- 修复：两个生产传输循环都按实际 written 数累计；写入返回错误时保留已写字节；无错误但 written 小于请求数时返回 `io.ErrShortWrite`，防止发布不完整的目标文件。
- 复测：`TestStreamTransferCountsPartialWritesAndRejectsShortWrites` 和 `TestFolderCopyCountsPartialWritesAndRejectsShortWrites`；两种返回形式下核对失败状态与准确的 2 字节进度，并重跑 SFTP 往返、取消、完整测试及 race。
- 文件：`sshx/manager.go`、`transfer_progress_test.go`、`transfer_limiter_test.go`。

## 5. 真实应用冒烟执行明细

脚本直接读取本机发现文件，使用 loopback，显式禁用环境代理，认证 token 不写入结果。执行的 50 项检查均可在 CSV 中按 `Live debug API` 过滤查看。

1. 无认证 HTTP 请求得到 401。
2. HTTP/MCP 工具目录一致，实际 98 个工具，无重名。
3. 健康、权限、设置 schema、服务器/分组/凭证/隧道/主机信任、命令、日志、事件、审计调用和传输列表查询共 14 项返回正常。
4. MCP initialize 与 resources/list 正常。
5. 首页、服务器页、隧道页导航，11 个设置分区可见性断言。
6. 新建真实本机终端，执行 ASCII、é、中文命令；用 `od -An -tx1` 独立核对 UTF-8 字节；发送原始 base64 命令，检查输出。
7. 调整终端大小、滚动、新建第二个真实终端、分屏和恢复、切换四种侧栏状态。
8. 断开本机会话，确认 closed；重连，确认 connected 并执行新命令成功。
9. 前端控制台 error 数为 0。
10. 只关闭测试新建标签和 PTY，确认原有标签保留并恢复先前界面状态。

唯一跳过项是 **MCP resources/templates/list**：当前服务返回 -32601，未实现该可选方法。`resources/list` 正常。该项明确记 SKIP，不计为 PASS。

冒烟脚本初版有几处自身假设错误，均在修正后重跑：`od` 字节之间可能有多个空格，需用 `\s+`；xterm 高度较大时 tail 的最后 20 行可能都是空白，应从 head 读取；真实 API 的断言字段是 passed/matched。**这些脚本错误不计入上述应用缺陷数量。** 修改 Go 源码会触发 Wails 自动重启，因此被重启中断的那一轮也没有用于最终结论。

## 6. 新增重点测试及复现命令

以下命令从仓库根目录执行。任一失败都不应通过删除断言或屏蔽测试来处理。

```sh
# 全量 Go 测试；两个手动协议探针使用本地 HTTP/替身前端，不操作真实主机
DSH_LIVE_PROBE=1 DSH_UI_PROBE=1 go test -json ./... -count=1

# 并发检测和静态检查
go test -race ./internal/... -count=1
go vet ./...

# 针对限速、并发槽位、取消字节数
go test -race ./internal/sshx -run 'TestTransfer|TestThrottled|TestCancelled' -count=1

# SSH 真实通道、早退、并发清理，重复检查
go test -race ./internal/sshx -run 'TestSSHManagerReal|TestSSHImmediate' -count=5

# 本机真实 PTY 与快速退出，重复检查
go test ./internal/localterm -count=10
go test ./internal/localterm -run TestImmediate -count=30 -cover

# Vault 生命周期、损坏数据、隔离边界与密钥轮换
go test ./internal/cryptox ./internal/store ./internal/apphost -count=1

# 真实协议 SFTP 和四类隧道
go test ./internal/sshx -run 'TestSFTPManager|Test.*Tunnel|TestUploadFolder|TestDownloadFolder' -count=1

# 前端单元/组件测试、类型检查和构建
(cd frontend && npm test)
(cd frontend && npm run build)

# 需要用户已开启调试应用；输出路径可自行替换
python3 scripts/audit/live_smoke.py --output /tmp/ding-ssh-live-result.json

# 原生生产构建；本命令在 macOS 环境执行
wails build -platform darwin/arm64 -o ding-ssh-audit

# 仅交叉编译，不执行 Windows/Linux 测试程序
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -exec /usr/bin/true ./internal/... -run '^$'
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -exec /usr/bin/true ./internal/... -run '^$'

# 包含资源门禁；本轮该命令预期仍因现有资源预算超标返回非 0
bash scripts/verify-ui2.sh
git diff --check
```

coverage 使用存在测试文件的包，避免当前 Go 工具链对无测试包收集覆盖率时报告 `go: no such tool "covdata"`：

```sh
go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./internal/... > /tmp/ding-ssh-cover-packages.txt
xargs go test -count=1 -coverprofile=/tmp/ding-ssh-cover.out < /tmp/ding-ssh-cover-packages.txt
go tool cover -func=/tmp/ding-ssh-cover.out
```

## 7. 覆盖率及证据口径

最终覆盖率命令退出成功；不是采用先前失败运行残留的 profile。结果如下：

| 包 | 语句覆盖率 |
| --- | ---: |
| appdir | 85.7% |
| apphost | 16.4% |
| autostart | 39.4% |
| cryptox | 55.7% |
| debugsrv | 71.8% |
| i18n | 91.3% |
| localterm | 74.8% |
| logx | 84.8% |
| sshconfig | 80.3% |
| sshx | 60.3% |
| store | 53.4% |
| updater | 22.3% |
| 上述包加权合计 | **53.2%** |

该数字不含 main、命令行入口及无测试文件的 logfilter/models；不含前端测试或进程外真实 Wails 冒烟覆盖率。apphost 和 updater 大量代码涉及原生窗口、系统路径、文件选择器和安装行为，自动化覆盖率明显较低。**执行测试多不等于执行了所有代码分支。**

测试过程中保留了修复前失败和修复后通过的原始日志，位置在本次运行机器的 `/tmp/ding-ssh-audit-20261121/`。该目录是临时证据，不保证长期保留，也不直接提交，避免包含真实应用上下文。长期可审阅证据是本报告、脱敏 summary、859 条结果目录及随代码提交的回归测试。

race 的 macOS 链接阶段出现 LC_DYSYMTAB 警告，原生 Wails 编译出现重复 objc 库警告；命令实际成功，未把这些链接器警告误报为业务测试失败。

## 8. 仍失败的资源体积门禁

`scripts/verify-ui2.sh` 中的构建、前端测试、Go 测试通过后，资源检查得到：

| 指标 | 本次测量 | 脚本阈值 | 判定 |
| --- | ---: | ---: | --- |
| CSS raw | 263,454 B | 100,000 B | warning |
| CSS gzip | 77,575 B | 30,000 B | warning |
| index JS raw | 762,918 B | 900,000 B | 通过 |
| index JS gzip | 249,905 B | 270,000 B | 通过 |
| font-face | 114 | 32 | **失败，脚本在此退出** |
| woff/woff2 文件数 | 222 | 32 | 静态核对超标，因前项退出未执行到该断言 |
| 字体总字节 | 6,110,464 B | 无单独字节阈值 | 记录 |
| dist 磁盘占用 | 10,240,000 B | 2,621,440 B | 静态核对超标，因前项退出未执行到该断言 |

主要来源是 Noto Sans SC 完整分片及多字体/字重同时包含 WOFF、WOFF2，以及约 2MB 的 PNG 资源。该问题在本轮初始构建就存在，本轮功能修复没有新增字体或图片资源。Vite 的单 chunk 大于 500KB 警告也仍存在。

处理结论：本轮保留原字体选项和离线中文覆盖，没有修改门禁阈值。后续需要专门进行字体/图片打包优化并核对视觉与字符覆盖，再决定是否调整早期 UI 预算。**这一项是明确未解决的资源/性能质量问题，不能用测试通过掩盖。**

## 9. 未实际验证与剩余风险

1. **外部真实服务器兼容性**：协议夹具覆盖 SSH/SFTP/四种隧道的真实数据通道，但没有遍历 OpenSSH/Dropbear/商业堡垒机、不同 PAM/MFA、真实 SSH agent 或复杂企业网络。本轮没有读取或使用用户的真实登录凭据去补这些验证。
2. **跨平台运行**：在 macOS ARM64 实际执行；Windows/Linux 只交叉编译 internal 包，不能声称相应原生 UI、ConPTY、注册表、自启动、系统代理或权限提升已实测。
3. **原生更新与权限提升**：已有测试验证计划、hash、回滚、重启策略；未对当前用户安装目录执行真实提权升级或替换运行中的应用。构建时的本地自签名不等于正式签名、公证和发布验证。
4. **崩溃一致性**：B10 保证已构造的返回错误场景回滚，不提供 security.json 和 SQLite 的跨存储断电原子性；断电/磁盘损坏恢复仍需专门设计与故障注入。
5. **GUI/GPU 与传输兼容性**：组件和渲染工具测试不等价于所有 GPU/WebGL、IME、复杂 TUI、真实 rz/sz 对端、大文件长时间弱网、休眠唤醒或剪贴板系统权限实测。
6. **调试安全**：本轮沿用用户已启用的调试能力；没有扩大权限。对未授权工具的拒绝路径由 debugsrv 测试覆盖。实际部署仍需妥善保管调试 token。
7. **资源体积**：第 8 节明确未通过；该报告不把它降级为已修复。

## 10. 提交与复查

提交前已检查工作区差异、生成绑定一致性、测试结果与脱敏文件，并确认从 `origin/dev` 拉取时与起始 HEAD 无分叉。本报告与测试代码随修复一并提交到 `dev`。实际推送结果和提交号以本次任务最终回复及 `git log -1` 为准；报告不预先填写未经服务器确认的 push 成功状态。

建议后续修改传输、会话或安全模块时，至少运行全量 Go 测试、race、前端测试/构建和真实本机终端冒烟，再核对本报告中的高风险回归用例。
