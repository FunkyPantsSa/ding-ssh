import {mount} from '@vue/test-utils'
import {expect, it, vi} from 'vitest'
import ServerStatusBar from './ServerStatusBar.vue'

const holder = vi.hoisted(() => ({state: null as unknown as {
  sysInfoCollectEnabled: boolean
  sysInfoRealtimeEnabled: boolean
  setSysInfoRealtimeEnabled: ReturnType<typeof vi.fn>
}}))
const service = vi.hoisted(() => ({
  start: vi.fn(async () => {}),
  stop: vi.fn(async () => {}),
  setIdle: vi.fn(async () => {}),
}))
const onSnapshot = vi.hoisted(() => vi.fn(() => () => {}))
const realtime = vi.hoisted(() => ({set: vi.fn()}))

vi.mock('../stores/settings', async () => {
  const {reactive} = await import('vue')
  const state = reactive({
    sysInfoCollectEnabled: true,
    sysInfoRealtimeEnabled: false,
    setSysInfoRealtimeEnabled: vi.fn(async () => {}),
  })
  holder.state = state
  return {useSettingsStore: () => state}
})
vi.mock('../services/sysinfo', async () => {
  const {reactive} = await import('vue')
  const overrides = reactive<Record<string, boolean>>({})
  realtime.set = vi.fn(async (id: string, enabled: boolean) => {
    overrides[id] = enabled
  })
  return {
    onSysInfoSnapshot: onSnapshot,
    sysInfoService: service,
    sysInfoRealtimeFor: (id: string | undefined, fallback: boolean) =>
      id && id in overrides ? overrides[id] : fallback,
    setSysInfoRealtime: realtime.set,
    forgetSysInfoRealtime: vi.fn(),
  }
})
vi.mock('../utils/clipboard', () => ({copyText: vi.fn(async () => {})}))

it('toggles between single snapshot and live refresh from one button', async () => {
  const wrapper = mount(ServerStatusBar, {
    props: {tab: {status: 'connected', sessionId: 'sess-1', node: {id: 'node-1'}} as never},
  })
  await wrapper.vm.$nextTick()

  const toggle = wrapper.get('.statusbar-live')
  expect(toggle.text()).toBe('单次')
  expect(toggle.attributes('aria-pressed')).toBe('false')
  expect(wrapper.get('[role="tooltip"]').text()).toContain('单次快照')

  await toggle.trigger('click')
  await wrapper.vm.$nextTick()
  expect(realtime.set).toHaveBeenCalledWith('sess-1', true)
  expect(holder.state.setSysInfoRealtimeEnabled).not.toHaveBeenCalled()
  expect(service.start).toHaveBeenCalledWith('sess-1')
  expect(toggle.text()).toBe('实时')
  expect(toggle.attributes('aria-pressed')).toBe('true')
  expect(wrapper.get('[role="tooltip"]').text()).toContain('实时刷新')

  await toggle.trigger('click')
  await wrapper.vm.$nextTick()
  expect(realtime.set).toHaveBeenCalledWith('sess-1', false)
  expect(toggle.text()).toBe('单次')
  expect(toggle.attributes('aria-pressed')).toBe('false')
  wrapper.unmount()
})
