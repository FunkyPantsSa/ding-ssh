import {describe, expect, it} from 'vitest'
import {computeUiCssVars, deriveBrandVars, deriveSurfaceVars, deriveTerminalTheme, monoFontStack, resolveTone, uiFontStack} from './engine'
import {paletteToTheme, defaultPreset} from './presets'

describe('theme engine', () => {
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
    expect(vars['--foreground']).toBeUndefined()
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
  })
})
