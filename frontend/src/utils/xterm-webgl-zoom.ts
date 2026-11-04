// WebGL 渲染器在 CSS zoom 下的「位图 / 网格」一致性修正。
//
// 背景：界面缩放用 `.app-shell { zoom: z }` 实现（z = uiScale / 100）。
// `@xterm/addon-webgl` 的 canvas 位图尺寸来自 ResizeObserver 的
// `devicePixelContentBoxSize`，而 Chromium 的这个值**含 CSS zoom**：zoom = 1.25 时
// 实测位图 990×553，渲染器内部网格却只有 792×442（网格按未缩放的 CSS 像素 × dpr 算）。
//
// 更麻烦的是 `_setCanvasDevicePixelDimensions()` 只改位图，**不重设 GL viewport /
// resolution uniform / 顶点归一化**（那三件事只在 `handleResize()` 里做），于是整幅
// 网格被画进超大位图的左下角（WebGL 原点在左下）——内容整体下移、比例也不对。
// 缓冲区、DOM 几何、scrollTop 全都正确，只有画出来的像素错位，所以表现为
// 「终端顶部前几行看不到，且缓冲区已在顶部无法上滚」，`refresh()` /
// `clearTextureAtlas()` 都纠不回来。
//
// 修法：
//   1. zoom ≠ 1 时丢弃观察器上报的（含 zoom 的）尺寸，位图保持 `handleResize()`
//      按未缩放网格算出的尺寸；
//   2. 每次 fit / 缩放切换后自检一次「位图 == 网格」，不一致就重跑 `handleResize()`，
//      把 viewport / uniform / 顶点一起补齐（其它引擎或私有实现变化时也能自愈）。
//
// 补丁失效（addon 私有字段改名等）时返回 false，调用方按老策略回退 Canvas。

/** addon-webgl 渲染器里我们需要的字段（私有 API，仅此处使用）。 */
interface WebglRendererLike {
  _canvas?: HTMLCanvasElement
  _terminal?: {cols: number; rows: number}
  dimensions?: {
    css?: {canvas?: {width: number; height: number}}
    device?: {canvas?: {width: number; height: number}}
  }
  _setCanvasDevicePixelDimensions?: (width: number, height: number) => void
  handleResize?: (cols: number, rows: number) => void
}

interface WebglAddonLike {
  _renderer?: WebglRendererLike
}

function rendererOf(addon: unknown): WebglRendererLike | null {
  const r = (addon as WebglAddonLike | null | undefined)?._renderer
  if (!r || typeof r !== 'object') return null
  if (!r._canvas || typeof r._setCanvasDevicePixelDimensions !== 'function') return null
  if (typeof r.handleResize !== 'function') return null
  return r
}

function gridSize(r: WebglRendererLike): {width: number; height: number} | null {
  const dev = r.dimensions?.device?.canvas
  if (!dev || !(dev.width > 0) || !(dev.height > 0)) return null
  return {width: Math.round(dev.width), height: Math.round(dev.height)}
}

/**
 * 把 canvas 位图拉回「渲染器自己的网格尺寸」，并在需要时重跑 handleResize()
 * （重设 GL viewport / resolution uniform / 顶点归一化 / 字符图集）。
 *
 * @returns true 表示做了修正（位图原本与网格不一致）
 */
export function syncWebglCanvas(addon: unknown): boolean {
  try {
    const r = rendererOf(addon)
    if (!r) return false
    const grid = gridSize(r)
    const canvas = r._canvas
    const term = r._terminal
    if (!grid || !canvas || !term) return false
    if (canvas.width === grid.width && canvas.height === grid.height) return false
    r.handleResize!(term.cols, term.rows)
    return true
  } catch {
    // 私有 API 行为变化时绝不能把异常抛进 fit() / 渲染流程
    return false
  }
}

/**
 * 给 WebglAddon 打上 CSS zoom 修正补丁。
 *
 * @param addon 已 `loadAddon` 的 WebglAddon 实例
 * @param getZoom 当前界面缩放比（1 = 100%）
 * @returns 是否打补丁成功（false 表示私有 API 不可用，调用方应回退 Canvas）
 */
export function patchWebglZoomCanvas(addon: unknown, getZoom: () => number): boolean {
  const r = rendererOf(addon)
  if (!r) return false
  const original = r._setCanvasDevicePixelDimensions!.bind(r)

  r._setCanvasDevicePixelDimensions = (width: number, height: number) => {
    const zoom = getZoom()
    // 100% 时观察器的值就是真·设备像素（还能修正非整数 dpr 的舍入），保持原行为
    if (!(zoom > 0) || zoom === 1) {
      original(width, height)
      return
    }
    // zoom ≠ 1：上报值含 zoom，与网格不一致 → 丢弃，必要时把位图/视口重新对齐
    syncWebglCanvas(addon)
  }

  // 观察器可能已经用「含 zoom」的尺寸设过位图了，立刻纠正一次
  syncWebglCanvas(addon)
  return true
}

/** 渲染器当前几何快照，供日志/诊断用（失败返回 null，绝不抛错）。 */
export function webglCanvasReport(addon: unknown, zoom: number): string | null {
  try {
    return reportOf(addon, zoom)
  } catch {
    return null
  }
}

function reportOf(addon: unknown, zoom: number): string | null {
  const r = rendererOf(addon)
  if (!r) return null
  const grid = gridSize(r)
  const canvas = r._canvas
  const css = r.dimensions?.css?.canvas
  const rect = canvas?.getBoundingClientRect()
  const parts = [
    `uiScale=${Math.round(zoom * 100)}%`,
    `dpr=${window.devicePixelRatio}`,
    `grid=${grid ? `${grid.width}x${grid.height}` : '?'}`,
    `bitmap=${canvas ? `${canvas.width}x${canvas.height}` : '?'}`,
    `css=${css ? `${css.width}x${css.height}` : '?'}`,
    `rect=${rect ? `${Math.round(rect.width)}x${Math.round(rect.height)}` : '?'}`,
  ]
  return parts.join(' ')
}
