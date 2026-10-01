// 调试模式：前端桥。
//
// 后端（internal/debugsrv + debug.go）通过 `debug:request` 事件下发操作，这里执行后调用
// Wails 绑定 DebugReply(id, payload) 回包；payload 形如 {"ok":true,"data":...}。
//
// 之所以需要桥：xterm 的缓冲区、滚动位置、DOM 几何只有页面里才知道；后端 API 负责鉴权、
// 会话管理与对外协议，前端桥负责「页面内视角」的能力（读状态 / 输入 / 滚动 / eval）。
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {DebugReply} from '../../wailsjs/go/main/App'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {useUIStore} from '../stores/ui'
import {getDebugTerminal, listDebugTerminals, type DebugTerminalEntry} from './registry'

interface DebugRequest {
  id: string
  op: string
  args?: Record<string, unknown>
}

const str = (v: unknown, fallback = ''): string => (typeof v === 'string' ? v : fallback)
const num = (v: unknown, fallback = 0): number => {
  const n = Number(v)
  return Number.isFinite(n) ? n : fallback
}

/** 读取缓冲区某段文本行。 */
function readLines(e: DebugTerminalEntry, from: unknown, lines: unknown) {
  const b = e.term.buffer.active
  const total = b.length
  const want = Math.max(1, Math.min(20000, num(lines, 200)))
  let start = Math.max(0, total - want)
  if (typeof from === 'number') {
    start = Math.max(0, Math.min(Math.max(0, total - 1), from))
  } else if (from === 'head') {
    start = 0
  }
  const out: string[] = []
  for (let i = start; i < Math.min(total, start + want); i++) {
    const line = b.getLine(i)
    out.push(line ? line.translateToString(true) : '')
  }
  return {start, total, baseY: b.baseY, viewportY: b.viewportY, count: out.length, lines: out, text: out.join('\n')}
}

/** 终端状态快照：xterm 内部状态 + 容器几何（排障「顶部截断」这类问题全靠这些数字）。 */
function terminalState(e: DebugTerminalEntry) {
  const t = e.term
  const b = t.buffer.active
  const el = e.container()
  const vp = el?.querySelector('.xterm-viewport') as HTMLElement | null
  const sc = el?.querySelector('.xterm-screen') as HTMLElement | null
  const sa = el?.querySelector('.xterm-scroll-area') as HTMLElement | null
  const cv = sc?.querySelector('canvas') as HTMLElement | null
  const cs = vp ? getComputedStyle(vp) : null
  return {
    clientId: e.clientId,
    title: e.title,
    kind: e.kind,
    rows: t.rows,
    cols: t.cols,
    bufferLength: b.length,
    baseY: b.baseY,
    viewportY: b.viewportY,
    cursorX: b.cursorX,
    cursorY: b.cursorY,
    scrollbackOption: t.options.scrollback,
    firstLines: readLines(e, 0, 6).lines,
    visibleText: readLines(e, b.viewportY, t.rows).text,
    // 诊断用聚合字段：回滚是否可达、视口是否在两端（"顶部截断"类问题一眼可判）
    hiddenAbove: b.baseY,
    atTop: b.viewportY === 0,
    atBottom: b.viewportY >= b.baseY,
    scrollable: (() => {
      const vpEl = el?.querySelector('.xterm-viewport') as HTMLElement | null
      return vpEl ? vpEl.scrollHeight > vpEl.clientHeight : null
    })(),
    geometry: el
      ? {
          container: el.clientHeight,
          viewport: vp?.clientHeight ?? null,
          viewportScrollH: vp?.scrollHeight ?? null,
          viewportScrollTop: vp?.scrollTop ?? null,
          screen: sc?.offsetHeight ?? null,
          scrollArea: sa?.offsetHeight ?? null,
          canvasH: cv?.offsetHeight ?? null,
          canvasW: cv?.offsetWidth ?? null,
          insetTop: cs?.top ?? null,
          insetBottom: cs?.bottom ?? null,
        }
      : null,
  }
}

function requireTerminal(id: unknown): DebugTerminalEntry {
  const e = getDebugTerminal(str(id))
  if (!e) throw new Error(`未找到终端: ${str(id) || '(空 id)'}`)
  return e
}

