import {mount} from '@vue/test-utils'
import {afterEach, describe, expect, it} from 'vitest'
import TunnelCard from './TunnelCard.vue'
import type {TunnelInfo} from '@/types'

const tunnel: TunnelInfo = {
  id: 't1',
  name: 'test',
  serverId: 's1',
  serverName: '测试服务器',
  serverHost: '10.0.0.1',
  listenHost: '0.0.0.0',
  mode: 'local',
  localPort: 13306,
  remoteHost: '172.20.50.66',
  remotePort: 32280,
  status: 'stopped',
  startedAt: 0,
}

function mountCard() {
  return mount(TunnelCard, {
    attachTo: document.body,
    props: {
      tunnel,
      address: '0.0.0.0:13306',
      addressLabel: '本机监听地址',
      target: '172.20.50.66:32280',
      copyOptions: ['100.108.95.99:13306', '127.0.0.1:13306'],
      copied: false,
      busy: false,
    },
  })
}

function menus(wrapper: ReturnType<typeof mountCard>): HTMLDetailsElement[] {
  return wrapper.findAll('details').map((menu) => menu.element as HTMLDetailsElement)
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('tunnel card menus', () => {
  it('closes open menus when clicking outside of them', () => {
    const wrapper = mountCard()
    const [copyMenu, moreMenu] = menus(wrapper)
    copyMenu.open = true
    moreMenu.open = true

    document.body.dispatchEvent(new Event('pointerdown', {bubbles: true}))

    expect(copyMenu.open).toBe(false)
    expect(moreMenu.open).toBe(false)
    wrapper.unmount()
  })

  it('keeps a menu open when the click is inside it', () => {
    const wrapper = mountCard()
    const [copyMenu] = menus(wrapper)
    copyMenu.open = true

    copyMenu.dispatchEvent(new Event('pointerdown', {bubbles: true}))

    expect(copyMenu.open).toBe(true)
    wrapper.unmount()
  })

  it('closes menus with Escape and keeps only one menu open at a time', () => {
    const wrapper = mountCard()
    const [copyMenu, moreMenu] = menus(wrapper)
    copyMenu.open = true
    moreMenu.open = true

    copyMenu.dispatchEvent(new Event('toggle'))
    expect(copyMenu.open).toBe(true)
    expect(moreMenu.open).toBe(false)

    document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))
    expect(copyMenu.open).toBe(false)
    wrapper.unmount()
  })
})
