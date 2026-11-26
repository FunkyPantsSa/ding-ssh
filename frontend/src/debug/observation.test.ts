import {createApp, defineComponent, h, type App} from 'vue'
import {createPinia, setActivePinia} from 'pinia'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {installDebugUI, handleUIOp} from './ui'
import {markDebugReady, recordDebugError} from './observation'

let app: App | undefined
let topElement: Element | null
const rect = {x: 10, y: 10, width: 100, height: 24, top: 10, left: 10, right: 110, bottom: 34, toJSON: () => ({})}
const fast = {timeoutMs: 1500, stableForMs: 0}

beforeEach(() => {
  setActivePinia(createPinia())
  document.body.innerHTML = '<div id="app"></div>'
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(rect)
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([rect] as unknown as DOMRectList)
  Object.defineProperty(document, 'elementFromPoint', {configurable: true, value: vi.fn(() => topElement)})
  topElement = null
  app = createApp(defineComponent({name: 'ObservationFixture', render: () => h('section', {'data-ui': 'fixture'}, [h('button', {id: 'target'}, 'Connect'), h('input', {id: 'input'}), h('input', {id: 'password', type: 'password', value: 'do-not-export-123'})])}))
  installDebugUI(app)
  app.mount('#app')
  markDebugReady()
})

afterEach(() => {
  app?.unmount()
  app = undefined
  vi.restoreAllMocks()
  delete (document as unknown as Record<string, unknown>).elementFromPoint
  document.body.innerHTML = ''
})

async function observe() {
  return await handleUIOp('ui.observe', fast) as {observationId: string; pageEpoch: string; nodes: Array<{ref: string; name: string; tag: string; sensitive: boolean}>; sensitiveRects: Array<{x: number; y: number; w: number; h: number}>; stability: {settled: boolean}; coverage: Record<string, unknown>}
}

