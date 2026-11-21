import {t} from '../i18n'
import {restoreCustomFonts} from './custom-fonts'
import {ensureRemoteFont} from './remote-fonts'
import {DEFAULT_TERMINAL_FONT, latinUnicodeRange} from './font-catalog'

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
            const ruleWeight = rule.style.getPropertyValue('font-weight') || '400'
            if (ruleWeight !== String(weight) && !(family === 'Noto Sans SC' && ruleWeight === '400')) continue
            const range = latinUnicodeRange(rule.style.getPropertyValue('unicode-range'))
            if (!range) continue
            const alias = await new FontFace(`DingLatin-${family}`, rule.style.getPropertyValue('src'), {weight: ruleWeight, unicodeRange: range}).load()
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
  {value: DEFAULT_TERMINAL_FONT, label: DEFAULT_TERMINAL_FONT, get description() { return t("内置中英文字体，离线可用") }, families: ['Geist Mono', 'Noto Sans SC']},
  {value: 'Maple Mono NF CN', label: 'Maple Mono NF CN', get description() { return t("中英文、终端图标一体化") }, families: ['Maple Mono NF CN']},
  {value: 'JetBrains Mono + Sarasa Mono SC', label: 'JetBrains Mono + Sarasa Mono SC', get description() { return t("清晰易读，适合长期运维") }, families: ['JetBrains Mono', 'Sarasa Mono SC']},
  {value: 'Geist Mono + Sarasa Mono SC', label: 'Geist Mono + Sarasa Mono SC', get description() { return t("简洁现代，适合开发者工具") }, families: ['Geist Mono', 'Sarasa Mono SC']},
] as const

let bundledChinese: Promise<void> | undefined
function loadBundledChinese(): Promise<void> {
  if (!bundledChinese) {
    const faces: Promise<FontFace>[] = []
    const fonts = document.fonts as FontFaceSet & {forEach(fn: (face: FontFace) => void): void}
    fonts.forEach(face => {
      if (face.family.replace(/["']/g, '') === 'Noto Sans SC') faces.push(face.load())
    })
    // Load every local shard before xterm draws new Chinese output. Loading only
    // the preview text can leave later glyphs stuck in the fallback atlas.
    const loading = Promise.all(faces).then(() => {}).catch((error: unknown) => {
      bundledChinese = undefined
      throw error
    })
    bundledChinese = loading
  }
  return bundledChinese
}

export function terminalFontFamilies(name: string): readonly string[] {
  if (name.startsWith('pair:')) { const [en, zh] = fontPair(name); return [en === 'system' ? 'ui-monospace' : `DingLatin-${en}`, zh === 'system' ? 'Noto Sans SC' : zh] }
  return TERMINAL_FONT_SCHEMES.find(scheme => scheme.value === name)?.families ?? [name]
}

export async function loadTerminalFonts(name: string, size: number, weight: number): Promise<void> {
  await Promise.all([
    loadBundledChinese(),
    ...[...new Set([weight, 700])].map(w => document.fonts.load(`${w} ${size}px "Geist Mono"`)),
  ])
  if (name.includes('DingCustom-')) await restoreCustomFonts()
  if (name === 'system') return
  const pair = name.startsWith('pair:') ? fontPair(name) : undefined
  await Promise.all((pair ?? terminalFontFamilies(name)).flatMap(family =>
    [...new Set([weight, 700])].map(async w => {
      await ensureRemoteFont(family, w)
      await document.fonts.load(`${w} ${size}px "${family}"`, t("终端中文 English 0123  "))
    }),
  ))
  if (pair) await Promise.all([weight, 700].map(w => loadLatin(pair[0], w)))
}
