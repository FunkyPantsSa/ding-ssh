import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import ServerRow from './ServerRow.vue'
import type {ServerNode} from '../../types'

const node: ServerNode = {
  id: 's1',
  name: '生产服务器',
  group: '',
  host: '10.0.0.1',
  port: 22,
  user: 'root',
  authType: 'password',
  bgImage: '',
  blurAmount: 0,
  envVars: {},
}

describe('ServerRow accessibility', () => {
  it('exposes the row action as a real button and keyboard menu shortcut', async () => {
    const wrapper = mount(ServerRow, {
      props: {node, status: 'connected', active: false, dragging: false, badge: null, copied: false},
    })

    const main = wrapper.get('.server-row-main')
    expect(main.element.tagName).toBe('BUTTON')
    expect(main.attributes('type')).toBe('button')
    expect(main.attributes('aria-label')).toContain('生产服务器')

    await main.trigger('click')
    expect(wrapper.emitted('connect')).toHaveLength(1)

    await main.trigger('keydown', {key: 'F10', shiftKey: true})
    expect(wrapper.emitted('menu')).toHaveLength(1)
  })
})
