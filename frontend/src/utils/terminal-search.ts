/** Keep the terminal's helper textarea eligible, but leave unrelated form fields alone. */
export function isTerminalSearchShortcut(event: KeyboardEvent, active: boolean, searchRoot?: HTMLElement): boolean {
  if (!active || event.isComposing || !(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== 'f') return false
  if (hasOpenModal()) return false
  const target = event.target as HTMLElement | null
  const dialog = target?.closest<HTMLElement>('[role="dialog"]')
  if (dialog && isAvailableControl(dialog)) return false
  return !(target?.closest('input,textarea,select,[contenteditable="true"]') && !target.closest('.xterm') && !searchRoot?.contains(target))
}
import {hasOpenModal} from './terminal-focus'
import {isAvailableControl} from './keyboard-navigation'
