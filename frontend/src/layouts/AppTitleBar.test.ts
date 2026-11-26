import {mount} from '@vue/test-utils'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {WindowClose, WindowMinimise, WindowToggleMaximise} from '@/services/desktop'
import AppTitleBar from './AppTitleBar.vue'

vi.mock('@/services/desktop', () => ({
  WindowClose: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
}))

beforeEach(() => vi.clearAllMocks())

describe('window titlebar', () => {
  it('keeps the controls in the Windows titlebar with and without active sessions', async () => {
    const view = mount(AppTitleBar, {props: {windows: true, onlineCount: 0}})
    expect(view.get('header > .window-controls').findAll('button')).toHaveLength(3)
    await view.get('[aria-label="最小化"]').trigger('click')
    await view.get('[aria-label="最大化或还原"]').trigger('click')
    await view.get('[aria-label="关闭窗口"]').trigger('click')
    expect(WindowMinimise).toHaveBeenCalledTimes(1)
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1)
    expect(WindowClose).toHaveBeenCalledTimes(1)
    await view.get('[aria-label="最小化"]').trigger('dblclick')
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1)
    await view.get('header').trigger('dblclick')
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(2)
    await view.setProps({onlineCount: 3})
    expect(view.get('.window-drag-status').text()).toContain('3')
    expect(view.get('header > .window-controls').findAll('button')).toHaveLength(3)
    view.unmount()
  })

  it('leaves macOS window controls to the native titlebar', () => {
    const view = mount(AppTitleBar, {props: {windows: false, onlineCount: 1}})
    expect(view.find('.window-controls').exists()).toBe(false)
    view.unmount()
  })
})
