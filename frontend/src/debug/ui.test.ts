import {createApp, defineComponent, h, type App} from 'vue'
import {createPinia, setActivePinia} from 'pinia'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import Input from '../components/ui/input/Input.vue'
import {handleUIOp, installDebugUI} from './ui'

let app: App | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  document.body.innerHTML = '<div id="app"></div>'
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({x: 0, y: 0, width: 100, height: 20, top: 0, left: 0, right: 100, bottom: 20, toJSON: () => ({})})
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{width: 100, height: 20}] as unknown as DOMRectList)
  vi.spyOn(HTMLElement.prototype, 'offsetParent', 'get').mockImplementation(function (this: HTMLElement) { return this.parentElement })
})

afterEach(() => {
  app?.unmount()
  app = undefined
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('UI inspection secret boundaries', () => {
  it('reports component prop types and sizes without hidden modelValue or arbitrary values', async () => {
    const secrets = ['hidden-password-8b1', 'opaque-blob-7a2', 'nested-secret-key-6c3', 'secret-symbol-5d4']
    const Blob = defineComponent({
      name: 'OpaqueInput',
      props: ['value', 'blob', 'config', 'count', 'enabled', 'symbol'],
      render: () => h('div', 'Visible form'),
    })
    app = createApp(defineComponent({
      name: 'InspectionFixture',
      render: () => h('section', [
        h(Input, {modelValue: secrets[0], type: 'password'}),
        h(Blob, {value: secrets[1], blob: [secrets[1]], config: {[secrets[2]]: secrets[0]}, count: 123456, enabled: true, symbol: Symbol(secrets[3])}),
      ]),
    }))
    app.mount('#app')
    const result = await handleUIOp('ui.tree', {depth: 6, limit: 200}) as {nodes: Array<{name: string; propsSummary: Record<string, string>}>}
    const text = JSON.stringify(result)
    for (const secret of secrets) expect(text).not.toContain(secret)
    const input = result.nodes.find(node => node.name === 'Input')!
    expect(input.propsSummary.modelValue).toBe(`[String(${secrets[0].length})]`)
    const opaque = result.nodes.find(node => node.name === 'OpaqueInput')!
    expect(opaque.propsSummary.blob).toBe('[Array(1)]')
    expect(opaque.propsSummary.config).toBe('[Object(1 keys)]')
    expect(opaque.propsSummary.count).toBe('[number]')
    expect(text).not.toContain('123456')
  })

  it('keeps query, element facts and fingerprints within visible text and safe attributes', async () => {
    document.body.innerHTML = `<section id="inspect-root">Visible heading
      <span hidden>hidden-secret-11</span><span style="display:none">hidden-secret-22</span>
      <span style="opacity:0"><b>hidden-secret-33</b></span><span aria-hidden="true">hidden-secret-44</span>
      <script type="application/json">{"password":"hidden-secret-55"}</script>
      <textarea id="private-key-content">hidden-secret-66</textarea>
      <input id="password" type="password" value="hidden-secret-77" title="hidden-secret-88" />
      <input type="hidden" value="hidden-secret-99" />
      <a href="https://user:hidden-secret-aa@example.test/?token=hidden-secret-bb" data-opaque="hidden-secret-cc">Visible link</a>
      <input id="ordinary" value="hidden-secret-dd" />
    </section>`
    for (const [op, args] of [
      ['ui.query', {selector: '#inspect-root, #password, #ordinary, a', props: ['value', 'href', 'data-opaque', 'title']}],
      ['ui.elementFacts', {selector: '#inspect-root'}],
      ['ui.fingerprint', {selector: '#inspect-root'}],
    ] as const) {
      const result = JSON.stringify(await handleUIOp(op, args))
      expect(result).toContain('Visible')
      expect(result).not.toContain('hidden-secret-')
    }
    const query = await handleUIOp('ui.query', {selector: '#ordinary'}) as {matches: Array<{attrs: Record<string, string>}>}
    expect(query.matches[0].attrs).toHaveProperty('id', 'ordinary')
    expect(query.matches[0].attrs).not.toHaveProperty('value')
  })

  it('captures console and exception metadata without exporting hidden argument or stack bodies', async () => {
    const levels = ['log', 'info', 'warn', 'error', 'debug'] as const
    const originals = Object.fromEntries(levels.map(level => [level, console[level]]))
    const oldOnError = window.onerror
    try {
      for (const level of levels) console[level] = vi.fn()
      installDebugUI()
      console.log('console-secret-1', {opaque: 'console-secret-2'})
      const error = new Error('exception-secret-3')
      error.stack = 'stack-secret-4'
      window.onerror?.('message-secret-5', 'https://example.test/?token=url-secret-6', 1, 1, error)
      const event = new Event('unhandledrejection')
      Object.defineProperty(event, 'reason', {value: 'promise-secret-7'})
      window.dispatchEvent(event)
      const result = await handleUIOp('ui.console', {}) as {entries: Array<{level: string; text: string[]; source?: string; stack?: string}>}
      const text = JSON.stringify(result)
      expect(result.entries.length).toBeGreaterThanOrEqual(3)
      expect(result.entries.some(entry => entry.source === 'window.onerror')).toBe(true)
      expect(result.entries.every(entry => entry.stack === undefined)).toBe(true)
      expect(text).not.toMatch(/(?:console|exception|stack|message|url|promise)-secret-/)
      expect(text).toContain('[String(')
    } finally {
      for (const level of levels) console[level] = originals[level]
      window.onerror = oldOnError
    }
  })
})
