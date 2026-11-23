import {readFileSync} from 'node:fs'
import {createRequire} from 'node:module'
import {resolve} from 'node:path'
import {expect, it, vi} from 'vitest'
import {BUNDLED_FONT_FAMILIES, DEFAULT_TERMINAL_FONT, isRemoteFont, latinUnicodeRange, REMOTE_FONT_FAMILIES} from './font-catalog'

it('ships only the offline font set as WOFF2 while preserving every Chinese range', async () => {
  const {bundledFontCss} = await vi.importActual<{bundledFontCss(): string}>('../../build/bundled-fonts')
  const css = bundledFontCss()
  const require = createRequire(import.meta.url)
  const noto = readFileSync(require.resolve('@fontsource/noto-sans-sc/400.css'), 'utf8')
  const families = [...new Set([...css.matchAll(/font-family:\s*'([^']+)'/g)].map(match => match[1]))]
  expect(families).toEqual([...BUNDLED_FONT_FAMILIES])
  expect(css).not.toMatch(/\.woff["')]/)
  expect(css).not.toMatch(/https?:/)
  expect(css.match(/unicode-range:[^;]+/g)).toEqual(noto.match(/unicode-range:[^;]+/g))
  for (const family of REMOTE_FONT_FAMILIES) expect(css).not.toContain(`'${family}'`)
})

it('keeps frontend and native defaults aligned with the bundled set', () => {
  const frontend = readFileSync(resolve('src/stores/settings.ts'), 'utf8')
  const backend = readFileSync(resolve('../internal/models/models.go'), 'utf8')
  expect(frontend).toContain('terminalFont: DEFAULT_TERMINAL_FONT')
  expect(backend).toContain(`TerminalFont:     "${DEFAULT_TERMINAL_FONT}"`)
  for (const family of DEFAULT_TERMINAL_FONT.split(' + ')) expect(isRemoteFont(family)).toBe(false)
})

it('restricts aliases to the Latin characters actually covered by each shard', () => {
  expect(latinUnicodeRange()).toBe('U+0000-024F,U+1E00-1EFF')
  expect(latinUnicodeRange('U+4E00-9FFF')).toBeUndefined()
  expect(latinUnicodeRange('U+0-FF,U+4E00-9FFF')).toBe('U+0-ff')
  expect(latinUnicodeRange('U+1E??')).toBe('U+1e00-1eff')
})
