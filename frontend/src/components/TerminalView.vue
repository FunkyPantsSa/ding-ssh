<script lang="ts" setup>
import {errorText} from '../i18n'

import {t} from '../i18n'

import {copyText as ClipboardSetText} from '../utils/clipboard'
import {computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import {Terminal} from '@xterm/xterm'
import {guardProgrammaticSelection} from '../utils/selection-copy-guard'
import {isTerminalSearchShortcut} from '../utils/terminal-search'
import {hasOpenModal, shouldFocusTerminal} from '../utils/terminal-focus'
import {encodeTerminalInput} from '../utils/terminal-input'
import {SearchAddon} from '@xterm/addon-search'
import {Button, Input} from './ui'
import {FitAddon} from '@xterm/addon-fit'
import {WebglAddon} from '@xterm/addon-webgl'
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
  onSudoPrompt,
  onSftpSyncPath,
  reconnect,
  sshService,
} from '../services/ssh'
import {cacheServerSnapshot} from '../services/serverSnapshot'
import {onSysInfoSnapshot, sysInfoService} from '../services/sysinfo'
import {attachZmodem, type ZmodemController, type ZmodemProgress} from '../services/zmodem'
import {Colorizer} from '../utils/output-color'
import {patchXtermZoomCoords} from '../utils/xterm-zoom'
import {openTerminalStream, type TerminalStreamClient} from '../services/terminal-stream'
import {invalidateWebglGlyphModel, patchWebglZoomCanvas, syncWebglCanvas, webglCanvasReport, webglGeometrySnapshot} from '../utils/xterm-webgl-zoom'
import {
  registerSharedAtlasTerminal,
  resetSharedAtlasTerminals,
  scheduleSharedAtlasReset,
  unregisterSharedAtlasTerminal,
} from '../utils/terminal-atlas'
import {registerDebugTerminal, unregisterDebugTerminal, type DebugTerminalEntry} from '../debug/registry'
import Icon from './Icon.vue'
import TerminalContextMenu from './terminal/TerminalContextMenu.vue'
import TerminalCompletionPanel from './terminal/TerminalCompletionPanel.vue'
import TerminalConnectionOverlay from './terminal/TerminalConnectionOverlay.vue'
import TerminalTransferOverlay from './terminal/TerminalTransferOverlay.vue'
import TerminalSudoPopover from './terminal/TerminalSudoPopover.vue'
import {readScreenCommand} from './terminal/command-line'
import {useSudoAssistant} from '../composables/useSudoAssistant'
import {terminalCursorAnchor, type CursorAnchor} from '../utils/terminal-popover'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {useUIStore} from '../stores/ui'
import {useTerminalCompletion} from '../composables/useTerminalCompletion'
import {loadTerminalFonts, terminalFontReadyKeys} from '../theme/terminal-fonts'
import {remoteFontStatus} from '../theme/remote-fonts'
import {systemTone} from '../composables/useSystemTone'
import {ClipboardGetText} from '../services/desktop'
import {LogClient, SetLogTrace} from '../../bindings/ding-ssh/internal/apphost/app'
import {effectiveBrand, isManagedTerminalBackground, monoFontStack, readableOn, rgba, resolveTerminalForeground, resolveTerminalSurface, terminalAccentColor, terminalForegroundColor, terminalSurfaceColor} from '../theme/engine'
import {presetById, resolveActiveTerminalPalette} from '../theme/presets'
import {recordRecentServer} from '../services/recent'
import type {SessionTab} from '../types'

const props = defineProps<{tab: SessionTab; paneId?: string}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()
const ui = useUIStore()
// 分屏中：该 TerminalView 所在格是否焦点格；未分屏：全局 activeId 是否命中
const isActiveTab = computed(() => {
  if (props.paneId) return sessions.focusedPaneId === props.paneId
  return sessions.activeId === props.tab.clientId
})

const container = ref<HTMLElement>()
const terminalRoot = ref<HTMLElement>()
const connectionOverlay = ref<{focus: () => void} | null>(null)
const menu = ref<{x: number; y: number} | null>(null)
const fontSize = ref(settings.fonts.terminalFontSize || 13)
const zmodemMsg = ref('')
const zmodemProgress = ref<ZmodemProgress | null>(null)
const webglFallbackToast = ref('')

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let terminalStream: TerminalStreamClient | null = null
let searchAddon: SearchAddon | null = null
let selectionCopyGuard: ReturnType<typeof guardProgrammaticSelection> | null = null
function onUserSelection(event: PointerEvent) {
  if ((event.target as HTMLElement | null)?.closest('.xterm-screen')) selectionCopyGuard?.beginUserSelection()
}
const searchOpen = ref(false)
const searchQuery = ref('')
const searchCase = ref(false)
const searchCount = ref(0)
const searchIndex = ref(-1)
const searchRoot = ref<HTMLElement>()
function findOutput(previous = false, incremental = false) {
  if (!searchAddon || !searchQuery.value) {
    searchAddon?.clearDecorations(); term?.clearSelection()
    searchCount.value = 0; searchIndex.value = -1
    return
  }
  const options = {caseSensitive: searchCase.value, incremental, decorations: {
    matchBackground: '#73551a', matchBorder: '#efbf52', matchOverviewRuler: '#efbf52',
    activeMatchBackground: '#175a88', activeMatchBorder: '#70c8ff', activeMatchColorOverviewRuler: '#70c8ff',
  }}
  if (previous) searchAddon.findPrevious(searchQuery.value, options)
  else searchAddon.findNext(searchQuery.value, options)
}
watch([searchQuery, searchCase], () => findOutput(false, true))
function closeSearch() {
  searchOpen.value = false
  searchAddon?.clearDecorations()
  term?.clearSelection()
  focusTerminal()
}
function onSearchShortcut(event: KeyboardEvent) {
  if (ui.cmdOpen || ui.terminalSidebarOpen || !isTerminalSearchShortcut(event, ui.view === 'workspace' && isActiveTab.value, searchRoot.value)) return
  event.preventDefault(); event.stopImmediatePropagation()
  searchOpen.value = true
  void nextTick(() => { const input = searchRoot.value?.querySelector('input'); input?.focus(); input?.select() })
}
onMounted(() => window.addEventListener('keydown', onSearchShortcut, true))

