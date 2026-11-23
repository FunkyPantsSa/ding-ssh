import {afterEach, expect, it, vi} from 'vitest'

vi.mock('../../bindings/ding-ssh/internal/apphost/app', () => ({GetUpdateChannel: vi.fn(async () => 'dev'), CheckUpdate: vi.fn(), DownloadUpdate: vi.fn(), InstallUpdateNow: vi.fn()}))
vi.mock('./desktop', () => ({EventsOn: vi.fn(() => () => {})}))

afterEach(() => { vi.restoreAllMocks(); vi.resetModules() })

it('keeps startup and settings usable when local storage is unavailable', async () => {
  vi.resetModules()
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('blocked', 'SecurityError') })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('full', 'QuotaExceededError') })
  const service = await import('./updates')
  expect(service.updateChannel.value).toBe('main')
  expect(() => service.setUpdateChannel('dev')).not.toThrow()
  expect(service.updateChannel.value).toBe('dev')
  expect(() => service.setStartupCheck(false)).not.toThrow()
  expect(service.checkUpdatesOnStartup.value).toBe(false)
  await expect(service.startUpdateChecks()).resolves.toBeTypeOf('function')
})
