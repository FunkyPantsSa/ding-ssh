import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {nextTick} from 'vue'
import {afterEach, expect, it} from 'vitest'
import SearchSelect from './SearchSelect.vue'

enableAutoUnmount(afterEach)

afterEach(() => {
  document.body.innerHTML = ''
})

it('filters by label and keywords, and commits only an existing option', async () => {
  const wrapper = mount(SearchSelect, {
    props: {
      modelValue: 'prod',
      options: [
        {value: 'prod', label: '生产', keywords: 'root 10.0.0.1'},
        {value: 'stage', label: '测试', keywords: 'dev 10.0.0.2'},
      ],
    },
  })
  const input = wrapper.get('input')
  expect(input.element.value).toBe('生产')
  await input.trigger('focus')
  await input.setValue('missing')
  await input.trigger('keydown', {key: 'Enter'})
  expect(document.body.textContent).toContain('没有匹配选项')
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()

  await input.trigger('keydown', {key: 'Escape'})
  expect(input.element.value).toBe('生产')
  await input.trigger('click')
  await input.setValue('DEV 10.0')
  const options = Array.from(document.body.querySelectorAll<HTMLElement>('[role=option]'))
  expect(options.map((option) => option.textContent)).toEqual(['测试'])
  await input.trigger('keydown', {key: 'Enter'})
  expect(wrapper.emitted('update:modelValue')).toEqual([['stage']])
  wrapper.unmount()
})

it('renders the list in an overlay layer and closes it on an outside pointerdown', async () => {
  const wrapper = mount(SearchSelect, {
    props: {
      modelValue: '',
      options: [{value: 'a', label: '选项 A'}],
    },
  })
  await wrapper.get('input').trigger('click')
  await flushPromises()

  const list = document.body.querySelector<HTMLElement>('[role=listbox]')
  expect(list).not.toBeNull()
  // 浮层挂在 body 上，而不是留在表单流里撑高布局
  expect(list!.parentElement).toBe(document.body)
  expect(wrapper.element.contains(list)).toBe(false)

  document.body.dispatchEvent(new Event('pointerdown', {bubbles: true}))
  await nextTick()
  expect(document.body.querySelector('[role=listbox]')).toBeNull()
  wrapper.unmount()
})

it('navigates enabled options with arrows and Home/End while preserving editable input keys', async () => {
  const wrapper = mount(SearchSelect, {attachTo: document.body, props: {
    modelValue: '', options: [
      {value: 'hidden', label: 'Hidden', hidden: true},
      {value: 'disabled', label: 'Disabled', disabled: true},
      {value: 'first', label: 'First choice'},
      {value: 'middle', label: 'Middle choice'},
      {value: 'last', label: 'Last choice'},
    ],
  }})
  const input = wrapper.get<HTMLInputElement>('input')
  input.element.focus()
  await nextTick()
  const focusedOption = () => document.getElementById(input.attributes('aria-activedescendant') ?? '')?.textContent
  const press = async (key: string, extras: KeyboardEventInit = {}) => {
    const event = new KeyboardEvent('keydown', {key, bubbles: true, cancelable: true, ...extras})
    input.element.dispatchEvent(event)
    await nextTick()
    return event
  }
  expect(document.body.querySelectorAll('[role="option"]')).toHaveLength(4)
  expect(focusedOption()).toBe('First choice')
  await press('End')
  expect(focusedOption()).toBe('Last choice')
  await press('ArrowDown')
  expect(focusedOption()).toBe('First choice')
  await press('Home')
  await press('ArrowUp')
  expect(focusedOption()).toBe('Last choice')
  expect((await press(' ')).defaultPrevented).toBe(false)
  expect((await press('Enter', {isComposing: true})).defaultPrevented).toBe(false)
  document.body.querySelector<HTMLElement>('[aria-disabled="true"]')!.click()
  expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  await press('Enter')
  expect(wrapper.emitted('update:modelValue')).toEqual([['last']])
  expect(document.activeElement).toBe(input.element)
  expect(input.attributes('aria-expanded')).toBe('false')
  await press('Enter')
  expect(input.attributes('aria-expanded')).toBe('true')
  expect((await press('Tab')).defaultPrevented).toBe(false)
  expect(input.attributes('aria-expanded')).toBe('false')
  wrapper.unmount()
})
