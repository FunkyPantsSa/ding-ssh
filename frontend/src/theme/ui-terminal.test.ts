import {describe, it, expect} from 'vitest'
import {effectScope, nextTick, reactive} from 'vue'
import {useTheme} from '../composables/useTheme'
import {defaultAppearance, defaultFonts, defaultTheme} from '../stores/settings'
import {clearRootTheme, computeUiCssVars, resolveTone} from './engine'
import {systemTone} from '../composables/useSystemTone'
import type {ThemeCapabilities} from './material'

const NATIVE: ThemeCapabilities = {platform: 'macos', wails: true, nativeBackdrop: true}

function applyCacheSeed(root: HTMLElement) {
  root.setAttribute('data-tone', 'light')
  root.style.setProperty('--workspace-bg', '#eeeeee')
}

describe('UI and terminal independence', () => {
  it('does not write terminal colors when seed, tone or UI preset changes', async () => {
    let writes = 0
    const settings = reactive({
      loaded: true,
      appearance: defaultAppearance(),
      fonts: defaultFonts(),
      theme: defaultTheme(),
      async setTheme() { writes++ },
    })
    const original = JSON.stringify(settings.theme)
    const scope = effectScope()
    scope.run(() => useTheme(settings))
    settings.appearance.primary = '#ff805c'
    settings.appearance.baseTone = 'light'
    settings.appearance.presetId = 'sky'
    await nextTick()
    expect(writes).toBe(0)
    expect(JSON.stringify(settings.theme)).toBe(original)
    scope.stop()
  })

  it('controls overlay blur independently of surface opacity and never fogs the terminal', () => {
    const appearance = {
      ...defaultAppearance(),
      mode: 'custom' as const,
      primary: '#71b5d2',
      glassEnabled: true,
      materialMode: 'soft' as const,
      glassOpacity: 85,
    }
    const a = computeUiCssVars({...appearance, glassBlur: 0}, defaultFonts(), 'light', NATIVE)
    const b = computeUiCssVars({...appearance, glassBlur: 45}, defaultFonts(), 'light', NATIVE)
    expect(a['--theme-bg-sidebar']).toBe(b['--theme-bg-sidebar'])
    expect(parseFloat(a['--glass-opacity']!)).toBeLessThan(100)
    expect(a['--glass-overlay-blur']).toBe('0px')
    expect(b['--glass-overlay-blur']).toBe('45px')
    // The shell itself never gets a simulated desktop blur.
    expect(a['--glass-shell-blur']).toBe('0px')
    expect(b['--glass-shell-blur']).toBe('0px')
    expect(a['--terminal-bg']).toMatch(/^#[0-9a-f]{6}$/)
  })

  it('reacts to shared OS light/dark changes in auto mode', async () => {
    const root = document.documentElement
    const previous = systemTone.value
    const settings = reactive({
      loaded: true,
      appearance: {...defaultAppearance(), baseTone: 'auto' as const},
      fonts: defaultFonts(),
      theme: defaultTheme(),
      async setTheme() {},
    })
    const scope = effectScope()
    scope.run(() => useTheme(settings))
    systemTone.value = 'dark'
    await nextTick()
    expect(root.getAttribute('data-tone')).toBe('dark')
    systemTone.value = 'light'
    await nextTick()
    expect(root.getAttribute('data-tone')).toBe('light')
    scope.stop()
    clearRootTheme()
    systemTone.value = previous ?? resolveTone('auto')
  })

  it('does not overwrite the bootstrap cache before settings finish loading', async () => {
    const root = document.documentElement
    applyCacheSeed(root)
    const settings = reactive({
      loaded: false,
      appearance: {...defaultAppearance(), baseTone: 'dark' as const, primary: '#FACC15'},
      fonts: defaultFonts(),
      theme: defaultTheme(),
      async setTheme() {},
    })
    const scope = effectScope()
    scope.run(() => useTheme(settings))
    await nextTick()
    expect(root.getAttribute('data-tone')).toBe('light')
    expect(root.style.getPropertyValue('--workspace-bg')).toBe('#eeeeee')
    settings.loaded = true
    await nextTick()
    expect(root.getAttribute('data-tone')).toBe('dark')
    expect(root.style.getPropertyValue('--workspace-bg')).toMatch(/^#[0-9a-f]{6}$/)
    scope.stop()
    clearRootTheme()
  })

  it('keeps the terminal opaque even when the shell uses clear glass', () => {
    const appearance = {
      ...defaultAppearance(),
      mode: 'custom' as const,
      primary: '#EF4444',
      materialMode: 'clear' as const,
      glassEnabled: true,
      glassOpacity: 65,
    }
    const vars = computeUiCssVars(appearance, defaultFonts(), 'dark', NATIVE)
    expect(vars['--theme-bg-surface']).toContain('rgba(')
    expect(vars['--terminal-bg']).toMatch(/^#[0-9a-f]{6}$/)
  })
})
