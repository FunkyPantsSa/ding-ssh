import {nextTick, onBeforeUnmount, onMounted, type Ref} from 'vue'

/** Use DOM visibility rather than layout size so zero-size icons and WebView overlays remain navigable. */
export function isMenuItemAvailable(element: HTMLElement): boolean {
  if (element.matches(':disabled, [disabled], [aria-disabled="true"]')) return false
  for (let node: HTMLElement | null = element; node; node = node.parentElement) {
    if (node.hidden || node.hasAttribute('inert') || node.getAttribute('aria-hidden') === 'true') return false
    const style = getComputedStyle(node)
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
    if (node instanceof HTMLDetailsElement && !node.open) {
      const summary = node.querySelector('summary')
      if (!summary?.contains(element)) return false
    }
  }
  return true
}

function editableTarget(target: EventTarget | null): boolean {
  return target instanceof Element && !!target.closest('input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"]')
}

export function useMenuKeyboard(
  root: Readonly<Ref<HTMLElement | null | undefined>>,
  close: () => void,
  options: {initialFocus?: boolean; restoreOnUnmount?: boolean; trigger?: () => HTMLElement | null | undefined} = {},
) {
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const items = () => Array.from(root.value?.querySelectorAll<HTMLElement>('[role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"]') ?? [])
    .filter(isMenuItemAvailable)

  function focusAt(index: number) {
    const list = items()
    if (!list.length) return
    const element = list[(index + list.length) % list.length]
    element?.focus()
    element?.scrollIntoView?.({block: 'nearest'})
  }

  function restoreFocus() {
    const trigger = options.trigger?.() ?? previous
    if (trigger?.isConnected && isMenuItemAvailable(trigger)) trigger.focus()
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.altKey || event.ctrlKey || event.metaKey) return
    if (editableTarget(event.target)) return
    const list = items()
    const target = event.target instanceof Element ? event.target.closest<HTMLElement>('[role^="menuitem"]') : null
    const current = list.indexOf(target ?? document.activeElement as HTMLElement)
    if (event.key === 'Tab') {
      close()
      return
    }
    if (!['Escape', 'ArrowDown', 'ArrowUp', 'Home', 'End', 'Enter', ' '].includes(event.key)) return
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape') {
      restoreFocus()
      close()
    } else if (event.key === 'ArrowDown') {
      focusAt(current < 0 ? 0 : current + 1)
    } else if (event.key === 'ArrowUp') {
      focusAt(current < 0 ? list.length - 1 : current - 1)
    } else if (event.key === 'Home') {
      focusAt(0)
    } else if (event.key === 'End') {
      focusAt(list.length - 1)
    } else if (!event.repeat) {
      list[current]?.click()
    }
  }

  onMounted(async () => {
    if (options.initialFocus === false) return
    await nextTick()
    focusAt(0)
  })
  onBeforeUnmount(() => {
    if (options.restoreOnUnmount !== false && (root.value?.contains(document.activeElement) || document.activeElement === document.body)) restoreFocus()
  })
  return {items, focusAt, onKeydown, restoreFocus}
}
