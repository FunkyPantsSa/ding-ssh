/** Visibility checks intentionally avoid dimensions: hidden ancestors matter,
 * while a surface can legitimately be waiting for its first layout. */
function visible(element: HTMLElement): boolean {
  if (!element.isConnected) return false
  for (let current: HTMLElement | null = element; current; current = current.parentElement) {
    if (current.hidden || current.hasAttribute('inert') || current.getAttribute('aria-hidden') === 'true') return false
    const style = getComputedStyle(current)
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
  }
  return true
}

export function hasOpenModal(): boolean {
  return Array.from(document.querySelectorAll<HTMLElement>('[aria-modal="true"], dialog[open]')).some(visible)
}

export interface TerminalFocusContext {
  root: HTMLElement | null | undefined
  terminal?: HTMLElement | null
  active: boolean
  workspace: boolean
  overlayOpen?: boolean
  intent?: 'background' | 'activate' | 'restore'
  restoreFrom?: readonly (HTMLElement | null | undefined)[]
}

/** Async connection/transfer updates may reclaim a passive terminal surface,
 * but must preserve a user's selection in any other control. */
export function shouldFocusTerminal(context: TerminalFocusContext): boolean {
  const {root, terminal, active, workspace, overlayOpen, intent = 'background'} = context
  if (!root || !active || !workspace || overlayOpen || !visible(root) || hasOpenModal()) return false
  const focused = document.activeElement
  if (!focused || focused === document.body || focused === document.documentElement) return true
  if (intent === 'activate') return true
  if (focused === terminal || (root.contains(focused) && focused.closest('.xterm'))) return true
  if (root.contains(focused) && focused.hasAttribute('data-terminal-focus-surface')) return true
  if (intent === 'restore' && context.restoreFrom?.some(surface => surface?.contains(focused))) return true
  return false
}
