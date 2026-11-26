# MCP 服务观测与真机验收

本文记录 MCP 观测、UI 诊断和真机操作当前已经落到代码里的范围，区分“源码已有”“自动化测试有覆盖”和“真实桌面已验收”。它配合[重设计总方案](MCP-AI真机观测与UI诊断重设计方案.md)使用：总方案说明目标架构，本文件回答哪些目标已实现、哪些证据还缺。

评估日期：2026-11-26。本文不把未运行的真机步骤或未确认的测试结果标成通过。

## 当前结论

目前已经形成一条可以工作的观测路径：AI 通过 MCP 连接正在运行的本机实例，读取宿主和前端状态，采集当前 WebView 的真实像素图，读取语义节点与布局诊断，再通过 `ui.act` 执行受约束的动作并等待显式后置条件。`verify.run` 可以把只读观测封装成异步 job 并保存报告。

这还不是“所有桌面 UI 都能无条件验收”。macOS 和 Windows 有像素适配，Linux 目前没有像素 provider；原生窗口输入只在 macOS+cgo 有实现，且要求目标窗口已经聚焦。诊断以安全的 DOM/几何规则为主，真实视觉异常仍需要查看图片。网络探针只有本机网络执行点，不能代表 SSH 远端机器的网络视角。当前验证 runner 是只读观测，没有替 AI 完成冷启动、任意业务路径或跨平台验收。

## 实施覆盖矩阵

“已实现”表示源码存在对应路径，不代表每个平台或生产二进制都已实测；“自动化覆盖”仅指相关测试文件存在，是否通过要看本次实际测试输出；“真机未验”表示需要针对目标平台和构建运行 smoke。

| 能力面 | 当前实现 | 覆盖边界 / 验收状态 |
| --- | --- | --- |
| 发现与附着 | `ding-ssh-mcp` 可读现有 discovery 信息，并提供 `--launch=if-needed`、隔离 profile、PID/实例/build 校验和前端 ready 等待。 | launcher 单测已在前序工作中通过；本轮没有启动应用，未做完整冷启动实测。附着必须核对 discovery、`app.health`、`app.observe` 的实例身份，不能只凭端口可连判断实例正确。 |
| MCP 协议会话 | HTTP MCP 支持 `initialize`、session id、协议版本和 `notifications/initialized`。 | smoke 客户端曾因 initialize 后未发 initialized 通知而在 `tools/list` 收到 HTTP 400。本轮已修正握手并增加 HTTP 状态/响应体诊断；没有连正在运行的隔离实例重跑 smoke。 |
| 宿主运行状态 | `app.observe` 聚合进程/build 身份、Go runtime 指标、前端 heartbeat、bridge、窗口、session 和 tunnel 状态；`app.runtimeCapabilities` 报告当前采集和输入 provider 能力。 | 前后端分项状态可见，但完整覆盖仍是 partial。宿主心跳不能证明画面像素正确；前端 heartbeat 暂停时也要结合窗口可见性和运行环境判断。 |
| 前端响应与错误 | 前端显式 ready、页面 epoch、mutation/layout/frame/error 序号；收集 Vue/window/promise 错误的安全元数据和生成文件栈帧；可读公开 Vue 组件 registry。 | 不返回任意异常正文、凭据或私有输入值。网络请求没有统一埋点；生产实际字形字体仍不可读取；heartbeat 也不是逐帧渲染证明。 |
| 当前 UI 像素 | macOS 使用 `wkwebview-snapshot` 采集 WebView surface；Windows 使用 `win32-printwindow-client` 采集窗口 client area；采集结果带 provider、surface、尺寸、坐标映射和一致性信息。 | macOS/Windows provider 已有源码。实际可用性需要窗口可见且截图成功；Windows 的 PrintWindow 输出不等于桌面最终合成；Linux 当前不支持。需要在每个平台分别验证缩放、Retina/DPI、透明层、最小化和 GPU/canvas 表现。 |
| DOM 与组件观察 | `ui.observe` 输出语义节点、ref、actionability、view、viewport、组件/终端摘要和错误；`ui.inspect` 提供样式、裁切链、命中与组件归属。 | ref 与实例、页面 epoch、布局/DOM 序号绑定；页面变化后要重新 observe。节点数量有界，截断需看 coverage 字段。 |
| UI 规则诊断 | `ui.diagnose` 检查越界 overlay、遮挡、文字可见性、图片加载、字体加载、终端 canvas/grid 等问题，并带节点与几何证据。 | 这是规则扫描，不是通用视觉理解。像素外观、字形、颜色、图标细节、合成层等仍要查看返回图像；规则没报问题不等于 UI 无缺陷。 |
| 页面等待与动作 | `ui.wait` 返回显式 condition、超时、稳定性和最后 observation。`ui.act` 支持 DOM synthetic；目标须唯一、可见、未禁用/遮挡，并检查 ref 新鲜度。 | DOM synthetic 只能验证应用事件处理，`trusted:false`，不证明真实窗口输入。动作超时或传输中断有可能已经派发，返回结果未知时应先重新观察，不能盲目重试。 |
| 原生窗口输入 | macOS+cgo 适配用 `NSWindow sendEvent` 派发 click/key，文本通过 focused responder 的 `insertText`；运行时报告 provider、action 和限制。 | 只操作 ding-ssh 自己的窗口，不做全局输入、不抢焦点；目标窗口必须可见且 key/focused。当前 provider 将 `trustedEvent` 明确标成 `unverified`，没有采集 WebView DOM 事件的 `isTrusted` 作为证据；输入文本路径不覆盖 IME、实体键盘、系统快捷键或原生菜单。Windows/Linux 未实现原生输入。 |
| 像素/语义对比 | `ui.compare` 比较已保存 observation 的像素与语义/几何变化，并检查环境是否可比。 | 对比受 observation 留存、viewport、DPR、主题和捕获状态影响。没有基线或环境不匹配时不能把差异解释成 UI 回归。 |
| 自动验证 job | `verify.run` 支持 `desktop.current`、`desktop.boot`，`verify.get` 读取报告；runner 检查 ready、真实像素、截图一致性、运行时错误；boot 还检查组件 registry。 | 当前两个场景都是对**已运行实例**做只读观测；`desktop.boot` 目前不启动应用。runner 的 `inputMode` 是 `none-read-only`，不验证 click/type/key，也不验证业务流程。 |
| 服务/目标探测 | `targets.probe` 支持本机执行的 TCP、TLS、HTTP 检查，限时、限响应体，不返回完整响应内容。 | 不支持从 SSH 远端执行探测，不会自动读取用户保存的服务器/凭据，也不等于 SSH 登录、SFTP 或 tunnel 业务验证。 |
| Wails 官方 MCP | Wails 3.1.0 提供实验性的 MCP 开发辅助能力，可复用其窗口/DOM 调试思路。 | 官方 `screenshot_dom` 是 DOM 摘要，不是最终像素；不能替代当前项目的 host capture。现有设计继续由 ding-ssh MCP 统一鉴权、实例身份和审计。 |

