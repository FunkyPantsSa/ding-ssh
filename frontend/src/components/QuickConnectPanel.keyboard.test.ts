import {flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {afterEach, expect, it, vi} from 'vitest'
import QuickConnectPanel from './QuickConnectPanel.vue'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import {useGroupsStore} from '../stores/groups'
import type {ServerNode} from '../types'

vi.mock('../services/ssh', () => ({sshService: {getServers: vi.fn(async () => [])}}))
vi.mock('../services/groups', () => ({groupService: {list: vi.fn(async () => [])}}))
vi.mock('../services/desktop', () => ({EventsOn: vi.fn(() => () => {}), EventsOff: vi.fn()}))

afterEach(() => { document.body.innerHTML = ''; localStorage.clear(); vi.unstubAllGlobals() })

it('focuses search, moves to results, protects IME Enter, and restores focus after Escape', async () => {
  const pinia = createPinia(); setActivePinia(pinia)
  const servers = useServersStore(); const ui = useUIStore(); const sessions = useSessionsStore(); const groups = useGroupsStore()
  servers.loadedOnce = true
  servers.servers = ['Alpha', 'Beta'].map((name, i) => ({id: String(i), name, group: 'Prod', host: `10.0.0.${i}`, user: 'root', port: 22, authType: 'password', envVars: {}} as ServerNode))
  groups.list = ['Prod']
  const openTab = vi.spyOn(sessions, 'openTab').mockImplementation(() => ({clientId: 'opened'} as any))
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { callback(0); return 1 })
  const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus()
  const wrapper = mount(QuickConnectPanel, {attachTo: document.body, global: {plugins: [pinia], stubs: {Transition: false}}})
  try {
    ui.openTerminalSidebar()
    await flushPromises()
    const panel = document.querySelector<HTMLElement>('[data-ui="quick-connect"]')!
    const input = panel.querySelector<HTMLInputElement>('input')!
    expect(document.activeElement).toBe(input)
    const press = async (key: string, extras: KeyboardEventInit = {}) => {
      const event = new KeyboardEvent('keydown', {key, bubbles: true, cancelable: true, ...extras})
      document.activeElement!.dispatchEvent(event); await flushPromises(); return event
    }
    await press('ArrowDown')
    expect(document.activeElement?.getAttribute('data-quick-row')).toBe('server-0')
    await press('End')
    expect(document.activeElement?.getAttribute('data-quick-row')).toBe('server-1')
    expect((await press('Enter', {isComposing: true})).defaultPrevented).toBe(false)
    expect(openTab).not.toHaveBeenCalled()
    await press('Escape')
    expect(ui.terminalSidebarOpen).toBe(false)
    expect(document.activeElement).toBe(trigger)
    ui.openTerminalSidebar(); await flushPromises()
    input.value = 'Beta'; input.dispatchEvent(new Event('input', {bubbles: true})); await flushPromises()
    await press('Enter')
    expect(openTab).toHaveBeenCalledTimes(1)
    expect(openTab).toHaveBeenCalledWith(expect.objectContaining({name: 'Beta'}))
    expect(ui.terminalSidebarOpen).toBe(false)
  } finally { wrapper.unmount() }
})
