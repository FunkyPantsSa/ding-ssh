import {flushPromises, mount} from '@vue/test-utils'
import {afterEach, expect, it, vi} from 'vitest'
import {GetRuntimeStatus} from '../../../wailsjs/go/main/App'
import SettingsAboutSection from './SettingsAboutSection.vue'

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetRuntimeStatus: vi.fn().mockResolvedValue({uptimeSec: 1, goroutines: 2, heapBytes: 3}),
}))
vi.mock('../../services/ssh', () => ({
  sshService: {getPlatform: vi.fn().mockResolvedValue('darwin')},
}))

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

it('refreshes only when enabled and switches off when the settings view is hidden', async () => {
  vi.useFakeTimers()
  const wrapper = mount(SettingsAboutSection, {props: {active: true}})
  try {
    await flushPromises()
    const toggle = wrapper.get('button[role="switch"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await vi.advanceTimersByTimeAsync(5000)
    expect(GetRuntimeStatus).toHaveBeenCalledTimes(1)

    await toggle.trigger('click')
    await vi.advanceTimersByTimeAsync(5000)
    expect(GetRuntimeStatus).toHaveBeenCalledTimes(3)

    await wrapper.setProps({active: false})
    await wrapper.setProps({active: true})
    await vi.advanceTimersByTimeAsync(10000)
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(GetRuntimeStatus).toHaveBeenCalledTimes(3)
  } finally {
    wrapper.unmount()
  }
})
