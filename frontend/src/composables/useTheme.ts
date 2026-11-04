import {computed, onBeforeUnmount, ref, watch} from 'vue'
import {applyRootTheme, clearRootTheme, computeUiCssVars, deriveTerminalTheme, deriveSurfaceVars, resolveTone, type Tone} from '@/theme/engine'
import {defaultPreset, paletteToTheme, presetById} from '@/theme/presets'
import type {Fonts, Theme, UIAppearance} from '@/types'

const PALETTE_KEYS = [
  'background', 'foreground', 'cursor', 'selection',
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow',
  'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
] as const

/** Keeps document-level theme variables and terminal palette in sync with settings. */
export function useTheme(settings: {
  loaded: boolean
  appearance: UIAppearance
  fonts: Fonts
  theme: Theme
  setTheme(theme: Theme): Promise<void>
}) {
  const systemTone = ref<Tone>(resolveTone('auto'))
  const tone = computed<Tone>(() => settings.appearance.baseTone === 'auto' ? systemTone.value : settings.appearance.baseTone)
  const cssVars = computed(() => computeUiCssVars(settings.appearance, settings.fonts, tone.value))

  function paletteSignature(t: Theme): string {
    return PALETTE_KEYS.map((key) => t[key]).join('|')
  }
  function presetPaletteTheme(base: Theme, nextTone: Tone): Theme {
    const preset = presetById(settings.appearance.presetId) ?? defaultPreset()
    const palette = paletteToTheme(nextTone === 'dark' ? preset.dark : preset.light)
    const next = {...base}
    for (const key of PALETTE_KEYS) next[key] = palette[key]
    next.background = nextTone === 'light' && preset.id === 'geist' ? '#f5f6f7' : palette.background
    return next
  }
  function syncTerminalPalette() {
    if (!settings.loaded || settings.appearance.mode === 'custom') return
    const next = settings.appearance.mode === 'preset'
      ? presetPaletteTheme(settings.theme, tone.value)
      : deriveTerminalTheme(settings.theme, settings.appearance.primary, tone.value)
    if (paletteSignature(next) !== paletteSignature(settings.theme)) void settings.setTheme(next)
  }
  function onSchemeChange() { systemTone.value = resolveTone('auto') }

  watch([tone, () => settings.loaded, () => settings.appearance.mode, () => settings.appearance.presetId, () => settings.appearance.primary], syncTerminalPalette, {immediate: true})
  watch([tone, cssVars], ([nextTone, vars]) => applyRootTheme(nextTone, vars), {immediate: true})
  onBeforeUnmount(clearRootTheme)

  return {tone, onSchemeChange}
}
