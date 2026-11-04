import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import TerminalContextMenu from './TerminalContextMenu.vue'

describe('terminal context menu contract', () => {
  it('exposes copy, paste and visual controls through stable test IDs', async () => {
    const wrapper = mount(TerminalContextMenu, {attachTo: document.body, props: {position: {x: 10, y: 10}, logTracking: false}})
    await document.querySelector<HTMLElement>('[data-testid="terminal-copy"]')!.click()
    await document.querySelector<HTMLElement>('[data-testid="terminal-paste"]')!.click()
    expect(wrapper.emitted('copy')).toHaveLength(1)
    expect(wrapper.emitted('paste')).toHaveLength(1)
    wrapper.unmount()
  })

  it('uses menu semantics, focuses the first item and closes on Escape', async () => {
    const wrapper = mount(TerminalContextMenu, {attachTo: document.body, props: {position: {x: 10, y: 10}, logTracking: false}})
    await new Promise((resolve) => setTimeout(resolve, 0))

    const menu = document.querySelector<HTMLElement>('[role="menu"]')!
    expect(menu.getAttribute('aria-label')).toBe('终端操作')
    expect(document.querySelectorAll('[role="menuitem"]').length).toBeGreaterThan(4)
    expect(document.activeElement?.getAttribute('data-testid')).toBe('terminal-copy')

    menu.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
