import {mount} from '@vue/test-utils'
import {nextTick} from 'vue'
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

it('opens menus from the summary, selects an address with Enter, and restores focus on Escape', async () => {
  const wrapper = mountCard()
  try {
    const [copyMenu, moreMenu] = menus(wrapper)
    const copySummary = copyMenu!.querySelector('summary')!
    const moreSummary = moreMenu!.querySelector('summary')!
    copySummary.focus()
    const press = async (key: string) => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {key, bubbles: true, cancelable: true}))
      await nextTick()
    }
    await press('ArrowDown')
    expect(copyMenu!.open).toBe(true)
    expect(document.activeElement?.textContent).toBe('100.108.95.99:13306')
    await press('End')
    expect(document.activeElement?.textContent).toBe('127.0.0.1:13306')
    await press('Enter')
    expect(wrapper.emitted('copy')).toEqual([['127.0.0.1:13306']])
    expect(copyMenu!.open).toBe(false)
    expect(document.activeElement).toBe(copySummary)
    moreSummary.focus()
    await press(' ')
    expect(moreMenu!.open).toBe(true)
    expect(document.activeElement?.textContent).toBe('删除')
    await press('Escape')
    expect(moreMenu!.open).toBe(false)
    expect(document.activeElement).toBe(moreSummary)
    await wrapper.setProps({busy: true})
    await press('ArrowDown')
    expect(document.activeElement).toBe(moreSummary)
    expect(wrapper.emitted('remove')).toBeUndefined()
  } finally { wrapper.unmount() }
})
