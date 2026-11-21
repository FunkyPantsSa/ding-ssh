# ding-ssh 第二轮代码复查、修复与回归报告

日期：2026-11-21（Asia/Shanghai）。基线：`3ffe48d`，分支：`dev`。

本报告记录第一次审计完成后的独立复查。第一轮的 19 组缺陷、当时的结果和限制保留在 [第一轮报告](2026-11-21-full-audit.md)，不把旧结果改写成第二轮结果。

## 1. 本轮结论

第二轮新增确认并修复 **5 类问题**。均先构造用例观察修复前失败，再修改实现并执行回归。重点是跨层衔接、密钥来源连续性、初始化失败处理、完整存储操作与密钥轮换之间的同步，以及下载目标的文件系统边界。

| 新发现 | 风险与实际影响 | 修复后 |
| --- | --- | --- |
| R2-01 传输事件丢失任务 ID | 同名任务可能合并显示；取消退回文件名匹配，后端拒绝有歧义的请求 | 事件桥保留 ID，兼容旧格式 |
| R2-02 默认密钥来源变化 | 钥匙串故障或恢复后可能生成/选用另一把 key，已有密文无法解密 | 持久化来源；缺失、损坏及歧义场景拒绝自动替换 |
| R2-03 保险库初始化失败后放行 | 安全状态仍显示已解锁，允许把新服务器密码/凭证以明文保存 | 宿主阻止访问和写入；前端显示初始化错误 |
| R2-04 文件夹下载跟随目标符号链接 | 下载内容能覆盖所选本地目录以外的已有文件 | 使用 `os.Root` 限制目录、临时文件和最终提交的访问边界 |
| R2-05 轮换密钥插入存储操作中间 | 两个字段可能分别使用不同 key，或旧 key 加密后在轮换完成才写入 | 完整读写操作持有同一代密钥；轮换等待这些操作结束 |

**功能回归通过；现有前端资源预算门禁仍失败。** 本轮没有降低门禁标准，也没有删除字体、图片或离线中文支持来凑预算。

## 2. 最终验证结果

| 检查 | 结果 |
| --- | --- |
| `DSH_LIVE_PROBE=1 DSH_UI_PROBE=1 go test -json ./... -count=1` | 431 个顶层测试通过，含父测试与子测试共 522 条通过记录，0 失败、0 跳过 |
| 前端 Vitest | 67 个文件、314 个测试通过，0 失败 |
| 运行中 Wails 应用 | 49 项通过、1 项可选接口跳过、0 失败；可选 `resources/templates/list` 未实现而跳过 |
| `go test -race ./internal/... -count=1` | 通过 |
| 新存储/密钥并发场景 race 重复 3 次 | 通过，含 5 种存储操作 |
| 下载目录限制及相关下载/替换场景 race 重复 5 次 | 通过 |
| `go vet ./...` | 通过 |
| `npm run build` | 类型检查和生产构建通过 |
| `wails build -platform darwin/arm64 -o ding-ssh-audit` | macOS ARM64 原生编译、打包、自签名通过 |
| Windows/Linux AMD64 internal 包交叉编译 | 通过；不是对应系统的实机运行测试 |
| 有测试文件的 internal 包语句覆盖率 | 54.5%；单独测量时未开启 live/UI probe 环境变量，不含 main、无测试包、前端及进程外 Wails 冒烟 |
| `bash scripts/verify-ui2.sh` | 前置构建和功能测试通过，资源预算检查退出码 1 |
| `git diff --check` | 通过 |

与第一轮最终快照相比，Go 顶层测试从 421 增至 431，前端从 306 增至 314。新增用例不仅检查函数返回值，还检查数据库里是否真的留下明文、重开后是否仍可解密，以及目录外文件是否真的保持原内容。

交付：

- [逐项结果 CSV](2026-11-21-round2-test-cases.csv)：886 条最终结果记录，885 条 PASS、1 条 SKIP。Go 父子测试分别占行，不能称为 886 个独立顶层场景。
- [机器可读汇总 JSON](2026-11-21-round2-summary.json)：脱敏统计、覆盖率、构建结果和资源门禁数据。
- 新增/扩展的 Go、TypeScript 测试：随代码长期保留，可在后续变更时重复运行。
- 原始失败与通过日志：本机 `/tmp/ding-ssh-audit-20261121-round2/`。临时目录不保证长期保留，不提交包含应用上下文的原始日志。

