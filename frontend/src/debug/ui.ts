// 调试模式：UI 观测与实验（M5 + M6）的前端实现。
//
// 分工（与 internal/debugsrv/ui.go 配套）：
//   - 本文件只提供「页面内的事实与动作」：DOM 几何 / 计算样式 / 设计令牌 / 组件树 /
//     控制台缓冲 / 元素事实 / 指纹 / 合成事件 / store 读写 / 窗口控制；
//   - Go 侧负责裁剪、体积上限、截图坐标换算、像素与指纹差异、快照落盘与 MCP image 内容。
//
// 两条硬约束：
//   1. **不引入新依赖**（只用 vue / pinia / Wails runtime 与浏览器 API）；
//   2. **不调用任何 save()**：setToken / injectCSS / storePatch 都只改内存态，
//      刷新即还原（写类工具的描述里已向 AI 声明）。
//
// 控制台钩子（installDebugUI）由 main.ts 在挂载后调用一次：它包装 console.*、
// window.onerror 与 Vue 的 app.config.errorHandler —— 都是**包裹而不是覆盖**，
// 保证应用原有的输出与错误处理行为完全不变。
import {nextTick} from 'vue'
import {getActivePinia} from 'pinia'
import {
  WindowGetPosition,
  WindowGetSize,
  WindowIsMaximised,
  WindowIsMinimised,
  WindowMaximise,
  WindowMinimise,
  WindowReload,
  WindowSetPosition,
  WindowSetSize,
  WindowUnmaximise,
  WindowUnminimise,
} from '../../wailsjs/runtime/runtime'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {useUIStore} from '../stores/ui'
import {defaultPreset, presetById} from '../theme/presets'

// ---- 基础工具 ----

type Args = Record<string, unknown>

const str = (v: unknown, fallback = ''): string => (typeof v === 'string' ? v : fallback)
const bool = (v: unknown): boolean => v === true
const num = (v: unknown, fallback = 0): number => {
  const n = Number(v)
  return Number.isFinite(n) ? n : fallback
}
const clamp = (n: number, lo: number, hi: number): number => Math.max(lo, Math.min(hi, n))
const clip = (s: string, max: number): string => (s.length <= max ? s : s.slice(0, max) + `…（已截断 ${s.length - max} 字符）`)
const errText = (e: unknown): string => (e instanceof Error ? e.message : String(e))

/** 等一帧：让 Vue 的更新与浏览器布局完成（保证「返回即可截图」）。 */
function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()))
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

interface Rect {
  x: number
  y: number
  w: number
  h: number
}

function rectOf(el: Element): Rect {
  const r = el.getBoundingClientRect()
  return {x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height)}
}

function normalizeRect(v: unknown): Rect | null {
  if (!v || typeof v !== 'object') return null
  const m = v as Record<string, unknown>
  const r = {x: num(m.x), y: num(m.y), w: num(m.w), h: num(m.h)}
  if (r.w <= 0 || r.h <= 0) return null
  return {x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.w), h: Math.round(r.h)}
}

function classList(el: Element): string[] {
  const raw = el.getAttribute('class') ?? ''
  return raw.split(/\s+/).filter(Boolean).slice(0, 12)
}

function describe(el: Element | null): {tag: string; id: string; classes: string[]} {
  if (!el) return {tag: '', id: '', classes: []}
  return {tag: el.tagName.toLowerCase(), id: el.id || '', classes: classList(el)}
}

/** textContent 归一化（连续空白折叠成一个空格）。 */
function textOf(el: Element): string {
  return (el.textContent ?? '').replace(/\s+/g, ' ').trim()
}

/**
 * 可见性判定：display/visibility/opacity + 是否有布局盒。
 * 注意这是「CSS 可见」，不代表在视口内（视口内另由 inViewport 表示）。
 */
function isVisible(el: Element): boolean {
  const he = el as HTMLElement
  if (he.hidden) return false
  const cs = getComputedStyle(el)
  if (cs.display === 'none' || cs.visibility === 'hidden' || cs.visibility === 'collapse') return false
  if (Number(cs.opacity) === 0) return false
  if (he.getClientRects().length === 0) return false
  if (cs.position !== 'fixed' && he.offsetParent === null && el.tagName !== 'BODY' && el.tagName !== 'HTML') return false
  const r = he.getBoundingClientRect()
  return r.width > 0 && r.height > 0
}

function inViewport(el: Element): boolean {
  const r = el.getBoundingClientRect()
  return r.bottom > 0 && r.right > 0 && r.top < window.innerHeight && r.left < window.innerWidth
}

/** 该元素上所有 --* 自定义属性的当前值（计算样式 + 内联样式）。 */
function cssVarsOf(el: Element): Record<string, string> {
  const out: Record<string, string> = {}
  const cs = getComputedStyle(el)
  for (let i = 0; i < cs.length; i++) {
    const name = cs.item(i)
    if (name && name.startsWith('--')) out[name] = cs.getPropertyValue(name).trim()
  }
  const inline = (el as HTMLElement).style
  for (let i = 0; i < inline.length; i++) {
    const name = inline.item(i)
    if (name && name.startsWith('--')) out[name] = inline.getPropertyValue(name).trim()
  }
  return out
}

function styleProps(el: Element, props: string[]): Record<string, string> {
  const cs = getComputedStyle(el)
  const out: Record<string, string> = {}
  for (const p of props) {
    const name = str(p).trim()
    if (!name) continue
    out[name] = cs.getPropertyValue(name).trim()
  }
  return out
}

function uiScaleFraction(): number {
  try {
    const s = useSettingsStore()
    const v = Number(s.uiScale)
    return Number.isFinite(v) && v > 0 ? v / 100 : 1
  } catch {
    return 1
  }
}

/** 窗口 / 视口几何：Go 侧换算「CSS 坐标 → 截图像素」的依据。 */
function windowInfo(): Record<string, unknown> {
  return {
    viewport: {
      width: window.innerWidth,
      height: window.innerHeight,
      dpr: window.devicePixelRatio || 1,
      uiScale: uiScaleFraction(),
    },
    // outer/inner 之差 = 标题栏 + 边框（Go 侧据此计算内容区在截图里的偏移）
    outer: {w: window.outerWidth, h: window.outerHeight},
    inner: {w: window.innerWidth, h: window.innerHeight},
    screen: {x: window.screenX, y: window.screenY},
  }
}

// ---- 控制台钩子 + revision ----

interface ConsoleEntry {
  ts: number
  level: string
  text: string[]
  source?: string
  stack?: string
}

const CONSOLE_CAPACITY = 200
const consoleRing: ConsoleEntry[] = []

function pushConsole(level: string, args: unknown[], source?: string, stack?: string): void {
  const text = args.slice(0, 20).map((a) => formatArg(a))
  consoleRing.push({ts: Date.now(), level, text, source, stack: stack ? clip(stack, 2000) : undefined})
  if (consoleRing.length > CONSOLE_CAPACITY) consoleRing.splice(0, consoleRing.length - CONSOLE_CAPACITY)
}

