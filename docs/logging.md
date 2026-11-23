# 日志规范（服务状态可观测性）

本文约定 ding-ssh 后端日志的级别、格式、标签、高频路径限流与敏感信息边界。
目标：**每一步操作和反馈都能从日志判断服务状态**——谁在什么时候对什么对象做了什么、
结果如何、耗时多少、失败原因是什么。

> 适用范围：`internal/**`、`cmd/**`、`main.go` 的 Go 代码。前端跨端日志不在本规范内。

## 1. 现有框架

- 入口：`internal/logx`，四个函数 `Debugf / Infof / Warnf / Errorf`，外加 `APILogf`
  （MCP/调试调用专用，独立开关）。
- 双通道：控制台（`SetEnabled`）与日志文件（`SetFileEnabled`）**互相独立**，
  默认都关闭；级别默认 `info`，设置页实时切换。
- 脱敏：所有输出统一过 `logx.Redact`（token / password / 私钥 / Bearer 一律打码）。
- 文件：`<ConfigDir>/logs/app-YYYYMMDD.log`，8MB 或跨天轮转，保留 20 个。
- 新增辅助：
  - `DebugEnabled / InfoEnabled / WarnEnabled / ErrorEnabled`：在拼装昂贵参数前预判；
  - `Sampler` + `DebugSampled / InfoSampled / WarnSampled / ErrorSampled`：高频路径限流。

## 2. 级别语义

| 级别 | 用途 | 典型内容 |
| --- | --- | --- |
| **DEBUG** | 排查细节；默认不输出，量可大 | 函数进入/关键分支；缓存命中/未命中；参数摘要（不含敏感值）；重试次数；轮询/分块采样；耗时、字节数、计数；状态机每次迁移 |
| **INFO** | 服务状态主线；默认输出 | 生命周期（启动/就绪/关闭/退出）；用户发起的操作开始与完成（连接、保存、导入、上传、下载、更新、隧道启停）；后台任务启动/停止；批量操作汇总（成功/失败计数、总耗时）；设置变更 |
| **WARN** | 可恢复异常、降级、兜底 | 回退到默认值/JSON/本地缓存；某步失败但已重试或跳过；超时后放弃；数据不一致导致功能降级；非法配置回落；外部依赖不可用但主流程可继续 |
| **ERROR** | 操作失败且影响用户/功能 | 连接失败、鉴权失败、读写失败、安装/更新失败、数据库打开失败、端口监听失败；必须带 `err=` 与定位上下文 |

约定：

- 不要用 WARN 代替 ERROR 掩盖真实失败；也不要每个 `if err != nil` 都打 ERROR，
  只在**决定该错误对用户意味着什么**的边界层打 ERROR（见 §7）。
- 循环里持续失败用 `WarnSampled/ErrorSampled`（首次必出，之后限流），避免刷屏。

## 3. 标签与格式

统一格式：

```
[<tag>] <中文动作/对象>: key=value key=value ...
```

- tag 用包名（或包内子域），固定小写方括号，便于 `grep '^\[INFO\] \[store\]'`：
  - `[appdir] [store] [cryptox] [sshconfig] [sshx] [apphost] [debugsrv] [updater] [localterm] [autostart] [i18n]`
  - debug 服务内部再细化：`[debugsrv/server]`、`[debugsrv/mcp]`、`[debugsrv/ui]`、
    `[debugsrv/sftp]`、`[debugsrv/sudo]`、`[debugsrv/appctl]`、`[debugsrv/stream]`、`[debugsrv/audit]`
- 冒号用中文 `：` 或 ASCII `: `？**统一用 ASCII `: `**（便于脚本切分）。中文动作里已有全角标点时不受限。
- key 固定小写：`session= server= host= user= path= remote= local= port= id= count= bytes= elapsed= err= status= reason=`。
- 值：字符串不加引号；路径/命令/可能带空格的用 `%q`；布尔用 `%v`；耗时统一 `elapsed=` 加 `time.Since`。
- 一条日志讲清一件事，不要换行。消息末尾不加句号。
- 已有日志保留原中文措辞，仅补 tag；新增日志按本规范写。

示例：

```go
logx.Debugf("[sshx] 发起 SSH 连接: session=%s server=%s host=%s:%d user=%s auth=%s",
    sessionID, node.Name, node.Host, node.Port, node.User, node.AuthType)
logx.Infof("[store] 保存服务器: id=%s name=%s count=%d elapsed=%s", n.ID, n.Name, len(nodes), time.Since(start))
logx.Warnf("[updater] 主通道检查失败，回退镜像: url=%s err=%v", url, err)
logx.Errorf("[apphost] 打开数据库失败: path=%s err=%v", path, err)
```

