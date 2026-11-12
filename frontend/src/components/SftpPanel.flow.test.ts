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

it('offers directory actions on blank space and refresh on entries', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  const wrapper = mount(SftpPanel, {attachTo: document.body, props: {tab: {...tab, sftpPath: '/home'}}, global: {plugins: [pinia]}})
  await flushPromises()
  await wrapper.get('[data-testid="sftp-file-list"]').trigger('contextmenu', {clientX: 50, clientY: 50})
  let menu = document.querySelector('[data-ui="context-menu"]')!
  expect(menu.textContent).toContain('复制当前目录路径')
  expect(menu.textContent).toContain('新建文件夹')
  expect(menu.textContent).not.toContain('重命名')
  const refresh = Array.from(menu.querySelectorAll('button')).find(b => b.textContent === '刷新')!
  const previous = vi.mocked(sshService.sftpList).mock.calls.length
  refresh.click()
  await flushPromises()
  expect(vi.mocked(sshService.sftpList).mock.calls.length).toBeGreaterThan(previous)
  await wrapper.get('[data-testid="sftp-entry"]').trigger('contextmenu')
  menu = document.querySelector('[data-ui="context-menu"]')!
  expect(menu.textContent).toContain('刷新')
  expect(menu.textContent).toContain('复制完整路径')
  expect(menu.textContent).toContain('重命名')
  wrapper.unmount()
})
