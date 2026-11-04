import type {Fonts, Settings, UIAppearance} from '../types'
import {legacyDefaultPreset, paletteToTheme, presetById} from './presets'

export const UI_SCHEMA = 2
export const UI2_MIGRATION_BACKUP_KEY = 'ding-ssh:ui2-migration-backup'

const LEGACY_APPEARANCE: UIAppearance = {
  mode: 'preset', presetId: 'signal', baseTone: 'auto', primary: '#3ec4b4', secondary: '#c97a4a', uiText: '#eef1f5',
}
const LEGACY_FONTS: Fonts = {uiFont: 'Inter', terminalFont: 'IBM Plex Mono', terminalFontSize: 13}
const GEIST_APPEARANCE: UIAppearance = {
  mode: 'preset', presetId: 'geist', baseTone: 'auto', primary: '#3b82f6', secondary: '#a1a1aa', uiText: '#fafafa',
}
const GEIST_FONTS: Fonts = {uiFont: 'Geist Sans', terminalFont: 'Geist Mono', terminalFontSize: 13}

function equal<T extends object>(a: T | undefined, b: T): boolean {
  return Object.keys(b).every((key) => a?.[key as keyof T] === b[key as keyof T])
}

/**
 * Upgrade only untouched legacy presentation settings. User-made appearance or
 * font choices are deliberately preserved; uiSchema still marks the migration.
 */
export function migrateUiSettings(input: Settings, tone: 'dark' | 'light') {
  if ((input.uiSchema ?? 0) >= UI_SCHEMA) return {settings: input, migrated: false}
  const next: Settings = {...input, uiSchema: UI_SCHEMA}
  const untouched = equal(input.appearance, LEGACY_APPEARANCE) && equal(input.fonts, LEGACY_FONTS)
  const backup = JSON.stringify({
    appearance: input.appearance, theme: input.theme, fonts: input.fonts, terminalColorMode: input.terminalColorMode,
  })
  if (untouched) {
    const palette = paletteToTheme(tone === 'light' ? legacyDefaultPreset().light : legacyDefaultPreset().dark)
    // The old palette must also be untouched. If it was customized, preserve it.
    if (Object.entries(palette).every(([key, value]) => input.theme?.[key as keyof typeof palette] === value)) {
      const geist = paletteToTheme(tone === 'light' ? presetById('geist')!.light : presetById('geist')!.dark)
      next.appearance = {...GEIST_APPEARANCE}
      next.fonts = {...GEIST_FONTS}
      next.theme = {...input.theme, ...geist}
    }
  }
  return {settings: next, migrated: true, backup}
}
