/** Keyboard navigation for application controls outside the terminal/editor. */
export const FOCUSABLE_SELECTOR = 'a[href],button,input:not([type="hidden"]),select,textarea,summary,[tabindex]'

export function isAvailableControl(element: HTMLElement): boolean {
  if (!element.isConnected || element.matches(':disabled,[aria-disabled="true"]')) return false
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    if (node.hidden || node.hasAttribute('inert') || node.getAttribute('aria-hidden') === 'true') return false
    const style = getComputedStyle(node)
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
    if (node instanceof HTMLDetailsElement && !node.open && !node.querySelector('summary')?.contains(element)) return false
  }
  return true
}

export function focusableControls(root: ParentNode): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
    .filter(element => element.tabIndex >= 0 && isAvailableControl(element))
}

export function focusControl(element: HTMLElement | null | undefined): void {
  if (!element || !isAvailableControl(element)) return
  element.focus({preventScroll: true})
  element.scrollIntoView?.({block: 'nearest', inline: 'nearest'})
}

function ownsArrowKeys(element: HTMLElement): boolean {
  return !!element.closest('.xterm,[data-keyboard-native],textarea,select,[contenteditable]:not([contenteditable="false"]),[role="combobox"],[role="slider"],[role="spinbutton"],[role="separator"]')
    || (element instanceof HTMLInputElement && !['button', 'submit', 'reset', 'checkbox'].includes(element.type))
}

type Direction = 'ArrowLeft' | 'ArrowRight' | 'ArrowUp' | 'ArrowDown'
export type NavigationMode = 'horizontal' | 'vertical' | 'linear' | 'grid'
const directions = new Set(['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'])

function spatialTarget(items: HTMLElement[], active: HTMLElement, key: Direction): HTMLElement | undefined {
  const from = active.getBoundingClientRect()
  const horizontal = key === 'ArrowLeft' || key === 'ArrowRight'
  const forward = key === 'ArrowRight' || key === 'ArrowDown'
  const center = horizontal ? (from.left + from.right) / 2 : (from.top + from.bottom) / 2
  let best: {element: HTMLElement; score: number} | undefined
  for (const element of items) {
    if (element === active) continue
    const to = element.getBoundingClientRect()
    if (!to.width || !to.height) continue
    const along = horizontal ? (to.left + to.right) / 2 : (to.top + to.bottom) / 2
    const distance = (along - center) * (forward ? 1 : -1)
    if (distance <= 1) continue
    const gap = horizontal
      ? Math.max(0, from.top - to.bottom, to.top - from.bottom)
      : Math.max(0, from.left - to.right, to.left - from.right)
    // Prefer controls in the same row/column, then the closest visible neighbour.
    const score = distance + gap * 3
    if (!best || score < best.score) best = {element, score}
  }
  return best?.element
}

/** Called after a widget's own key handler so native editing always wins. */
export function navigateKeyboardFocus(event: KeyboardEvent, boundary?: HTMLElement): boolean {
  if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return false
  if (!(event.target instanceof HTMLElement) || ownsArrowKeys(event.target)) return false
  if (!directions.has(event.key) && event.key !== 'Home' && event.key !== 'End') return false
  const active = event.target.closest<HTMLElement>(FOCUSABLE_SELECTOR) ?? event.target
  let scope = active.closest<HTMLElement>('[data-keyboard-nav]')
  if (boundary && scope && !boundary.contains(scope)) scope = null
  const localMode = scope?.dataset.keyboardNav as NavigationMode | undefined
  const fallbackOrigin = scope?.parentElement ?? active
  let leavingAxis = false
  // An orthogonal arrow leaves a row and looks for a control in its panel.
  if ((localMode === 'horizontal' && ['ArrowUp', 'ArrowDown'].includes(event.key))
    || (localMode === 'vertical' && ['ArrowLeft', 'ArrowRight'].includes(event.key))) {
    leavingAxis = true
    scope = scope?.parentElement?.closest<HTMLElement>('[data-keyboard-nav]') ?? null
  }
  scope ??= boundary ?? (leavingAxis ? fallbackOrigin : active).closest<HTMLElement>('[role="dialog"],[role="menu"],.qconn,nav,main,.app-shell')
  if (!scope || !isAvailableControl(scope)) return false
  const mode = leavingAxis ? 'grid' : (scope.dataset.keyboardNav as NavigationMode | undefined ?? 'grid')
  const items = focusableControls(scope).filter(element => !element.closest('.xterm,[data-keyboard-native]')
    && (mode === 'grid' || element.closest('[data-keyboard-nav]') === scope))
  if (!items.length) return false
  const index = items.indexOf(active)
  const previous = event.key === 'ArrowLeft' || event.key === 'ArrowUp'
  let next: HTMLElement | undefined
  if (event.key === 'Home') next = items[0]
  else if (event.key === 'End') next = items.at(-1)
  else if (mode === 'grid' && index >= 0) {
    next = spatialTarget(items, active, event.key as Direction)
    // Layout-free environments and unlaid-out controls retain DOM-order navigation.
    if (!active.getBoundingClientRect().width) next = items[(index + (previous ? -1 : 1) + items.length) % items.length]
  } else next = items[index < 0 ? (previous ? items.length - 1 : 0) : (index + (previous ? -1 : 1) + items.length) % items.length]
  if (!next || next === active) return false
  event.preventDefault()
  event.stopPropagation()
  focusControl(next)
  return true
}

export function installKeyboardNavigation(root: Document = document): () => void {
  const onKey = (event: KeyboardEvent) => {
    if (navigateKeyboardFocus(event)) return
    if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.repeat || event.altKey || event.ctrlKey || event.metaKey) return
    if (!(event.target instanceof HTMLElement) || ownsArrowKeys(event.target)) return
    if (event.key !== 'Enter' && event.key !== ' ') return
    const target = event.target
    // Native controls already activate exactly once through the browser.
    if (target.matches('button,input,select,textarea,a[href],summary')) return
    if (!target.matches('[role="button"],[role="tab"],[role="menuitem"],[role="switch"],[role="checkbox"]') || !isAvailableControl(target)) return
    event.preventDefault()
    event.stopPropagation()
    target.click()
  }
  root.addEventListener('keydown', onKey)
  return () => root.removeEventListener('keydown', onKey)
}
