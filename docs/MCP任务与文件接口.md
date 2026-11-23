# MCP 任务与文件接口

本文描述本次新增及调整的实际接口。参数名称以 `tools/list` 返回的 `inputSchema` 为准；工具结果同时提供兼容文本 JSON 与 `structuredContent`。示例省略 JSON-RPC 包装，格式为 `工具名 参数`，示例中的 ID 和审批 token 均需替换为真实返回值。

## 身份、授权与目标

- `serverId` 是保存的服务器配置 ID；`sessionId` 是后端连接/终端会话 ID；目前终端标签的 `tabId` 与它相同。它们都不同于 HTTP `Mcp-Session-Id` 或 `jobId`。
- 任务属于鉴权客户端身份，不属于 HTTP 传输会话。同一客户端重连后可以继续读取自己的任务；其他客户端即使获准使用同一个 SSH 会话，也不能读取、取消或接收该任务的状态事件。
- 能力开关、客户端目标范围和远端路径范围共同生效。客户端撤销会取消其活动任务并删除尚未执行的计划。客户端权限不会因为调用 MCP 而扩大。
- `terminal.input` 允许在相应目标上执行任意 SSH 命令，是高权限能力。SFTP 路径白名单只约束文件接口，不构成 shell 沙箱。受限客户端禁止本机执行、任意 JS 和明文凭据导出。
- 文件写入和服务重启经独立可信界面批准，确认 token 一次性绑定客户端、精确工具、动作和参数。MCP 不能批准自己或修改自身授权配置。

## 独立执行

### `exec.start`

使用现有 SSH 连接另开非 PTY exec 通道。不会向用户终端输入字符，也不改变用户终端的工作目录或环境。命令的副作用取决于命令本身。

| 参数 | 类型与限制 |
| --- | --- |
| `command` | 必填字符串，非空、无 NUL、最多 256 KiB |
| `sessionId` | 已连接的 SSH 会话；与 `local=true` 二选一 |
| `local` | 默认 `false`；只有显式 `true` 才执行本机 shell，另需 `exec.local` |
| `mode` | `raw`（默认）或 `posix` |
| `cwd` | 仅显式 `posix` 支持，最多 8192 字节 |
| `env` | 仅显式 `posix` 支持；最多 128 个变量，变量名为合法 shell 标识符，单值最多 32768 字节 |
| `timeoutMs` | 整数，默认 30000，范围 200–600000；包含排队时间 |
| `maxBytes` | stdout 与 stderr 共用的存储预算；默认 262144，范围 1024–1048576 |
| `idempotencyKey` | 可选字符串，最多 200 字符；同一身份、同一完整请求返回原任务；复用到不同参数会拒绝 |

`raw` 保留远端 shell 命令文本，不拼接工作目录和环境。`posix` 明确依赖远端 `sh`，对 `cwd/env` 做单引号转义；`command` 本身仍是获授权的命令文本。本机在 Unix 使用 `/bin/sh -c`，Windows 使用 `cmd.exe`，Windows 本机不支持 `posix` 模式。交互式 JumpServer 网关不支持独立 exec，会明确拒绝。

```text
exec.start {"sessionId":"ssh-1","command":"uname -s","idempotencyKey":"inspect-os-1"}
exec.start {"sessionId":"ssh-1","mode":"posix","cwd":"/srv/app","env":{"LANG":"C"},"command":"pwd; printf '%s' \"$LANG\""}
```

返回任务摘要，包括 `jobId/sessionId/local/state/createdAt/outputBytes/capturedBytes/truncated/cancelRequested/terminationProven`。开始后有 `startedAt`，完成后有 `finishedAt`；收到明确退出状态才提供 `exitCode`，可能另有 `signal/error/errorCode`。HTTP 请求结束不会取消已提交任务。任务固定使用提交时的 SSH transport，后续重连不会把它改投另一个连接。

### 任务查询与输出

| 工具 | 参数 | 结果 |
| --- | --- | --- |
| `jobs.get` | 必填 `jobId` | 当前任务摘要 |
| `jobs.wait` | 必填 `jobId`；`timeoutMs` 默认 1000，整数 0–30000 | 等到完成或等待预算耗尽，返回当时摘要；等待超时不会取消执行 |
| `jobs.output` | 必填 `jobId`；`cursor` 默认 0；`maxBytes` 默认 65536，范围 1024–262144 | 输出分片、`nextCursor/hasMore/complete/truncated/outputBytes/capturedBytes` |
| `jobs.cancel` | 必填 `jobId` | 取消请求后的摘要；完成任务调用不会重新执行 |
| `jobs.list` | 无参数 | 当前身份的 `jobs/count/retentionSec` |

