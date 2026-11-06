import {afterEach, expect, it, vi} from 'vitest'
import {loadTerminalFonts, fontPair, encodeFontPair, terminalFontFamilies} from './terminal-fonts'
vi.mock('./remote-fonts', () => ({ensureRemoteFont: vi.fn().mockResolvedValue(undefined)}))

afterEach(() => vi.unstubAllGlobals())

it('waits for Chinese and bold faces before the terminal can rebuild its atlas', async () => {
  const ready: (() => void)[] = []
  const load = vi.fn((_font: string, _text: string) => new Promise<void>(resolve => ready.push(resolve)))
  vi.stubGlobal('document', {fonts: {load}})
  let finished = false
  const loading = loadTerminalFonts('JetBrains Mono + Sarasa Mono SC', 16, 500).then(() => { finished = true })
  await Promise.resolve()
  expect(load.mock.calls.map(call => call[0])).toEqual([
    '500 16px "JetBrains Mono"', '700 16px "JetBrains Mono"',
    '500 16px "Sarasa Mono SC"', '700 16px "Sarasa Mono SC"',
  ])
  ready.slice(0, 3).forEach(resolve => resolve())
  await Promise.resolve()
  expect(finished).toBe(false)
  ready[3]()
  await loading
  expect(finished).toBe(true)
})


it('keeps English and Chinese choices independent and preserves legacy schemes', () => {
  const name = encodeFontPair('Maple Mono NF CN', 'Sarasa Mono SC')
  expect(fontPair(name)).toEqual(['Maple Mono NF CN', 'Sarasa Mono SC'])
  expect(terminalFontFamilies(name)).toEqual(['DingLatin-Maple Mono NF CN', 'Sarasa Mono SC'])
  expect(fontPair('JetBrains Mono + Sarasa Mono SC')).toEqual(['JetBrains Mono', 'Sarasa Mono SC'])
})
