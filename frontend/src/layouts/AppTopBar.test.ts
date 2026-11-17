import {mount} from '@vue/test-utils'
import {expect, it} from 'vitest'
import AppTopBar from './AppTopBar.vue'
it('exposes named actions and reflects the tools toggle state', async () => {
 const wrapper = mount(AppTopBar, {props:{title:'终端',subtitle:'',navCollapsed:true,workspace:true,servers:false,onlineCount:2,contextVisible:false}})
 await wrapper.get('[aria-label="展开导航"]').trigger('click')
 await wrapper.get('[aria-label="打开命令面板"]').trigger('click')
 const tools = wrapper.get('[aria-label="侧栏工具（SFTP / 运行状态）"]')
 expect(tools.attributes('aria-pressed')).toBe('false')
 await tools.trigger('click')
 await wrapper.setProps({contextVisible:true})
 expect(tools.attributes('aria-pressed')).toBe('true')
 for(const event of ['expandNav','command','toggleContext']) expect(wrapper.emitted(event)).toHaveLength(1)
 await wrapper.setProps({workspace:false,servers:false})
 expect(wrapper.find('[aria-label="侧栏工具（SFTP / 运行状态）"]').exists()).toBe(false)
 wrapper.unmount()
})
