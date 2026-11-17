import {computed, onScopeDispose, ref, watch} from 'vue'
import {
  applyRootTheme,
  clearRootTheme,
  computeTheme,
  detectCapabilities,
  writeThemeCache,
  type Tone,
} from '@/theme/engine'
import {normalizeAppearance} from '@/theme/migrate'
import {refreshSystemTone, systemTone} from './useSystemTone'
import type {Fonts, Theme, UIAppearance} from '@/types'

/**
 * Single reactive bridge between Pinia settings and the CSS token layer.
 *
 * - The theme is not applied until settings are loaded, so the inline bootstrap
 *   cache in index.html keeps the last correct palette on screen (no flash).
 * - Terminal colors are never written here; the terminal composable owns them.
 */
export function useTheme(settings: {
  loaded: boolean
  appearance: UIAppearance
  fonts: Fonts
  theme: Theme
  setTheme(theme: Theme): Promise<void>
}) {
  const capabilities = ref(detectCapabilities())
  const tone = computed<Tone>(() =>
    settings.appearance.baseTone === 'auto' ? systemTone.value : settings.appearance.baseTone,
  )
  const computedTheme = computed(() =>
    computeTheme(normalizeAppearance(settings.appearance), settings.fonts, tone.value, capabilities.value),
  )
  const cssVars = computed(() => computedTheme.value.vars)

  function onSchemeChange() {
    refreshSystemTone()
  }

  function refreshCapabilities() {
    capabilities.value = detectCapabilities()
  }

  // CSS application is synchronous; persisting a ~5 KB cache on every slider
  // tick would be wasteful. Debounce only the storage write.
  let cacheTimer: ReturnType<typeof setTimeout> | undefined
  function scheduleCache(nextTone: Tone, theme: ReturnType<typeof computeTheme>) {
    if (cacheTimer) clearTimeout(cacheTimer)
    cacheTimer = setTimeout(() => {
      cacheTimer = undefined
      writeThemeCache(nextTone, theme)
    }, 150)
  }

  watch(
    [() => settings.loaded, tone, computedTheme],
    ([loaded, nextTone, theme]) => {
      if (!loaded) return
      applyRootTheme(nextTone, theme.vars, theme.meta)
      scheduleCache(nextTone, theme)
    },
    {immediate: true},
  )

  onScopeDispose(() => {
    if (cacheTimer) clearTimeout(cacheTimer)
    clearRootTheme()
  })

  return {tone, cssVars, capabilities, onSchemeChange, refreshCapabilities}
}
