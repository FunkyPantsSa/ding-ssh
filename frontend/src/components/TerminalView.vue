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
import {currentZoom} from '../utils/dom'
import {patchXtermZoomCoords} from '../utils/xterm-zoom'
import {registerDebugTerminal, unregisterDebugTerminal, type DebugTerminalEntry} from '../debug/registry'
import Icon from './Icon.vue'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {ClipboardGetText, ClipboardSetText} from '../../wailsjs/runtime/runtime'
import {LogClient, SetLogTrace} from '../../wailsjs/go/main/App'
import {monoFontStack} from '../theme/engine'
import type {SessionTab} from '../types'

const props = defineProps<{tab: SessionTab; paneId?: string}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()
// 分屏中：该 TerminalView 所在格是否焦点格；未分屏：全局 activeId 是否命中
const isActiveTab = computed(() => {
  if (props.paneId) return sessions.focusedPaneId === props.paneId
  return sessions.activeId === props.tab.clientId
})

const container = ref<HTMLElement>()
const statusPanel = ref<HTMLElement | null>(null)
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
// 本实例在调试注册表里的那一份登记（注销时按身份匹配，见 onBeforeUnmount 的说明）
let debugEntry: DebugTerminalEntry | null = null
let disposed = false
let lineBuf = ''
let composing = false
let suggestTimer: ReturnType<typeof setTimeout> | null = null
let inputLocked = false // Zmodem 进行中挂起键盘
let autoReconnectTimer: ReturnType<typeof setTimeout> | null = null
let autoReconnectAttempt = 0
const autoReconnectCountdown = ref(0)
let countdownTimer: ReturnType<typeof setInterval> | null = null
let focusTimer: ReturnType<typeof setTimeout> | null = null
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

function xtermCore(t: Terminal): {
  _renderService?: {dimensions?: {css?: {cell?: {width: number; height: number}}}}
} | undefined {
  return (t as unknown as {_core?: {
    _renderService?: {dimensions?: {css?: {cell?: {width: number; height: number}}}}
  }})._core
}

/** 界面缩放比（app-shell 的 CSS zoom）；100% 时为 1。 */
function uiZoom(): number {
  const z = (settings.uiScale || 100) / 100
  return Number.isFinite(z) && z > 0 ? z : 1
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

const terminalStyle = computed(() => ({
  '--xterm-text-shadow': settings.theme.textShadow
    ? `0 1px 3px rgba(0, 0, 0, 0.8), 0 0 ${settings.theme.shadowBlur}px rgba(0, 0, 0, 0.5)`
    : 'none',
  // 终端容器底色跟随终端主题：xterm 的内边距区域也呈现终端背景色，
  // 避免浅色界面下终端四周露出一圈界面底色边框（终端主题与界面明暗相互独立）。
  background: settings.theme.background,
  // 不再反向抵消全局 zoom：app-shell 的 zoom 会整体等比缩放（含终端字号），
  // 若在此处设置 zoom: 1/z 会把 xterm 容器压缩 1/z，导致分屏格右侧/底部出现空白。
}))

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

/**
 * 是否允许使用 WebGL 渲染。
 *
 * 实测结论（调试模式 A/B/C 对照实验）：界面缩放走的是 `.app-shell { zoom }`，而
 * `@xterm/addon-webgl` 在 CSS zoom ≠ 1 时会把画布内容整体上移数行（缓冲区与 DOM 几何都正确，
 * 只是画出来的像素错位，且 `clearTextureAtlas()+refresh()` 无法纠正）。
 * zoom = 1 时 WebGL 正常。因此仅在缩放 100% 时启用 WebGL，其余情况回退 Canvas。
 */
function webglUsable(): boolean {
  return uiZoom() === 1
}

function tryEnableWebGL() {
  if (!term || !settings.webGLEnabled) return
  if (!webglUsable()) {
    disableWebGL()
    console.info('[webgl] 界面缩放 ≠ 100%，已使用 Canvas 渲染（WebGL 在 CSS zoom 下顶部会错位）')
    logClient('info', `[webgl] uiScale=${settings.uiScale} → Canvas 渲染（WebGL 在 CSS zoom 下会错位）`)
    return
  }
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
    logClient('debug', `[webgl] enabled (uiScale=${settings.uiScale}, dpr=${window.devicePixelRatio})`)
  } catch {
    webglAddon = null
    webglFallbackToast.value = '当前环境不支持 WebGL，已降级 Canvas 渲染'
    setTimeout(() => {
      webglFallbackToast.value = ''
    }, 4000)
  }
}

