import {describe, expect, it, vi} from 'vitest'

const listeners = vi.hoisted(() => new Set<(...args: any[]) => void>())
vi.mock('../../wailsjs/go/main/App', () => ({
  SetSysInfoIdle: vi.fn(), StartSysInfoCollector: vi.fn(), StopSysInfoCollector: vi.fn(),
}))
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: (_event: string, handler: (...args: any[]) => void) => {
    listeners.add(handler)
    return () => listeners.delete(handler)
  },
}))
import {onSysInfoSnapshot} from './sysinfo'

describe('shared sysinfo subscriptions', () => {
  it('keeps the status bar subscribed after the panel closes, including recovery', () => {
    const status = vi.fn()
    const panel = vi.fn()
    const offStatus = onSysInfoSnapshot('session', status)
    const offPanel = onSysInfoSnapshot('session', panel)
    for (const handler of listeners) handler({error: '远程无输出'})
    offPanel()
    for (const handler of listeners) handler({memTotalMb: 1024})
    expect(status).toHaveBeenCalledTimes(2)
    expect(status).toHaveBeenLastCalledWith({memTotalMb: 1024})
    expect(panel).toHaveBeenCalledTimes(1)
    offStatus()
    expect(listeners.size).toBe(0)
  })
})