输出分片包含 `cursor/stream/text/base64/bytes`，其中 `stream` 为 `stdout` 或 `stderr`。游标按捕获的原始字节计数；精确保存或重组跨 UTF-8 边界的内容应使用 `base64`。`text` 是适合显示的有效 UTF-8 文本。页内只读取已捕获数据，`hasMore=false` 不表示运行中的任务以后不会再有输出。`truncated=true` 表示有输出因存储预算或分片数量限制未保留。

状态为 `queued/running/cancel_requested/succeeded/failed/cancelled/unknown`。`cancel_requested` 仅表示已请求停止。远端收到信号、SSH 通道关闭或网络断开都不能单独证明远端进程及其子进程已终止；没有退出确认时返回 `unknown` 和 `RESULT_UNKNOWN`。`terminationProven` 只描述被执行命令的退出确认，不保证所有脱离会话的后代都结束。结果未知时应先检查目标状态，不能盲目重试有副作用的命令。

```text
jobs.wait {"jobId":"job_…","timeoutMs":30000}
jobs.output {"jobId":"job_…","cursor":0,"maxBytes":65536}
jobs.output {"jobId":"job_…","cursor":65536,"maxBytes":65536}
```

进程内最多接受 8 个未完成任务（排队任务也计入），最多保留 64 个任务；完成记录保留一小时，后台每分钟清理。每任务最多保留 1 MiB、4096 个输出分片。宿主在配置目录的 `mcp-jobs/` 持久化摘要及已保存输出，目录权限 0700、文件权限 0600，采用临时文件加原子替换；不保存命令正文或环境，只保存请求摘要哈希和幂等键。输出本身可能含命令产生的敏感内容。启动前无法安全保存状态会拒绝执行。应用重启后未完成任务标为 `unknown`，不会自动重放；执行期间的输出只在最终状态保存时持久化，崩溃前最后一段输出可能缺失。

每个 SSH transport 还限制最多 8 个尚未实际结束的独立通道操作（包括固定指标/服务读取）。若远端不响应 channel-open 或 exec 请求，调用方超时后会保留这个槽位，直到底层操作结束或连接关闭；后续请求只能在各自预算内等待。这样既不为取消一个任务而关闭用户整条连接，也不会因反复超时无限积累等待协程。连接失去响应时应由用户判断是否重连；自动重连不会重放未知任务。

幂等保护限于保留期内的记录，不承诺无限期或机器掉电情况下的 exactly-once。任务执行期间没有自动重试机制。

### 多主机计划与批量执行

`exec.plan` 必填 `sessionIds`（1–8 个不重复的已连接 SSH 会话）与 `command`，可选 `mode/cwd/env/timeoutMs/maxBytes`，含义和限制与单目标执行相同。本机目标不支持批量执行。

计划阶段逐目标授权、校验并固定连接及参数，不运行命令。返回 `planId/dryRun:true/executionStarted:false/targets/count/commandSHA256/commandBytes/expiresAt`。最多保留 32 个计划，每个计划 120 秒过期，仅创建它的身份可用。

`exec.batch` 必填 `planId`，可选 `concurrency`（默认 2，整数 1–4）。执行前重新检查每个冻结目标的授权。返回 `batchId/planId/targets/concurrency/count/commandSHA256`；每个目标有 `submitted`，成功提交的目标含独立 `job`，提交失败的目标含 `error`。同一个计划只能提交一次，在有效期内重复调用返回原批次。每个 `jobId` 分别查询和取消；排队期间取消的任务不会启动。跨主机操作没有事务回滚承诺。

```text
exec.plan {"sessionIds":["ssh-1","ssh-2"],"command":"uname -s","timeoutMs":30000}
exec.batch {"planId":"job_…","concurrency":2}
```

## 会话与服务器发现

| 工具 | 参数与语义 |
| --- | --- |
| `sessions.connect` | `serverId` 或显式 `local:true` 二选一；连接已保存服务器并打开标签，复用 `open_tab` 流程与权限；依赖运行中的应用前端 |
| `sessions.disconnect` | 必填 `sessionId`；复用 `disconnect_session`，保留标签；中断原 SSH 连接上的任务 |
| `sessions.reconnect` | 必填 `sessionId`；复用 `reconnect_session`，保留标签；依赖前端与其保存的服务器配置 |
| `sessions.get` | 必填 `sessionId`；仅读后端状态，不建立连接 |
| `sessions.wait` | 必填 `sessionId`；`status` 默认 `connected`，允许 `connected/connecting/disconnected/closed`；`timeoutMs` 默认 1000，整数 0–30000 |
| `hostTrust.challenges` | 无参数；返回 `challenges/count/requiresUserApproval:true`，只公开指纹挑战，不具备批准能力 |
| `servers.search` | 可选 `query/group/offset/limit`；默认 `offset:0,limit:100`，每页 1–500 条 |

