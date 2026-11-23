/** The only offline font set shipped with the application. */
export const DEFAULT_TERMINAL_FONT = 'Geist Mono + Noto Sans SC'
export const BUNDLED_FONT_FAMILIES = ['Geist Sans', 'Geist Mono', 'Noto Sans SC'] as const
export const REMOTE_FONT_FAMILIES = ['JetBrains Mono', 'Fira Code', 'Maple Mono NF CN', 'Sarasa Mono SC'] as const

export function isRemoteFont(family: string): boolean {
  return (REMOTE_FONT_FAMILIES as readonly string[]).includes(family)
}

/** Latin aliases must not claim characters belonging to a different CJK shard. */
export function latinUnicodeRange(unicodeRange?: string): string | undefined {
  if (!unicodeRange) return 'U+0000-024F,U+1E00-1EFF'
  const ranges: string[] = []
  for (const part of unicodeRange.split(',')) {
    const match = part.trim().match(/^U\+([\da-f?]+)(?:-([\da-f]+))?$/i)
    if (!match) continue
    const start = parseInt(match[1].replace(/\?/g, '0'), 16)
    const end = parseInt(match[2] ?? match[1].replace(/\?/g, 'F'), 16)
    for (const [low, high] of [[0, 0x24f], [0x1e00, 0x1eff]]) {
      const a = Math.max(start, low), b = Math.min(end, high)
      if (a <= b) ranges.push(`U+${a.toString(16)}-${b.toString(16)}`)
    }
  }
  return ranges.length ? ranges.join(',') : undefined
}
