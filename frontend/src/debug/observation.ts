import {nextTick, type App, type ComponentPublicInstance} from 'vue'
import {useUIStore} from '../stores/ui'
import {useSessionsStore} from '../stores/sessions'
import {frontendRuntimeStatus} from '../utils/runtime-status'
import {remoteFontStatus, remoteFontProgress} from '../theme/remote-fonts'
import {REMOTE_FONT_FAMILIES} from '../theme/font-catalog'
import {listDebugTerminals} from './registry'
import {APP_VERSION, APP_COMMIT, BUILD_TIME} from '../version'

type Args = Record<string, unknown>
type Rect = {x: number; y: number; w: number; h: number}
type ComponentRecord = {id: number; parent: number | null; instance: ComponentPublicInstance}
type SafeError = {id: string; ts: number; source: string; code: string; phase?: string; vueRuntimeCode?: number; component?: string; frames: Array<{file: string; line: number; column: number}>}
type Issue = {rule: string; severity: 'error' | 'warning' | 'info'; confidence: 'confirmed' | 'candidate'; nodeRef?: string; rect?: Rect; evidence: Record<string, unknown>}
type Observation = {id: string; createdAt: number; mutationSeq: number; layoutSeq: number; refs: Map<string, Element>; signatures: Map<string, string>}
type Sequences = {mutation: number; layout: number; frame: number; error: number}
type MaskedElement = Element & {style: CSSStyleDeclaration}
type CaptureMask = {opacity: {value: string; priority: string}; transition: {value: string; priority: string}; caret: {value: string; priority: string}}
type CaptureLock = {token: string; expiresAt: number; preparedAt: number; stylesheet: HTMLStyleElement; masked: Map<MaskedElement, CaptureMask>; observer: MutationObserver; watchdog: ReturnType<typeof setTimeout>; sensitiveRects: Rect[]; frames: number}
const pageMutationOptions: MutationObserverInit = {subtree: true, childList: true, attributes: true, characterData: true}

