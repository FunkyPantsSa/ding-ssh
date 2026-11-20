import {mount, flushPromises} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import ServerList from '../ServerList.vue'
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

function node(id: string, name: string, group: string): ServerNode {
  return {
    id,
    name,
    group,
    host: `10.0.0.${id.slice(1)}`,
    port: 22,
    user: 'root',
    authType: 'password',
    bgImage: '',
    blurAmount: 0,
    envVars: {},
  }
}

const nestedServers = [
  node('s1', 'prod-hz', '生产环境/华东/杭州'),
  node('s2', 'prod-sh', '生产环境/华东/上海'),
  node('s3', 'prod-gz', '生产环境/华南'),
  node('s4', 'lonely', ''),
]

async function mountList(servers = nestedServers, groups = ['测试环境']) {
  const pinia = createPinia()
  setActivePinia(pinia)
  vi.mocked(sshService.getServers).mockResolvedValue(servers.map((item) => ({...item})))
  vi.mocked(sshService.saveServer).mockImplementation(async (saved) => ({...saved, id: saved.id || 'new'}))
  vi.mocked(groupService.list).mockResolvedValue([...groups])
  vi.mocked(credentialService.list).mockResolvedValue([])
  const wrapper = mount(ServerList, {global: {plugins: [pinia]}})
  await flushPromises()
  return wrapper
}

function rows(wrapper: Awaited<ReturnType<typeof mountList>>) {
  return wrapper.findAll('[data-testid^="server-row-"]')
}

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  vi.mocked(sshService.getServers).mockResolvedValue([])
  vi.mocked(groupService.list).mockResolvedValue([])
  vi.mocked(credentialService.list).mockResolvedValue([])
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('nested group tree', () => {
  it('renders implicit ancestors, expands children and counts servers by subtree', async () => {
    const wrapper = await mountList()

    expect(wrapper.get('[data-group-path="生产环境"]').text()).toContain('生产环境')
    expect(wrapper.get('[data-group-path="生产环境"]').text()).toContain('3')
    expect(wrapper.get('[data-group-path="生产环境/华东"]').text()).toContain('2')
    // 默认展开到二级，杭州/上海直接可见。
    expect(wrapper.get('[data-group-path="生产环境/华东/杭州"]').text()).toContain('1')

    // 折叠一级子组后其子项隐藏；再次展开恢复。
    await wrapper.get('[data-group-path="生产环境/华东"] .group-tree-disclosure').trigger('click')
    expect(wrapper.find('[data-group-path="生产环境/华东/杭州"]').exists()).toBe(false)
    await wrapper.get('[data-group-path="生产环境/华东"] .group-tree-disclosure').trigger('click')
    expect(wrapper.get('[data-group-path="生产环境/华东/杭州"]').text()).toContain('1')

    await wrapper.get('[data-group-path="生产环境/华东"]').trigger('click')
    await flushPromises()
    expect(rows(wrapper)).toHaveLength(2)

    await wrapper.get('[data-group-path="生产环境/华东/杭州"]').trigger('click')
    await flushPromises()
    expect(rows(wrapper)).toHaveLength(1)
    expect(wrapper.get('[data-testid="server-row-s1"]').text()).toContain('prod-hz')
    expect(wrapper.get('.group-breadcrumb .current').text()).toBe('杭州')

    await wrapper.get('[data-group-path="生产环境"]').trigger('click')
    await flushPromises()
    expect(rows(wrapper)).toHaveLength(3)

    await wrapper.get('.side-group-item').trigger('click')
    await flushPromises()
    expect(rows(wrapper)).toHaveLength(4)
    wrapper.unmount()
  })

  it('moves a server onto a nested group by drag and drop', async () => {
    const wrapper = await mountList()
    const dataTransfer = {
      data: {} as Record<string, string>,
      effectAllowed: '',
      dropEffect: '',
      setData(type: string, value: string) {
        this.data[type] = value
      },
      getData(type: string) {
        return this.data[type] ?? ''
      },
    }

    await wrapper.get('[data-testid="server-row-s1"]').trigger('dragstart', {dataTransfer})
    await wrapper.get('[data-group-path="生产环境/华南"]').trigger('dragover', {dataTransfer})
    await wrapper.get('[data-group-path="生产环境/华南"]').trigger('drop', {dataTransfer})
    await flushPromises()

    expect(sshService.saveServer).toHaveBeenCalledWith(expect.objectContaining({id: 's1', group: '生产环境/华南'}))
    wrapper.unmount()
  })

  it('creates a nested group from the sidebar editor', async () => {
    const wrapper = await mountList()
    await wrapper.get('[aria-label="新建分组"]').trigger('click')
    await flushPromises()

    const input = document.querySelector<HTMLInputElement>('#group-editor-input')!
    input.value = '测试环境/预发'
    input.dispatchEvent(new Event('input', {bubbles: true}))
    await wrapper.vm.$nextTick()
    const save = Array.from(document.querySelectorAll<HTMLElement>('button')).find((button) => button.textContent?.trim() === '保存')!
    save.click()
    await flushPromises()

    expect(groupService.add).toHaveBeenCalledWith('测试环境/预发')
    expect(wrapper.find('[data-group-path="测试环境/预发"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('opens the tree-shaped group manager', async () => {
    const wrapper = await mountList()
    await wrapper.get('[aria-label="分组管理"]').trigger('click')
    await flushPromises()

    expect(document.body.textContent).toContain('分组管理')
    expect(document.body.textContent).toContain('每一行右侧的 +')
    expect(document.querySelectorAll('[aria-label="新建子分组"]').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('renames and deletes through the group manager', async () => {
    vi.mocked(groupService.rename).mockResolvedValue()
    vi.mocked(groupService.remove).mockResolvedValue()
    const wrapper = await mountList()
    await wrapper.get('[aria-label="分组管理"]').trigger('click')
    await flushPromises()

    const findRow = (name: string) => Array.from(document.querySelectorAll<HTMLElement>('.row'))
      .find((row) => row.querySelector('.row-name')?.textContent?.trim() === name)!

    const east = findRow('华东')
    east.querySelector<HTMLButtonElement>('[aria-label="重命名"]')!.click()
    await flushPromises()
    const input = document.querySelector<HTMLInputElement>('[aria-label="重命名分组 生产环境/华东"]')!
    input.value = '华东区'
    input.dispatchEvent(new Event('input', {bubbles: true}))
    await wrapper.vm.$nextTick()
    Array.from(document.querySelectorAll<HTMLElement>('button')).find((button) => button.textContent?.trim() === '确定')!.click()
    await flushPromises()
    expect(groupService.rename).toHaveBeenCalledWith('生产环境/华东', '华东区')

    await wrapper.vm.$nextTick()
    const hz = findRow('杭州')
    hz.querySelector<HTMLButtonElement>('[aria-label="删除"]')!.click()
    await flushPromises()
    const confirm = document.querySelector<HTMLElement>('[data-testid="group-delete-confirm"]')
    expect(confirm).toBeNull()
    const deleteButton = Array.from(document.querySelectorAll<HTMLElement>('.delete-panel button'))
      .find((button) => button.textContent?.trim() === '删除')!
    deleteButton.click()
    await flushPromises()
    expect(groupService.remove).toHaveBeenCalledWith('生产环境/华东/杭州', false)
    wrapper.unmount()
  })
})