/** 上报一条渲染诊断日志到后端（失败静默忽略，绝不能影响终端初始化）。 */
function logClient(level: string, message: string) {
  try {
    void LogClient(level, message).catch(() => {})
  } catch {
    /* ignore：绑定缺失 / 非 Wails 环境 */
  }
}

/** 该会话是否已在「日志」页签开启逐条跟踪。 */
const logTracking = computed(() => settings.logTraceTabs.includes(props.tab.clientId))

/** 右键菜单：开始 / 停止跟踪此会话日志（切换后刷新设置里的追踪列表）。 */
async function toggleLogTrace() {
  const id = props.tab.clientId
  const next = !logTracking.value
  closeMenu()
  try {
    await SetLogTrace(id, next)
    await settings.load()
  } catch {
    /* ignore：绑定缺失时不影响终端使用 */
  }
  focusTerminal()
}

function disableWebGL() {
  webglAddon?.dispose()
  webglAddon = null
}

function focusActiveSurface() {
  if (disposed || !isActiveTab.value) return
  if (props.tab.status === 'connected') {
    container.value?.focus()
    term?.focus()
    return
  }
  if (props.tab.status === 'disconnected' || props.tab.status === 'closed') {
    // 先释放 xterm 隐藏输入框持有的键盘焦点，避免焦点残留在终端光标处
    term?.blur()
    disconnectedPanel.value?.focus()
    return
  }
  if (props.tab.status === 'connecting' || props.tab.status === 'error') {
    statusPanel.value?.focus()
  }
}

function scheduleFocusActiveSurface() {
  if (focusTimer !== null) {
    clearTimeout(focusTimer)
    focusTimer = null
  }
  focusTimer = setTimeout(() => {
    focusTimer = null
    void nextTick(() => focusActiveSurface())
  }, 0)
}

function fit() {
  if (!term || !fitAddon || disposed) return
  fitAddon.fit()
  if (props.tab.sessionId) {
    sshService.resize(props.tab.sessionId, term.cols, term.rows).catch(() => {})
  }
}

