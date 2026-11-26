# Windows 实测与修复记录（2026-11-26）

在 Windows 本机运行原生 Go 测试、PowerShell/cmd ConPTY、SSH/SFTP 集成测试和 Wails/WebView2 桌面冒烟测试。测试使用临时目录和隔离配置，没有连接已保存的服务器或使用已保存凭据。

## 已确认并修复

| 问题 | 复现与影响 | 修复 |
| --- | --- | --- |
| SQLite 新建连接没有 busy timeout | 20 路同时保存设置时，修复前有 19 路报 SQLITE_BUSY；连接池扩张后可能出现设置保存失败 | 在 SQLite DSN 中配置 busy_timeout，让每条连接都有 5000ms 等待时间 |
| 设置保存没有事务 | 用触发器让较后面的设置项失败，前面已经写入的 UIScale 仍然改变；并发保存也可能混合不同次提交的数据 | 所有设置项在一个事务内提交，失败整体回滚 |
| SFTP 覆盖已有文件时无条件 chown | Windows 上的进程内 SFTP 服务拒绝无意义的属主修改，导致写入和 patch 失败 | 比较临时文件与目标文件 UID/GID，仅在需要修改时调用 chown；真正需要修改而失败时仍返回错误 |
| PowerShell 输出已到达却等待超时 | 桌面实测中命令回显紧贴 `C3-A9` 结果，echo 过滤向后查找换行，把首行真实输出也排除 | 只跳过命令本身及紧邻的换行；新增单块、分块回归用例，并保留回显防误命中测试 |

SQLite 两项也影响其他平台；SFTP 问题取决于服务器对 chown 的支持；终端匹配问题影响调试接口的 terminal.run 自动化等待。

## 验证结果

- 完整 Windows Go 测试：13 个测试包通过，651 个测试/子测试通过，16 个跳过，0 失败。
- 前端：84 个测试文件、394 个测试全部通过；类型检查与生产构建通过。构建保留现有的大分块提示。
- 原生终端：实际启动 PowerShell 和 cmd，验证输出、中文结果、resize、退出清理、重建和断开。
- SSH/SFTP：本机网络和进程内服务执行握手、文件 CRUD、中文路径、二进制传输、条件写入、append 和 patch 等集成测试；没有连接外部服务器。
- 桌面冒烟：修复后连续 3 轮，每轮 50 项全部通过。覆盖鉴权、HTTP/MCP 工具目录一致性、页面与设置分区、真实本地终端、ASCII/Unicode/中文输入、base64 输入、resize、滚动、分屏、侧栏、断开重连、前端控制台和会话清理。
- 不带测试 overlay 的 Windows 正式入口构建成功，产物位于 `.smoke/windows-production-final/ding-ssh.exe`。
- 新增 Windows CI 测试任务，覆盖 internal 和 cmd 包，包含原生终端测试；尚未在 GitHub Actions 运行。

## 复测命令

```powershell
go test ./... -count=1 -timeout=120s
go test ./internal/store -run 'TestSQLite(BusyTimeout|SettingsSave|ConcurrentSettings)' -count=10
npm --prefix frontend test -- --run
npm --prefix frontend run build
wails3 task build SKIP_FRONTEND=true BIN_DIR=.smoke/windows-production-final
python scripts/audit/live_smoke.py --debug-info .smoke/windows-profile/debug.json --output .smoke/windows-live-final.json
```

桌面脚本需要已经启动且显式启用了调试服务及相关能力的实例。它通过调试接口操作真实前端及 PTY，不等同于逐项人工点击验收。Windows 默认发现路径现已支持 `%APPDATA%/ding-ssh/debug.json`，JSON 读写明确使用 UTF-8。

本次原始日志保留在未跟踪的 `.smoke/`：`windows-go-final.jsonl`、`windows-frontend-test.log`、`windows-frontend-build.log`、`windows-live-final*.json` 和 `windows-production-final.log`。日志不纳入提交，避免带入本机环境信息。

## 覆盖边界

- 桌面测试使用独立 SingleInstance ID、WebView 数据目录和数据库。执行环境拒绝系统通知注册所需的注册表访问，测试 overlay 因而排除了通知服务注册；系统通知的运行行为未验证。正式源码没有该排除，正式入口构建已验证。
- 16 个跳过项涉及 POSIX 专用命令、无符号链接创建权限的用例、注册表通知注册及需要手动启用的探测。Windows 下不比较 Unix 0600 位；这不代表已经验证 Windows ACL。
- SSH config 的反斜杠行为与本机 OpenSSH 对照后确认一致，没有修改解析规则；仅修正测试夹具的引号，并增加含空格、中文及转义反斜杠路径的 Windows 测试。
- 未覆盖真实外部 SSH 服务器、系统安装/卸载及升级交付流程。

本次创建的隔离桌面实例已关闭，用户原有开发实例继续保留。
