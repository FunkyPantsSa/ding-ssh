/**
 * 多个终端共享字形图集时的重绘协调。
 *
 * xterm 6 + addon-webgl 0.19 会在字体/主题/DPR 相同时让多个 Terminal 共用同一个
 * TextureAtlas（`CharAtlasCache` 的 ownedBy 机制）。任何一方
 * `clearTextureAtlas()`（字体加载完成、系统恢复）或图集页合并后，其它终端的 WebGL
 * 顶点模型仍指向旧纹理坐标；它们又常处于隐藏标签页、不会立即重绘，于是切回来时
 * 整屏字形错位（乱码）。
 *
 * 注意：`term.refresh()` 并不够 —— WebglRenderer 的 `_updateModel` 只在单元格内容
 * 变化时才重写顶点，未变化的行会一直保留旧坐标。必须显式丢弃渲染器的顶点模型
 * （WebglAddon 私有字段 `_clearModel`，与 xterm-webgl-zoom 一样属于受控的私有 API
 * 用法），再整屏重绘。上游修复见 xtermjs/xterm.js#6042、#6055。
 */
type ModelTarget = {
  /** 丢弃 WebGL 顶点模型缓存，让下一次重绘按当前图集坐标重建。 */
  invalidateModel: () => void
  /** 整屏重绘（0 .. rows-1）。 */
  refresh: () => void
}

const terminals = new Set<ModelTarget>()
let pending = false
let globalHooksInstalled = false

function installGlobalHooks() {
  if (globalHooksInstalled || typeof window === 'undefined') return
  globalHooksInstalled = true
  // 从其它窗口切回来时，GPU 侧的字形图集/渲染缓存可能已经失效：重建模型并重绘一次。
  window.addEventListener('focus', scheduleSharedAtlasReset)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') scheduleSharedAtlasReset()
  })
}

export function registerSharedAtlasTerminal(term: ModelTarget): void {
  terminals.add(term)
  installGlobalHooks()
}

export function unregisterSharedAtlasTerminal(term: ModelTarget): void {
  terminals.delete(term)
}

/** 立即丢弃所有终端的顶点模型并整屏重绘（共享图集刚被清空/合并时调用）。 */
export function resetSharedAtlasTerminals(): void {
  // 复制一份再遍历：重绘过程中可能有终端被注销
  const targets = [...terminals]
  for (const target of targets) {
    try {
      target.invalidateModel()
    } catch {
      /* 终端可能正在销毁，忽略 */
    }
  }
  for (const target of targets) {
    try {
      target.refresh()
    } catch {
      /* 同上 */
    }
  }
}

/** 合并同一轮里的多次图集变化，下一轮事件循环统一重建模型 + 重绘。 */
export function scheduleSharedAtlasReset(): void {
  if (pending) return
  pending = true
  window.setTimeout(() => {
    pending = false
    resetSharedAtlasTerminals()
  }, 0)
}