/** 把任意值转成一行可读文本（循环安全、体积有界）。 */
function formatArg(v: unknown, depth = 0): string {
  if (v === null) return 'null'
  if (v === undefined) return 'undefined'
  switch (typeof v) {
    case 'string':
      return clip(v, 500)
    case 'number':
    case 'boolean':
    case 'bigint':
      return String(v)
    case 'symbol':
      return v.toString()
    case 'function':
      return `[Function ${(v as {name?: string}).name || 'anonymous'}]`
  }
  if (v instanceof Error) return clip(`${v.name}: ${v.message}`, 500)
  if (typeof Element !== 'undefined' && v instanceof Element) {
    const d = describe(v)
    return `<${d.tag}${d.id ? '#' + d.id : ''}${d.classes.length ? '.' + d.classes.join('.') : ''}>`
  }
  if (depth > 2) return '[…]'
  if (Array.isArray(v)) {
    const head = v.slice(0, 20).map((x) => formatArg(x, depth + 1)).join(', ')
    return `[${head}${v.length > 20 ? `, …+${v.length - 20}` : ''}]`
  }
  try {
    const seen = new WeakSet<object>()
    const json = JSON.stringify(v, (_k, val) => {
      if (typeof val === 'function') return `[Function ${(val as {name?: string}).name || 'anonymous'}]`
      if (typeof val === 'bigint') return String(val)
      if (val && typeof val === 'object') {
        if (seen.has(val as object)) return '[Circular]'
        seen.add(val as object)
      }
      return val
    })
    return clip(json ?? String(v), 500)
  } catch {
    return '[Object]'
  }
}

/** 安装 console / window.onerror / unhandledrejection 采集（保留原有行为）。 */
function installConsoleHook(): void {
  const levels = ['log', 'info', 'warn', 'error', 'debug'] as const
  for (const level of levels) {
    const original = console[level].bind(console)
    console[level] = ((...args: unknown[]) => {
      try {
        pushConsole(level, args)
      } catch {
        /* 采集失败绝不能影响原有输出 */
      }
      original(...args)
    }) as typeof console.log
  }
  const prevOnError = window.onerror
  window.onerror = (message, source, lineno, colno, error) => {
    try {
      const text = error instanceof Error ? `${error.name}: ${error.message}` : String(message)
      pushConsole('error', [text], source ? `${source}:${lineno ?? 0}:${colno ?? 0}` : 'window.onerror', error instanceof Error ? error.stack : undefined)
    } catch {
      /* 同上 */
    }
    if (typeof prevOnError === 'function') {
      return prevOnError.call(window, message, source, lineno, colno, error)
    }
    return false
  }
  window.addEventListener('unhandledrejection', (ev) => {
    const reason = (ev as PromiseRejectionEvent).reason
    try {
      pushConsole('error', [reason instanceof Error ? reason : String(reason)], 'unhandledrejection', reason instanceof Error ? reason.stack : undefined)
    } catch {
      /* 同上 */
    }
  })
}

/** 取 Vue 应用实例（main.ts 的 app.mount('#app') 会把它挂到 #app 上）。 */
function vueApp(): {config?: {errorHandler?: unknown; globalProperties?: Record<string, unknown>}} | null {
  const el = document.querySelector('#app') as (HTMLElement & {__vue_app__?: unknown}) | null
  const app = el?.__vue_app__
  if (!app || typeof app !== 'object') return null
  return app as {config?: {errorHandler?: unknown; globalProperties?: Record<string, unknown>}}
}

/** 包裹 Vue 的 errorHandler（若应用已设置则先调用它，绝不覆盖）。 */
function installErrorHandler(): void {
  const app = vueApp()
  if (!app || !app.config) return
  const prev = app.config.errorHandler as ((err: unknown, instance: unknown, info: string) => void) | undefined
  app.config.errorHandler = (err: unknown, instance: unknown, info: string) => {
    try {
      pushConsole('error', [err instanceof Error ? err : String(err)], `vue:errorHandler:${info}`, err instanceof Error ? err.stack : undefined)
    } catch {
      /* 同上 */
    }
    if (typeof prev === 'function') prev(err, instance, info)
  }
}

// revision：同一次会话（sessionStorage 生命周期）内单调递增，刷新 / HMR 后 +1。
// AI 用它判断「我的 DOM 认知是否失效」；快照与 diff 也带上它判断可比性。
const REVISION_KEY = 'ding-ssh:debug:revision'
let revision = 1
{
  let prev = 0
  try {
    prev = Number(sessionStorage.getItem(REVISION_KEY) ?? '0')
  } catch {
    prev = 0
  }
  revision = Number.isFinite(prev) && prev > 0 ? prev + 1 : 1
  try {
    sessionStorage.setItem(REVISION_KEY, String(revision))
  } catch {
    /* 存储不可用时 revision 只在本次页面内单调，够用 */
  }
}
const loadedAt = Date.now()
const timeOrigin = typeof performance !== 'undefined' && performance.timeOrigin ? performance.timeOrigin : loadedAt

/** 安装 UI 观测域的前端钩子（main.ts 在挂载后调用一次）。 */
export function installDebugUI(): void {
  installConsoleHook()
  installErrorHandler()
}

// ---- 页面事实 ----

interface LayoutKey {
  name: string
  selector: string
  /** 浮层：它盖住别的元素属于预期（命令面板 / 右键菜单 / 快速连接）。 */
  overlay?: boolean
  /** 命中后改用其父元素（用于「设置页容器」这类没有独立根节点的容器）。 */
  containerOf?: boolean
}

// 关键选择器：与 App.vue / 各面板当前结构对应。
// UI 大改后选择器会失效——那时 ui.layout 会如实返回 found:false，这正是它的用途之一。
const LAYOUT_KEYS: LayoutKey[] = [
  {name: 'topbar', selector: '.topbar'},
  {name: 'nav', selector: 'aside.nav'},
  {name: 'sftp', selector: '.tool'},
  {name: 'terminal', selector: '.terminal-bg'},
  {name: 'statusbar', selector: 'footer.status-bar'},
  {name: 'commandPalette', selector: '.modal[aria-label="命令面板"]', overlay: true},
  {name: 'quickConnect', selector: '.qconn', overlay: true},
  {name: 'settingsPage', selector: '.set-nav', containerOf: true},
  {name: 'contextMenu', selector: '.menu-pop', overlay: true},
]

function viewSummary(): Record<string, unknown> {
  const ui = useUIStore()
  const sessions = useSessionsStore()
  return {
    view: ui.view,
    cmdOpen: ui.cmdOpen,
    terminalSidebarOpen: ui.terminalSidebarOpen,
    sftpVisible: sessions.sftpVisible,
    rightPanel: sessions.rightPanel,
    splitShown: sessions.splitShown,
    activeId: sessions.activeId,
    tabCount: sessions.tabs.length,
    revision,
  }
}

function layoutOp(): unknown {
  const elements = LAYOUT_KEYS.map((key) => {
    let el: Element | null = null
    try {
      el = document.querySelector(key.selector)
    } catch {
      el = null
    }
    if (el && key.containerOf) el = el.parentElement
    return {key, el}
  })
  const entries = elements.map(({key, el}) => {
    const cs = el ? getComputedStyle(el) : null
    return {
      name: key.name,
      selector: key.selector,
      found: !!el,
      visible: el ? isVisible(el) : false,
      inViewport: el ? inViewport(el) : false,
      rect: el ? rectOf(el) : null,
      zIndex: cs?.zIndex ?? null,
      overflow: cs ? `${cs.overflowX}/${cs.overflowY}` : null,
      pointerEvents: cs?.pointerEvents ?? null,
      overlay: !!key.overlay,
      // contains：我包含的其它关键元素名（Go 侧据此排除「祖先-后代」这种正常嵌套）
      contains: elements
        .filter((other) => other.el && el && other.el !== el && el.contains(other.el))
        .map((other) => other.key.name),
      classes: el ? classList(el) : [],
      tag: el ? el.tagName.toLowerCase() : '',
    }
  })
  return {
    ...windowInfo(),
    entries,
    view: viewSummary(),
    revision,
  }
}

