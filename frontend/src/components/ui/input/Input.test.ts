import {mount} from '@vue/test-utils'
import {describe, expect, it} from 'vitest'
import Input from './Input.vue'

describe('Input clear action', () => {
  it('clears through the input event without submitting a form', async () => {
    const wrapper = mount(Input, {props: {modelValue: 'server'}, attrs: {'aria-label': '服务器'}})
    expect(wrapper.get('button').attributes('type')).toBe('button')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([''])
    expect(wrapper.get('input').element.value).toBe('')
    expect(wrapper.get('input').attributes('aria-label')).toBe('服务器')
  })
  it.each([
    {modelValue: ''},
    {modelValue: 'value', clearable: false},
  ])('hides the action when unavailable', (props) => {
    expect(mount(Input, {props: {modelValue: props.modelValue, clearable: props.clearable ?? true}}).find('button').exists()).toBe(false)
  })
  it.each([{readonly: true}, {disabled: true}, {type: 'number'}])('respects input restrictions', (attrs) => {
    expect(mount(Input, {props: {modelValue: '123'}, attrs}).find('button').exists()).toBe(false)
  })
})
