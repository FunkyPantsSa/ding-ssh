import {restoreCustomFonts} from './custom-fonts'
import {ensureRemoteFont} from './remote-fonts'

export function fontPair(name: string): [string, string] {
  if (name.startsWith('pair:')) {
    try { const pair = JSON.parse(name.slice(5)); if (Array.isArray(pair) && pair.length === 2 && pair.every(v => typeof v === 'string')) return pair as [string, string] } catch { /* Legacy preference. */ }
  }
  const families = TERMINAL_FONT_SCHEMES.find(s => s.value === name)?.families
  return [families?.[0] ?? name, families?.[1] ?? 'Noto Sans SC']
}
export function encodeFontPair(english: string, chinese: string) { return `pair:${JSON.stringify([english, chinese])}` }
const latinLoads = new Map<string, Promise<void>>()
async function loadLatin(family: string, weight: number) {
  if (family === 'system') return
  const key = `${family}:${weight}`
  if (!latinLoads.has(key)) {
    const loading = (async () => {
      for (const sheet of Array.from(document.styleSheets)) {
        try {
          for (const rule of Array.from(sheet.cssRules)) {
            if (!(rule instanceof CSSFontFaceRule) || rule.style.fontFamily.replace(/["']/g, '') !== family) continue
            const alias = await new FontFace(`DingLatin-${family}`, rule.style.getPropertyValue('src'), {weight: rule.style.getPropertyValue('font-weight') || '400', unicodeRange: 'U+0000-024F,U+1E00-1EFF'}).load()
            ;(document.fonts as FontFaceSet & {add(face: FontFace): void}).add(alias)
          }
        } catch { /* Cross-origin sheets cannot be inspected. */ }
      }

    })()
    latinLoads.set(key, loading)
  }
  await latinLoads.get(key)
}
/** Stored values remain strings so existing font preferences stay compatible. */
export const TERMINAL_FONT_SCHEMES = [
  {value: 'Maple Mono NF CN', label: 'Maple Mono NF CN', description: '中英文、终端图标一体化', families: ['Maple Mono NF CN']},
  {value: 'JetBrains Mono + Sarasa Mono SC', label: 'JetBrains Mono + Sarasa Mono SC', description: '清晰易读，适合长期运维', families: ['JetBrains Mono', 'Sarasa Mono SC']},
  {value: 'Geist Mono + Sarasa Mono SC', label: 'Geist Mono + Sarasa Mono SC', description: '简洁现代，适合开发者工具', families: ['Geist Mono', 'Sarasa Mono SC']},
] as const

export function terminalFontFamilies(name: string): readonly string[] {
  if (name.startsWith('pair:')) { const [en, zh] = fontPair(name); return [en === 'system' ? 'ui-monospace' : `DingLatin-${en}`, zh === 'system' ? 'Noto Sans SC' : zh] }
  return TERMINAL_FONT_SCHEMES.find(scheme => scheme.value === name)?.families ?? [name]
}

export async function loadTerminalFonts(name: string, size: number, weight: number): Promise<void> {
  if (name.includes('DingCustom-')) await restoreCustomFonts()
  if (name === 'system') return
  const pair = name.startsWith('pair:') ? fontPair(name) : undefined
  await Promise.all((pair ?? terminalFontFamilies(name)).flatMap(family =>
    [weight, 700].map(async w => {
      await ensureRemoteFont(family, w)
      await document.fonts.load(`${w} ${size}px "${family}"`, '终端中文 English 0123 \ue0b0 \uf120')
    }),
  ))
  if (pair) await Promise.all([weight, 700].map(w => loadLatin(pair[0], w)))
}
