import {ref} from 'vue'
import {resolveTone, type Tone} from '@/theme/engine'

/**
 * Shared OS light/dark preference. App.vue owns the matchMedia listener and
 * calls refreshSystemTone() on change; components that need a reactive tone
 * (CSS bridge, terminal surface, appearance preview) read this single ref
 * instead of calling matchMedia inside a computed, which would not invalidate.
 */
export const systemTone = ref<Tone>(resolveTone('auto'))

export function refreshSystemTone(): void {
  systemTone.value = resolveTone('auto')
}