## 3. 本轮构造方法与隔离

扫描重点包括 `cryptox`、SQLite 存储、宿主启动与安全入口、SFTP 文件夹和原子替换、前端事件桥与安全启动，以及上一轮修改之间的组合行为。既有全量测试继续覆盖 SSH/SFTP/隧道、本机终端、服务器与分组、sudo 助手、设置、更新及调试接口；这些功能的详细矩阵见第一轮报告。

钥匙串异常、恢复、缺失与损坏通过 `go-keyring` 内存 mock 构造；不通过删除/替换真实系统钥匙串来做破坏性测试。数据库、security.json、文件密钥、远端 SFTP 文件及目录外保护文件都在临时目录中创建。

真实调试验证沿用已启用的调试权限，未扩大权限。复测开始时应用为 0 标签、0 会话、0 隧道；脚本新建自己的两条本机 PTY，完成后清理并恢复 UI。没有连接保存的真实 SSH 主机，也没有对真实配置执行主密码切换、密码写入或导入覆盖。认证 token 不写入交付物。

## 4. R2-01：传输任务 ID 在前端事件转换时丢失

**根因。** Go 事件含 `transferId`，SftpPanel 也支持按 ID 分离同名任务及 `@id:` 精确取消；但 `services/ssh.ts` 的 `normalizeTransferEvent` 重建对象时没有复制 ID。第一轮组件测试直接调用了模拟事件回调，绕过了这段真实转换，所以后端与组件各自通过，组合后仍有问题。

**修复前复现。** 通过真实 `onSftpTransfer` 注册 Wails 回调，依次发送两个 `same.txt` 事件，ID 分别是 `first`、`second`。组件收到的两个 ID 均为 `undefined`。camelCase 与 PascalCase 两种对象格式都失败。

**修复。** 对象转换保留 `transferId` / `TransferID`，继续支持没有 ID 的旧数组格式。没有改变取消接口或削弱后端对同名歧义的保护。

**复测。** 新 `frontend/src/services/ssh.events.test.ts` 三个测试通过；现有 SftpPanel 同名任务分离、按 ID 取消、取消后部分字节保留用例一起通过。

## 5. R2-02：钥匙串可用性变化导致密钥不连续

**修复前复现。**

1. 首次启动模拟钥匙串不可用，应用退回文件 key，保存测试密文；随后恢复为空的钥匙串，再 OpenVault。原代码创建新钥匙串 key，旧密文解密失败。
2. 首次使用钥匙串并记录迁移完成，随后模拟钥匙串暂时锁定。原代码创建新的 `.master.key`，把已有保险库作为另一把 key 的“已解锁”实例打开。
3. 为旧版配置准备有效文件 key 和迁移元数据，钥匙串为空。原代码忽略文件 key 并创建新 key。

三条修复前测试均失败；不是只根据源码猜测风险。

**修复。** 默认模式在 `security.json` 中记录非敏感的 `keySource`，取值 `file` 或 `keyring`。记录来源后只加载该来源的已有 key，不因临时不可用、删除或损坏而自动生成替代 key。全新配置仍可在系统钥匙串不可用时使用文件来源。

旧版没有来源标记的配置保留有效已有 key；如果文件和钥匙串同时存在且 key 不同，拒绝自动猜测，不覆盖任一来源。损坏的钥匙串条目也不会被新文件 key 绕过。开启主密码时清除默认来源标记；关闭主密码时记录新 key 实际写入的来源。开发隔离配置不访问真实钥匙串，旧隔离配置已存在元数据但 key 丢失时同样拒绝重新生成。

**复测。** `vault_sources_test.go` 覆盖故障→恢复、钥匙串暂时不可用、旧文件来源、两种记录来源的 key 丢失、旧来源冲突、损坏钥匙串和旧隔离 key 丢失；原主密码完整生命周期和失败回滚也通过。

