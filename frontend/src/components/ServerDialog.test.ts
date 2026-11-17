import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import ServerDialog from './ServerDialog.vue'
import {useServersStore} from '../stores/servers'
import type {ServerNode} from '../types'

vi.mock('../services/ssh', () => ({
  sshService: {
    selectKeyFile: vi.fn(),
    testServerLogin: vi.fn(),
  },
}))
vi.mock('../services/credentials', () => ({
  credentialService: {list: vi.fn(), save: vi.fn(), remove: vi.fn()},
}))

const {credentialService} = await import('../services/credentials')

const node: ServerNode = {
  id: 's1',
  name: '测试机',
  group: '测试环境',
  host: '10.0.0.1',
  port: 22,
  user: 'root',
  authType: 'password',
  bgImage: '',
  blurAmount: 0,
  envVars: {},
}

async function mountDialog(editing: ServerNode | null = null, defaultGroup = '') {
  const pinia = createPinia()
  setActivePinia(pinia)
  const servers = useServersStore()
  servers.servers = [{...node}]
  const wrapper = mount(ServerDialog, {
    props: {modelValue: false, editing, defaultGroup},
    global: {plugins: [pinia]},
  })
  await wrapper.setProps({modelValue: true})
  await flushPromises()
  return wrapper
}

enableAutoUnmount(afterEach)

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.mocked(credentialService.list).mockResolvedValue([])
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('server dialog', () => {
  it('never offers the "all" sentinel as a group option', async () => {
    await mountDialog(null, 'all')

    const groupInput = document.querySelector<HTMLInputElement>('input[aria-label="服务器分组"]')!
    groupInput.click()
    await flushPromises()

    const options = Array.from(document.querySelectorAll<HTMLElement>('[role=option]')).map((option) => option.textContent?.trim())
    expect(options).toContain('测试环境')
    expect(options).not.toContain('all')
  })

  it('collapses jump host, host key alias and known_hosts into advanced settings', async () => {
    await mountDialog()

    const toggle = document.querySelector<HTMLElement>('.server-advanced-toggle')!
    expect(toggle.textContent).toContain('高级设置')
    expect(toggle.getAttribute('aria-expanded')).toBe('false')

    toggle.click()
    await flushPromises()
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(document.body.textContent).toContain('跳板机连接路径')
  })

  it('auto-expands advanced settings when the server already uses them', async () => {
    await mountDialog({...node, jumpServerIds: ['s2'], hostKeyAlias: 'jump-alias'})

    const toggle = document.querySelector<HTMLElement>('.server-advanced-toggle')!
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(document.body.textContent).toContain('主机密钥别名')
  })
})
