import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {DEFAULT_TERMINAL_FONT} from './font-catalog'

vi.mock('./remote-fonts', () => ({ensureRemoteFont: vi.fn().mockResolvedValue(undefined)}))

beforeEach(() => vi.resetModules())
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

it('waits for Chinese and bold faces before the terminal can rebuild its atlas', async () => {
  const {loadTerminalFonts} = await import('./terminal-fonts')
  const ready: (() => void)[] = []
  const load = vi.fn((font: string) => font.includes('Geist Mono')
    ? Promise.resolve([])
    : new Promise<void>(resolve => ready.push(resolve)))
  vi.stubGlobal('document', {fonts: {load, forEach: vi.fn()}})
  let finished = false
  const loading = loadTerminalFonts('JetBrains Mono + Sarasa Mono SC', 16, 500).then(() => { finished = true })
  await vi.waitFor(() => expect(ready).toHaveLength(4))
  expect(load.mock.calls.map(call => call[0])).toEqual([
    '500 16px "Geist Mono"', '700 16px "Geist Mono"',
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

it('loads every bundled Chinese shard, including glyphs outside the preview text', async () => {
  const {loadTerminalFonts} = await import('./terminal-fonts')
  let release!: () => void
  const rare = {family: '"Noto Sans SC"', load: vi.fn(() => new Promise<void>(resolve => { release = resolve }))}
  const common = {family: 'Noto Sans SC', load: vi.fn().mockResolvedValue(undefined)}
  const unrelated = {family: 'Unselected Font', load: vi.fn()}
  vi.stubGlobal('document', {fonts: {
    load: vi.fn().mockResolvedValue([]),
    forEach: (fn: (face: unknown) => void) => [common, rare, unrelated].forEach(fn),
  }})
  let finished = false
  const loading = loadTerminalFonts(DEFAULT_TERMINAL_FONT, 13, 400).then(() => { finished = true })
  await Promise.resolve()
  expect(common.load).toHaveBeenCalledOnce()
  expect(rare.load).toHaveBeenCalledOnce()
  expect(unrelated.load).not.toHaveBeenCalled()
  expect(finished).toBe(false)
  release()
  await loading
  expect(finished).toBe(true)
  await loadTerminalFonts('system', 16, 500)
  expect(common.load).toHaveBeenCalledOnce()
})

it('prepares the offline fallback before attempting an optional download', async () => {
  const {loadTerminalFonts} = await import('./terminal-fonts')
  const {ensureRemoteFont} = await import('./remote-fonts')
  const load = vi.fn().mockResolvedValue([])
  const chinese = {family: 'Noto Sans SC', load: vi.fn().mockResolvedValue(undefined)}
  vi.stubGlobal('document', {fonts: {load, forEach: (fn: (face: unknown) => void) => fn(chinese)}})
  vi.mocked(ensureRemoteFont).mockImplementation(async () => {
    expect(chinese.load).toHaveBeenCalledOnce()
    expect(load).toHaveBeenCalledWith('400 13px "Geist Mono"')
    expect(load).toHaveBeenCalledWith('700 13px "Geist Mono"')
    throw new Error('offline')
  })
  await expect(loadTerminalFonts('Fira Code', 13, 400)).rejects.toThrow('offline')
})

it('keeps English and Chinese choices independent and preserves legacy schemes', async () => {
  const {fontPair, encodeFontPair, isSameTerminalFont, terminalFontFamilies} = await import('./terminal-fonts')
  const name = encodeFontPair('Maple Mono NF CN', 'Sarasa Mono SC')
  expect(fontPair(name)).toEqual(['Maple Mono NF CN', 'Sarasa Mono SC'])
  expect(terminalFontFamilies(name)).toEqual(['DingLatin-Maple Mono NF CN', 'Sarasa Mono SC'])
  expect(fontPair('JetBrains Mono + Sarasa Mono SC')).toEqual(['JetBrains Mono', 'Sarasa Mono SC'])
  expect(fontPair(DEFAULT_TERMINAL_FONT)).toEqual(['JetBrains Mono', 'Sarasa Mono SC'])
  expect(fontPair('Geist Mono + Noto Sans SC')).toEqual(['Geist Mono', 'Noto Sans SC'])
  expect(fontPair('Maple Mono NF CN')).toEqual(['Maple Mono NF CN', 'Maple Mono NF CN'])
  expect(encodeFontPair('JetBrains Mono', 'Sarasa Mono SC')).toBe(DEFAULT_TERMINAL_FONT)
  expect(encodeFontPair('Geist Mono', 'Noto Sans SC')).toBe('Geist Mono + Noto Sans SC')
  expect(encodeFontPair('Maple Mono NF CN', 'Maple Mono NF CN')).toBe('Maple Mono NF CN')
  expect(encodeFontPair('system', 'system')).toBe('system')
  expect(isSameTerminalFont('pair:["JetBrains Mono","Sarasa Mono SC"]', DEFAULT_TERMINAL_FONT)).toBe(true)
  expect(isSameTerminalFont('pair:["Geist Mono","Noto Sans SC"]', 'Geist Mono + Noto Sans SC')).toBe(true)
  expect(terminalFontFamilies('pair:["Maple Mono NF CN","Maple Mono NF CN"]')).toEqual(['Maple Mono NF CN'])
  expect(terminalFontFamilies('pair:["system","Sarasa Mono SC"]')).toEqual(['ui-monospace', 'SF Mono', 'Menlo', 'Monaco', 'Consolas', 'Sarasa Mono SC'])
  expect(terminalFontFamilies('pair:["JetBrains Mono","system"]')).toEqual(['DingLatin-JetBrains Mono', 'ui-monospace', 'SF Mono', 'Menlo', 'Monaco', 'Consolas', 'PingFang SC', 'Microsoft YaHei'])
})

it('requests only non-CJK shards when a CJK-capable remote font is paired only for English', async () => {
  const {loadTerminalFonts} = await import('./terminal-fonts')
  const {ensureRemoteFont} = await import('./remote-fonts')
  vi.mocked(ensureRemoteFont).mockResolvedValue(undefined)
  vi.stubGlobal('document', {fonts: {load: vi.fn().mockResolvedValue([]), forEach: vi.fn()}, styleSheets: []})
  await loadTerminalFonts('pair:["Maple Mono NF CN","Noto Sans SC"]', 14, 400)
  expect(vi.mocked(ensureRemoteFont)).toHaveBeenCalledWith('Maple Mono NF CN', 400, {nonCjkOnly: true})
  expect(vi.mocked(ensureRemoteFont)).toHaveBeenCalledWith('Maple Mono NF CN', 700, {nonCjkOnly: true})
  expect(vi.mocked(ensureRemoteFont)).toHaveBeenCalledWith('Noto Sans SC', 400, {nonCjkOnly: false})
})
