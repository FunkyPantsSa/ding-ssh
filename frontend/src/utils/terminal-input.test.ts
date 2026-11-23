import {expect, it} from 'vitest'
import {encodeTerminalInput} from './terminal-input'

it.each(['plain ASCII\r', 'éü£±', '测试', '🙂', 'a\x00\x03\t'])('encodes terminal text as UTF-8: %s', text => {
  const bytes = Uint8Array.from(atob(encodeTerminalInput(text)), c => c.charCodeAt(0))
  expect(new TextDecoder('utf-8', {fatal: true}).decode(bytes)).toBe(text)
})

it('preserves every byte in raw debug/base64 input', () => {
  const raw = String.fromCharCode(...Array.from({length: 256}, (_, i) => i))
  expect(atob(encodeTerminalInput(raw, true))).toBe(raw)
})

it('supports large Unicode pastes without exceeding argument limits', () => {
  const text = 'é测试🙂'.repeat(50000)
  const bytes = Uint8Array.from(atob(encodeTerminalInput(text)), c => c.charCodeAt(0))
  expect(new TextDecoder().decode(bytes)).toBe(text)
})
