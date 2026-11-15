# 界面与 MCP 检查报告

检查日期：2026-11-15。未修改业务代码或持久配置。保留既有未提交修改。

## 实测范围

- 屏幕 1920×1080，内容区 1920×1002，DPR 1。
- 服务器、隧道、空工作区、全部 11 个设置分区：DOM 几何检查未检测到横向越界。此结果不能证明视觉对比度、文字裁切或滚动可达性完全正常。
- 新建服务器（560×592）、新建隧道（600×615）、新建凭证（520×363）弹窗均在视口内；表单按钮无视口越界。打开后取消，未保存。
- 实测 13 个 MCP 工具：app.health、app.permissions、list_terminals、list_servers、servers.list、credentials.list、tunnels.list、settings.schema、app.commands、sftp.transfers、ui.store（仅名称清单）、ui.revision、ui.console；均未返回协议错误或 isError。
- 当前终端清单为空，远端读写、实际终端输出、分屏、文件列表及真实传输没有实测；未执行远端命令。
- 此前同一工作区生产构建、42 个前端测试文件/185 项测试、go test ./... 均通过。本轮没有代码变更，未重复执行。
- macOS /v1/screenshot 实测 HTTP 501，因此没有完成截图级视觉验收。

## 优先修复

### P1：ui.store 缺少敏感数据隔离（代码确认，未读取真实密码）

frontend/src/debug/ui.ts 的 storeOp 允许读取任意已加载 store 及点路径，sanitize 仅限制深度/长度，没有按字段脱敏。servers store 持有 GetServers 返回的节点（包括认证字段），credentials store 持有凭证列表。internal/debugsrv/ui.go 的 uiStore 和 internal/apphost/debug.go 的 ui.* 透传也没有 secrets.read 检查；工具登记为永远允许的读取类。

影响：即使关闭 secrets.read，也可能经 ui.store 读取已加载到前端的认证数据。这里区别于用户明确开启 eval 后的完全控制能力：ui.store 本身是无需 eval/ui.write 的读取工具。

建议：默认只开放经过白名单投影的 store；按字段递归脱敏，并在选择子路径之前执行权限判定。添加关闭 secrets.read 时根对象、数组和直接 password/privateKey 路径均不可泄漏的测试。继续审查 ui.query/tree 的表单值返回。

### P1 待复现：前端与后端会话不一致

初始 /v1/state 返回 frontend.tabs=[]、terminalIds=[]，但 sessions 中有两个 connected SSH 会话；工作区显示 0 会话。TerminalView 卸载时会 disconnect，但页面重载会丢失内存标签且不一定执行 Vue 卸载。

不能据此认定正常关闭标签必然泄漏。建议分别复现关闭标签、连接尚未完成时关闭、HMR、页面重载；决定采用重连恢复、孤立会话回收或显式后台会话列表。先为后台连接建立可见性，避免统计误导。

### P2：通用确认提交无法保持所有工具语义（代码确认）

debugExecutor 的 ctlActionOps 将 action 直接映射为宿主 op：servers.duplicate 复用 servers.create，servers.moveGroup/credentials.attach 复用 servers.update，sftp.syncCwd 复用 sftp.write。通用 confirm.commit 无原始工具身份，可能失败或落到错误执行器。源码已有注释要求改用原工具携带 token。

建议确认记录绑定 tool+action+args，提交时调用原工具执行路径；或者明确拒绝不支持通用提交的动作，并在 schema/prepare 返回中给出下一步。不得将“准备成功”视为“可正确提交”。

### P2：能力目录存在不可执行的确认动作

app.permissions 实测列出 app.reload、sftp.upload；89 工具清单无 sftp.upload，debugExecutor 也无这两个 action 映射。ui.reload 是独立路径，不能视为 app.reload 通用提交已实现。terminal.sudo 同样没有通用 ExecuteAction 映射，需走原工具带 token。