function effectiveFontSize(): number {
  return Math.max(8, Math.min(40, fontSize.value))
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

/** 是否可向终端写入（连接中 / 断开 / Zmodem 传输中一律拒绝）。 */
function canSendInput(): boolean {
  if (disposed || !term) return false
  if (props.tab.status !== 'connected') return false
  return !inputLocked && !zmodem?.active()
}

/** 把键盘焦点交回终端（xterm 隐藏输入框），避免菜单 / 剪贴板操作后焦点残留在别处。 */
function focusTerminal() {
  if (disposed || !term) return
  if (props.tab.status !== 'connected') return
  try {
    term.focus()
  } catch {
    /* ignore */
  }
}

/**
 * 读取剪贴板文本。
 * Windows WebView2 下 Wails 关闭了浏览器加速键，Ctrl+V 不会触发原生 paste 事件，
 * 因此优先走 Wails runtime（Go 侧直接读系统剪贴板），失败再退回浏览器 Clipboard API。
 */
async function readClipboardText(): Promise<string> {
  try {
    const text = await ClipboardGetText()
    if (typeof text === 'string' && text.length > 0) return text
  } catch {
    /* 忽略：dev 浏览器 / 权限受限时退回 Clipboard API */
  }
  try {
    return (await navigator.clipboard.readText()) ?? ''
  } catch {
    return ''
  }
}

/** 把文本按 xterm 原生粘贴通道写入会话（自动处理换行归一化与 bracketed paste）。 */
function pasteText(text: string) {
  if (!text || !canSendInput() || !term || !props.tab.sessionId) return
  closeMenu()
  // 与 xterm 的 Ctrl+V 一致：\r?\n → \r，并在 shell 开启 2004 模式时加括号包裹，
  // 使 vim / tmux / zsh 等能识别为「一次粘贴」而不是逐行回车执行。
  term.paste(text)
  focusTerminal()
}

async function pasteFromClipboard() {
  if (!canSendInput()) return
  const text = await readClipboardText()
  if (text) pasteText(text)
  focusTerminal()
}

/** 键盘粘贴组合键：Ctrl+V / Cmd+V（含 Shift）与 Shift+Insert。 */
function isPasteCombo(e: KeyboardEvent): boolean {
  if (e.altKey) return false
  if (e.key === 'Insert' && e.shiftKey) return true
  if ((e.key === 'v' || e.key === 'V') && (e.ctrlKey || e.metaKey)) return true
  return false
}

/** 键盘复制组合键：macOS Cmd+C / 其他平台 Ctrl+Shift+C（Ctrl+C 必须留给 SIGINT）。 */
function isCopyCombo(e: KeyboardEvent): boolean {
  if (e.key !== 'c' && e.key !== 'C') return false
  if (e.altKey) return false
  if (e.metaKey && !e.ctrlKey) return true
  return e.ctrlKey && e.shiftKey
}

function doCopySelection() {
  const sel = term?.getSelection()
  if (sel) void ClipboardSetText(sel)
}

/**
 * 原生 paste 事件（部分环境仍可用）：捕获阶段拦截，避免与 xterm 自身监听重复粘贴。
 */
function onPasteEvent(e: ClipboardEvent) {
  if (!canSendInput()) return
  const text = e.clipboardData?.getData('text/plain') ?? ''
  if (!text) return
  e.preventDefault()
  e.stopPropagation()
  pasteText(text)
}

function openMenu(e: MouseEvent) {
  e.preventDefault()
  // 「直接粘贴」模式：连接中才粘贴；断开 / 冻结态仍弹出菜单，保留复制 / 全屏等操作
  if (settings.rightClickAction === 'paste' && canSendInput()) {
    void pasteFromClipboard()
    return
  }
  menu.value = {x: Math.min(e.clientX, window.innerWidth - 160), y: Math.min(e.clientY, window.innerHeight - 160)}
}

function closeMenu() {
  menu.value = null
}

function doCopy() {
  doCopySelection()
  closeMenu()
  focusTerminal()
}

function doPaste() {
  void pasteFromClipboard()
}

function doClear() {
  term?.clear()
  closeMenu()
  focusTerminal()
}

function doSelectAll() {
  term?.selectAll()
  closeMenu()
  focusTerminal()
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
  // container 的 gBCR 相对 viewport（含 app-shell 的 zoom），而面板用绝对定位（未缩放坐标）。
  // 统一先折算回布局像素，cursor 位置与面板尺寸才不会随 uiScale 漂移。
  const z = currentZoom(container.value)
  const rect = container.value.getBoundingClientRect()
  const left = Math.min(Math.max(8, rect.left / z + buf.cursorX * cellW), window.innerWidth / z - 320)
  // 先占位再量真实高度，保证面板底边在光标行上方
  const cursorLineTop = rect.top / z + buf.cursorY * cellH
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
    top: Math.max(8, Math.min(top, window.innerHeight / z - h - 8)),
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

  // 断开/关闭冻结态：Enter 直接重连。此监听挂在终端容器上（capture），
  // 即使焦点残留在 xterm 隐藏输入框，回车也能触发重连而不会被终端吞掉。
  if (
    (props.tab.status === 'disconnected' || props.tab.status === 'closed') &&
    e.key === 'Enter' &&
    !e.ctrlKey &&
    !e.metaKey &&
    !e.altKey
  ) {
    e.preventDefault()
    e.stopPropagation()
    void reconnectSession()
    return
  }

  if (e.key === 'Escape' && menu.value && !suggestions.value.length) {
    menu.value = null
    e.preventDefault()
    return
  }

  // 剪贴板快捷键：WebView2 下 Wails 关闭了浏览器加速键，原生 Ctrl+V / Cmd+C 不会生效，
  // 这里在捕获阶段自行处理并阻止冒泡，避免 xterm 把 Ctrl+V 当成控制字符 \x16 发给远端。
  if (canSendInput() && isPasteCombo(e)) {
    e.preventDefault()
    e.stopPropagation()
    void pasteFromClipboard()
    return
  }
  if (isCopyCombo(e) && term?.hasSelection()) {
    e.preventDefault()
    e.stopPropagation()
    doCopySelection()
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

// 连接重入保护：并发的第二次 connect() 会在横幅已开始输出后执行 term.reset()，
// 把已经写入终端的那几行擦掉（现象：横幅顶部缺行、且缓冲区里没有回滚内容可查）。
let connectInFlight = false

/** 建立连接（带重入保护，见下）。 */
async function connect(keepBuffer = false): Promise<boolean> {
  if (connectInFlight) return false
  connectInFlight = true
  try {
    return await doConnect(keepBuffer)
  } finally {
    connectInFlight = false
  }
}

/**
 * 实际连接流程。
 * @param keepBuffer 自动/隐式重连时保留已有缓冲区（只追加一条分隔行）：避免 reset() 把已输出的
 *   登录横幅开头清掉，历史也仍留在回滚里可查。仅在用户显式重连/新会话时才清屏。
 */
async function doConnect(keepBuffer = false): Promise<boolean> {
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
  if (keepBuffer) {
    term?.write('\r\n\x1b[33m── 重新连接 ──\x1b[0m\r\n')
  } else {
    term?.reset()
  }

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

  // 输出监听必须在发起连接之前注册：远端登录横幅（/etc/motd、fastfetch 等）通常在
  // Connect 返回前就已回传，而 Wails 事件不缓存、无监听即丢弃 —— 订阅晚了会丢掉开头
  // 几行输出（表现为「终端顶部吞字符」，且滚屏也找不回来）。
  // 会话 id 由前端生成并传入后端（后端事件名、返回值都用它），因此可以提前订阅。
  disposers.push(
    onSessionOutput(sid, (data) => {
      if (disposed || !term) return
      const bytes = base64ToBytes(data)
      if (zmodem) zmodem.consume(bytes)
      else term.write(bytes)
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
  // 完整重连：保留已有缓冲区（只追加分隔行），避免清屏擦掉刚输出的横幅开头
  return connect(true)
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
  // 调试模式：把自己注册进终端注册表，供调试 API 读取 xterm 内部状态 / 输入 / 滚动。
  // 放在 WebGL / 字号 / 事件接线之前：后面任何一步抛错都不该造成「终端已挂载、注册表却为空」
  //（调试 API 的 list_terminals / terminal.* 全都依赖这份登记）。
  debugEntry = {
    clientId: props.tab.clientId,
    title: props.tab.serverName,
    kind: props.tab.kind ?? 'ssh',
    term,
    container: () => container.value ?? null,
    write: writeRaw,
    reconnect: () => {
      void reconnectSession()
    },
    close: closeTab,
  }
  registerDebugTerminal(debugEntry)
  patchXtermZoomCoords(term, uiZoom)
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
    // 非连接态（断开/关闭/失败）：画面冻结只读，忽略键盘输入，避免写入已死会话
    if (props.tab.status !== 'connected') return
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
  // 捕获阶段拦截原生粘贴（WebView2 关闭加速键时不会触发；浏览器 / 未来环境可用时避免与 xterm 重复写入）
  container.value?.addEventListener('paste', onPasteEvent, true)
  window.addEventListener('click', closeMenu)
  void connect()
  scheduleFocusActiveSurface()
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
  () => [isActiveTab.value, props.tab.status] as const,
  ([active]) => {
    if (!active) return
    scheduleFocusActiveSurface()
  },
  {immediate: true},
)

watch(
  () => settings.uiScale,
  () => {
    if (!term || disposed) return
    nextTick(() => {
      applyFontSize()
      // 缩放变化会改变 CSS zoom：zoom≠1 时 WebGL 会画偏，需要重新评估渲染器
      if (!settings.webGLEnabled) return
      if (webglAddon && !webglUsable()) disableWebGL()
      else if (!webglAddon && webglUsable()) tryEnableWebGL()
    })
  },
)

onBeforeUnmount(() => {
  disposed = true
  // 带上自己那一份登记的引用：只有它还是注册表里的当前项才注销，
  // 避免 HMR「先挂新的、后卸旧的」时把新实例的登记顺手删掉（那会让注册表重新变空）。
  if (debugEntry) unregisterDebugTerminal(props.tab.clientId, debugEntry)
  debugEntry = null
  if (focusTimer) clearTimeout(focusTimer)
  cancelAutoReconnect()
  if (suggestTimer) clearTimeout(suggestTimer)
  if (lineBufSyncTimer) clearTimeout(lineBufSyncTimer)
  disposers.forEach((d) => d())
  resizeObserver?.disconnect()
  container.value?.removeEventListener('paste', onPasteEvent, true)
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
      :class="tab.status === 'connected' ? '' : 'frozen-surface'"
      tabindex="0"
      @focus.self
    ></div>

    <!-- 连接中 / 连接失败：可展开的分步日志面板（本机终端仅显示简要状态） -->
    <div
      v-if="tab.status === 'connecting' || tab.status === 'error'"
      ref="statusPanel"
      class="absolute inset-0 z-30 grid place-items-center pointer-events-auto overlay-backdrop"
      tabindex="0"
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
            <button v-if="tab.status === 'error'" class="btn btn-primary btn-sm" @click="connect()">重试</button>
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

    <!-- 已断开 / 已关闭：顶部横幅（不遮屏，断开前的终端画面保留可回看/滚屏/复制） -->
    <div
      v-else-if="tab.status === 'closed' || tab.status === 'disconnected'"
      ref="disconnectedPanel"
      class="absolute inset-x-3 top-3 z-30 pointer-events-auto disconnect-banner flex items-center gap-3 px-4 py-2.5"
      tabindex="0"
      @keydown.enter.prevent="reconnectSession"
    >
      <span class="banner-accent" aria-hidden="true"></span>
      <div class="min-w-0 flex-1">
        <p class="text-[14px] font-semibold leading-snug truncate text-[var(--warn-500)]">
          <template v-if="tab.kind === 'local'">本机终端已结束</template>
          <template v-else>连接已断开</template>
          <span class="font-normal text-[12px] text-[var(--mist-200)]"> · {{ tab.serverName }}</span>
        </p>
        <p v-if="tab.message" class="mt-0.5 text-[11px] font-normal text-[var(--mist-300)] truncate">{{ tab.message }}</p>
        <!-- 自动重连倒计时 -->
        <p
          v-if="tab.status === 'disconnected' && settings.autoReconnect && autoReconnectCountdown > 0"
          class="mt-0.5 text-[11px] text-amber-400 flex items-center gap-1.5"
        >
          <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/>
          </svg>
          {{ autoReconnectAttempt > 1 ? `第 ${autoReconnectAttempt - 1} 次失败，` : '' }}{{ autoReconnectCountdown }}s 后自动重连…
        </p>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <button class="btn btn-primary btn-sm" @click.stop="reconnectSession">立即重连</button>
        <button
          v-if="tab.status === 'disconnected' && settings.autoReconnect && autoReconnectCountdown > 0"
          class="btn btn-ghost btn-sm"
          @click.stop="cancelAutoReconnect"
        >取消自动重连</button>
        <button class="btn btn-ghost btn-sm" @click.stop="closeTab">关闭标签页</button>
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

    <!-- 右键菜单：@mousedown.prevent 阻止菜单抢走 xterm 隐藏输入框的焦点（否则粘贴/复制后无法继续输入） -->
    <Teleport to="body">
      <div
        v-if="menu"
        class="menu-pop neo fixed z-50"
        :style="{left: menu.x + 'px', top: menu.y + 'px'}"
        @contextmenu.prevent
        @mousedown.prevent
        @click.stop
      >
        <button @click="doCopy">复制 <span class="text-mist">Ctrl+Shift+C</span></button>
        <button @click="doPaste">粘贴 <span class="text-mist">Ctrl+V</span></button>
        <div class="divider-h my-1"></div>
        <button @click="doSelectAll">全选</button>
        <button @click="doClear">清除屏幕</button>
        <div class="divider-h my-1"></div>
        <button @click="adjustFontSize(1)">放大 <span class="text-mist">Ctrl+=</span></button>
        <button @click="adjustFontSize(-1)">缩小 <span class="text-mist">Ctrl+-</span></button>
        <button @click="toggleFullscreen">全屏 <span class="text-mist">F11</span></button>
        <div class="divider-h my-1"></div>
        <button @click="toggleLogTrace">
          {{ logTracking ? '停止跟踪此会话日志' : '开始跟踪此会话日志' }}
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style>
.terminal-theme .xterm-rows {
  text-shadow: var(--xterm-text-shadow, none);
}

/* 断开/关闭冻结态：禁止 xterm 隐藏输入框抢键盘焦点，保留滚屏与选中复制 */
.frozen-surface .xterm-helper-textarea {
  pointer-events: none;
}

/* 断开横幅：平面警示风 —— 实底表面 + 琥珀描边 + 左侧警示条，突出"已断开"重点 */
.disconnect-banner {
  background: var(--ink-850);
  border: 1px solid var(--warn-500);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-pop);
  animation: fadeRise 200ms var(--ease);
}
.disconnect-banner:focus-visible {
  outline: 2px solid var(--warn-500);
  outline-offset: 2px;
}
.banner-accent {
  flex-shrink: 0;
  width: 3px;
  align-self: stretch;
  border-radius: 2px;
  background: var(--warn-500);
}

.completion-panel {
  animation: fadeRise 200ms cubic-bezier(0.22, 1, 0.36, 1);
}
</style>
