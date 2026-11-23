import {enableAutoUnmount, flushPromises, mount} from '@vue/test-utils'
import {afterEach, describe, expect, it} from 'vitest'
import TerminalSudoPopover from './TerminalSudoPopover.vue'
import {language} from '../../i18n'

enableAutoUnmount(afterEach)
function popover(automatic = true) {
  return mount(TerminalSudoPopover, {attachTo: document.body, props: {
    prompt: {sequence: 1, requestId: 'prompt', state: 'prompt', user: 'operator', hasSavedPassword: true, retry: !automatic, sensitive: true, expiresAt: Date.now() + 30_000},
    anchor: {left: 240, top: 60, height: 20}, bounds: {width: 900, height: 600}, serverName: 'Test server', automatic, submitting: false, error: '', password: '',
  }})
}
describe('terminal sudo popover', () => {
  it('renders a cursor-anchored non-modal dialog with no saved secret in the DOM', async () => {
    const wrapper = popover()
    expect(wrapper.get('[role="dialog"]').attributes('style')).toContain('left: 240px')
    expect(wrapper.find('[aria-modal]').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.text()).toContain('1 秒后自动提交')
    await wrapper.trigger('keydown', {key: 'Escape'})
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    await wrapper.trigger('keydown', {key: 'Enter'})
    expect(wrapper.emitted('submit')).toHaveLength(1)
  })
  it('uses a focused password field for retries and ignores IME Enter', async () => {
    const wrapper = popover(false)
    await flushPromises()
    const input = wrapper.get('input')
    expect(input.attributes('type')).toBe('password')
    expect(document.activeElement).toBe(input.element)
    expect(wrapper.text()).toContain('密码未通过验证')
    await input.trigger('keydown', {key: 'Enter', isComposing: true})
    expect(wrapper.emitted('submit')).toBeUndefined()
    await input.trigger('keydown', {key: 'c', ctrlKey: true})
    expect(wrapper.emitted('interrupt')).toHaveLength(1)
  })
  it('switches to manual entry when typing during the countdown', async () => {
    const wrapper = popover()
    await wrapper.trigger('keydown', {key: 'x'})
    expect(wrapper.emitted('manual')).toEqual([['x']])
  })
  it('does not turn Enter on the Cancel button into a password submission', async () => {
    const wrapper = popover()
    await wrapper.findAll('button').find(button => button.text() === '取消')!.trigger('keydown', {key: 'Enter'})
    expect(wrapper.emitted('submit')).toBeUndefined()
  })
  it('localizes the prompt in English', () => {
    language.value = 'en'
    try { expect(popover().text()).toContain('sudo authentication') } finally { language.value = 'zh-CN' }
  })
})
