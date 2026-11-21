import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {sshService} from '../services/ssh'
import {useSudoAssistant} from './useSudoAssistant'
import type {SudoPromptEvent} from '../types'

vi.mock('../services/ssh', () => ({sshService: {submitSudoPassword: vi.fn(), cancelSudoPrompt: vi.fn()}}))

function event(overrides: Partial<SudoPromptEvent> = {}): SudoPromptEvent {
  return {sequence: 1, requestId: 'challenge-one', state: 'prompt', user: 'operator', hasSavedPassword: true, retry: false, sensitive: true, expiresAt: Date.now() + 30_000, ...overrides}
}
function fixture() {
  let active = true
  const focusTerminal = vi.fn(), resetInput = vi.fn()
  return {...useSudoAssistant({sessionId: () => 'session-one', active: () => active, focusTerminal, resetInput}), focusTerminal, resetInput, deactivate: () => { active = false }}
}
beforeEach(() => {
  vi.useFakeTimers(); vi.clearAllMocks()
  vi.mocked(sshService.submitSudoPassword).mockResolvedValue(undefined)
  vi.mocked(sshService.cancelSudoPrompt).mockResolvedValue(undefined)
})
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

describe('sudo assistant', () => {
  it('submits once after one second without requesting a password from the backend', async () => {
    const assistant = fixture()
    assistant.handleEvent(event())
    expect(assistant.sensitive.value).toBe(true)
    expect(assistant.password.value).toBe('')
    await vi.advanceTimersByTimeAsync(999)
    expect(sshService.submitSudoPassword).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(sshService.submitSudoPassword).toHaveBeenCalledExactlyOnceWith('session-one', 'challenge-one', '', true)
    expect(assistant.prompt.value).toBeNull()
    expect(assistant.sensitive.value).toBe(false)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(sshService.submitSudoPassword).toHaveBeenCalledTimes(1)
  })

  it('cancels on dismissal, terminal typing, inactive tabs, expiry and teardown', async () => {
    for (const action of ['cancel', 'typing', 'inactive', 'expiry', 'reset']) {
      vi.mocked(sshService.submitSudoPassword).mockClear()
      const assistant = fixture()
      assistant.handleEvent(event({expiresAt: Date.now() + (action === 'expiry' ? 400 : 30_000)}))
      if (action === 'cancel') assistant.cancel()
      if (action === 'typing') assistant.terminalInput()
      if (action === 'inactive') assistant.deactivate()
      if (action === 'reset') assistant.reset()
      await vi.advanceTimersByTimeAsync(1100)
      expect(sshService.submitSudoPassword).not.toHaveBeenCalled()
      expect(assistant.prompt.value).toBeNull()
      assistant.reset()
    }
  })

  it('keeps manual terminal input out of history mode after cancelling the popup', () => {
    const assistant = fixture()
    assistant.handleEvent(event())
    assistant.cancel()
    expect(assistant.sensitive.value).toBe(true)
    assistant.handleEvent(event({sequence: 2, state: 'close', sensitive: false, requestId: ''}))
    expect(assistant.sensitive.value).toBe(false)
  })

  it('requires manual input on retries, missing passwords and private-key login', async () => {
    for (const overrides of [{retry: true}, {hasSavedPassword: false}]) {
      const assistant = fixture()
      assistant.handleEvent(event(overrides))
      await vi.advanceTimersByTimeAsync(1100)
      expect(sshService.submitSudoPassword).not.toHaveBeenCalled()
      assistant.password.value = 'manual-secret'
      await assistant.submit()
      expect(sshService.submitSudoPassword).toHaveBeenLastCalledWith('session-one', 'challenge-one', 'manual-secret', false)
      expect(assistant.password.value).toBe('')
      vi.mocked(sshService.submitSudoPassword).mockClear()
    }
  })

  it('switches to manual entry and stops the countdown', async () => {
    const assistant = fixture()
    assistant.handleEvent(event())
    assistant.manual('x')
    await vi.advanceTimersByTimeAsync(1100)
    expect(sshService.submitSudoPassword).not.toHaveBeenCalled()
    expect(assistant.password.value).toBe('x')
    await assistant.submit()
    expect(sshService.submitSudoPassword).toHaveBeenCalledExactlyOnceWith('session-one', 'challenge-one', 'x', false)
  })

  it('preserves a newer retry prompt when the earlier submission resolves', async () => {
    let resolve!: () => void
    vi.mocked(sshService.submitSudoPassword).mockReturnValue(new Promise<void>(done => { resolve = done }))
    const assistant = fixture()
    assistant.handleEvent(event())
    const pending = assistant.submit()
    assistant.handleEvent(event({sequence: 2, requestId: 'retry', retry: true, hasSavedPassword: false}))
    resolve(); await pending
    expect(assistant.prompt.value?.requestId).toBe('retry')
    expect(assistant.automatic.value).toBe(false)
    expect(assistant.sensitive.value).toBe(true)
  })

  it('ignores delayed duplicate/out-of-order backend events', () => {
    const assistant = fixture()
    assistant.handleEvent(event({sequence: 3, requestId: 'latest'}))
    assistant.handleEvent(event({sequence: 2, state: 'close', sensitive: false}))
    assistant.handleEvent(event({sequence: 3, requestId: 'duplicate'}))
    expect(assistant.prompt.value?.requestId).toBe('latest')
    expect(assistant.sensitive.value).toBe(true)
  })

  it('offers manual entry after credential lookup fails without retrying automatically', async () => {
    vi.mocked(sshService.submitSudoPassword).mockRejectedValue(new Error('No saved password'))
    const assistant = fixture()
    assistant.handleEvent(event())
    await vi.advanceTimersByTimeAsync(1000)
    expect(assistant.error.value).toContain('No saved password')
    expect(assistant.automatic.value).toBe(false)
    await vi.advanceTimersByTimeAsync(2000)
    expect(sshService.submitSudoPassword).toHaveBeenCalledTimes(1)
  })
})