**行为边界。** 真正丢失的 key 无法由本修复恢复。来源冲突会停止自动解锁，需要保留原文件后进行人工恢复；本轮没有冒险重写真实用户 key。旧版程序会忽略新增元数据，回退旧程序仍会保留旧的选择逻辑。

## 6. R2-03：保险库初始化失败后，宿主仍允许保存明文

**修复前复现。** 临时目录写入损坏的启用主密码元数据，调用真实 `App.Startup`。Vault 初始化确实失败，但 `GetSecurityStatus` 返回 `unlocked=true`；`SaveServer` 与 `SaveCredential` 都成功。随后直接查 SQLite，两张表均存在测试明文密码。

**根因。** 初始化错误只写日志，没有保留为后续操作的阻断状态；`vault == nil` 被当成允许兼容明文模式。同时，前端启动读取安全状态失败时直接放行工作区。

**修复。** 宿主持有 Vault 初始化错误，`ensureUnlocked` 拒绝相关操作；服务器、凭证删除入口也经过校验。安全状态报告未解锁、需要阻断和 `initializationError`。前端安全状态请求失败或结构错误时保持锁定；初始化错误界面显示恢复提示，不提供无法生效的密码表单。正常主密码解锁流程保留。

**复测。** `security_startup_test.go` 使用真实 Startup，检查状态、拒绝保存/删除，并直接确认数据库没有落入测试记录。`security.startup.test.ts` 检查桥接失败、坏响应及正常状态；`AppUnlock.test.ts` 验证错误提示和正常密码表单。

修复过程中未向真实应用写入损坏元数据。新的 Wails SecurityStatus 绑定随构建更新。

## 7. R2-04：下载目标目录的符号链接可越出用户选择

**修复前复现。** 构造真实进程内 SFTP 服务端和目录外 `keep.txt=outside-original`，分别设置：

- 所选本地目录中的整个目标文件夹是指向外部目录的符号链接；
- 目标文件夹中的子目录是指向外部目录的符号链接。

调用实际 `SftpDownloadFolder` 后，原实现返回成功，两种情形的目录外文件都变成 `remote-replacement`。原代码“跳过源端符号链接”没有保护目标目录里的链接。

**修复。** 打开用户所选目录的 `os.Root`，在该边界内创建/打开下载根目录；下载树继续持有自身 root。目录创建、临时文件打开、状态检查、权限保留、备份/发布重命名和失败清理均使用绑定到该 root 的方法。相对路径还需通过 `filepath.IsLocal`。单文件下载的原有提交协议继续复用，文件夹下载采用有边界的文件操作实现。

**复测。** 两条越界用例必须返回错误，目录外文件内容不变，也不留下临时文件。普通递归下载、空目录、源端符号链接跳过、取消、短写及本地原子替换等现有用例通过；相关 race 场景重复 5 次通过。

定向回归曾发现新包装器在“已取消且本地根目录不存在”时先返回文件打开错误，已调整为先检查取消状态，再打开 root；最终保留原取消语义。此为本轮实现迭代，不另计一类应用缺陷。

边界说明：所选根目录本身是用户授权的路径；本修复限制的是其下生成的下载树。root 方法会拒绝越界的链接，不承诺允许所有历史上的目标链接用法，也不声称隔离操作系统挂载点、设备文件或所有平台实现差异。

## 8. R2-05：每字段锁不足以保护一次存储读写

**修复前复现。** 包装真实 Vault，在处理第一个字段后、离开该字段锁后暂停，让主密码轮换执行完，再恢复操作。以下五条路径全部复现失败：

| 路径 | 修复前观察 |
| --- | --- |
| 保存服务器 | password 用旧 key，key_content 用新 key，保存后无法解密 |
| 保存凭证 | 同上 |
| 批量保存服务器 | 同上，单个 SQL 事务并不能固定加密使用的 key |
| 列出服务器 | 第一字段已解密，轮换后再解密已扫描的旧密文第二字段时失败 |
| 列出凭证 | 同上 |

**根因。** 第一轮修复了迁移事务和错误回滚，但 Vault 的字段级锁在一个字段完成后就释放。SQLite 写入发生在字段加密之后，读操作也可能持有旧的查询快照；race 检测不一定能发现这种顺序合法、业务错误的交错。