function queryOp(args: Args): unknown {
  const selector = str(args.selector)
  if (!selector.trim()) throw new Error('缺少 selector（例如 .xterm / aside.nav）')
  let all: Element[]
  try {
    all = Array.from(document.querySelectorAll(selector))
  } catch (e) {
    throw new Error(`选择器非法：${selector}（${errText(e)}）`)
  }
  const limit = clamp(Math.round(num(args.limit, 20)), 1, 100)
  const extra = Array.isArray(args.props) ? args.props.map((p) => String(p)) : []
  const matches = all.slice(0, limit).map((el, i) => ({
    index: i,
    tag: el.tagName.toLowerCase(),
    id: el.id || '',
    classes: classList(el),
    text: clip(textOf(el), 200),
    rect: rectOf(el),
    visible: isVisible(el),
    inViewport: inViewport(el),
    childCount: el.children.length,
    attrs: attrsOf(el, extra),
  }))
  return {selector, total: all.length, matches}
}

const ATTR_ALLOW = new Set(['id', 'class', 'title', 'role', 'type', 'name', 'value', 'placeholder', 'href', 'aria-label', 'aria-expanded', 'aria-pressed', 'aria-current', 'aria-hidden', 'disabled', 'readonly', 'checked', 'data-statusbar-trigger', 'data-tone'])

function attrsOf(el: Element, extra: string[]): Record<string, string> {
  const out: Record<string, string> = {}
  let n = 0
  for (const attr of Array.from(el.attributes)) {
    const name = attr.name.toLowerCase()
    const keep = ATTR_ALLOW.has(name) || name.startsWith('data-') || name.startsWith('aria-') || extra.includes(attr.name) || extra.includes(name)
    if (!keep) continue
    out[name] = clip(attr.value, 120)
    if (++n >= 16) break
  }
  return out
}

const DEFAULT_STYLE_PROPS = [
  'display', 'position', 'z-index', 'color', 'background-color',
  'font-family', 'font-size', 'font-weight', 'line-height', 'letter-spacing',
  'padding', 'margin', 'width', 'height', 'min-width', 'max-width', 'min-height', 'max-height',
  'overflow', 'opacity', 'transform', 'transition', 'border-radius', 'box-shadow',
  'flex', 'gap', 'visibility', 'pointer-events', 'white-space', 'text-align',
]

function stylesOp(args: Args): unknown {
  const selector = str(args.selector)
  if (!selector.trim()) throw new Error('缺少 selector（例如 .topbar）')
  let all: Element[]
  try {
    all = Array.from(document.querySelectorAll(selector))
  } catch (e) {
    throw new Error(`选择器非法：${selector}（${errText(e)}）`)
  }
  const limit = clamp(Math.round(num(args.limit, 5)), 1, 20)
  const props = Array.isArray(args.props) && args.props.length ? args.props.map((p) => String(p)) : DEFAULT_STYLE_PROPS
  const items = all.slice(0, limit).map((el, i) => ({
    index: i,
    tag: el.tagName.toLowerCase(),
    id: el.id || '',
    classes: classList(el),
    rect: rectOf(el),
    visible: isVisible(el),
    styles: styleProps(el, props),
    cssVars: cssVarsOf(el),
  }))
  return {
    selector,
    total: all.length,
    props,
    items,
    note: 'cssVars 是该元素上 --* 自定义属性的当前值（计算样式与内联样式合并）；不同浏览器对「计算样式里是否枚举自定义属性」支持不同，缺失时请改用 ui.tokens 看 <html>。',
  }
}

function tokensOp(args: Args): unknown {
  const filter = str(args.filter).toLowerCase()
  const root = document.documentElement
  const pick = (m: Record<string, string>): Record<string, string> => {
    if (!filter) return m
    const out: Record<string, string> = {}
    for (const [k, v] of Object.entries(m)) if (k.toLowerCase().includes(filter)) out[k] = v
    return out
  }
  const inline = pick(cssVarsInline(root))
  const computed = pick(cssVarsOf(root))
  let theme: Record<string, unknown> = {note: 'settings store 不可用'}
  try {
    const s = useSettingsStore()
    const appearance = s.appearance
    const preset = presetById(appearance?.presetId ?? '') ?? defaultPreset()
    const tone = root.getAttribute('data-tone') ?? null
    theme = {
      mode: appearance?.mode ?? null,
      presetId: appearance?.presetId ?? null,
      presetName: preset.name,
      baseTone: appearance?.baseTone ?? null,
      resolvedTone: tone,
      primary: appearance?.primary ?? null,
      secondary: appearance?.secondary ?? null,
      uiText: appearance?.uiText ?? null,
      uiFont: s.fonts?.uiFont ?? null,
      terminalFont: s.fonts?.terminalFont ?? null,
      terminalFontSize: s.fonts?.terminalFontSize ?? null,
    }
  } catch (e) {
    theme = {error: errText(e)}
  }
  return {
    element: 'html',
    selector: ':root',
    tone: root.getAttribute('data-tone') ?? null,
    theme,
    inline,
    computed,
    count: Object.keys(computed).length,
    viewport: {dpr: window.devicePixelRatio || 1, uiScale: uiScaleFraction(), uiScalePercent: uiScaleFraction() * 100, width: window.innerWidth, height: window.innerHeight},
    note: '令牌真正落在 <html>（style.css 的 :root/[data-tone] 定义 + 主题引擎写入的内联变量）；inline 是内联（主题引擎动态注入）部分，computed 是合并后的有效值。',
  }
}

function cssVarsInline(el: HTMLElement): Record<string, string> {
  const out: Record<string, string> = {}
  for (let i = 0; i < el.style.length; i++) {
    const name = el.style.item(i)
    if (name && name.startsWith('--')) out[name] = el.style.getPropertyValue(name).trim()
  }
  return out
}

function hitOp(args: Args): unknown {
  if (args.x === undefined || args.y === undefined) throw new Error('缺少 x / y（视口 CSS 坐标）')
  const x = Math.round(num(args.x))
  const y = Math.round(num(args.y))
  const list: Element[] = typeof document.elementsFromPoint === 'function'
    ? document.elementsFromPoint(x, y)
    : ([document.elementFromPoint(x, y)].filter(Boolean) as Element[])
  const chain = list.map((el, i) => {
    const cs = getComputedStyle(el)
    return {
      index: i,
      tag: el.tagName.toLowerCase(),
      id: el.id || '',
      classes: classList(el),
      pointerEvents: cs.pointerEvents,
      zIndex: cs.zIndex,
      position: cs.position,
      rect: rectOf(el),
      text: clip(textOf(el), 60),
    }
  })
  return {x, y, chain}
}

// ---- 组件树 ----

interface VueInstance {
  uid?: number
  type?: unknown
  vnode?: {key?: unknown; el?: unknown}
  subTree?: unknown
  props?: Record<string, unknown>
}

function componentName(instance: VueInstance): string {
  const type = instance.type as {name?: string; __name?: string; __file?: string} | undefined
  if (!type) return '(anonymous)'
  return type.name || type.__name || (type.__file ? type.__file.split(/[\\/]/).pop() || '(anonymous)' : '(anonymous)')
}

function componentNameFromType(type: unknown): string {
  if (!type || typeof type !== 'object') return '(anonymous)'
  const t = type as {name?: string; __name?: string; __file?: string}
  return t.name || t.__name || (t.__file ? t.__file.split(/[\\/]/).pop() || '(anonymous)' : '(anonymous)')
}

function isComponentLike(v: unknown): boolean {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  return o.__isVue === true || typeof o.setup === 'function' || typeof o.render === 'function' || typeof o.__name === 'string' || typeof o.__file === 'string'
}

function isRefLike(v: unknown): boolean {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  return o.__v_isRef === true || o._isRef === true
}

