<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import {WebglAddon} from '@xterm/addon-webgl'
import {
  acceptSuffix,
  completionPrefix,
  extractScreenWords,
  mergeSuggestions,
  type Suggestion,
} from '../completion/engine'
import {
  DEFAULT_COMPLETION_NAV_HOTKEY,
  formatHotkeyLabel,
  matchHotkey,
} from '../completion/hotkey'
import {
  base64ToBytes,
  onSessionOutput,
  onSessionProgress,
  onSessionStatus,
  onSftpSyncPath,
  reconnect,
  sshService,
} from '../services/ssh'
import {historyService} from '../services/history'
import {sysInfoService} from '../services/sysinfo'
import {attachZmodem, type ZmodemController, type ZmodemProgress} from '../services/zmodem'
import Icon from './Icon.vue'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import {monoFontStack} from '../theme/engine'
import type {SessionTab} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()

const container = ref<HTMLElement>()
const disconnectedPanel = ref<HTMLElement | null>(null)
const menu = ref<{x: number; y: number} | null>(null)
const fontSize = ref(settings.fonts.terminalFontSize || 13)
const suggestions = ref<Suggestion[]>([])
const selectedIdx = ref(0)
/** 是否已进入补全导航（自定义热键 / 鼠标悬停）；未导航时不拦截 Tab/↑↓/Enter */
const navigating = ref(false)
const panelPos = ref<{left: number; top: number} | null>(null)
const panelEl = ref<HTMLElement | null>(null)
const zmodemMsg = ref('')
const zmodemProgress = ref<ZmodemProgress | null>(null)
const webglFallbackToast = ref('')

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let webglAddon: WebglAddon | null = null
let zmodem: ZmodemController | null = null
let resizeObserver: ResizeObserver | null = null
const disposers: Array<() => void> = []
let disposed = false
let lineBuf = ''
let composing = false
let suggestTimer: ReturnType<typeof setTimeout> | null = null
let inputLocked = false // Zmodem 进行中挂起键盘
let autoReconnectTimer: ReturnType<typeof setTimeout> | null = null
let autoReconnectAttempt = 0
const autoReconnectCountdown = ref(0)
let countdownTimer: ReturnType<typeof setInterval> | null = null
/** trackLineInput 解析 ANSI/方向键转义，避免 [A [B 写入历史 */
type EscState = 'none' | 'esc' | 'csi' | 'osc'
let escState: EscState = 'none'

function sanitizeHistoryCommand(cmd: string): string {
  let s = cmd
    .replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '')
    .replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?/g, '')
    .replace(/\x1b./g, '')
    .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f]/g, '')
    .replace(/^(\[[A-D]|O[A-D])+$/g, '')
    .trim()
  if (looksLikeCommandPollution(s)) {
    s = trimPollutedCommand(s)
  }
  s = s
    .split(/\s+/)
    .filter(Boolean)
    .join(' ')
  // 仍明显污染则丢弃，避免脏历史进面板
  if (looksLikeCommandPollution(s) || s.length > 300) return ''
  return s
}

function currentUIZoom(): number {
  const n = (settings.uiScale || 100) / 100
  return n > 0 ? n : 1
}

function xtermCore(t: Terminal): {
  _renderService?: {dimensions?: {css?: {cell?: {width: number; height: number}}}}
} | undefined {
  return (t as unknown as {_core?: {
    _renderService?: {dimensions?: {css?: {cell?: {width: number; height: number}}}}
  }})._core
}

function hexToRgba(hex: string, alpha: number): string {
  const m = hex.replace('#', '')
  const full = m.length === 3 ? m.split('').map((c) => c + c).join('') : m
  const n = parseInt(full, 16)
  if (Number.isNaN(n)) return hex
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}

const bgImageStyle = computed(() => {
  const t = settings.theme
  if (!t.bgImage) return {}
  return {
    backgroundImage: `url("${t.bgImage}")`,
    filter: t.blurAmount > 0 ? `blur(${t.blurAmount}px) brightness(0.85)` : 'brightness(0.85)',
    transform: 'scale(1.06)',
  }
})

const terminalStyle = computed(() => {
  const z = currentUIZoom()
  return {
    '--xterm-text-shadow': settings.theme.textShadow
      ? `0 1px 3px rgba(0, 0, 0, 0.8), 0 0 ${settings.theme.shadowBlur}px rgba(0, 0, 0, 0.5)`
      : 'none',
    // xterm 不支持位于 CSS zoom 祖先中：仅反向抵消全局缩放。
    // 父级 zoom 已改变布局视口，额外按 z 缩宽高会让画布再次缩小。
    zoom: 1 / z,
  }
})

const sourceLabel: Record<string, string> = {
  history: '历史',
  dict: '字典',
  screen: '屏幕',
}

function applyTheme() {
  if (!term) return
  const t = settings.theme
  const background = t.bgImage ? hexToRgba(t.background, 0.85) : t.background
  term.options.allowTransparency = !!t.bgImage
  term.options.theme = {
    background,
    foreground: t.foreground,
    cursor: t.cursor,
    cursorAccent: contrastText(t.cursor),
    selectionBackground: t.selection,
    black: t.black,
    red: t.red,
    green: t.green,
    yellow: t.yellow,
    blue: t.blue,
    magenta: t.magenta,
    cyan: t.cyan,
    white: t.white,
    brightBlack: t.brightBlack,
    brightRed: t.brightRed,
    brightGreen: t.brightGreen,
    brightYellow: t.brightYellow,
    brightBlue: t.brightBlue,
    brightMagenta: t.brightMagenta,
    brightCyan: t.brightCyan,
    brightWhite: t.brightWhite,
  }
  term.refresh(0, term.rows - 1)
}

