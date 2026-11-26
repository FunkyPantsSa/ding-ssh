import {flushPromises, mount} from '@vue/test-utils'
import {afterEach, expect, it, vi} from 'vitest'
import CommandPalette from './CommandPalette.vue'
import type {AppCommand} from '../../commands'

const commands: AppCommand[] = [
  {id: 'alpha', title: 'Alpha', section: '操作', cap: '', confirm: false, note: '', run: () => {}},
  {id: 'beta', title: 'Beta', section: '操作', cap: '', confirm: false, note: '', run: () => {}},
]
afterEach(() => { document.body.innerHTML = '' })

async function press(element: HTMLElement, key: string, extras: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', {key, bubbles: true, cancelable: true, ...extras})
  element.dispatchEvent(event)
  await flushPromises()
  return event
}

it('focuses an initially open palette and navigates commands without trapping Tab on its search input', async () => {
  const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus()
  const wrapper = mount(CommandPalette, {attachTo: document.body, props: {modelValue: true, commands, recentIds: ['alpha']}})
  try {
    await flushPromises()
    const input = wrapper.get<HTMLInputElement>('input')
    const clear = wrapper.get<HTMLButtonElement>('header button')
    expect(document.activeElement).toBe(input.element)
    expect((await press(input.element, 'Tab')).defaultPrevented).toBe(false)
    clear.element.focus()
    expect((await press(clear.element, 'Tab')).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(input.element)
    await press(input.element, 'Tab', {shiftKey: true})
    expect(document.activeElement).toBe(clear.element)
    await press(clear.element, 'Enter')
    expect(wrapper.emitted('run')).toBeUndefined()
    input.element.focus()
    await press(input.element, 'End')
    expect(input.attributes('aria-activedescendant')).toBe('command-beta')
    expect((await press(input.element, 'Enter', {isComposing: true})).defaultPrevented).toBe(false)
    expect(wrapper.emitted('run')).toBeUndefined()
    await press(input.element, 'Enter')
    expect(wrapper.emitted('run')).toEqual([[commands[1]]])
    await wrapper.setProps({modelValue: false})
    expect(document.activeElement).toBe(trigger)
  } finally { wrapper.unmount() }
})

it('commits the latest search immediately when Enter precedes the search debounce', async () => {
  const many = [...commands, ...Array.from({length: 205}, (_, i) => ({...commands[0]!, id: `filler-${i}`, title: `Filler ${i}`}))]
  const wrapper = mount(CommandPalette, {attachTo: document.body, props: {modelValue: true, commands: many, recentIds: []}})
  try {
    await flushPromises()
    const input = wrapper.get<HTMLInputElement>('input')
    await input.setValue('Beta')
    await press(input.element, 'Enter')
    expect(wrapper.emitted('run')).toEqual([[commands[1]]])
  } finally { wrapper.unmount() }
})
