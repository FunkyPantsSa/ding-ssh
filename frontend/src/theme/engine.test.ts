import {describe, expect, it} from 'vitest'
import {isManagedTerminalBackground, computeUiCssVars, deriveBrandVars, deriveSurfaceVars, deriveTerminalTheme, monoFontStack, resolveTone, uiFontStack} from './engine'
import {paletteToTheme, defaultPreset} from './presets'

describe('theme engine', () => {
  it('recognizes old generated terminal backgrounds and preserves independent choices', () => {
    expect(isManagedTerminalBackground('#435e70', '#71b5d2', 'dark')).toBe(false)
    expect(isManagedTerminalBackground('#223550', '#71b5d2', 'dark')).toBe(true)
    expect(isManagedTerminalBackground('#fafbfc', '#ff805c', 'dark')).toBe(true)
    expect(isManagedTerminalBackground('#123456', '#71b5d2', 'dark')).toBe(false)
  })
  it('keeps dark background regions close in brightness and low in chroma', () => {
    const vars = deriveSurfaceVars('#2f41ae', 'dark')
    const channels = (hex: string) => [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16))
    const main = channels(vars['--theme-bg-main'])
    const side = channels(vars['--theme-bg-sidebar'])
    expect(Math.max(...main) - Math.min(...main)).toBeLessThan(24)
    expect(main.every((value, i) => Math.abs(value - side[i]) < 12)).toBe(true)
  })
  it('exposes translucent surfaces only while desktop glass is enabled', () => {
    const appearance = {mode: 'custom' as const, baseTone: 'dark' as const, presetId: 'geist', primary: '#3b82f6', secondary: '#3b82f6', uiText: '#fff', glassOpacity: 65, glassBlur: 24, glassEnabled: true}
    const fonts = {uiFont: 'system', terminalFont: 'system', terminalFontSize: 13}
    const glass = computeUiCssVars(appearance, fonts, 'dark')
    expect(glass['--theme-bg-sidebar']).toContain('rgba(')
    expect(glass['--terminal-bg']).toContain('rgba(')
    const solid = computeUiCssVars({...appearance, glassEnabled: false}, fonts, 'dark')
    expect(solid['--background']).toMatch(/^#[0-9a-f]{6}$/)
    expect(solid['--theme-glass-opacity']).toBe('100%')
  })
  it('derives semantic brand variables without legacy aliases', () => {
    const vars = deriveBrandVars('#3b82f6', '#8b5cf6')

    expect(vars).toMatchObject({
      '--primary': '#3b82f6',
      '--secondary': '#8b5cf6',
      '--primary-foreground': '#ffffff',
    })
    expect(Object.keys(vars).every((key) => !key.includes('ink') && !key.includes('mist'))).toBe(true)
  })

  it('uses deterministic font stacks and leaves text contrast to the active tone', () => {
    expect(uiFontStack('system')).toContain('PingFang SC')
    expect(monoFontStack('JetBrains Mono')).toContain('JetBrains Mono')
    const vars = computeUiCssVars(
      {mode: 'custom', baseTone: 'dark', presetId: 'default', primary: '#000000', secondary: '#ffffff', uiText: '#eeeeee', glassOpacity: 85, glassBlur: 24},
      {uiFont: 'system', terminalFont: 'system', terminalFontSize: 14},
      'dark',
    )
    expect(vars['--foreground']).toMatch(/^#[0-9a-f]{6}$/)
  })

  it('keeps light glass luminous and derives text and selection from the seed', () => {
    const appearance = {mode: 'custom' as const, baseTone: 'light' as const, presetId: 'geist', primary: '#498df0', secondary: '#498df0', uiText: '#fff', glassOpacity: 40, glassBlur: 0, glassEnabled: true}
    const fonts = {uiFont: 'system', terminalFont: 'system', terminalFontSize: 13}
    const blue = computeUiCssVars(appearance, fonts, 'light')
    const rose = computeUiCssVars({...appearance, primary: '#f15e6a'}, fonts, 'light')
    expect(blue['--theme-bg-sidebar']).toContain('rgba(')
    expect(blue['--terminal-bg']).toContain('rgba(')
    expect(blue['--theme-bg-toolbar']).not.toBe(blue['--theme-bg-sidebar'])
    for (const key of ['--foreground', '--hover-strong']) expect(blue[key]).not.toBe(rose[key])
  })

  it('keeps glass cards translucent and restores solid surfaces when disabled', () => {
    const appearance = {mode: 'custom' as const, baseTone: 'light' as const, presetId: 'geist', primary: '#498df0', secondary: '#498df0', uiText: '#fff', glassOpacity: 40, glassBlur: 0, glassEnabled: true}
    const fonts = {uiFont: 'system', terminalFont: 'system', terminalFontSize: 13}
    for (const tone of ['light', 'dark'] as const) {
      const glass = computeUiCssVars(appearance, fonts, tone)
      expect(parseFloat(glass['--theme-glass-opacity']!)).toBeLessThan(100)
      expect(glass['--theme-bg-surface']).toContain('rgba(')
      const solid = computeUiCssVars({...appearance, glassEnabled: false}, fonts, tone)
      expect(solid['--theme-glass-opacity']).toBe('100%')
      expect(solid['--theme-glass-blur']).toBe('0px')
      expect(solid['--theme-bg-surface']).not.toContain('rgba(')
    }
  })

  it('resolves explicit tone without consulting system preferences', () => {
    expect(resolveTone('light')).toBe('light')
    expect(resolveTone('dark')).toBe('dark')
  })

  it('creates distinct sidebar and main surfaces for both tones', () => {
    const light = deriveSurfaceVars('#ff805c', 'light')
    const dark = deriveSurfaceVars('#71b5d2', 'dark')
    expect(light['--theme-bg-sidebar']).not.toBe(light['--theme-bg-main'])
    expect(dark['--theme-bg-sidebar']).not.toBe(dark['--theme-bg-main'])
    expect(light['--theme-glass-blur']).toBe('24px')
  })

  it('tints terminal background, cursor and blue ANSI colors from the seed', () => {
    const base = paletteToTheme(defaultPreset().dark)
    const next = deriveTerminalTheme(base, '#FF805C', 'dark')
    expect(next.background).not.toBe(base.background)
    expect(next.cursor).not.toBe(base.cursor)
    expect(next.blue).toBe('#FF805C')
    expect(next.red).toBe(base.red)
    const light = deriveTerminalTheme(base, '#ff805c', 'light')
    expect(light.cyan).toBe('#087582')
    expect(light.green).toBe('#227447')
  })
})
