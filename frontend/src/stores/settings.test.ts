import {createPinia, setActivePinia} from 'pinia'
import {beforeEach, describe, expect, it, vi} from 'vitest'
import {useSettingsStore} from './settings'

vi.mock('../services/settings', () => ({
  settingsService: {
    getSettings: vi.fn(),
    saveSettings: vi.fn(),
  },
}))

const {settingsService} = await import('../services/settings')

beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
})

describe('settings defaults', () => {
  it('keeps semantic terminal colors enabled by default (auto)', async () => {
    vi.mocked(settingsService.getSettings).mockResolvedValue({uiSchema: 2} as never)
    const store = useSettingsStore()
    await store.load()
    expect(store.terminalColorMode).toBe('auto')
  })

  it('preserves an explicit off choice', async () => {
    vi.mocked(settingsService.getSettings).mockResolvedValue({uiSchema: 2, terminalColorMode: 'off'} as never)
    const store = useSettingsStore()
    await store.load()
    expect(store.terminalColorMode).toBe('off')
  })

  it('normalizes dynamic theme fields on load', async () => {
    vi.mocked(settingsService.getSettings).mockResolvedValue({
      uiSchema: 4,
      appearance: {
        mode: 'custom', presetId: 'sky', baseTone: 'dark', primary: '#A78BFA', secondary: '#A78BFA',
        uiText: '#fff', atmosphere: 120, surfaceSeparation: -5, materialMode: 'soft',
        glassEnabled: true, glassOpacity: 50, glassBlur: 90,
      },
    } as never)
    const store = useSettingsStore()
    await store.load()
    expect(store.appearance).toMatchObject({
      atmosphere: 100,
      colorIntensity: 100,
      surfaceSeparation: 0,
      materialMode: 'soft',
      glassEnabled: true,
      glassOpacity: 60,
      glassBlur: 60,
    })
  })

  it('migrates legacy glass into the soft material mode', async () => {
    vi.mocked(settingsService.getSettings).mockResolvedValue({
      uiSchema: 3,
      appearance: {
        mode: 'preset', presetId: 'sky', baseTone: 'dark', primary: '#71B5D2', secondary: '#71B5D2',
        uiText: '#fff', colorIntensity: 60, glassEnabled: true, glassOpacity: 70, glassBlur: 20,
      },
    } as never)
    const store = useSettingsStore()
    await store.load()
    expect(store.appearance.materialMode).toBe('soft')
    expect(store.appearance.atmosphere).toBe(60)
  })
})

it('persists a changed preference and reloads the saved choice', async () => {
 vi.mocked(settingsService.saveSettings).mockResolvedValue(undefined)
 const store=useSettingsStore()
 await store.setCopyOnSelect(true)
 expect(settingsService.saveSettings).toHaveBeenCalledWith(expect.objectContaining({copyOnSelect:true}))
 const saved=vi.mocked(settingsService.saveSettings).mock.calls[0][0]
 vi.mocked(settingsService.getSettings).mockResolvedValue(saved)
 setActivePinia(createPinia())
 const restored=useSettingsStore();await restored.load()
 expect(restored.copyOnSelect).toBe(true)
})
it('propagates save failures so the caller can report them and permits retry', async () => {
 const store=useSettingsStore()
 vi.mocked(settingsService.saveSettings).mockRejectedValueOnce(new Error('disk full'))
 await expect(store.setCopyOnSelect(true)).rejects.toThrow('disk full')
 vi.mocked(settingsService.saveSettings).mockResolvedValue(undefined)
 await expect(store.setCopyOnSelect(true)).resolves.toBeUndefined()
 expect(settingsService.saveSettings).toHaveBeenCalledTimes(2)
})
