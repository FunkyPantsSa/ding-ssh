import {mount} from '@vue/test-utils'
import {expect, it, vi} from 'vitest'
import {nextTick} from 'vue'
import ServerConfigSummary from './ServerConfigSummary.vue'
import type {ServerNode} from '../../types'
vi.mock('../../services/serverSnapshot', () => ({getServerSnapshot: () => ({osName:'openEuler 22.03', architecture:'aarch64', cpuCount:24, memTotalMb:65536, diskUsage:[], collectedAt:1})}))
it('shows full configuration together in the expandable card', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one'} as ServerNode,status:'connected'}})
 expect(wrapper.find('.collapsed-label').text()).toContain('配置信息')
 await wrapper.find('.config-trigger').trigger('click')
 const card = wrapper.find('.details')
 // 浮层材质由 style.css 的 :is(..., .config-popover) 规则提供（近不透明底 + 高斯模糊）
 expect(card.classes()).toContain('config-popover')
 for (const value of ['openEuler 22.03','aarch64','24 核','64 GiB','本地文件系统']) expect(card.text()).toContain(value)
 expect(wrapper.find('.config-trigger').attributes('aria-expanded')).toBe('true')
 wrapper.unmount()
})

it('opens the card when any configuration tag is clicked, and closes on a second click', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one'} as ServerNode,status:'connected'}})
 for (const id of ['config-os','config-cpu','config-memory','config-disk']) {
   const tag = wrapper.get(`[data-testid="${id}"]`)
   await tag.trigger('click')
   expect(tag.attributes('aria-expanded')).toBe('true')
   expect(wrapper.get('.summary').classes()).toContain('pinned')
   await tag.trigger('click')
   expect(tag.attributes('aria-expanded')).toBe('false')
   expect(wrapper.get('.summary').classes()).not.toContain('pinned')
 }
 wrapper.unmount()
})

it('closes when the trigger is clicked again, on outside pointerdown, and via the close button', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one'} as ServerNode,status:'connected'}})
 const trigger = wrapper.get('.config-trigger')
 await trigger.trigger('click')
 expect(wrapper.get('.summary').classes()).toContain('pinned')
 await trigger.trigger('click')
 expect(trigger.attributes('aria-expanded')).toBe('false')
 expect(wrapper.get('.summary').classes()).not.toContain('pinned')

 await trigger.trigger('click')
 document.body.dispatchEvent(new Event('pointerdown', {bubbles:true}))
 await nextTick()
 expect(trigger.attributes('aria-expanded')).toBe('false')

 await trigger.trigger('click')
 expect(wrapper.get('.details-close').attributes('aria-label')).toBe('关闭')
 await wrapper.get('.details-close').trigger('click')
 expect(trigger.attributes('aria-expanded')).toBe('false')
 wrapper.unmount()
})

it('restores focus to the trigger after closing the card', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one'} as ServerNode,status:'connected'}, attachTo: document.body})
 const trigger = wrapper.get('.config-trigger')
 await trigger.trigger('click')
 await wrapper.get('.details-close').trigger('click')
 expect(document.activeElement).toBe(trigger.element)
 wrapper.unmount()
})

it('keeps the card inside the scroll panel and flips it above the row near the bottom', async () => {
 const scroller = document.createElement('div')
 scroller.className = 'server-list-panel'
 document.body.appendChild(scroller)
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one'} as ServerNode,status:'connected'}, attachTo: scroller})
 const summary = wrapper.get('.summary').element as HTMLElement
 const panel = wrapper.get('.details').element as HTMLElement
 const rect = (top: number, bottom: number): DOMRect => ({top, bottom, height: bottom - top, left: 0, right: 1000, width: 1000, x: 0, y: top, toJSON: () => ({})}) as DOMRect
 vi.spyOn(scroller, 'getBoundingClientRect').mockReturnValue(rect(0, 400))
 vi.spyOn(summary, 'getBoundingClientRect').mockReturnValue(rect(340, 380))
 Object.defineProperty(panel, 'scrollHeight', {value: 300, configurable: true})
 await wrapper.get('[data-testid="config-os"]').trigger('click')
 await nextTick()
 await nextTick()
 expect(wrapper.get('.details').classes()).toContain('drop-up')
 expect(wrapper.get('.details').attributes('style')).toContain('max-height: 330px')
 wrapper.unmount()
 scroller.remove()
})

vi.mock('../../stores/sessions', () => ({useSessionsStore: () => ({tabs: refreshState.tabs})}))
vi.mock('../../stores/settings', () => ({useSettingsStore: () => ({sysInfoCollectEnabled: refreshState.enabled})}))
vi.mock('../../services/sysinfo', () => ({
  sysInfoService: {start: refreshState.start, stop: refreshState.stop},
  onSysInfoSnapshot: (_id: string, handler: (s: any) => void) => { refreshState.snapshot = handler; return refreshState.off },
}))
const refreshState = vi.hoisted(() => ({
  tabs: [] as any[],
  enabled: true,
  snapshot: null as ((s: any) => void) | null,
  off: vi.fn(),
  start: vi.fn().mockResolvedValue(undefined),
  stop: vi.fn().mockResolvedValue(undefined),
}))

it('supports Escape and announces refresh prerequisites and failures', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one',host:'host',port:22} as ServerNode,status:'connected'}})
 await wrapper.get('.config-trigger').trigger('click')
 await wrapper.get('.config-trigger').trigger('keydown', {key:'Escape'})
 expect(wrapper.get('.config-trigger').attributes('aria-expanded')).toBe('false')
 await wrapper.get('[data-testid="refresh-config"]').trigger('click')
 expect(wrapper.get('[role="status"]').text()).toContain('请先连接')
 refreshState.tabs=[{node:{id:'one',host:'host',port:22},status:'connected',sessionId:'ssh-one'}]
 refreshState.start.mockRejectedValueOnce(new Error('offline'))
 await wrapper.get('[data-testid="refresh-config"]').trigger('click')
 await new Promise(resolve => setTimeout(resolve,0))
 expect(wrapper.get('[role="status"]').text()).toBe('刷新失败')
 await wrapper.get('[data-testid="refresh-config"]').trigger('click')
 await new Promise(resolve => setTimeout(resolve,0))
 expect(wrapper.get('[role="status"]').text()).toBe('已请求刷新')
 expect(wrapper.get('[data-testid="refresh-config"]').attributes('disabled')).toBeDefined()
 refreshState.snapshot?.({error: undefined, collectedAt: Date.now()})
 await nextTick()
 expect(wrapper.get('[role="status"]').text()).toBe('刷新完成')
 expect(refreshState.off).toHaveBeenCalled()
 expect(wrapper.get('[data-testid="refresh-config"]').attributes('disabled')).toBeUndefined()

 await wrapper.get('[data-testid="refresh-config"]').trigger('click')
 await new Promise(resolve => setTimeout(resolve,0))
 refreshState.snapshot?.({error: '远程无输出', collectedAt: Date.now()})
 await nextTick()
 expect(wrapper.get('[role="status"]').text()).toBe('刷新失败')
 wrapper.unmount()
 refreshState.tabs=[]
})