let pendingOutputBytes = 0
let peakOutputBytes = 0
let outputSequence = 0, parsedSequence = 0, renderSequence = 0
let lastOutputAt = 0, lastParsedAt = 0, lastRenderAt = 0, lastFitAt = 0
let webglAddon: WebglAddon | null = null
// WebGL 画布位图补丁探测结果：null = 尚未探测，false = 私有 API 不可用（缩放≠100% 时回退 DOM）
let webglZoomPatched: boolean | null = null
let zmodem: ZmodemController | null = null
let resizeObserver: ResizeObserver | null = null
const disposers: Array<() => void> = []
// Terminal-lifetime hooks must survive session reconnect cleanup.
const terminalDisposers: Array<() => void> = []
// 本实例在调试注册表里的那一份登记（注销时按身份匹配，见 onBeforeUnmount 的说明）
let debugEntry: DebugTerminalEntry | null = null
let disposed = false
// 语义着色器：只往输出里注入 SGR（颜色），不改可见字符；一个终端实例一个（alt-buffer 闸门是会话状态）
const colorize = new Colorizer(settings.terminalColorMode)
let composing = false
let inputLocked = false // Zmodem 进行中挂起键盘
let autoReconnectTimer: ReturnType<typeof setTimeout> | null = null
let autoReconnectAttempt = 0
const autoReconnectCountdown = ref(0)
let countdownTimer: ReturnType<typeof setInterval> | null = null
let focusTimer: ReturnType<typeof setTimeout> | null = null

/**
 * 共享图集协调登记项：图集被清空/合并时，先丢弃本终端的 WebGL 顶点模型（只 refresh
 * 不会重建未变化的单元格），再整屏重绘。
 */
const atlasEntry = {
  invalidateModel: () => {
    if (!webglAddon) return
    // 私有 API 不可用时退回公开的 clearTextureAtlas（会连带清图集，但保证模型失效）
    if (!invalidateWebglGlyphModel(webglAddon)) term?.clearTextureAtlas()
  },
  refresh: () => term?.refresh(0, (term?.rows ?? 1) - 1),
}

// 命令补全的候选、输入行跟踪与快捷键从视图层抽出（见 composables/useTerminalCompletion.ts）。
const completion = useTerminalCompletion({
  term: () => term,
  container: () => container.value,
  tab: props.tab,
  settings,
  isComposing: () => composing,
  write: writeRaw,
  colorize,
})
const {
  suggestions, selectedIdx, navigating, panelPos, panelEl,
  hideSuggestions, enterNavigation, scheduleSuggest, trackLineInput,
  acceptSuggestion, handleCompletionKey, resetInput, dispose: disposeCompletion,
} = completion

const sudo = useSudoAssistant({
  sessionId: () => props.tab.sessionId,
  active: () => terminalInteractionAllowed() && props.tab.status === 'connected' && !!container.value?.clientWidth && !!container.value?.clientHeight && !inputLocked && !zmodem?.active(),
  focusTerminal,
  resetInput,
})
const {prompt: sudoPrompt, sensitive: sudoSensitive, automatic: sudoAutomatic, password: sudoPassword, submitting: sudoSubmitting, error: sudoError} = sudo
const sudoAnchor = ref<CursorAnchor | null>(null)
const sudoBounds = ref({width: 0, height: 0})
function positionSudo() {
  if (!term || !container.value || !sudoPrompt.value) return
  sudoAnchor.value = terminalCursorAnchor(term, container.value)
  sudoBounds.value = {width: container.value.clientWidth, height: container.value.clientHeight}
  if (!sudoAnchor.value) sudo.cancel(false)
}
function interruptSudo() {
  sudo.cancel()
  writeRaw('\x03')
}
watch(sudoPrompt, () => { if (sudoPrompt.value) { hideSuggestions(); void nextTick(positionSudo) } })

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

const followsAppTheme = computed(() => !settings.theme.bgImage)
const appTone = computed(() => settings.appearance.baseTone === 'auto' ? systemTone.value : settings.appearance.baseTone)
const terminalTone = computed(() => appTone.value)
const terminalBackground = computed(() => {
  const isLight = terminalTone.value === 'light'
  const seed = effectiveBrand(settings.appearance).primary
  const atmo = settings.appearance.atmosphere ?? settings.appearance.colorIntensity ?? 75
  const sep = settings.appearance.surfaceSeparation ?? 65
  const dynamicBg = terminalSurfaceColor(seed, terminalTone.value, atmo, sep)

  if (settings.theme.bgImage) {
    return settings.theme.background
  }

  // Preserve user custom background if explicitly unmanaged
  if (!isManagedTerminalBackground(settings.theme.background, seed, terminalTone.value)) {
    const isDarkBg = readableOn(settings.theme.background) === '#ffffff'
    // If the unmanaged custom background tone clashes with app mode, protect contrast
    if (isLight && isDarkBg) {
      return dynamicBg
    }
    if (!isLight && !isDarkBg) {
      return dynamicBg
    }
    return settings.theme.background
  }

  return dynamicBg
})
const terminalAccent = computed(() =>
  terminalAccentColor(
    effectiveBrand(settings.appearance).primary,
    terminalTone.value,
    settings.appearance.atmosphere ?? settings.appearance.colorIntensity ?? 75,
    settings.appearance.surfaceSeparation ?? 65,
  ),
)

