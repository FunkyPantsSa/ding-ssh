import {flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import TunnelPage from './TunnelPage.vue'
import type {ServerNode} from '../types'

vi.mock('../services/ssh', () => ({
  onTunnelStatus: vi.fn(() => () => {}),
  sshService: {
    getServers: vi.fn(),
    listTunnels: vi.fn(),
    startTunnel: vi.fn(),
    updateTunnel: vi.fn(),
    stopTunnel: vi.fn(),
    restartTunnel: vi.fn(),
    removeTunnel: vi.fn(),
  },
}))
vi.mock('../../wailsjs/runtime/runtime', () => ({
  ClipboardSetText: vi.fn(),
  EventsOn: vi.fn(() => () => {}),
  EventsOff: vi.fn(),
}))

const {sshService, onTunnelStatus} = await import('../services/ssh')

const node: ServerNode = {
  id: 's1',
  name: '测试服务器',
  group: '',
  host: '10.0.0.1',
  port: 22,
  user: 'root',
  authType: 'password',
  bgImage: '',
  blurAmount: 0,
  envVars: {},
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.mocked(sshService.getServers).mockResolvedValue([node])
  vi.mocked(sshService.listTunnels).mockResolvedValue([])
  vi.mocked(sshService.startTunnel).mockResolvedValue(undefined as never)
})

describe('tunnel business flow', () => {
  it('creates a local tunnel with the form values', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(TunnelPage, {global: {plugins: [pinia]}, attachTo: document.body})
    await flushPromises()

    await wrapper.get('[data-testid="tunnel-create"]').trigger('click')
    await flushPromises()

    const name = document.querySelector<HTMLInputElement>('#tunnel-name')!
    name.value = '测试隧道'
    name.dispatchEvent(new Event('input', {bubbles: true}))
    const port = document.querySelector<HTMLInputElement>('#tunnel-local-port')!
    port.value = '15432'
    port.dispatchEvent(new Event('input', {bubbles: true}))

    document.querySelector<HTMLElement>('[data-testid="tunnel-save"]')!.click()
    await flushPromises()

    expect(sshService.startTunnel).toHaveBeenCalledWith(node, '测试隧道', 'local', 15432, '127.0.0.1', 3306)
    expect(onTunnelStatus).toHaveBeenCalled()
    expect(sshService.listTunnels).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
