import {ensureRemoteFont} from './remote-fonts'

/** Stored values remain strings so existing font preferences stay compatible. */
export const TERMINAL_FONT_SCHEMES = [
  {value: 'Maple Mono NF CN', label: 'Maple Mono NF CN', description: '中英文、终端图标一体化', families: ['Maple Mono NF CN']},
  {value: 'JetBrains Mono + Sarasa Mono SC', label: 'JetBrains Mono + Sarasa Mono SC', description: '清晰易读，适合长期运维', families: ['JetBrains Mono', 'Sarasa Mono SC']},
  {value: 'Geist Mono + Sarasa Mono SC', label: 'Geist Mono + Sarasa Mono SC', description: '简洁现代，适合开发者工具', families: ['Geist Mono', 'Sarasa Mono SC']},
] as const

export function terminalFontFamilies(name: string): readonly string[] {
  return TERMINAL_FONT_SCHEMES.find(scheme => scheme.value === name)?.families ?? [name]
}

export async function loadTerminalFonts(name: string, size: number, weight: number): Promise<void> {
  if (name === 'system') return
  await Promise.all(terminalFontFamilies(name).flatMap(family =>
    [weight, 700].map(async w => {
      await ensureRemoteFont(family, w)
      await document.fonts.load(`${w} ${size}px "${family}"`, '终端中文 English 0123 \ue0b0 \uf120')
    }),
  ))
}