// 根据光标颜色亮度选择光标上的文字色
function contrastText(hex: string): string {
  const h = (hex || '').trim().replace('#', '')
  const n = parseInt(h.length === 3 ? h.split('').map((c) => c + c).join('') : h, 16)
  if (Number.isNaN(n)) return '#ffffff'
  const r = (n >> 16) & 255
  const g = (n >> 8) & 255
  const b = n & 255
  const lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255
  return lum > 0.55 ? '#0c1016' : '#ffffff'
}

function applyFontSettings() {
  if (!term) return
  term.options.fontFamily = monoFontStack(settings.fonts.terminalFont)
  term.options.fontSize = effectiveFontSize()
  fit()
}

function tryEnableWebGL() {
  if (!term || !settings.webGLEnabled) return
  try {
    webglAddon?.dispose()
    webglAddon = new WebglAddon()
    webglAddon.onContextLoss(() => {
      webglAddon?.dispose()
      webglAddon = null
      webglFallbackToast.value = 'WebGL 上下文丢失，已降级 Canvas 渲染'
      setTimeout(() => {
        webglFallbackToast.value = ''
      }, 4000)
    })
    term.loadAddon(webglAddon)
  } catch {
    webglAddon = null
    webglFallbackToast.value = '当前环境不支持 WebGL，已降级 Canvas 渲染'
    setTimeout(() => {
      webglFallbackToast.value = ''
    }, 4000)
  }
}

function disableWebGL() {
  webglAddon?.dispose()
  webglAddon = null
}

function fit() {
  if (!term || !fitAddon || disposed) return
  fitAddon.fit()
  if (props.tab.sessionId) {
    sshService.resize(props.tab.sessionId, term.cols, term.rows).catch(() => {})
  }
}

function effectiveFontSize(): number {
  return Math.max(8, Math.min(40, Math.round(fontSize.value * currentUIZoom() * 10) / 10))
}

function applyFontSize() {
  if (!term) return
  term.options.fontSize = effectiveFontSize()
  fit()
}

function adjustFontSize(delta: number) {
  if (!term) return
  fontSize.value = Math.max(8, Math.min(32, fontSize.value + delta))
  applyFontSize()
  persistFontSize()
}

function persistFontSize() {
  if (settings.fonts.terminalFontSize === fontSize.value) return
  void settings.setFonts({...settings.fonts, terminalFontSize: fontSize.value})
}

function toggleFullscreen() {
  const el = container.value?.closest('.terminal-bg') as HTMLElement | null
  if (!el) return
  if (document.fullscreenElement) {
    document.exitFullscreen()
  } else {
    el.requestFullscreen()
  }
}

function openMenu(e: MouseEvent) {
  e.preventDefault()
  menu.value = {x: Math.min(e.clientX, window.innerWidth - 160), y: Math.min(e.clientY, window.innerHeight - 160)}
}

function closeMenu() {
  menu.value = null
}

function doCopy() {
  if (!term) return
  const sel = term.getSelection()
  if (sel) ClipboardSetText(sel)
  closeMenu()
}

function doPaste() {
  closeMenu()
  navigator.clipboard.readText().then((text) => {
    if (text && props.tab.sessionId) {
      sshService.write(props.tab.sessionId, btoa(text)).catch(() => {})
    }
  }).catch(() => {})
}

function doClear() {
  term?.clear()
  closeMenu()
}

function doSelectAll() {
  term?.selectAll()
  closeMenu()
}

function hideSuggestions() {
  suggestions.value = []
  panelPos.value = null
  selectedIdx.value = 0
  navigating.value = false
}

function enterNavigation(idx = 0) {
  if (!suggestions.value.length) return
  navigating.value = true
  selectedIdx.value = Math.max(0, Math.min(idx, suggestions.value.length - 1))
}

/**
 * 补全快捷键。返回 false = 拦截不交给终端；true = 放行。
 * 未导航：↑↓/Tab 放行，Enter 关面板后放行执行；自定义热键切换导航。
 * 已导航：↑↓ 切换，Tab/Enter 只采纳插入不执行；同一热键退出导航。
 */
function handleCompletionKey(e: KeyboardEvent): boolean {
  if (!suggestions.value.length) return true

  if (e.key === 'Escape') {
    hideSuggestions()
    return false
  }

  const hotkey = settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY
  if (matchHotkey(e, hotkey)) {
    if (navigating.value) {
      navigating.value = false
    } else {
      enterNavigation(0)
    }
    return false
  }

  if (navigating.value) {
    if (e.key === 'ArrowDown') {
      selectedIdx.value = (selectedIdx.value + 1) % suggestions.value.length
      return false
    }
    if (e.key === 'ArrowUp') {
      selectedIdx.value = (selectedIdx.value - 1 + suggestions.value.length) % suggestions.value.length
      return false
    }
    if (e.key === 'Tab') {
      acceptSuggestion()
      return false
    }
    if (e.key === 'Enter') {
      acceptSuggestion()
      return false
    }
    return true
  }

  // 未导航：Enter 关面板并放行执行；其余快捷键不拦截
  if (e.key === 'Enter') {
    hideSuggestions()
    return true
  }
  return true
}

async function updatePanelPosition() {
  if (!term || !container.value) return
  const dims = xtermCore(term)?._renderService?.dimensions?.css?.cell
  const cellW = dims?.width ?? 8
  const cellH = dims?.height ?? 16
  const buf = term.buffer.active
  const rect = container.value.getBoundingClientRect()
  const left = Math.min(Math.max(8, rect.left + buf.cursorX * cellW), window.innerWidth - 320)
  // 先占位再量真实高度，保证面板底边在光标行上方
  const cursorLineTop = rect.top + buf.cursorY * cellH
  const gap = 10
  panelPos.value = {left, top: Math.max(8, cursorLineTop - 180)}
  await nextTick()
  const h = panelEl.value?.offsetHeight ?? Math.min(240, 36 + suggestions.value.length * 30)
  let top = cursorLineTop - gap - h
  if (top < 8) {
    // 上方不够：放到光标行下方，额外留出一行高度
    top = cursorLineTop + cellH + gap
  }
  panelPos.value = {
    left,
    top: Math.max(8, Math.min(top, window.innerHeight - h - 8)),
  }
}

