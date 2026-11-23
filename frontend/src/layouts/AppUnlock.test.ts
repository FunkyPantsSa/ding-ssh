import {mount} from '@vue/test-utils'
import {expect, it} from 'vitest'
import AppUnlock from './AppUnlock.vue'

it('shows an initialization error without offering an ineffective password form', () => {
  const wrapper = mount(AppUnlock, {props: {modelValue: '', unlocking: false, error: '', initializationError: 'vault unavailable'}})
  expect(wrapper.get('[role="alert"]').text()).toContain('vault unavailable')
  expect(wrapper.find('form').exists()).toBe(false)
  expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  wrapper.unmount()
})

it('retains password unlocking for a normally locked vault', () => {
  const wrapper = mount(AppUnlock, {props: {modelValue: '', unlocking: false, error: ''}})
  expect(wrapper.find('form').exists()).toBe(true)
  expect(wrapper.find('input[type="password"]').exists()).toBe(true)
  wrapper.unmount()
})
