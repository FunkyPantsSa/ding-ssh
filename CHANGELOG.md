# 变更日志（CHANGELOG）

本文件按版本及开发顺序倒序记录已完成的、可对外说明的变更（最新在最前）。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
分类使用：**新增 / 变更 / 修复 / 文档**。

每条尽量写成「现象 → 原因 → 修法」，便于回溯当时的判断依据。

## [1.2.1] 2026-11-17

- 合并 `dev` 上自 1.1.1 起的 22 个提交，发布为 1.2.1。
- 新增：简体中文 / English 双语界面（原地切换，不重载页面、不重建会话）；JumpServer 堡垒机模式（终端与 SFTP 目录独立，先在 SFTP 选定资产与账号目录再操作）；`main` / `dev` 自动更新通道（按通道读取 `update.json`，SHA-256 校验，立即或退出时安装）；反向 SOCKS5 隧道与客户端地址选择；终端字体方案与按需下载缓存；启动与关闭偏好、终端缓冲区搜索。
- 变更：动态配色改用 HCT 色调映射并区分 Solid / Soft Glass / Clear Glass 材质；深色骨架抬亮并重新拉开层级，终端 Ink 与导航 / 工具栏 / SFTP 同色系；统一 UI 令牌与管理边界；macOS 改为 Apple Silicon / Intel 独立安装包。
- 修复：终端共享字形图集导致的切标签乱码；zsh `%` 提示符下本机终端输入不回显；关闭系统看板后状态栏停止刷新；拖放高亮与「未分组」哨兵值冲突；窄窗口服务器行与对话框焦点；MCP 控制与传输状态加固。
- 验证：Go `internal` 测试通过；前端 `vue-tsc --noEmit`、`npm run build` 通过，51 个测试文件、228 项测试通过。三平台安装包由 Release 工作流构建；真实远端与跨平台桌面冒烟未执行。

---

## [未发布] 2026-11-23 · 补齐项目级 MIT 开源许可证

- 现象：仓库此前未声明正式项目级许可证，中英文 README 提示以「后续补充的 `LICENSE` 为准」，缺乏法律层面的授权明确性。
- 修法：
  - 根目录正式补充标准 `LICENSE` 文件（MIT 许可证，版权声明为 FunkyPantsSa）。
  - 更新 `README.md` 与 `README.en.md`：总览表格与文末声明正式指向 `LICENSE`，并补充 MIT 徽标。
  - 前端 `frontend/package.json` 显式增加 `"license": "MIT"` 声明。
- 验证：文件结构与许可证文本完整无误。

---

## [未发布] 2026-11-22 · MCP 控制面：独立审批、客户端授权、任务执行与文件闭环

- 现象：控制面只有一个共享 Bearer token 和一组能力位，`confirm.prepare` 由同一调用方立即拿到可执行 token，无法证明用户批准；终端自动化缺少稳定的独立执行与任务生命周期；文件接口没有条件写入、补丁预览与分片上传下载；权限分类把终端读取、文件读取、连接、界面观测混在一起；工具目录与 README 的工具数量、能力说明长期滞后。
- 原因：早期实现以「单机可信脚本」为前提，能力位粒度粗、审批入口在被自动化的 WebView 内，且工具主要围绕交互式 PTY 构建，没有为非 PTY 任务、客户端隔离和文件可靠性设计独立的策略执行层。
- 修法：
  - **权限与身份**：拆出 19 个能力位（`read`、`terminal.read`、`fs.remote.read`、`ui.inspect`、`session.connect/disconnect`、`credentials.use`、`terminal.input`、`exec.local`、`ui.write`、`automation.trusted`、`config.write`、`secrets.read/write`、`fs.remote.write`、`transfer.cancel`、`sudo.credential`、`lifecycle`、`eval`），新增能力默认关闭；设置页提供观察 / 运维 / 完全信任开发预设，扩权需系统原生批准。
  - **独立审批**：`confirm.prepare` 同步弹出独立于 WebView 的原生窗口，批准后返回 `operationId`、脱敏摘要和一次性执行凭据；支持批准一次或十分钟内相同调用方、动作、工具 + 完整参数；凭据绑定客户端、工具、动作与参数，120 秒过期，`confirm.status` 可查状态但不回传 token。MCP 不能通过能力、设置或 store patch 扩权。
  - **客户端授权**：新增每客户端独立 `dsc_` token，限定服务器、会话、读写路径、能力与到期时间（最多 24 小时、最多 32 个、重启失效、可撤销）；受限客户端禁止明文凭据、本机执行、任意 JS 与完全信任界面自动化。
  - **独立执行**：`exec.start` 使用非 PTY SSH exec，支持 `raw` / `posix`、`cwd/env`、超时、有界输出与幂等键；`jobs.get/wait/output/cancel/list` 提供游标分页与未知结果语义；应用重启后未完成任务标为 `unknown` 且不自动重放。`exec.plan/batch` 支持 1–8 目标冻结计划与 1–4 并发。
  - **文件闭环**：新增 `sftp.download`、`sftp.upload`、`sftp.patch.preview`、`sftp.patch`；覆盖写使用同目录临时文件与原子替换，支持 `expectedHash`（含 `missing`）与 `append`；宿主校验 canonical 路径、拒绝写入最终符号链接，白名单为空时禁止一切写入。
  - **运维观测**：新增 `hosts.metrics`、`processes.list`、`tunnels.health`、`services.status/logs`、经批准的 `services.restart`、`sessions.connect/disconnect/reconnect/wait`、`hostTrust.challenges`、`servers.search`。
  - **协议与资源**：协商 `2024-11-05` / `2025-03-26` / `2025-06-18`，严格校验 JSON-RPC、Content-Type 与参数 schema；新增资源模板与订阅、工具注解、结构化输出；每 MCP 会话只允许一个活动 SSE，最多 128 个会话、30 分钟空闲回收；事件带 `seq` / `after` / `gap`；stdio 代理支持 32 路并发与取消，只重试确定未送达的请求。
  - **安全与数据**：HTTP 只接受 header token，执行确认只接受 `X-Ding-Confirm-Token`；Host / Origin 校验、并发与请求体上限；持久审计只写固定元数据；诊断包默认只含安全投影；UI 组件树、DOM 文本和控制台只返回安全元信息；UI 快照 64 组 / 64 MiB / 24 小时清理。
  - **文档**：工具目录当前 124 个工具；新增《MCP 任务与文件接口》《MCP 改造实施与验证记录》《MCP 能力设计评审与演进方案》，并重写中英文 README，移除失效的设计文档链接。
- 兼容：原有 MCP / HTTP 工具名称与文本结果格式保留（兼容别名继续可用），新增 `structuredContent` 只在 2025-06-18 协商下返回；旧调试配置保留已明确保存的能力，不自动开启新增能力；旧 `/tmp` sudo 临时文件清理路径仍被识别。
- 边界：普通 SFTP 无法消除远端检查 / 写入竞态，追加写非原子，ACL / xattr 不保证；任意 shell 可绕过文件白名单；取消 SSH channel 不保证远端进程树终止；应用重启不保证任务继续；像素截图仅 Windows；远程接入无 TLS / OAuth，仍应通过 SSH 转发。
- 验证：`go test ./...`、`go test -race ./internal/... ./cmd/ding-ssh-mcp`、`go vet ./...` 通过；前端 69 个测试文件、333 项测试与 `npm run build` 通过；macOS `wails build` 与 Windows/Linux amd64 后端交叉编译通过；官方 TypeScript MCP SDK 1.32.2 经 HTTP / stdio 完成初始化、完整目录与健康调用；隔离实例安全联调 12 项通过。完整记录见 `docs/MCP改造实施与验证记录.md` 与 `docs/validation/mcp-2026-11-22.json`；原生审批实机点击与跨平台像素截图未验证。

---

## [未发布] 2026-11-20 · 服务器分组支持文件夹式嵌套

- 现象：分组只能平铺，服务器多了只能靠 `生产-华东` 这类命名前缀人工分类；父级重命名/删除要在服务器列表里逐台改，缺少层级视图。
- 修法：
  - 数据层采用**路径即身份**：`servers.grp` / `groups.name` 存完整路径（如 `生产环境/华东/杭州`），不新增表或列；`GetGroups` 自动补齐隐式祖先。
  - 后端新增 `internal/store/grouppath.go`（NFC/空段/深度/控制字符校验、祖先/父级/子孙判断）与 `internal/store/grouptree.go`（SQLite 事务）：`AddGroup` 创建全部祖先、`RenameGroup` 只改最后一段并级联更新子分组与服务器、`RemoveGroup` 支持递归删除或保留子分组上移一级；启动时对旧库非法路径做一次幂等规范化，正常数据零改动。
  - 前端左栏改为分组树：展开/收起、父组选中含全部子孙、层级细引导线、计数、右键新建子分组/在此新建服务器/重命名/删除、拖服务器到任意层级（悬停折叠父组自动展开）、搜索自动展开命中路径；列表上方新增面包屑与“含子分组 · N 台”chip。
  - 树形视觉二次收紧：展开箭头移到行尾、计数之前，行首文件夹图标与“全部服务器/未分组”严格对齐；层级缩进 14→12px，去掉左侧箭头占位，长名称获得更多横向空间。
  - 新建/重命名使用带路径预览的轻量弹窗；分组管理弹窗树化，支持新建子组、行内重命名、删除时选择递归或保留子组；快速连接面板同步树形化；服务器编辑弹窗的分组选项改为读取完整分组列表，并支持 `父组/子组` 直接新建多级路径。
  - 分组之间拖拽调整层级（`MoveGroup`）列为后续 TODO，本期不实现。
- 兼容：旧扁平分组原样显示；旧分组名中的 `/` 自动解释为层级；降级到旧版本时路径按扁平名显示，不丢服务器/分组；`.dingpack` v1 与 SSH Config 导入格式不变。
- 验证：`GOCACHE=.smoke/gocache go test ./internal/...` 全部通过；`go vet ./internal/...`、`go build ./...` 通过；`vue-tsc --noEmit`、`vitest`（57 文件 / 273 用例）、`npm run build`、`wails build` 通过。分组功能不依赖远端 SSH，未额外做真实远端连接冒烟。

---

## [未发布] 2026-11-19 · 全量补全后端日志：debug / info / warn / error 四级覆盖与日志规范

