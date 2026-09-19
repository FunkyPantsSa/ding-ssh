// DOM 测量工具：统一处理 CSS zoom 下的坐标系差异。
//
// 背景：App 通过 `.app-shell { zoom: z }`（z = uiScale/100）实现整体界面缩放，
// 该缩放会放大显示，但布局坐标系（offset*/client*/CSS 长度、绝对定位）仍按未缩放的
// 逻辑像素解释。而 WebKit/标准行为下 getBoundingClientRect() 返回的是**含 zoom 的渲染
// 尺寸**（渲染像素）。因此：
//   - 用 getBoundingClientRect 量尺寸再喂给绝对定位 → 尺寸偏大 z 倍 → 右侧/底部空一截
//   - mouse clientX/Y 处于渲染坐标系，绝对定位按未缩放坐标解释 → 拖拽换算需要反向折算
//
// 本文件提供两个语义清晰的工具：
//   getElementRect(el)  —— 元素在**布局（未缩放）坐标系**中的矩形（含定位与尺寸）。
//   unzoom(delta, el)   —— 把一段渲染像素位移（如鼠标增量）换算为布局像素。

/** 元素在当前缩放上下文中的有效 zoom（无 CSS zoom 时返回 1）。 */
export function currentZoom(el: Element | null | undefined): number {
  if (!el) return 1
  const z = (el as Element & {currentCSSZoom?: number}).currentCSSZoom
  if (typeof z === 'number' && Number.isFinite(z) && z > 0) return z
  // 兜底：读取计算样式 zoom（webkit 也支持），解析为数值。
  const cssZoom = window.getComputedStyle(el).zoom
  if (!cssZoom || cssZoom === 'normal') return 1
  const n = parseFloat(cssZoom)
  return Number.isFinite(n) && n > 0 ? n : 1
}

export interface PlainRect {
  left: number
  top: number
  right: number
  bottom: number
  width: number
  height: number
}

/**
 * 返回元素在「布局（未缩放）坐标系」中的矩形。
 * 实现：getBoundingClientRect ÷ currentCSSZoom。引擎（Chromium/WebKit）对 gBCR 是否含
 * zoom 的行为已趋同（含），无 zoom 时除法恒为 1，可安全用于所有环境。
 */
export function getElementRect(el: Element | null | undefined): PlainRect {
  if (!el) return {left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0}
  const r = el.getBoundingClientRect()
  const z = currentZoom(el)
  return {
    left: r.left / z,
    top: r.top / z,
    right: r.right / z,
    bottom: r.bottom / z,
    width: r.width / z,
    height: r.height / z,
  }
}

/**
 * 把渲染坐标系中的增量（如 mousemove 的 clientX - startX）换算为布局像素增量。
 * 鼠标坐标相对 viewport（含 zoom），而绝对定位偏移按未缩放坐标解释，故除以 zoom。
 */
export function unzoom(delta: number, el: Element | null | undefined): number {
  return delta / currentZoom(el)
}
