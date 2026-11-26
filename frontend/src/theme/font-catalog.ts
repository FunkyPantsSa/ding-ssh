/** Default terminal font scheme; falls back to the offline bundled set before remote download completes. */
export const DEFAULT_TERMINAL_FONT = 'JetBrains Mono + Sarasa Mono SC'
export const BUNDLED_FONT_FAMILIES = ['Geist Sans', 'Geist Mono', 'Noto Sans SC'] as const
export const REMOTE_FONT_FAMILIES = ['JetBrains Mono', 'Fira Code', 'Maple Mono NF CN', 'Sarasa Mono SC'] as const

export function isRemoteFont(family: string): boolean {
  return (REMOTE_FONT_FAMILIES as readonly string[]).includes(family)
}

/** Non-CJK intervals so English/icon fonts keep arrows, box-drawing, and Nerd Font glyphs while yielding CJK to the Chinese font. */
const NON_CJK_RANGES: readonly [number, number][] = [
  [0x0000, 0x2e7f],
  [0x4dc0, 0x4dff],
  [0xa000, 0xabff],
  [0xd7b0, 0xf8ff],
  [0xfb00, 0xfe0f],
  [0xfe20, 0xfe2f],
  [0xfe70, 0xfeff],
  [0xfff0, 0x1ffff],
  [0x2fa20, 0x10ffff],
]

const LATIN_RANGES: readonly [number, number][] = [
  [0x0000, 0x024f],
  [0x1e00, 0x1eff],
]

function intersectUnicodeRanges(unicodeRange: string | undefined, allowed: readonly [number, number][], fallback: string): string | undefined {
  if (!unicodeRange) return fallback
  const ranges: string[] = []
  for (const part of unicodeRange.split(',')) {
    const match = part.trim().match(/^U\+([\da-f?]+)(?:-([\da-f]+))?$/i)
    if (!match) continue
    const start = parseInt(match[1].replace(/\?/g, '0'), 16)
    const end = parseInt(match[2] ?? match[1].replace(/\?/g, 'F'), 16)
    for (const [low, high] of allowed) {
      const a = Math.max(start, low), b = Math.min(end, high)
      if (a <= b) ranges.push(a === b ? `U+${a.toString(16)}` : `U+${a.toString(16)}-${b.toString(16)}`)
    }
  }
  return ranges.length ? ranges.join(',') : undefined
}

/** Latin aliases must not claim characters belonging to a different CJK shard. */
export function latinUnicodeRange(unicodeRange?: string): string | undefined {
  return intersectUnicodeRanges(unicodeRange, LATIN_RANGES, 'U+0000-024F,U+1E00-1EFF')
}

export const DEFAULT_NON_CJK_RANGE = NON_CJK_RANGES.map(([low, high]) => `U+${low.toString(16)}-${high.toString(16)}`).join(',')

/** Exclude CJK ideographs/kana/hangul/fullwidth punctuation while preserving Latin, symbols, box-drawing, and Nerd Font PUA glyphs. */
export function nonCjkUnicodeRange(unicodeRange?: string): string | undefined {
  return intersectUnicodeRanges(unicodeRange, NON_CJK_RANGES, DEFAULT_NON_CJK_RANGE)
}
