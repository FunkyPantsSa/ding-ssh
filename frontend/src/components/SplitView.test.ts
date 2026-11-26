import {mount} from '@vue/test-utils'
import {describe, expect, it, vi} from 'vitest'
import {reactive} from 'vue'
import type {PaneLayout} from '../panes/layout'

const sessions = reactive({
  focusedPaneId: 'left',
  tabs: [{clientId: 'one', serverName: '生产', status: 'connected'}, {clientId: 'two', serverName: '测试', status: 'connected'}],
  findPane: (id: string) => ({paneActiveId: id === 'left' ? 'one' : 'two'}),
  setFocusedPane: vi.fn((id: string) => { sessions.focusedPaneId = id }),
})
vi.mock('../stores/sessions', () => ({useSessionsStore: () => sessions}))
import SplitView from './SplitView.vue'

describe('split pane selection', () => {
  it('exposes each pane header as a native keyboard button with current selection', async () => {
    const layout: PaneLayout = {
      leafRects: new Map([
        ['left', {left: 0, top: 0, width: 400, height: 600, paneId: 'left'}],
        ['right', {left: 405, top: 0, width: 400, height: 600, paneId: 'right'}],
      ]),
      dividers: [],
    }
    const wrapper = mount(SplitView, {props: {layout}})
    expect(wrapper.attributes('data-keyboard-nav')).toBe('grid')
    const headers = wrapper.findAll('.split-pane-header')
    expect(headers.every(header => header.element.tagName === 'BUTTON' && header.attributes('type') === 'button')).toBe(true)
    expect(headers.map(header => header.attributes('aria-pressed'))).toEqual(['true', 'false'])
    await headers[1].trigger('click')
    expect(sessions.setFocusedPane).toHaveBeenCalledWith('right')
    expect(headers.map(header => header.attributes('aria-pressed'))).toEqual(['false', 'true'])
    wrapper.unmount()
  })
})
