import {nextTick, ref, type Ref} from 'vue'
import type {Terminal} from '@xterm/xterm'
import {
  acceptSuffix,
  completionPrefix,
  extractScreenWords,
  mergeSuggestions,
  type Suggestion,
} from '../completion/engine'
import {DEFAULT_COMPLETION_NAV_HOTKEY, matchHotkey} from '../completion/hotkey'
import {historyService} from '../services/history'
import {currentZoom} from '../utils/dom'
import type {Colorizer} from '../utils/output-color'
import type {SessionTab} from '../types'
import type {useSettingsStore} from '../stores/settings'
import {
  looksLikeCommandPollution,
  readScreenCommand,
  resolveExecutedCommand,
  sanitizeHistoryCommand,
} from '../components/terminal/command-line'

type SettingsStore = ReturnType<typeof useSettingsStore>

export interface TerminalCompletionOptions {
  term: () => Terminal | null
  container: () => HTMLElement | undefined
  tab: SessionTab
  settings: SettingsStore
  isComposing: () => boolean
  write: (data: string) => void
  colorize: Colorizer
}

/**
 * 终端命令补全：候选、面板定位、输入行跟踪与快捷键。
 *
 * 从 TerminalView 抽出，保持原有行为不变：只有显式导航后才拦截 ↑↓/Tab/Enter，
 * 历史命令按整行写入，屏幕/字典按当前 token 补全。
 */
export function useTerminalCompletion(opts: TerminalCompletionOptions) {
  const suggestions = ref<Suggestion[]>([])
  const selectedIdx = ref(0)
  const navigating = ref(false)
  const panelPos = ref<{left: number; top: number} | null>(null)
  const panelEl = ref<HTMLElement | null>(null)

  let lineBuf = ''
  let suggestTimer: ReturnType<typeof setTimeout> | null = null
  let lineBufSyncTimer: ReturnType<typeof setTimeout> | null = null
  type EscState = 'none' | 'esc' | 'csi' | 'osc'
  let escState: EscState = 'none'

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

  function xtermCellSize(term: Terminal): {width: number; height: number} {
    const core = (term as unknown as {
      _core?: {_renderService?: {dimensions?: {css?: {cell?: {width: number; height: number}}}}}
    })._core
    return core?._renderService?.dimensions?.css?.cell ?? {width: 8, height: 16}
  }

  async function updatePanelPosition() {
    const term = opts.term()
    const container = opts.container()
    if (!term || !container) return
    const dims = xtermCellSize(term)
    const cellW = dims.width ?? 8
    const cellH = dims.height ?? 16
    const buf = term.buffer.active
    // container 的 gBCR 相对 viewport（含 app-shell 的 zoom），而面板用绝对定位（未缩放坐标）。
    // 统一先折算回布局像素，cursor 位置与面板尺寸才不会随 uiScale 漂移。
    const z = currentZoom(container)
    const rect = container.getBoundingClientRect()
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

  async function refreshSuggestions() {
    const term = opts.term()
    if (!opts.settings.completionEnabled || !term || opts.isComposing() || opts.tab.status !== 'connected') {
      hideSuggestions()
      return
    }
    const {full, token} = completionPrefix(lineBuf)
    const query = full.length >= 1 ? full : token
    if (!query || query.length < 1) {
      hideSuggestions()
      return
    }
    const limit = Math.max(3, Math.min(30, opts.settings.completionPanelLimit || 8))
    const hist = await historyService.query(opts.tab.node.id, full || token, limit)
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

  /** Tab / ↑↓ 后延时从屏上同步 lineBuf，避免缺字与补全面板前缀错位。 */
  function scheduleLineBufSyncFromScreen() {
    if (lineBufSyncTimer) clearTimeout(lineBufSyncTimer)
    lineBufSyncTimer = setTimeout(() => {
      lineBufSyncTimer = null
      const term = opts.term()
      if (!term || opts.tab.status !== 'connected') return
      const {cmd} = readScreenCommand(term)
      // 命令执行后输出刷屏时勿把输出并进 lineBuf
      if (!cmd || looksLikeCommandPollution(cmd)) return
      lineBuf = cmd
      if (opts.settings.completionEnabled) scheduleSuggest()
    }, 40)
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
        const term = opts.term()
        const cmd = term ? sanitizeHistoryCommand(resolveExecutedCommand(term, lineBuf)) : ''
        if (cmd) historyService.add(opts.tab.node.id, cmd)
        // 回显行（prompt + 命令）不着色：`cat error.log` 这类命令本身含关键字，整行红会误导
        if (cmd) opts.colorize.noteCommand(cmd)
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
    const term = opts.term()
    if (!item || !opts.tab.sessionId || !term) return
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
    opts.write(suffix)
    // 等 shell 回显后校正（退格/宽字符极端情况）
    scheduleLineBufSyncFromScreen()
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

    const hotkey = opts.settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY
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

  /** 新连接 / 重连开始时清空输入跟踪与候选。 */
  function resetInput() {
    lineBuf = ''
    escState = 'none'
    if (lineBufSyncTimer) {
      clearTimeout(lineBufSyncTimer)
      lineBufSyncTimer = null
    }
    if (suggestTimer) {
      clearTimeout(suggestTimer)
      suggestTimer = null
    }
    hideSuggestions()
  }

  function dispose() {
    if (suggestTimer) clearTimeout(suggestTimer)
    if (lineBufSyncTimer) clearTimeout(lineBufSyncTimer)
    suggestTimer = null
    lineBufSyncTimer = null
  }

  return {
    suggestions: suggestions as Ref<Suggestion[]>,
    selectedIdx,
    navigating,
    panelPos,
    panelEl,
    hideSuggestions,
    enterNavigation,
    refreshSuggestions,
    scheduleSuggest,
    trackLineInput,
    acceptSuggestion,
    handleCompletionKey,
    resetInput,
    dispose,
  }
}