/** 值摘要：函数 / 组件实例只报名字，容器只报规模 —— 绝不把响应式对象整个丢回去。 */
function summarizeValue(v: unknown, depth = 0): string {
  if (v === null) return 'null'
  if (v === undefined) return 'undefined'
  const t = typeof v
  if (t === 'function') return `[Function ${(v as {name?: string}).name || 'anonymous'}]`
  if (t === 'string') return clip(v as string, 60)
  if (t === 'number' || t === 'boolean' || t === 'bigint') return String(v)
  if (t === 'symbol') return (v as symbol).toString()
  if (isRefLike(v)) return depth > 1 ? '[Ref]' : `[Ref → ${summarizeValue((v as {value?: unknown}).value, depth + 1)}]`
  if (isComponentLike(v)) return `[Component ${componentNameFromType(v)}]`
  if (Array.isArray(v)) {
    if (depth > 1) return `[Array(${v.length})]`
    return `[Array(${v.length}) ${v.slice(0, 3).map((x) => summarizeValue(x, depth + 1)).join(', ')}]`
  }
  if (typeof Element !== 'undefined' && v instanceof Element) return `[Element <${describe(v).tag}>]`
  const keys = Object.keys(v as object).slice(0, 8)
  return `[Object keys: ${keys.join(', ')}${Object.keys(v as object).length > 8 ? ', …' : ''}]`
}

function summarizeProps(props: Record<string, unknown> | undefined): Record<string, string> {
  if (!props || typeof props !== 'object') return {}
  const out: Record<string, string> = {}
  const keys = Object.keys(props).slice(0, 12)
  for (const k of keys) out[k] = summarizeValue(props[k])
  if (Object.keys(props).length > keys.length) out['…'] = `还有 ${Object.keys(props).length - keys.length} 个 prop 未列出`
  return out
}

/** 直接子组件实例（沿 subTree 的 vnode 树找带 component 的节点）。 */
function childInstances(instance: VueInstance): VueInstance[] {
  const out: VueInstance[] = []
  const seen = new Set<unknown>()
  const push = (vnode: unknown, depth: number): void => {
    if (!vnode || typeof vnode !== 'object' || depth > 30 || seen.has(vnode)) return
    seen.add(vnode)
    const v = vnode as {component?: unknown; children?: unknown}
    if (v.component) {
      out.push(v.component as VueInstance)
      return
    }
    const kids = v.children
    if (Array.isArray(kids)) for (const k of kids) push(k, depth + 1)
  }
  push(instance.subTree, 0)
  return out
}

function domSummary(el: unknown): {tag: string; id: string; classes: string[]} {
  if (typeof Element !== 'undefined' && el instanceof Element) return describe(el)
  return {tag: '', id: '', classes: []}
}

function treeOp(args: Args): unknown {
  const app = vueApp() as {_instance?: VueInstance} | null
  const root = app?._instance
  if (!root) throw new Error('未找到 Vue 应用实例（#app.__vue_app__._instance 不可用）')
  const depthMax = clamp(Math.round(num(args.depth, 3)), 1, 6)
  const limit = clamp(Math.round(num(args.limit, 60)), 1, 200)
  const filter = str(args.filter).toLowerCase()
  const nodes: Record<string, unknown>[] = []
  const visited = new Set<VueInstance>()
  let truncated = false
  const walk = (instance: VueInstance, depth: number): void => {
    if (!instance || visited.has(instance)) return
    if (nodes.length >= limit) {
      truncated = true
      return
    }
    visited.add(instance)
    const name = componentName(instance)
    const children = childInstances(instance)
    if (!filter || name.toLowerCase().includes(filter)) {
      nodes.push({
        name,
        key: (instance.vnode?.key ?? null) as unknown,
        depth,
        dom: domSummary(instance.vnode?.el),
        propsSummary: summarizeProps(instance.props),
        childCount: children.length,
        uid: instance.uid ?? null,
      })
    }
    if (depth + 1 > depthMax) {
      if (children.length) truncated = true
      return
    }
    for (const child of children) walk(child, depth + 1)
  }
  walk(root, 0)
  return {
    nodes,
    total: nodes.length,
    truncated,
    depth: depthMax,
    limit,
    note: 'props 只做浅层摘要（函数 / 组件实例只报名字），并限制深度与数量；childCount 是直接子组件数。',
  }
}

// ---- 元素事实（ui.assert 用）----

function elementFactsOp(args: Args): unknown {
  const selector = str(args.selector)
  if (!selector.trim()) throw new Error('缺少 selector')
  let el: Element | null = null
  try {
    el = document.querySelector(selector)
  } catch (e) {
    throw new Error(`选择器非法：${selector}（${errText(e)}）`)
  }
  const count = (() => {
    try {
      return document.querySelectorAll(selector).length
    } catch {
      return 0
    }
  })()
  if (!el) return {selector, found: false, count: 0, visible: false, rect: null, text: '', styles: {}}
  const props = Array.isArray(args.props) ? args.props.map((p) => String(p)) : []
  return {
    selector,
    found: true,
    count,
    tag: el.tagName.toLowerCase(),
    id: el.id || '',
    classes: classList(el),
    visible: isVisible(el),
    inViewport: inViewport(el),
    rect: rectOf(el),
    text: clip(textOf(el), 500),
    zIndex: getComputedStyle(el).zIndex,
    styles: props.length ? styleProps(el, props) : {},
  }
}

// ---- 指纹（快照 / 差异用）----

const FINGERPRINT_MAX = 400
const FINGERPRINT_STYLE_PROPS = ['display', 'position', 'z-index', 'color', 'background-color', 'font-size', 'opacity', 'border-radius']

const SKIP_TAGS = new Set(['SCRIPT', 'STYLE', 'LINK', 'META', 'HEAD', 'TITLE', 'NOSCRIPT', 'TEMPLATE', 'BR'])

/** 结构路径：从 #app 起的分段（用 :nth-of-type，比 nth-child 稳定）。 */
function selectorPath(el: Element): string {
  const parts: string[] = []
  let cur: Element | null = el
  let guard = 0
  while (cur && cur.nodeType === 1 && guard++ < 14) {
    if (cur.id) {
      parts.unshift('#' + cur.id)
      break
    }
    const parent: Element | null = cur.parentElement
    let idx = 1
    let sib: Element | null = cur.previousElementSibling
    while (sib) {
      if (sib.tagName === cur.tagName) idx++
      sib = sib.previousElementSibling
    }
    parts.unshift(`${cur.tagName.toLowerCase()}:nth-of-type(${idx})`)
    if (cur === document.body || !parent) break
    cur = parent
  }
  return parts.join(' > ')
}

function fingerprintOp(args: Args): unknown {
  const selector = str(args.selector)
  let root: Element | null = null
  if (selector) {
    root = document.querySelector(selector)
    if (!root) throw new Error(`未找到选择器 ${selector}（无法采集指纹）`)
  } else {
    root = document.body
  }
  const candidates: Element[] = [root, ...Array.from(root.querySelectorAll('*'))]
  const fingerprint: Record<string, unknown>[] = []
  let truncated = false
  for (const el of candidates) {
    if (fingerprint.length >= FINGERPRINT_MAX) {
      truncated = true
      break
    }
    if (SKIP_TAGS.has(el.tagName)) continue
    if (el.getAttribute('aria-hidden') === 'true') continue
    if (!isVisible(el)) continue
    const cs = getComputedStyle(el)
    fingerprint.push({
      selectorPath: selectorPath(el),
      tag: el.tagName.toLowerCase(),
      rect: rectOf(el),
      classes: classList(el).slice(0, 4),
      text: clip(textOf(el), 60),
      zIndex: cs.zIndex,
      visible: true,
      styles: styleProps(el, FINGERPRINT_STYLE_PROPS),
    })
  }
  return {
    fingerprint,
    count: fingerprint.length,
    truncated,
    limit: FINGERPRINT_MAX,
    selector,
    revision,
    view: viewSummary(),
    ...windowInfo(),
  }
}

