/** 补全导航热键：形如 Alt+ArrowDown / Ctrl+Shift+Space */

export const DEFAULT_COMPLETION_NAV_HOTKEY = 'Alt+ArrowDown'

const MOD_ORDER = ['Ctrl', 'Alt', 'Shift', 'Meta'] as const

export function normalizeHotkeyKey(key: string): string {
  if (key === ' ') return 'Space'
  return key
}

/** 从 KeyboardEvent 生成热键字符串；仅修饰键时返回空。 */
export function hotkeyFromEvent(e: KeyboardEvent): string {
  if (['Control', 'Alt', 'Shift', 'Meta'].includes(e.key)) return ''
  const parts: string[] = []
  if (e.ctrlKey) parts.push('Ctrl')
  if (e.altKey) parts.push('Alt')
  if (e.shiftKey) parts.push('Shift')
  if (e.metaKey) parts.push('Meta')
  parts.push(normalizeHotkeyKey(e.key))
  // 裸字母易与输入冲突：至少需要一个修饰键，或功能键/方向键/Space
  const main = parts[parts.length - 1]
  const hasMod = parts.length > 1
  const okBare =
    main.startsWith('F') ||
    main.startsWith('Arrow') ||
    main === 'Space' ||
    main === 'Tab' ||
    main === 'Escape' ||
    main === 'Enter'
  if (!hasMod && !okBare) return ''
  return parts.join('+')
}

export function matchHotkey(e: KeyboardEvent, hotkey: string): boolean {
  const expected = (hotkey || DEFAULT_COMPLETION_NAV_HOTKEY).split('+').filter(Boolean)
  if (!expected.length) return false
  const wantCtrl = expected.includes('Ctrl')
  const wantAlt = expected.includes('Alt')
  const wantShift = expected.includes('Shift')
  const wantMeta = expected.includes('Meta')
  const main = expected.find((p) => !MOD_ORDER.includes(p as (typeof MOD_ORDER)[number]))
  if (!main) return false
  if (!!e.ctrlKey !== wantCtrl) return false
  if (!!e.altKey !== wantAlt) return false
  if (!!e.shiftKey !== wantShift) return false
  if (!!e.metaKey !== wantMeta) return false
  return normalizeHotkeyKey(e.key) === main
}

/** 界面展示用（ArrowDown → ↓）。 */
export function formatHotkeyLabel(hotkey: string): string {
  return (hotkey || DEFAULT_COMPLETION_NAV_HOTKEY)
    .split('+')
    .map((p) => {
      if (p === 'ArrowDown') return '↓'
      if (p === 'ArrowUp') return '↑'
      if (p === 'ArrowLeft') return '←'
      if (p === 'ArrowRight') return '→'
      if (p === 'Meta') return '⌘'
      if (p === 'Control' || p === 'Ctrl') return 'Ctrl'
      return p
    })
    .join('+')
}