const terminalStyle = computed(() => ({
  '--xterm-text-shadow': settings.theme.textShadow && appTone.value !== 'light'
    ? 'var(--terminal-text-shadow)'
    : 'none',
  '--terminal-shadow-blur': `${settings.theme.shadowBlur}px`,
  // 终端容器底色跟随终端主题：xterm 的内边距区域也呈现终端背景色，
  // 避免浅色界面下终端四周露出一圈界面底色边框（终端主题与界面明暗相互独立）。
  background: terminalBackground.value,
  // 不再反向抵消全局 zoom：app-shell 的 zoom 会整体等比缩放（含终端字号），
  // 若在此处设置 zoom: 1/z 会把 xterm 容器压缩 1/z，导致分屏格右侧/底部出现空白。
}))

const sourceLabel: Record<string, string> = {
  get history() { return t("历史") },
  get dict() { return t("字典") },
  get screen() { return t("屏幕") },
}

function applyTheme() {
  if (!term) return
  const t = settings.theme
  // Windows 透明字形比不透明背景上的字形显细，使用真实 Medium 字重补偿。
  term.options.fontWeight = terminalFontWeight()
  const background = t.bgImage ? hexToRgba(t.background, 0.85) : terminalBackground.value
  // Readability protection: only a user-provided background image may make the
  // terminal translucent. Glass material never lowers terminal opacity.
  term.options.allowTransparency = !!t.bgImage
  const follow = followsAppTheme.value
  const tone = terminalTone.value
  const isLight = tone === 'light'
  const seed = effectiveBrand(settings.appearance).primary
  const atmo = settings.appearance.atmosphere ?? settings.appearance.colorIntensity ?? 75
  const sep = settings.appearance.surfaceSeparation ?? 65
  const pal = resolveActiveTerminalPalette(settings.appearance, tone)

  const cursor = pal ? pal.cursor : (follow ? terminalAccent.value : t.cursor)
  const selectionBackground = pal ? pal.selection : (follow ? rgba(terminalAccent.value, 0.28) : t.selection)
  const foreground = (!isManagedTerminalBackground(t.background, seed, tone) && !follow)
    ? resolveTerminalForeground(false, t.foreground, background)
    : (pal ? pal.foreground : terminalForegroundColor(seed, tone, atmo, sep))

  const ansi = pal ? pal.ansi : [t.black, t.red, t.green, t.yellow, t.blue, t.magenta, t.cyan, t.white]
  const bright = pal ? pal.bright : [t.brightBlack, t.brightRed, t.brightGreen, t.brightYellow, t.brightBlue, t.brightMagenta, t.brightCyan, t.brightWhite]

  term.options.theme = {
    background,
    foreground,
    cursor,
    cursorAccent: readableOn(cursor),
    selectionBackground,
    black: ansi[0],
    red: ansi[1],
    green: ansi[2],
    yellow: ansi[3],
    blue: ansi[4],
    magenta: ansi[5],
    cyan: ansi[6],
    white: ansi[7],
    brightBlack: bright[0],
    brightRed: bright[1],
    brightGreen: bright[2],
    brightYellow: bright[3],
    brightBlue: bright[4],
    brightMagenta: bright[5],
    brightCyan: bright[6],
    brightWhite: bright[7],
  }
  term.refresh(0, term.rows - 1)
  atlasEntry.invalidateModel()
  atlasEntry.refresh()
}

let fontLoadSeq = 0

function applyFontSettings() {
  if (!term) return
  const seq = ++fontLoadSeq
  term.options.fontFamily = monoFontStack(settings.fonts.terminalFont)
  term.options.fontSize = effectiveFontSize()
  fit()
  void loadTerminalFonts(settings.fonts.terminalFont, effectiveFontSize(), terminalFontWeight()).then(() => {
    if (!term || seq !== fontLoadSeq) return
    term.clearTextureAtlas()
    fit()
    // 图集是多终端共享的：清空后必须让兄弟终端也重建顶点模型，否则切回其它标签会整屏乱码。
    resetSharedAtlasTerminals()
  }).catch((error: unknown) => {
    if (!term || seq !== fontLoadSeq) return
    term.clearTextureAtlas()
    fit()
    resetSharedAtlasTerminals()
    logClient('warn', t("[fonts] 终端字体加载失败，使用回退字体：{0}", [errorText(error)]))
  })
}

function terminalFontWeight(): 400 | 500 {
  return navigator.userAgent.includes('Windows') && !!settings.theme.bgImage ? 500 : 400
}

/**
 * 是否允许使用 WebGL 渲染。
 *
 * 历史结论（已推翻并修正）：界面缩放走 `.app-shell { zoom }`，`@xterm/addon-webgl`
 * 在 Chromium 下会把 canvas 位图按「含 zoom 的 devicePixelContentBoxSize」放大，而渲染器
 * 内部网格仍按未缩放的 CSS 像素算，于是整幅网格被画进超大位图的左下角 —— 表现为
 * 「顶部前几行看不到、缓冲区已在顶部无法上滚」（缓冲区与 DOM 几何都正确，只是像素错位，
 * `refresh()` / `clearTextureAtlas()` 都纠不回来）。
 *
 * 现在由 `patchWebglZoomCanvas()`（见 utils/xterm-webgl-zoom.ts）把位图拉回网格尺寸并
 * 重设 GL viewport / uniform，任意缩放档位都能正常使用 WebGL；只有补丁打不上
 * （addon 私有 API 变化）且缩放 ≠ 100% 时，才回退 DOM。
 */