// ---- 几何（截图 / 快照用）----

function geometryOp(args: Args): unknown {
  const selector = str(args.selector)
  const clip = normalizeRect(args.clip)
  let el: Element | null = null
  if (selector) {
    try {
      el = document.querySelector(selector)
    } catch (e) {
      throw new Error(`选择器非法：${selector}（${errText(e)}）`)
    }
  }
  return {
    selector,
    found: !!el,
    visible: el ? isVisible(el) : false,
    rect: el ? rectOf(el) : null,
    clip,
    tag: el ? el.tagName.toLowerCase() : '',
    classes: el ? classList(el) : [],
    zIndex: el ? getComputedStyle(el).zIndex : null,
    ...windowInfo(),
  }
}

// ---- store ----

interface SafeResult {
  value: unknown
  bytes: number
  truncated: boolean
  note?: string
}

/** JSON 安全的深拷贝：循环安全、字符串 / 数组 / 深度都有限额。 */
function sanitize(value: unknown, depth: number, state: {truncated: boolean}, seen: WeakSet<object>): unknown {
  if (value === null || value === undefined) return value
  const t = typeof value
  if (t === 'string') {
    const s = value as string
    if (s.length > 2000) state.truncated = true
    return s.length > 2000 ? s.slice(0, 2000) + `…（已截断 ${s.length - 2000} 字符）` : s
  }
  if (t === 'number' || t === 'boolean' || t === 'bigint') return t === 'bigint' ? String(value) : value
  if (t === 'function') {
    state.truncated = true
    return `[Function ${(value as {name?: string}).name || 'anonymous'}]`
  }
  if (t === 'symbol') return (value as symbol).toString()
  if (depth >= 6) {
    state.truncated = true
    return Array.isArray(value) ? `[Array(${value.length})]` : '[Object: 超过深度上限]'
  }
  if (value instanceof Element) return `<${describe(value).tag}>`
  if (value instanceof Date) return value.toISOString()
  if (value instanceof Map) return `[Map(${value.size})]`
  if (value instanceof Set) return `[Set(${value.size})]`
  const obj = value as object
  if (seen.has(obj)) return '[Circular]'
  seen.add(obj)
  if (Array.isArray(value)) {
    if (value.length > 200) state.truncated = true
    return value.slice(0, 200).map((v) => sanitize(v, depth + 1, state, seen))
  }
  const out: Record<string, unknown> = {}
  const keys = Object.keys(obj as Record<string, unknown>)
  if (keys.length > 200) state.truncated = true
  for (const k of keys.slice(0, 200)) out[k] = sanitize((obj as Record<string, unknown>)[k], depth + 1, state, seen)
  return out
}

function jsonSafe(value: unknown, budget: number): SafeResult {
  const state = {truncated: false}
  const out = sanitize(value, 0, state, new WeakSet<object>())
  let text = ''
  try {
    text = JSON.stringify(out) ?? ''
  } catch {
    text = ''
  }
  // 字节数按 UTF-8 估算（中文占 3 字节）：MCP 的限额是字节而不是字符。
  const bytes = new TextEncoder().encode(text).length
  if (bytes > budget) {
    const keys = out && typeof out === 'object' && !Array.isArray(out) ? Object.keys(out as Record<string, unknown>).slice(0, 60) : []
    return {
      value: {topLevelKeys: keys},
      bytes,
      truncated: true,
      note: `state 序列化后约 ${bytes} 字节，超过 ${budget} 字节上限：只返回顶层键名，请用 path 精确取值（如 appearance.presetId）。`,
    }
  }
  return {value: out, bytes, truncated: state.truncated, note: state.truncated ? '超长字符串 / 超大数组 / 过深对象已截断（字符串 2000 字符、数组 200 项、深度 6）。' : undefined}
}

function getByPath(value: unknown, path: string): unknown {
  let cur: unknown = value
  for (const part of path.split('.').filter(Boolean)) {
    if (cur === null || cur === undefined || typeof cur !== 'object') return undefined
    cur = (cur as Record<string, unknown>)[part]
  }
  return cur
}

function storeOp(args: Args): unknown {
  const pinia = getActivePinia()
  if (!pinia) throw new Error('Pinia 未初始化：无法读取 store')
  const names = Array.from(pinia._s.keys()).sort()
  const name = str(args.name)
  if (!name) return {stores: names, count: names.length, note: '传 name 可读取某个 store 的 state；传 path 可只取子路径。'}
  const store = pinia._s.get(name)
  if (!store) throw new Error(`未找到 store: ${name}（可用：${names.join(' / ')}）`)
  const budget = clamp(Math.round(num(args.maxBytes, 65536)), 1024, 262144)
  const path = str(args.path)
  const raw: unknown = path ? getByPath(store.$state, path) : store.$state
  if (path && raw === undefined) throw new Error(`store ${name} 的 state 里没有路径 ${path}`)
  const safe = jsonSafe(raw, budget)
  return {
    name,
    path: path || null,
    keys: Object.keys((store.$state ?? {}) as object),
    state: safe.value,
    bytes: safe.bytes,
    truncated: safe.truncated,
    truncatedNote: safe.note,
    persisted: false,
    note: '只读快照：本工具不调用任何 save()，也不会有副作用。',
  }
}

// ---- 写类 ----

function setTokenOp(args: Args): unknown {
  const root = document.documentElement
  const tokens = (args.tokens && typeof args.tokens === 'object' && !Array.isArray(args.tokens) ? args.tokens : {}) as Record<string, unknown>
  const applied: Record<string, string> = {}
  const removed: string[] = []
  for (const [rawName, rawValue] of Object.entries(tokens)) {
    const name = rawName.trim()
    if (!name.startsWith('--')) throw new Error(`令牌名必须以 -- 开头：${rawName}`)
    const value = rawValue === null || rawValue === undefined ? '' : String(rawValue)
    if (value === '') {
      root.style.removeProperty(name)
      removed.push(name)
      continue
    }
    root.style.setProperty(name, value)
    applied[name] = root.style.getPropertyValue(name).trim()
  }
  const reset = Array.isArray(args.reset) ? args.reset.map((r) => String(r).trim()) : []
  for (const name of reset) {
    if (!name.startsWith('--')) throw new Error(`要移除的令牌名必须以 -- 开头：${name}`)
    root.style.removeProperty(name)
    removed.push(name)
    delete applied[name]
  }
  return {
    element: 'html',
    applied,
    removed,
    appliedCount: Object.keys(applied).length,
    removedCount: removed.length,
    inlineTokenCount: Object.keys(cssVarsInline(root)).length,
    persisted: false,
    note: '只改了 <html> 的内联样式（主题引擎写入的变量同样在这一层）：刷新即还原；传空值或 reset 可移除。',
  }
}