建议统一生成工具、能力和确认执行目录，标明 tool-only 动作并检验所有 advertised action 的可执行性。

### P2：SFTP 大目录不完整

sftp.list 上限 500，仅返回 truncated，无 offset/cursor/filter，无法可靠读取其余条目。增加分页及稳定排序；不能仅提高上限。

### P2：macOS 截图能力缺失

screenshot_other.go 明确不支持非 Windows；本机接口返回 501。截图、元素截图、快照、像素差异等依赖应按平台声明支持状态，避免已注册看似可用。

### P3：文案与状态展示

- 安全页显示“未解锁”，同时“主密码未启用”，需核对异步加载与状态含义，不能直接判定加密功能故障。
- 调试说明显示 Markdown ** 符号；curl 示例运行界面出现转义引号。当前源码示例与运行界面存在差异，需要确认构建/热更新版本后修复。
- ui.navigate 不支持已有 home 页面；app.commands 也无 home 命令。
- 生产主 JS 约 552 KB，logo 约 1.57 MB；可拆分非首屏模块和压缩资源。

## MCP 尚缺的产品能力

以下指缺少专用、类型明确的工具，不代表不能借助 eval/终端绕行：

| 能力 | 当前状态 | 建议 |
|---|---|---|
| 分组增删改查 | 应用有 GetGroups/AddGroup/RenameGroup/RemoveGroup；无 groups.* 工具 | 增加 groups.*，与服务器移动分组分开 |
| 服务器连接测试 | 应用有 TestServer/TestServerLogin/TestServers；无专用工具 | servers.test，区分 TCP 可达与认证成功 |
| 上传/下载本地文件与目录 | 应用有 SftpUpload/Download/Folder；MCP 只有内容分片读写 | 增加任务式 upload/download，支持进度、取消与本地路径约束 |
| 主机信任管理 | 应用有读取/删除/解决信任冲突入口；无专用工具 | hostTrust.list/resolve/remove，提供指纹及确认绑定 |
| 标签激活、分屏、面板 | 有 open/close；无专用类型化控制工具 | tabs.activate、workspace.split、panels.show/hide |
| 系统信息 | 应用有采集器，MCP 无专用查询/订阅工具 | sysinfo.get/subscribe，明确缓存时间与采集状态 |
| 隧道重启 | 应用有 RestartTunnel，MCP 只有 start/stop | 可先组合实现，必要时加 restart 保持原配置 |
| 平台能力查询 | 有权限查询，未充分表达平台不可用和依赖链 | 增加 supported/available/reason/platform 字段 |
| 一致结构化输出 | 89 工具中仅 13 声明 structured output | 逐步补 outputSchema，统一错误码/重试建议 |
| 传输任务历史 | 仅最近 300 条事件折叠，ID 由会话/方向/文件名组成 | 独立任务 ID 与生命周期，避免同名任务合并及历史消失 |

## 验证边界与后续顺序

本报告是界面 DOM 实测、13 个 MCP 读取工具实测及源码链路审查，不是 89 工具全部端到端通过的证明。配置写入、凭证操作、远端写删、sudo、退出/重载未执行；当前关闭 fs.remote.write/lifecycle/secrets.read/secrets.write/sudo.credential 属于权限状态，不应计作未实现。

建议顺序：敏感数据隔离 → 确认执行一致性 → 会话生命周期复现 → 大目录分页与平台能力声明 → 文件任务/分组/测试/信任工具 → 真实终端、分屏、传输及小窗口视觉验收。

## 首批修复进度

已实现：ui.store 在读取非 ui store 前要求 secrets.read（包括直接子路径）；confirm.prepare 新增可选 tool，精确提交复用原工具的权限/预检/确认链路，拒绝跨工具复用；无 tool 的共享动作提交会在消费 token 前明确拒绝；移除未实现 app.reload/sftp.upload 确认动作；SFTP offset/limit/nextOffset 分页与宿主排序；相对目录绝对路径修正；home 导航；HTTP 工具目录声明截图平台支持；清理 sudo 说明中的 Markdown 符号。

