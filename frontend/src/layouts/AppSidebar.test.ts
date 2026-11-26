import {mount} from '@vue/test-utils'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {reactive} from 'vue'
const mockSettings = reactive({tabBarPlacement: 'top', settingsPlacement: 'bottom', navSectionOrder: 'nav,sessions,tabs'})
vi.mock('@/stores/servers', () => ({useServersStore: () => ({servers: []})}))
vi.mock('@/stores/sessions', () => ({useSessionsStore: () => ({tabs: []})}))
vi.mock('@/stores/settings', () => ({useSettingsStore: () => mockSettings}))
const mockUI = {view: 'home', showSettings: vi.fn(), requestNewServer: vi.fn()}
vi.mock('@/stores/ui', () => ({useUIStore: () => mockUI}))
import AppSidebar from './AppSidebar.vue'
afterEach(() => {
  vi.useRealTimers()
  mockSettings.settingsPlacement = 'bottom'
})
function sidebar() {
 return mount(AppSidebar, {props: {collapsed: true, onlineCount: 0}, global: {stubs: {Icon: true, TabBar: true}}})
}
describe('sidebar hover preview', () => {
 it('opens only after dwell and closes after leaving, without changing pinned state', async () => {
  vi.useFakeTimers(); const view = sidebar()
  await view.trigger('pointerenter'); await view.get('.nav-mark').trigger('pointerenter')
  await vi.advanceTimersByTimeAsync(499); expect(view.classes()).not.toContain('nav-preview')
  await vi.advanceTimersByTimeAsync(1); expect(view.classes()).toContain('nav-preview')
  expect(view.emitted('update:collapsed')).toBeUndefined()
  await view.trigger('pointerleave'); await vi.advanceTimersByTimeAsync(249); expect(view.classes()).toContain('nav-preview')
  await vi.advanceTimersByTimeAsync(1); expect(view.classes()).not.toContain('nav-preview'); view.unmount()
 })
 it('cancels a pending open when pointer leaves the trigger', async () => {
  vi.useFakeTimers(); const view = sidebar()
  await view.get('.nav-mark').trigger('pointerenter'); await view.get('.nav-mark').trigger('pointerleave')
  await vi.advanceTimersByTimeAsync(600); expect(view.classes()).not.toContain('nav-preview'); view.unmount()
 })
 it('pins the navigation when the expanded preview button is clicked', async () => {
  vi.useFakeTimers(); const view = sidebar()
  await view.get('.nav-mark').trigger('pointerenter'); await vi.advanceTimersByTimeAsync(500)
  await view.get('[aria-label="固定展开导航"]').trigger('click')
  expect(view.emitted('update:collapsed')).toEqual([[false]]); view.unmount()
 })
 it('keeps settings at the bottom by default and honors an explicit navigation placement', async () => {
  const expanded = mount(AppSidebar, {props: {collapsed: false, onlineCount: 0}, global: {stubs: {Icon: true, TabBar: true}}})
  expect(expanded.find('.nav-body [data-ui="nav-item-settings"]').exists()).toBe(false)
  expect(expanded.find('.nav-foot-capsule [data-ui="nav-item-settings-bottom"]').exists()).toBe(true)
  mockSettings.settingsPlacement = 'sidebar'
  await expanded.vm.$nextTick()
  expect(expanded.find('.nav-body [data-ui="nav-item-settings"]').exists()).toBe(true)
  expect(expanded.find('.nav-foot-capsule').exists()).toBe(false)
  expanded.unmount()
 })
 it('triggers new server creation from the sidebar navigation', async () => {
  mockUI.requestNewServer.mockClear()
  const expanded = mount(AppSidebar, {props: {collapsed: false, onlineCount: 0}, global: {stubs: {Icon: true, TabBar: true}}})
  await expanded.get('[data-ui="nav-item-new-server"]').trigger('click')
  expect(mockUI.requestNewServer).toHaveBeenCalledTimes(1)
  expanded.unmount()
 })
})