const string = (value: unknown, fallback = '') => typeof value === 'string' ? value : fallback
const number = (value: unknown, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback
const bounded = (value: unknown, fallback: number, low: number, high: number) => Math.max(low, Math.min(high, number(value, fallback)))
const textLimit = (value: string, max = 160) => value.replace(/\s+/g, ' ').trim().slice(0, max)
const epoch = () => globalThis.crypto?.randomUUID?.() ?? `page-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
const frame = () => new Promise<void>(resolve => {
  // Hidden/minimised WebViews can suspend rAF. A bounded fallback reports this
  // in stability instead of leaving an MCP request pending forever.
  let done = false
  const timer = setTimeout(() => { if (!done) { done = true; resolve() } }, 100)
  requestAnimationFrame(() => { if (!done) { done = true; clearTimeout(timer); state.sequences.frame++; resolve() } })
})
const pause = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms))

const initialState = {
  pageEpoch: epoch(), windowId: 'main', installed: false, ready: false, mountedAt: 0,
  sequences: {mutation: 0, layout: 0, frame: 0, error: 0} as Sequences,
  lastChangeAt: performance.now(), components: new Map<number, ComponentRecord>(),
  componentIds: new WeakMap<object, number>(), nextComponent: 0,
  errors: [] as SafeError[], droppedErrors: 0,
  elementIds: new WeakMap<Element, string>(), nextElement: 0, nextObservation: 0,
  observations: new Map<string, Observation>(),
  observer: undefined as MutationObserver | undefined,
  capture: undefined as CaptureLock | undefined,
}
// Vite can reload this module while component instances stay alive. Keep the
// registry and installed observers shared, but invalidate all observation refs.
const scope = globalThis as typeof globalThis & {__dingDebugObservationState?: typeof initialState}
const previousState = scope.__dingDebugObservationState
const state = previousState ?? initialState
if (previousState) { state.pageEpoch = epoch(); state.observations.clear(); state.nextObservation = 0; state.sequences.layout++ }
scope.__dingDebugObservationState = state

function changed(mutations = true) {
  if (mutations) state.sequences.mutation++
  state.sequences.layout++
  state.lastChangeAt = performance.now()
}

function flushMutations() {
  if (state.observer?.takeRecords().length) changed()
}

function withCaptureMutationsSuppressed<T>(callback: () => T): T {
  const observer = state.observer
  if (!observer) return callback()
  // Account for application changes queued before pausing. Capture-owned DOM
  // edits are synchronous, so observation resumes before the browser can run
  // application code or deliver another mutation callback.
  flushMutations()
  observer.disconnect()
  try { return callback() }
  finally { observer.observe(document.documentElement, pageMutationOptions) }
}

function drainCaptureMutations() {
  // Capture-owned edits are isolated while they are applied. Any page records
  // still pending here therefore belong to application changes and must advance
  // the normal sequence instead of being silently discarded.
  flushMutations()
  state.capture?.observer.takeRecords()
}

/** No arbitrary message, error object, prop or URL content enters diagnostics. */
export function recordDebugError(error: unknown, source: 'vue' | 'window' | 'promise' | 'startup', info?: string, instance?: ComponentPublicInstance | null) {
  const allowedNames = ['Error', 'TypeError', 'ReferenceError', 'SyntaxError', 'RangeError', 'URIError', 'EvalError', 'AggregateError', 'DOMException']
  const name = error instanceof Error && allowedNames.includes(error.name) ? error.name : 'Error'
  // Paths are limited to generated app assets / source modules, and queries,
  // URL credentials, exception messages and native filesystem paths are lost.
  const frames: SafeError['frames'] = []
  if (error instanceof Error && typeof error.stack === 'string') {
    for (const line of error.stack.split('\n').slice(1, 16)) {
      const match = line.match(/(?:\/src\/([A-Za-z0-9_./-]+\.(?:vue|ts|js))|\/assets\/([A-Za-z0-9_.-]+\.js))(?:\?[^\s:)]*)?:(\d+):(\d+)/)
      if (match && !match[1]?.split('/').includes('..')) frames.push({file: match[1] ? `src/${match[1]}` : `assets/${match[2]}`, line: Number(match[3]), column: Number(match[4])})
      if (frames.length === 5) break
    }
  }
  const component = instance ? componentName(instance) : undefined
  const phases = ['setup function', 'render function', 'watcher getter', 'watcher callback', 'watcher cleanup function', 'native event handler', 'component event handler', 'beforeMount hook', 'mounted hook', 'beforeUpdate hook', 'updated hook', 'beforeUnmount hook', 'unmounted hook', 'scheduler flush']
  const entry: SafeError = {id: `error-${++state.sequences.error}`, ts: Date.now(), source, code: `JS_${name.replace(/Error$/, '').toUpperCase() || 'ERROR'}`, frames}
  if (info && phases.includes(info)) entry.phase = info
  const productionCode = info?.match(/^https:\/\/vuejs\.org\/error-reference\/#runtime-(\d{1,3})$/)
  if (productionCode) entry.vueRuntimeCode = Number(productionCode[1])
  if (component) entry.component = component
  state.errors.push(entry)
  if (state.errors.length > 100) { state.errors.shift(); state.droppedErrors++ }
}

function componentName(instance: ComponentPublicInstance): string {
  const options = instance.$options as {name?: string; __name?: string}
  const name = options.name || options.__name || 'Anonymous'
  return /^[A-Za-z][A-Za-z0-9_-]{0,100}$/.test(name) ? name : 'Anonymous'
}

/** Register public component instances before mounting; production needs no Vue private root. */
export function installObservation(app: App) {
  const installed = app as App & {__dingObservationInstalled?: boolean}
  if (installed.__dingObservationInstalled) return
  installed.__dingObservationInstalled = true
  app.mixin({
    beforeCreate(this: ComponentPublicInstance) {
      const id = ++state.nextComponent
      state.componentIds.set(this, id)
      state.components.set(id, {id, parent: this.$parent ? state.componentIds.get(this.$parent) ?? null : null, instance: this})
    },
    unmounted(this: ComponentPublicInstance) {
      const id = state.componentIds.get(this)
      if (id) state.components.delete(id)
    },
  })
  if (state.installed) return
  state.installed = true
  const mode = new URLSearchParams(location.search).get('mode')
  state.windowId = mode === 'detached-terminal' || mode === 'detached-sftp' ? `${mode}:${state.pageEpoch}` : 'main'
  window.addEventListener('error', event => recordDebugError(event.error, 'window'))
  window.addEventListener('unhandledrejection', event => recordDebugError(event.reason, 'promise'))
  state.observer = new MutationObserver(() => changed())
  state.observer.observe(document.documentElement, pageMutationOptions)
  if (typeof ResizeObserver === 'function') {
    const observer = new ResizeObserver(() => changed(false))
    observer.observe(document.documentElement)
  }
  window.addEventListener('resize', () => changed(false))
  document.fonts?.addEventListener('loadingdone', () => changed(false))
  document.fonts?.addEventListener('loadingerror', () => changed(false))
}

export function markDebugReady() { state.ready = true; state.mountedAt = Date.now(); changed() }

/** Cheap liveness payload: never walk the DOM, stores, components or terminals. */
export function debugRevision() {
  flushMutations()
  return {windowId: state.windowId, pageEpoch: state.pageEpoch, ready: state.ready, sequences: {...state.sequences}, at: Date.now(), visibility: document.visibilityState}
}

export function debugComponentTree(args: Args) {
  const limit = bounded(args.limit, 60, 1, 200)
  const maxDepth = bounded(args.depth, 3, 1, 6)
  const filter = string(args.filter).toLowerCase()
  const records = [...state.components.values()]
  const nodes: Record<string, unknown>[] = []
  let truncated = false
  for (const record of records) {
    let depth = 0, parent = record.parent
    const visited = new Set<number>()
    while (parent && !visited.has(parent)) { visited.add(parent); depth++; parent = state.components.get(parent)?.parent ?? null }
    const name = componentName(record.instance)
    if (depth > maxDepth) { truncated = true; continue }
    if (filter && !name.toLowerCase().includes(filter)) continue
    if (nodes.length >= limit) { truncated = true; break }
    const el = record.instance.$el
    const props = record.instance.$props as Record<string, unknown>
    const propsSummary = Object.fromEntries(Object.keys(props).slice(0, 12).map(key => {
      const value = props[key]
      return [key, typeof value === 'string' ? `[String(${value.length})]` : Array.isArray(value) ? `[Array(${value.length})]` : value === null ? 'null' : value && typeof value === 'object' ? `[Object(${Object.keys(value).length} keys)]` : `[${typeof value}]`]
    }))
    nodes.push({uid: record.id, parentUid: record.parent, name, depth, dom: el instanceof Element ? {tag: el.tagName.toLowerCase(), id: el.id, classes: Array.from(el.classList).slice(0, 12)} : null, propsSummary, childCount: records.filter(other => other.parent === record.id).length})
  }
  return {nodes, total: nodes.length, truncated, depth: maxDepth, limit, source: 'public-component-registry', ready: state.ready}
}

const sensitive = /password|passphrase|private.?key|key.?content|(?:^|[-_])(?:token|secret)(?:$|[-_])/i
const nonContentTags = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'HEAD', 'META', 'LINK'])
function privateContent(el: Element): boolean {
  for (let cur: Element | null = el; cur; cur = cur.parentElement) {
    if (nonContentTags.has(cur.tagName) || cur.matches('[data-sensitive],[data-secret],input[type="password"],input[type="hidden"]')) return true
    if (['INPUT', 'TEXTAREA', 'SELECT'].includes(cur.tagName) && (sensitive.test(`${cur.id} ${cur.getAttribute('name') || ''}`) || (cur.getAttribute('autocomplete') || '').includes('password'))) return true
  }
  return false
}

/** Pixel masks are independent of semantic-node limits and text inspection. */
function sensitiveRegions() {
  const elements = Array.from(document.querySelectorAll('[data-sensitive],[data-secret],[data-sensitive] *,[data-secret] *,input,textarea,select')).filter(el => privateContent(el) && visible(el))
  const roots = elements.filter(el => !elements.some(other => other !== el && other.contains(el)))
  return roots.map(el => rectOf(el))
}

const captureSelector = 'input,textarea,select,[contenteditable="true"],[data-sensitive],[data-secret],[data-sensitive] *,[data-secret] *'
const captureMaskProperties = [
  {property: 'transition-property', key: 'transition', value: 'none'},
  {property: 'opacity', key: 'opacity', value: '0'},
  {property: 'caret-color', key: 'caret', value: 'transparent'},
] as const

function maskedControls(lock: CaptureLock) {
  return [...lock.masked.keys()].filter(el => el.isConnected).map(el => {
    const item: Record<string, unknown> = {tag: el.tagName.toLowerCase(), rect: rectOf(el)}
    if (el instanceof HTMLInputElement) item.type = el.type || 'text'
    if (el.getAttribute('contenteditable') === 'true') item.contenteditable = true
    return item
  })
}

function captureLockStatus() {
  const lock = state.capture
  return lock ? {active: true, token: lock.token, preparedAt: lock.preparedAt, expiresAt: lock.expiresAt, maskedCount: lock.masked.size, maskedControls: maskedControls(lock), frames: lock.frames, maskMode: 'renderer-opacity'} : {active: false}
}

function maskCaptureElements(lock: CaptureLock) {
  // Descendants are also masked because display:contents can remove a marked
  // element's compositing box. Existing inline !important styles are overridden
  // directly, while the stylesheet protects newly inserted controls immediately.
  for (const candidate of Array.from(document.querySelectorAll(captureSelector))) {
    if (!(candidate instanceof HTMLElement || candidate instanceof SVGElement)) continue
    const el = candidate as MaskedElement
    let saved = lock.masked.get(el)
    if (!saved) {
      saved = {
        opacity: {value: el.style.getPropertyValue('opacity'), priority: el.style.getPropertyPriority('opacity')},
        transition: {value: el.style.getPropertyValue('transition-property'), priority: el.style.getPropertyPriority('transition-property')},
        caret: {value: el.style.getPropertyValue('caret-color'), priority: el.style.getPropertyPriority('caret-color')},
      }
      lock.masked.set(el, saved)
    }
    for (const {property, key, value} of captureMaskProperties) {
      const current = el.style.getPropertyValue(property), priority = el.style.getPropertyPriority(property)
      if (current === value && priority === 'important') continue
      // Application updates made during capture become the value restored at
      // release. Other style properties are never rewritten or snapshotted.
      saved[key] = {value: current, priority}
      el.style.setProperty(property, value, 'important')
    }
  }
}

function releaseCapture(token: string, expired = false) {
  const lock = state.capture
  if (!lock) return {released: false, reason: 'NO_ACTIVE_CAPTURE', captureLock: captureLockStatus(), ...debugRevision()}
  if (token !== lock.token) return {released: false, reason: 'CAPTURE_TOKEN_MISMATCH', captureLock: captureLockStatus(), ...debugRevision()}
  withCaptureMutationsSuppressed(() => {
    maskCaptureElements(lock)
    lock.observer.disconnect()
    clearTimeout(lock.watchdog)
    // Restore the most recent application value for each property. Removing the
    // masking stylesheet last prevents intermediate visible/transition frames.
    for (const [el, saved] of lock.masked) {
      for (const {property, key, value} of captureMaskProperties) {
        if (el.style.getPropertyValue(property) !== value || el.style.getPropertyPriority(property) !== 'important') continue
        if (saved[key].value) el.style.setProperty(property, saved[key].value, saved[key].priority)
        else el.style.removeProperty(property)
      }
    }
    lock.stylesheet.remove()
    lock.observer.takeRecords()
  })
  drainCaptureMutations()
  state.capture = undefined
  return {released: true, expired, captureToken: token, captureLock: captureLockStatus(), ...debugRevision()}
}

async function prepareCapture() {
  if (state.capture) return {accepted: false, error: {code: 'CAPTURE_BUSY'}, captureLock: captureLockStatus(), ...debugRevision()}
  if (document.visibilityState !== 'visible') return {accepted: false, error: {code: 'CAPTURE_DOCUMENT_NOT_VISIBLE'}, captureLock: captureLockStatus(), ...debugRevision()}
  const token = `capture-${epoch()}`, preparedAt = Date.now()
  const stylesheet = document.createElement('style')
  stylesheet.setAttribute('data-ding-capture-mask', token)
  stylesheet.textContent = `${captureSelector}{opacity:0!important;transition-property:none!important;caret-color:transparent!important}`
  const lock: CaptureLock = {token, preparedAt, expiresAt: preparedAt + 15000, stylesheet, masked: new Map(), observer: new MutationObserver(() => {
    if (state.capture === lock) {
      withCaptureMutationsSuppressed(() => maskCaptureElements(lock))
      lock.observer.takeRecords()
    }
  }), watchdog: setTimeout(() => { releaseCapture(token, true) }, 15000), sensitiveRects: sensitiveRegions(), frames: 0}
  state.capture = lock
  try {
    withCaptureMutationsSuppressed(() => {
      document.head.append(stylesheet)
      maskCaptureElements(lock)
    })
    lock.observer.observe(document.documentElement, {childList: true, attributes: true, subtree: true, attributeFilter: ['style','class','type','contenteditable','data-sensitive','data-secret']})
    await nextTick()
    // A frame callback precedes compositing; two callbacks ensure one complete
    // render cycle passed with the mask installed. Timeout fallbacks cannot
    // certify this, so fail closed if the WebView stopped rendering.
    const first = state.sequences.frame
    await frame(); await frame()
    lock.frames = state.sequences.frame - first
    if (state.capture !== lock || lock.frames < 2 || document.visibilityState !== 'visible') {
      releaseCapture(token)
      return {accepted: false, error: {code: 'CAPTURE_MASK_NOT_PAINTED'}, captureLock: captureLockStatus(), ...debugRevision()}
    }
    // Read computed styles to flush layout. Any application inline edits from
    // this frame are re-masked before the host is allowed to capture pixels.
    withCaptureMutationsSuppressed(() => maskCaptureElements(lock))
    lock.observer.takeRecords()
    const unsafe = [...lock.masked.keys()].some(el => el.isConnected && getComputedStyle(el).opacity !== '0')
    if (unsafe) {
      releaseCapture(token)
      return {accepted: false, error: {code: 'CAPTURE_MASK_FAILED'}, captureLock: captureLockStatus(), ...debugRevision()}
    }
    lock.observer.takeRecords()
    drainCaptureMutations()
    const observation = await observe({timeoutMs: 0, stableForMs: 0}, {settled: true, elapsedMs: Date.now() - preparedAt, stableForMs: 0, pending: [], checkedFrames: lock.frames, scope: 'capture-mask', paintConfirmed: false})
    return {accepted: true, captureToken: token, preparedAt, expiresAt: lock.expiresAt, captureLock: captureLockStatus(), sensitiveRects: lock.sensitiveRects, viewport: viewport(), observation, ...debugRevision()}
  } catch {
    releaseCapture(token)
    return {accepted: false, error: {code: 'CAPTURE_PREPARE_FAILED'}, captureLock: captureLockStatus(), ...debugRevision()}
  }
}

function sensitiveLayoutActivity() {
  const elements = Array.from(document.querySelectorAll(captureSelector))
  const animations = new Set<Animation>()
  for (const el of elements) {
    for (let ancestor: Element | null = el; ancestor; ancestor = ancestor.parentElement) {
      try { for (const animation of ancestor.getAnimations?.() ?? []) if (animation.playState === 'running' || animation.pending) animations.add(animation) } catch { /* unsupported animation inspection */ }
    }
  }
  return {candidateCount: elements.length, runningAnimationCount: animations.size, geometrySampledAt: Date.now(), motionTracking: 'sampled-geometry-and-web-animations', captureProtection: 'renderer-lock-required'}
}

function buildInfo() {
  const mode = new URLSearchParams(location.search).get('mode')
  return {version: APP_VERSION, commit: APP_COMMIT, buildTime: BUILD_TIME, dev: import.meta.env.DEV,
    mode: mode === 'detached-terminal' || mode === 'detached-sftp' ? mode : 'main'}
}
function visible(el: Element): boolean {
  if (!el.isConnected) return false
  for (let cur: Element | null = el; cur; cur = cur.parentElement) {
    const css = getComputedStyle(cur)
    if (cur.hasAttribute('hidden') || css.display === 'none' || ['hidden', 'collapse'].includes(css.visibility) || css.opacity === '0') return false
  }
  const rect = el.getBoundingClientRect()
  return rect.width > 0 && rect.height > 0 && el.getClientRects().length > 0
}
function renderedText(el: Element): string {
  if (privateContent(el) || !visible(el) || el.getAttribute('aria-hidden') === 'true') return ''
  const pieces: string[] = []
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, {acceptNode(node) {
    const parent = node.parentElement
    if (!parent || privateContent(parent)) return NodeFilter.FILTER_REJECT
    for (let cur: Element | null = parent; cur; cur = cur.parentElement) {
      const css = getComputedStyle(cur)
      if (cur.hasAttribute('hidden') || cur.getAttribute('aria-hidden') === 'true' || css.display === 'none' || css.visibility === 'hidden' || css.opacity === '0') return NodeFilter.FILTER_REJECT
    }
    return NodeFilter.FILTER_ACCEPT
  }})
  let node: Node | null
  while ((node = walker.nextNode())) {
    pieces.push(node.textContent || '')
    if (pieces.join('').length > 800) break
  }
  return textLimit(pieces.join(' '))
}
function roleOf(el: Element) {
  if (el.getAttribute('role')) return textLimit(el.getAttribute('role')!, 40)
  if (el.tagName === 'BUTTON') return 'button'
  if (el.tagName === 'A') return 'link'
  if (el instanceof HTMLSelectElement) return 'combobox'
  if (el instanceof HTMLInputElement) return el.type === 'checkbox' ? 'checkbox' : el.type === 'radio' ? 'radio' : el.type === 'range' ? 'slider' : 'textbox'
  if (el instanceof HTMLTextAreaElement || (el as HTMLElement).isContentEditable) return 'textbox'
  return 'region'
}
function nameOf(el: Element) {
  if (privateContent(el)) return ''
  const labelled = (el.getAttribute('aria-labelledby') || '').split(/\s+/).filter(Boolean).map(id => document.getElementById(id)).filter((label): label is HTMLElement => !!label).map(renderedText).join(' ')
  return textLimit(labelled || el.getAttribute('aria-label') || renderedText(el) || el.getAttribute('title') || '')
}
function rectOf(el: Element): Rect {
  const rect = el.getBoundingClientRect()
  return {x: Math.round(rect.x * 100) / 100, y: Math.round(rect.y * 100) / 100, w: Math.round(rect.width * 100) / 100, h: Math.round(rect.height * 100) / 100}
}
function refOf(el: Element): string {
  let ref = state.elementIds.get(el)
  if (!ref) { ref = `n${++state.nextElement}`; state.elementIds.set(el, ref) }
  return ref
}
function inert(el: Element) { return !!el.closest('[inert],[aria-hidden="true"]') }
function disabled(el: Element) { return el.matches(':disabled,[aria-disabled="true"]') || !!el.closest('[aria-disabled="true"]') }
function intersect(a: Rect, b: Rect): Rect | null {
  const x = Math.max(a.x, b.x), y = Math.max(a.y, b.y)
  const w = Math.min(a.x + a.w, b.x + b.w) - x, h = Math.min(a.y + a.h, b.y + b.h) - y
  return w > 0 && h > 0 ? {x, y, w, h} : null
}
function clippedRect(el: Element) {
  let rect: Rect | null = intersect(rectOf(el), {x: 0, y: 0, w: innerWidth, h: innerHeight})
  const chain: Array<{ref: string; rect: Rect; overflowX: string; overflowY: string}> = []
  for (let parent = el.parentElement; parent && rect; parent = parent.parentElement) {
    const css = getComputedStyle(parent)
    if (!/(hidden|clip|auto|scroll)/.test(`${css.overflowX} ${css.overflowY}`)) continue
    const parentRect = rectOf(parent)
    chain.push({ref: refOf(parent), rect: parentRect, overflowX: css.overflowX, overflowY: css.overflowY})
    const clip = {x: /(hidden|clip|auto|scroll)/.test(css.overflowX) ? parentRect.x : rect.x, y: /(hidden|clip|auto|scroll)/.test(css.overflowY) ? parentRect.y : rect.y, w: /(hidden|clip|auto|scroll)/.test(css.overflowX) ? parentRect.w : rect.w, h: /(hidden|clip|auto|scroll)/.test(css.overflowY) ? parentRect.h : rect.h}
    rect = intersect(rect, clip)
  }
  return {rect, chain}
}
function hitTest(el: Element) {
  const clipped = clippedRect(el)
  if (!clipped.rect) return {supported: typeof document.elementFromPoint === 'function', reachable: false, point: null, topRef: null}
  const rect = clipped.rect
  const point = {x: rect.x + rect.w / 2, y: rect.y + rect.h / 2}
  if (typeof document.elementFromPoint !== 'function') return {supported: false, reachable: false, point, topRef: null}
  const top = document.elementFromPoint(point.x, point.y)
  return {supported: true, reachable: !!top && (top === el || el.contains(top)), point, topRef: top ? refOf(top) : null}
}
function signature(el: Element) { return JSON.stringify([roleOf(el), nameOf(el), rectOf(el), disabled(el), inert(el)]) }
function nodeFacts(el: Element) {
  const hit = hitTest(el)
  const shown = visible(el)
  const isDisabled = disabled(el), isInert = inert(el)
  return {
    ref: refOf(el), tag: el.tagName.toLowerCase(), role: roleOf(el), name: nameOf(el),
    automationId: privateContent(el) ? '' : textLimit(el.getAttribute('data-automation-id') || el.getAttribute('data-testid') || el.getAttribute('data-ui') || '', 100),
    rect: rectOf(el), visible: shown, inViewport: !!clippedRect(el).rect,
    disabled: isDisabled, inert: isInert, focused: document.activeElement === el,
    actionable: shown && !isDisabled && !isInert && hit.reachable,
    hit, sensitive: privateContent(el),
  }
}
const nodeSelector = '[data-ui],[data-automation-id],[data-testid],button,a[href],input:not([type="hidden"]),textarea,select,img,[contenteditable="true"],[role],[tabindex],[aria-busy]'
function candidates(scope?: Element) { return [...(scope ? [scope] : []), ...Array.from((scope ?? document.body).querySelectorAll(nodeSelector))].filter((el, index, all) => all.indexOf(el) === index && !nonContentTags.has(el.tagName)) }

function view() {
  try {
    const ui = useUIStore(), sessions = useSessionsStore()
    return {name: ui.view, settingsSection: ui.settingsSection, activeId: sessions.activeId, tabCount: sessions.tabs.length, splitShown: sessions.splitShown, rightPanel: sessions.rightPanel, rightPanelVisible: sessions.sftpVisible,
      dialogs: Array.from(document.querySelectorAll('[role="dialog"],[role="alertdialog"]')).filter(visible).map(el => ({ref: refOf(el), name: nameOf(el), modal: el.getAttribute('aria-modal') === 'true'}))}
  } catch { return {name: 'unknown', dialogs: []} }
}
function viewport() {
  const root = document.querySelector('[data-ui="app"]')
  const zoom = root ? Number.parseFloat(getComputedStyle(root).zoom || '1') : 1
  return {width: innerWidth, height: innerHeight, dpr: devicePixelRatio || 1, uiScale: Number.isFinite(zoom) && zoom > 0 ? zoom : 1}
}
function fontProbe() {
  const faces: Array<{family: string; weight: string; status: string}> = []
  // Font family names come from CSS, but only shipped/known font names are
  // returned. User font aliases and URLs are not diagnostic text channels.
  const allowed = ['Geist Sans', 'Geist Mono', 'Noto Sans SC', ...REMOTE_FONT_FAMILIES]
  if (document.fonts && typeof document.fonts.forEach === 'function') document.fonts.forEach(face => {
    const family = face.family.replace(/["']/g, '')
    if (allowed.includes(family as never) && faces.length < 60) faces.push({family, weight: /^\d+(?: \d+)?$/.test(face.weight) ? face.weight : 'normal', status: face.status})
  })
  const remote = Object.entries(remoteFontStatus).filter(([key]) => REMOTE_FONT_FAMILIES.some(family => key.startsWith(`${family}:`))).map(([key, status]) => ({key, status, progress: remoteFontProgress[key] ? {...remoteFontProgress[key]} : null}))
  return {supported: !!document.fonts, status: document.fonts?.status ?? 'unsupported', faces, remote, failedCount: faces.filter(face => face.status === 'error').length + remote.filter(face => face.status === 'error').length,
    coverage: 'registered-faces-only', actualGlyphFont: 'unavailable'}
}
function terminalProbe() {
  return listDebugTerminals().slice(0, 50).map(entry => {
    const el = entry.container(), term = entry.term, screen = el?.querySelector('.xterm-screen')
    const runtime = entry.runtimeStatus?.()
    const canvases = Array.from(el?.querySelectorAll('canvas') ?? []).slice(0, 4).map(canvas => ({bitmap: {w: canvas.width, h: canvas.height}, css: {w: canvas.clientWidth, h: canvas.clientHeight}, rect: rectOf(canvas)}))
    return {clientId: entry.clientId, renderer: runtime ? runtime.webgl ? 'webgl' : 'dom' : 'unknown', rows: term.rows, cols: term.cols, pendingOutputBytes: runtime?.pendingOutputBytes ?? null, peakOutputBytes: runtime?.peakOutputBytes ?? null,
      container: el ? rectOf(el) : null, screen: screen ? rectOf(screen) : null, visible: !!el && visible(el), canvases,
      buffer: {type: term.buffer.active.type, length: term.buffer.active.length, baseY: term.buffer.active.baseY, viewportY: term.buffer.active.viewportY},
      font: {size: term.options.fontSize, lineHeight: term.options.lineHeight},
      watermarks: runtime ? {outputSequence: runtime.outputSequence ?? null, parsedSequence: runtime.parsedSequence ?? null, renderSequence: runtime.renderSequence ?? null, lastOutputAt: runtime.lastOutputAt ?? null, lastParsedAt: runtime.lastParsedAt ?? null, lastRenderAt: runtime.lastRenderAt ?? null, lastFitAt: runtime.lastFitAt ?? null} : null,
      webglGeometry: runtime?.webglGeometry ?? null, ptySizeConfirmed: 'unknown', paintConfirmed: 'unknown'}
  })
}
function runtimeProbe() {
  let runtime: unknown = null
  try { runtime = frontendRuntimeStatus() } catch { /* a partially created terminal must not break diagnosis */ }
  return {ready: state.ready, mountedAt: state.mountedAt || null, visibility: document.visibilityState, runtime, fonts: fontProbe(), terminals: terminalProbe()}
}
function diagnoseElements(elements: Element[]) {
  const issues: Issue[] = []
  const add = (el: Element, rule: string, severity: Issue['severity'], evidence: Issue['evidence'], confidence: Issue['confidence'] = 'confirmed') => {
    if (issues.length < 100) issues.push({rule, severity, confidence, nodeRef: refOf(el), rect: rectOf(el), evidence})
  }
  const modal = Array.from(document.querySelectorAll('[role="dialog"][aria-modal="true"],[role="alertdialog"][aria-modal="true"]')).filter(visible).at(-1)
  for (const el of elements) {
    if (!visible(el)) continue
    const rect = rectOf(el), css = getComputedStyle(el), clipped = clippedRect(el)
    const isControl = el.matches('button,a[href],input,textarea,select,[role="button"],[role="menuitem"],[role="combobox"]')
    if (el.matches('[role="dialog"],[role="menu"],[data-ui="context-menu"]') && (rect.x < -1 || rect.y < -1 || rect.x + rect.w > innerWidth + 1 || rect.y + rect.h > innerHeight + 1)) add(el, 'overlay-outside-viewport', 'warning', {viewport: viewport()})
    if (isControl && !disabled(el) && !inert(el) && clipped.rect) {
      const hit = hitTest(el)
      // Background controls intentionally covered by an active modal are not bugs.
      if (hit.supported && !hit.reachable && (!modal || modal.contains(el))) add(el, 'control-center-occluded', 'warning', {hit}, 'candidate')
    }
    const html = el as HTMLElement
    if (!privateContent(el) && renderedText(el) && html.clientWidth > 0 && html.scrollWidth > html.clientWidth + 2 && css.overflowX === 'hidden' && css.textOverflow !== 'ellipsis') add(el, 'text-may-be-clipped', 'warning', {clientWidth: html.clientWidth, scrollWidth: html.scrollWidth, overflowX: css.overflowX}, 'candidate')
    if (el instanceof HTMLImageElement && el.complete && el.naturalWidth === 0) add(el, 'image-load-failed', 'error', {complete: true, naturalWidth: 0})
    if (css.pointerEvents === 'none' && isControl && !disabled(el)) add(el, 'control-pointer-events-none', 'info', {pointerEvents: 'none'}, 'candidate')
  }
  if (modal && document.activeElement && document.activeElement !== document.body && !modal.contains(document.activeElement)) add(modal, 'modal-focus-outside', 'warning', {activeRef: refOf(document.activeElement)}, 'candidate')
  const fonts = fontProbe()
  if (fonts.failedCount) issues.push({rule: 'font-load-failed', severity: 'warning', confidence: 'confirmed', evidence: {failedCount: fonts.failedCount}})
  for (const terminal of terminalProbe()) {
    if (terminal.visible && terminal.screen && terminal.container && (terminal.screen.h > terminal.container.h + 2 || terminal.screen.w > terminal.container.w + 2)) issues.push({rule: 'terminal-screen-exceeds-container', severity: 'warning', confidence: 'candidate', rect: terminal.container, evidence: {clientId: terminal.clientId, screen: terminal.screen, container: terminal.container}})
    if (terminal.visible && terminal.canvases.some(canvas => canvas.bitmap.w === 0 || canvas.bitmap.h === 0)) issues.push({rule: 'terminal-zero-bitmap', severity: 'error', confidence: 'confirmed', evidence: {clientId: terminal.clientId}})
    if (terminal.visible && terminal.webglGeometry?.matches === false) issues.push({rule: 'terminal-webgl-grid-mismatch', severity: 'error', confidence: 'confirmed', evidence: {clientId: terminal.clientId, ...terminal.webglGeometry}})
  }
  return issues
}

type Stability = {settled: boolean; elapsedMs: number; stableForMs: number; pending: string[]; checkedFrames: number; scope: string; paintConfirmed: false}
function pendingReasons(ignoreTerminalOutput = false) {
  const pending: string[] = []
  if (!state.ready) pending.push('frontend-not-ready')
  if (document.visibilityState !== 'visible') pending.push('document-not-visible')
  if (document.fonts?.status === 'loading' || Object.values(remoteFontStatus).includes('loading')) pending.push('fonts-loading')
  if (!ignoreTerminalOutput && listDebugTerminals().some(entry => (entry.runtimeStatus?.().pendingOutputBytes ?? 0) > 0)) pending.push('terminal-output-pending')
  if (Array.from(document.querySelectorAll('[aria-busy="true"],[data-loading="true"]')).some(visible)) pending.push('visible-region-busy')
  return pending
}
function layoutSignature(root?: Element) {
  return candidates(root).filter(visible).slice(0, 250).map(el => `${refOf(el)}:${JSON.stringify(rectOf(el))}:${nameOf(el)}:${disabled(el)}`).join('|')
}
async function waitStable(args: Args = {}, root?: Element): Promise<Stability> {
  const start = performance.now(), timeout = bounded(args.timeoutMs, 1500, 0, 15000), stableFor = bounded(args.stableForMs, 100, 0, 1000)
  let last = '', stableAt = start, checkedFrames = 0, pending: string[] = []
  await nextTick()
  do {
    await frame(); checkedFrames++; flushMutations()
    const now = performance.now(), current = layoutSignature(root)
    if (current !== last) { last = current; stableAt = now }
    pending = pendingReasons(args.ignoreTerminalOutput === true)
    if (now - stableAt < stableFor || checkedFrames < 2) pending.push('layout-changing')
    if (!pending.length) return {settled: true, elapsedMs: Math.round(now - start), stableForMs: stableFor, pending: [], checkedFrames, scope: root ? 'target-subtree' : 'visible-controls', paintConfirmed: false}
    if (now - start >= timeout) break
    await pause(Math.min(25, Math.max(0, timeout - (now - start))))
  } while (performance.now() - start <= timeout + 100)
  return {settled: false, elapsedMs: Math.round(performance.now() - start), stableForMs: stableFor, pending, checkedFrames, scope: root ? 'target-subtree' : 'visible-controls', paintConfirmed: false}
}

function selectorElements(value: unknown) {
  if (!string(value).trim()) throw new Error('[TARGET_REQUIRED] Supply targetRef with observationId, or a selector.')
  try { return Array.from(document.querySelectorAll(string(value))) } catch { throw new Error('[INVALID_SELECTOR] Selector syntax is invalid.') }
}
function resolveTarget(args: Args, requireFresh = false): Element {
  if (args.targetRef) {
    flushMutations()
    const observation = state.observations.get(string(args.observationId))
    if (!observation || Date.now() - observation.createdAt > 120000) throw new Error('[STALE_OBSERVATION] Capture a new ui.observe before using this reference.')
    const target = observation.refs.get(string(args.targetRef))
    if (!target?.isConnected) throw new Error('[STALE_TARGET] The referenced element has been removed; observe again.')
    if (requireFresh && (observation.layoutSeq !== state.sequences.layout || observation.mutationSeq !== state.sequences.mutation || observation.signatures.get(string(args.targetRef)) !== signature(target))) throw new Error('[STALE_OBSERVATION] The page changed after this observation; observe again before acting.')
    return target
  }
  const matches = selectorElements(args.selector)
  if (matches.length !== 1) throw new Error(matches.length ? '[AMBIGUOUS_TARGET] Selector must match exactly one element.' : '[TARGET_NOT_FOUND] No element matches the selector.')
  return matches[0]
}

async function observe(args: Args = {}, settled?: Stability) {
  const scope = args.selector ? resolveTarget({selector: args.selector}) : undefined
  const stability = settled ?? await waitStable(args, scope)
  const limit = bounded(args.limit, 120, 1, 500)
  const all = candidates(scope).filter(visible)
  const elements = all.slice(0, limit)
  const nodes = elements.map(nodeFacts)
  flushMutations()
  const id = `obs-${state.pageEpoch}-${++state.nextObservation}`
  const record: Observation = {id, createdAt: Date.now(), mutationSeq: state.sequences.mutation, layoutSeq: state.sequences.layout,
    refs: new Map(elements.map(el => [refOf(el), el])), signatures: new Map(elements.map(el => [refOf(el), signature(el)]))}
  state.observations.set(id, record)
  while (state.observations.size > 8) state.observations.delete(state.observations.keys().next().value!)
  return {observationId: id, windowId: state.windowId, pageEpoch: state.pageEpoch, ready: state.ready, capturedAt: record.createdAt,
    sequences: {...state.sequences}, build: buildInfo(), appearance: {colorScheme: getComputedStyle(document.documentElement).colorScheme || 'unknown', background: getComputedStyle(document.body).backgroundColor, foreground: getComputedStyle(document.body).color}, viewport: viewport(), outer: {w: outerWidth, h: outerHeight}, inner: {w: innerWidth, h: innerHeight}, screen: {x: screenX, y: screenY}, view: view(), nodes, sensitiveRects: state.capture ? state.capture.sensitiveRects : sensitiveRegions(), captureLock: captureLockStatus(),
    issues: diagnoseElements(candidates(scope).slice(0, 500)), errors: state.errors.slice(-20), stability, runtime: runtimeProbe(),
    coverage: {screenshot: 'provided-by-host', dom: elements.length < all.length ? 'partial' : 'complete-for-semantic-nodes', nodeCount: nodes.length, totalSemanticNodes: all.length, truncated: elements.length < all.length,
      components: state.components.size ? 'public-registry' : 'unavailable', hitTest: typeof document.elementFromPoint === 'function' ? 'available' : 'unsupported', network: 'not-instrumented', glyphFont: 'unavailable', nativeInput: 'unsupported', paint: 'requires-native-screenshot'},
  }
}

function inspect(args: Args) {
  const el = resolveTarget(args), css = getComputedStyle(el)
  const props = ['display','position','z-index','overflow-x','overflow-y','pointer-events','visibility','opacity','color','background-color','font-family','font-size','font-weight','line-height','white-space','text-overflow','width','height','min-width','min-height','max-width','max-height','padding','margin','border','box-sizing','transform','zoom','flex','flex-shrink','gap']
  const owners = [...state.components.values()].filter(record => record.instance.$el instanceof Element && (record.instance.$el === el || record.instance.$el.contains(el)))
  const owner = owners.sort((a, b) => a.instance.$el.contains(b.instance.$el) ? 1 : -1)[0]
  return {...nodeFacts(el), windowId: state.windowId, pageEpoch: state.pageEpoch, sequences: {...state.sequences}, text: renderedText(el), styles: Object.fromEntries(props.map(prop => [prop, css.getPropertyValue(prop)])),
    clipping: clippedRect(el), scroll: {left: el.scrollLeft, top: el.scrollTop, width: el.scrollWidth, height: el.scrollHeight, clientWidth: el.clientWidth, clientHeight: el.clientHeight},
    component: owner ? {uid: owner.id, name: componentName(owner.instance)} : null, issues: diagnoseElements([el]), font: {requested: css.fontFamily, actualGlyphFont: 'unavailable'}, inputMode: 'dom-synthetic'}
}

function conditionFacts(condition: Args) {
  const kind = string(condition.kind || condition.type, condition.selector || condition.targetRef ? 'element' : 'stable')
  if (kind === 'ready') return {matched: state.ready, kind}
  if (kind === 'fonts') return {matched: !pendingReasons(true).includes('fonts-loading'), kind}
  if (kind === 'terminal-output') {
    const entries = listDebugTerminals().filter(entry => !condition.clientId || entry.clientId === condition.clientId)
    const sequence = condition.sequence === undefined ? null : Math.max(0, number(condition.sequence))
    return {matched: entries.length > 0 && entries.every(entry => {
      const runtime = entry.runtimeStatus?.()
      return runtime && (sequence === null ? runtime.pendingOutputBytes === 0 : runtime.parsedSequence !== undefined && runtime.parsedSequence >= sequence)
    }), kind, sequence, basis: sequence === null ? 'pending-parser-bytes' : 'parsed-output-watermark', paintConfirmed: false}
  }
  if (kind === 'view') {
    const current = view()
    return {matched: current.name === condition.view && (!condition.section || ('settingsSection' in current && current.settingsSection === condition.section)), kind}
  }
  if (kind === 'stable') return {matched: pendingReasons(condition.ignoreTerminalOutput === true).length === 0, kind}
  if (kind !== 'element') throw new Error('[INVALID_CONDITION] Supported condition kinds: element, view, ready, fonts, terminal-output, stable.')
  let elements: Element[] = []
  if (condition.targetRef) {
    const observation = state.observations.get(string(condition.observationId))
    if (!observation || Date.now() - observation.createdAt > 120000 || !observation.refs.has(string(condition.targetRef))) throw new Error('[STALE_OBSERVATION] Observe again before waiting on this reference.')
    const el = observation.refs.get(string(condition.targetRef))!
    if (el.isConnected) elements = [el]
  } else elements = selectorElements(condition.selector)
  const wantedExists = condition.exists !== false
  if (!wantedExists) return {matched: elements.length === 0, kind, count: elements.length}
  if (elements.length !== 1) return {matched: false, kind, count: elements.length, reason: elements.length ? 'ambiguous-target' : 'target-not-found'}
  const el = elements[0]
  let matched = true
  if (condition.visible !== undefined) matched &&= visible(el) === condition.visible
  if (condition.enabled !== undefined) matched &&= !disabled(el) === condition.enabled
  if (condition.focused !== undefined) matched &&= (document.activeElement === el) === condition.focused
  if (condition.textContains !== undefined) matched &&= !privateContent(el) && renderedText(el).includes(string(condition.textContains))
  if (condition.text !== undefined) matched &&= !privateContent(el) && renderedText(el) === string(condition.text)
  if (condition.checked !== undefined) matched &&= el instanceof HTMLInputElement && el.checked === condition.checked
  return {matched, kind, count: 1, targetRef: refOf(el)}
}

async function wait(args: Args) {
  const condition = (args.condition && typeof args.condition === 'object' ? args.condition : {kind: 'stable'}) as Args
  const start = performance.now(), timeout = bounded(args.timeoutMs, 3000, 0, 15000), stableFor = bounded(args.stableForMs, 100, 0, 1000)
  let matchAt = 0, last: Record<string, unknown> = {}, previous = '', checkedFrames = 0
  do {
    await nextTick(); await frame(); checkedFrames++; flushMutations()
    const now = performance.now()
    last = conditionFacts(condition)
    const geometry = string(condition.kind || condition.type, 'stable') === 'stable' ? layoutSignature() : ''
    if (geometry !== previous) { matchAt = 0; previous = geometry }
    if (last.matched) { if (!matchAt) matchAt = now } else matchAt = 0
    if (matchAt && now - matchAt >= stableFor && checkedFrames >= 2) break
    if (now - start >= timeout) break
    await pause(25)
  } while (performance.now() - start <= timeout + 100)
  const matched = !!last.matched && !!matchAt && performance.now() - matchAt >= stableFor && checkedFrames >= 2
  const stability: Stability = {settled: matched, elapsedMs: Math.round(performance.now() - start), stableForMs: stableFor, pending: matched ? [] : ['condition-not-met'], checkedFrames, scope: 'condition', paintConfirmed: false}
  return {matched, timedOut: !matched, condition: last, stability, observation: await observe({timeoutMs: 0, stableForMs: 0}, stability)}
}

function nativeValue(el: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement, value: string) {
  const prototype = el instanceof HTMLInputElement ? HTMLInputElement.prototype : el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLSelectElement.prototype
  Object.getOwnPropertyDescriptor(prototype, 'value')?.set?.call(el, value)
  el.dispatchEvent(new Event('input', {bubbles: true})); el.dispatchEvent(new Event('change', {bubbles: true}))
}

async function act(args: Args) {
  const action = string(args.action), params = (args.params && typeof args.params === 'object' ? args.params : args) as Args
  let el: Element
  try { el = resolveTarget(args, true) } catch (error) { return {accepted: false, dispatched: false, settled: false, effectConfirmed: false, inputMode: 'dom-synthetic', error: {code: (error as Error).message.match(/\[([A-Z_]+)\]/)?.[1] || 'TARGET_ERROR', message: (error as Error).message}} }
  const reject = (code: string) => ({accepted: false, dispatched: false, settled: false, effectConfirmed: false, inputMode: 'dom-synthetic', error: {code}, targetRef: refOf(el)})
  if (!['click','type','key','hover','scroll','focus','select','check'].includes(action)) return reject('UNSUPPORTED_ACTION')
  if (!visible(el)) return reject('TARGET_NOT_VISIBLE')
  if (disabled(el)) return reject('TARGET_DISABLED')
  if (inert(el)) return reject('TARGET_INERT')
  if (getComputedStyle(el).pointerEvents === 'none' && action !== 'key') return reject('TARGET_POINTER_EVENTS_NONE')
  if (action === 'type' || action === 'select') {
    if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement || (el as HTMLElement).isContentEditable)) return reject('TARGET_NOT_EDITABLE')
    if ((el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) && el.readOnly) return reject('TARGET_READONLY')
    if (typeof params.text !== 'string' || params.text.length > 32768) return reject('INVALID_TEXT')
  }
  if (action === 'check' && !(el instanceof HTMLInputElement && ['checkbox','radio'].includes(el.type))) return reject('TARGET_NOT_CHECKABLE')
  if (action === 'key' && (!string(params.key) || string(params.key).length > 40)) return reject('INVALID_KEY')
  ;(el as HTMLElement).scrollIntoView?.({block: 'nearest', inline: 'nearest', behavior: 'instant'})
  await nextTick(); await frame()
  // Scrolling or async work may replace the target. Never dispatch into a
  // disconnected node or ignore a modal that appeared in the meantime.
  if (!el.isConnected) return reject('STALE_TARGET')
  if (disabled(el) || inert(el) || !visible(el)) return reject('TARGET_CHANGED')
  const hit = hitTest(el)
  if (!hit.supported) return reject('HIT_TEST_UNSUPPORTED')
  if (!hit.reachable || !hit.point) return reject('TARGET_OCCLUDED')
  const dispatched: string[] = []
  let expectedValue: string | undefined, expectedChecked: boolean | undefined
  const scrollBefore = {x: el.scrollLeft, y: el.scrollTop}
  const focus = () => { if (el instanceof HTMLElement) el.focus({preventScroll: true}) }
  if (action === 'click' || action === 'check') {
    focus()
    if (action === 'check') expectedChecked = params.checked !== false
    if (action !== 'check' || (el as HTMLInputElement).checked !== expectedChecked) {
      const options: MouseEventInit = {bubbles: true, cancelable: true, composed: true, clientX: hit.point.x, clientY: hit.point.y, button: 0}
      for (const type of ['pointerdown','mousedown','pointerup','mouseup','click']) {
        el.dispatchEvent(type.startsWith('pointer') && typeof PointerEvent === 'function' ? new PointerEvent(type, {...options, pointerId: 1, pointerType: 'mouse', isPrimary: true, buttons: type.endsWith('down') ? 1 : 0}) : new MouseEvent(type, {...options, buttons: type.endsWith('down') ? 1 : 0}))
        dispatched.push(type)
      }
    }
  } else if (action === 'focus') { focus(); dispatched.push('focus') }
  else if (action === 'type' || action === 'select') {
    focus()
    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) {
      expectedValue = params.clear === false && action !== 'select' ? el.value + string(params.text) : string(params.text)
      nativeValue(el, expectedValue)
    } else {
      expectedValue = params.clear === false ? (el.textContent ?? '') + string(params.text) : string(params.text)
      el.textContent = expectedValue
      el.dispatchEvent(new InputEvent('input', {bubbles: true, inputType: 'insertText', data: string(params.text)}))
    }
    dispatched.push('input','change')
  } else if (action === 'key') {
    focus()
    const options: KeyboardEventInit = {key: string(params.key), code: string(params.code), ctrlKey: params.ctrl === true, altKey: params.alt === true, shiftKey: params.shift === true, metaKey: params.meta === true, bubbles: true, cancelable: true, composed: true}
    for (const type of ['keydown','keyup']) { el.dispatchEvent(new KeyboardEvent(type, options)); dispatched.push(type) }
  } else if (action === 'hover') {
    for (const type of ['mouseover','mouseenter','mousemove']) { el.dispatchEvent(new MouseEvent(type, {clientX: hit.point.x, clientY: hit.point.y, bubbles: type !== 'mouseenter'})); dispatched.push(type) }
  } else if (action === 'scroll') {
    const html = el as HTMLElement
    if (typeof html.scrollBy !== 'function') return reject('SCROLL_UNSUPPORTED')
    html.scrollBy({left: bounded(params.dx, 0, -100000, 100000), top: bounded(params.dy, 0, -100000, 100000), behavior: 'instant'})
    dispatched.push('scroll')
  }
  const waitArgs = (args.wait && typeof args.wait === 'object' ? args.wait : {}) as Args
  let waited: Awaited<ReturnType<typeof wait>> | undefined
  if (waitArgs.condition) waited = await wait(waitArgs)
  const stability = waited?.stability ?? await waitStable(waitArgs)
  const actualValue = el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement ? el.value : el.textContent
  const effectConfirmed = waited ? waited.matched : expectedValue !== undefined ? el.isConnected && actualValue === expectedValue : expectedChecked !== undefined ? el.isConnected && (el as HTMLInputElement).checked === expectedChecked : action === 'focus' ? document.activeElement === el : action === 'scroll' ? el.scrollLeft !== scrollBefore.x || el.scrollTop !== scrollBefore.y : false
  return {accepted: true, dispatched: true, dispatchedEvents: dispatched, settled: stability.settled, effectConfirmed, effectBasis: waited ? 'explicit-condition' : ['type','select','check','focus','scroll'].includes(action) ? 'target-state' : 'no-explicit-effect-condition', inputMode: 'dom-synthetic', trusted: false,
    limitations: action === 'key' ? ['browser-default-key-actions-not-emulated'] : action === 'hover' ? ['native-pointer-and-css-hover-not-emulated'] : [], targetRef: refOf(el), stability,
    observation: waited?.observation ?? await observe({timeoutMs: 0}, stability)}
}

/** Host-only native-input preflight. Never dispatch keyboard, pointer or input events. */
async function prepareAct(args: Args) {
  const action = string(args.action), params = (args.params && typeof args.params === 'object' ? args.params : args) as Args
  const rejected = (code: string, target?: Element) => ({accepted: false, dispatched: false, settled: false, effectConfirmed: false, inputMode: 'native-preflight', error: {code}, targetRef: target ? refOf(target) : undefined, ...debugRevision()})
  let el: Element
  try { el = resolveTarget(args, true) } catch (error) { return rejected((error as Error).message.match(/\[([A-Z_]+)\]/)?.[1] || 'TARGET_ERROR') }
  if (!['click','type','key'].includes(action)) return rejected('UNSUPPORTED_ACTION', el)
  if (!visible(el)) return rejected('TARGET_NOT_VISIBLE', el)
  if (disabled(el)) return rejected('TARGET_DISABLED', el)
  if (inert(el)) return rejected('TARGET_INERT', el)
  if (getComputedStyle(el).pointerEvents === 'none' && action !== 'key') return rejected('TARGET_POINTER_EVENTS_NONE', el)
  if (action === 'type') {
    if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || (el as HTMLElement).isContentEditable)) return rejected('TARGET_NOT_EDITABLE', el)
    if ((el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) && el.readOnly) return rejected('TARGET_READONLY', el)
    if (typeof params.text !== 'string' || params.text.length > 32768) return rejected('INVALID_TEXT', el)
  }
  if (action === 'key' && (!string(params.key) || string(params.key).length > 40)) return rejected('INVALID_KEY', el)
  ;(el as HTMLElement).scrollIntoView?.({block: 'nearest', inline: 'nearest', behavior: 'instant'})
  await nextTick(); await frame()
  if (!el.isConnected) return rejected('STALE_TARGET', el)
  if (disabled(el) || inert(el) || !visible(el)) return rejected('TARGET_CHANGED', el)
  let hit = hitTest(el)
  if (!hit.supported) return rejected('HIT_TEST_UNSUPPORTED', el)
  if (!hit.reachable || !hit.point) return rejected('TARGET_OCCLUDED', el)
  const requiresFocusedTarget = action === 'type' || action === 'key'
  if (requiresFocusedTarget) {
    if (!(el instanceof HTMLElement)) return rejected('TARGET_NOT_FOCUSABLE', el)
    el.focus({preventScroll: true})
    await nextTick(); await frame()
    if (!el.isConnected || !visible(el) || disabled(el) || inert(el)) return rejected('TARGET_CHANGED', el)
    if (document.activeElement !== el) return rejected('TARGET_FOCUS_FAILED', el)
    hit = hitTest(el)
    if (!hit.reachable || !hit.point) return rejected('TARGET_OCCLUDED', el)
  }
  return {accepted: true, dispatched: false, point: hit.point, targetRef: refOf(el), requiresFocusedTarget, focused: document.activeElement === el, ...debugRevision()}
}

export function observationDiagnostics() {
  const sensitiveRects = state.capture ? state.capture.sensitiveRects : sensitiveRegions()
  return {windowId: state.windowId, pageEpoch: state.pageEpoch, ready: state.ready, sequences: {...state.sequences}, viewport: viewport(), outer: {w: outerWidth, h: outerHeight}, inner: {w: innerWidth, h: innerHeight}, screen: {x: screenX, y: screenY}, sensitiveRects, sensitiveLayout: sensitiveLayoutActivity(), captureLock: captureLockStatus(), build: buildInfo(), runtime: runtimeProbe(), errors: state.errors.slice(-50), droppedErrors: state.droppedErrors,
    components: {count: state.components.size, source: 'public-component-registry'}, observations: {retained: state.observations.size, max: 8, ttlMs: 120000},
    supportedActions: ['click','type','key','hover','scroll','focus','select','check'], inputMode: 'dom-synthetic', trusted: false,
    coverage: {network: 'not-instrumented', nativeInput: 'unsupported', actualGlyphFont: 'unavailable', rendererGrid: 'per-terminal-probe', screenshot: 'host-owned', errorDetails: 'safe-metadata-only'}}
}

export async function handleObservationOp(op: string, args: Args): Promise<unknown> {
  switch (op) {
    case 'ui.observe': return observe(args)
    case 'ui.inspect': return inspect(args)
    case 'ui.diagnose': {
      const root = args.selector ? resolveTarget({selector: args.selector}) : undefined
      const all = candidates(root), limit = bounded(args.limit, 500, 1, 1000)
      return {windowId: state.windowId, pageEpoch: state.pageEpoch, sequences: {...state.sequences}, issues: diagnoseElements(all.slice(0, limit)), scannedNodes: Math.min(limit, all.length), truncated: all.length > limit, coverage: {rules: ['overlay-outside-viewport','control-center-occluded','text-may-be-clipped','image-load-failed','control-pointer-events-none','modal-focus-outside','font-load-failed','terminal-screen-exceeds-container','terminal-zero-bitmap','terminal-webgl-grid-mismatch'], visualJudgment: 'requires-image-review'}}
    }
    case 'ui.wait': return wait(args)
    case 'ui.act': return act(args)
    case 'ui.prepareAct': return prepareAct(args)
    case 'ui.capturePrepare': return prepareCapture()
    case 'ui.captureRelease': return releaseCapture(string(args.captureToken))
    case 'ui.diagnostics': return observationDiagnostics()
    default: throw new Error('[UNKNOWN_OBSERVATION_OP] Unknown observation operation.')
  }
}
