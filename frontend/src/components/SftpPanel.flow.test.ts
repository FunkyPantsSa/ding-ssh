import {flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import SftpPanel from './SftpPanel.vue'
import {useSettingsStore} from '../stores/settings'
import {EventsOn} from '../services/desktop'
import {useTransfersStore} from '../stores/transfers'
import type {ServerNode, SessionTab} from '../types'

vi.mock('../services/ssh', () => ({
  onSftpTransfer: vi.fn(() => () => {}),
  onSftpDirUpdated: vi.fn(() => () => {}),
  sshService: {
    sftpList: vi.fn(),
    sftpCancelTransfer: vi.fn().mockResolvedValue(undefined),
    selectLocalFiles: vi.fn(),
    sftpUpload: vi.fn(),
    localPathIsDir: vi.fn(),
    syncSftpToTerminal: vi.fn(),
  },
}))
vi.mock('../services/desktop', () => ({
  EventsOn: vi.fn(() => () => {}),
  ClipboardSetText: vi.fn(),
}))

const {sshService,onSftpTransfer} = await import('../services/ssh')

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

it('recovers from a failed listing through the visible retry action', async () => {
 const pinia=createPinia();setActivePinia(pinia)
 vi.mocked(sshService.sftpList).mockRejectedValueOnce(new Error('permission denied'))
 const wrapper=mount(SftpPanel,{props:{tab:{...tab,sftpPath:'/home'}},global:{plugins:[pinia]}})
 await flushPromises()
 expect(wrapper.text()).toContain('permission denied')
 expect(wrapper.text()).not.toContain('空目录')
 const retry=wrapper.findAll('button').find(button => button.text()==='重试')!
 await retry.trigger('click');await flushPromises()
 expect(wrapper.text()).not.toContain('permission denied')
 expect(wrapper.find('[data-path="/home/logs"]').exists()).toBe(true)
 wrapper.unmount()
})

it('does not sync directory navigation to the terminal when synchronization is disabled', async () => {
 const pinia=createPinia();setActivePinia(pinia)
 const settings=useSettingsStore();settings.loaded=true;settings.sftpToTerminalSync=false
 const wrapper=mount(SftpPanel,{props:{tab:{...tab,sftpPath:'/home'}},global:{plugins:[pinia]}})
 await flushPromises()
 await wrapper.get('[data-path="/home/logs"]').trigger('dblclick');await flushPromises()
 expect(sshService.sftpList).toHaveBeenLastCalledWith('sess-1','/home/logs')
 expect(sshService.syncSftpToTerminal).not.toHaveBeenCalled()
 wrapper.unmount()
})


it('separates same-name tasks and preserves partial bytes on cancellation', async () => {
 const pinia=createPinia();setActivePinia(pinia)
 const wrapper=mount(SftpPanel,{props:{tab:{...tab,sftpPath:'/home'}},global:{plugins:[pinia]}})
 await flushPromises()
 const receive=vi.mocked(onSftpTransfer).mock.calls.at(-1)![1]
 receive({sessionId:'sess-1',transferId:'first',direction:'upload',name:'same.txt',transferred:100,total:1000,done:false})
 receive({sessionId:'sess-1',transferId:'second',direction:'upload',name:'same.txt',transferred:200,total:1000,done:false})
 await flushPromises()
 expect(wrapper.findAll('.prog')).toHaveLength(2)
 const cancel=wrapper.findAll('button').filter(b=>b.text()==='取消')
 await cancel[0].trigger('click')
 expect(sshService.sftpCancelTransfer).toHaveBeenCalledWith('sess-1','upload','@id:first')
 receive({sessionId:'sess-1',transferId:'first',direction:'upload',name:'same.txt',transferred:100,total:1000,done:true,error:'已取消'})
 await flushPromises()
 expect(wrapper.text()).toContain('100 B / 1000 B')
 expect(wrapper.text()).toContain('已取消')
 expect(wrapper.findAll('button').filter(b=>b.text()==='取消')).toHaveLength(1)
 wrapper.unmount()
})


it('records cancellation even when the final progress event has not arrived',async()=>{
 const pinia=createPinia();setActivePinia(pinia)
 const history=useTransfersStore();history.records=[]
 vi.mocked(sshService.selectLocalFiles).mockResolvedValue(['/test/data.txt'])
 vi.mocked(sshService.sftpUpload).mockRejectedValue(new Error('传输已取消'))
 const wrapper=mount(SftpPanel,{props:{tab:{...tab,sftpPath:'/home'}},global:{plugins:[pinia]}})
 await flushPromises()
 await wrapper.get('[data-testid="sftp-upload-menu"]').trigger('click');await flushPromises()
 await wrapper.get('[data-testid="sftp-upload"]').trigger('click');await flushPromises()
 expect(history.records).toHaveLength(1)
 expect(history.records[0].status).toBe('cancelled')
 expect(history.records[0].transferred).toBe(0)
 wrapper.unmount()
})

it('renders a single root slash and clearly styled file actions', async () => {
  const pinia = createPinia(); setActivePinia(pinia)
  const wrapper = mount(SftpPanel, {props: {tab: {...tab, sftpPath:'/home/logs'}}, global: {plugins:[pinia]}})
  await flushPromises()
  const crumbs=wrapper.get('[data-testid="sftp-breadcrumbs"]')
  expect(crumbs.findAll('button').map(button=>button.text())).toEqual(['/','home','logs'])
  expect(crumbs.findAll('span').map(span=>span.text())).toEqual(['/'])
  expect(wrapper.get('[data-testid="sftp-create-folder"]').classes()).toContain('ui-button-ghost')
  expect(wrapper.get('[data-testid="sftp-upload-menu"]').text()).toBe('上传')
  expect(wrapper.find('[role="menu"]').exists()).toBe(false)
  await wrapper.get('[data-testid="sftp-upload-menu"]').trigger('click')
  expect(wrapper.get('[role="menu"]').text()).toContain('上传文件夹')
  await wrapper.get('[role="menu"]').trigger('keydown', {key:'Escape'})
  expect(wrapper.find('[role="menu"]').exists()).toBe(false)
  wrapper.unmount()
})

it('separates directory navigation from actions and shows the complete path on hover', async () => {
  const pinia=createPinia();setActivePinia(pinia)
  const fullPath='/home/root/very-long-directory/logs'
  const wrapper=mount(SftpPanel,{props:{tab:{...tab,sftpPath:fullPath}},global:{plugins:[pinia]}})
  await flushPromises()
  const navigation=wrapper.get('[data-testid="sftp-navigation"]')
  expect(navigation.get('[data-testid="sftp-breadcrumbs"]').attributes('title')).toBe(fullPath)
  expect(navigation.get('[data-testid="sftp-breadcrumbs"]').findAll('button').map(button => button.text())).toEqual(['/','…','very-long-directory','logs'])
  expect(navigation.find('[data-testid="sftp-create-folder"]').exists()).toBe(false)
  const actions=wrapper.get('[data-testid="sftp-actions"]')
  expect(actions.findAll('button')).toHaveLength(2)
  expect(actions.find('[data-testid="sftp-breadcrumbs"]').exists()).toBe(false)
  wrapper.unmount()
})

it('uploads a native file drop only in its target SFTP pane', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  vi.mocked(sshService.localPathIsDir).mockResolvedValue(false)
  vi.mocked(sshService.sftpUpload).mockReset().mockResolvedValue(undefined)
  const first = mount(SftpPanel, {
    props: {tab: {...tab, sftpPath: '/home'}}, global: {plugins: [pinia]},
  })
  const second = mount(SftpPanel, {
    props: {tab: {...tab, clientId: 'tab-2', sessionId: 'sess-2', sftpPath: '/other'}},
    global: {plugins: [pinia]},
  })
  await flushPromises()

  const target = second.get('[data-file-drop-target]').attributes('id')
  for (const [event, receive] of vi.mocked(EventsOn).mock.calls) {
    if (event === 'sftp:files-dropped') receive({target, paths: ['/test/upload.txt']})
  }
  await flushPromises()

  expect(sshService.sftpUpload).toHaveBeenCalledExactlyOnceWith('sess-2', '/test/upload.txt', '/other/upload.txt')
  first.unmount()
  second.unmount()
})
