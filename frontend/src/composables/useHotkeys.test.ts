import {enableAutoUnmount, mount} from '@vue/test-utils'
import {defineComponent, h} from 'vue'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {useHotkeys} from './useHotkeys'

enableAutoUnmount(afterEach)
afterEach(() => document.body.replaceChildren())

function fixture() {
  const close = vi.fn(), palette = vi.fn()
  mount(defineComponent({setup() {
    useHotkeys([
      {id: 'close-tab', keys: 'Mod+W', label: 'Close tab', scope: 'workspace', run: close},
      {id: 'command-palette', keys: 'Mod+K', label: 'Command palette', scope: 'global', run: palette},
    ])
    return () => h('button', {id: 'outside'}, 'Workspace')
  }}), {attachTo: document.body})
  return {close, palette, outside: document.getElementById('outside')!}
}

function press(target: HTMLElement, key: string) {
  target.focus()
  const event = new KeyboardEvent('keydown', {key, ctrlKey: true, bubbles: true, cancelable: true})
  target.dispatchEvent(event)
}

describe('application hotkey boundaries', () => {
  it('leaves already handled keys with the focused control', () => {
    const {close, outside} = fixture()
    outside.addEventListener('keydown', event => event.preventDefault())
    press(outside, 'w')
    expect(close).not.toHaveBeenCalled()
  })

  it('does not close tabs or open a palette through a security modal button', () => {
    const {close, palette, outside} = fixture()
    const modal = document.createElement('section')
    modal.setAttribute('aria-modal', 'true')
    modal.innerHTML = '<button>Trust and connect</button><input type="checkbox">'
    document.body.append(modal)
    for (const target of [modal.querySelector('button')!, modal.querySelector('input')!, outside]) {
      press(target, 'w')
      press(target, 'k')
    }
    expect(close).not.toHaveBeenCalled()
    expect(palette).not.toHaveBeenCalled()
  })

  it('allows closing the active palette with its toggle while isolating other workspace hotkeys', () => {
    const {close, palette, outside} = fixture()
    const modal = document.createElement('section')
    modal.setAttribute('aria-modal', 'true')
    modal.setAttribute('data-ui', 'command-palette')
    modal.innerHTML = '<input>'
    document.body.append(modal)
    const input = modal.querySelector('input')!
    press(input, 'w')
    press(input, 'k')
    expect(close).not.toHaveBeenCalled()
    expect(palette).toHaveBeenCalledOnce()
    modal.remove()
    press(outside, 'w')
    expect(close).toHaveBeenCalledOnce()
  })
})
