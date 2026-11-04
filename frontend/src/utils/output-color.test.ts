import {describe, expect, it} from 'vitest'
import {Colorizer} from './output-color'

const bytes = (value: string) => new TextEncoder().encode(value)
const text = (value: Uint8Array) => new TextDecoder().decode(value)

describe('Colorizer', () => {
  it('immediately forwards an ANSI zsh prompt without a trailing newline', () => {
    const colorizer = new Colorizer('auto')
    const prompt = '\r\x1b]7;file://host/Users/test\x1b\\\x1b[01;32m➜  \x1b[36m~\x1b[00m '

    expect(text(colorizer.feed(bytes(prompt)))).toBe(prompt)
    expect(colorizer.flush()).toHaveLength(0)
  })

  it('does not delay a prompt until the next carriage return', () => {
    const colorizer = new Colorizer('auto')
    const firstPrompt = '\x1b[32m➜  \x1b[36m~\x1b[0m '
    const nextPrompt = '\r\n\x1b[32m➜  \x1b[36m~\x1b[0m '

    expect(text(colorizer.feed(bytes(firstPrompt)))).toBe(firstPrompt)
    expect(text(colorizer.feed(bytes(nextPrompt)))).toBe(nextPrompt)
  })
})
