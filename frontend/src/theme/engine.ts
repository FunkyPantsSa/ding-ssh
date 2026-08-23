// 主题引擎：从 UI 外观配置派生整组 CSS 变量，供根节点绑定。
import type {Fonts, UIAppearance} from '../types'
import {defaultPreset, presetById} from './presets'

// ---- 颜色工具 ----

export interface Rgb {
  r: number
  g: number
  b: number
}

export function hexToRgb(hex: string): Rgb {
  let h = (hex || '').trim().replace('#', '')
  if (h.length === 3) {
    h = h
      .split('')
      .map((c) => c + c)
      .join('')
  }
  const n = parseInt(h, 16)
  if (Number.isNaN(n) || h.length !== 6) return {r: 0, g: 0, b: 0}
  return {r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255}
}

/** 与目标色按比例混合（ratio=0 返回原色，1 返回目标色）。 */
function mix(hex: string, target: string, ratio: number): string {
  const a = hexToRgb(hex)
  const b = hexToRgb(target)
  const r = Math.round(a.r + (b.r - a.r) * ratio)
  const g = Math.round(a.g + (b.g - a.g) * ratio)
  const bl = Math.round(a.b + (b.b - a.b) * ratio)
  return `#${((r << 16) | (g << 8) | bl).toString(16).padStart(6, '0')}`
}

export function lighten(hex: string, amount: number): string {
  return mix(hex, '#ffffff', Math.max(0, Math.min(1, amount)))
}

export function darken(hex: string, amount: number): string {
  return mix(hex, '#000000', Math.max(0, Math.min(1, amount)))
}

export function rgba(hex: string, alpha: number): string {
  const {r, g, b} = hexToRgb(hex)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

// ---- CSS 变量派生 ----

/** 从主/辅色派生品牌色整组变量（signal = 主色，copper = 辅色）。 */
export function deriveBrandVars(primary: string, secondary: string): Record<string, string> {
  return {
    '--signal-500': primary,
    '--signal-400': lighten(primary, 0.18),
    '--signal-300': lighten(primary, 0.32),
    '--signal-glow': rgba(primary, 0.32),
    '--signal-glow-soft': rgba(primary, 0.15),
    '--signal-weak': rgba(primary, 0.12),
    '--signal-border': rgba(primary, 0.25),
    '--signal-strong-border': rgba(primary, 0.45),
    '--signal-hover': rgba(primary, 0.4),
    '--copper-500': secondary,
    '--copper-400': lighten(secondary, 0.16),
    '--copper-300': lighten(secondary, 0.28),
    '--copper-glow': rgba(secondary, 0.28),
    '--copper-glow-soft': rgba(secondary, 0.09),
    '--copper-weak': rgba(secondary, 0.12),
    '--copper-border': rgba(secondary, 0.25),
    '--copper-hover': rgba(secondary, 0.4),
  }
}

/** 当前预设的 UI 主/辅色（custom 模式用用户自定义值）。 */
export function effectiveBrand(appearance: UIAppearance): {primary: string; secondary: string} {
  if (appearance.mode === 'preset') {
    const p = presetById(appearance.presetId) ?? defaultPreset()
    return {primary: p.primary, secondary: p.secondary}
  }
  return {
    primary: appearance.primary || defaultPreset().primary,
    secondary: appearance.secondary || defaultPreset().secondary,
  }
}

export type Tone = 'light' | 'dark'

/** 解析实际明暗模式（auto 跟随系统）。 */
export function resolveTone(baseTone: UIAppearance['baseTone']): Tone {
  if (baseTone !== 'auto') return baseTone
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'light'
  return 'dark'
}

// ---- 字体栈 ----

export const UI_FONT_FALLBACK = '"PingFang SC", "Helvetica Neue", "Microsoft YaHei", system-ui, sans-serif'
export const MONO_FONT_FALLBACK = 'ui-monospace, "SF Mono", Menlo, Monaco, Consolas, "Cascadia Mono", monospace'

export function uiFontStack(name: string | undefined): string {
  if (!name || name === 'system') return UI_FONT_FALLBACK
  return `"${name}", ${UI_FONT_FALLBACK}`
}

export function monoFontStack(name: string | undefined): string {
  if (!name || name === 'system') return MONO_FONT_FALLBACK
  return `"${name}", ${MONO_FONT_FALLBACK}`
}

/** 计算根节点应绑定的全部 CSS 变量。 */
export function computeUiCssVars(appearance: UIAppearance, fonts: Fonts, tone: Tone): Record<string, string> {
  const {primary, secondary} = effectiveBrand(appearance)
  const uiText = appearance.mode === 'custom' && appearance.uiText ? appearance.uiText : ''
  return {
    ...deriveBrandVars(primary, secondary),
    '--font-sans': uiFontStack(fonts.uiFont),
    '--font-mono': monoFontStack(fonts.terminalFont),
    // custom 模式：自定义界面主文字色（text-white / slate 等均走 --mist-100）
    ...(uiText ? {'--mist-100': uiText} : {}),
  }
}

export function isSystemToneQuery(tone: Tone): string {
  return tone === 'dark' ? '(prefers-color-scheme: dark)' : '(prefers-color-scheme: light)'
}

/** 由主题引擎写入 <html> 的动态 CSS 变量（卸载时需清除）。 */
const MANAGED_ROOT_VARS = [
  '--signal-500', '--signal-400', '--signal-300',
  '--signal-glow', '--signal-glow-soft', '--signal-weak', '--signal-border', '--signal-strong-border', '--signal-hover',
  '--copper-500', '--copper-400', '--copper-300',
  '--copper-glow', '--copper-glow-soft', '--copper-weak', '--copper-border', '--copper-hover',
  '--font-sans', '--font-mono', '--mist-100',
] as const

/** 将明暗模式与品牌色变量同步到 <html>，供 Teleport 到 body 的浮层继承。 */
export function applyRootTheme(tone: Tone, vars: Record<string, string>) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.setAttribute('data-tone', tone)
  for (const key of MANAGED_ROOT_VARS) {
    root.style.removeProperty(key)
  }
  for (const [key, value] of Object.entries(vars)) {
    if (value) root.style.setProperty(key, value)
  }
}

/** 卸载时清除 <html> 上的主题注入。 */
export function clearRootTheme() {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.removeAttribute('data-tone')
  for (const key of MANAGED_ROOT_VARS) {
    root.style.removeProperty(key)
  }
}
