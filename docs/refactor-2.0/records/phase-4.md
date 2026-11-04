# 阶段 4 验收记录

## 1. 环境

| 项 | 值 |
|---|---|
| 日期 | 2026-11-03 |
| 平台 | macOS / 本地构建门禁 |
| 前端 | Vue 3.5 + Vite 7 + WebView 运行时待冒烟 |
| 调试模式 | 代码与选择器契约已校验；运行时回归见 `ui-regression.md` |

## 2. 完成项

| 任务 | 状态 | 说明 |
|---|---|---|
| P4-01 | 已完成 | 命令分组、最近记录、搜索排序、防抖、渲染上限 |
| P4-02 | 已完成（现有视图） | overview/transfers 已登记；未虚构独立 debug/about 视图 |
| P4-03 | 已完成 | 唯一快捷键注册表、焦点/IME 保护、设置速查 |
| P4-04 | 基础完成 | Toast/Skeleton/ErrorState 已统一；业务页按后续触点逐步接入 |
| P4-05 | 已完成 | 100–180ms 与 reduced-motion |
| P4-06 | 已完成 | 关键布局与动态项稳定标识 |
| P4-07 | 已完成 | `ui.layout` fallback 与 `aliases.css` 已删除；源码旧 token grep 为 0 |
| P4-08 | 部分完成 | 浏览器运行时已验证命令/最近/设置；原生 WebView 余项见独立记录 |
| P4-09 | 已完成 | 所有构建预算达标 |
| P4-10 | 待发布环境 | Windows/macOS/Linux 冒烟不可由单机静态测试替代 |
| P4-11 | 已完成 | README、CHANGELOG、设计规范、验收记录 |
| P4-12 | 待发布授权 | 不擅自打 tag 或创建 Release |

## 3. 门禁结果

- [x] Vue 类型检查与生产构建
- [x] `go test ./...`
- [x] CSS / JS / 字体 / dist 性能预算
- [x] 命令与快捷键单一来源
- [x] stable DOM selector primary 路径
- [ ] 运行中桌面 12 项自动化回归
- [ ] 三平台 WebView / IME / 高 DPI 冒烟与截图
- [ ] 旧设置样本升级 / 降级演练
- [ ] 发布 tag 与 Release

## 4. 发布与回滚

发布前执行 `scripts/verify-ui2.sh`，再按 `05-阶段4-交互自动化与交付.md` 的 P4-10/P4-12 清单完成三平台冒烟。代码回滚使用阶段合并提交的 `git revert`；设置迁移可从 `ding-ssh:ui2-migration-backup` 与发布前 `settings.json` 备份恢复。