describe('UI observation and verified synthetic interaction', () => {
  it('captures semantic refs and explicit coverage without leaking input values', async () => {
    topElement = document.querySelector('#target')
    const result = await observe()
    expect(result.pageEpoch).toBeTruthy()
    expect(result.observationId).toContain(result.pageEpoch)
    expect(result.nodes.some(node => node.name === 'Connect')).toBe(true)
    expect(result.nodes.some(node => node.tag === 'input' && node.sensitive && node.name === '')).toBe(true)
    expect(result.sensitiveRects).toEqual([{x: 10, y: 10, w: 100, h: 24}])
    expect(JSON.stringify(result)).not.toContain('do-not-export-123')
    expect(result.coverage.paint).toBe('requires-native-screenshot')
    expect(result.stability.settled).toBe(true)
  })

  it('collects all visible secret pixel masks independently of node truncation', async () => {
    const section = document.createElement('section')
    section.setAttribute('data-sensitive', '')
    section.innerHTML = '<div>hidden-private-label</div><input value="hidden-private-value" />'
    document.body.append(section)
    const hidden = document.createElement('input')
    hidden.type = 'password'
    hidden.style.display = 'none'
    document.body.append(hidden)
    const result = await handleUIOp('ui.observe', {...fast, limit: 1}) as {sensitiveRects: unknown[]}
    expect(result.sensitiveRects).toHaveLength(2)
    expect(JSON.stringify(result)).not.toContain('hidden-private-')
  })

  it('locks all form controls for a painted screenshot and restores inline styles', async () => {
    const password = document.querySelector('#password') as HTMLInputElement
    const ordinary = document.querySelector('#input') as HTMLInputElement
    password.style.opacity = '0.7'
    ordinary.style.transitionProperty = 'opacity'
    const prepared = await handleUIOp('ui.capturePrepare', {}) as {accepted: boolean; captureToken: string; captureLock: {active: boolean; maskedCount: number; frames: number}; sensitiveRects: unknown[]; observation: {captureLock: {active: boolean}}}
    expect(prepared).toMatchObject({accepted: true, captureLock: {active: true, frames: 2}, observation: {captureLock: {active: true}}})
    expect(prepared.captureLock.maskedCount).toBeGreaterThanOrEqual(2)
    expect(prepared.sensitiveRects).toHaveLength(1)
    expect(getComputedStyle(password).opacity).toBe('0')
    expect(getComputedStyle(ordinary).opacity).toBe('0')
    const locked = await handleUIOp('ui.diagnostics', {}) as {captureLock: {active: boolean; token: string}; sensitiveRects: unknown[]; viewport: unknown}
    expect(locked.captureLock).toMatchObject({active: true, token: prepared.captureToken})
    expect(locked.sensitiveRects).toEqual(prepared.sensitiveRects)
    expect(locked.viewport).toBeTruthy()
    const released = await handleUIOp('ui.captureRelease', {captureToken: prepared.captureToken}) as {released: boolean; captureLock: {active: boolean}}
    expect(released).toMatchObject({released: true, captureLock: {active: false}})
    expect(password.style.opacity).toBe('0.7')
    expect(ordinary.style.transitionProperty).toBe('opacity')
  })

  it('keeps capture-owned DOM edits out of observation sequences while counting app mutations', async () => {
    const before = await handleUIOp('ui.observe', fast) as {sequences: {mutation: number; layout: number}}
    const prepared = await handleUIOp('ui.capturePrepare', {}) as {
      accepted: boolean
      captureToken: string
      observation: {sequences: {mutation: number; layout: number}}
    }
    expect(prepared.accepted).toBe(true)
    expect(prepared.observation.sequences).toMatchObject({mutation: before.sequences.mutation, layout: before.sequences.layout})

    const ordinary = document.querySelector('#input') as HTMLInputElement
    ordinary.style.opacity = '0.4'
    await new Promise(resolve => setTimeout(resolve, 0))
    const during = await handleUIOp('ui.diagnostics', {}) as {sequences: {mutation: number; layout: number}}
    expect(during.sequences.mutation).toBe(prepared.observation.sequences.mutation + 1)
    expect(during.sequences.layout).toBe(prepared.observation.sequences.layout + 1)
    expect(getComputedStyle(ordinary).opacity).toBe('0')

    await handleUIOp('ui.captureRelease', {captureToken: prepared.captureToken})
    const after = await handleUIOp('ui.diagnostics', {}) as {sequences: {mutation: number; layout: number}}
    expect(after.sequences).toEqual(during.sequences)
    expect(ordinary.style.opacity).toBe('0.4')
  })

  it('settles a queued application mutation before taking the capture-lock observation', async () => {
    const before = await handleUIOp('ui.diagnostics', {}) as {sequences: {mutation: number; layout: number}}
    document.body.append(document.createElement('aside'))
    const prepared = await handleUIOp('ui.capturePrepare', {}) as {
      accepted: boolean
      captureToken: string
      observation: {sequences: {mutation: number; layout: number}}
    }
    expect(prepared.accepted).toBe(true)
    expect(prepared.observation.sequences.mutation).toBe(before.sequences.mutation + 1)
    expect(prepared.observation.sequences.layout).toBe(before.sequences.layout + 1)
    await handleUIOp('ui.captureRelease', {captureToken: prepared.captureToken})
  })

  it('rejects nested or stale screenshot locks and never leaves masking active', async () => {
    const first = await handleUIOp('ui.capturePrepare', {}) as {accepted: boolean; captureToken: string}
    const nested = await handleUIOp('ui.capturePrepare', {}) as {accepted: boolean; error: {code: string}}
    expect(first.accepted).toBe(true)
    expect(nested).toMatchObject({accepted: false, error: {code: 'CAPTURE_BUSY'}})
    const stale = await handleUIOp('ui.captureRelease', {captureToken: 'capture-stale-token'}) as {released: boolean; reason: string; captureLock: {active: boolean}}
    expect(stale).toMatchObject({released: false, reason: 'CAPTURE_TOKEN_MISMATCH', captureLock: {active: true}})
    const released = await handleUIOp('ui.captureRelease', {captureToken: first.captureToken}) as {released: boolean; captureLock: {active: boolean}}
    expect(released).toMatchObject({released: true, captureLock: {active: false}})
  })

  it('rejects stale references before dispatching an action', async () => {
    const target = document.querySelector('#target')!
    topElement = target
    const clicked = vi.fn()
    target.addEventListener('click', clicked)
    const seen = await observe()
    const ref = seen.nodes.find(node => node.name === 'Connect')!.ref
    target.textContent = 'Changed after observation'
    const result = await handleUIOp('ui.act', {observationId: seen.observationId, targetRef: ref, action: 'click', wait: fast}) as {accepted: boolean; error: {code: string}}
    expect(result.accepted).toBe(false)
    expect(result.error.code).toBe('STALE_OBSERVATION')
    expect(clicked).not.toHaveBeenCalled()
  })

  it('refuses ambiguous, occluded, disabled and inert controls', async () => {
    const target = document.querySelector('#target')!
    const blocked = await handleUIOp('ui.act', {selector: '#target', action: 'click', wait: fast}) as {error: {code: string}}
    expect(blocked.error.code).toBe('TARGET_OCCLUDED')
    topElement = target
    target.setAttribute('disabled', '')
    const disabled = await handleUIOp('ui.act', {selector: '#target', action: 'click', wait: fast}) as {error: {code: string}}
    expect(disabled.error.code).toBe('TARGET_DISABLED')
    target.removeAttribute('disabled')
    target.parentElement!.setAttribute('inert', '')
    const inert = await handleUIOp('ui.act', {selector: '#target', action: 'click', wait: fast}) as {error: {code: string}}
    expect(inert.error.code).toBe('TARGET_INERT')
    const ambiguous = await handleUIOp('ui.act', {selector: 'input', action: 'focus', wait: fast}) as {error: {code: string}}
    expect(ambiguous.error.code).toBe('AMBIGUOUS_TARGET')
  })

  it('distinguishes dispatched clicks from a confirmed business effect', async () => {
    topElement = document.querySelector('#target')
    const result = await handleUIOp('ui.act', {selector: '#target', action: 'click', wait: fast}) as {accepted: boolean; dispatched: boolean; settled: boolean; effectConfirmed: boolean; trusted: boolean; inputMode: string}
    expect(result).toMatchObject({accepted: true, dispatched: true, settled: true, effectConfirmed: false, trusted: false, inputMode: 'dom-synthetic'})
  })

  it('prepares native clicks without dispatching pointer or click events', async () => {
    const target = document.querySelector('#target')!
    topElement = target
    const clicked = vi.fn(), pointer = vi.fn()
    target.addEventListener('click', clicked)
    target.addEventListener('pointerdown', pointer)
    const result = await handleUIOp('ui.prepareAct', {selector: '#target', action: 'click'}) as {accepted: boolean; dispatched: boolean; point: {x: number; y: number}; pageEpoch: string; sequences: unknown}
    expect(result).toMatchObject({accepted: true, dispatched: false, point: {x: 60, y: 22}, pageEpoch: expect.any(String), sequences: expect.any(Object)})
    expect(clicked).not.toHaveBeenCalled()
    expect(pointer).not.toHaveBeenCalled()
  })

  it('focuses native typing targets without changing or emitting their value', async () => {
    const target = document.querySelector('#input') as HTMLInputElement
    topElement = target
    const input = vi.fn()
    target.addEventListener('input', input)
    const result = await handleUIOp('ui.prepareAct', {selector: '#input', action: 'type', params: {text: 'native text'}}) as {accepted: boolean; requiresFocusedTarget: boolean; focused: boolean}
    expect(result).toMatchObject({accepted: true, requiresFocusedTarget: true, focused: true})
    expect(document.activeElement).toBe(target)
    expect(target.value).toBe('')
    expect(input).not.toHaveBeenCalled()
  })

  it('confirms typing through the input state and never echoes a password', async () => {
    topElement = document.querySelector('#password')
    const result = await handleUIOp('ui.act', {selector: '#password', action: 'type', params: {text: 'new-secret-value'}, wait: fast}) as {effectConfirmed: boolean}
    expect(result.effectConfirmed).toBe(true)
    expect((document.querySelector('#password') as HTMLInputElement).value).toBe('new-secret-value')
    expect(JSON.stringify(result)).not.toContain('new-secret-value')
  })

  it('returns a useful last observation on a wait timeout', async () => {
    const result = await handleUIOp('ui.wait', {condition: {kind: 'element', selector: '#missing', visible: true}, timeoutMs: 40, stableForMs: 0}) as {matched: boolean; timedOut: boolean; observation: {observationId: string}; stability: {pending: string[]}}
    expect(result.matched).toBe(false)
    expect(result.timedOut).toBe(true)
    expect(result.observation.observationId).toBeTruthy()
    expect(result.stability.pending).toContain('condition-not-met')
  })

  it('inspects safe facts and reports a geometric diagnostic with evidence', async () => {
    topElement = document.querySelector('#target')
    const result = await handleUIOp('ui.inspect', {selector: '#target'}) as {component: {name: string}; hit: {reachable: boolean}; rect: {w: number}}
    expect(result.component.name).toBe('ObservationFixture')
    expect(result.hit.reachable).toBe(true)
    expect(result.rect.w).toBe(100)
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    document.body.append(dialog)
    vi.spyOn(dialog, 'getBoundingClientRect').mockReturnValue({...rect, x: -20, left: -20})
    const diagnostics = await handleUIOp('ui.diagnose', {}) as {issues: Array<{rule: string; rect: unknown; evidence: unknown}>}
    expect(diagnostics.issues.some(issue => issue.rule === 'overlay-outside-viewport' && issue.rect && issue.evidence)).toBe(true)
  })

  it('records safe structured errors without messages or URL credentials', () => {
    const error = new TypeError('unlabelled-secret-456')
    error.stack = 'TypeError: unlabelled-secret-456\n at render (http://localhost/src/App.vue?token=url-secret:12:4)'
    recordDebugError(error, 'vue', 'render function')
    return handleUIOp('ui.diagnostics', {}).then(result => {
      const text = JSON.stringify(result)
      expect(text).toContain('JS_TYPE')
      expect(text).toContain('render function')
      expect(text).not.toContain('unlabelled-secret-456')
      expect(text).not.toContain('url-secret')
    })
  })

  it('captures render errors before mount and calls the existing Vue error handler', async () => {
    app?.unmount()
    const original = vi.fn()
    app = createApp(defineComponent({name: 'BrokenFixture', render() { throw new ReferenceError('private-error-message') }}))
    app.config.errorHandler = original
    installDebugUI(app)
    app.mount('#app')
    expect(original).toHaveBeenCalledOnce()
    const result = await handleUIOp('ui.diagnostics', {}) as {errors: Array<{code: string; source: string; component?: string}>}
    expect(result.errors.some(error => error.code === 'JS_REFERENCE' && error.source === 'vue' && error.component === 'BrokenFixture')).toBe(true)
    expect(JSON.stringify(result)).not.toContain('private-error-message')
  })

  it('uses a production-compatible registry for the component tree', async () => {
    const result = await handleUIOp('ui.tree', {}) as {source: string; nodes: Array<{name: string}>}
    expect(result.source).toBe('public-component-registry')
    expect(result.nodes.some(node => node.name === 'ObservationFixture')).toBe(true)
  })
})
