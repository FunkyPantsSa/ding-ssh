import {mount} from '@vue/test-utils'
import {expect, it, vi} from 'vitest'
import ServerConfigSummary from './ServerConfigSummary.vue'
import type {ServerNode} from '../../types'
vi.mock('../../services/serverSnapshot', () => ({getServerSnapshot: () => ({osName:'openEuler 22.03', architecture:'aarch64', cpuCount:24, memTotalMb:65536, diskUsage:[], collectedAt:1})}))
it('shows full configuration together in the expandable card', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one'} as ServerNode,status:'connected'}})
 expect(wrapper.find('.collapsed-label').text()).toContain('配置信息')
 await wrapper.find('.config-trigger').trigger('click')
 const card = wrapper.find('.details')
 for (const value of ['openEuler 22.03','aarch64','24 核','64 GiB','本地文件系统']) expect(card.text()).toContain(value)
 expect(wrapper.find('.config-trigger').attributes('aria-expanded')).toBe('true')
})

vi.mock('../../stores/sessions', () => ({useSessionsStore: () => ({tabs: refreshState.tabs})}))
vi.mock('../../stores/settings', () => ({useSettingsStore: () => ({sysInfoCollectEnabled: refreshState.enabled})}))
vi.mock('../../services/sysinfo', () => ({sysInfoService: {start: refreshState.start, stop: refreshState.stop}}))
const refreshState = vi.hoisted(() => ({tabs: [] as any[], enabled:true, start:vi.fn().mockResolvedValue(undefined), stop:vi.fn().mockResolvedValue(undefined)}))

it('supports Escape and announces refresh prerequisites and failures', async () => {
 const wrapper = mount(ServerConfigSummary, {props:{node:{id:'one',host:'host',port:22} as ServerNode,status:'connected'}})
 await wrapper.get('.config-trigger').trigger('click')
 await wrapper.get('.config-trigger').trigger('keydown', {key:'Escape'})
 expect(wrapper.get('.config-trigger').attributes('aria-expanded')).toBe('false')
 await wrapper.get('.details button').trigger('click')
 expect(wrapper.get('[role="status"]').text()).toContain('请先连接')
 refreshState.tabs=[{node:{id:'one',host:'host',port:22},status:'connected',sessionId:'ssh-one'}]
 refreshState.start.mockRejectedValueOnce(new Error('offline'))
 await wrapper.get('.details button').trigger('click')
 await new Promise(resolve => setTimeout(resolve,0))
 expect(wrapper.get('[role="status"]').text()).toBe('刷新失败')
 await wrapper.get('.details button').trigger('click')
 await new Promise(resolve => setTimeout(resolve,0))
 expect(wrapper.get('[role="status"]').text()).toBe('已请求刷新')
 wrapper.unmount()
 refreshState.tabs=[]
})
