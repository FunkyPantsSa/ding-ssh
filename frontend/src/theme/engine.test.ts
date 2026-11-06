import {describe, expect, it} from 'vitest'
import {contrastRatio} from './material'
import type {ThemeCapabilities} from './material'
import {
  computeUiCssVars,
  deriveBrandVars,
  deriveSurfaceVars,
  deriveTerminalTheme,
  isManagedTerminalBackground,
  monoFontStack,
  resolveTone,
  resolveTerminalForeground,
  resolveTerminalSurface,
  terminalAccentColor,
  terminalContrastForeground,
  terminalSurfaceColor,
  relativeLuminance,
  uiFontStack,
} from './engine'
import {defaultPreset, paletteToTheme} from './presets'

const NATIVE: ThemeCapabilities = {platform: 'macos', wails: true, nativeBackdrop: true}
const PLAIN: ThemeCapabilities = {platform: 'web', wails: false, nativeBackdrop: false}
const FONTS = {uiFont: 'system', terminalFont: 'system', terminalFontSize: 13}

describe('theme engine', () => {
  it('repairs stale terminal foreground against the rendered background without changing readable colors', () => {
    expect(terminalContrastForeground('#e8edf2', '#fafbfc')).toBe('#09090b')
    expect(terminalContrastForeground('#18181b', '#f5f6f7')).toBe('#18181b')
    expect(terminalContrastForeground('#18181b', '#09090b')).toBe('#ffffff')
    expect(terminalContrastForeground('#e8edf2', '#09090b')).toBe('#e8edf2')
  })

  it('recognizes old generated terminal backgrounds and preserves independent choices', () => {
    expect(isManagedTerminalBackground('#435e70', '#71b5d2', 'dark')).toBe(false)
    expect(isManagedTerminalBackground('#223550', '#71b5d2', 'dark')).toBe(true)
    expect(isManagedTerminalBackground('#fafbfc', '#ff805c', 'dark')).toBe(true)
    expect(isManagedTerminalBackground('#123456', '#71b5d2', 'dark')).toBe(false)
  })

  it('gives the dark sidebar a clearer brightness hierarchy', () => {
    const vars = deriveSurfaceVars('#2f41ae', 'dark')
    const channels = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))
    const main = channels(vars['--theme-bg-main'])
    const side = channels(vars['--theme-bg-sidebar'])
    expect(Math.max(...main) - Math.min(...main)).toBeLessThan(28)
    expect(side.reduce((a, b) => a + b, 0)).toBeGreaterThan(main.reduce((a, b) => a + b, 0))
  })

  it('exposes translucent surfaces only when a native backdrop exists', () => {
    const appearance = {
      mode: 'custom' as const,
      baseTone: 'dark' as const,
      presetId: 'geist',
      primary: '#3b82f6',
      secondary: '#3b82f6',
      uiText: '#fff',
      glassOpacity: 85,
      glassBlur: 24,
      glassEnabled: true,
      materialMode: 'soft' as const,
    }
    const glass = computeUiCssVars(appearance, FONTS, 'dark', NATIVE)
    expect(glass['--theme-bg-sidebar']).toContain('rgba(')
    // The terminal stays opaque even when shell glass is enabled.
    expect(glass['--terminal-bg']).toMatch(/^#[0-9a-f]{6}$/)
    expect(glass['--glass-shell-blur']).toBe('0px')

    const unsupported = computeUiCssVars(appearance, FONTS, 'dark', PLAIN)
    expect(unsupported['--theme-bg-sidebar']).toMatch(/^#[0-9a-f]{6}$/)

    const solid = computeUiCssVars({...appearance, materialMode: 'solid', glassEnabled: false}, FONTS, 'dark', NATIVE)
    expect(solid['--background']).toMatch(/^#[0-9a-f]{6}$/)
    expect(solid['--glass-opacity']).toBe('100%')
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
    expect(monoFontStack('Maple Mono NF CN')).toMatch(/^"Maple Mono NF CN",/)
    expect(monoFontStack('JetBrains Mono + Sarasa Mono SC')).toMatch(/^"JetBrains Mono", "Sarasa Mono SC",/)
    expect(monoFontStack('Geist Mono + Sarasa Mono SC')).toMatch(/^"Geist Mono", "Sarasa Mono SC",/)
    const vars = computeUiCssVars(
      {
        mode: 'custom',
        baseTone: 'dark',
        presetId: 'default',
        primary: '#000000',
        secondary: '#ffffff',
        uiText: '#eeeeee',
        glassOpacity: 85,
        glassBlur: 24,
      },
      {uiFont: 'system', terminalFont: 'system', terminalFontSize: 14},
      'dark',
      PLAIN,
    )
    expect(vars['--text-primary']).toMatch(/^#[0-9a-f]{6}$/)
    expect(vars['--text-primary']).toBe(vars['--foreground'])
  })

  it('keeps light glass luminous and derives interactive colors from the seed', () => {
    const appearance = {
      mode: 'custom' as const,
      baseTone: 'light' as const,
      presetId: 'geist',
      primary: '#498df0',
      secondary: '#498df0',
      uiText: '#fff',
      glassOpacity: 85,
      glassBlur: 12,
      glassEnabled: true,
      materialMode: 'soft' as const,
    }
    const blue = computeUiCssVars(appearance, FONTS, 'light', NATIVE)
    const rose = computeUiCssVars({...appearance, primary: '#f15e6a'}, FONTS, 'light', NATIVE)
    expect(blue['--theme-bg-sidebar']).toContain('rgba(')
    expect(blue['--terminal-bg']).toMatch(/^#[0-9a-f]{6}$/)
    expect(blue['--theme-bg-toolbar']).not.toBe(blue['--theme-bg-sidebar'])
    expect(blue['--interactive-active-bg']).not.toBe(rose['--interactive-active-bg'])
    expect(blue['--accent-bg']).not.toBe(rose['--accent-bg'])
  })

  it('keeps glass cards translucent and restores solid surfaces when disabled', () => {
    const appearance = {
      mode: 'custom' as const,
      baseTone: 'light' as const,
      presetId: 'geist',
      primary: '#498df0',
      secondary: '#498df0',
      uiText: '#fff',
      glassOpacity: 85,
      glassBlur: 12,
      glassEnabled: true,
      materialMode: 'soft' as const,
    }
    for (const tone of ['light', 'dark'] as const) {
      const glass = computeUiCssVars(appearance, FONTS, tone, NATIVE)
      expect(parseFloat(glass['--glass-opacity']!)).toBeLessThan(100)
      expect(glass['--theme-bg-surface']).toContain('rgba(')
      const solid = computeUiCssVars(
        {...appearance, materialMode: 'solid', glassEnabled: false},
        FONTS,
        tone,
        NATIVE,
      )
      expect(solid['--glass-opacity']).toBe('100%')
      expect(solid['--glass-overlay-blur']).toBe('0px')
      expect(solid['--theme-bg-surface']).not.toContain('rgba(')
    }
  })

  it('preserves colored opaque surfaces when glass is disabled', () => {
    const appearance = {
      mode: 'custom' as const,
      baseTone: 'light' as const,
      presetId: 'geist',
      primary: '#71b5d2',
      secondary: '#71b5d2',
      uiText: '#fff',
      glassOpacity: 15,
      glassBlur: 24,
      glassEnabled: false,
      materialMode: 'solid' as const,
    }
    const vars = computeUiCssVars(appearance, FONTS, 'light', PLAIN)
    expect(vars['--theme-bg-main']).not.toBe('#ffffff')
    expect(vars['--theme-bg-sidebar']).not.toBe(vars['--theme-bg-main'])
    expect(vars['--theme-bg-toolbar']).not.toBe('#ffffff')
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

  it('coordinates terminal surface and accent with the app tone', () => {
    const lightSurface = terminalSurfaceColor('#71B5D2', 'light', 100, 100)
    const darkSurface = terminalSurfaceColor('#71B5D2', 'dark', 100, 100)
    expect(relativeLuminance(lightSurface)).toBeGreaterThan(relativeLuminance(darkSurface))
    const blue = terminalAccentColor('#71B5D2', 'light', 100, 100)
    const green = terminalAccentColor('#72B69B', 'light', 100, 100)
    expect(blue).not.toBe(green)
    expect(contrastRatio(blue, lightSurface)).toBeGreaterThanOrEqual(3)
  })

  it('keeps an independent terminal scheme byte-for-byte when follow mode is off', () => {
    const scheme = paletteToTheme(defaultPreset().dark)
    const surface = resolveTerminalSurface(false, scheme.background, '#E89AB8', 'dark', 100, 100)
    expect(surface).toBe(scheme.background)
    expect(resolveTerminalForeground(false, scheme.foreground, surface)).toBe(scheme.foreground)
    // Cursor and selection are chosen directly from the scheme by the view;
    // no app accent may be substituted in independent mode.
    expect(scheme.cursor).toBe(defaultPreset().dark.cursor)
    expect(scheme.selection).toBe(defaultPreset().dark.selection)

    const followed = resolveTerminalSurface(true, scheme.background, '#E89AB8', 'dark', 100, 100)
    expect(followed).not.toBe(scheme.background)
    expect(followed).toBe(terminalSurfaceColor('#E89AB8', 'dark', 100, 100))
    expect(resolveTerminalForeground(true, '#18181b', '#09090b')).toBe('#ffffff')
  })

  it('follows the workspace background while preserving terminal colors', () => {
    const base = paletteToTheme(defaultPreset().dark)
    const next = deriveTerminalTheme(base, '#FF805C', 'dark')
    expect(next.background).not.toBe(base.background)
    expect(next.cursor).toBe(base.cursor)
    expect(next.blue).toBe(base.blue)
    expect(next.red).toBe(base.red)
    const light = deriveTerminalTheme(base, '#ff805c', 'light')
    expect(light.cyan).toBe(base.cyan)
    expect(light.green).toBe(base.green)
  })
})

describe('dynamic palette accessibility', () => {
  for (const seed of ['#87CEEB', '#72B69B', '#A78BFA', '#FACC15', '#EF4444', '#F8FAFC', '#111827', '#000000', '#ffffff', '#808080']) {
    for (const tone of ['dark', 'light'] as const) {
      it(`${seed} ${tone}: readable neutral text across surfaces`, () => {
        const vars = deriveSurfaceVars(seed, tone)
        for (const key of ['--background', '--surface-1', '--surface-2', '--hover', '--hover-strong', '--surface-raised-bg']) {
          expect(vars[key]).toMatch(/^#[0-9a-f]{6}$/)
          for (const text of ['--text-primary', '--text-secondary', '--text-muted']) {
            expect(contrastRatio(vars[key]!, vars[text]!)).toBeGreaterThanOrEqual(4.5)
          }
        }
        expect(contrastRatio(vars['--accent-foreground']!, vars['--accent-bg']!)).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(vars['--accent-bg']!, vars['--workspace-bg']!)).toBeGreaterThanOrEqual(3)
      })
    }
  }
})