## 4. 必记场景清单

补日志时逐函数过一遍，重点覆盖：

1. **公开方法/绑定方法**（Wails 可调用的）：DEBUG 入口带关键参数；成功 INFO 带结果与耗时；
   失败 ERROR 带 err。前端每一步操作都应对应至少一条后端日志。
2. **状态迁移**：`connecting → connected → closed/error`、隧道 running/stopped、
   后台任务 start/stop、开关切换、语言/主题/设置生效。
3. **分支与兜底**：降级到 JSON、缓存未命中、数据校验失败回落默认、跳过不可进入目录、
   配置非法值回落——至少 DEBUG，影响结果用 WARN。
4. **外部交互**：TCP/SSH/SFTP/HTTP/隧道/子进程/文件/数据库的一次操作的开始/结果/失败。
5. **批量与循环**：每项 DEBUG（大列表可每 N 项或只记失败项），结束时 INFO 汇总
   （`total= ok= fail= elapsed=`）。
6. **超时与重试**：每次重试 DEBUG，最终放弃 WARN/ERROR，带 `attempt=`。
7. **启动与关闭**：进程启动、各子系统初始化成功/失败、退出原因与耗时。
8. **后台周期任务**：启动参数、每轮采样（用 Sampler 限流）、停止原因。

不要求逐行覆盖；要求**每个对外可观察的行为都有前因后果**。

## 5. 高频路径限流（强制）

以下路径**禁止**逐条/逐块打日志，否则日志文件暴涨并拖慢终端：

- 终端输出字节流（`session.go` 读写循环、`localterm`）
- SFTP 分块读写、传输进度回调
- sysinfo / top / 网卡 / 磁盘轮询
- SSH 心跳、keepalive
- 调试服务的事件流（SSE）逐个事件
- 前端高频 RPC（滚动、resize、输入）

统一做法：

```go
var chunkLog = logx.NewSampler(256, 2*time.Second) // 每 256 次或每 2s 至多一条

// 热路径里廉价参数直接调用；采样器内部先判级别，不会被禁用日志的开关白耗配额。
logx.DebugSampled(chunkLog, "[sshx] 终端输出分块: session=%s bytes=%d", s.ID, n)
```

- 字节流只记「长度/方向/累计值」，**绝不记录内容**（除非已有 `LogTraceTabs` 旁路机制）。
- 进度类日志用百分比里程碑（25/50/75/100）或每次操作结束汇总，不要每块一条。
- 循环里拼装 JSON/长字符串等昂贵操作先用 `if logx.DebugEnabled() { ... }` 守卫。

## 6. 敏感信息（红线）

- **绝对不记录**：密码、passphrase、私钥内容、API token、Cookie、解密后的字段值、
  终端输入/输出内容、文件内容、剪贴板内容、完整环境变量值。
- 可以记录：主机、端口、用户名、服务器名/ID、会话 ID、路径、文件名、大小、计数、
  状态枚举、错误文本（错误文本本身也要过 `logx.Redact`，已自动处理）。
- 路径里可能含用户名或 token（URL），统一 `%q` 并在必要时 `logx.Redact`。
- 不要依赖脱敏兜底而主动拼敏感值：脱敏只保证「已知模式」不出现在磁盘上。
- 调试服务（`debugsrv`）日志只记操作与结果摘要，不记请求/响应正文；正文大小用 `bytes=`。

## 7. 避免重复与噪音

- 一个错误只在一个「边界层」记 ERROR：比如 apphost 绑定方法把 `sshx` 返回的错误
  记 ERROR（带用户操作上下文），`sshx` 内部只记 DEBUG/WARN。
- 不要同时 `Errorf` 又 `return fmt.Errorf` 后再被上层 `Errorf` 记录成两条相同错误；
  若上层一定会记，下层只记 DEBUG。
- 成功路径 INFO 要克制：一次用户操作 1 条完成日志即可，不要每层都报成功。
- 库初始化、纯数据转换（`models` 等无副作用包）不需要日志；
  若配置值非法回落，打一条 WARN 即可。

## 8. 验收

- 只加日志与必要的守卫/采样器声明，**不改变现有业务行为与函数签名**。
- `gofmt -w`；`go build ./...`；`go vet ./internal/<包>`；`go test ./internal/<包>` 全绿。
- 不修改任何 `_test.go` 断言；如测试对输出敏感，调整日志而非改测试。
- 自查：把级别设为 debug、控制台+文件打开后运行一遍相关流程，确认 INFO 能看懂主线、
  DEBUG 能定位分支、WARN/ERROR 能解释异常。