**修复。** Vault 增加存储操作与轮换之间的读写同步。SQLite List、Save、SaveBatch 从第一个字段直到 SQL 操作结束持有操作作用域；启用/关闭/更换主密码及 Unlock 使用排他作用域。明文迁移也持有完整作用域。另用存储内的 cipher 锁稳定注入对象，避免重复 attachCipher 与在途操作竞争。

作用域的获取在 SQL 查询/事务之前；迁移回调使用直接数据库重加密，不递归调用持有作用域的 List/Save。这样既固定 key 的代次，也避免读查询持有 SQL 资源后再等待轮换造成相互等待。

**复测。** `store/vault_concurrency_test.go` 的五个子场景通过，断言轮换不能在暂停点提交；恢复后再检查两张表的两类字段均可解密。全量 race 和聚焦 race 重复 3 次通过。

**未扩大承诺。** 此修复是进程内并发一致性；仍未实现 security.json 与 SQLite 在任意断电点的跨存储原子恢复日志。

## 9. 资源门禁与未实测范围

本轮再次实际执行 `bash scripts/verify-ui2.sh`。类型检查、构建、前端测试和普通 Go 测试通过，随后因 `font-face` 预算失败退出：

| 指标 | 本轮测量 | 阈值/说明 |
| --- | ---: | --- |
| CSS raw / gzip | 263,454 / 77,575 B | 超过 100,000 / 30,000 B，脚本记 warning |
| index JS raw / gzip | 764,093 / 250,245 B | 低于 900,000 / 270,000 B |
| font-face | 114 | 超过 32，实际退出点 |
| 字体文件数 | 222 | 超过 32；因前一断言退出未执行到此断言 |
| 字体总大小 | 6,110,464 B | 记录值 |
| dist 磁盘占用 | 10,240,000 B | 超过 2,621,440 B；同上未执行到此断言 |

这是沿用的未解决资源问题，不计入本轮已修复的 5 类。Vite 大 chunk 警告、macOS 链接器警告也如实保留在原始日志中；成功构建不等于资源门禁通过或正式公证签名完成。

本轮未进行真实 Windows/Linux 桌面运行、真实系统钥匙串破坏/恢复、真实提权升级、生产 SSH 主机兼容性、长时间弱网大文件、断电及磁盘故障注入。语句覆盖率范围有限，不能声称所有代码分支和所有外部环境都已验证。

## 10. 重跑命令与提交核对

从仓库根目录执行：

```sh
DSH_LIVE_PROBE=1 DSH_UI_PROBE=1 go test -json ./... -count=1
go test -race ./internal/... -count=1
go vet ./...
go test -race ./internal/store -run TestVaultRotationWaitsForWholeStorageOperation -count=3
go test -race ./internal/sshx -run 'TestFolderDownloadCannotFollowDestination|TestDownloadFolderTree|TestLocalReplacement' -count=5
go test ./internal/cryptox -run 'TestVault(FileFallback|KeyringOutage|Legacy|MissingRecorded|CorruptKeyring)' -count=1
go test ./internal/apphost -run TestVaultInitializationFailureBlocksSecretWrites -count=1
go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./internal/... > /tmp/ding-ssh-coverage-packages.txt
xargs go test -count=1 -coverprofile=/tmp/ding-ssh-coverage.out < /tmp/ding-ssh-coverage-packages.txt
go tool cover -func=/tmp/ding-ssh-coverage.out
(cd frontend && npm test)
(cd frontend && npm run build)
python3 scripts/audit/live_smoke.py --output /tmp/ding-ssh-round2-live.json
wails build -platform darwin/arm64 -o ding-ssh-audit
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test ./internal/... -run '^$' -exec /usr/bin/true
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test ./internal/... -run '^$' -exec /usr/bin/true
bash scripts/verify-ui2.sh  # 当前资源预算仍返回非 0
git diff --check
```

提交前核对报告与 CSV 数量、构建和测试退出状态、生成绑定、敏感信息及远端分支。本轮文档随修复一起提交；实际 push 成功与提交号以任务最终回复及远端引用核对结果为准。