连接远端需 `session.connect` 与凭据使用授权 `credentials.use`；打开本机终端另需 `exec.local`。断开需 `session.disconnect`，重连同时需连接、断开及对应凭据使用授权。连接与重连失败由原调用返回错误；后端不会编造历史错误状态。

会话观测结果含 `sessionId/tabId/local/exists/status`；已知 SSH 会话另有 `serverId/serverName/host/user/createdAt/independentExecSupported`。`matched` 表示当前状态是否符合等待条件。未知或已移除会话为 `exists:false,status:"closed"`；保留的断线 SSH 会话为 `disconnected`。本机终端的独立执行入口仍是显式 `exec.start local=true`。

服务器搜索只在名称、地址、用户和分组上做不区分大小写的子串匹配，不查询密码、私钥等字段；`group` 为精确匹配。按名称、ID 稳定排序后分页，返回 `servers/count/total/totalAll/offset/limit/nextOffset/hasMore/redacted`。`total` 是筛选后数量，`totalAll` 是全部数量。`servers.list` 和兼容的 `list_servers` 也支持这些筛选及分页参数，默认每页 500 条。列表变更后应从新一轮分页开始。受限客户端没有全局列表访问权时会明确拒绝。

## 主机、进程、隧道与服务

| 工具 | 参数 | 结果与限制 |
| --- | --- | --- |
| `hosts.metrics` | 必填 `sessionId` | 固定 Linux 采集脚本，独立 SSH 通道、10 秒预算；`snapshot/collectedAt/source/unavailable` |
| `processes.list` | 必填 `sessionId` | Linux/procps top 快照；`processes/available/collectedAt/source/error` |
| `tunnels.health` | 可选隧道 `id` | 当前隧道配置与运行状态；`activeProbe:false,serviceReachability:"unknown"`，没有主动探测远端服务 |
| `services.status` | 必填 `sessionId/unit` | Linux systemd 固定 `systemctl show`，返回 `properties/available/execution/collectedAt/source/truncated` |
| `services.logs` | 必填 `sessionId/unit`；`lines` 默认 100，整数 1–1000 | Linux journald JSON 日志，返回 `entries/count/parseErrors/partial/available/execution/truncated` |
| `services.restart` | 必填 `sessionId/unit`；批准后的 `token`，可选 `idempotencyKey` | 当前 SSH 用户权限下重启精确 systemd unit，异步返回任务摘要；120 秒执行预算，完成后另用 `services.status` 验证 |

主机、进程和服务接口不占用户终端。`hosts.metrics/processes.list` 需要 `terminal.input`；服务读取还需要 `terminal.read`。网络速率需要两次采样，单次快照不会伪造速率。Linux 上缺少 procps、systemd、journald 或权限不足时，使用返回的 `available/error/diagnostic/execution` 判断可用性；这些接口不自动 sudo。

`unit` 必须是精确 ASCII 名称，匹配 `^[A-Za-z0-9][A-Za-z0-9_.@:\-]{0,199}$`，例如 `nginx.service`；不接受 shell 片段、路径或以 `-` 开头的参数。服务读取合计输出限制 256 KiB，执行预算 10 秒。服务重启是写操作，必须先取得针对本次 `sessionId/unit/idempotencyKey` 参数的批准：

```text
confirm.prepare {"tool":"services.restart","action":"services.restart","args":{"sessionId":"ssh-1","unit":"nginx.service","idempotencyKey":"restart-nginx-1"}}
services.restart {"sessionId":"ssh-1","unit":"nginx.service","idempotencyKey":"restart-nginx-1","token":"已批准的token"}
jobs.wait {"jobId":"job_…","timeoutMs":30000}
services.status {"sessionId":"ssh-1","unit":"nginx.service"}
```

## 安全文件读取、修改与传输

所有文件读取需要 `fs.remote.read`。写入需要 `fs.remote.write`、一次性批准、应用写路径白名单，并同时满足客户端目标及读写路径范围。路径经过绝对路径、`..` 段、符号链接与远端 canonical 路径检查；白名单为空时拒绝写入。客户端文件范围不约束它另外获准执行的 shell 命令。

