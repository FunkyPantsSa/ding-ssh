import {mount} from '@vue/test-utils'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {reactive} from 'vue'
import type {PaneDirection} from '../types'

const parent = reactive({children: [{size: 0.5}, {size: 0.5}]})
const sessions = {
  findPane: vi.fn(() => parent),
  resizePane: vi.fn((_parentId: string, _index: number, delta: number) => {
    const size = Math.max(0.1, Math.min(0.9, parent.children[0].size + delta))
    parent.children[0].size = size
    parent.children[1].size = 1 - size
  }),
}
vi.mock('../stores/sessions', () => ({useSessionsStore: () => sessions}))
import SplitDivider from './SplitDivider.vue'

beforeEach(() => { parent.children[0].size = 0.5; parent.children[1].size = 0.5; vi.clearAllMocks() })

describe('split pane keyboard resizing', () => {
  it.each([
    ['row', 'vertical', 'ArrowRight', 'ArrowLeft', 'ArrowDown'],
    ['column', 'horizontal', 'ArrowDown', 'ArrowUp', 'ArrowRight'],
  ])('adjusts a %s split on its axis and reports the live ratio', async (direction, orientation, forward, backward, other) => {
    const wrapper = mount(SplitDivider, {props: {direction: direction as PaneDirection, parentId: 'split', index: 1, axisTotal: 1000}})
    expect(wrapper.attributes('role')).toBe('separator')
    expect(wrapper.attributes('tabindex')).toBe('0')
    expect(wrapper.attributes('aria-orientation')).toBe(orientation)
    await wrapper.trigger('keydown', {key: forward})
    expect(sessions.resizePane).toHaveBeenLastCalledWith('split', 1, 0.02)
    expect(wrapper.attributes('aria-valuenow')).toBe('52')
    await wrapper.trigger('keydown', {key: backward, shiftKey: true})
    expect(sessions.resizePane).toHaveBeenLastCalledWith('split', 1, -0.1)
    expect(wrapper.attributes('aria-valuenow')).toBe('42')
    await wrapper.trigger('keydown', {key: other})
    expect(sessions.resizePane).toHaveBeenCalledTimes(2)
    await wrapper.trigger('keydown', {key: 'Enter'})
    expect(parent.children.map(child => child.size)).toEqual([0.5, 0.5])
    expect(wrapper.attributes('aria-valuenow')).toBe('50')
    wrapper.unmount()
  })

  it('supports endpoint keys without exceeding the existing pane limits', async () => {
    const wrapper = mount(SplitDivider, {props: {direction: 'row', parentId: 'split', index: 1, axisTotal: 1000}})
    await wrapper.trigger('keydown', {key: 'Home'})
    expect(wrapper.attributes('aria-valuenow')).toBe('10')
    await wrapper.trigger('keydown', {key: 'ArrowLeft'})
    expect(wrapper.attributes('aria-valuenow')).toBe('10')
    await wrapper.trigger('keydown', {key: 'End'})
    expect(wrapper.attributes('aria-valuenow')).toBe('90')
    await wrapper.trigger('keydown', {key: 'ArrowRight', shiftKey: true})
    expect(wrapper.attributes('aria-valuenow')).toBe('90')
    wrapper.unmount()
  })
})