async function handle(req: DebugRequest): Promise<unknown> {
  const args = req.args ?? {}
  const sessions = useSessionsStore()
  const ui = useUIStore()

  switch (req.op) {
    case 'state': {
      const settings = useSettingsStore()
      return {
        view: ui.view,
        activeId: sessions.activeId,
        splitShown: sessions.splitShown,
        tabs: sessions.tabs.map((t) => ({
          clientId: t.clientId,
          name: t.serverName,
          status: t.status,
          kind: t.kind,
          sessionId: t.sessionId ?? '',
          sftpPath: t.sftpPath ?? '',
        })),
        terminalIds: listDebugTerminals().map((e) => e.clientId),
        settings: {
          uiScale: settings.uiScale,
          webGLEnabled: settings.webGLEnabled,
          tabBarPlacement: settings.tabBarPlacement,
          navSectionOrder: settings.navSectionOrder,
          rightClickAction: settings.rightClickAction,
          terminalFontSize: settings.fonts.terminalFontSize,
        },
      }
    }

    case 'tabs':
      return sessions.tabs.map((t) => ({
        clientId: t.clientId,
        name: t.serverName,
        status: t.status,
        kind: t.kind,
        sessionId: t.sessionId ?? '',
        active: t.clientId === sessions.activeId,
      }))

    case 'terminals':
      return listDebugTerminals().map(terminalState)

    case 'terminal.buffer':
      return readLines(requireTerminal(args.id), args.from, args.lines)

    case 'terminal.input': {
      const e = requireTerminal(args.id)
      const b64 = str(args.base64)
      const data = b64 ? atob(b64) : str(args.data)
      if (!data) throw new Error('缺少 data / base64')
      e.write(data)
      return {ok: true, bytes: data.length}
    }

    case 'terminal.scroll': {
      const e = requireTerminal(args.id)
      const to = str(args.to, 'bottom')
      const value = num(args.value)
      if (to === 'top') e.term.scrollToTop()
      else if (to === 'bottom') e.term.scrollToBottom()
      else if (to === 'line') e.term.scrollToLine(value)
      else if (to === 'lines') e.term.scrollLines(value)
      else throw new Error(`未知的滚动目标: ${to}（可用 top|bottom|line|lines）`)
      const b = e.term.buffer.active
      return {ok: true, viewportY: b.viewportY, baseY: b.baseY}
    }

    case 'terminal.resize': {
      const e = requireTerminal(args.id)
      const cols = Math.max(2, num(args.cols, e.term.cols))
      const rows = Math.max(1, num(args.rows, e.term.rows))
      e.term.resize(cols, rows)
      return {ok: true, cols: e.term.cols, rows: e.term.rows}
    }

    case 'session.reconnect': {
      const e = requireTerminal(args.id)
      e.reconnect()
      return {ok: true}
    }

    case 'tab.open': {
      ui.showWorkspace()
      if (args.local) {
        sessions.openLocalTab()
        return {ok: true, kind: 'local'}
      }
      // 优先用 serverId：由页面自己去 Go 侧取完整节点，凭据不经过 API 调用方
      let node = args.node as {id?: string; name?: string} | undefined
      const serverId = str(args.serverId)
      if (!node && serverId) {
        const api = (window as unknown as {go?: {main?: {App?: {GetServers?: () => Promise<{id?: string}[]>}}}}).go?.main?.App
        const list = (await api?.GetServers?.()) ?? []
        node = list.find((s) => s.id === serverId)
        if (!node) throw new Error(`未找到服务器: ${serverId}`)
      }
      if (!node || !node.id) throw new Error('tab.open 需要 serverId / node，或 local=true')
      sessions.openTab(node as never)
      return {ok: true, kind: 'ssh', node: node.id}
    }

    case 'tab.close': {
      const id = str(args.id)
      if (!id) throw new Error('缺少 id')
      sessions.closeTab(id)
      return {ok: true}
    }

    case 'eval': {
      const js = str(args.js)
      if (!js.trim()) throw new Error('缺少 js')
      // 调试用逃生口：默认由后端开关控制（设置 → 调试模式 → 允许执行 JS）
      let out = (0, eval)(js) // eslint-disable-line no-eval
      if (out && typeof (out as Promise<unknown>).then === 'function') out = await out
      return {result: out === undefined ? null : out}
    }

    default:
      throw new Error(`未知操作: ${req.op}`)
  }
}

/** 安装前端桥（应用挂载后调用一次）。 */
export function installDebugBridge(): void {
  EventsOn('debug:request', async (payload: DebugRequest) => {
    if (!payload || typeof payload.id !== 'string') return
    try {
      const data = await handle(payload)
      await DebugReply(payload.id, JSON.stringify({ok: true, data: data ?? null}))
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      try {
        await DebugReply(payload.id, JSON.stringify({ok: false, error: msg}))
      } catch {
        /* 回包失败时无补救手段，交给后端超时 */
      }
    }
  })
}
