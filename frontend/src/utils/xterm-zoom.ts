// xterm 在 CSS zoom 祖先中的鼠标坐标修正。
//
// 界面缩放用 `.app-shell { zoom: z }` 实现，但各引擎对 zoom 的坐标系处理并不一致：
//
//   Chromium（WebView2）
//     - getBoundingClientRect() 处于「显示（已缩放）」坐标系：rect.width ≈ offsetWidth × z
//     - 鼠标 clientX/Y 也是显示坐标
//     → 布局偏移 = (clientX - rect.left) / z
//
//   WebKit（WKWebView / macOS，以及 Safari）
//     - getBoundingClientRect() 仍是「布局（未缩放）」坐标：rect.width ≈ offsetWidth
//     - 鼠标 clientX/Y 却是显示坐标（实测 80% 时 1px 鼠标位移 = 1px clientX）
//     - xterm 的单元格尺寸也是布局坐标
//     → 布局坐标 = origin + (clientX - origin) / z
//     其中 origin 是 zoom 根（.app-shell）在视口中的位置；本应用固定在 (0,0)，
//     但这里仍按实测值计算，避免以后外壳出现偏移时再次错位。
//
// 上一版直接用设置里的 uiScale 当比例：在 Chromium 上正确，在 WKWebView 上会把本就
// 偏移的方向弄反/偏移量不足（选区随距离整体偏移）。这里改为实测：
//   - zoom 值：逐级读取祖先的 computed zoom 相乘（Chromium/WebKit 都可用），
//     取不到时退回设置值；
//   - gBCR 是否含 zoom：rect.width/offsetWidth 与 rect.height/offsetHeight，
//     比例≈1 判定为 WebKit 的布局坐标，否则为 Chromium 的显示坐标。

/** xterm 内部鼠标相关服务的形状（只用到方法签名）。 */
type MouseCoordFn = (event: MouseEvent, element: HTMLElement, ...rest: unknown[]) => unknown

interface XtermCore {
  _mouseService?: Record<string, unknown>
  _selectionService?: Record<string, unknown>
}

/** 需要修正的三个入口：选区/链接悬停、鼠标上报、拖拽到边缘时自动滚动。 */
const PATCH_TARGETS: Array<[keyof XtermCore, string]> = [
  ['_mouseService', 'getCoords'],
  ['_mouseService', 'getMouseReportCoords'],
  ['_selectionService', '_getMouseEventScrollAmount'],
]

/** gBCR / offset* 比例接近 1 视为同一坐标系（容忍亚像素舍入，如 0.9992）。 */
const SAME_SPACE_EPSILON = 0.02

interface ZoomContext {
  zoom: number
  /** zoom 根在视口（显示坐标）中的原点；单层 zoom 下事件坐标 = origin + zoom × (布局坐标 - origin)。 */
  originX: number
  originY: number
}

/** 逐级读取祖先 computed zoom，得到当前元素的累计缩放与 zoom 根原点。 */
function measureZoomContext(element: HTMLElement, fallback: number): ZoomContext {
  let zoom = 1
  let found = false
  let root: HTMLElement | null = null
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    const raw = getComputedStyle(node).zoom
    if (!raw || raw === 'normal') continue
    const value = Number.parseFloat(raw)
    if (!Number.isFinite(value) || value <= 0) continue
    zoom *= value
    found = true
    // 循环向上，最后写入的是最外层非 1 的 zoom 祖先
    if (Math.abs(value - 1) > 1e-6) root = node
  }
  if (!found || !(zoom > 0) || zoom > 20) {
    zoom = fallback > 0 ? fallback : 1
  }
  let originX = 0
  let originY = 0
  if (root && Math.abs(zoom - 1) > 1e-6) {
    // zoom 根自身的 gBCR 不受自己的 zoom 影响，仍处于视口（显示）坐标系。
    const r = root.getBoundingClientRect()
    originX = r.left
    originY = r.top
  }
  return {zoom, originX, originY}
}

/**
 * 判断 gBCR 是否已包含 zoom。
 * 返回 null 表示元素未布局 / 测量不可信，调用方应保持原样、不要折算。
 */
function rectIsInEventSpace(element: HTMLElement, rect: DOMRect): boolean | null {
  const ratios: number[] = []
  if (element.offsetWidth > 0 && rect.width > 0) {
    ratios.push(rect.width / element.offsetWidth)
  }
  if (element.offsetHeight > 0 && rect.height > 0) {
    ratios.push(rect.height / element.offsetHeight)
  }
  if (!ratios.length) return null
  const ratio = ratios.reduce((sum, r) => sum + r, 0) / ratios.length
  if (!(ratio > 0.05) || ratio > 20) return null
  // ≈1 → WebKit 的布局坐标；≈zoom → Chromium 的显示坐标
  return Math.abs(ratio - 1) > SAME_SPACE_EPSILON
}

/**
 * 给 xterm 实例装上 CSS zoom 坐标修正。
 *
 * @param term xterm Terminal 实例（访问内部服务，仅此处使用私有字段）
 * @param getZoom 设置里的界面缩放比（1 = 100%），仅在 DOM 测不出 zoom 时兜底
 * @returns 实际打上补丁的方法名，便于自检/测试
 */
export function patchXtermZoomCoords(term: unknown, getZoom: () => number): string[] {
  const core = (term as {_core?: XtermCore} | undefined)?._core
  if (!core) return []
  const patched: string[] = []

  for (const [serviceKey, name] of PATCH_TARGETS) {
    const service = core[serviceKey]
    const original = service?.[name]
    if (!service || typeof original !== 'function') continue

    // 必须绑定回服务实例：xterm 内部实现依赖 this（_charSizeService / _renderService 等），
    // 直接调用会抛 TypeError，导致鼠标按下被中断、终端里完全选不中内容。
    const call = (original as MouseCoordFn).bind(service)
    service[name] = (event: MouseEvent, element: HTMLElement, ...rest: unknown[]) => {
      if (!element || !event || typeof event.clientX !== 'number') {
        return call(event, element, ...rest)
      }
      const {zoom, originX, originY} = measureZoomContext(element, getZoom())
      if (!(zoom > 0) || Math.abs(zoom - 1) < 1e-4) {
        return call(event, element, ...rest)
      }
      const rect = element.getBoundingClientRect()
      const inEventSpace = rectIsInEventSpace(element, rect)
      if (inEventSpace === null) {
        // 未布局（display:none 等）或比例异常：不能安全换算，保持原样
        return call(event, element, ...rest)
      }

      let clientX: number
      let clientY: number
      if (inEventSpace) {
        // Chromium：gBCR 与鼠标坐标同为显示坐标，先折回布局偏移
        clientX = rect.left + (event.clientX - rect.left) / zoom
        clientY = rect.top + (event.clientY - rect.top) / zoom
      } else {
        // WebKit：gBCR 是布局坐标、鼠标是显示坐标，把显示坐标折回布局坐标
        clientX = originX + (event.clientX - originX) / zoom
        clientY = originY + (event.clientY - originY) / zoom
      }

      // 只替换鼠标坐标：原型链仍是原生事件（target / button / buttons 等照常可读）
      const adjusted = Object.create(event) as MouseEvent
      Object.defineProperty(adjusted, 'clientX', {value: clientX, enumerable: true})
      Object.defineProperty(adjusted, 'clientY', {value: clientY, enumerable: true})
      return call(adjusted, element, ...rest)
    }
    patched.push(name)
  }

  return patched
}
