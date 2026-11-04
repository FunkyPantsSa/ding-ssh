import {flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import SettingsGeneralSection from './SettingsGeneralSection.vue'
import {useSettingsStore} from '../../stores/settings'

vi.mock('../../services/settings', () => ({
  settingsService: {
    getSettings: vi.fn(),
    saveSettings: vi.fn(),
  },
}))
vi.mock('../../services/ssh', () => ({
  sshService: {
    getPlatform: vi.fn(),
    getLocalShellOptions: vi.fn(),
  },
}))

const {settingsService} = await import('../../services/settings')
const {sshService} = await import('../../services/ssh')

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsService.saveSettings).mockResolvedValue(undefined)
  vi.mocked(sshService.getPlatform).mockResolvedValue('darwin')
  vi.mocked(sshService.getLocalShellOptions).mockResolvedValue([])
})

describe('settings save flow', () => {
  it('persists a toggle change through the settings store', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const settings = useSettingsStore()
    settings.loaded = true
    settings.webGLEnabled = true

    const wrapper = mount(SettingsGeneralSection, {global: {plugins: [pinia]}})
    await flushPromises()

    const webglCard = wrapper.findAll('.neo').find((card) => card.text().includes('WebGL 硬件加速'))
    expect(webglCard).toBeTruthy()
    await webglCard!.get('[role="switch"]').trigger('click')
    await flushPromises()

    expect(settings.webGLEnabled).toBe(false)
    expect(settingsService.saveSettings).toHaveBeenCalled()
    const saved = vi.mocked(settingsService.saveSettings).mock.calls.at(-1)?.[0] as {webGLEnabled?: boolean}
    expect(saved.webGLEnabled).toBe(false)
    wrapper.unmount()
  })
})
