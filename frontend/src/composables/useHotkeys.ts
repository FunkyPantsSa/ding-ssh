import {onBeforeUnmount, onMounted} from 'vue'
import type {HotkeyDefinition, HotkeyScope} from '@/hotkeys'

type HotkeyHandler = (event: KeyboardEvent) => void
export type HotkeyBinding = HotkeyDefinition & {run: HotkeyHandler; enabled?: () => boolean}

function editableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return !!target.closest('input, textarea, select, [contenteditable="true"]')
}

function normalizeEvent(event: KeyboardEvent): string {
  const parts: string[] = []
  if (event.metaKey || event.ctrlKey) parts.push('Mod')
  if (event.altKey) parts.push('Alt')
  if (event.shiftKey) parts.push('Shift')
  let key = event.key
  if (key === '+') key = '='
  if (key.length === 1) key = key.toUpperCase()
  parts.push(key)
  return parts.join('+')
}

function activeScope(): HotkeyScope {
  return document.activeElement?.closest?.('.xterm') ? 'terminal' : 'workspace'
}

export function useHotkeys(bindings: HotkeyBinding[]) {
  const unique: HotkeyBinding[] = []
  const seen = new Set<string>()
  for (const binding of bindings) {
    const key = `${binding.scope}:${binding.keys}`
    if (seen.has(key)) {
      if (import.meta.env.DEV) console.warn(`[hotkeys] duplicate ${key}; keeping the first binding`)
      continue
    }
    seen.add(key)
    unique.push(binding)
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.isComposing || event.key === 'Process') return
    const keys = normalizeEvent(event)
    const scope = activeScope()
    const binding = unique.find((item) => item.keys === keys && (
      item.scope === 'global' || item.scope === scope || (scope === 'terminal' && item.scope === 'workspace')
    ))
    if (!binding || binding.enabled?.() === false) return
    if (editableTarget(event.target) && binding.id !== 'command-palette' && binding.id !== 'close-overlay') return
    binding.run(event)
  }

  onMounted(() => window.addEventListener('keydown', onKeydown))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
  return {onKeydown}
}
