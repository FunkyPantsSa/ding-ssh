import type {Fonts, MaterialMode, Settings, UIAppearance} from '../types'
import {legacyDefaultPreset, paletteToTheme, presetById} from './presets'
import {clamp} from './material'

/**
 * Schema 4 introduces the dynamic color engine:
 *  - colorIntensity -> atmosphere (0–100)
 *  - surfaceSeparation (0–100)
 *  - materialMode (solid | soft | clear)
 *
 * Existing seed colors, tone, terminal palette and fonts are preserved.
 */
export const UI_SCHEMA = 4
export const UI2_MIGRATION_BACKUP_KEY = 'ding-ssh:ui2-migration-backup'
export const THEME_V4_MIGRATION_BACKUP_KEY = 'ding-ssh:theme-v4-migration-backup'

const LEGACY_APPEARANCE: UIAppearance = {
  mode: 'preset', presetId: 'signal', baseTone: 'auto', primary: '#3ec4b4', secondary: '#c97a4a', uiText: '#eef1f5', glassOpacity: 0, glassBlur: 0,
}
const LEGACY_FONTS: Fonts = {uiFont: 'Inter', terminalFont: 'IBM Plex Mono', terminalFontSize: 13}
const GEIST_APPEARANCE: UIAppearance = {
  mode: 'preset', presetId: 'geist', baseTone: 'auto', primary: '#3b82f6', secondary: '#a1a1aa', uiText: '#fafafa', glassOpacity: 85, glassBlur: 24,
}
const GEIST_FONTS: Fonts = {uiFont: 'Geist Sans', terminalFont: 'Geist Mono', terminalFontSize: 13}

function equal<T extends object>(a: T | undefined, b: T): boolean {
  return Object.keys(b).every((key) => a?.[key as keyof T] === b[key as keyof T])
}

function legacyMaterialMode(appearance: UIAppearance): MaterialMode {
  if (appearance.materialMode === 'soft' || appearance.materialMode === 'clear' || appearance.materialMode === 'solid') {
    return appearance.materialMode
  }
  return appearance.glassEnabled ? 'soft' : 'solid'
}

/**
 * Fills and clamps dynamic-theme fields without discarding user choices.
 * Safe to call on every load; idempotent.
 */
export function normalizeAppearance(appearance: UIAppearance): UIAppearance {
  const atmosphere = Math.round(clamp(appearance.atmosphere ?? appearance.colorIntensity ?? 75, 0, 100))
  const separation = Math.round(clamp(appearance.surfaceSeparation ?? 65, 0, 100))
  const materialMode = legacyMaterialMode(appearance)
  return {
    ...appearance,
    terminalBackgroundMode: appearance.terminalBackgroundMode === 'independent' ? 'independent' : 'follow',
    atmosphere,
    // Keep the old field as a mirror so older builds/scripts still read it.
    colorIntensity: atmosphere,
    surfaceSeparation: separation,
    materialMode,
    glassEnabled: materialMode !== 'solid',
    glassOpacity: Math.round(clamp(appearance.glassOpacity ?? 85, 60, 100)),
    glassBlur: Math.round(clamp(appearance.glassBlur ?? 24, 0, 60)),
  }
}

/**
 * Upgrade legacy presentation settings. Untouched old defaults are moved to
 * the Geist appearance (as before); user-made choices are preserved verbatim.
 */
export function migrateUiSettings(input: Settings, tone: 'dark' | 'light') {
  const schema = input.uiSchema ?? 0
  if (schema >= UI_SCHEMA) return {settings: input, migrated: false}

  const next: Settings = {...input, uiSchema: UI_SCHEMA}
  let appearance: UIAppearance = {...(input.appearance ?? GEIST_APPEARANCE)}
  const backup = JSON.stringify({
    appearance: input.appearance,
    theme: input.theme,
    fonts: input.fonts,
    terminalColorMode: input.terminalColorMode,
  })

  // Preserve the historical schema-2 -> 3 behavior only when it applies.
  if (schema < 3) {
    const untouched = equal(input.appearance, LEGACY_APPEARANCE) && equal(input.fonts, LEGACY_FONTS)
    if (untouched) {
      const palette = paletteToTheme(tone === 'light' ? legacyDefaultPreset().light : legacyDefaultPreset().dark)
      // The old palette must also be untouched. If it was customized, preserve it.
      if (Object.entries(palette).every(([key, value]) => input.theme?.[key as keyof typeof palette] === value)) {
        const geist = paletteToTheme(tone === 'light' ? presetById('geist')!.light : presetById('geist')!.dark)
        appearance = {...GEIST_APPEARANCE}
        next.fonts = {...GEIST_FONTS}
        next.theme = {...input.theme, ...geist}
      }
    }
  }

  next.appearance = normalizeAppearance(appearance)
  return {settings: next, migrated: true, backup}
}
