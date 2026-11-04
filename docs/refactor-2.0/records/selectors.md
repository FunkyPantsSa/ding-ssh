# UI 2.0 自动化选择器契约

> 冻结基线：`main@d0fe4c1`（2026-11-03）  
> P4-07 已完成：自动化只使用稳定 `data-ui` / `data-section` / `data-testid` 标识，旧 class 不再是调试桥 fallback。

## 契约规则

1. 自动化只依赖本表的稳定标识，不新增 `:nth-child`、深层子代链或纯位置选择器。
2. 迁移顺序固定为：增加 `data-ui` → 修改 DOM → 跑 `ui.layout` 和 `ui.assert` → 删除旧选择器依赖。
3. xterm 自有的 `.xterm-viewport`、`.xterm-screen`、`.xterm-scroll-area` 保持原语义，不用 `transform`、`filter` 或额外 `overflow` 覆盖。
4. 浮层允许覆盖底层区域；普通区域出现非祖先/后代重叠时视为回归。

## `ui.layout` 契约

| key | 稳定选择器 | 类型 | 状态 |
|---|---|---|---|---|
| `topbar` | `[data-ui="topbar"]` | 固定区域 | 已迁移 |
| `nav` | `[data-ui="sidebar"]` | 固定区域 | 已迁移 |
| `sftp` | `[data-ui="context-panel"]` | 可选区域 | 已迁移 |
| `terminal` | `[data-ui="terminal"]` | 核心区域 | 已迁移 |
| `statusbar` | `[data-ui="statusbar"]` | 固定区域 | 已迁移 |
| `commandPalette` | `[data-ui="command-palette"]` | 浮层 | 已迁移 |
| `quickConnect` | `[data-ui="quick-connect"]` | 浮层 | 已迁移 |
| `settingsPage` | `[data-ui="settings"]` | 页面 | 已迁移 |
| `contextMenu` | `[data-ui="context-menu"]` | 浮层 | 已迁移 |

## 变更门禁

- [x] 9 个 `ui.layout` key 只使用稳定选择器。
- [x] 旧 CSS token 别名已删除，源码 grep 为 0。
- [ ] 原生 WebView 的 12 项自动化回归与三平台冒烟（发布门禁）。
