import {afterEach, describe, expect, it} from 'vitest'
import {
  applyRootTheme,
  clearRootTheme,
  readThemeCache,
  THEME_CACHE_KEY,
  writeThemeCache,
  type ComputedTheme,
} from './engine'
import {deriveColorRoles, planMaterial, rolesToCssVars} from './material'
import type {ThemeTone} from './material'

function computed(tone: ThemeTone = 'dark'): ComputedTheme {
  const safeRoles = deriveColorRoles({seed: '#A78BFA', tone})
  const plan = planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos'})
  return {
    vars: rolesToCssVars(safeRoles, plan),
    plan,
    roles: safeRoles,
    meta: {
      tone,
      requestedMode: 'soft',
      materialMode: 'soft',
      fallback: 'none',
      nativeBackdrop: true,
      platform: 'macos',
    },
  }
}

afterEach(() => {
  clearRootTheme()
  localStorage.clear()
})

describe('root theme application', () => {
  it('writes semantic variables and material attributes to <html>', () => {
    const theme = computed()
    applyRootTheme('dark', theme.vars, theme.meta)
    const root = document.documentElement
    expect(root.getAttribute('data-tone')).toBe('dark')
    expect(root.getAttribute('data-material')).toBe('soft')
    expect(root.getAttribute('data-glass')).toBe('on')
    expect(root.getAttribute('data-native-backdrop')).toBe('on')
    expect(root.getAttribute('data-theme-platform')).toBe('macos')
    expect(root.style.getPropertyValue('--sidebar-primary-bg')).toContain('rgba(')
    expect(root.style.getPropertyValue('--sidebar-primary-gradient')).toContain('linear-gradient(180deg')
    expect(root.style.getPropertyValue('--workspace-gradient')).toContain('radial-gradient')
    expect(root.style.getPropertyValue('--workspace-gradient')).toContain(root.style.getPropertyValue('--workspace-bg'))
    expect(root.style.getPropertyValue('--terminal-bg')).toMatch(/^#[0-9a-f]{6}$/)
    expect(root.style.colorScheme).toBe('dark')
  })

  it('removes stale variables when the next theme omits them', () => {
    const first = computed()
    applyRootTheme('dark', first.vars, first.meta)
    const solidRoles = deriveColorRoles({seed: '#A78BFA', tone: 'light'})
    const solidPlan = planMaterial({mode: 'solid', nativeBackdrop: false, platform: 'web'})
    applyRootTheme('light', rolesToCssVars(solidRoles, solidPlan), {
      tone: 'light',
      requestedMode: 'soft',
      materialMode: 'solid',
      fallback: 'unsupported-platform',
      nativeBackdrop: false,
      platform: 'web',
    })
    const root = document.documentElement
    expect(root.getAttribute('data-material')).toBe('solid')
    expect(root.getAttribute('data-material-fallback')).toBe('unsupported-platform')
    expect(root.style.getPropertyValue('--sidebar-primary-bg')).toMatch(/^#[0-9a-f]{6}$/)
  })

  it('clears all managed attributes', () => {
    const theme = computed()
    applyRootTheme('dark', theme.vars, theme.meta)
    clearRootTheme()
    const root = document.documentElement
    expect(root.getAttribute('data-tone')).toBeNull()
    expect(root.getAttribute('data-material')).toBeNull()
    expect(root.style.getPropertyValue('--sidebar-primary-bg')).toBe('')
    expect(root.style.getPropertyValue('--workspace-gradient')).toBe('')
  })
})

describe('startup cache', () => {
  it('round-trips the last computed theme', () => {
    const theme = computed()
    writeThemeCache('dark', theme)
    const loaded = readThemeCache()
    expect(loaded?.tone).toBe('dark')
    expect(loaded?.material).toBe('soft')
    expect(loaded?.vars['--workspace-bg']).toBe(theme.vars['--workspace-bg'])
  })

  it('ignores corrupt or mismatched payloads', () => {
    localStorage.setItem(THEME_CACHE_KEY, '{oops')
    expect(readThemeCache()).toBeNull()
    localStorage.setItem(THEME_CACHE_KEY, JSON.stringify({version: 2, vars: {}}))
    expect(readThemeCache()).toBeNull()
  })
})
