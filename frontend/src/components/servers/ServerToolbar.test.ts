import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import ServerToolbar from './ServerToolbar.vue'

describe('server toolbar contract', () => {
  it('publishes stable actions and forwards the search value', async () => {
    const wrapper = mount(ServerToolbar, {props: {modelValue: '', total: 2, online: 1, testing: false}})

    await wrapper.get('[data-testid="server-search"]').setValue('prod')
    await wrapper.get('[data-testid="server-create"]').trigger('click')
    await wrapper.get('[data-testid="server-test-all"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['prod'])
    expect(wrapper.emitted('create')).toHaveLength(1)
    expect(wrapper.emitted('testAll')).toHaveLength(1)
    expect(wrapper.get('[data-testid="server-online-count"]').text()).toContain('1/2')
  })
})
