import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import Button from './Button.vue'

describe('ui.Button', () => {
  it('exposes a disabled, busy button while loading', () => {
    const wrapper = mount(Button, {props: {variant: 'primary', loading: true}, slots: {default: '保存'}})

    expect(wrapper.attributes('aria-busy')).toBe('true')
    expect((wrapper.element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.classes()).toContain('ui-button-primary')
    expect(wrapper.text()).toContain('保存')
  })
})
