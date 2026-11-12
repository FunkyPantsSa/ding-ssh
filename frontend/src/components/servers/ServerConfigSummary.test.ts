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