确认调用示例：confirm.prepare {action:"sftp.write",tool:"sftp.syncCwd",args:{sessionId:"…",remotePath:"/allowed"}}，随后 confirm.commit {token:"…"}。旧方式向原工具直接传 token 继续支持。

当前限制：敏感权限关闭时，仅开放 store 名清单与 ui 状态；其他 store 拒绝读取，尚未做字段级白名单投影。平台支持字段是静态支持声明，不等于窗口当前可截图。

仍未实现：孤立会话恢复/回收；macOS 原生截图；新增分组/服务器测试/上传下载/主机信任/系统信息等专用工具；传输独立任务 ID；全部工具结构化输出。上述内容不能视为已修复。

后端需重启后生效，本次未重启正在使用的应用或断开现有连接。


## 第二批修复进度

沿用现有目录结构，没有移动、重命名、清理已有文件；没有操作实际服务器、凭证、分组、known_hosts 或远端目录数据。

新增 9 个 MCP 工具（源码清单从 89 项增至 98 项，旧后端重启前仍是旧清单）：

- groups.list/create/rename/delete：复用已有分组接口，写入受 config.write 与两段式确认保护。
- servers.test：按保存的服务器 id 测试，显式区分 tcp/login；跳板节点不静默升级为登录验证。
- hostTrust.list：只读信任记录。
- tabs.activate、panels.set、workspace.split：激活已有标签、面板显示隐藏、已有标签分屏与取消。沿用常驻 TerminalView，取消布局保留标签与会话标识。

旧式通用 confirm.commit 执行前也会重查动作权限，拒绝时不消费 token。新增三个读取工具均提供结构化输出。

验证：go test ./...、生产构建通过；前端 43 个文件、188 项测试通过。新增测试覆盖权限撤销、确认缺失、token 重放、参数错误、标签不存在、面板切换、分屏取消保留会话标识。测试采用替身 Handler 与 Pinia 状态，没有将它们描述为真实远端验证。

仍待完成：会话重载恢复/回收、macOS 截图、任务式文件/目录传输、主机信任写操作、系统信息专用查询、完整结构化输出。此次未重启应用。

## 第三批 Bug 修复与测试

- 安全页不再把 null 状态显示为“未解锁/钥匙串不可用”；明确区分加载、读取失败、可重试与真实状态。状态未知时隐藏主密码表单，阻止重复提交。
- 每个文件及目录传输生成独立任务 ID；进度事件携带同一 ID，MCP 按 ID 折叠，同名任务不再合并。取消接口兼容旧文件名定位；有多个同名任务时拒绝模糊取消，不再一次取消所有同名任务。新事件使用任务 ID 精确取消。
- 取消操作向调用方返回取消结果，避免最终进度事件较晚到达时历史被误记为成功。失败/取消事件不再把已传字节补满到 total。
- SFTP 进度行使用独立 key，旧任务的延迟移除不会删除新的同名任务；面板卸载时清除延迟刷新定时器，异步传输返回后不再为已卸载面板安排刷新。
- 传输事件单独保留最多 300 条，不再被大量 SSH 输出挤出 MCP 的近期传输查询；它仍是有界近期缓存，不是永久任务数据库。
- app_state 增加 sessionBindings：仅在有效前端标签快照存在时报告未关联的活动连接，避免前端不可达时把全部连接误判为孤立。未自动关闭或重建这些会话；完整恢复/回收仍未完成。

验证：Go 全量测试通过；前端 44 个文件、193 项测试通过；生产构建通过；传输 ID/精确取消/部分字节/事件保留相关 Go 竞态检测通过。新增测试使用内存数据、替身调用和 jsdom，没有执行真实上传下载、取消用户传输或改动实际配置。目录结构及已有数据位置保持不变。
