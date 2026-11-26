import {flushPromises, mount} from '@vue/test-utils'
import {defineComponent, ref} from 'vue'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {useMenuKeyboard} from './useMenuKeyboard'

const mounted: ReturnType<typeof mount>[] = []
afterEach(() => { for (const wrapper of mounted.splice(0)) wrapper.unmount(); document.body.innerHTML = '' })

function setup() {
  const trigger = document.createElement('button')
  document.body.append(trigger)
  trigger.focus()
  const close = vi.fn()
  const action = vi.fn()
  const wrapper = mount(defineComponent({
    setup() { const menu = ref<HTMLElement>(); return {menu, action, ...useMenuKeyboard(menu, close)} },
    template: `<div ref="menu" role="menu" @keydown="onKeydown">
      <button role="menuitem" data-item="first" @click="action('first')">First</button>
      <button role="menuitem" disabled data-item="disabled">Disabled</button>
      <button role="menuitem" aria-disabled="true" data-item="aria-disabled">Disabled</button>
      <div hidden><button role="menuitem" data-item="hidden">Hidden</button></div>
      <div style="display:none"><button role="menuitem" data-item="display-none">Hidden</button></div>
      <button role="menuitem" data-item="last" @click="action('last')">Last</button>
      <input data-item="input" />
    </div>`,
  }), {attachTo: document.body})
  mounted.push(wrapper)
  return {wrapper, trigger, close, action}
}

function press(key: string, extras: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', {key, bubbles: true, cancelable: true, ...extras})
  document.activeElement!.dispatchEvent(event)
  return event
}

describe('scoped menu keyboard controls', () => {
  it('skips unavailable items, wraps, and activates once with Enter or Space', async () => {
    const {wrapper, action} = setup()
    await flushPromises()
    expect(document.activeElement?.getAttribute('data-item')).toBe('first')
    const outside = vi.fn()
    document.addEventListener('keydown', outside)
    try {
      expect(press('ArrowDown').defaultPrevented).toBe(true)
      expect(document.activeElement?.getAttribute('data-item')).toBe('last')
      press('ArrowDown')
      expect(document.activeElement?.getAttribute('data-item')).toBe('first')
      press('End')
      press('Enter')
      press(' ', {repeat: true})
      expect(action.mock.calls).toEqual([['last']])
      press('Home')
      press(' ')
      expect(action.mock.calls).toEqual([['last'], ['first']])
      press('ArrowUp')
      expect(document.activeElement?.getAttribute('data-item')).toBe('last')
      expect(outside).not.toHaveBeenCalled()
      wrapper.get('input').element.focus()
      expect(press('ArrowDown').defaultPrevented).toBe(false)
      expect(document.activeElement).toBe(wrapper.get('input').element)
      expect(outside).toHaveBeenCalledTimes(1)
    } finally { document.removeEventListener('keydown', outside) }
  })

  it('returns focus on Escape and unmount without stealing focus from a new dialog', async () => {
    const {wrapper, trigger, close} = setup()
    await flushPromises()
    press('Escape')
    expect(close).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(trigger)
    wrapper.get<HTMLButtonElement>('[data-item="first"]').element.focus()
    wrapper.unmount()
    expect(document.activeElement).toBe(trigger)

    const next = setup()
    await flushPromises()
    const dialogInput = document.createElement('input')
    document.body.append(dialogInput)
    dialogInput.focus()
    next.wrapper.unmount()
    expect(document.activeElement).toBe(dialogInput)
  })

  it('preserves IME input and lets Tab leave the closing menu', async () => {
    const {close} = setup()
    await flushPromises()
    expect(press('Enter', {isComposing: true}).defaultPrevented).toBe(false)
    expect(close).not.toHaveBeenCalled()
    expect(press('Tab').defaultPrevented).toBe(false)
    expect(close).toHaveBeenCalledTimes(1)
  })
})
