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
})
