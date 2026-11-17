import {mount} from '@vue/test-utils'
import {afterEach, describe, expect, it, vi} from 'vitest'
vi.mock('@/stores/servers', () => ({useServersStore: () => ({servers: []})}))
vi.mock('@/stores/sessions', () => ({useSessionsStore: () => ({tabs: []})}))
vi.mock('@/stores/settings', () => ({useSettingsStore: () => ({tabBarPlacement: 'top', navSectionOrder: 'nav,sessions,tabs'})}))
vi.mock('@/stores/ui', () => ({useUIStore: () => ({view: 'home'})}))
import AppSidebar from './AppSidebar.vue'
afterEach(() => {vi.useRealTimers()})
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
})
