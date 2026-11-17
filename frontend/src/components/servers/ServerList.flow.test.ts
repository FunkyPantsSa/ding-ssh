import {mount, flushPromises} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import ServerList from '../ServerList.vue'
import {useSessionsStore} from '../../stores/sessions'
import {useUIStore} from '../../stores/ui'
import type {ServerNode} from '../../types'

vi.mock('../../services/ssh', () => ({
  sshService: {
    getServers: vi.fn(),
    saveServer: vi.fn(),
    deleteServer: vi.fn(),
    testServer: vi.fn(),
    testServers: vi.fn(),
    selectKeyFile: vi.fn(),
    getPlatform: vi.fn(),
    getLocalShellOptions: vi.fn(),
  },
}))
vi.mock('../../services/groups', () => ({
  groupService: {list: vi.fn(), add: vi.fn(), rename: vi.fn(), remove: vi.fn()},
}))
vi.mock('../../services/credentials', () => ({
  credentialService: {list: vi.fn(), save: vi.fn(), remove: vi.fn()},
}))
vi.mock('../../../wailsjs/runtime/runtime', () => ({
  ClipboardSetText: vi.fn(),
  ClipboardGetText: vi.fn(),
  EventsOn: vi.fn(() => () => {}),
  EventsOff: vi.fn(),
}))

const {sshService} = await import('../../services/ssh')
const {groupService} = await import('../../services/groups')
const {credentialService} = await import('../../services/credentials')

const node: ServerNode = {
  id: 's1',
  name: '生产服务器',
  group: '',
  host: '10.0.0.1',
  port: 22,
  user: 'root',
  authType: 'password',
  bgImage: '',
  blurAmount: 0,
  envVars: {},
}

async function mountList() {
  const pinia = createPinia()
  setActivePinia(pinia)
  vi.mocked(sshService.getServers).mockResolvedValue([node])
  vi.mocked(groupService.list).mockResolvedValue([])
  vi.mocked(credentialService.list).mockResolvedValue([])
  const wrapper = mount(ServerList, {global: {plugins: [pinia]}})
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.mocked(sshService.getServers).mockResolvedValue([])
  vi.mocked(groupService.list).mockResolvedValue([])
  vi.mocked(credentialService.list).mockResolvedValue([])
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('server business flow', () => {
  it('renders saved servers and connects through the row action', async () => {
    const wrapper = await mountList()
    expect(wrapper.get('[data-testid="server-row-s1"]').text()).toContain('生产服务器')

    await wrapper.get('[data-testid="server-connect-s1"]').trigger('click')
    await flushPromises()

    const sessions = useSessionsStore()
    const ui = useUIStore()
    expect(sessions.tabs).toHaveLength(1)
    expect(sessions.tabs[0].node.id).toBe('s1')
    expect(ui.view).toBe('workspace')
    wrapper.unmount()
  })

  it('deletes a server only after the confirmation dialog', async () => {
    const wrapper = await mountList()
    vi.mocked(sshService.deleteServer).mockResolvedValue()

    await wrapper.get('[data-testid="server-delete-s1"]').trigger('click')
    await flushPromises()
    const confirm = document.querySelector<HTMLElement>('[data-testid="server-delete-confirm"]')
    expect(confirm).not.toBeNull()

    confirm!.click()
    await flushPromises()
    expect(sshService.deleteServer).toHaveBeenCalledWith('s1')
    wrapper.unmount()
  })

  it('opens the edit dialog on request from another page and syncs the open tab', async () => {
    const wrapper = await mountList()
    const sessions = useSessionsStore()
    const ui = useUIStore()
    sessions.openTab({...node, password: 'secret'})
    vi.mocked(sshService.saveServer).mockImplementation(async (saved) => ({...saved, id: 's1'}))

    ui.requestEditServer('s1')
    await flushPromises()
    expect(document.body.textContent).toContain('编辑服务器')

    const host = document.querySelector<HTMLInputElement>('#server-host')!
    host.value = '10.0.0.9'
    host.dispatchEvent(new Event('input', {bubbles: true}))
    const password = document.querySelector<HTMLInputElement>('#server-password')!
    password.value = 'secret'
    password.dispatchEvent(new Event('input', {bubbles: true}))
    const save = Array.from(document.querySelectorAll<HTMLElement>('button')).find((button) => button.textContent?.trim() === '保存')!
    save.click()
    await flushPromises()

    expect(sshService.saveServer).toHaveBeenCalled()
    expect(sessions.tabs[0].node.host).toBe('10.0.0.9')
    wrapper.unmount()
  })

  it('marks at most one group as selected and keeps 未分组 out of the drop-target style when idle', async () => {
    const wrapper = await mountList()
    const items = wrapper.findAll('.side-group-item')
    expect(items).toHaveLength(2)

    const [all, ungrouped] = items
    expect(all.classes()).toContain('active')
    expect(all.classes()).not.toContain('drop-target')
    // '' 是「未分组」的真实标识，空闲时不能被当成拖放目标高亮
    expect(ungrouped.classes()).not.toContain('active')
    expect(ungrouped.classes()).not.toContain('drop-target')

    await ungrouped.trigger('click')
    expect(all.classes()).not.toContain('active')
    expect(ungrouped.classes()).toContain('active')
    expect(ungrouped.classes()).not.toContain('drop-target')
    wrapper.unmount()
  })
})