## 原生 `ui.act`：点击与输入验收设计

真机验收需要把四件事分别留证：目标与页面状态、宿主原生派发结果、业务/UI 后置条件、操作后的像素图。单独看到 `accepted=true` 或 `dispatched=true` 不足以说明页面发生了预期变化。

### 前置条件

1. 只对明确的隔离 `.smoke` discovery 文件运行，不使用日常 profile，不填写或读取用户远端目标。
2. 确认 `app.runtimeCapabilities.input.native.available=true`，记录 provider、平台、`actions`、焦点要求及限制。当前实际 native provider 是 macOS+cgo 的 `appkit-nsevent`。
3. smoke 不会自动激活或抢占应用窗口。启动后有 10 秒默认准备时间；在窗口进入 native 动作之前，把隔离实例窗口置于前台并保持可见。若未聚焦，预期是 `WINDOW_NOT_FOCUSED` 安全拒绝，不应当把它当成点击失败后可自动重放。
4. 首先执行一次 `ui.observe`，保存 observationId、pageEpoch、构建身份、viewport、截图 provider 和截图。只使用当前 observation 内的 `targetRef`。

### 可重复、无远端副作用的动作序列

smoke 以命令面板为交互样例，避免连接服务器、保存设置或写入业务数据：

1. 从可操作节点中唯一选择 `data-ui="command-trigger"`，调用 `ui.act` 的 `native-window-events` click。要求宿主接受且报告 dispatch，随后 `ui.wait` 等待 `[data-ui="command-palette"]` 可见，并保存 `native-command-open.png`。
2. 对当前页面重新 observe，从命令面板中唯一选择 combobox 输入框。用 native type 输入一次仅供测试的“不可能匹配”查询；等待 `[data-ui="empty-state"]` 可见，并保存 `native-search-input.png`。输入值不得出现在报告、日志或错误详情中。
3. 再次 observe，使用新的目标 ref 将 Escape 作为 native key 发送；等待命令面板不存在，并保存 `native-command-close.png`。这是验证键盘事件路由和关闭后的页面状态，不宣称覆盖实体键盘、快捷键或 IME。

三步都要求 `accepted=true`、`dispatched=true`、`effectConfirmed=true`、`retrySafe=false`，并在每步报告 provider 和后置 observation。操作后图像应包含真实 host capture 且记录 screenshot provider。任一 effect 条件未满足时报告失败/未知，先获取新状态；不要自动重试动作。

**信任级别必须如实记录：**当前返回的 `trustedEvent=unverified`。这组测试能证明调用了原生窗口 provider，并且页面状态出现了所等待的后置变化；它不能证明 WebView 收到了 `isTrusted=true` 的 DOM 事件。若验收要求证明浏览器信任级别，下一步要增加受限的事件审计探针：只累计 event type、isTrusted、目标 ref 和 operation id，不记录输入文本；再用它关联 native click/key/type 与结果。完成该实现和平台实测前，报告不应使用“浏览器可信事件已证实”这一结论。

