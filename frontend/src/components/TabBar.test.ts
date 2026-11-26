import {DOMWrapper, mount} from '@vue/test-utils'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {nextTick, reactive} from 'vue'
import type {SessionTab} from '../types'

const sessions = reactive({
  tabs: [] as SessionTab[], activeId: 'one', panes: null as unknown, splitShown: false,
  collectLeaves: vi.fn(() => [{paneActiveId: 'one'}, {paneActiveId: 'two'}]),
  activateTab: vi.fn(), closeTab: vi.fn(), showSplit: vi.fn(), closeSplitGroup: vi.fn(),
})
const ui = {view: 'workspace', showWorkspace: vi.fn(), openTerminalSidebar: vi.fn()}
vi.mock('../stores/sessions', () => ({useSessionsStore: () => sessions}))
vi.mock('../stores/ui', () => ({useUIStore: () => ui}))
vi.mock('../../bindings/ding-ssh/internal/apphost/app', () => ({OpenTerminalWindow: vi.fn(), OpenSFTPWindow: vi.fn()}))
import TabBar from './TabBar.vue'

beforeEach(() => {
  sessions.tabs = [
    {clientId: 'one', serverName: '第一台', status: 'connected', node: {host: 'one'}},
    {clientId: 'two', serverName: '第二台', status: 'connected', node: {host: 'two'}},
  ] as SessionTab[]
  sessions.panes = null
  sessions.splitShown = false
  vi.clearAllMocks()
})
afterEach(() => { document.body.innerHTML = '' })

describe('session tab keyboard controls', () => {
  it.each([false, true])('uses separate native activate and close buttons, side=%s', async side => {
    const wrapper = mount(TabBar, {props: {side}, global: {stubs: {Icon: true}}})
    expect(wrapper.get('[data-ui="tabbar"]').attributes('data-keyboard-nav')).toBe(side ? 'vertical' : 'horizontal')
    const row = wrapper.get('[data-tab-id="one"]')
    expect(row.element.tagName).toBe('DIV')
    expect(row.get('.tab-main').element.tagName).toBe('BUTTON')
    expect(row.get('.tab-main').attributes('aria-pressed')).toBe('true')
    expect(row.get('.tab-close').element.tagName).toBe('BUTTON')
    expect(row.get('.tab-close').attributes('aria-label')).toContain('第一台')
    expect(wrapper.find('button button').exists()).toBe(false)
    await row.get('.tab-main').trigger('click')
    expect(sessions.activateTab).toHaveBeenCalledWith('one')
    sessions.activateTab.mockClear()
    await row.get('.tab-close').trigger('click')
    expect(sessions.closeTab).toHaveBeenCalledWith('one')
    expect(sessions.activateTab).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('opens the tab menu with Shift+F10, focuses its actions and restores focus on Escape', async () => {
    const wrapper = mount(TabBar, {attachTo: document.body, global: {stubs: {Icon: true}}})
    const trigger = wrapper.get<HTMLButtonElement>('[data-tab-id="one"] .tab-main')
    trigger.element.focus()
    expect(document.activeElement).toBe(trigger.element)
    await trigger.trigger('keydown', {key: 'F10', shiftKey: true})
    await nextTick()
    const menu = new DOMWrapper(document.querySelector<HTMLElement>('[data-ui="context-menu"]')!)
    expect(menu.attributes('data-keyboard-nav')).toBe('vertical')
    expect(document.activeElement).toBe(menu.get('button').element)
    const split = menu.findAll('button').find(button => button.attributes('aria-haspopup') === 'true')!
    await split.trigger('keydown', {key: 'ArrowRight'})
    await nextTick()
    const submenu = new DOMWrapper(document.querySelector<HTMLElement>('.split-submenu')!)
    expect(document.activeElement).toBe(submenu.get('button').element)
    await submenu.trigger('keydown', {key: 'ArrowLeft'})
    expect(document.activeElement).toBe(split.element)
    await menu.trigger('keydown', {key: 'Escape'})
    expect(document.querySelector('[data-ui="context-menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it.each([false, true])('keeps split-group activate and close keyboard controls separate, side=%s', async side => {
    sessions.panes = {}
    sessions.splitShown = true
    const wrapper = mount(TabBar, {props: {side}, global: {stubs: {Icon: true}}})
    expect(wrapper.get('.tab-main').attributes('aria-pressed')).toBe('true')
    await wrapper.get('.tab-main').trigger('click')
    expect(sessions.showSplit).toHaveBeenCalledOnce()
    await wrapper.get('.tab-close').trigger('click')
    expect(sessions.closeSplitGroup).toHaveBeenCalledOnce()
    expect(sessions.showSplit).toHaveBeenCalledOnce()
    wrapper.unmount()
  })
})