- 现象：应用日志只有约 105 处 `logx` 调用，且集中在 `apphost` / `sshx` / `localterm` 少数文件；`store`、`cryptox`、`updater`、`sshconfig`、`autostart`、`debugsrv` 几乎为零。出问题时只能看到「某一步失败」的结果，看不到操作链条、耗时、降级与重试过程，无法判断服务状态。
- 原因：历史代码只在偶发失败点随手打日志，没有统一的级别语义、消息格式与高频路径限流；部分高频循环一旦补日志就会刷屏，导致大家默认少打。
- 修法：
  - 新增 `docs/logging.md` 日志规范：四级语义、`[tag] 中文动作: key=value` 格式、必记场景清单、敏感信息红线、高频采样与「错误只在边界层记一次」原则。
  - `internal/logx` 新增 `DebugEnabled/InfoEnabled/WarnEnabled/ErrorEnabled` 预判辅助与 `Sampler`（次数 + 时间双维度限流）及 `DebugSampled/InfoSampled/WarnSampled/ErrorSampled`，日志关闭时不消耗采样配额。
  - 按包补全日志调用：`apphost` 73→547、`debugsrv` 0→390、`sshx` 28→261、`store` 0→326、`cryptox` 0→116、`updater` 0→99、`localterm` 4→49、`sshconfig` 0→21、`autostart` 0→20、`i18n` 0→6、`main.go` 0→6，`cmd/ding-ssh-mcp` 31→56（stdout 仍只输出 JSON-RPC，日志只走 stderr 并打码 token）；合计约 1893 处（基线约 136 处）。
  - 统一 `[包/子域]` 前缀与 `key=value` 上下文（session/host/path/count/bytes/elapsed/err 等），覆盖：生命周期与启动自检、连接/鉴权/PTY/心跳/重连、SFTP 与传输、隧道、sysinfo/top 采集、设置与存储迁移、加密保险库、更新检查/下载/安装、SSH 配置导入、主机信任、调试 HTTP/MCP 服务（请求、鉴权、工具调用、审计、确认、UI/SFTP/sudo 工具）、本地终端、开机自启、语言切换。
  - 高频路径全部采样（终端输入输出、传输分块、事件推送、runtime 状态、目录/代次轮询等），只记长度/计数/百分比；密码、私钥、passphrase、token、终端内容、文件内容、请求/响应正文一律不落日志。
  - `internal/appdir`（与 `logx` 存在 import cycle）与 `internal/models`（纯数据结构）不引入日志；`internal/logfilter` 是日志管道本身，也不自记。
  - 修复 `i18n` 语言代码判断：`zh-CN` / `zh-Hans` / `en-US` 等按主子标签识别，启动时不再误报「非预期语言代码」WARN。
- 验证：`gofmt -l` 干净；`go build ./...`、`go vet ./...` 通过；`go test -count=1 ./...` 全部通过（含 `logx` 新增的级别辅助与采样器用例）；`GOOS=windows` / `GOOS=linux` 交叉构建通过。`wails build` 后在隔离配置目录（`DING_SSH_DEV_CONFIG_DIR`）实际启动冒烟：四级日志同时出现在控制台与 `logs/app-YYYYMMDD.log`，`logs.tail` 的 level 过滤可用，调试 API / MCP（initialize、tools/list 98 个工具、未知工具错误）正常，日志中无 token 明文；修复前冒烟发现的 `zh-CN` 误报 WARN 已复验消失。
- 说明：日志开关默认仍为关闭、级别默认 info（保持原有行为），详细 debug 日志需在「设置 → 日志」打开控制台/文件日志并把级别切到 debug；debug 级别下调试服务开启时的 `ui.revision` 轮询会持续记录操作级 DEBUG（约 1.5s 一轮），长时间挂机建议用 info 级别。

---

## [未发布] 2026-11-19 · 状态栏增加会话级「单次 / 实时」切换按钮与悬浮说明

- 现象：底部状态栏的 CPU / 内存长时间不变，容易以为采集失效；切换标签或重开运行状态面板时会变一次。
- 原因：实时刷新默认关闭（设计如此）——每次连接只做一次快照；只有开启实时刷新后才会前台约 3 秒、后台约 10 秒周期采集。此前状态栏没有任何提示，也没有开关，容易误认为是在实时刷新。切换标签/重新绑定会请求一次新快照，所以表现为「切换时才刷新」。
- 修法：状态栏新增一个「单次 / 实时」切换按钮：按钮文案与脉冲点反映当前模式，点击即切换，切到实时时立即补采一帧；悬停或键盘聚焦显示说明气泡（单次 / 实时各自的刷新节奏 + 上次更新时间），气泡右对齐避免贴边裁切。该切换**只作用于当前会话**：后端新增会话级覆盖 `SetSysInfoRealtime`，不修改全局 `sysInfoRealtimeEnabled`；终端关闭（会话移除）时后端自动清除，下次连接回到全局默认。运行状态面板的「自动刷新」同样改为会话级，并复用同一状态。
- 验证：新增 Go 测试 `TestSysInfoRealtimeOverridePerSession`（覆盖只影响目标会话、关闭后清除、不污染全局）和 `TestSysInfoLoopPollsOnlyWhenRealtimeEnabled`（关闭时只采一次、开启后按间隔持续采集）；更新 `ServerStatusBar.test.ts` / `SysInfoPanel.test.ts`；80% 实例开两个终端实测：标签 A 切到实时后标签 B 仍为单次，切回 A 仍实时；关闭 A 后新开终端的默认值为单次，且全局设置数据库保持 `false`。`go test ./...`、`vue-tsc`、`vitest`（55 文件 / 255 用例）、`npm run build` 通过。

---

## [未发布] 2026-11-19 · 修复状态栏/下拉弹层在界面缩放下的错位，并统一贴边翻转

- 现象：80% / 125% 等非 100% 缩放下，底部状态栏的网卡 / 磁盘下拉跑到触发器右侧或窗口外；只有一个根分区时点击挂载点像「没反应」（菜单实际被定位到了屏幕外）。设置页等下拉选择器也有同类错位。
- 原因：这些弹层 Teleport 到 body、以 fixed 定位（显示坐标），但触发器矩形来自 `getElementRect`（布局坐标）或原始 `getBoundingClientRect`（WKWebView 下仍是布局坐标），两套坐标系混用；状态栏菜单还用手写的 bottom 估位，没有统一的翻转/贴边逻辑。
- 修法：新增 `getDisplayRect()`——按引擎把元素矩形统一到视口显示坐标（Chromium 原样；WKWebView × zoom；用「比例更接近 zoom 还是 1」判定，避免小元素边框/亚像素差异误判）。状态栏磁盘/网卡菜单改用共享 `useMenuPlacement`：按真实尺寸定位、底部自动翻到触发器上方、水平贴边（`flipX:false` 不左右翻）、超高时限高滚动。SearchSelect 下拉、服务器行键盘右键菜单、主题取色器也统一改用显示坐标。
- 验证：80% 缩放下实测状态栏网卡菜单与触发器左对齐（left 500.17）、底边在触发器上方 4px、完整在视口内，列出全部网卡；单根分区时点击挂载点正常弹出并列出 `/`；设置页下拉与输入框同宽同左。新增 `utils/dom.test.ts` 5 个用例与 `flipX` 用例；`vue-tsc`、`vitest`（54 文件 / 254 用例）、`npm run build` 通过。

---

## [未发布] 2026-11-19 · 重做服务器配置浮层视觉：紧凑卡片、分栏规格、磁盘用量与页脚操作

- 现象：浮层像一个白底深灰边框的方盒子——圆角小、边框重、规格信息挤在左侧、右侧大片留白；磁盘行和百分比像默认控件，更新时间与刷新按钮也散落在正文里。
- 修法：重排为一张紧凑浮层卡片：标题区加服务器图标与圆形关闭按钮；规格改为两列键值对（系统/架构、CPU/内存）填满宽度；磁盘区增加「本地文件系统 / 已用 · 总量」标题行，每个挂载点用等宽字体、用量、百分比胶囊与圆角进度条（≥85% 琥珀、≥95% 红）；底部用分隔线收口，左侧更新时间与状态、右侧带图标的刷新按钮。材质仍是统一浮层规则（近不透明叠加色 + 可选高斯模糊），并单独放软边框、加深投影；修正了因 `:is()` 选择器特异性导致浮层覆盖样式不生效的问题。
- 验证：独立实例以真实快照渲染并截图确认版式；`vue-tsc`、`vitest`（53 文件 / 248 用例）、`npm run build` 通过。

---

## [未发布] 2026-11-19 · 修复服务器配置浮层透明发虚、与背后行文字混在一起

- 现象：服务器未连接时点开「服务器配置信息」浮层，面板和文字一起发灰发透，背后服务器行的「配置待采集」等文字透上来与面板内容叠在一起；玻璃材质下最明显。
- 原因：① `.summary.stale` 用 `opacity:.65` 淡化整个摘要区，浮层作为其后代一起变透明，祖先 `opacity` 还会让 `backdrop-filter` 失效；② 浮层背景用 `--background`（soft/clear 材质下约 83% 不透明），又没接入统一浮层材质、没有高斯模糊，透明度叠加后就「糊在一起」。
- 修法：stale 只淡化触发标签（`.stale .summary-trigger,.stale .pending`），面板保持不透明；浮层新增 `config-popover` 类并接入 `style.css` 的统一浮层规则——`--surface-overlay-bg` 近不透明叠加色 + `backdrop-filter: blur(var(--glass-overlay-blur)) saturate(135%)` + 强边框/浮层阴影；solid 模式自动退回不透明无模糊，行为与菜单/弹窗一致。
- 验证：soft 玻璃模式下实测面板 `background: rgba(255,252,255,.92)`、`-webkit-backdrop-filter: blur(18px) saturate(1.35)`、1px 强边框；mock 的 stale 容器中触发标签 `opacity: .65`、面板 `opacity: 1`；截图确认面板内容清晰、背后行文字不再透出。`vue-tsc`、`vitest`（53 文件 / 248 用例）、`npm run build` 通过。

---

## [未发布] 2026-11-19 · 右键菜单按真实尺寸贴边，避免贴底被裁断

