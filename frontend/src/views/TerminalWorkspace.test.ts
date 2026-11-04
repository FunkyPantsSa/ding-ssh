import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import {defineComponent, nextTick} from 'vue'
import TerminalWorkspace from './TerminalWorkspace.vue'

describe('TerminalWorkspace persistence boundary', () => {
  it('keeps terminal children mounted while the view is hidden', async () => {
    let mounts = 0
    const Child = defineComponent({
      name: 'FakeTerminal',
      template: '<div class="fake-terminal">terminal</div>',
      mounted() {
        mounts++
      },
    })

    const wrapper = mount(TerminalWorkspace, {props: {active: true}, slots: {default: Child}})
    expect(wrapper.get('[data-ui="workspace"]').isVisible()).toBe(true)
    expect(wrapper.findAll('.fake-terminal')).toHaveLength(1)

    await wrapper.setProps({active: false})
    await nextTick()

    // v-show 只隐藏，不卸载：切换视图不会重连 / 丢会话。
    expect(wrapper.findAll('.fake-terminal')).toHaveLength(1)
    expect((wrapper.get('[data-ui="workspace"]').element as HTMLElement).style.display).toBe('none')
    expect(mounts).toBe(1)
  })
})
