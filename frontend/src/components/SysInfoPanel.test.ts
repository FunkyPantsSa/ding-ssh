import {mount, flushPromises} from '@vue/test-utils'
import {beforeEach, describe, expect, it, vi} from 'vitest'

const mocks = vi.hoisted(() => ({start: vi.fn().mockResolvedValue(undefined), off: vi.fn(), snapshot: null as null | ((s: any) => void)}))
vi.mock('../services/sysinfo', () => ({
  sysInfoService: {start: mocks.start, setIdle: vi.fn()},
  onSysInfoSnapshot: (_id: string, handler: (s: any) => void) => {mocks.snapshot = handler; return mocks.off},
}))
vi.mock('../stores/settings', () => ({useSettingsStore: () => ({sysInfoCollectEnabled: true, sysInfoRealtimeEnabled: false})}))
vi.mock('../stores/sessions', () => ({useSessionsStore: () => ({showRightPanel: vi.fn()})}))
import SysInfoPanel from './SysInfoPanel.vue'
import type {SessionTab} from '../types'
const tab = {sessionId:'ssh-1', status:'connected'} as SessionTab

describe('运行状态面板', () => {
  beforeEach(() => {vi.clearAllMocks(); localStorage.clear()})
  it('requests a fresh snapshot every time the panel opens, even without realtime', async () => {
    const first = mount(SysInfoPanel, {props:{tab}})
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledWith('ssh-1')
    first.unmount()
    const second = mount(SysInfoPanel, {props:{tab}})
    await flushPromises()
    expect(mocks.start).toHaveBeenCalledTimes(2)
    expect(mocks.off).toHaveBeenCalledTimes(1)
    second.unmount()
  })
  it('persists custom card order and keeps readable process headers', async () => {
    const wrapper = mount(SysInfoPanel, {props:{tab}})
    await flushPromises()
    await wrapper.findAll('button').find(b => b.text()==='调整卡片顺序')!.trigger('click')
    await wrapper.get('[aria-label="下移CPU"]').trigger('click')
    expect(JSON.parse(localStorage.getItem('sysinfo-card-order')!)[0]).toBe('memory')
    expect(wrapper.get('.process-table').findAll('th').map(th => th.text())).toContain('累计 CPU 时间')
    wrapper.unmount()
    const reopened = mount(SysInfoPanel, {props:{tab}})
    expect(reopened.findAll('.resource-card')[0]!.attributes('style')).toContain('order: 1')
    reopened.unmount()
  })
})
