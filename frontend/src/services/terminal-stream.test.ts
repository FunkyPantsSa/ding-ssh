import { describe, expect, it, vi, beforeEach } from 'vitest'
import { openTerminalStream } from './terminal-stream'

vi.mock('@wailsio/runtime', () => ({
  Stream: vi.fn(),
}))

vi.mock('./desktop-environment', () => ({
  isDesktop: vi.fn(),
}))

import { Stream } from '@wailsio/runtime'
import { isDesktop } from './desktop-environment'

describe('terminal-stream', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns null in non-desktop environments', () => {
    vi.mocked(isDesktop).mockReturnValue(false)
    const client = openTerminalStream('test-sid', () => {})
    expect(client).toBeNull()
  })

  it('connects to Stream and attaches session in desktop environment', () => {
    vi.mocked(isDesktop).mockReturnValue(true)

    const listeners: Record<string, ((ev?: any) => void)[]> = {}
    const mockSocket = {
      addEventListener: vi.fn((event: string, fn: (ev?: any) => void) => {
        listeners[event] = listeners[event] || []
        listeners[event].push(fn)
      }),
      send: vi.fn(),
      close: vi.fn(),
    }

    vi.mocked(Stream).mockReturnValue(mockSocket as any)

    const onOutput = vi.fn()
    const onStatusChange = vi.fn()
    const client = openTerminalStream('s123', onOutput, onStatusChange)

    expect(client).not.toBeNull()
    expect(Stream).toHaveBeenCalledWith('terminal')

    // Simulate open
    listeners['open']?.forEach(fn => fn())
    expect(onStatusChange).toHaveBeenCalledWith(true)
    expect(mockSocket.send).toHaveBeenCalledWith(JSON.stringify({ action: 'attach', sessionId: 's123' }))
    expect(client?.isOpen()).toBe(true)

    // Simulate binary message
    const rawData = new Uint8Array([104, 101, 108, 108, 111])
    listeners['message']?.forEach(fn => fn({ data: rawData.buffer }))
    expect(onOutput).toHaveBeenCalledWith(rawData)

    // Send input
    client?.send('ls\n')
    expect(mockSocket.send).toHaveBeenCalledWith('ls\n')

    // Send interactive
    client?.sendInteractive('\r', 'sudo apt update')
    expect(mockSocket.send).toHaveBeenCalledWith(JSON.stringify({
      action: 'input_interactive',
      sessionId: 's123',
      data: '\r',
      command: 'sudo apt update',
    }))

    // Send resize
    client?.sendResize(120, 40)
    expect(mockSocket.send).toHaveBeenCalledWith(JSON.stringify({
      action: 'resize',
      sessionId: 's123',
      cols: 120,
      rows: 40,
    }))

    // Close
    client?.close()
    expect(mockSocket.close).toHaveBeenCalled()
  })
})
