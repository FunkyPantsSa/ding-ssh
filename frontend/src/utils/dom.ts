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

/** 元素所处的累计 zoom（含祖先；Chromium 的 currentCSSZoom 直接给出有效值）。 */
function effectiveZoom(el: Element | null | undefined): number {
  if (!el) return 1
  const direct = (el as Element & {currentCSSZoom?: number}).currentCSSZoom
  if (typeof direct === 'number' && Number.isFinite(direct) && direct > 0) return direct
  let zoom = 1
  for (let node: Element | null = el; node; node = node.parentElement) {
    const raw = window.getComputedStyle(node).zoom
    if (!raw || raw === 'normal') continue
    const value = parseFloat(raw)
    if (Number.isFinite(value) && value > 0) zoom *= value
  }
  return zoom > 0 ? zoom : 1
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
 * 返回元素在「视口显示坐标」中的矩形：用于 Teleport 到 body、以 fixed 定位的弹层
 * （下拉菜单、浮层），这些弹层的 left/top/bottom 都按显示像素解释。
 *
 * 坐标系判定与 xterm 缩放修正一致：
 *   - getBoundingClientRect / offsetWidth ≈ zoom → gBCR 已含 zoom（Chromium），直接用；
 *   - 比例≈1 → gBCR 仍是布局坐标（WKWebView），乘 zoom 回到显示坐标。
 * 本应用唯一的 zoom 根是铺满窗口的 `.app-shell`（视口原点 0,0），因此乘法成立。
 */
export function getDisplayRect(el: Element | null | undefined): PlainRect {
  const zero = {left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0}
  if (!el) return zero
  const r = el.getBoundingClientRect()
  const zoom = effectiveZoom(el)
  const raw = {left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height}
  if (Math.abs(zoom - 1) < 1e-4) return raw

  const html = el as HTMLElement
  const ratios: number[] = []
  if (html.offsetWidth > 0 && r.width > 0) ratios.push(r.width / html.offsetWidth)
  if (html.offsetHeight > 0 && r.height > 0) ratios.push(r.height / html.offsetHeight)
  const ratio = ratios.length ? ratios.reduce((sum, value) => sum + value, 0) / ratios.length : 1
  // 判定 gBCR 所在的坐标系：更接近 zoom → 显示坐标（Chromium）；更接近 1 → 布局坐标（WebKit）。
  // 不用固定阈值：小元素（按钮）的 offset* 与 gBCR 会因边框/亚像素舍入产生几个百分点差异，
  // 固定 2% 阈值会把 WebKit 误判成 Chromium。
  const closerToZoom = Math.abs(ratio - zoom) < Math.abs(ratio - 1)
  if (closerToZoom) return raw
  return {
    left: r.left * zoom,
    top: r.top * zoom,
    right: r.right * zoom,
    bottom: r.bottom * zoom,
    width: r.width * zoom,
    height: r.height * zoom,
  }
}

/**
 * 把渲染坐标系中的增量（如 mousemove 的 clientX - startX）换算为布局像素增量。
 * 鼠标坐标相对 viewport（含 zoom），而绝对定位偏移按未缩放坐标解释，故除以 zoom。
 */
export function unzoom(delta: number, el: Element | null | undefined): number {
  return delta / currentZoom(el)
}