本轮 smoke 仅修复了协议握手并补上上述 native 交互序列；**没有启动或重建应用，也没有运行该真机 smoke**。因此原生点击/输入这一项目前是可执行的验收设计，不是本次已通过的结果。

## 建议的完整验收顺序

| 顺序 | 操作 | 通过证据 |
| --- | --- | --- |
| 1. 锁定实例 | 用隔离 discovery 附着，执行 `app.health`、`app.runtimeCapabilities`。 | PID、instanceId、binary SHA/build 与预期一致；ready、窗口状态和 provider 能力有明确结果。 |
| 2. 采集初始画面 | 执行 `app.observe`，保存原始 MCP image、结构化 observation 和 capture 元数据。 | frontend ready；真实像素 capture 成功；DOM 和截图前后序号能证明 stable-window；报告带 coverage 和未覆盖原因。 |
| 3. 扫描可见 UI | 执行 `ui.diagnose`；对有疑问的 nodeRef 执行 `ui.inspect`；人工/AI查看截图与节点位置。 | 缺陷可定位到区域、规则、rect、命中/裁切证据或图像区域；不能只用“规则扫描为零”作无缺陷声明。 |
| 4. 做最小输入闭环 | 执行上面的 command palette native click/type/Escape 序列；每个 action 前刷新 observation。 | 三个显式后置条件逐个成立，动作图像可读取，`retrySafe=false`，原生 trust 仍按 `unverified` 报告。 |
| 5. 只读验证当前页 | 调用 `verify.run(scenarioId="desktop.current")`，用 `verify.get` 取 job 和报告。 | 报告中的 ready、pixels、capture consistency、runtime errors 与 artifact 状态逐项可见；状态为 inconclusive/failed 时按具体断言处理。 |
| 6. 环境矩阵 | 对 macOS、Windows、Linux 分别记录 OS、Wails/WebView、窗口状态、capture/input provider 和限制。 | 只对实际跑过真机的 provider、系统版本和桌面环境宣告支持；不支持项保留为 unsupported/未测。 |

命令行客户端必须完成标准握手：`initialize` 成功取得 `Mcp-Session-Id` 后，使用同一 session 和协议版本发送不带 JSON-RPC id 的 `notifications/initialized`，收到 HTTP 202 后再请求 `tools/list` 或调用工具。`scripts/audit/mcp_observability_smoke.py` 已按此顺序实现；HTTP 错误会带回状态码和限长响应体，便于定位下一次协议问题。

示例（路径替换成实际隔离实例生成的 discovery 文件）：

```sh
python3 scripts/audit/mcp_observability_smoke.py \
  --discovery /path/to/isolated/.smoke/debug.json \
  --output output/mcp-smoke
```

报告写入 `report.json`，并在输出目录保存 smoke 捕获的 PNG。该命令不会启动或重建应用；没有正在运行的隔离实例时应先按项目 launcher 流程创建隔离实例，再执行 smoke。不要把 production 用户数据目录当作测试 profile。

## 当前可引用的既有证据

`docs/validation/mcp-observability-probe-2026-11-26.json` 是对当时运行中的旧 macOS 实例做的只读探针，不是当前改动的 E2E 结果。它显示旧实例工具目录有 124 个工具；旧版 `ui.tree` 不能发现 Vue app 实例，旧版截图只支持 Windows，`app.health` 缺少 frontend/render 健康汇总。由于运行实例未提供可用于源码比对的 build identity，不能将这份历史记录与当前未提交代码视为同一 build，也不能据此声称当前截图或组件注册已在真机通过。

新增的 `frontend/src/debug/observation.test.ts`、`internal/debugsrv/{probes,verify}_test.go`、`internal/desktopcapture/*_test.go` 和 launcher 测试可分别支撑单元/契约验证；本文件不代替实际测试输出。当前 smoke 的语法检查也只证明 Python 文件可解析，不证明 MCP 握手或窗口输入运行成功。

## 继续补齐的工作

1. 在隔离 macOS 生产构建运行本 smoke，核对 native provider、窗口聚焦、三项后置状态、三张操作图和 `verify.get` 报告；将真实报告与构建身份一并保存。
2. 增加事件信任审计与 operationId 关联，并在 WebKit 实际运行中验证 click/key/type 的事件 trust 结果。不要仅因为 NSEvent 由 Cocoa 创建就假定 WebView 的 `isTrusted`。
3. 增加故意制造的视觉缺陷 fixture：裁切、遮挡、零尺寸/错尺寸 canvas、字体回退/加载失败和越界弹层。逐项确认图片能让 AI 一眼看出、规则能定位、比较环境可复现。
4. Windows 上实测 HWND/client capture 的缩放和遮挡语义；Linux 建立真实 capture provider 或保持明确 unsupported。native input 按平台单独设计与验收。
5. 补远端视角的受限探测、网络请求生命周期和关键业务流程场景；当前 `targets.probe` 的 local 可达性不能替代这些能力。
