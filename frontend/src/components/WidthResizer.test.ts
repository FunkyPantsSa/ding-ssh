import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import WidthResizer from './WidthResizer.vue'

function resizer(side: 'left' | 'right' = 'left', width = 200) {
  return mount(WidthResizer, {props: {side, width, min: 150, max: 300, inset: '196px', label: '导航'}})
}

describe('panel width keyboard resizing', () => {
  it.each(['left', 'right'] as const)('moves the %s panel edge in the pressed arrow direction', async side => {
    const wrapper = resizer(side)
    expect(wrapper.attributes('role')).toBe('separator')
    expect(wrapper.attributes('tabindex')).toBe('0')
    expect(wrapper.attributes('aria-valuenow')).toBe('200')
    await wrapper.trigger('keydown', {key: 'ArrowRight'})
    const right = side === 'left' ? 210 : 190
    expect(wrapper.emitted('resize')?.at(-1)).toEqual([right])
    await wrapper.setProps({width: right})
    await wrapper.trigger('keydown', {key: 'ArrowLeft', shiftKey: true})
    expect(wrapper.emitted('resize')?.at(-1)).toEqual([side === 'left' ? 170 : 230])
    wrapper.unmount()
  })

  it('clamps changes, reports limits and lets Enter reset without changing width', async () => {
    const wrapper = resizer('left', 295)
    expect(wrapper.attributes('aria-valuemin')).toBe('150')
    expect(wrapper.attributes('aria-valuemax')).toBe('300')
    await wrapper.trigger('keydown', {key: 'ArrowRight', shiftKey: true})
    expect(wrapper.emitted('resize')?.at(-1)).toEqual([300])
    await wrapper.setProps({width: 300})
    await wrapper.trigger('keydown', {key: 'ArrowRight'})
    expect(wrapper.emitted('resize')).toHaveLength(1)
    await wrapper.trigger('keydown', {key: 'Home'})
    expect(wrapper.emitted('resize')?.at(-1)).toEqual([150])
    await wrapper.trigger('keydown', {key: 'Enter'})
    expect(wrapper.emitted('reset')).toEqual([[]])
    await wrapper.trigger('keydown', {key: 'ArrowLeft', metaKey: true})
    expect(wrapper.emitted('resize')).toHaveLength(2)
    wrapper.unmount()
  })
})