function injectCSSOp(args: Args): unknown {
  if (!('css' in args)) throw new Error('缺少 css（要注入的样式文本；删除请传 css:"" 且给 id）')
  const css = str(args.css)
  const id = str(args.id) || 'default'
  const nodes = Array.from(document.head.querySelectorAll('style[data-ding-ssh-debug]'))
  const styles = nodes as HTMLStyleElement[]
  let target = styles.find((s) => s.getAttribute('data-ding-ssh-debug-id') === id) ?? null
  if (!css.trim()) {
    if (target) {
      target.remove()
      return {removed: true, id, styleCount: styles.length - 1, persisted: false}
    }
    return {removed: false, id, styleCount: styles.length, note: '没有找到该 id 的调试样式段（无需移除）'}
  }
  if (!target) {
    target = document.createElement('style')
    target.setAttribute('data-ding-ssh-debug', '1')
    target.setAttribute('data-ding-ssh-debug-id', id)
    document.head.appendChild(target)
  }
  target.textContent = css
  return {
    injected: true,
    id,
    bytes: css.length,
    styleCount: document.head.querySelectorAll('style[data-ding-ssh-debug]').length,
    persisted: false,
    note: '样式段带 data-ding-ssh-debug 标记，刷新即消失；同 id 再次注入会替换内容。',
  }
}

function deepMerge(target: Record<string, unknown>, patch: Record<string, unknown>): void {
  for (const [k, v] of Object.entries(patch)) {
    const cur = target[k]
    if (v && typeof v === 'object' && !Array.isArray(v) && cur && typeof cur === 'object' && !Array.isArray(cur)) {
      deepMerge(cur as Record<string, unknown>, v as Record<string, unknown>)
      continue
    }
    target[k] = v
  }
}

async function storePatchOp(args: Args): Promise<unknown> {
  const pinia = getActivePinia()
  if (!pinia) throw new Error('Pinia 未初始化：无法修改 store')
  const name = str(args.name)
  const names = Array.from(pinia._s.keys()).sort()
  const store = name ? pinia._s.get(name) : undefined
  if (!store) throw new Error(`未找到 store: ${name || '(空)'}（可用：${names.join(' / ')}）`)
  const patch = (args.patch && typeof args.patch === 'object' && !Array.isArray(args.patch) ? args.patch : null) as Record<string, unknown> | null
  if (!patch) throw new Error('缺少 patch（要合并进 state 的字段对象）')
  const before: Record<string, string> = {}
  for (const k of Object.keys(patch)) before[k] = JSON.stringify((store.$state as Record<string, unknown>)[k] ?? null)
  if (bool(args.deep)) {
    store.$patch((state: Record<string, unknown>) => deepMerge(state, patch))
  } else {
    // 对象形式 = 浅合并；pinia 的类型要求精确的 state 形状，这里按「按名合并」的语义放宽。
    ;(store.$patch as unknown as (p: Record<string, unknown>) => void)(patch)
  }
  await nextTick()
  await nextFrame()
  const after: Record<string, unknown> = {}
  const changed: string[] = []
  for (const k of Object.keys(patch)) {
    const now = JSON.stringify((store.$state as Record<string, unknown>)[k] ?? null)
    after[k] = (store.$state as Record<string, unknown>)[k]
    if (now !== before[k]) changed.push(k)
  }
  const safe = jsonSafe(store.$state, 32768)
  return {
    name,
    deep: bool(args.deep),
    patchedKeys: Object.keys(patch),
    changedKeys: changed,
    unchangedKeys: Object.keys(patch).filter((k) => !changed.includes(k)),
    valuesAfter: after,
    state: safe.value,
    persisted: false,
    note: '只改了内存里的 store：本工具不调用任何 save()（也不会触发写库的 watcher）。',
  }
}

const SETTINGS_SECTIONS: Record<string, string> = {
  general: '通用',
  theme: '外观',
  credentials: '保存的凭证',
  security: '安全',
  debug: '调试模式',
  logs: '日志',
  audit: 'AI 记录',
  migrate: '导入导出',
  about: '关于',
}

async function navigateOp(args: Args): Promise<unknown> {
  const view = str(args.view)
  const ui = useUIStore()
  switch (view) {
    case 'workspace':
      ui.showWorkspace()
      break
    case 'servers':
      ui.showServers()
      break
    case 'tunnel':
      ui.showTunnel()
      break
    case 'settings':
      ui.showSettings()
      break
    default:
      throw new Error(`view 必须是 workspace | servers | tunnel | settings（收到 ${view || '(空)'}）`)
  }
  ui.closeCommandPalette()
  await nextTick()
  await nextFrame()

  let section: string | null = null
  const wanted = str(args.section)
  if (view === 'settings' && wanted) {
    if (!(wanted in SETTINGS_SECTIONS)) {
      throw new Error(`设置页没有分区 ${wanted}（可用：${Object.keys(SETTINGS_SECTIONS).join(' / ')}）`)
    }
    const buttons = Array.from(document.querySelectorAll('.set-nav button'))
    const label = SETTINGS_SECTIONS[wanted]
    let btn = buttons.find((b) => (b.textContent ?? '').trim() === label) ?? null
    if (!btn) {
      const idx = Object.keys(SETTINGS_SECTIONS).indexOf(wanted)
      btn = buttons[idx] ?? null
    }
    if (!btn) throw new Error(`设置页未渲染出分区按钮（选择器 .set-nav button 未命中）：当前 view=${ui.view}`)
    const r = btn.getBoundingClientRect()
    clickElement(btn, {x: Math.round(r.x + r.width / 2), y: Math.round(r.y + r.height / 2), button: 0, clickCount: 1}, true)
    section = wanted
    await nextTick()
    await nextFrame()
  }
  return {
    view: ui.view,
    section,
    sectionLabel: section ? SETTINGS_SECTIONS[section] : null,
    activeElement: describe(document.activeElement),
    persisted: false,
    note: '已切到目标视图并等待一帧，可直接截图 / 断言；导航状态不落库。',
  }
}

async function windowOp(args: Args): Promise<unknown> {
  const rt = (window as unknown as {runtime?: Record<string, unknown>}).runtime
  if (!rt) throw new Error('Wails runtime 不可用（window.runtime 缺失）：无法调整窗口')
  const wantRestore = bool(args.restore)
  const wantMax = bool(args.maximize)
  const wantMin = bool(args.minimize)
  const hasSize = args.width !== undefined || args.height !== undefined
  const hasPos = args.x !== undefined || args.y !== undefined
  if (wantRestore) {
    WindowUnmaximise()
    WindowUnminimise()
  }
  if (hasSize) {
    const cur = await WindowGetSize().catch(() => null)
    const w = args.width !== undefined ? Math.max(320, Math.round(num(args.width))) : (cur?.w ?? 1280)
    const h = args.height !== undefined ? Math.max(240, Math.round(num(args.height))) : (cur?.h ?? 800)
    WindowSetSize(w, h)
  }
  if (hasPos) WindowSetPosition(Math.round(num(args.x)), Math.round(num(args.y)))
  if (wantMax) WindowMaximise()
  if (wantMin) WindowMinimise()
  await sleep(150)
  await nextFrame()
  const [size, pos, maximised, minimised] = await Promise.all([
    WindowGetSize().catch(() => null),
    WindowGetPosition().catch(() => null),
    WindowIsMaximised().catch(() => null),
    WindowIsMinimised().catch(() => null),
  ])
  return {
    size,
    position: pos,
    maximised,
    minimised,
    outer: {w: window.outerWidth, h: window.outerHeight},
    inner: {w: window.innerWidth, h: window.innerHeight},
    note: '返回的是调整后的实际尺寸 / 位置；最小化状态下截图会失败，视觉验证前请 restore。',
  }
}

