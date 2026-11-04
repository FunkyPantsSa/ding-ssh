# P4 UI 自动化回归记录

> 日期：2026-11-03。静态与构建项已在本机完成；需要运行中桌面 WebView 或多平台设备的项目保留为发布门禁，不伪造通过结果。

| # | 场景 | 当前状态 | 证据 / 后续 |
|---:|---|---|---|
| 1 | 布局骨架 | 代码就绪 | `ui.layout` 已以 `data-ui` 为 primary，并报告 `viaFallback` |
| 2 | 6 个现有视图 | 部分通过 | 浏览器运行时已验证 workspace → settings；其余留给 Wails 回归 |
| 3 | 11 个设置分区 | 通过 | 实际可访问树显示 11 个入口；快捷键页成功打开并展示完整注册表 |
| 4 | 命令面板 | 通过 | 实际验证打开、`set` 搜索、Enter 执行、再次打开出现 RECENT/清除 |
| 5 | 终端存在 | 待运行时 | 需已连接或本地终端标签 |
| 6 | 右栏 | 待运行时 | `[data-ui="context-panel"]` |
| 7 | 服务器表 | 代码就绪 | 行使用业务 id 的 `data-testid` 与 `data-state` |
| 8 | 设置保存 | 待运行时 | 需保存后重启验证 |
| 9 | 旧设置迁移 | 待样本 | 需发布用旧版 `settings.json` 运行两次验证幂等 |
| 10 | 令牌 | 构建通过 | background / primary / radius-md / font-sans 均由 token 层定义 |
| 11 | 控制台 | 待原生窗口 | 纯浏览器预览只有预期的 Wails runtime 缺失错误；不能替代原生 WebView 结论 |
| 12 | 快照 | 待运行时 | dark/light、900×600、125%/150%、reduced-motion |

## 本地自动门禁

`./scripts/verify-ui2.sh`：通过。

- CSS：77,371 B；gzip 15,950 B（预算 100,000 / 30,000）
- 主 JS：889,246 B；gzip 260,449 B（预算 900,000 / 270,000）
- 字体：12 文件，6 个 `@font-face`（预算 32）
- dist：1,335,296 B（预算 2.5 MiB）
- Go：`go test ./...` 通过

## 本机运行时抽查

- `wails dev` 成功完成 bindings、前端编译、Go 编译、打包与自签名，应用进程启动。
- Vite 运行时可访问树验证：空工作区、命令面板 dialog/listbox、搜索 `set`、执行设置命令、11 个设置分区、快捷键注册表、最近命令均符合预期。
- P4-07 迁移后再次验证：计算样式中的 `--background` / `--foreground` / `--primary` 均有有效值，`--ink-950` 为空；`[data-ui="sidebar"]` 与打开后的 `[data-ui="command-palette"]` 均命中。
- 自动化宿主无法附着 Wails 开发 `.app`（LaunchServices 报开发包 executable 定位异常），因此本轮未把原生 `ui.console` / 截图标为通过；纯浏览器中的 `window.runtime` 缺失错误属于脱离 Wails 宿主的预期限制。