const PROMPT_MARKERS = ['# ', '$ ', '% ', '> ']

/** 读取 buffer 某一绝对行的可见文本。 */
function readLineTextAt(absY: number): string {
  if (!term) return ''
  const line = term.buffer.active.getLine(absY)
  if (!line) return ''
  let text = ''
  for (let i = 0; i < line.length; i++) {
    const cell = line.getCell(i)
    if (!cell) continue
    text += cell.getChars() || (cell.getWidth() ? ' ' : '')
  }
  return text.replace(/\s+$/, '')
}

function findPromptCut(line: string): number {
  let cut = -1
  for (const m of PROMPT_MARKERS) {
    const i = line.lastIndexOf(m)
    if (i >= 0) cut = Math.max(cut, i + m.length)
  }
  return cut
}

/** 是否把命令输出/二次 prompt 粘进了「命令」串。 */
function looksLikeCommandPollution(cmd: string): boolean {
  if (!cmd) return false
  if (cmd.length > 400) return true
  if (/warning\s*:/i.test(cmd)) return true
  if (/\bNAME\s+STATUS\b/.test(cmd)) return true
  if (/\bReady\b.*\b(?:control-plane|master|worker)\b/i.test(cmd)) return true
  // kubectl get node...root@host:~#
  if (/[a-z0-9](?:warning|NAME|STATUS|Ready|error:)/i.test(cmd)) return true
  if (/[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:/.test(cmd) && /[#$]/.test(cmd)) return true
  return false
}

/** 截到输出污染起点，尽量保留纯命令。 */
function trimPollutedCommand(cmd: string): string {
  if (!cmd) return ''
  const idxs = [
    cmd.search(/warning\s*:/i),
    cmd.search(/\bNAME\s+STATUS\b/),
    cmd.search(/[a-z0-9](?=warning\s*:)/i),
    cmd.search(/[a-z0-9](?=NAME\s+STATUS)/),
    cmd.search(/[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:[~\/]/),
  ].filter((i) => i > 0)
  let s = idxs.length ? cmd.slice(0, Math.min(...idxs)) : cmd
  return s.replace(/\s+$/, '').trim()
}

/**
 * 只读当前输入逻辑行：沿 xterm soft-wrap（isWrapped）向上拼。
 * 禁止跨过输出区拼到上一条 prompt（否则会变成 kubectl…NAME…root@…）。
 */
function readScreenCommand(): {cmd: string; viaPrompt: boolean} {
  if (!term) return {cmd: '', viaPrompt: false}
  const buf = term.buffer.active
  const curY = buf.baseY + buf.cursorY

  let startY = curY
  while (startY > 0) {
    const line = buf.getLine(startY)
    if (!line?.isWrapped) break
    startY--
  }

  let text = ''
  for (let y = startY; y <= curY; y++) {
    text += readLineTextAt(y)
  }
  text = text.replace(/\s+$/, '')

  const cut = findPromptCut(text)
  if (cut >= 0) {
    return {cmd: text.slice(cut).replace(/^\s+/, '').replace(/\s+$/, ''), viaPrompt: true}
  }
  return {cmd: text.replace(/^\s+/, ''), viaPrompt: false}
}

/** tracked 去空白后是否为 screen 子序列（Tab 补全后 lineBuf 缺 ctl 等字符）。 */
function isLooseSubsequence(tracked: string, screen: string): boolean {
  const a = tracked.replace(/\s+/g, '')
  const b = screen.replace(/\s+/g, '')
  if (!a || !b || a.length > b.length) return false
  let i = 0
  for (const ch of b) {
    if (ch === a[i]) i++
    if (i >= a.length) return true
  }
  return false
}

/**
 * 解析即将执行的命令。
 * 屏上（去 prompt / 软折行）优先；污染则截断或回退 lineBuf。
 */
function resolveExecutedCommand(): string {
  const trackedRaw = lineBuf
  const tracked = looksLikeCommandPollution(trackedRaw) ? trimPollutedCommand(trackedRaw) : trackedRaw
  let {cmd: fromScreen, viaPrompt} = readScreenCommand()
  if (looksLikeCommandPollution(fromScreen)) {
    fromScreen = trimPollutedCommand(fromScreen)
  }

  if (!fromScreen) return tracked
  if (!tracked) return fromScreen
  if (fromScreen === tracked) return fromScreen

  if (fromScreen.includes(tracked) && !looksLikeCommandPollution(fromScreen)) return fromScreen
  if (tracked.includes(fromScreen) && tracked.length > fromScreen.length + 2) return tracked
  if (isLooseSubsequence(tracked, fromScreen) && fromScreen.length >= tracked.length) {
    return fromScreen
  }
  if (viaPrompt) return fromScreen
  if (fromScreen.length > tracked.length) return fromScreen
  return tracked
}

let lineBufSyncTimer: ReturnType<typeof setTimeout> | null = null
/** Tab / ↑↓ 后延时从屏上同步 lineBuf，避免缺字与补全面板前缀错位。 */
function scheduleLineBufSyncFromScreen() {
  if (lineBufSyncTimer) clearTimeout(lineBufSyncTimer)
  lineBufSyncTimer = setTimeout(() => {
    lineBufSyncTimer = null
    if (!term || props.tab.status !== 'connected') return
    const {cmd} = readScreenCommand()
    // 命令执行后输出刷屏时勿把输出并进 lineBuf
    if (!cmd || looksLikeCommandPollution(cmd)) return
    lineBuf = cmd
    if (settings.completionEnabled) scheduleSuggest()
  }, 40)
}

async function refreshSuggestions() {
  if (!settings.completionEnabled || !term || composing || props.tab.status !== 'connected') {
    hideSuggestions()
    return
  }
  const {full, token} = completionPrefix(lineBuf)
  const query = full.length >= 1 ? full : token
  if (!query || query.length < 1) {
    hideSuggestions()
    return
  }
  const limit = Math.max(3, Math.min(30, settings.completionPanelLimit || 8))
  const hist = await historyService.query(props.tab.node.id, full || token, limit)
  const screen = extractScreenWords(term)
  // 历史整行；屏幕为路径/Pod 等 token；字典按末 token
  const merged = mergeSuggestions(
    token || full,
    hist.map((h) => ({command: h.command, source: 'history' as const, count: h.count})),
    screen,
    limit,
  )
  if (merged.length === 0) {
    hideSuggestions()
    return
  }
  suggestions.value = merged
  // 输入变化导致列表刷新时退出导航，避免误拦 Tab/历史键
  navigating.value = false
  selectedIdx.value = 0
  await updatePanelPosition()
}

function scheduleSuggest() {
  if (suggestTimer) clearTimeout(suggestTimer)
  suggestTimer = setTimeout(() => {
    void refreshSuggestions()
  }, 60)
}

function trackLineInput(data: string) {
  for (const ch of data) {
    // 过滤 CSI / OSC / 短 ESC 序列（↑↓ 等方向键）
    if (escState === 'none' && ch === '\x1b') {
      escState = 'esc'
      continue
    }
    if (escState === 'esc') {
      if (ch === '[') {
        escState = 'csi'
        continue
      }
      if (ch === ']') {
        escState = 'osc'
        continue
      }
      // SS3：ESC O A/B/C/D
      if (ch === 'O') {
        escState = 'csi'
        continue
      }
      escState = 'none'
      continue
    }
    if (escState === 'csi') {
      if (ch >= '\x40' && ch <= '\x7e') {
        escState = 'none'
        // shell 历史 ↑↓（含 ESC [ A / ESC O A）：屏上已替换整行，同步 lineBuf
        if (ch === 'A' || ch === 'B') scheduleLineBufSyncFromScreen()
      }
      continue
    }
    if (escState === 'osc') {
      if (ch === '\x07') {
        escState = 'none'
        continue
      }
      if (ch === '\x1b') {
        escState = 'esc'
        continue
      }
      continue
    }

    if (ch === '\r' || ch === '\n') {
      // 取消待执行的屏合同步，避免回车后输出刷屏把 lineBuf 污染
      if (lineBufSyncTimer) {
        clearTimeout(lineBufSyncTimer)
        lineBufSyncTimer = null
      }
      const cmd = sanitizeHistoryCommand(resolveExecutedCommand())
      if (cmd) historyService.add(props.tab.node.id, cmd)
      lineBuf = ''
      escState = 'none'
      hideSuggestions()
      continue
    }
    if (ch === '\x7f' || ch === '\b') {
      lineBuf = lineBuf.slice(0, -1)
      continue
    }
    if (ch === '\u0015') {
      // Ctrl+U
      lineBuf = ''
      continue
    }
    if (ch === '\u0003') {
      // Ctrl+C
      lineBuf = ''
      escState = 'none'
      hideSuggestions()
      continue
    }
    if (ch === '\t') {
      // shell Tab 补全只反映在回显里，延时从屏上拉回完整行
      scheduleLineBufSyncFromScreen()
      continue
    }
    if (ch >= ' ') lineBuf += ch
  }
}

function acceptSuggestion(idx = selectedIdx.value) {
  const item = suggestions.value[idx]
  if (!item || !props.tab.sessionId || !term) return
  let suffix: string
  if (item.source === 'history') {
    // 历史为整行：Ctrl+U 清行再写入（比按 lineBuf 长度退格更稳，避免 Tab 后屏上更长）
    suffix = '\u0015' + item.command
    lineBuf = item.command
  } else {
    suffix = acceptSuffix(lineBuf, item.command)
    if (suffix.startsWith('\x7f')) {
      const backs = suffix.match(/^\x7f+/)?.[0].length ?? 0
      lineBuf = lineBuf.slice(0, Math.max(0, lineBuf.length - backs)) + suffix.slice(backs)
    } else {
      lineBuf += suffix
    }
  }
  hideSuggestions()
  writeRaw(suffix)
  // 等 shell 回显后校正（退格/宽字符极端情况）
  scheduleLineBufSyncFromScreen()
}

function onKeydown(e: KeyboardEvent) {
  if (e.isComposing || e.keyCode === 229) {
    composing = true
    return
  }
  composing = false

  if (e.key === 'Escape' && menu.value && !suggestions.value.length) {
    menu.value = null
    e.preventDefault()
    return
  }

  if (suggestions.value.length > 0) {
    const pass = handleCompletionKey(e)
    if (!pass) {
      e.preventDefault()
      e.stopPropagation()
      return
    }
    // Enter 未导航：已关面板，继续放行给终端
  }

  if (e.key === 'f' && (e.ctrlKey || e.metaKey)) return
  if ((e.ctrlKey || e.metaKey) && (e.key === '=' || e.key === '+')) {
    e.preventDefault()
    adjustFontSize(1)
  }
  if ((e.ctrlKey || e.metaKey) && e.key === '-') {
    e.preventDefault()
    adjustFontSize(-1)
  }
  if ((e.ctrlKey || e.metaKey) && e.key === '0') {
    e.preventDefault()
    fontSize.value = 13
    applyFontSize()
    persistFontSize()
  }
  if (e.key === 'F11') {
    e.preventDefault()
    toggleFullscreen()
  }
}

async function setupZmodem(sessionId: string) {
  if (!term) return
  zmodem?.dispose()
  zmodem = null
  try {
    zmodem = await attachZmodem({
      term,
      sessionId,
      onProgress: (p) => {
        zmodemProgress.value = p
        if (p.done) {
          // 进度完成不直接解锁：由 onActiveChange 统一管理，避免握手未结束时误放行
          setTimeout(() => {
            zmodemProgress.value = null
          }, 2500)
        }
      },
      onActiveChange: (active) => {
        inputLocked = active
        if (!active) {
          // 确保 xterm 重新获得键盘焦点
          try {
            term?.focus()
          } catch {
            /* ignore */
          }
        }
      },
      onStatus: (msg) => {
        zmodemMsg.value = msg
        setTimeout(() => {
          zmodemMsg.value = ''
        }, 4000)
      },
    })
  } catch {
    zmodem = null
  }
}

async function connect(): Promise<boolean> {
  cancelAutoReconnect()
  disposers.forEach((d) => d())
  disposers.length = 0
  sessions.setStatus(props.tab.clientId, 'connecting')
  resetSteps()
  hideSuggestions()
  lineBuf = ''
  escState = 'none'
  if (lineBufSyncTimer) {
    clearTimeout(lineBufSyncTimer)
    lineBufSyncTimer = null
  }

  const sid = props.tab.clientId
  props.tab.sessionId = sid
  term?.reset()

  disposers.push(
    onSessionProgress(sid, (evt) => {
      const st = steps[evt.step]
      if (!st) return
      if (evt.log) {
        st.logs.push(evt.log)
        st.expanded = true
      }
      if (evt.status) {
        st.status = evt.status as StepStatus
        if (evt.message) st.message = evt.message
        // 进行中/失败自动展开，方便实时查看与定位卡住原因
        if (evt.status === 'running' || evt.status === 'error') st.expanded = true
      }
    }),
  )
  disposers.push(
    onSessionStatus(sid, (evt) => {
      sessions.setStatus(props.tab.clientId, evt.status, evt.message)
      if (evt.status !== 'connected') hideSuggestions()
      if (evt.status === 'disconnected' && settings.autoReconnect && !disposed) {
        scheduleAutoReconnect()
      }
    }),
  )
  // 终端→SFTP 目录同步：监听挂在终端上（会话存活期间始终在），避免 SFTP 面板 v-if 拆装漏事件
  disposers.push(
    onSftpSyncPath(sid, (newPath) => {
      if (!settings.terminalToSftpSync) {
        console.info('[sftp-sync] 开关已关，忽略', newPath)
        return
      }
      if (!newPath) {
        console.info('[sftp-sync] 空路径，忽略')
        return
      }
      if (newPath === props.tab.sftpPath) {
        console.info('[sftp-sync] 已在该目录，忽略', newPath)
        return
      }
      console.info('[sftp-sync] 写入 sftpPath', props.tab.sftpPath, '→', newPath)
      sessions.setSftpPath(props.tab.clientId, newPath)
    }),
  )

  try {
    const isLocal = props.tab.kind === 'local'
    const result = isLocal
      ? await sshService.connectLocal(sid, term?.cols ?? 80, term?.rows ?? 24)
      : await sshService.connect(sid, props.tab.node, term?.cols ?? 80, term?.rows ?? 24)
    if (disposed) {
      sshService.disconnect(result.sessionId).catch(() => {})
      return false
    }
    sessions.bindSession(props.tab.clientId, result.sessionId)
    sessions.setStatus(props.tab.clientId, 'connected')
    if (!isLocal) {
      await setupZmodem(result.sessionId)
    }
    disposers.push(
      onSessionOutput(result.sessionId, (data) => {
        if (disposed || !term) return
        const bytes = base64ToBytes(data)
        if (zmodem) zmodem.consume(bytes)
        else term.write(bytes)
      }),
    )
    // 本机会话无远程 SFTP / 系统监控
    if (!isLocal) {
      void sysInfoService.start(result.sessionId).catch(() => {})
      disposers.push(() => {
        void sysInfoService.stop(result.sessionId).catch(() => {})
      })
    }
    fit()
  } catch (e) {
    sessions.setStatus(props.tab.clientId, 'error', String(e))
    return false
  }
  return true
}

function cancelAutoReconnect() {
  if (autoReconnectTimer !== null) {
    clearTimeout(autoReconnectTimer)
    autoReconnectTimer = null
  }
  if (countdownTimer !== null) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
  autoReconnectCountdown.value = 0
  autoReconnectAttempt = 0
}

const AUTO_RECONNECT_DELAYS = [2000, 5000, 10000, 20000, 30000]

function scheduleAutoReconnect() {
  if (disposed || autoReconnectTimer !== null) return
  const delay = AUTO_RECONNECT_DELAYS[Math.min(autoReconnectAttempt, AUTO_RECONNECT_DELAYS.length - 1)]
  autoReconnectAttempt++

  // 启动倒计时显示
  autoReconnectCountdown.value = Math.round(delay / 1000)
  if (countdownTimer !== null) clearInterval(countdownTimer)
  countdownTimer = setInterval(() => {
    if (autoReconnectCountdown.value > 0) autoReconnectCountdown.value--
    else {
      clearInterval(countdownTimer!)
      countdownTimer = null
    }
  }, 1000)

  autoReconnectTimer = setTimeout(async () => {
    autoReconnectTimer = null
    if (countdownTimer !== null) {
      clearInterval(countdownTimer)
      countdownTimer = null
    }
    autoReconnectCountdown.value = 0
    if (disposed || !settings.autoReconnect) return
    if (props.tab.status !== 'disconnected') return
    term?.write(`\r\n\x1b[33m[自动重连] 第 ${autoReconnectAttempt} 次尝试…\x1b[0m\r\n`)
    const ok = await reconnectSession()
    if (ok) {
      autoReconnectAttempt = 0
    } else if (props.tab.status === 'disconnected' && settings.autoReconnect && !disposed) {
      scheduleAutoReconnect()
    }
  }, delay)
}

async function reconnectSession(): Promise<boolean> {
  if (!term) return false
  cancelAutoReconnect()
  const prevStatus = props.tab.status
  // 本机终端无 SSH Reconnect；closed/error 或本机会话一律走完整 connect
  if (props.tab.kind !== 'local' && prevStatus === 'disconnected' && props.tab.sessionId) {
    sessions.setStatus(props.tab.clientId, 'connecting')
    resetSteps()
    try {
      await reconnect(props.tab.sessionId, term.cols, term.rows)
      sessions.setStatus(props.tab.clientId, 'connected')
      autoReconnectAttempt = 0
      await setupZmodem(props.tab.sessionId)
      fit()
      return true
    } catch (e) {
      sessions.setStatus(props.tab.clientId, 'disconnected', String(e))
      return false
    }
  }
  return connect()
}

const CONNECT_STEPS: {key: string; label: string}[] = [
  {key: 'dns', label: 'DNS / 直连'},
  {key: 'tcp', label: 'TCP 握手'},
  {key: 'auth', label: 'SSH 鉴权'},
  {key: 'pty', label: '分配 PTY'},
  {key: 'ready', label: '会话就绪'},
]

type StepStatus = 'pending' | 'running' | 'done' | 'error'

interface StepState {
  status: StepStatus
  logs: string[]
  message: string
  expanded: boolean
}

function makeStepState(): StepState {
  return {status: 'pending', logs: [], message: '', expanded: false}
}

// 五步连接过程的实时状态与详细日志（后端按 step 推送增量事件）。
const steps = reactive<Record<string, StepState>>(
  Object.fromEntries(CONNECT_STEPS.map((s) => [s.key, makeStepState()])),
)

function resetSteps() {
  for (const s of CONNECT_STEPS) {
    steps[s.key] = makeStepState()
  }
}

function toggleStep(key: string) {
  steps[key].expanded = !steps[key].expanded
}

// 当前正在执行或已失败的步骤（用于顶部摘要）。
const activeStepKey = computed(() => {
  const found = CONNECT_STEPS.find((s) => {
    const st = steps[s.key].status
    return st === 'running' || st === 'error'
  })
  return found?.key ?? ''
})

const activeStepLabel = computed(
  () => CONNECT_STEPS.find((s) => s.key === activeStepKey.value)?.label ?? '',
)

const anyStepError = computed(() => CONNECT_STEPS.some((s) => steps[s.key].status === 'error'))

function closeTab() {
  sessions.closeTab(props.tab.clientId)
}

function writeRaw(data: string) {
  if (!props.tab.sessionId) return
  // btoa 仅支持 latin1；对 Unicode 用 encodeURIComponent 兜底
  try {
    void sshService.write(props.tab.sessionId, btoa(data)).catch(() => {})
  } catch {
    const bytes = new TextEncoder().encode(data)
    let bin = ''
    bytes.forEach((b) => {
      bin += String.fromCharCode(b)
    })
    void sshService.write(props.tab.sessionId, btoa(bin)).catch(() => {})
  }
}

onMounted(() => {
  term = new Terminal({
    allowProposedApi: true,
    allowTransparency: !!settings.theme.bgImage,
    cursorBlink: true,
    fontSize: effectiveFontSize(),
    fontFamily: monoFontStack(settings.fonts.terminalFont),
    theme: {
      background: settings.theme.background,
      foreground: settings.theme.foreground,
      cursor: settings.theme.cursor,
      cursorAccent: '#071210',
      selectionBackground: settings.theme.selection,
    },
    scrollback: 5000,
    // 关闭「用户输入时强制滚到底部」：浏览历史时按普通键/粘贴不再跳转；
    // 需要回到底部时按 Enter 即可（见 attachCustomKeyEventHandler）。
    scrollOnUserInput: false,
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(container.value!)
  tryEnableWebGL()
  applyFontSize()

  // 在 xterm 处理按键前拦截补全快捷键
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true
    if (e.isComposing || e.keyCode === 229) {
      composing = true
      return true
    }
    composing = false
    if (!handleCompletionKey(e)) return false
    // 浏览历史（视口不在最底部）时按回车：滚动回输入处，方便查看执行结果
    if (
      e.key === 'Enter' &&
      !e.ctrlKey &&
      !e.metaKey &&
      !e.altKey &&
      term &&
      term.buffer.active.baseY !== term.buffer.active.viewportY
    ) {
      term.scrollToBottom()
    }
    return true
  })

  term.onData((data) => {
    if (inputLocked || zmodem?.active()) return
    // 粘贴大段文本抑制补全
    if (data.length > 80) {
      hideSuggestions()
      lineBuf = ''
      escState = 'none'
      writeRaw(data)
      return
    }
    trackLineInput(data)
    writeRaw(data)
    if (settings.completionEnabled) scheduleSuggest()
  })

  term.onSelectionChange(() => {
    if (!settings.copyOnSelect) return
    const selection = term?.getSelection()
    if (selection) void ClipboardSetText(selection)
  })

  resizeObserver = new ResizeObserver(() => fit())
  resizeObserver.observe(container.value!)
  applyTheme()
  container.value?.addEventListener('keydown', onKeydown, true)
  container.value?.addEventListener('compositionstart', () => {
    composing = true
  })
  container.value?.addEventListener('compositionend', () => {
    composing = false
  })
  container.value?.addEventListener('contextmenu', openMenu)
  window.addEventListener('click', closeMenu)
  void connect()
})

watch(
  () => settings.theme,
  () => applyTheme(),
  {deep: true},
)

watch(
  () => settings.fonts,
  () => {
    fontSize.value = settings.fonts.terminalFontSize || 13
    applyFontSettings()
  },
  {deep: true},
)

watch(
  () => settings.loaded,
  (loaded) => {
    if (loaded) {
      fontSize.value = settings.fonts.terminalFontSize || 13
      applyTheme()
      applyFontSettings()
    }
  },
)

watch(
  () => settings.webGLEnabled,
  (enabled) => {
    if (enabled) tryEnableWebGL()
    else disableWebGL()
  },
)

watch(
  () => settings.completionEnabled,
  (enabled) => {
    if (!enabled) hideSuggestions()
  },
)

watch(
  () => props.tab.status,
  (status) => {
    if (status !== 'disconnected') return
    nextTick(() => disconnectedPanel.value?.focus())
  },
  {immediate: true},
)

watch(
  () => settings.uiScale,
  () => {
    if (!term || disposed) return
    nextTick(() => applyFontSize())
  },
)

onBeforeUnmount(() => {
  disposed = true
  cancelAutoReconnect()
  if (suggestTimer) clearTimeout(suggestTimer)
  if (lineBufSyncTimer) clearTimeout(lineBufSyncTimer)
  disposers.forEach((d) => d())
  resizeObserver?.disconnect()
  window.removeEventListener('click', closeMenu)
  zmodem?.dispose()
  disableWebGL()
  if (props.tab.sessionId) {
    void sshService.disconnect(props.tab.sessionId).catch(() => {})
  }
  term?.dispose()
  term = null
})
</script>

<template>
  <div class="relative h-full overflow-hidden terminal-theme" :style="terminalStyle">
    <div
      v-if="settings.theme.bgImage"
      class="absolute inset-0 bg-cover bg-center pointer-events-none"
      :style="bgImageStyle"
    ></div>

    <div
      ref="container"
      class="absolute inset-0"
      :class="tab.status === 'connected' ? '' : 'pointer-events-none'"
      tabindex="0"
      @focus.self
    ></div>

    <!-- 连接中 / 连接失败：可展开的分步日志面板（本机终端仅显示简要状态） -->
    <div
      v-if="tab.status === 'connecting' || tab.status === 'error'"
      class="absolute inset-0 z-30 grid place-items-center pointer-events-auto overlay-backdrop"
    >
      <div class="neo w-[min(460px,92%)] p-6 max-h-[86vh] overflow-y-auto">
        <div class="flex items-center justify-between gap-3 mb-1">
          <h3 class="text-[15px] font-semibold text-[var(--mist-100)] truncate">
            <template v-if="tab.kind === 'local'">
              {{ tab.status === 'connecting' ? `正在打开 ${tab.serverName}` : `打开 ${tab.serverName} 失败` }}
            </template>
            <template v-else>
              {{ tab.status === 'connecting' ? `正在连接 ${tab.serverName}` : `连接 ${tab.serverName} 失败` }}
            </template>
          </h3>
          <button class="btn-icon btn-sm shrink-0" title="关闭标签页" aria-label="关闭标签页" @click.stop="closeTab">
            <Icon name="close" :size="14" />
          </button>
        </div>
        <p v-if="tab.status === 'error' && tab.message" class="text-xs text-[#e57373] break-all mb-3">
          {{ tab.message }}
        </p>
        <template v-if="tab.kind === 'local'">
          <p class="text-xs text-mist mb-4">
            {{ tab.status === 'connecting' ? '正在启动本机 Shell…' : '可关闭后重试，或在设置中更换本机 Shell。' }}
          </p>
          <div class="flex gap-2">
            <button v-if="tab.status === 'error'" class="btn btn-primary btn-sm" @click="connect">重试</button>
            <button class="btn btn-ghost btn-sm" @click="closeTab">关闭</button>
          </div>
        </template>
        <template v-else>
        <p class="text-xs text-mist mb-4">
          <template v-if="activeStepLabel">
            当前：<span class="text-[var(--mist-100)]">{{ activeStepLabel }}</span>
            <span v-if="anyStepError" class="text-[#e57373]"> · 已失败</span>
          </template>
          <template v-else-if="tab.status === 'connecting'">解析主机 → 鉴权 → 打开 PTY → 同步环境</template>
        </p>

        <div class="conn-steps">
          <div
            v-for="(s, i) in CONNECT_STEPS"
            :key="s.key"
            class="conn-step"
            :class="[steps[s.key].status, {open: steps[s.key].expanded}]"
          >
            <button
              class="conn-step-head"
              @click="toggleStep(s.key)"
              :aria-expanded="steps[s.key].expanded"
            >
              <span class="n">
                <Icon v-if="steps[s.key].status === 'done'" name="check" :size="12" />
                <span v-else>{{ i + 1 }}</span>
              </span>
              <span class="flex-1 min-w-0 text-left truncate">{{ s.label }}</span>
              <span v-if="steps[s.key].status === 'running'" class="badge run">进行中</span>
              <span v-else-if="steps[s.key].status === 'error'" class="badge err">失败</span>
              <span v-else-if="steps[s.key].status === 'done'" class="badge done">完成</span>
              <Icon
                v-if="steps[s.key].logs.length"
                name="chevron-down"
                :size="14"
                extra-class="chev transition-transform"
                :class="steps[s.key].expanded ? 'open' : ''"
              />
            </button>
            <div v-if="steps[s.key].expanded && steps[s.key].logs.length" class="conn-step-logs">
              <p
                v-for="(l, j) in steps[s.key].logs"
                :key="j"
                class="log-line"
                :class="j === steps[s.key].logs.length - 1 && steps[s.key].status === 'error' ? 'err' : ''"
              >{{ l }}</p>
              <p v-if="steps[s.key].status === 'error' && steps[s.key].message" class="log-line err">
                {{ steps[s.key].message }}
              </p>
              <p v-else-if="steps[s.key].status === 'running'" class="log-line hint">等待该步骤完成…</p>
            </div>
          </div>
        </div>

        <div class="flex gap-2 mt-5">
          <template v-if="tab.status === 'connecting'">
            <button class="btn btn-ghost btn-sm" @click.stop="closeTab">关闭标签页</button>
          </template>
          <template v-else>
            <button class="btn btn-primary btn-sm" @click.stop="reconnectSession">重试连接</button>
            <button class="btn btn-ghost btn-sm" @click.stop="closeTab">关闭标签页</button>
          </template>
        </div>
        </template>
      </div>
    </div>

    <!-- 已断开 -->
    <div
      v-else-if="tab.status === 'closed' || tab.status === 'disconnected'"
      class="absolute inset-0 z-30 grid place-items-center pointer-events-auto overlay-backdrop"
      ref="disconnectedPanel"
      tabindex="0"
      @keydown.enter.prevent="reconnectSession"
    >
      <div class="neo w-[min(420px,90%)] p-6 text-center">
        <p class="text-sm text-[var(--mist-100)] break-all">
          {{ tab.message || '连接已断开' }}
        </p>
        <!-- 自动重连倒计时 -->
        <div
          v-if="tab.status === 'disconnected' && settings.autoReconnect && autoReconnectCountdown > 0"
          class="mt-3 text-xs text-amber-400/80 flex items-center justify-center gap-1.5"
        >
          <svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/>
          </svg>
          {{ autoReconnectAttempt > 1 ? `第 ${autoReconnectAttempt - 1} 次失败，` : '' }}{{ autoReconnectCountdown }}s 后自动重连…
        </div>
        <div class="flex justify-center gap-2 mt-5">
          <button class="btn btn-primary" @click.stop="reconnectSession">立即重连</button>
          <button
            v-if="tab.status === 'disconnected' && settings.autoReconnect && autoReconnectCountdown > 0"
            class="btn btn-ghost"
            @click.stop="cancelAutoReconnect"
          >取消自动重连</button>
          <button class="btn btn-ghost" @click.stop="closeTab">关闭标签页</button>
        </div>
      </div>
    </div>

    <!-- WebGL / Zmodem 状态条 -->
    <div
      v-if="webglFallbackToast || zmodemMsg || (zmodemProgress && !zmodemProgress.done)"
      class="absolute left-3 right-3 bottom-3 z-40 pointer-events-none flex flex-col gap-2 items-start"
    >
      <div
        v-if="webglFallbackToast"
        class="toast neo"
        style="min-width:auto"
      >
        {{ webglFallbackToast }}
      </div>
      <div v-if="zmodemMsg" class="toast neo ok" style="min-width:auto">
        {{ zmodemMsg }}
      </div>
      <div
        v-if="zmodemProgress && !zmodemProgress.done"
        class="neo w-full max-w-sm px-3 py-2 text-xs"
      >
        <div class="flex justify-between gap-2 mb-1">
          <span class="truncate">{{ zmodemProgress.direction === 'upload' ? '上传' : '下载' }} · {{ zmodemProgress.name }}</span>
          <span class="text-signal shrink-0">
            {{ zmodemProgress.total > 0 ? Math.min(100, Math.round((zmodemProgress.transferred / zmodemProgress.total) * 100)) : 0 }}%
          </span>
        </div>
        <div class="prog">
          <i
            :style="{
              width:
                zmodemProgress.total > 0
                  ? `${Math.min(100, (zmodemProgress.transferred / zmodemProgress.total) * 100)}%`
                  : '10%',
            }"
          ></i>
        </div>
      </div>
    </div>

    <!-- 智能补全面板 -->
    <Teleport to="body">
      <div
        v-if="suggestions.length && panelPos"
        ref="panelEl"
        class="fixed z-[60] min-w-[240px] max-w-[360px] neo p-1.5 text-xs completion-panel"
        :style="{left: panelPos.left + 'px', top: panelPos.top + 'px'}"
        @mousedown.prevent
      >
        <button
          v-for="(item, i) in suggestions"
          :key="item.source + ':' + item.command"
          class="w-full flex items-center gap-2 px-2.5 py-2 rounded-[6px] text-left font-mono"
          :class="navigating && i === selectedIdx ? 'bg-[var(--signal-weak)] shadow-[inset_0_0_0_1px_var(--signal-border)]' : 'hover:bg-[var(--hover)]'"
          @mouseenter="enterNavigation(i)"
          @click="acceptSuggestion(i)"
        >
          <span class="flex-1 truncate">{{ item.command }}</span>
          <span class="shrink-0 text-[12px] text-mist font-sans">
            {{ sourceLabel[item.source] || item.source }}
            <template v-if="item.count && item.count > 1"> · {{ item.count }}</template>
          </span>
        </button>
        <div class="flex gap-3 px-2.5 pt-1.5 mt-1 text-[11px] text-mist inset-line-t">
          <span v-if="navigating"><span class="kbd">↑↓</span> 选择 · <span class="kbd">Tab</span> 采纳 · <span class="kbd">Esc</span> 关闭</span>
          <span v-else><span class="kbd">{{ formatHotkeyLabel(settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY) }}</span> 进入 · <span class="kbd">Esc</span> 关闭</span>
        </div>
      </div>
    </Teleport>

    <!-- 右键菜单 -->
    <Teleport to="body">
      <div
        v-if="menu"
        class="menu-pop neo fixed z-50"
        :style="{left: menu.x + 'px', top: menu.y + 'px'}"
        @contextmenu.prevent
        @click.stop
      >
        <button @click="doCopy">复制</button>
        <button @click="doPaste">粘贴</button>
        <div class="divider-h my-1"></div>
        <button @click="doSelectAll">全选</button>
        <button @click="doClear">清除屏幕</button>
        <div class="divider-h my-1"></div>
        <button @click="adjustFontSize(1)">放大 <span class="text-mist">Ctrl+=</span></button>
        <button @click="adjustFontSize(-1)">缩小 <span class="text-mist">Ctrl+-</span></button>
        <button @click="toggleFullscreen">全屏 <span class="text-mist">F11</span></button>
      </div>
    </Teleport>
  </div>
</template>

<style>
.terminal-theme .xterm-rows {
  text-shadow: var(--xterm-text-shadow, none);
}

.completion-panel {
  animation: fadeRise 200ms cubic-bezier(0.22, 1, 0.36, 1);
}
</style>
