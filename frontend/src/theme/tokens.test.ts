import {readFileSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import {describe, expect, it} from 'vitest'

const read = (name: string) => readFileSync(fileURLToPath(new URL(name, import.meta.url)), 'utf8')

describe('design token single source', () => {
  it('defines radius and font primitives only in tokens.css', () => {
    const tokens = read('./tokens.css')
    const style = read('../style.css')
    const semantic = read('./semantic.css')
    const components = read('./components.css')

    expect(tokens).toContain('--radius-window: 12px')
    expect(tokens).toContain('--radius-layout: 0px')
    expect(tokens).toContain('--radius-card: 16px')
    expect(tokens).toContain('--radius-nav-active: 10px')
    expect(tokens).toContain('--radius-control: 8px')
    expect(tokens).toContain('--radius-small: 4px')
    expect(tokens).toContain('--radius-pill: 9999px')
    expect(tokens).toContain('--font-sans: "Geist Sans"')
    expect(tokens).toContain('--font-mono: "Geist Mono"')
    expect(tokens).not.toMatch(/Inter|Sora|JetBrains/)
    expect(style).not.toContain('--radius-lg:')
    expect(style).not.toContain('--font-sans:')
    expect(style).not.toContain('--background:')
    expect(semantic).not.toContain('--radius-lg:')
    expect(components).not.toContain('--radius-lg:')
  })
})
