import {describe, expect, it} from 'vitest'
import {computeUiCssVars, deriveBrandVars, monoFontStack, resolveTone, uiFontStack} from './engine'

describe('theme engine', () => {
  it('derives semantic brand variables without legacy aliases', () => {
    const vars = deriveBrandVars('#3b82f6', '#8b5cf6')

    expect(vars).toMatchObject({
      '--primary': '#3b82f6',
      '--secondary': '#8b5cf6',
      '--primary-foreground': '#ffffff',
    })
    expect(Object.keys(vars).every((key) => !key.includes('ink') && !key.includes('mist'))).toBe(true)
  })

  it('uses deterministic font stacks and custom foreground values', () => {
    expect(uiFontStack('system')).toContain('PingFang SC')
    expect(monoFontStack('JetBrains Mono')).toContain('JetBrains Mono')
    const vars = computeUiCssVars(
      {mode: 'custom', baseTone: 'dark', presetId: 'default', primary: '#000000', secondary: '#ffffff', uiText: '#eeeeee'},
      {uiFont: 'system', terminalFont: 'system', terminalFontSize: 14},
      'dark',
    )
    expect(vars['--foreground']).toBe('#eeeeee')
  })

  it('resolves explicit tone without consulting system preferences', () => {
    expect(resolveTone('light')).toBe('light')
    expect(resolveTone('dark')).toBe('dark')
  })
})
