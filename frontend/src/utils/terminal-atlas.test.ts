import {afterEach, describe, expect, it, vi} from 'vitest'
import {
  registerSharedAtlasTerminal,
  resetSharedAtlasTerminals,
  scheduleSharedAtlasReset,
  unregisterSharedAtlasTerminal,
} from './terminal-atlas'

function fakeTerminal() {
  return {invalidateModel: vi.fn(), refresh: vi.fn()}
}

const registered: Array<ReturnType<typeof fakeTerminal>> = []

function register(term: ReturnType<typeof fakeTerminal>) {
  registered.push(term)
  registerSharedAtlasTerminal(term)
  return term
}

afterEach(() => {
  for (const term of registered.splice(0)) unregisterSharedAtlasTerminal(term)
  vi.useRealTimers()
})

describe('shared terminal atlas reset', () => {
  it('invalidates the vertex model and redraws every registered terminal', () => {
    const a = register(fakeTerminal())
    const b = register(fakeTerminal())

    resetSharedAtlasTerminals()

    expect(a.invalidateModel).toHaveBeenCalledTimes(1)
    expect(b.invalidateModel).toHaveBeenCalledTimes(1)
    expect(a.refresh).toHaveBeenCalledTimes(1)
    expect(b.refresh).toHaveBeenCalledTimes(1)
  })

  it('coalesces multiple atlas changes into one scheduled reset', () => {
    vi.useFakeTimers()
    const a = register(fakeTerminal())

    scheduleSharedAtlasReset()
    scheduleSharedAtlasReset()
    expect(a.invalidateModel).not.toHaveBeenCalled()

    vi.runAllTimers()
    expect(a.invalidateModel).toHaveBeenCalledTimes(1)
    expect(a.refresh).toHaveBeenCalledTimes(1)

    scheduleSharedAtlasReset()
    vi.runAllTimers()
    expect(a.invalidateModel).toHaveBeenCalledTimes(2)
  })

  it('stops touching terminals after they are unregistered', () => {
    const term = register(fakeTerminal())
    unregisterSharedAtlasTerminal(term)

    resetSharedAtlasTerminals()

    expect(term.invalidateModel).not.toHaveBeenCalled()
    expect(term.refresh).not.toHaveBeenCalled()
  })
})
