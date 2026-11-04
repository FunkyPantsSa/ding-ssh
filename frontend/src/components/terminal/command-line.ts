import type {Terminal} from '@xterm/xterm'

const PROMPT_MARKERS = ['# ', '$ ', '% ', '> ']

/** 清洗一条即将写入命令历史的输入：去掉 ANSI/控制序列与输出污染。 */
export function sanitizeHistoryCommand(cmd: string): string {
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

/** 读取 buffer 某一绝对行的可见文本。 */
export function readLineTextAt(term: Terminal, absY: number): string {
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

export function findPromptCut(line: string): number {
  let cut = -1
  for (const m of PROMPT_MARKERS) {
    const i = line.lastIndexOf(m)
    if (i >= 0) cut = Math.max(cut, i + m.length)
  }
  return cut
}

/** 是否把命令输出 / 二次 prompt 粘进了「命令」串。 */
export function looksLikeCommandPollution(cmd: string): boolean {
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
export function trimPollutedCommand(cmd: string): string {
  if (!cmd) return ''
  const idxs = [
    cmd.search(/warning\s*:/i),
    cmd.search(/\bNAME\s+STATUS\b/),
    cmd.search(/[a-z0-9](?=warning\s*:)/i),
    cmd.search(/[a-z0-9](?=NAME\s+STATUS)/),
    cmd.search(/[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+:[~/]/),
  ].filter((i) => i > 0)
  const s = idxs.length ? cmd.slice(0, Math.min(...idxs)) : cmd
  return s.replace(/\s+$/, '').trim()
}

/**
 * 只读当前输入逻辑行：沿 xterm soft-wrap（isWrapped）向上拼。
 * 禁止跨过输出区拼到上一条 prompt（否则会变成 kubectl…NAME…root@…）。
 */
export function readScreenCommand(term: Terminal): {cmd: string; viaPrompt: boolean} {
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
    text += readLineTextAt(term, y)
  }
  text = text.replace(/\s+$/, '')

  const cut = findPromptCut(text)
  if (cut >= 0) {
    return {cmd: text.slice(cut).replace(/^\s+/, '').replace(/\s+$/, ''), viaPrompt: true}
  }
  return {cmd: text.replace(/^\s+/, ''), viaPrompt: false}
}

/** tracked 去空白后是否为 screen 子序列（Tab 补全后 lineBuf 缺 ctl 等字符）。 */
export function isLooseSubsequence(tracked: string, screen: string): boolean {
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
export function resolveExecutedCommand(term: Terminal, lineBuf: string): string {
  const tracked = looksLikeCommandPollution(lineBuf) ? trimPollutedCommand(lineBuf) : lineBuf
  let {cmd: fromScreen, viaPrompt} = readScreenCommand(term)
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