### 有界读取与目录分页

- `sftp.list`：必填 `sessionId`；可选 `path/offset/limit`。默认路径为远端 home，每页默认及最多 500 条，结果含 `entries/count/offset/nextOffset/limit/truncated`。目录变化后重新分页。
- `sftp.read`：必填 `sessionId/path`；可选 `encoding`（`text` 或 `base64`）、`offset/maxBytes`。默认 256 KiB，单片最多 1 MiB；`sha256` 只对应本片，不能当成大文件整体哈希。
- `sftp.download`：必填 `sessionId/path`；可选 `offset/maxBytes`，默认 256 KiB、最多 1 MiB。结果含 `base64/bytes/size/offset/nextOffset/sha256/truncated`，调用方保存字节并继续分页，不向任意本机路径写入。

### 精确文本补丁

`sftp.patch.preview` 参数：必填 `sessionId/path/oldText/newText`，可选 `expectedMatches`（默认 1，整数 1–1000）。只处理不超过 1 MiB 的 UTF-8 普通文件，按精确文本匹配，不忽略空白。匹配数量不符则拒绝，返回不含全文：

```json
{"expectedHash":"原文件整体SHA256","newHash":"新文件整体SHA256","matches":1,"beforeBytes":120,"afterBytes":121,"changed":true,"dryRun":true,"applied":false,"atomic":false}
```

`sftp.patch` 接受同样参数，以及提交时必填的 `expectedHash`、批准 token（`confirm`）、可选 `dryRun`。预览结果不会自动授权提交。提交前重新读取整个文件并核对哈希；版本冲突即拒绝。采用同目录私有临时文件和服务器的 POSIX 原子 rename，缺少安全替换能力时保留原文件并报错。`dryRun:true` 仍应优先使用独立的 preview 工具，避免申请写授权。

```text
sftp.patch.preview {"sessionId":"ssh-1","path":"/srv/app/config.ini","oldText":"workers=2","newText":"workers=4"}
confirm.prepare {"tool":"sftp.patch","action":"sftp.patch","args":{"sessionId":"ssh-1","path":"/srv/app/config.ini","oldText":"workers=2","newText":"workers=4","expectedHash":"预览返回的hash"}}
sftp.patch {"sessionId":"ssh-1","path":"/srv/app/config.ini","oldText":"workers=2","newText":"workers=4","expectedHash":"预览返回的hash","confirm":"已批准的token"}
```

### 上传与覆盖写

`sftp.upload` 必填 `sessionId/path/base64`，可选 `createDirs/append/expectedHash/confirm`。解码后最多 1 MiB，不读取任意本机路径。兼容的 `sftp.write` 可传文本 `content` 或 `base64`，同样支持版本条件和安全替换。

`expectedHash` 可为原文件整体 SHA-256，或 `missing`（要求目标原本不存在）；它与读取单片返回的哈希不同。覆盖写先暂存，再原子替换；已有目标且服务器不支持原子覆盖时拒绝。`append:true` 必须显式指定，属于非原子追加。结果含 `ok/path/bytes/append/atomic/sha256/metadataPreserved/metadataNote`。

尽力保留已有文件 mode、owner/group 等基础元数据；不承诺 ACL、扩展属性或远端其他进程同时改名/替换符号链接时的系统级事务隔离。文件写入没有自动回滚，重要文件应按业务要求先备份并在提交后回读验证。

### 面板同步与取消

`sftp.syncCwd {sessionId,remotePath?}` 只需 `ui.write`，只切换文件面板，不向终端键入 `cd`，也不要求远端写白名单。`sftp.cancel {transferId}` 只需 `transfer.cancel`，不要求远端写白名单或一次性文件写批准。取消是请求，在下一数据块检查点生效；受限客户端不能取消 GUI 所有者的传输。

当前上传/下载接口采用有界分片，不提供任意本地路径代理、整个目录递归传输任务或大文件原子续传任务。需要这些行为时应另行设计任务生命周期、分片校验、恢复及清理协议，不能用隐式 shell 命令替代文件权限边界。

## 验证方式

测试使用内存宿主、临时配置目录、本机隔离 SSH/SFTP 服务，不连接用户真实主机，不运行用户终端命令。覆盖独立 SSH 双流/退出码、取消后 unknown、连接冻结、任务所有权/幂等/预算/恢复/落盘失败拒绝执行、多目标并发、服务批准和参数注入、会话观察和别名权限、搜索秘密隔离、文件哈希冲突/原子写/符号链接越界等行为。竞态检查与完整验证结果见本次交付说明。
