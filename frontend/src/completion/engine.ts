import type {Terminal} from '@xterm/xterm'
import {STATIC_DICT} from './dict'
import {fuzzyFilter} from './fzf'
import {Trie} from './trie'

export interface Suggestion {
  command: string
  source: 'history' | 'dict' | 'screen'
  count?: number
}

const dictTrie = new Trie()
dictTrie.insertMany(STATIC_DICT)

const TOKEN_RE = /[A-Za-z0-9_./:@+-]{2,}/g

/** kubectl 表头等屏幕噪声，不当作补全候选。 */
const SCREEN_NOISE = new Set([
  'NAME', 'READY', 'STATUS', 'RESTARTS', 'AGE', 'NAMESPACE', 'TYPE',
  'CLUSTER-IP', 'EXTERNAL-IP', 'PORT', 'PORTS', 'CAPACITY', 'ALLOCATABLE',
  'VERSION', 'INTERNAL-IP', 'OS-IMAGE', 'KERNEL-VERSION', 'CONTAINER-RUNTIME',
  'ROLES', 'CPU', 'MEMORY', 'EPHEMERAL-STORAGE', 'HUGEPAGES',
])

/** 优先保留路径、主机名、K8s 风格资源名等 token。 */
function isPreferredScreenToken(t: string): boolean {
  if (t.includes('/')) return true
  if (t.includes('@')) return true
  if (t.includes(':') && /[A-Za-z]/.test(t)) return true
  // pod / deployment 风格：含连字符的标识
  if (/-/.test(t) && /[A-Za-z]/.test(t) && t.length >= 4) return true
  // 带点的主机名 / FQDN
  if (/\.[A-Za-z]/.test(t) && t.length >= 4) return true
  return false
}

/** 从当前可视缓冲区提取路径 / Pod 名等词（非整行命令；整行来自历史）。 */
export function extractScreenWords(term: Terminal): string[] {
  const buf = term.buffer.active
  const preferred = new Set<string>()
  const other = new Set<string>()
  const start = buf.viewportY
  const end = Math.min(start + term.rows, buf.length)
  for (let y = start; y < end; y++) {
    const line = buf.getLine(y)
    if (!line) continue
    const text = line.translateToString(true)
    const matches = text.match(TOKEN_RE)
    if (!matches) continue
    for (const m of matches) {
      if (m.length < 2 || m.length > 120) continue
      if (SCREEN_NOISE.has(m) || SCREEN_NOISE.has(m.toUpperCase())) continue
      if (isPreferredScreenToken(m)) preferred.add(m)
      else if (m.length >= 3) other.add(m)
    }
  }
  // 有足够「路径/资源名」时只返回它们，避免屏幕噪声挤占面板
  if (preferred.size >= 2) return [...preferred]
  return [...preferred, ...other]
}

/** 合并三级词库：历史频次 > 屏幕上下文 > 静态字典，最多 limit 条。 */
export function mergeSuggestions(
  prefix: string,
  history: Suggestion[],
  screenWords: string[],
  limit = 8,
): Suggestion[] {
  const seen = new Set<string>()
  const out: Suggestion[] = []
  const max = Math.max(3, Math.min(30, limit || 8))

  const push = (s: Suggestion) => {
    if (!s.command || seen.has(s.command)) return
    seen.add(s.command)
    out.push(s)
  }

  for (const h of history) {
    if (out.length >= max) break
    push({...h, source: 'history'})
  }

  if (out.length < max && prefix) {
    const screenHits = fuzzyFilter(screenWords, prefix, max)
    for (const hit of screenHits) {
      if (out.length >= max) break
      // 屏幕词通常补全最后一个 token
      push({command: hit.text, source: 'screen'})
    }
  }

  if (out.length < max) {
    const trieHits = dictTrie.prefixSearch(prefix, max)
    for (const cmd of trieHits) {
      if (out.length >= max) break
      push({command: cmd, source: 'dict'})
    }
  }

  // 前缀树无命中时，对静态字典做模糊匹配兜底
  if (out.length < max && prefix.length >= 2) {
    const fuzzyHits = fuzzyFilter(STATIC_DICT, prefix, max)
    for (const hit of fuzzyHits) {
      if (out.length >= max) break
      push({command: hit.text, source: 'dict'})
    }
  }

  return out.slice(0, max)
}

/** 取当前输入行中用于匹配的前缀（整行；屏幕/字典可再取末 token）。 */
export function completionPrefix(line: string): {full: string; token: string} {
  const full = line.replace(/^\s+/, '')
  const m = full.match(/^(.*?)(\S*)$/)
  const token = m?.[2] ?? full
  return {full, token}
}

/** 采纳补全：返回应追加写入终端的后缀（不含已输入部分）。 */
export function acceptSuffix(line: string, suggestion: string): string {
  const {full, token} = completionPrefix(line)
  // 历史整行匹配：用 suggestion 替换当前整行剩余
  if (suggestion.startsWith(full) && full.length > 0) {
    return suggestion.slice(full.length)
  }
  // token 级：替换末 token
  if (token && (suggestion.startsWith(token) || fuzzyLikely(token, suggestion))) {
    const before = full.slice(0, full.length - token.length)
    const desired = before + suggestion
    if (desired.startsWith(full)) return desired.slice(full.length)
    // 需要先退格再写入
    return '\x7f'.repeat(token.length) + suggestion
  }
  // 无法对齐时整行替换，禁止 suggestion.slice(full.length) 误拼接（kube+get→kubeget）
  if (full.length > 0) return '\x7f'.repeat(full.length) + suggestion
  return suggestion
}

function fuzzyLikely(token: string, suggestion: string): boolean {
  if (!token) return true
  return suggestion.toLowerCase().includes(token.toLowerCase()) ||
    fuzzyFilter([suggestion], token, 1).length > 0
}
