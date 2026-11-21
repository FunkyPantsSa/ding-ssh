import {afterEach, expect, it, vi} from 'vitest'
import {getStartupSecurityStatus, securityService} from './security'

afterEach(() => vi.restoreAllMocks())

it('keeps startup locked and exposes the error when the security status request fails', async () => {
  vi.spyOn(securityService, 'getStatus').mockRejectedValue(new Error('bridge unavailable'))
  await expect(getStartupSecurityStatus()).resolves.toMatchObject({unlocked: false, needsUnlock: true, initializationError: expect.stringContaining('bridge unavailable')})
})

it('rejects a malformed security response instead of opening the workspace', async () => {
  vi.spyOn(securityService, 'getStatus').mockResolvedValue(null as any)
  await expect(getStartupSecurityStatus()).resolves.toMatchObject({unlocked: false, needsUnlock: true})
})

it('preserves successful and master-password-locked status responses', async () => {
  const unlocked = {unlocked: true, needsUnlock: false, keyringAvailable: true, masterPasswordEnabled: false}
  const locked = {...unlocked, unlocked: false, needsUnlock: true, masterPasswordEnabled: true}
  vi.spyOn(securityService, 'getStatus').mockResolvedValueOnce(unlocked).mockResolvedValueOnce(locked)
  await expect(getStartupSecurityStatus()).resolves.toEqual(unlocked)
  await expect(getStartupSecurityStatus()).resolves.toEqual(locked)
})
