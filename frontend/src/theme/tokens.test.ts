import {readFileSync, readdirSync, statSync} from 'node:fs'
import {join} from 'node:path'
import {fileURLToPath} from 'node:url'
import {describe, expect, it} from 'vitest'

const read = (name: string) => readFileSync(fileURLToPath(new URL(name, import.meta.url)), 'utf8')

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    return statSync(path).isDirectory() ? walk(path) : [path]
  })
}

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

  it('keeps Solid surfaces opaque by forbidding translucent utility backgrounds', () => {
    const src = join(process.cwd(), 'src')
    for (const file of walk(src).filter((path) => path.endsWith('.vue'))) {
      const text = readFileSync(file, 'utf8')
      expect(text, file).not.toMatch(/bg-(?:slate|zinc|neutral|white|black)-\d+\/\d+/)
    }
    const style = readFileSync(join(src, 'style.css'), 'utf8')
    expect(style).not.toMatch(/background:\s*color-mix\([^)]*transparent\)/)
  })

  it('exposes the dynamic semantic token set and maps components to it', () => {
    const semantic = read('./semantic.css')
    const components = read('./components.css')
    for (const token of [
      '--app-bg', '--sidebar-primary-bg', '--sidebar-secondary-bg', '--workspace-bg',
      '--surface-bg', '--surface-raised-bg', '--surface-overlay-bg',
      '--interactive-hover-bg', '--interactive-active-bg', '--interactive-pressed-bg',
      '--focus-ring-color', '--text-primary', '--text-secondary', '--text-muted',
      '--text-on-accent', '--border-subtle', '--border-default', '--border-strong',
      '--accent-bg', '--accent-hover-bg', '--accent-foreground',
      '--status-success', '--status-warning', '--status-error', '--status-info',
      '--terminal-bg',
    ]) {
      expect(semantic, token).toContain(`${token}:`)
    }
    expect(semantic).not.toMatch(/--border-subtle:\s*var\(--border-subtle\)/)
    expect(components).toContain('--nav-bg: var(--sidebar-primary-bg)')
    expect(components).toContain('--secondary-nav-bg: var(--sidebar-secondary-bg)')
    expect(components).toContain('--card-bg: var(--surface-bg)')
  })

  it('paints ambient gradients from engine tokens with base-colour fallbacks', () => {
    const style = read('../style.css')
    expect(style).toContain('var(--sidebar-primary-gradient, var(--sidebar-primary-bg')
    expect(style).toContain('var(--workspace-gradient,var(--workspace-bg))')
    // Only the two shell regions may receive an ambient gradient; cards and
    // controls keep their solid semantic token.
    expect(style.match(/linear-gradient\(\s*135deg/g)?.length ?? 0).toBe(0)
  })
})