- 现象：标签 / 终端 / 服务器 / SFTP 的右键菜单在靠近窗口底部（或右侧）弹出时，下半部分被窗口裁掉，最后几项看不到也点不到；标签右键菜单最明显。
- 原因：各菜单都用写死的像素值估算高度做贴边保护（标签 160px、服务器 250px、SFTP 320px），而菜单实际高度随条目变化（标签菜单约 250px、终端菜单约 291px）。贴底时只按估算值上移，真实高度更大就会溢出窗口；菜单又是 `Teleport` 到 `body` 的 `fixed` 元素，外层没有可滚动容器兜底。
- 修法：新增 `frontend/src/composables/useMenuPlacement.ts`，菜单渲染后测量真实宽高再贴边：放得下保持原位；下方放不下整体上移；子菜单优先翻到锚点左侧 / 上方；菜单比视口还高时贴顶并加 `max-height` + 滚动。标签（含分屏子菜单）、终端、服务器、SFTP 四个右键菜单统一接入，各组件不再传用估算高度夹取后的坐标。
- 验证：独立实例 1280×800 下用 `clientY=798` 打开标签菜单，菜单实际 250px，最终 top=542 / bottom=792（视口 800，边距 8），最后一项「关闭全部标签」完整可见；右下角终端菜单（291px）落在 top=501 / bottom=792；分屏子菜单自动翻到主菜单左侧且完整显示。`vue-tsc`、`vitest`（53 文件 / 248 用例，新增 7 个定位用例）、`npm run build` 通过。

---

## [未发布] 2026-11-19 · 修复界面缩放下终端选区 / 鼠标坐标偏移（WKWebView）

- 现象：界面缩放调到 80%–150% 的非 100% 档位后，终端里拖选文本的选区与鼠标实际位置不一致，80% 时选区明显落到鼠标左侧、且离终端左上角越远偏得越多；macOS 上 `vim` / `tmux` 的鼠标上报同样错位；100% 档位正常。
- 原因：WKWebView 的坐标系是「混搭」——`getBoundingClientRect()` 返回布局（未缩放）坐标（实测 `rect.width ≈ offsetWidth`），而鼠标 `clientX/Y` 返回显示（已缩放）坐标（实测 80% 时 1px 屏幕位移 = 1px clientX），xterm 的单元格尺寸又按布局坐标计算。旧补丁直接套用「gBCR 与鼠标同为显示坐标」的 Chromium 语义（`rect.left + (clientX - rect.left) / z`），在 WKWebView 上方向和量都不对：80% 时把本应在第 15 列的鼠标算成第 6 列左右。
- 修法：`frontend/src/utils/xterm-zoom.ts` 改为实测两件事：① zoom 值——逐级读取祖先的 computed `zoom` 相乘（取不到时退回设置值）；② gBCR 是否含 zoom——`rect.width/offsetWidth`、`rect.height/offsetHeight` 比例≈1 判定为 WebKit 的布局坐标，否则为 Chromium 的显示坐标。Chromium 仍按 `rect.left + (clientX - rect.left) / zoom` 折回；WebKit 则按 zoom 根原点把显示坐标折回布局坐标：`origin + (clientX - origin) / zoom`。缩放为 100% 或测不出坐标系时保持原样。
- 验证：在独立 WKWebView 实例（80%）里先用标记元素确认「布局坐标 (100,150) 实际显示在 (80,120)」，再按显示坐标从第 15 列拖到第 27 列，`getSelectionPosition()` 返回 `start.x=15 / end.x=27`，选中文本 `FGHIJKLMNOPQ` 与缓冲区同一区间逐字符一致。新增 `frontend/src/utils/xterm-zoom.test.ts` 9 个用例（Chromium 80% / 125%、WebKit 折回 80% / 125%、zoom 根不在原点、亚像素舍入、零尺寸兜底、100% 放行、私有服务缺失）；`vitest`、`vue-tsc`、`npm run build` 通过。

---

## [未发布] 2026-11-19 · 修复终端目录同步路径层层叠加（/root/install/install/…）

- 现象：在提示符只显示目录名（如 CentOS 默认 `[root@host install]#`）的服务器上执行相对 `cd`（`cd install`、`cd ..`）后，日志持续刷出 `目录同步: path=/root/install/install/install/...`，SFTP 面板被带到不存在的超长路径。
- 原因：`pump` 每读到一块输出就把整个滚动缓冲交给 `ParseDirFromOutput` 重新解析，历史 `cd` 回显会随每次按键 / 每行输出被反复命中；相对参数又以刚被这条回显更新过的 `lastDir` 为基准拼接，于是每解析一次多叠一层，去重永远不生效。提示符只显示目录名时会被 `normalizeRemotePath` 判为非绝对路径而丢弃，无法兜底。
- 修法一（流式消费）：`cd` 回显只在本次新补齐的完整行里匹配，每行最多解析一次（跨 Read 的残行用 carry 续接）；相对参数按命令执行前的目录解析，同一块里多条 `cd` 依次推进基准。提示符 / OSC 7 / OSC 3008 仍按绝对路径匹配并保持原有优先级。
- 修法二（失败命令不上报）：新增每会话一个串行目录同步协程，解析结果入队处理，终端输出读取不会因 SFTP 请求阻塞；`cd` 上报前用 SFTP `Stat` 校验目标存在且确实是目录，目录不存在 / 是文件时只记调试日志、不改 `lastDir`，因此后续相对 `cd` 的基准仍然正确。校验不可用（SFTP 被禁用、超时 800ms）时退回旧行为直接上报；超时后 30 秒内跳过校验，避免反复起协程；重连 / 关闭时丢弃旧连接的遗留任务。
- 修法三（视图不一致不误杀）：若目标"不存在"且当前目录在 SFTP 视角也 `Stat` 不到（如 SFTP 被 chroot、与 Shell 命名空间不同），判定校验不可信并停用校验、照常上报，避免把正常 `cd` 全部吞掉。
- 变更（无参数 cd）：`cd` 不带参数（回到 `$HOME`）时，提示符同行回显与脚本里独立成行的 `cd` 都会解析为远端 home（`~`），与 `cd ~` 一样走存在性校验；此前后两类回显都不匹配，在只显示目录名的提示符下会漏同步。
- 验证：`go test ./...`、`go test -race ./internal/sshx` 通过；新增 15 个回归用例（逐字符投喂不再叠加、相对 `cd ..` 只生效一次、跨块拆行、单块多条 `cd`、`cd -` / `cd .`、提示符去重、脚本裸 `cd` 行、失败 `cd` 不上报且不污染基准、绝对路径 `cd` 同样校验、校验不可用时回退、串行队列处理与停止、真实 SFTP `Stat` 的目录/文件/不存在判定、SFTP 与 Shell 视图不一致时停用校验、校验冷却期不再请求远端、无参数 cd 回 home）。

---

## [未发布] 2026-11-19 · 服务器配置浮层支持任意摘要触发、跟随滚动并回报刷新结果

- 变更：系统、核数、内存、磁盘四个摘要标签都可以打开配置浮层（此前只有磁盘标签可点），再次点击任一标签或点击浮层外关闭。
- 变更：浮层由 `position: fixed` 改为锚定服务器行的 `position: absolute`，上下滚动服务器列表时随行移动；下方空间不足时自动翻到行上方，并按列表可视区域限制高度，避免被滚动容器裁掉。
- 变更：点击「刷新配置」后按钮临时禁用；收到新快照后状态从「已请求刷新」变为「刷新完成」，采集报错或超时则显示「刷新失败」/「刷新超时，请稍后重试」。
- 修复：展开浮层的行提升堆叠层级，避免浮层被后续行遮挡。
- 验证：`vue-tsc --noEmit`、`vitest run` 通过（51 个测试文件、232 项测试）；新增任意标签触发、滚动容器内定位/向上展开、刷新完成与失败用例。

---

## [未发布] 2026-11-18 · 修复服务器配置信息浮层无法关闭

- 现象：点击服务器行里的「配置信息」后浮层一直显示，再点触发按钮也关不掉，浮层里也没有关闭入口。
- 原因：浮层显示由 `.disk:hover`、`.disk:focus-within` 与 `.pinned` 三处 CSS 共同控制。点击按钮后焦点留在按钮上，`focus-within` 让浮层始终可见，`pinned` 取反也无法隐藏；同时缺少关闭按钮和点击浮层外部关闭的逻辑。
- 修法：浮层只由点击状态控制；再次点击触发按钮、点击浮层外、点击新增的关闭按钮或按 Esc 都会关闭，关闭后焦点回到触发按钮。Esc 只在焦点位于浮层或触发按钮时处理，避免抢走终端里的 Esc。
- 验证：`vue-tsc --noEmit`、`vitest run` 通过（51 个测试文件、230 项测试）；新增点击切换、点击外部、关闭按钮和焦点回归用例。

---

## [未发布] 2026-11-12 · 系统信息采集共享与连接日志自动滚底

- 系统信息采集器改为会话级共享：关闭系统看板只取消本组件的事件监听，不再停止状态栏等其它消费者的采集，避免看板一关状态栏就停止刷新。
- `onSysInfoSnapshot()` 直接返回 Wails `EventsOn` 的注销函数，删除手写的 `EventsOff`，同一事件多订阅者互不影响。
- 连接失败 / 进行中面板的步骤日志在状态、展开或日志更新后自动滚动到底部，长日志不再停在顶部。
- 验证：新增 `sysinfo` 订阅生命周期测试（看板退订后状态栏仍收到恢复快照）；`vue-tsc --noEmit`、`npm run build` 通过，37 个测试文件、160 项测试通过。

---

## [未发布] 2026-11-11 · 修复本机终端输入回显被缓存（打几个字后不显示）

- 现象：本机终端输入命令时只显示前一两个字符，后续字符不回显，直到回车才一起出现。
- 原因：语义着色器为了按行着色，会把不以换行结尾的半行挂在 `pending` 里等换行；zsh 默认提示符以 `% ` 结尾，而 `PROMPT_TAIL_RES` 只识别 `$` / `#`，于是提示符和紧随其后的逐字符回显都被当成「未完成输出行」缓存。输入回显没有新行，就一直不写进 xterm。
- 修法：`Colorizer` 新增 `beginInput()`；`TerminalView.writeRaw()` 在每次向会话发送输入（按键 / 粘贴 / 补全）前调用它：先把挂起的半行（通常是提示符）立即补写，并让本次输入行的回显在遇到换行前原样透传，不再参与按行缓存。提示符识别同时补上 macOS 默认 zsh 的 `%`。命令提交（换行）后恢复正常的语义着色。
- 验证：新增 4 个回归用例（zsh `%` 提示符立即输出、`beginInput()` 补写挂起提示符、逐字符回显立即透传、换行后恢复着色）；`vue-tsc --noEmit`、`npm run build` 通过，36 个测试文件、159 项测试通过。