function reloadOp(): unknown {
  const rt = (window as unknown as {runtime?: {WindowReload?: () => void}}).runtime
  if (!rt || typeof rt.WindowReload !== 'function') throw new Error('Wails runtime 不可用（window.runtime.WindowReload 缺失）：无法重载页面')
  // 先回包再重载：留一点时间让 DebugReply 走出去（回包丢失也属正常，AI 不应重试）。
  setTimeout(() => {
    try {
      WindowReload()
    } catch {
      /* 重载失败时页面仍在，AI 会从后续 ui.revision 看出未变化 */
    }
  }, 400)
  return {
    scheduled: true,
    delayMs: 400,
    note: '已安排重载：整个前端会重新挂载、终端会重连，DOM 状态与快照指纹全部失效（ui.revision 会 +1）。',
  }
}

// ---- 合成交互 ----

interface ClickOpts {
  x: number
  y: number
  button: number
  clickCount: number
}

function mouseInit(o: ClickOpts): MouseEventInit {
  return {
    bubbles: true,
    cancelable: true,
    composed: true,
    view: window,
    clientX: o.x,
    clientY: o.y,
    button: o.button,
    buttons: 0,
    detail: o.clickCount,
  }
}

/** 派发完整点击序列：pointerdown/mousedown/pointerup/mouseup/click。 */
function clickElement(el: Element, o: ClickOpts, alreadyCheckedVisibility = false): string[] {
  if (!alreadyCheckedVisibility && !isVisible(el)) {
    throw new Error(`元素不可见：${describe(el).tag}${el.id ? '#' + el.id : ''}（display:none / 尺寸为 0 / 被隐藏），无法点击`)
  }
  const dispatched: string[] = []
  const hasPointer = typeof PointerEvent === 'function'
  // 按下时 buttons 表示「当前按住的键」，抬起后归零（与真实鼠标事件一致）
  const downInit: MouseEventInit = {...mouseInit(o), buttons: o.button === 0 ? 1 : 2}
  const upInit: MouseEventInit = {...mouseInit(o), buttons: 0}
  const seq: Array<[string, MouseEventInit, boolean]> = [
    ['pointerdown', downInit, true],
    ['mousedown', downInit, false],
    ['pointerup', upInit, true],
    ['mouseup', upInit, false],
    ['click', upInit, false],
  ]
  for (const [type, init, pointer] of seq) {
    let ev: Event
    if (pointer && hasPointer) {
      ev = new PointerEvent(type, {...init, pointerId: 1, pointerType: 'mouse', isPrimary: true, width: 1, height: 1, pressure: type === 'pointerdown' ? 0.5 : 0})
    } else {
      const name = pointer ? type.replace('pointer', 'mouse') : type
      ev = new MouseEvent(name, init)
    }
    el.dispatchEvent(ev)
    dispatched.push(ev.type)
  }
  return dispatched
}

function resolveClickTarget(args: Args): {el: Element; point: {x: number; y: number}} {
  const selector = str(args.selector)
  const hasXY = args.x !== undefined && args.y !== undefined
  if (selector) {
    let el: Element | null = null
    try {
      el = document.querySelector(selector)
    } catch (e) {
      throw new Error(`选择器非法：${selector}（${errText(e)}）`)
    }
    if (!el) throw new Error(`未找到选择器 ${selector}（元素不在 DOM 中）`)
    if (hasXY) return {el, point: {x: Math.round(num(args.x)), y: Math.round(num(args.y))}}
    const r = el.getBoundingClientRect()
    if (r.width <= 0 || r.height <= 0) throw new Error(`元素不可见（尺寸为 0）：${selector}`)
    return {el, point: {x: Math.round(r.x + r.width / 2), y: Math.round(r.y + r.height / 2)}}
  }
  if (!hasXY) throw new Error('需要 selector，或 x/y 坐标（视口 CSS px）')
  const x = Math.round(num(args.x))
  const y = Math.round(num(args.y))
  const el = document.elementFromPoint(x, y)
  if (!el) throw new Error(`坐标 (${x}, ${y}) 上没有元素（可能在窗口外）`)
  return {el, point: {x, y}}
}

function clickOp(args: Args): unknown {
  const {el, point} = resolveClickTarget(args)
  const button = clamp(Math.round(num(args.button, 0)), 0, 4)
  const clickCount = clamp(Math.round(num(args.clickCount, 1)), 1, 3)
  const dispatched = clickElement(el, {x: point.x, y: point.y, button, clickCount})
  return {
    dispatched,
    target: describe(el),
    point,
    clickCount,
    button,
    activeElement: describe(document.activeElement),
    persisted: false,
  }
}

/** 用原型上的原生 setter 写值，绕过 Vue v-model 的缓存（关键技巧）。 */
function setNativeValue(el: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement, value: string): void {
  const proto = el instanceof HTMLTextAreaElement
    ? HTMLTextAreaElement.prototype
    : el instanceof HTMLSelectElement
      ? HTMLSelectElement.prototype
      : HTMLInputElement.prototype
  const desc = Object.getOwnPropertyDescriptor(proto, 'value')
  if (desc?.set) desc.set.call(el, value)
  else (el as {value: string}).value = value
  // Vue 3 的 v-model 会缓存上一次的值（_value / _vModelValue），清掉以免 diff 认为没变。
  delete (el as unknown as Record<string, unknown>)._value
}

function typeOp(args: Args): unknown {
  if (!('text' in args)) throw new Error('缺少 text')
  const text = str(args.text)
  const selector = str(args.selector)
  const clear = bool(args.clear)
  const submit = bool(args.submit)
  let el: Element | null = null
  if (selector) {
    try {
      el = document.querySelector(selector)
    } catch (e) {
      throw new Error(`选择器非法：${selector}（${errText(e)}）`)
    }
    if (!el) throw new Error(`未找到选择器 ${selector}（元素不在 DOM 中）`)
  } else {
    el = document.activeElement
    if (!el || el === document.body) throw new Error('当前没有焦点元素：请给 selector，或先用 ui.click 聚焦输入框')
  }
  const dispatched: string[] = []
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    if (el.disabled) throw new Error('输入元素处于禁用状态（disabled），无法写入')
    if (el.readOnly) throw new Error('输入元素处于只读状态（readonly），无法写入')
    if (!isVisible(el)) throw new Error(`输入元素不可见（${describe(el).tag}），无法写入`)
    const next = clear ? text : el.value + text
    setNativeValue(el, next)
    el.dispatchEvent(new Event('input', {bubbles: true}))
    el.dispatchEvent(new Event('change', {bubbles: true}))
    dispatched.push('input', 'change')
    let submitResult: string | null = null
    if (submit) {
      submitResult = submitNearestForm(el)
      dispatched.push(submitResult === 'submitted' ? 'requestSubmit' : submitResult)
    }
    return {dispatched, target: describe(el), valueAfter: el.value, submit: submitResult, persisted: false}
  }
  if (el instanceof HTMLSelectElement) {
    const next = clear ? text : el.value
    setNativeValue(el, next)
    el.dispatchEvent(new Event('input', {bubbles: true}))
    el.dispatchEvent(new Event('change', {bubbles: true}))
    dispatched.push('input', 'change')
    return {dispatched, target: describe(el), valueAfter: el.value, persisted: false}
  }
  if (el instanceof HTMLElement && el.isContentEditable) {
    el.focus()
    if (clear) el.textContent = ''
    let viaCommand = false
    try {
      viaCommand = document.execCommand('insertText', false, text)
    } catch {
      viaCommand = false
    }
    if (!viaCommand) el.textContent = (el.textContent ?? '') + text
    el.dispatchEvent(new InputEvent('input', {bubbles: true, data: text, inputType: 'insertText'}))
    dispatched.push('input')
    return {dispatched, target: describe(el), valueAfter: el.textContent ?? '', viaCommand, persisted: false}
  }
  throw new Error(`元素不可输入（${describe(el).tag}）：ui.type 只支持 input / textarea / select / contenteditable`)
}

