// xterm 在 CSS zoom 祖先中的鼠标坐标修正。
//
// 背景：界面缩放用 `.app-shell { zoom: z }` 实现。此时元素渲染尺寸与鼠标 clientX/Y 同处
// 「已缩放」坐标系（getBoundingClientRect 含 zoom），而 xterm 用 canvas.measureText 量出的
// 单元格尺寸是**未缩放**的逻辑像素；两者相除会让列/行被放大 z 倍，表现为 uiScale≠100% 时
// 选区高亮、鼠标上报（vim / tmux 鼠标模式）与链接悬停整体偏移。
//
// 修法：在交给 xterm 内部换算前，把鼠标增量按缩放比折算回未缩放坐标系。折算比用设置里的
// uiScale 精确值，而不是 DOM 实测比值——WebKit 下实测比值恒为 1，那样补丁会失效。

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

/**
 * 给 xterm 实例装上 CSS zoom 坐标修正。
 *
 * @param term xterm Terminal 实例（访问内部服务，仅此处使用私有字段）
 * @param getZoom 当前缩放比（1 = 100%）
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
      const zoom = getZoom()
      if (!(zoom > 0) || zoom === 1 || !element || !event || typeof event.clientX !== 'number') {
        return call(event, element, ...rest)
      }
      // 只替换鼠标坐标：原型链仍是原生事件（target / button / buttons 等照常可读）
      const rect = element.getBoundingClientRect()
      const adjusted = Object.create(event) as MouseEvent
      Object.defineProperty(adjusted, 'clientX', {
        value: rect.left + (event.clientX - rect.left) / zoom,
        enumerable: true,
      })
      Object.defineProperty(adjusted, 'clientY', {
        value: rect.top + (event.clientY - rect.top) / zoom,
        enumerable: true,
      })
      return call(adjusted, element, ...rest)
    }
    patched.push(name)
  }

  return patched
}
