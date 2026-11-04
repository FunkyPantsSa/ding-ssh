import {flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import SftpPanel from './SftpPanel.vue'
import {useSettingsStore} from '../stores/settings'
import type {ServerNode, SessionTab} from '../types'

vi.mock('../services/ssh', () => ({
  onSftpTransfer: vi.fn(() => () => {}),
  onSftpDirUpdated: vi.fn(() => () => {}),
  sshService: {
    sftpList: vi.fn(),
    syncSftpToTerminal: vi.fn(),
  },
}))
vi.mock('../../wailsjs/runtime/runtime', () => ({
  OnFileDrop: vi.fn(),
  OnFileDropOff: vi.fn(),
  ClipboardSetText: vi.fn(),
}))

const {sshService} = await import('../services/ssh')

const node: ServerNode = {
  id: 's1', name: '测试服务器', group: '', host: '10.0.0.1', port: 22, user: 'root',
  authType: 'password', bgImage: '', blurAmount: 0, envVars: {},
}
const tab: SessionTab = {
  clientId: 'tab-1', sessionId: 'sess-1', serverName: '测试服务器', host: '10.0.0.1', user: 'root',
  node, status: 'connected', createdAt: 0, sftpPath: '/home', kind: 'ssh',
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.mocked(sshService.syncSftpToTerminal).mockResolvedValue(undefined)
  vi.mocked(sshService.sftpList).mockImplementation(async (_sid, dir) => {
    if (dir === '/home') return [{name: 'logs', path: '/home/logs', isDir: true, size: 0, modTime: 0}]
    return []
  })
})

describe('SFTP flow', () => {
  it('lists the remote directory and navigates into a folder with terminal sync', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const settings = useSettingsStore()
    settings.loaded = true
    settings.sftpToTerminalSync = true

    const wrapper = mount(SftpPanel, {attachTo: document.body, props: {tab}, global: {plugins: [pinia]}})
    await flushPromises()

    expect(sshService.sftpList).toHaveBeenCalledWith('sess-1', '/home')
    const folder = wrapper.get('[data-testid="sftp-entry"][data-path="/home/logs"]')
    expect(folder.attributes('data-dir')).toBe('true')

    await folder.trigger('dblclick')
    await flushPromises()

    expect(sshService.sftpList).toHaveBeenCalledWith('sess-1', '/home/logs')
    expect(sshService.syncSftpToTerminal).toHaveBeenCalledWith('sess-1', '/home/logs')
    expect(tab.sftpPath).toBe('/home/logs')
    wrapper.unmount()
  })
})