function webglUsable(): boolean {
  if (!settings.webGLEnabled) return false
  if (uiZoom() === 1) return true
  return webglZoomPatched !== false
}

/** 渲染诊断串：WebGL 画布几何 + xterm 行列 + 视口滚动状态（用于定位「顶部缺行」类问题）。 */
function renderDiag(): string {
  const parts: string[] = []
  if (webglAddon) parts.push(webglCanvasReport(webglAddon, uiZoom()) ?? 'webgl=?')
  if (term) {
    parts.push(`cols=${term.cols} rows=${term.rows}`)
    const vp = term.element?.querySelector('.xterm-viewport') as HTMLElement | null
    if (vp) {
      parts.push(`viewport=${vp.clientHeight}/${vp.scrollHeight} scrollTop=${Math.round(vp.scrollTop)}`)
    }
    const buf = term.buffer.active
    parts.push(`baseY=${buf.baseY} viewportY=${buf.viewportY}`)
  }
  return parts.join(' ')
}

function tryEnableWebGL() {
  if (!term || !settings.webGLEnabled) return
  if (!webglUsable()) {
    disableWebGL()
    console.info(t("[webgl] 界面缩放 ≠ 100% 且无法修正画布位图，已使用 DOM 渲染"))
    logClient('info', t("[webgl] uiScale={0} → DOM 渲染（画布位图补丁不可用）", [settings.uiScale]))
    return
  }
  try {
    webglAddon?.dispose()
    webglAddon = new WebglAddon()
    webglAddon.onContextLoss(() => {
      disableWebGL()
      logClient('warn', t("[webgl] 上下文丢失，已回退 DOM 渲染"))
      webglFallbackToast.value = t("WebGL 上下文丢失，已降级 DOM 渲染")
      setTimeout(() => {
        webglFallbackToast.value = ''
      }, 4000)
    })
    term.loadAddon(webglAddon)
    // 图集页合并/新增后，共享同一图集的其它终端模型会失效；合并到下一轮统一重绘。
    webglAddon.onAddTextureAtlasCanvas(() => scheduleSharedAtlasReset())
    // CSS zoom 修正：必须在 loadAddon 之后（此时才拿得到渲染器实例）
    const patched = patchWebglZoomCanvas(webglAddon, uiZoom)
    if (webglZoomPatched === null) webglZoomPatched = patched
    if (!patched && uiZoom() !== 1) {
      // 私有 API 变了 → 老策略兜底：缩放 ≠ 100% 时宁可用 DOM，也不画错位
      disableWebGL()
      webglFallbackToast.value = t("WebGL 在界面缩放下不可用，已降级 DOM 渲染")
      setTimeout(() => {
        webglFallbackToast.value = ''
      }, 4000)
      logClient('warn', t("[webgl] 画布位图补丁不可用（uiScale={0}）→ DOM 渲染", [settings.uiScale]))
      return
    }
    syncWebglCanvas(webglAddon)
    logClient('debug', `[webgl] enabled ${renderDiag()}`)
  } catch {
    disableWebGL()
    logClient('warn', t("[webgl] 初始化失败，已回退 DOM 渲染"))
    webglFallbackToast.value = t("当前环境不支持 WebGL，已降级 DOM 渲染")
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

function terminalFocusAllowed(intent: 'background' | 'activate' | 'restore' = 'background') {
  return shouldFocusTerminal({
    root: terminalRoot.value,
    terminal: container.value,
    active: isActiveTab.value,
    workspace: ui.view === 'workspace',
    overlayOpen: ui.cmdOpen || ui.terminalSidebarOpen,
    intent,
    restoreFrom: [searchRoot.value, terminalRoot.value?.querySelector<HTMLElement>('[data-testid="terminal-sudo-popover"]'), document.querySelector<HTMLElement>('[data-testid="terminal-context-menu"]')],
  })
}

function focusActiveSurface(intent: 'background' | 'activate' = 'background') {
  if (disposed || !terminalFocusAllowed(intent)) return
  if (sudoPrompt.value) return
  if (props.tab.status === 'connected') {
    container.value?.focus()
    term?.focus()
    return
  }
  if (props.tab.status === 'disconnected' || props.tab.status === 'closed') {
    // 先释放 xterm 隐藏输入框持有的键盘焦点，避免焦点残留在终端光标处
    term?.blur()
    connectionOverlay.value?.focus()
    return
  }
  if (props.tab.status === 'connecting' || props.tab.status === 'error') {
    connectionOverlay.value?.focus()
  }
}

function scheduleFocusActiveSurface(intent: 'background' | 'activate' = 'background') {
  if (focusTimer !== null) {
    clearTimeout(focusTimer)
    focusTimer = null
  }
  focusTimer = setTimeout(() => {
    focusTimer = null
    void nextTick(() => focusActiveSurface(intent))
  }, 0)
}

function fit() {
  if (!term || !fitAddon || disposed) return
  const {cols, rows} = term
  fitAddon.fit()
  lastFitAt = Date.now()
  // fit 改了网格尺寸：WebGL 下确保 canvas 位图 / GL viewport 跟着对齐
  // （CSS zoom 下观察器上报的是含 zoom 的尺寸，不能直接采用）
  if (webglAddon) syncWebglCanvas(webglAddon)
  if (props.tab.sessionId && (cols !== term.cols || rows !== term.rows)) {
    if (terminalStream && terminalStream.isOpen()) {
      terminalStream.sendResize(term.cols, term.rows)
    } else {
      sshService.resize(props.tab.sessionId, term.cols, term.rows).catch(() => {})
    }
  }
  positionSudo()
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
function terminalInteractionAllowed(): boolean {
  return ui.view === 'workspace' && isActiveTab.value && !ui.cmdOpen && !ui.terminalSidebarOpen && !hasOpenModal()
}

function canSendInput(): boolean {
  if (disposed || !term) return false
  if (!terminalInteractionAllowed()) return false
  if (props.tab.status !== 'connected') return false
  return !inputLocked && !zmodem?.active()
}

/** 把键盘焦点交回终端（xterm 隐藏输入框），避免菜单 / 剪贴板操作后焦点残留在别处。 */
function focusTerminal() {
  if (disposed || !term) return
  if (props.tab.status !== 'connected') return
  if (!terminalFocusAllowed('restore')) return
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
  if (sel) void ClipboardSetText(sel).catch(() => {})
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
  // 原始点击坐标交给 TerminalContextMenu：它在渲染后按真实尺寸贴边/翻转
  menu.value = {x: e.clientX, y: e.clientY}
}

function closeMenu(restoreFocus = false) {
  menu.value = null
  if (restoreFocus) focusTerminal()
}

/** window click 监听不接收布尔参数：单独包一层，避免 click 事件被当成 restoreFocus。 */
function onWindowClick() {
  closeMenu()
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
  selectionCopyGuard?.beginUserSelection()
  term?.selectAll()
  closeMenu()
  focusTerminal()
}

function onKeydown(e: KeyboardEvent) {
  if (!terminalInteractionAllowed()) return
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
    closeMenu(true)
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

function writeOutput(output: Uint8Array) {
  if (!term || disposed || !output.length) return
  const size = output.length
  const sequence = ++outputSequence
  lastOutputAt = Date.now()
  pendingOutputBytes += size
  peakOutputBytes = Math.max(peakOutputBytes, pendingOutputBytes)
  term.write(output, () => {
    pendingOutputBytes -= size
    parsedSequence = Math.max(parsedSequence, sequence)
    lastParsedAt = Date.now()
  })
}

async function setupZmodem(sessionId: string) {
  if (!term) return
  zmodem?.dispose()
  zmodem = null
  try {
    zmodem = await attachZmodem({
      term,
      sessionId,
      writeOutput,
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
            focusTerminal()
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
  sudo.reset()
  disposers.forEach((d) => d())
  disposers.length = 0
  sessions.setStatus(props.tab.clientId, 'connecting')
  resetSteps()
  resetInput()
  // 新会话：清掉上一个会话的挂起半行、alt-buffer 闸门与命令回显记录
  colorize.reset()

  const sid = props.tab.clientId
  props.tab.sessionId = sid
  if (keepBuffer) {
    term?.write(t("\r\n\u001b[33m── 重新连接 ──\u001b[0m\r\n"))
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
      if (evt.status !== 'connected') sudo.reset()
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
        console.info(t("[sftp-sync] 开关已关，忽略"), newPath)
        return
      }
      if (!newPath) {
        console.info(t("[sftp-sync] 空路径，忽略"))
        return
      }
      if (props.tab.node.jumpServerMode) return
      if (newPath === props.tab.sftpPath) {
        console.info(t("[sftp-sync] 已在该目录，忽略"), newPath)
        return
      }
      console.info(t("[sftp-sync] 写入 sftpPath"), props.tab.sftpPath, '→', newPath)
      sessions.setSftpPath(props.tab.clientId, newPath)
    }),
  )

  // 输出监听必须在发起连接之前注册：远端登录横幅（/etc/motd、fastfetch 等）通常在
  // Connect 返回前就已回传，而 Wails 事件不缓存、无监听即丢弃 —— 订阅晚了会丢掉开头
  // 几行输出（表现为「终端顶部吞字符」，且滚屏也找不回来）。
  // 会话 id 由前端生成并传入后端（后端事件名、返回值都用它），因此可以提前订阅。
  let streamReady = false
  const handleTerminalBytes = (bytes: Uint8Array) => {
    if (disposed || !term) return
    if (sudoSensitive.value && !zmodem?.active()) {
      writeOutput(colorize.flush())
      writeOutput(bytes)
    }
    else if (zmodem) zmodem.consume(bytes)
    // 语义着色：只注入 SGR；off 档 / TUI（alt-buffer）内部直接原样返回输入字节
    else writeOutput(colorize.feed(bytes))
  }

  terminalStream = openTerminalStream(sid, (bytes) => {
    handleTerminalBytes(bytes)
  }, (ready) => {
    streamReady = ready
  })
  if (terminalStream) {
    disposers.push(() => {
      terminalStream?.close()
      terminalStream = null
    })
  }

  disposers.push(
    onSudoPrompt(sid, (event) => {
      if (disposed) return
      sudo.handleEvent(event)
    }),
    onSessionOutput(sid, (data) => {
      if (disposed || !term || streamReady) return
      handleTerminalBytes(base64ToBytes(data))
    }),
  )

  try {
    const isLocal = props.tab.kind === 'local'
    const result = isLocal
      ? await sshService.connectLocal(sid, term?.cols ?? 80, term?.rows ?? 24)
      : await sshService.connect(sid, props.tab.node, term?.cols ?? 80, term?.rows ?? 24, props.tab.securityPrompts !== false)
    if (disposed) {
      sshService.disconnect(result.sessionId).catch(() => {})
      return false
    }
    sessions.bindSession(props.tab.clientId, result.sessionId, result.server)
    sessions.setStatus(props.tab.clientId, 'connected')
    if (!isLocal) recordRecentServer({id: props.tab.node.id, name: props.tab.serverName, host: props.tab.host, connectedAt: Date.now()})
    if (!isLocal) {
      await setupZmodem(result.sessionId)
    }
    // 本机会话无远程 SFTP / 系统监控
    if (!isLocal) {
      // 「登录后自动采集」开关关闭时，不在远端启动采集（status bar / 看板无数据）。
      if (settings.sysInfoCollectEnabled) {
        disposers.push(onSysInfoSnapshot(result.sessionId, snap => cacheServerSnapshot(props.tab.node, snap)))
        void sysInfoService.start(result.sessionId).catch(() => {})
        disposers.push(() => {
          void sysInfoService.stop(result.sessionId).catch(() => {})
        })
      }
    }
    fit()
  } catch (e) {
    sessions.setStatus(props.tab.clientId, 'error', errorText(e))
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
    term?.write(t("\r\n\u001b[33m[自动重连] 第 {0} 次尝试…\u001b[0m\r\n", [autoReconnectAttempt]))
    const ok = await reconnectSession(false)
    if (ok) {
      autoReconnectAttempt = 0
    } else if (props.tab.status === 'disconnected' && settings.autoReconnect && !disposed) {
      scheduleAutoReconnect()
    }
  }, delay)
}

async function reconnectSession(countLogin = true): Promise<boolean> {
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
      recordRecentServer({id: props.tab.node.id, name: props.tab.serverName, host: props.tab.host, connectedAt: Date.now()}, countLogin)
      autoReconnectAttempt = 0
      await setupZmodem(props.tab.sessionId)
      fit()
      return true
    } catch (e) {
      sessions.setStatus(props.tab.clientId, 'disconnected', errorText(e))
      return false
    }
  }
  // 完整重连：保留已有缓冲区（只追加分隔行），避免清屏擦掉刚输出的横幅开头
  return connect(true)
}

const CONNECT_STEPS: {key: string; label: string}[] = [
  {key: 'dns', get label() { return t("DNS / 直连") }},
  {key: 'tcp', get label() { return t("TCP 握手") }},
  {key: 'auth', get label() { return t("SSH 鉴权") }},
  {key: 'pty', get label() { return t("分配 PTY") }},
  {key: 'ready', get label() { return t("会话就绪") }},
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

function writeRaw(data: string, commandHint?: string, fromUser = true, binary = false) {
  if (!props.tab.sessionId) return
  const command = fromUser && !sudoSensitive.value && (data === '\r' || data === '\n') && term?.buffer.active.type === 'normal'
    ? (commandHint ?? readScreenCommand(term).cmd) : ''
  sudo.terminalInput()
  // 发往会话的输入（按键 / 粘贴 / 补全）先补写被语义着色挂起的提示符，并让回显逐字符透传。
  // 否则逐字符输入会被当成「未完成的输出行」缓存，表现为输入了但屏幕不显示。
  const pending = colorize.beginInput()
  if (pending.length) term?.write(pending)
  if (terminalStream && terminalStream.isOpen()) {
    if (fromUser && command) {
      terminalStream.sendInteractive(data, command)
    } else {
      terminalStream.send(data)
    }
  } else {
    const send = (encoded: string) => fromUser
      ? sshService.writeInteractive(props.tab.sessionId, encoded, command)
      : sshService.write(props.tab.sessionId, encoded)
    void send(encodeTerminalInput(data, binary)).catch(() => {})
  }
}

onMounted(() => {
  term = new Terminal({
    allowProposedApi: true,
    allowTransparency: true,
    minimumContrastRatio: 4.5,
    fontWeight: terminalFontWeight(),
    fontWeightBold: 700,
    cursorBlink: true,
    fontSize: effectiveFontSize(),
    fontFamily: monoFontStack(settings.fonts.terminalFont),
    theme: {
      background: terminalBackground.value,
      foreground: resolveTerminalForeground(followsAppTheme.value, settings.theme.foreground, terminalBackground.value),
      cursor: followsAppTheme.value ? terminalAccent.value : settings.theme.cursor,
      cursorAccent: readableOn(followsAppTheme.value ? terminalAccent.value : settings.theme.cursor),
      selectionBackground: followsAppTheme.value ? rgba(terminalAccent.value, 0.28) : settings.theme.selection,
    },
    scrollback: 5000,
    // 关闭「用户输入时强制滚到底部」：浏览历史时按普通键/粘贴不再跳转；
    // 需要回到底部时按 Enter 即可（见 attachCustomKeyEventHandler）。
    scrollOnUserInput: false,
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(container.value!)
  searchAddon = new SearchAddon({highlightLimit: 1000})
  term.loadAddon(searchAddon)
  const searchResults = searchAddon.onDidChangeResults(result => {
    searchCount.value = result.resultCount
    searchIndex.value = result.resultIndex
  })
  terminalDisposers.push(() => searchResults.dispose())
  registerSharedAtlasTerminal(atlasEntry)
  // 调试模式：把自己注册进终端注册表，供调试 API 读取 xterm 内部状态 / 输入 / 滚动。
  // 放在 WebGL / 字号 / 事件接线之前：后面任何一步抛错都不该造成「终端已挂载、注册表却为空」
  //（调试 API 的 list_terminals / terminal.* 全都依赖这份登记）。
  debugEntry = {
    clientId: props.tab.clientId,
    title: props.tab.serverName,
    kind: props.tab.kind ?? 'ssh',
    term,
    runtimeStatus: () => ({webgl: webglAddon !== null, pendingOutputBytes, peakOutputBytes, outputSequence, parsedSequence, renderSequence, lastOutputAt, lastParsedAt, lastRenderAt, lastFitAt, webglGeometry: webglGeometrySnapshot(webglAddon)}),
    container: () => container.value ?? null,
    write: (data, binary) => writeRaw(data, undefined, false, binary),
    reconnect: () => {
      void reconnectSession()
    },
    close: closeTab,
  }
  registerDebugTerminal(debugEntry)

  const onReconnectActive = () => {
    if (sessions.activeId === props.tab.clientId) {
      void reconnectSession()
    }
  }
  window.addEventListener('terminal:reconnect-active', onReconnectActive)
  terminalDisposers.push(() => window.removeEventListener('terminal:reconnect-active', onReconnectActive))
  patchXtermZoomCoords(term, uiZoom)
  tryEnableWebGL()
  applyFontSize()

  // 在 xterm 处理按键前拦截补全快捷键
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true
    if (!canSendInput()) return false
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
    if (!canSendInput()) return
    if (sudoSensitive.value) {
      resetInput()
      writeRaw(data)
      return
    }
    // 粘贴大段文本抑制补全
    if (data.length > 80) {
      resetInput()
      writeRaw(data)
      return
    }
    const executedCommand = trackLineInput(data)
    writeRaw(data, executedCommand)
    if (settings.completionEnabled) scheduleSuggest()
  })
  terminalDisposers.push(() => sudo.reset())
  const sudoWrite = term.onWriteParsed(positionSudo)
  const debugRender = term.onRender(() => { renderSequence++; lastRenderAt = Date.now() })
  const sudoScroll = term.onScroll(positionSudo)
  terminalDisposers.push(() => { sudoWrite.dispose(); sudoScroll.dispose(); debugRender.dispose() })
  const cancelSudoOnBlur = () => sudo.cancel(false)
  window.addEventListener('blur', cancelSudoOnBlur)
  terminalDisposers.push(() => window.removeEventListener('blur', cancelSudoOnBlur))

  selectionCopyGuard = guardProgrammaticSelection(term)
  container.value?.addEventListener('pointerdown', onUserSelection, true)
  terminalDisposers.push(() => selectionCopyGuard?.dispose())
  term.onSelectionChange(() => {
    if (!settings.copyOnSelect || selectionCopyGuard?.isProgrammatic()) return
    const selection = term?.getSelection()
    if (selection) void ClipboardSetText(selection).catch(() => {})
  })

  resizeObserver = new ResizeObserver(() => fit())
  resizeObserver.observe(container.value!)
  applyTheme()
  applyFontSettings()
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
  window.addEventListener('click', onWindowClick)
  void connect()
  scheduleFocusActiveSurface('activate')
})

watch(
  () => [
    settings.theme,
    settings.appearance,
    terminalBackground.value,
    followsAppTheme.value,
    terminalAccent.value,
    terminalTone.value,
  ],
  () => {
    // Update xterm options only. The Terminal instance, SSH session, scroll
    // position, input buffer and viewport size are never recreated here.
    applyTheme()
  },
  {deep: true},
)

// 语义着色档位：把闸门里挂起的半行补写出去，后续输出按新档处理
watch(
  () => settings.terminalColorMode,
  (mode) => {
    if (!term) return
    const flushed = colorize.setMode(mode)
    if (flushed.length) term.write(flushed)
  },
)

watch(
  () => settings.fonts,
  () => {
    fontSize.value = settings.fonts.terminalFontSize || 13
    applyFontSettings()
  },
  {deep: true},
)

// A retry in Settings can finish without changing the saved font preference.
// Rebuild existing terminals when that selection becomes available.
watch(
  () => terminalFontReadyKeys(settings.fonts.terminalFont).map(key => remoteFontStatus[key] === 'ready'),
  (ready, previous) => {
    if (ready.some((value, index) => value && !previous?.[index])) applyFontSettings()
  },
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
  () => [isActiveTab.value, props.tab.status, ui.view] as const,
  ([active, , view], previous) => {
    if (!active) { sudo.cancel(false); return }
    // 共享字形图集可能在后台标签渲染时被清空/重排；切回时丢弃顶点模型并整屏重绘。
    atlasEntry.invalidateModel()
    atlasEntry.refresh()
    const activated = !previous?.[0] || (view === 'workspace' && previous?.[2] !== 'workspace')
    scheduleFocusActiveSurface(activated ? 'activate' : 'background')
  },
  {immediate: true},
)

watch(
  () => settings.uiScale,
  () => {
    if (!term || disposed) return
    nextTick(() => {
      applyFontSize()
      // 缩放变化会改变 CSS zoom：补丁可用的环境继续用 WebGL（位图已在 fit() 里重新对齐），
      // 补丁不可用且缩放 ≠ 100% 时回退 DOM
      if (!settings.webGLEnabled) return
      if (webglAddon && !webglUsable()) disableWebGL()
      else if (!webglAddon && webglUsable()) tryEnableWebGL()
      else if (webglAddon) {
        syncWebglCanvas(webglAddon)
        logClient('debug', `[webgl] uiScale→${settings.uiScale}% ${renderDiag()}`)
      }
    })
  },
)

onBeforeUnmount(() => {
  container.value?.removeEventListener('pointerdown', onUserSelection, true)
  window.removeEventListener('keydown', onSearchShortcut, true)
  searchAddon = null
  disposed = true
  // 带上自己那一份登记的引用：只有它还是注册表里的当前项才注销，
  // 避免 HMR「先挂新的、后卸旧的」时把新实例的登记顺手删掉（那会让注册表重新变空）。
  if (debugEntry) unregisterDebugTerminal(props.tab.clientId, debugEntry)
  debugEntry = null
  if (focusTimer) clearTimeout(focusTimer)
  cancelAutoReconnect()
  disposeCompletion()
  terminalDisposers.forEach(dispose => dispose())
  disposers.forEach((d) => d())
  resizeObserver?.disconnect()
  container.value?.removeEventListener('paste', onPasteEvent, true)
  window.removeEventListener('click', onWindowClick)
  zmodem?.dispose()
  disableWebGL()
  if (props.tab.sessionId) {
    void sshService.disconnect(props.tab.sessionId).catch(() => {})
  }
  // 补写挂起的半行（没有换行结尾的最后一段输出，否则会随终端一起丢掉）
  if (term) {
    const tail = colorize.flush()
    if (tail.length) term.write(tail)
  }
  unregisterSharedAtlasTerminal(atlasEntry)
  term?.dispose()
  term = null
})
</script>

<template>
  <div ref="terminalRoot" class="relative h-full overflow-hidden terminal-theme" :style="terminalStyle">
    <div v-if="searchOpen" ref="searchRoot" class="terminal-search" role="search" :aria-label="t('搜索终端输出')" @keydown.esc.stop.prevent="closeSearch">
      <Input v-model="searchQuery" @keydown.enter.stop.prevent="findOutput($event.shiftKey)" class="min-w-0 flex-1" :placeholder="t('搜索终端输出')" :aria-label="t('搜索终端输出')" />
      <span class="text-xs shrink-0" role="status">{{ searchQuery ? (searchCount ? `${searchIndex + 1}/${searchCount}` : t('无匹配结果')) : '' }}</span>
      <Button size="icon" variant="ghost" :aria-pressed="searchCase" :title="t('区分大小写')" :aria-label="t('区分大小写')" @click="searchCase = !searchCase">Aa</Button>
      <Button size="icon" variant="ghost" :aria-label="t('上一个匹配')" @click="findOutput(true)">↑</Button>
      <Button size="icon" variant="ghost" :aria-label="t('下一个匹配')" @click="findOutput()">↓</Button>
      <Button size="icon" variant="ghost" :aria-label="t('关闭搜索')" @click="closeSearch">×</Button>
    </div>
    <div
      v-if="settings.theme.bgImage"
      class="absolute inset-0 bg-cover bg-center pointer-events-none"
      :style="bgImageStyle"
    ></div>

    <div
      ref="container"
      class="absolute inset-0"
      :class="tab.status === 'connected' ? '' : 'frozen-surface'"
      data-keyboard-native
      tabindex="0"
      @focus.self="focusActiveSurface()"
    ></div>

    <TerminalConnectionOverlay
      ref="connectionOverlay"
      :tab="tab"
      :settings="settings"
      :steps="steps"
      :connect-steps="CONNECT_STEPS"
      :active-step-label="activeStepLabel"
      :any-step-error="anyStepError"
      :auto-reconnect-countdown="autoReconnectCountdown"
      :auto-reconnect-attempt="autoReconnectAttempt"
      @close="closeTab"
      @connect="connect"
      @reconnect="reconnectSession"
      @edit="ui.requestEditServer(tab.node.id)"
      @cancel-auto-reconnect="cancelAutoReconnect"
      @toggle-step="toggleStep"
    />

    <TerminalTransferOverlay
      :webgl-fallback-toast="webglFallbackToast"
      :zmodem-msg="zmodemMsg"
      :zmodem-progress="zmodemProgress"
    />

    <TerminalCompletionPanel v-if="suggestions.length && panelPos" ref="panelEl" :items="suggestions" :position="panelPos" :navigating="navigating" :selected-index="selectedIdx" :hotkey-label="formatHotkeyLabel(settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY)" :source-label="sourceLabel" @hover="enterNavigation" @accept="acceptSuggestion" />

    <TerminalSudoPopover v-if="sudoPrompt && isActiveTab" v-model:password="sudoPassword" :prompt="sudoPrompt" :anchor="sudoAnchor" :bounds="sudoBounds" :server-name="tab.serverName" :automatic="sudoAutomatic" :submitting="sudoSubmitting" :error="sudoError" @submit="sudo.submit" @cancel="sudo.cancel()" @manual="sudo.manual" @interrupt="interruptSudo" />

    <TerminalContextMenu v-if="menu" :position="menu" :log-tracking="logTracking" @copy="doCopy" @paste="doPaste" @select-all="doSelectAll" @clear="doClear" @font="adjustFontSize" @fullscreen="toggleFullscreen" @log-trace="toggleLogTrace" @close="closeMenu(true)" />
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
  background: var(--surface-2);
  border: 1px solid var(--warning);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-pop);
  animation: fadeRise 200ms var(--ease);
}
.disconnect-banner:focus-visible {
  outline: 2px solid var(--warning);
  outline-offset: 2px;
}
.banner-accent {
  flex-shrink: 0;
  width: 3px;
  align-self: stretch;
  border-radius:var(--radius-small);
  background: var(--warning);
}

.completion-panel {
  animation: fadeRise 200ms cubic-bezier(0.22, 1, 0.36, 1);
}
</style>

<style scoped>
.terminal-search{position:absolute;z-index:35;top:8px;right:8px;display:flex;align-items:center;gap:4px;padding:6px;width:min(400px,calc(100% - 16px));flex-wrap:wrap;border:1px solid color-mix(in srgb,var(--accent-border) 32%,var(--border-default));border-radius:var(--radius-md);background:var(--surface-overlay-bg);backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(160%);-webkit-backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(160%);color:var(--foreground);box-shadow:var(--shadow-floating)}
.terminal-search input{flex-basis:120px}
</style>