function submitNearestForm(el: Element): string {
  const form = el.closest('form')
  if (!form) return 'no-form'
  if (typeof form.requestSubmit === 'function') {
    form.requestSubmit()
    return 'submitted'
  }
  form.dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}))
  return 'submit-event'
}

function keyCodeOf(key: string): string {
  if (key.length === 1) {
    const c = key.toUpperCase()
    if (c >= 'A' && c <= 'Z') return 'Key' + c
    if (c >= '0' && c <= '9') return 'Digit' + c
    return ''
  }
  switch (key) {
    case ' ':
      return 'Space'
    case 'Escape':
    case 'Esc':
      return 'Escape'
    case 'Enter':
      return 'Enter'
    case 'Tab':
      return 'Tab'
    case 'Backspace':
      return 'Backspace'
    case 'Delete':
      return 'Delete'
    case 'ArrowUp':
    case 'ArrowDown':
    case 'ArrowLeft':
    case 'ArrowRight':
      return key
    default:
      return ''
  }
}

function keyOp(args: Args): unknown {
  const key = str(args.key)
  if (!key) throw new Error('缺少 key（如 Enter / Escape / ArrowDown / k）')
  const selector = str(args.selector)
  let el: Element | null = null
  if (selector) {
    try {
      el = document.querySelector(selector)
    } catch (e) {
      throw new Error(`选择器非法：${selector}（${errText(e)}）`)
    }
    if (!el) throw new Error(`未找到选择器 ${selector}（元素不在 DOM 中）`)
  } else {
    el = document.activeElement && document.activeElement !== document.body ? document.activeElement : document.body
  }
  const mods = {ctrlKey: bool(args.ctrl), shiftKey: bool(args.shift), altKey: bool(args.alt), metaKey: bool(args.meta)}
  const base: KeyboardEventInit = {bubbles: true, cancelable: true, composed: true, key, code: keyCodeOf(key), ...mods, view: window}
  const dispatched: string[] = []
  const down = new KeyboardEvent('keydown', base)
  el.dispatchEvent(down)
  dispatched.push('keydown')
  if (key.length === 1) {
    const press = new KeyboardEvent('keypress', base)
    el.dispatchEvent(press)
    dispatched.push('keypress')
  }
  const up = new KeyboardEvent('keyup', base)
  el.dispatchEvent(up)
  dispatched.push('keyup')
  const valueAfter = el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement ? el.value : undefined
  return {dispatched, target: describe(el), key, modifiers: mods, defaultPrevented: down.defaultPrevented, valueAfter, persisted: false}
}

function hoverOp(args: Args): unknown {
  const {el, point} = resolveClickTarget(args)
  const init: MouseEventInit = {
    bubbles: true,
    cancelable: true,
    composed: true,
    view: window,
    clientX: point.x,
    clientY: point.y,
    button: 0,
    buttons: 0,
    detail: 0,
  }
  const dispatched: string[] = []
  const hasPointer = typeof PointerEvent === 'function'
  const push = (ev: Event): void => {
    el.dispatchEvent(ev)
    dispatched.push(ev.type)
  }
  if (hasPointer) push(new PointerEvent('pointerover', {...init, pointerId: 1, pointerType: 'mouse', isPrimary: true}))
  push(new MouseEvent('mouseover', init))
  if (hasPointer) push(new PointerEvent('pointerenter', {...init, bubbles: false, pointerId: 1, pointerType: 'mouse', isPrimary: true}))
  push(new MouseEvent('mouseenter', {...init, bubbles: false}))
  if (hasPointer) push(new PointerEvent('pointermove', {...init, pointerId: 1, pointerType: 'mouse', isPrimary: true}))
  push(new MouseEvent('mousemove', init))
  return {dispatched, target: describe(el), point, topElement: describe(document.elementFromPoint(point.x, point.y)), persisted: false}
}

function scrollOp(args: Args): unknown {
  const selector = str(args.selector)
  const dx = num(args.dx, 0)
  const dy = num(args.dy, 0)
  const absX = args.x !== undefined
  const absY = args.y !== undefined
  if (selector) {
    let el: Element | null = null
    try {
      el = document.querySelector(selector)
    } catch (e) {
      throw new Error(`选择器非法：${selector}（${errText(e)}）`)
    }
    if (!el) throw new Error(`未找到选择器 ${selector}（元素不在 DOM 中）`)
    const target = el as HTMLElement
    if (absX || absY) {
      target.scrollTo({left: absX ? Math.round(num(args.x)) : target.scrollLeft, top: absY ? Math.round(num(args.y)) : target.scrollTop})
    } else {
      target.scrollBy({left: Math.round(dx), top: Math.round(dy)})
    }
    return {
      dispatched: ['scroll'],
      target: describe(el),
      scrollAfter: {scrollTop: Math.round(target.scrollTop), scrollLeft: Math.round(target.scrollLeft)},
      scrollable: target.scrollHeight > target.clientHeight || target.scrollWidth > target.clientWidth,
      scrollSize: {scrollHeight: target.scrollHeight, clientHeight: target.clientHeight},
      persisted: false,
    }
  }
  if (absX || absY) {
    window.scrollTo(absX ? Math.round(num(args.x)) : window.scrollX, absY ? Math.round(num(args.y)) : window.scrollY)
  } else {
    window.scrollBy(Math.round(dx), Math.round(dy))
  }
  return {
    dispatched: ['scroll'],
    target: describe(document.documentElement),
    scrollAfter: {scrollTop: Math.round(window.scrollY), scrollLeft: Math.round(window.scrollX)},
    persisted: false,
  }
}

// ---- 分发 ----

/** 处理一个 ui.* op（由 debug/bridge.ts 调用）。 */
export async function handleUIOp(op: string, args: Args): Promise<unknown> {
  switch (op) {
    // 观测
    case 'ui.layout':
      return layoutOp()
    case 'ui.query':
      return queryOp(args)
    case 'ui.styles':
      return stylesOp(args)
    case 'ui.tokens':
      return tokensOp(args)
    case 'ui.hit':
      return hitOp(args)
    case 'ui.tree':
      return treeOp(args)
    case 'ui.console':
      return {capacity: CONSOLE_CAPACITY, entries: consoleRing.slice()}
    case 'ui.revision':
      return {
        revision,
        loadedAt,
        timeOrigin,
        hotReloaded: revision > 1,
        href: location.href,
        view: viewSummary(),
        viewport: windowInfo().viewport,
      }
    // ---- 以下为 Go 侧内部 op（不是 MCP 工具）：只提供页面内的事实 ----
    case 'ui.view':
      return {...viewSummary(), revision, loadedAt, timeOrigin}
    case 'ui.geometry':
      return geometryOp(args)
    case 'ui.fingerprint':
      return fingerprintOp(args)
    case 'ui.elementFacts':
      return elementFactsOp(args)
    case 'ui.store':
      return storeOp(args)
    // 实验（写）
    case 'ui.setToken':
      return setTokenOp(args)
    case 'ui.injectCSS':
      return injectCSSOp(args)
    case 'ui.storePatch':
      return storePatchOp(args)
    case 'ui.navigate':
      return navigateOp(args)
    case 'ui.window':
      return windowOp(args)
    case 'ui.reload':
      return reloadOp()
    case 'ui.click':
      return clickOp(args)
    case 'ui.type':
      return typeOp(args)
    case 'ui.key':
      return keyOp(args)
    case 'ui.hover':
      return hoverOp(args)
    case 'ui.scroll':
      return scrollOp(args)
    default:
      throw new Error(`未知的 UI 操作: ${op}`)
  }
}
