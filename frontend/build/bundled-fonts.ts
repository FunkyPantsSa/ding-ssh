import {readFileSync} from 'node:fs'
import {createRequire} from 'node:module'
import type {Plugin} from 'vite'

const require = createRequire(import.meta.url)
const id = 'virtual:ding-bundled-fonts.css'
const resolvedId = `\0${id}`
const sources = [
  {name: '@fontsource/geist-sans', styles: ['latin-400', 'latin-500', 'latin-600']},
  {name: '@fontsource/geist-mono', styles: ['latin-400', 'latin-500', 'latin-600', 'latin-700']},
  {name: '@fontsource/noto-sans-sc', styles: ['400']},
]

/** Keep Fontsource's Unicode coverage and licenses, but ship only WOFF2. */
export function bundledFontCss(): string {
  return sources.flatMap(({name, styles}) => styles.map(style =>
    readFileSync(require.resolve(`${name}/${style}.css`), 'utf8')
      .replace(/,\s*url\([^)]*\.woff\)\s*format\(['"]woff['"]\)/g, '')
      .replace(/url\(\.\/files\/([^)]+)\)/g, `url("${name}/files/$1")`),
  )).join('\n')
}

export function bundledFonts(): Plugin {
  return {
    name: 'ding-bundled-fonts',
    resolveId(source) { if (source === id) return resolvedId },
    load(source) { if (source === resolvedId) return bundledFontCss() },
  }
}