---

## [未发布] 2026-11-09 · 终端乱码复修、服务器高级设置折叠与界面细节

### 修复

- **终端乱码（复现：关闭一个标签、自动切到上一个标签后整屏字形错位；关闭 WebGL 即恢复）**：xterm 6 + `@xterm/addon-webgl` 0.19 在字体/主题/DPR 相同时让多个 `Terminal` 共享同一个字形图集。任一终端 `clearTextureAtlas()`（字体加载、系统恢复）或图集页合并后，其它终端的 WebGL 顶点模型仍指向旧纹理坐标；后台标签不会立即重绘，切回时直接画出错位字形（上游 [`xtermjs/xterm.js#6014`](https://github.com/xtermjs/xterm.js/issues/6014)、[`#6042`](https://github.com/xtermjs/xterm.js/pull/6042)、[`#6055`](https://github.com/xtermjs/xterm.js/pull/6055)，稳定版修复尚未发布）。
  上一版只补了 `term.refresh()`，但 `WebglRenderer._updateModel` 只在单元格内容变化时才重写顶点，未变化的行会继续使用旧坐标，所以仍然乱码。改为在 `frontend/src/utils/terminal-atlas.ts` 登记所有终端：共享图集被清空、图集页新增/合并、窗口重新获得焦点、页面恢复可见、标签重新激活时，通过 `xterm-webgl-zoom.invalidateWebglGlyphModel()` 丢弃每个渲染器的顶点模型（addon 私有 `_clearModel(true)`，与既有画布缩放补丁同类的受控私有 API 用法；私有 API 失效时回退公开的 `term.clearTextureAtlas()`），再整屏重绘。
- 连接失败页新增「编辑服务器」：从 `TerminalConnectionOverlay` 发出 `edit`，经 UI store 让常驻的 `ServerList` 复用编辑弹窗（不切页）；保存后 `sessions.syncServerNode()` 同步同 id 标签的节点，重试连接会使用新配置。
- 「新建服务器」分组下拉出现 `all`：`defaultGroup` 传入了左栏「全部服务器」的哨兵值，`groups` 计算属性又把它当真实分组加入选项。现在 `all` 会被排除（与弹窗打开时回落到「未分组」的逻辑一致）。
- 分段选择器（如设置页「打开选项栏 / 直接粘贴」）看起来像「按钮 + 外框」两层、内层圆角与外框不贴合：`.seg` 选中项移除多余的内描边，内层圆角改为 `外圆角 - 2px`；同步修正 `ui/tabs` 的同类圆角不一致与「深色材质」按钮的 `border + inset` 双描边。

### 变更

- 「新建 / 编辑服务器」把跳板机连接路径、主机密钥别名、额外 known_hosts 收进可折叠的「高级设置」；已有相关配置时自动展开，收起时显示「已配置」。SSH Agent 的地址 / 身份文件仍跟随认证方式直接显示。

### 验证

- 新增 / 更新测试：`terminal-atlas`（模型失效 + 重绘、合并去重、注销）、`xterm-webgl-zoom`（私有模型失效 API 与兜底）、`ServerDialog`（分组不含 `all`、高级设置默认折叠与自动展开）、`TerminalConnectionOverlay`（编辑按钮）、`ServerList`（跨页面请求编辑并同步标签）。
- `vue-tsc --noEmit` 通过，`npm run build` 成功；36 个测试文件、155 项测试通过。

---

## [未发布] 2026-11-06 · 深色骨架抬亮与层次重排

- 现象：上一轮已经把导航/终端/SFTP 的色相关系校准，但实际终端页仍然偏“沉重”：终端接近纯黑，导航、顶部栏、SFTP 挤在约 14–28 tone 的窄带里，大面积阅读区域缺少空气感。
- 修法：把深色骨架整体抬亮，并把层级拉开。终端 14.4→17.3、顶部工具栏 20.2→23.3、工作区 21.1→25.2、SFTP/卡片 26.0→30.8、主导航 28.2→33.8、抬升卡片 33.4、浮层 36.4；终端相对工具栏的 Ink 步进从 5.5 调整为 6.0。浅色分支保持不变。
- 结果：五主题终端为蓝色 `#232c2f`、粉色 `#30272a`、绿色 `#242c27`、黄色 `#2e2922`、紫色 `#2b2931`；终端仍是最暗、色度最低的一层，但不再是无光黑的画布。
- 关系保持：终端/顶部工具栏 tone 步进 5.6–6.5，终端/SFTP 步进 12.3–13.9；终端 chroma 为 SFTP 的 0.56–0.72×（五主题），任意色相下仍严格低于导航、工具栏与 SFTP；可感知色度下终端与相邻表面色相差 ≤7.9°。
- 终端页现在是“亮一档导航 + 中深顶部栏 + 深 Ink 终端 + 亮一档 SFTP”的四级阶梯；设置页的 workspace/cards 也同步抬亮，不再是整片深灰。
- 仅调整深色 Tone 骨架与终端 Ink 步进；ANSI 16/256/TrueColor、独立终端方案、SSH 会话、xterm 生命周期、布局、圆角与原生毛玻璃均未改。
- 修复设置页二级导航 active 项左侧 2px 强调条被圆角裁成高亮月牙的问题：active 统一为 1px accent outline；主导航与侧栏标签页同步移除同样的左强调条，避免出现“选项前面多一段高亮”。
- 重新生成五主题完整终端 + SFTP 布局、蓝/粉对照与关系图表；28 个测试文件、134 项测试通过。旧设置页矩阵移出最新验收，A-B-C/总览旧图在文档中标记为历史基线。

---

## [未发布] 2026-11-06 · 深色终端 Ink 与导航 / 工具栏 / SFTP 关系校准

- 现象：深色终端使用固定中性近黑，粉色主题下导航/SFTP 是暖紫红、终端却接近纯黑；蓝色主题下终端与冷蓝灰外壳也没有冷暖联系，像嵌入了另一个应用。
- 原因：终端 Ink 被固定为单一 L/C，且 `applyTint` 在低色度时直接回退纯灰，导致终端既不能继承主题色相，也无法在低 Atmosphere 下与相邻区域保持连续。
- 修法：终端改为深色材料体系内的独立 Ink 层：`layout.terminal = layout.toolbar − 5.5`，`DARK_TINT.terminal.influence = 0.40`；与导航、顶部工具栏、SFTP 共享 `primaryHue` 与连续冷暖权重，再用“终端 chroma 严格低于导航/工具栏/SFTP”的相对关系做像素级收敛，并把终端色相与顶部工具栏对齐。不再把某个固定 HCT chroma 上限作为唯一协调性判据。
- 结果：天空蓝终端 `#1d2629`（T≈14.4 / OKLCH C≈0.014）、粉色 `#2a2124`、绿色 `#1e2621`、黄色 `#28231c`、紫色 `#25232b`；比顶部工具栏低约 5.8 tone、比 SFTP 低约 11.6 tone，色度为 SFTP 的 0.61–0.73×。
- 相邻关系：顶部工具栏保持低色度深材料层（T≈20.2 / HCT C≈12.5），SFTP/卡片 T≈26.0，主导航 T≈28.2；在可感知色度（C≥0.008）下终端与相邻表面的色相差 ≤9.1°。
- `applyTint` 的最低 chroma 阈值从 0.02 降到 0.001：Atmosphere 20–100 连续保留主题色相，不再出现“导航有 tint、终端纯灰”的断层；极低色度下若量化后终端反而更艳，则回退为中性 Ink。
- 独立终端方案：关闭“终端跟随应用主题”后，背景、前景、光标、选区逐字节保留；对比度保护只在 follow 模式启用。ANSI 16 色、256 色与 TrueColor 未改动，xterm 生命周期、布局、圆角与原生毛玻璃均未改。
- 新增全色相严格色度顺序、低氛围连续性、冷暖一致性与独立方案保留测试；28 个测试文件、134 项测试通过。
- 新增蓝色、粉色、绿色、黄色、紫色五主题完整终端 + SFTP 布局预览（真实 Token + 应用 CSS），以及关系图表、蓝/粉同源对照与五主题矩阵。

---

## [未发布] 2026-11-06 · 深色中性骨架与主题表达分离（初版，已被后续关系校准修正）

- 深色模式不再用 Seed Hue 生成大面积中性色：新增独立中性骨架（Terminal Ink / Workspace Canvas / Toolbar / Card / Raised / Overlay），工作区、卡片、工具栏、终端的基础色完全中性。
- 新增深色 Tint Influence 映射：主导航保高 tint，二级导航中低 tint，工作区/卡片/工具栏/终端接近零；主题身份改由导航、选中项、按钮、焦点和左上角局部柔光承担。
- 工作区默认改为「中性基底 + 左上角 corner 柔光」（A 实色 / B 极弱渐变 / C 角落柔光比较后选择 C）；深色不再使用全幅 135° 渐变，浅色渐变保持不变。
- 深色 Tone 层级重新拉开：终端 ≈15.6、工作区 ≈20.8、工具栏 ≈21.7、二级导航 ≈24.2、卡片 ≈25.8、主导航 ≈28.2、抬升卡片 ≈28.4；解决右侧灰暗、卡片层级不够和顶部暗色横条。
- 终端改用独立中性 Ink：tone ≈15.2–15.6、HCT Chroma 实测 ≈0.9–1.4，不随工作区 Atmosphere 一起提高；ANSI 16 色/256 色/TrueColor 与 xterm 实例逻辑不变。
- 新增连续 hue 权重 `darkHueWeight()` 与 OKLCH tint 合成；不为单色写死，HCT Tone/Chroma 与 OKLCH Chroma 分开计算与标注。
- 浅色分支未改动；对 8 个种子的浅色关键 Token 做调整前后对比，0 差异。
- 新增深色中性骨架、OKLCH/HCT 分层、A/B/C 策略、终端独立色度测试；生成真实组件设置页/终端页截图、六主题矩阵与前后参数对照图。

---

## [未发布] 2026-11-06 · 深色模式明度抬升与色彩克制优化

- 深色映射改为「先抬 Tone、再小幅加 Chroma」：默认参数下主导航 ≈29.8、二级导航 ≈26.9、工作区 ≈23.5、卡片 ≈28.4、抬升卡片 ≈31.0，解决右侧工作区与卡片共同灰暗的问题。
- 深色 TINT_LEVELS 独立校准：二级导航/工具栏/工作区/卡片的 Chroma 配置上限调整为 ≤12 / 9 / 9 / 3.2，Theme Atmosphere=100% 时仍受上限约束；浅色参数保持不变。
- 红色 #F15E6A 等低色度种子不再呈现大面积深灰褐：工作区在抬高明度的同时只小幅增加 Chroma，呈现深酒红灰/蓝灰等主题中性色，而不是高饱和彩色。
- 深色终端改用独立低色度调色板：tone 稳定在 ≈20.5–20.9，Chroma 配置上限 3.0（实测 ≤3.4），不随工作区 Theme Atmosphere 一起提高；ANSI 16 色、256 色与 TrueColor 语义不变。
- 主导航与工作区 Tone 差从约 9 缩小到约 6，顶部工具栏只比工作区高约 1 tone，避免暗色横条和突兀断层；OKLCH 环境渐变继续使用原有方向与明度差，端点随新基础色自动重算。
- 新增深色 Tone/Chroma 克制范围、卡片层级、导航-工作区落差与终端独立色度测试；生成深色多主题验收截图。

---

## [未发布] 2026-11-06 · 三级色彩表达与 Ambient Gradient 环境渐变

- 将界面颜色表达拆成三级：主导航/选中项/主按钮/焦点为一级，二级导航/工具栏/工作区为二级，卡片/表单/输入框/弹窗/表格/终端为三级；同一主色相只改变色度与明度，不再让所有区域使用接近的色浓度。
- 重新设计深浅映射：浅色工作区保留低色度主题倾向并停止回退纯白/普通灰；深色工作区抬离近黑，主导航改为中深色主题色而非高饱和近黑青，卡片比工作区略亮。
- 新增 Ambient Gradient 生成：从语义表面色出发，在 OKLCH 中生成同色系 5 停靠点 ramp，色相偏移 ≤ 8°、浅色明度差 0.024–0.030、深色 0.016–0.022，并逐点做 sRGB 色域映射；不新增固定渐变颜色表。
- 仅主导航（180° 纵向）与普通工作区（135° 左上→右下）使用渐变；二级导航、工具栏、卡片、表单、弹窗与 SSH 终端保持实色。Solid 使用不透明停靠点，Soft / Clear 使用对应材质 Alpha 合成。
- 渐变停靠点纳入文字、focus ring 与 strong border 的对比度检查；无渐变时组件通过 `var(--gradient, var(--base-bg))` 自动回退基础语义色。
- 移除首页不受控的装饰光斑与叠层渐变，改由统一的 `--workspace-gradient` 承担工作区氛围；新增三级层级、渐变范围/色相/色域、端点对比度测试与更新后的预览图。

---

## [未发布] 2026-11-06 · 动态配色明快度与终端同步优化

- 修复高 `atmosphere` 下浅色主题灰蓝、低饱和的问题：不再继承 MCU Secondary / Neutral Variant 的低 chroma，只取其 hue；区域 chroma 改为「种子饱和度 × 角色比例 × atmosphere」单次计算，移除重复的 `chromaFactor` 削弱。
- 提高导航/工作区/卡片的角色色度上限，并让高氛围强度下的浅色表面轻微降低 tone，使 HCT 在 sRGB 色域内真正渲染出蓝白、绿白等有色表面，而不是被高 tone 压成近白。
- 重新校准 Solid 表面：外壳、导航、工作区、卡片、浮层、输入框、终端全部输出不透明 hex；清理 `bg-slate-*/opacity`、`color-mix(..., transparent)` 等会在 Solid 模式露出下层/桌面的表面背景。
- 终端同步：跟随模式现在使用应用主题的明暗与种子色，光标/选区使用应用 accent；ANSI 16 色与独立终端方案保持不变，关闭跟随即可完整使用终端方案自身的背景/光标/选区。
- 新增高氛围色度门限、四层明度层级、Solid 不透明表面与终端 accent 同步测试；文档同步更新。

---

## [未发布] 2026-11-06 · Arc 风格动态配色与材质系统

- 配色引擎从固定灰阶 / OKLCH 改为 Material Color Utilities 的 HCT + Tonal Spot：用户种子色自动生成主导航、二级导航、工作区、卡片、交互、文字与状态六类语义区域，深浅模式分别推导。
- 新增 Theme Atmosphere（0–100）与 Surface Separation（0–100）：前者只控制大面积背景色度，后者只控制区域层级差；交互强调色保持独立可辨识。
- 所有前景/背景组合加入 WCAG AA 对比度保护；状态色使用固定语义色相，红色/绿色主题不再破坏错误、成功状态的辨识度。
- 新增 Solid / Soft Glass / Clear Glass 材质模式；原生毛玻璃仍由 macOS NSVisualEffectView 与 Windows Acrylic 提供，无原生 backdrop 的平台自动回退 Solid，前端不再重复模拟全窗口模糊。
- 终端配色与应用主题解耦：终端背景始终不透明并按终端调色板明暗染色，ANSI 16 色、SSH 会话与 xterm 实例不受主题切换影响。
- `uiSchema=4` 迁移旧外观（`colorIntensity → atmosphere`、`glassEnabled → materialMode`），保留种子色、终端主题与字体；启动首帧使用主题缓存，避免默认主题闪烁。
- 新增动态配色、对比度、材质降级、迁移与首帧缓存测试；开发文档见 `docs/theme-system.md`。

---

## [未发布] 2026-11-05 · 主机密钥覆盖按钮修复

- 只读 OpenSSH 来源的旧记录不再使覆盖按钮禁用；明确核对后写入应用独立信任记录，源文件保持不变。
- 应用信任记录优先，轮换后的旧密钥拒绝连接；所有来源中的撤销记录继续生效。
- 增加只读来源轮换、不改源文件、拒绝旧密钥及撤销优先的组合测试。

---

## [未发布] 2026-11-05 · 快速连接亮度与首页布局

- 快速连接使用高不透明度主题底色，避免透出深色遮罩而发灰；关闭玻璃时使用纯色。
- 空终端页显示首页时隐藏空工作区占位，避免首页只占部分高度。
- 首页内容自然撑高并统一滚动，按实际内容宽度调整单/双栏，避免小窗口或缩放后卡片被截断。

---

## [未发布] 2026-11-05 · 窗口拖动与浅色渲染修复

- 修复 Windows 窗口按钮容器覆盖整个拖动区的问题，只将右侧按钮标为不可拖动。
- Windows 外框取消 CSS 圆角裁切，交由系统处理；浅色终端关闭阴影、优化不透明渲染和字重。
- 关闭毛玻璃时浅色界面使用更明亮的纯色底。

---

## [未发布] 2026-11-05 · 浅色终端可读性修复

- 修复浅色背景与旧浅色正文混用导致输出不可读的问题，正文按实际背景校验对比度。
- 启用 xterm 最低 4.5:1 对比度保护，覆盖 ANSI、亮色与显式颜色字符。
- 透明终端保留实际背景 RGB，避免使用透明黑色计算浅色背景对比度。

---

## [未发布] 2026-11-05 · 跨平台主机确认与标题栏修复

- 首次信任与密钥变化改为内置弹窗，修复 Windows 原生 WarningDialog 仅返回 Ok 导致无法信任的问题。
- 后端一次性请求绑定主机及公钥；明确核对后可覆盖应用自身记录，撤销及只读冲突继续拒绝。
- 主机信任管理增加搜索、分页及限高滚动。
- Windows 改用应用自绘标题栏，顶栏材质随主题毛玻璃开关切换。

---

## [1.1.1] 2026-11-04

- 新增侧栏悬停预览与固定展开切换，不推动主内容和终端尺寸。
- 补齐主页、服务器列表/分组、设置、隧道及弹窗毛玻璃覆盖。
- 修复共用按钮挤压换行，改善窄窗口表单和工具栏布局。
- 包含下列 SSH 连接体系升级与 UI 2.0 调整。
- 验证限制：UI2 字体预算仍超限；三平台桌面及真实远端联测未执行。

---

## [未发布] 2026-11-04 · SSH 连接体系升级

- 新增 known_hosts 强制校验、首次显式确认、密钥变化拒绝及安全设置中的信任管理。
- 新增最多五级 SSH 原生跳板链，各跳独立认证，统一终端、SFTP、状态采集和隧道连接路径；提供取消和链资源清理。
- 新增本机 SSH Agent、身份过滤与公钥信息查看，Windows 使用 OpenSSH 命名管道。
- 新增 OpenSSH Config 预览和原子快照导入、冲突差异及跳板引用解析；不执行配置命令。
- SQLite 自动迁移 SSH 扩展配置，保留旧字段；临时密码短语与主机信任不进入配置包。
- 增加主机校验、真实多跳握手/清理、Agent 签名、解析规则和存储迁移测试。
- 限制：不支持 Match、ProxyCommand、LocalCommand、CanonicalizeHostname；内联 ProxyJump 地址需先转换为 Host 别名。首次确认在中止握手后重试。

---

## [未发布] 2026-11-04 · UI 2.0 评审回归修复

### 变更

- **恢复原信息架构**：移除「概览」「传输」两个新增顶级导航与独立页面；传输历史仍从工作区右侧面板进入。命令面板不再提供 `overview` / `transfers` 路由，`ui.navigate` 仅把这两个旧值作为兼容别名映射到工作区（传输额外打开右侧面板）。`SettingsView` / `ServersPage` / `TunnelsPage` 三个四行转发壳删除，页面组件由 `App.vue` 直接挂载。
- **Token 单一来源**：`tokens.css` 成为颜色基色、间距、圆角、字体、动效与布局变量的唯一定义处；`style.css` 删除整套重复颜色 / 尺度定义，只保留组件与布局规则；`semantic.css` 负责深浅语义色，`components.css` 只保留派生别名。`--radius-lg` 统一为 8px、`--radius-xl` 统一为 12px。
- **字体兜底改为 Geist**：静态 `--font-sans` / `--font-mono` 与 `engine.ts` 的默认栈均以 Geist Sans / Geist Mono 开头；`system` 选项仍显式回落到系统中文字体，不再出现 Inter / Sora / JetBrains Mono 兜底。
- **基础组件落地**：`App.vue` 解锁页、服务器列表 / 工具栏 / 编辑弹窗、凭证弹窗、隧道页 / 卡片、设置各分区、SFTP 与传输面板、终端连接浮层均改用 `Button` / `Input` / `Field` / `Select` / `Textarea` / `Dialog` / `ErrorState` / `EmptyState`。生产模板中不再使用 `.btn` / `.input` / `.select` / `.textarea` 旧类。
- **继续拆分大组件**：`App.vue` 903 → 683 行（导航抽到 `AppSidebar.vue`、解锁页抽到 `AppUnlock.vue`）；`SettingsPage.vue` 2191 → 61 行，拆成通用 / 快捷键 / 连接说明 / 外观 / 凭证 / 安全 / 调试 / 日志 / AI 记录 / 导入导出 / 关于 11 个分区组件；`TerminalView.vue` 1582 → 1173 行，补全状态与输入跟踪抽到 `useTerminalCompletion.ts`，命令解析抽到 `terminal/command-line.ts`。
- **无障碍补齐**：服务器行改为真实 `<button>`（Enter / Space 可连接，Shift+F10 打开菜单）；导入导出密码框使用显式 `label[for]`；终端 / 服务器右键菜单使用 `role="menu"` + `menuitem`、方向键 / Home / End / Esc 与打开后聚焦；`Dialog` 增加初始焦点、Tab 焦点陷阱、Esc 关闭与关闭后焦点恢复。

### 修复

- **本机 zsh 提示符与光标错行** —— 现象：oh-my-zsh / powerlevel10k 的 `➜  ~ ` 在上一行，光标和输入落在下一行；原因不是 WebGL，而是语义着色器为了按行着色，把「不以换行结尾」且不匹配 `$` / `#` 的 ANSI zsh 提示符留在 `pending`，直到下次回车才写入 xterm。修法：半行中出现 ANSI/OSC/CSI/C0 控制序列时立即原样透传，不参与语义着色；新增无换行 ANSI zsh prompt 和连续回车的回归用例。通过运行中调试接口实测：首行 `cursorX=5,cursorY=0`，回车后 `cursorX=5,cursorY=1`，均紧跟提示符。
- **语义着色默认值恢复**：新安装的 `terminalColorMode` 从 `off` 恢复为 `auto`（前端 store、JSON 设置存储、SQLite 设置存储三处一致）；显式 `off` / `vivid` 仍原样保留。
- `types.ts` 删除重复的 `tabBarPlacement` 字段声明，`Fonts.uiFont` 注释改为 Geist。

### 验证

- 前端 16 个测试文件 / 24 个用例通过：新增服务器连接与删除确认、隧道创建、SFTP 目录进入与终端目录同步、设置保存、终端节点常驻、IA 契约、Token 单一来源、Dialog / 菜单 / 服务器行 / 导入导出无障碍等用例。
- Go 全量测试通过，并新增 `terminalColorMode` 默认 `auto`（JSON 新装 / 旧配置缺字段 / SQLite 新库）与显式 `off` 保留的存储测试。
- `scripts/verify-ui2.sh` 通过：CSS 79,392 B（gzip 15,868 B）、入口 JS 409,107 B（gzip 128,134 B）、字体 12 个、dist 1,384,448 B；vendor 拆分为 `vendor-vue` / `vendor-xterm` 两个可缓存 chunk。迁移守卫在没有 `rg` 的环境下自动回退到 `grep`，避免漏扫。
- 本机 Node 22.11 低于 Vite 7 / Vitest 5 要求的 22.12，验证使用 `.nvmrc` 中的 22.23.4；`verify-ui2.sh` 已加入版本前置检查。

## [未发布] 2026-11-03 · UI 2.0 P4 交互、自动化与性能门禁

### 新增

- 命令面板新增最近记录、服务器 / 操作 / 导航分组、标题 / 地址 / id 搜索、1000 台服务器场景的 80ms 防抖与 60 项渲染上限；执行成功后才记录最近命令。
- 新增唯一快捷键注册表与设置页「快捷键」速查，统一处理输入法组合、表单焦点、标签切换、侧栏开关和工作区作用域。
- 新增统一 Toast、Skeleton、ErrorState 基础组件；错误 Toast 6 秒、普通 Toast 3 秒，同一操作按 key 去重。

### 变更

- 关键布局、导航、标签、菜单、设置分区与命令项改用稳定 `data-ui` / `data-section` / `data-tab-id` / `data-testid`，`ui.layout` 优先查询语义选择器。
- 动效收敛到 100–180ms；`prefers-reduced-motion` 下同时关闭时长与位移。
- Geist 字体从全字符集入口改为 Latin 400/500/600 子集：构建产物字体由 30 个降至 12 个，CSS 由 138 KB 降至 77 KB，dist 为 1.34 MB。

### 验证

- `scripts/verify-ui2.sh` 通过：Vue 类型检查 / Vite 构建、`go test ./...`、CSS 77,371 B（gzip 15,950 B）、JS 889,246 B（gzip 260,449 B）、12 个字体文件、dist 1,335,296 B。
- 三平台 WebView、IME、高 DPI 与真实窗口快照仍由发布流水线和人工冒烟清单确认，不以本机静态门禁替代。

## [未发布] 2026-10-29 · 设置页「连接说明」分区 + 「登录后自动采集」开关

### 新增

- 设置页新增 **连接说明** 分区（左侧菜单「通用」下方）：按代码梳理 SSH 登录一台服务器后会自动执行什么、采集什么信息——`internal/sshx/sysinfo.go` 的采集脚本（`sh -c` 在独立无 PTY 会话上周期运行 `cat /proc/stat` / `cat /proc/meminfo` / `df -k -P` / `cat /proc/net/dev` / `ip -o -4 addr show` / `cat /proc/uptime`）、SSH 保活请求 `keepalive@openssh.com`、SFTP `Getwd`，以及被动目录同步，并逐项标注用途。
- 该分区内置 **「登录后自动采集系统信息」开关**（`Settings.sysInfoCollectEnabled`，默认开启）。关闭后登录服务器不再向远端执行任何采集命令，并明确告知受影响（底部状态栏 / 系统看板无数据）与不受影响（终端、SFTP、传输、隧道、重连、目录同步）的功能。

### 变更

- `Settings` 新增字段 `sysInfoCollectEnabled`：JSON 与 SQLite 两套设置存储在「无字段时补默认（开启）」的迁移路径上保持一致，`settings.schema` / `settings.reset` 的默认值抽到 `ctlDefaultSettings`。
- `Manager.StartSysInfoCollector` 依据该设置放行：关闭时直接返回并停掉可能残留的采集器；`App.SaveSettings` 在关闭时调用新增的 `Manager.StopAllSysInfoCollectors()`，已连接会话立即停止采集。
- 前端 `TerminalView` / `ServerStatusBar` 在开关关闭时不发起采集；底部状态栏改为显示「已关闭」占位提示。

## [未发布] 2026-10-22 · 修复 WebGL 在界面缩放下的顶部缺行（画布位图 / 网格一致性）

### 修复

- **界面缩放 ≠ 100% 时终端顶部前几行看不到，且缓冲区已在顶部无法上滚** —— 现象：把界面缩放调到 110% / 125% 后，终端最上面几行不显示，往回滚也没用（缓冲区 `baseY` 已在顶部）；关掉「WebGL 硬件加速」即恢复正常。
  原因：`@xterm/addon-webgl` 用 `ResizeObserver` 的 `devicePixelContentBoxSize` 给 canvas 定尺寸，**Chromium 的这个值含 CSS zoom**（zoom=1.25 实测位图 990×553，渲染器内部网格只有 792×442）；而 `_setCanvasDevicePixelDimensions()` 只改位图，**不重设 GL viewport / resolution uniform / 顶点归一化**（那三件事只在 `handleResize()` 里做）。于是整幅网格被画进超大位图的左下角（WebGL 原点在左下），内容整体下移、比例也不对——缓冲区与 DOM 几何都正确，只是像素错位，`refresh()` / `clearTextureAtlas()` 都纠不回来。
  修法：新增 `frontend/src/utils/xterm-webgl-zoom.ts`——zoom ≠ 1 时丢弃观察器上报的（含 zoom 的）尺寸，位图保持 `handleResize()` 按未缩放网格算出的尺寸；并在每次 `fit()` / 缩放切换后自检「位图 == 网格」，不一致就重跑 `handleResize()`，把 viewport / uniform / 顶点一起补齐（其它引擎或 addon 私有实现变化时也能自愈）。因此**不再因为缩放 ≠ 100% 就关掉 WebGL**：任意档位都走硬件加速，只有补丁打不上（addon 私有 API 变化）且缩放 ≠ 100% 时才回退 Canvas。

### 变更

- `TerminalView.vue` 的 WebGL 开关策略：由「仅 `uiScale = 100` 启用」改为「补丁可用即启用」，缩放变化时重新对齐位图并重新评估；上下文丢失 / 环境不支持仍降级 Canvas 并提示。
- 新增渲染诊断日志：`[webgl] enabled …` / `[webgl] uiScale→…% …`，含 `uiScale / dpr / grid / bitmap / css / rect / cols×rows / viewport / scrollTop / baseY`，便于线上取证（日志级别 `debug`）。

### 验证

- 无头 Chromium（Blink）：`uiScale` 80/90/100/110/125/150 六档，逐档对比 DOM 与 WebGL 的「首行 y / 末行 y」，修前 125% 顶部空 74px、150% 空 156px，修后与 DOM 基线一致（±2px）。
- WKWebView（macOS 真实渲染引擎）：100% / 125% 两档无回归（WKWebView 不支持 `device-pixel-content-box`，观察器本就自动断开，位图由 xterm 自己按 device 尺寸设定）。

## [未发布] 2026-10-01 · M10 收尾 · `terminal.sudo` 挂起/超时后的清理保证（独立 SFTP 通道 + `cleanupOnly`）

工具面数量不变（仍是 89 个）：本次只给既有工具 `terminal.sudo` 加了一个可选参数 `cleanupOnly`，并新增一条**宿主 op**（`sudo.cleanup`，不进 MCP 工具清单，只在 `internal/apphost/debug.go` 的 `Handle` 里分发）。

### 修复

- **命令挂起 / 超时时，含明文密码的临时文件会留在目标机** —— 现象：`get_hd_smartinfo -d 1 -i 1 | head -45` 这类**永不返回**的命令下，`terminal.sudo` 超时返回后目标机残留 `/tmp/.ding-sudo-<12hex>`（**含明文密码**）。原因：当时唯一的兜底清理是「在**同一条远端 shell** 里再发 `rm -f` + `ls` 校验」，它排在用户命令之后 —— 用户命令不返回 ⇒ 那行从未执行；随后靠 Ctrl-C 恢复终端时，tty 在 SIGINT 时会**丢弃输入队列**，兜底行同样丢失。修法：新增**独立通道清理**（宿主 op `sudo.cleanup`：复用会话已有的 SFTP 客户端 `Remove` + `Stat` 复核，不新建连接、不走终端、不走 shell），`terminal.sudo` 的收尾**无论成功 / 失败 / 超时都先走它再返回**；`cleanedUp` 的语义明确为「结束时会话确认文件已不存在」= 原 shell 的 `ls` 校验 **或** 独立通道的 `verified`（两者取或），因此超时批次也可以是 `cleanedUp=true`；独立通道失败时 `cleanedUp=false`，`hint` 给出**残留文件完整路径**与中文处置建议（删除 + 轮换该账号密码）。
- **清理入口只能是本工具生成的路径**：`cleanupOnly` 与 `sudo.cleanup` 都严格要求 `^/tmp/\.ding-sudo-[0-9a-f]{12}$`（并仍要过既有远端写入白名单），其它路径一律中文 `ErrBadInput` —— 否则「事后清理」就成了任意删除原语。宿主拿不到 SFTP 客户端时返回 `verified=false` + 中文原因，**不退化**成「再发一条终端命令」（终端可能仍被挂起的作业占着）。

### 新增

- `terminal.sudo` 的返回新增 `cleanupAttempted` / `cleanupVerified` 两个布尔字段（独立通道是否被调用 / 是否复核确认删除）；成功路径也会调用独立通道，用于如实报告。
- `terminal.sudo` 的可选参数 **`cleanupOnly`**（string，远端路径）：用于超时 / 异常后清理可能残留的临时文件；传了它 ⇒ 跳过取密码与 stage，只对指定路径执行独立清理，返回 `{ok, sudo:false, cleanupOnly:true, sessionId, path, removed, existed, cleanupAttempted, cleanupVerified, cleanedUp, hint?}`，幂等（文件不存在也算成功）。不接受任意路径，只接受本工具生成的 `/tmp/.ding-sudo-<12hex>` 形态；仍受 `sudo.credential` 能力位与两段式确认（action 仍是 `terminal.sudo`）约束；它不碰终端，因此也不占「同会话串行化」名额。相应地 `command` 不再是 schema 的必填项（cleanupOnly 模式下不需要），主流程缺 `command` 时仍报中文 `ErrBadInput`。
- 新增宿主 op **`sudo.cleanup`**：入参 `{sessionId, path}`，返回 `{removed, existed, verified, reason}`；校验顺序为「路径形态 → 白名单 → 会话 SFTP 客户端」。
- 用例：`internal/debugsrv/sudo_test.go` 新增「超时批次走独立清理」「独立清理先于终端兜底」「独立清理失败给残留路径」「独立清理通道报错」「cleanupOnly 正常 / 幂等 / 非法路径拒绝 / 白名单 / 能力位与两段式 / 不占终端名额」等；`internal/apphost/sudo_cleanup_test.go`（包 apphost）覆盖宿主侧的入参校验、白名单、「拿不到 SFTP 客户端」的降级行为，并用 **pkg/sftp 的内存 SFTP 服务端**（`InMemHandler` + 管道）跑一遍真实的 `Remove` / `Lstat` 协议路径，验证 `verified` 的判据与幂等语义；全部用替身，**不连任何主机**。

### 文档

- `README.md` 的 `sudo.credential` 小节补「超时 / 异常时的清理保证」（独立 SFTP 通道 + `cleanupAttempted` / `cleanupVerified` 语义 + `cleanupOnly` 用法 + 失败时 hint 给出残留路径 + SFTP 不可用时的行为），并更新工作流程第 5–7 步、`terminal.sudo` 的工具说明与「不做的事」里的审计 args 清单。

---

## [未发布] 2026-10-01 · M10 · sudo 凭证提权（`terminal.sudo`）与超长命令回显修复

工具面从 88 个扩到 **89 个**（`tools/list` 与 `GET /v1/tools` 的 `count` 一致）。登记口径：**63** 个由各域在能力注册表里登记（`AllRegisteredToolNames()` 的长度 = ui 25 + 应用控制 25 + SFTP/终端 12 + `terminal.sudo` 1），另外 **26** 个内置工具的能力登记在内置表 `toolCaps` 里（`screenshot` 不登记能力位）。数量请以运行时为准，不要写死；新增用例 `TestToolEntryRegistryConsistency` 会在「有条目但没登记能力位」时直接失败。

### 新增

- **能力位 `sudo.credential`（默认关）**：允许 AI 在**当前会话对应的服务器**上，用**应用里保存的密码**执行 `sudo -i`。前置条件是**两个**：能力位本身 + 「允许读取敏感数据」（`secrets.read`，密码只从凭据库读）——缺哪一个都会在中文拒绝原因里明确写出来；后端 `NormalizeDebugSettings` 与前端 `normalizeDebug` 都做了 `!allowSecrets ⇒ capSudoCredential=false` 的兜底（与 `capSecretWrite` 同款），前端「设置 → 调试模式 → MCP 能力」里**开启需二次确认**（`GetCapabilities().requiresConfirm` 已列出 `sudo.credential`）。
- **工具 `terminal.sudo {id, command, timeoutMs?, idleMs?}`**：能力位 `sudo.credential` + `terminal.input`，**每次执行仍需两段式确认**（`confirm.prepare(action="terminal.sudo")`，`ConfirmableActions()` 已包含它；未带 token 时返回中文提示且不执行）。工作流程：先过远端写入白名单（`/tmp` 不在白名单 → 中文拒绝并说明去哪加）→ 生成**随机**临时文件 `/tmp/.ding-sudo-<12 位十六进制>` → 宿主按 `servers.get includeSecrets=true` 的同一条路径（`App.GetServers` → 存储层解密）取出保存的密码，用**会话已有的 SFTP 客户端**写入 `<密码>\n`（返回值只有 `{ok, path, bytes}`）→ 在**同一条 shell 命令**里 `chmod 600`（失败则不执行 sudo）→ `sudo -S -p '' -i -c '<command>' < <临时文件>`（密码**只经由文件重定向**，绝不进命令行）→ `rm -f ... || true` → `ls -d` 校验删除 → 打印退出码标记 → **输出净化**（宿主把输出里出现的密码换成 `***`）→ 返回 `{ok, sudo:true, sessionId, matched, reason, elapsedMs, exitCode?, tempFile, cleanedUp, scrubbed, output, outputBytes, truncated, hint?}`。标记没出现（超时 / 静默 / 流关闭 / 取消）或发送失败时都会再补一次清理并如实报告 `cleanedUp`；密码错误 / 无 sudo 权限 / 缺 TTY / 没有 sudo 命令 / chmod 失败都给中文 `hint`，超时与静默**不是协议错误**。与本机终端、没有保存密码的服务器、以及调用方直接传入密码（**永不接受**，`schema` 里也没有 `password` 字段）都不兼容；并发与 `terminal.run` / `terminal.expect` 共用同一份「同会话串行化」名额。

### 修复

- **`terminal.run` 超长命令的回显定位失准** —— 现象是命令比终端宽度长时，PTY 把回显折行成交错的多段（可见文本里多出换行），旧的「逐字命中 + 取其后第一个换行」定位失败或只命中残片，于是回显残片会参与 `expect` 匹配、`echoSkippedBytes` 也不准（实测发现）；原因是回显被折行后命令文本在可见文本里不再连续，而 `\r` 归一化又让片段被拼接。修法是给回显定位加一个**长度口径**（`terminalEchoEndByLength` / `echoMatchAt`：逐字节与命令比较、比较时**跳过折行插入的换行**，中文按字节数算与显示宽度无关；候选起点扫描上限 4096 字节），与既有换行口径 `terminalEchoEndByNewline` **取较大者**（折行时换行口径低估、不折行时它最精确），两者都定位不到则退回「不跳过」的保守行为；顺带在 `waitTerminal` 里加了「回显已收尾在换行处就不再重复定位」的短路，避免在大段输出上反复做字节比较。

### 文档

- `README.md`：「能力位与两段式确认」一节补上 `sudo.credential` 的**设计说明**（开启条件、完整工作流程、边界与「不做的事」、以及用 `sudoers.d` 配置**免密 sudo** 或「密钥 + 受限 sudo 规则」的更安全替代），工具总览与能力位表更新为 89 个工具 / 10 个能力位，「SFTP 与终端自动化」补 `terminal.sudo` 与超长命令回显定位的说明；顺带修正工具登记口径的表述（此前写成「87 个登记在能力注册表」，实际 `AllRegisteredToolNames()` 的长度是 63：各域注册 62 + M10 新增 1，26 个内置工具在内置表 `toolCaps` 里）。

---

## [未发布] 2026-10-01 · MCP 扩展与收尾（能力位 / 确认 / 审计 / ui.* / 应用控制 / SFTP）

工具面从最初的内置 21 个扩展到 **88 个**（`tools/list` 与 `GET /v1/tools` 的 `count` 一致）。登记口径当时写成了「87 个登记在能力注册表里（`AllRegisteredToolNames()` 的长度）」，实际是 **62**（ui 25 + 应用控制 25 + SFTP/终端 12）—— 另外 26 个内置工具的能力登记在内置表 `toolCaps` 里；该表述已在 M10 的文档更新里改正，并补了注册表一致性用例。数量请以运行时为准，不要写死。

### 新增

- **能力位（权限内核）**：把「调试模式总开关」细分成 9 个能力 —— `read`（永远允许）、`terminal.input`（默认开）、`ui.write`、`config.write`、`secrets.read`、`secrets.write`、`fs.remote.write`、`lifecycle`、`eval`（沿用「允许执行 JS」）。宿主在每次工具调用前校验，被拒时返回**中文原因 + 怎么开启**（不是协议错误）。前端在 **设置 → 调试模式 → MCP 能力** 逐项开关。
- **两段式确认**：破坏性 / 不可逆操作必须先 `confirm.prepare`（返回一次性 token，绑定 `action` + `args`，默认 120 秒过期）再 `confirm.commit` 或把 token 交给写工具；**可逆 / 高频操作（`send_input`、`terminal.run/expect`、`eval_js`、全部 `ui.*` 写入）只受能力位约束，不需要 token**。开启敏感能力本身（`capability.enable`）也在需要确认的清单里。
- **审计与「AI 记录」页签**：每次调用（成功 / 被拒 / 失败）都留一条结构化记录（来源 `mcp|http|ui`、工具名、能力位、耗时、结果、**已脱敏**参数、可否撤销），环形缓冲保留 500 条；应用内可过滤查看，设置类改动（`update_settings` / `set_log_options`）可一键**撤销**（保留最近 20 份改动前快照）；AI 侧对应 `app.recent_calls`。
- **UI 观测与实验（25 个 `ui.*` 工具）**：观测用 `ui.layout` / `ui.query` / `ui.styles` / `ui.tokens` / `ui.hit` / `ui.tree` / `ui.console` / `ui.revision` / `ui.screenshot` / `ui.elementShot` / `ui.snapshot` / `ui.diff` / `ui.assert` / `ui.store`；实验用 `ui.setToken` / `ui.injectCSS` / `ui.storePatch` / `ui.navigate` / `ui.window` / `ui.reload` / `ui.click` / `ui.type` / `ui.key` / `ui.hover` / `ui.scroll`（需要 `ui.write`，**只改内存态、不写设置、刷新即还原**）。资源：`ui://layout`、`ui://view`、`ui://console`；`ui.layout` 会给出可见元素之间的**非预期重叠**，`ui.diff` 走**像素 + 指纹双口径**（16×16 分块 + 差异区域 + diff 图 + 指纹 added/removed/changed），快照落盘在日志目录的 `ui-snapshots/`。
- **应用控制面（25 个工具）**：`servers.list/get/create/update/delete/duplicate/moveGroup/export/import`、`credentials.list/create/update/delete/attach`、`tunnels.list/create/update/delete/start/stop`、`settings.schema` / `settings.reset`、`app.commands` / `app.command`、`app.quit`。读类默认脱敏（`includeSecrets=true` 需要 `secrets.read`，返回里用 `plaintextFields` 标出明文），写类需要 `config.write` / `secrets.write` / `lifecycle` 且基本都要两段式确认；`app.command` 的权限**按命令实际影响**判定（只读 / 切换视图类命令不需要能力位与 token，未登记的命令按最保守策略处理）。
- **SFTP 与终端自动化（12 个工具）**：`sftp.list/stat/read/transfers/write/mkdir/rename/remove/syncCwd/cancel` 与 `terminal.run` / `terminal.expect`。远端写入要求路径是绝对路径、不含 `..`，且命中**白名单前缀**（路径段边界匹配；留空 = 一律拒绝），每个写操作都要两段式确认并提示「远端改动不可撤销」；`terminal.run` 支持 `expect` 正则、`wrapExitMarker` 退出码捕获、`idleMs` 静默窗口，`terminal.expect` 纯等待并用 `sinceMarker` 去重；两者共用「同会话串行化」（第二个并发调用直接拒绝，不排队）。
- **工具目录 `GET /v1/tools`**：一次返回全部工具的 `{name, capability[], confirmAction?, needsToken, description, inputSchema, hasStructuredOutput, outputSchema?}`，与 `tools/list` 同源且沿用同一个 Bearer 鉴权；`app.permissions` 同时新增 `toolCount`（当前工具总数，不写死）。
- **MCP 结构化输出（2025-06-18）**：13 个工具（`app_state`、`app.health`、`app.permissions`、`list_terminals`、`read_terminal`、`ui.layout`、`ui.diff`、`ui.query`、`sftp.list`、`servers.list`、`servers.get`、`settings.schema`、`app.recent_calls`）声明 `outputSchema`，并在 `tools/call` 里除文本内容外返回 `structuredContent`；`content[0].text` 保持原有格式，老客户端忽略新字段即可。

### 修复

- **切换能力位不再重启调试服务** —— 现象是保存设置 / 勾选能力位后正在连的 MCP / HTTP 客户端掉线，且端口为自动（`port=0`）时 `url` 直接变成另一个随机端口；原因是 `reloadDebug` 只要**任何**调试设置字段变化就 `stop + start` 调试服务；修法是只在**影响监听地址**的三个字段（`enabled` / `port` / `bindLan`）变化时重启，其余（能力位、`allowEval`、`allowSecrets`、CDP、日志相关）只更新内存兜底快照 —— 能力位本来就地生效（Gate 每次调用实时读设置）。`SaveSettings` 与 `SetCapability` 两条路径都汇入这一个函数，因此一处修复覆盖两处；测试断言切换能力位后底层 `debugsrv.Server` 实例、监听地址与 token 都不变。
- **`terminal.run` 的 `expect` 不再被「命令回显」假命中** —— 现象是命令里含 `echo ===DONE===`、`expect="===DONE==="` 时 1ms 就返回 `matched=true`，输出里只有被回显的命令行；原因是 PTY 先回显命令行，而匹配直接在累积文本上做；修法是匹配前先定位**本次发送命令的回显**（`terminalEchoEnd`：只取命令第一行，找到它之后回显行的结束位置并跳过），只在回显之后的文本上匹配，返回里新增 `echoSkippedBytes` 如实说明跳过了多少字节；终端不回声或无法定位回显时不做跳过，行为与之前一致。描述里补充推荐写法：让标记在远端运行时展开（`D=__MCP_$(echo DONE)__; …; echo $D` + `expect="__MCP_DONE__"`）。
- **`terminal.run` 未给 `expect` 时不再把回显当「真实输出」** —— 现象是「先回显、后静默、最后才吐输出」的命令（如 `docker ps`）在 ~400ms 就被判定跑完，只拿到回显；原因是首个输出块（其实是回显）把静默窗口从 1500ms 宽限重置成 400ms；修法是只有出现**回显之外的真实输出**才把窗口收窄到 `idleMs`，并要求真实输出前保持更宽的宽限；同时新增 `idleMs` 参数（默认 400，范围 50–60000）并在返回里回显。
- **`ui.diff` 的 `styles` 指纹细化到 CSS 属性级** —— 现象是 `changed[].fields[]` 里 `styles` 整份 `from`/`to` 丢回来，AI 还得自己做一遍 diff；修法是只报**真正变化**的属性（`{name:"styles", styles:{属性:{from,to}}}`，没有属性变化就不出现该条目），其余字段（`rect` / `classes` / `zIndex` / `visible` / `text`）形状不变。
- **`sftp.read` 对 `/etc/hostname` 报错的排查结论：不是缺陷** —— 完整错误是 `打开远端文件 "/etc/hostname" 失败：远端路径不存在（file does not exist）`（`isError=true`）；同会话 `sftp.list /etc` 的 147 条里确实没有 `hostname`（该 Arch 主机没生成这个文件），`/etc/os-release`（符号链接）、`/etc/passwd`、无换行结尾的文件、0 字节文件读取均正常，目录路径会返回「是目录，请用 sftp.list」。调用方需要注意：错误是 `isError=true` + 文本，不要当 JSON 解析。
- **`sftp.write` 写不出 0 字节文件** —— 现象是 `sftp.write{path:"/tmp/x", content:""}` 返回 `缺少写入内容（content / base64）`，而 `sftpWriteContent` 的中文提示里明确写了「写空文件请显式传 content: ""」；原因是宿主按 base64 的**值**是否为空判断有没有内容，而 debugsrv 侧总是把内容编码成 base64 传下来（空内容编码后正好是空串），于是「空内容」与「没给内容」被混为一谈；修法是改成按**键是否存在**判定（base64 优先、其次 content，空字符串是合法内容），并补了参数解释规则的回归用例（`internal/apphost/sftp_write_test.go`）。

### 变更

- **命令面板与 `app.commands` 同源**：命令定义抽到 `frontend/src/commands.ts`（`buildCommands(ctx)` / 类型 / 影响范围与能力判定），`App.vue` 的命令面板与 `debug/bridge.ts` 都从这里取，不再各维护一份清单（id 与行为不变）；`app.command` 的执行走同一份 `run` 闭包，不会再出现「面板里有、AI 报未知命令」。
- `initialize.instructions` 补充工具目录、结构化输出与回显相关说明。
- 测试与文档中的工具数量改为**注册表驱动**（`AllRegisteredToolNames()` / 工具条目表），不再写死数字；跨域的测试助手（`ensureAllRegistrations` / `toolsListFromMCP` / `toolNamesOf` / `assertRegistryToolsListed`）归位到 `internal/debugsrv/registry_test.go`。

### 文档

- `README.md`：新增「能力位与两段式确认」「审计与『AI 记录』页签」「UI 观测与实验」「SFTP 与终端自动化」四节，把「MCP（Streamable HTTP）」一节改成**按域分组的工具总览**（内置 26 / `ui.*` 25 / 应用控制 25 / SFTP 与终端 12，共 88）并补上 `GET /v1/tools` 与 `structuredContent` 的说明；HTTP 接口表补 `/v1/tools` 与 `/v1/ui/*`，资源表补 `ui://*`、`sftp://{id}/cwd`、`transfers://current`；「已知问题 → 其他限制」补上 Hub 通道满会丢事件、`ui.reload` 会重挂前端、非 dev 构建需先构建前端否则 `ui.*` 报未知操作、`wrapExitMarker` 的 POSIX 语义、`sftp.write` 不接受空内容。

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
