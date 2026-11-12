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

describe('Colorizer user input echo', () => {
  it('flushes the default macOS zsh prompt immediately', () => {
    const colorizer = new Colorizer('auto')
    const prompt = 'maqingjie@maqingjies-MacBook-Pro ~ % '

    expect(text(colorizer.feed(bytes(prompt)))).toBe(prompt)
    expect(colorizer.flush()).toHaveLength(0)
  })

  it('flushes a pending prompt when the user starts typing', () => {
    const colorizer = new Colorizer('auto')
    // 不是内置能识别的提示符形状：先挂在 pending 里
    expect(text(colorizer.feed(bytes('❯ ')))).toBe('')

    expect(text(colorizer.beginInput())).toBe('❯ ')
    expect(text(colorizer.feed(bytes('l')))).toBe('l')
    expect(text(colorizer.feed(bytes('s')))).toBe('s')
  })

  it('streams every echoed character immediately instead of buffering until newline', () => {
    const colorizer = new Colorizer('auto')
    expect(text(colorizer.feed(bytes('user@host ~ % ')))).toBe('user@host ~ % ')

    colorizer.beginInput()
    for (const char of 'ping example.com') {
      expect(text(colorizer.feed(bytes(char)))).toBe(char)
    }
    expect(text(colorizer.feed(bytes('\r\n')))).toBe('\r\n')
  })

  it('resumes semantic coloring after the input line ends', () => {
    const colorizer = new Colorizer('auto')
    colorizer.beginInput()
    expect(text(colorizer.feed(bytes('ls')))).toBe('ls')
    expect(text(colorizer.feed(bytes('\r\n')))).toBe('\r\n')

    const output = text(colorizer.feed(bytes('error: something failed\n')))
    expect(output).toContain('\x1b[31m')
  })
})
