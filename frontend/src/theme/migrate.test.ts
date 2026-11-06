import {describe, expect, it} from 'vitest'
import {migrateUiSettings, normalizeAppearance, UI_SCHEMA} from './migrate'
import type {Settings, UIAppearance} from '../types'

function settings(patch: Partial<Settings> = {}): Settings {
  return {
    uiSchema: 3,
    theme: {background: '#101010', foreground: '#fafafa'},
    appearance: {
      mode: 'custom',
      presetId: 'sky',
      baseTone: 'dark',
      primary: '#A78BFA',
      secondary: '#A78BFA',
      uiText: '#fafafa',
      glassEnabled: true,
      glassOpacity: 72,
      glassBlur: 30,
      colorIntensity: 55,
    },
    fonts: {uiFont: 'system', terminalFont: 'system', terminalFontSize: 13},
    ...patch,
  } as Settings
}

describe('dynamic theme migration (schema 4)', () => {
  it('leaves schema 4 untouched', () => {
    const input = settings({uiSchema: UI_SCHEMA})
    const result = migrateUiSettings(input, 'dark')
    expect(result.migrated).toBe(false)
    expect(result.settings).toBe(input)
  })

  it('maps colorIntensity and glassEnabled without losing the seed', () => {
    const input = settings()
    const result = migrateUiSettings(input, 'dark')
    expect(result.migrated).toBe(true)
    expect(result.settings.uiSchema).toBe(4)
    expect(result.settings.appearance).toMatchObject({
      atmosphere: 55,
      colorIntensity: 55,
      surfaceSeparation: 65,
      materialMode: 'soft',
      glassEnabled: true,
      primary: '#A78BFA',
      presetId: 'sky',
      baseTone: 'dark',
      glassOpacity: 72,
      glassBlur: 30,
    })
    expect(result.settings.theme).toEqual(input.theme)
    expect(result.settings.fonts).toEqual(input.fonts)
  })

  it('treats a legacy boolean glass switch as soft glass', () => {
    const input = settings({
      appearance: {
        mode: 'preset',
        presetId: 'signal',
        baseTone: 'auto',
        primary: '#3ec4b4',
        secondary: '#c97a4a',
        uiText: '#eee',
        glassEnabled: true,
        glassOpacity: 90,
        glassBlur: 12,
      } as UIAppearance,
    })
    const result = migrateUiSettings(input, 'light')
    expect(result.settings.appearance.materialMode).toBe('soft')
    expect(result.settings.appearance.atmosphere).toBe(75)
  })

  it('keeps a disabled legacy glass switch solid', () => {
    const input = settings({
      appearance: {
        mode: 'preset',
        presetId: 'signal',
        baseTone: 'auto',
        primary: '#3ec4b4',
        secondary: '#c97a4a',
        uiText: '#eee',
        glassEnabled: false,
        glassOpacity: 0,
        glassBlur: 0,
      } as UIAppearance,
    })
    const result = migrateUiSettings(input, 'light')
    expect(result.settings.appearance.materialMode).toBe('solid')
    expect(result.settings.appearance.glassEnabled).toBe(false)
    expect(result.settings.appearance.glassOpacity).toBe(60)
  })

  it('preserves a user-selected material mode', () => {
    const input = settings({
      appearance: {
        ...settings().appearance,
        materialMode: 'clear',
        glassEnabled: false,
      },
    })
    const result = migrateUiSettings(input, 'dark')
    expect(result.settings.appearance.materialMode).toBe('clear')
    expect(result.settings.appearance.glassEnabled).toBe(true)
  })
})

describe('appearance normalization', () => {
  it('clamps ranges and mirrors the legacy intensity field', () => {
    const normalized = normalizeAppearance({
      mode: 'custom',
      presetId: 'sky',
      baseTone: 'dark',
      primary: '#fff',
      secondary: '#fff',
      uiText: '#fff',
      atmosphere: 140,
      surfaceSeparation: -20,
      glassOpacity: 10,
      glassBlur: 200,
    })
    expect(normalized.atmosphere).toBe(100)
    expect(normalized.colorIntensity).toBe(100)
    expect(normalized.surfaceSeparation).toBe(0)
    expect(normalized.glassOpacity).toBe(60)
    expect(normalized.glassBlur).toBe(60)
    expect(normalized.materialMode).toBe('solid')
  })

  it('is idempotent', () => {
    const once = normalizeAppearance(settings().appearance)
    expect(normalizeAppearance(once)).toEqual(once)
  })

  it('rejects unknown material modes', () => {
    const normalized = normalizeAppearance({
      ...settings().appearance,
      materialMode: 'acrylic' as never,
      glassEnabled: true,
    })
    expect(normalized.materialMode).toBe('soft')
  })
})
