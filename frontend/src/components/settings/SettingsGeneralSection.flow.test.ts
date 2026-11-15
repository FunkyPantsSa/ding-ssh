import {flushPromises, mount} from '@vue/test-utils'
import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {language} from '../../i18n'
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
  it('uses the shared select style and renders the English settings page', async () => {
    language.value = 'en'
    const pinia = createPinia()
    setActivePinia(pinia)
    useSettingsStore().loaded = true
    const wrapper = mount(SettingsGeneralSection, {global: {plugins: [pinia]}})
    try {
      await flushPromises()
      expect(wrapper.get('#app-language').classes()).toContain('ui-select')
      expect(wrapper.get('#app-language').element.parentElement?.classList.contains('w-44')).toBe(true)
      expect(wrapper.get('#app-language').element.tagName).toBe('SELECT')
      expect(wrapper.get('h3').text()).toBe('General')
      expect(wrapper.text()).toContain('Copy selection automatically')
      expect(wrapper.text()).toContain('SSH keepalive')
    } finally {
      wrapper.unmount()
      language.value = 'zh-CN'
    }
  })
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
