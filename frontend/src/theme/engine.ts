// 主题引擎：从 UI 外观配置派生整组 CSS 变量，供根节点绑定。
import type {Fonts, Theme, UIAppearance} from '../types'
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

export function readableOn(hex: string): string {
  const {r, g, b} = hexToRgb(hex)
  return (r * 299 + g * 587 + b * 114) / 1000 > 160 ? '#09090b' : '#ffffff'
}

// ---- CSS 变量派生 ----

/** 从主/辅色派生语义化品牌色变量。 */
export function deriveBrandVars(primary: string, secondary: string): Record<string, string> {
  return {
    '--primary': primary,
    '--primary-hover': lighten(primary, 0.18),
    '--primary-glow': rgba(primary, 0.32),
    '--primary-glow-soft': rgba(primary, 0.15),
    '--primary-weak': rgba(primary, 0.12),
    '--primary-border': rgba(primary, 0.25),
    '--primary-strong-border': rgba(primary, 0.45),
    '--primary-hover-bg': rgba(primary, 0.4),
    '--primary-foreground': readableOn(primary),
    '--secondary': secondary,
    '--secondary-hover': lighten(secondary, 0.16),
    '--secondary-glow': rgba(secondary, 0.28),
    '--secondary-glow-soft': rgba(secondary, 0.09),
    '--secondary-weak': rgba(secondary, 0.12),
    '--secondary-border': rgba(secondary, 0.25),
    '--secondary-hover-bg': rgba(secondary, 0.4),
  }
}

/**
 * Seed color -> application surfaces. Large backgrounds keep only a restrained
 * amount of chroma; navigation is intentionally lighter than the workspace in
 * dark mode and darker than it in light mode, matching the Arc-like hierarchy.
 */
export function deriveSurfaceVars(seed: string, tone: Tone, glassOpacity = 85, glassBlur = 24): Record<string, string> {
  const opacity = `${Math.min(100, Math.max(40, glassOpacity || 85))}%`
  const blur = `${Math.min(60, Math.max(0, glassBlur))}px`
  if (tone === 'light') {
    const main = mix(seed, '#ffffff', 0.91)
    const sidebar = mix(seed, '#ffffff', 0.82)
    const toolbar = mix(seed, '#ffffff', 0.88)
    return {
      '--background': main,
      '--surface-1': sidebar,
      '--surface-2': mix(seed, '#ffffff', 0.95),
      '--surface-3': mix(seed, '#ffffff', 0.87),
      '--surface-4': mix(seed, '#ffffff', 0.78),
      '--border': mix(seed, '#d4d4d8', 0.72),
      '--border-strong': mix(seed, '#a1a1aa', 0.78),
      '--border-subtle': rgba(darken(seed, 0.4), 0.12),
      '--theme-bg-main': main,
      '--theme-bg-sidebar': sidebar,
      '--theme-bg-toolbar': toolbar,
      '--theme-bg-surface': mix(seed, '#ffffff', 0.95),
      '--theme-glass-opacity': opacity,
      '--theme-glass-blur': blur,
      '--theme-glass-tint': rgba(seed, 0.14),
    }
  }

  const main = mix(seed, '#0d1117', 0.9)
  const sidebar = mix(seed, '#18202b', 0.79)
  const toolbar = mix(seed, '#121821', 0.86)
  return {
    '--background': main,
    '--surface-1': sidebar,
    '--surface-2': mix(seed, '#1b232e', 0.86),
    '--surface-3': mix(seed, '#25303d', 0.82),
    '--surface-4': mix(seed, '#344152', 0.8),
    '--border': mix(seed, '#334155', 0.84),
    '--border-strong': mix(seed, '#475569', 0.8),
    '--border-subtle': rgba(lighten(seed, 0.55), 0.12),
    '--theme-bg-main': main,
    '--theme-bg-sidebar': sidebar,
    '--theme-bg-toolbar': toolbar,
    '--theme-bg-surface': mix(seed, '#1b232e', 0.86),
    '--theme-glass-opacity': opacity,
    '--theme-glass-blur': blur,
    '--theme-glass-tint': rgba(seed, 0.12),
  }
}

/** Keep terminal contrast independent while tinting its key colors from the UI seed. */
export function deriveTerminalTheme(base: Theme, seed: string, tone: Tone): Theme {
  const light = tone === 'light'
  const cursor = light ? darken(seed, 0.12) : lighten(seed, 0.12)
  return {
    ...base,
    background: light ? lighten(seed, 0.93) : darken(seed, 0.88),
    foreground: light ? '#20242a' : '#e8edf2',
    cursor,
    selection: rgba(cursor, light ? 0.22 : 0.3),
    black: light ? '#20242a' : darken(seed, 0.94),
    brightBlack: light ? '#6b7280' : lighten(darken(seed, 0.72), 0.28),
    blue: light ? darken(seed, 0.28) : seed,
    brightBlue: light ? darken(seed, 0.12) : lighten(seed, 0.22),
    white: light ? '#e5e7eb' : '#d9e0e7',
    brightWhite: '#ffffff',
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

const SYSTEM_UI_FONT_STACK = '"PingFang SC", "Helvetica Neue", "Microsoft YaHei", system-ui, sans-serif'
const SYSTEM_MONO_FONT_STACK = 'ui-monospace, "SF Mono", Menlo, Monaco, Consolas, "Cascadia Mono", monospace'

// 默认与静态兜底都指向已捆绑的 Geist；只有用户显式选择 system 才回落到系统字体。
export const UI_FONT_FALLBACK = `"Geist Sans", ${SYSTEM_UI_FONT_STACK}`
export const MONO_FONT_FALLBACK = `"Geist Mono", ${SYSTEM_MONO_FONT_STACK}`

export function uiFontStack(name: string | undefined): string {
  if (!name) return UI_FONT_FALLBACK
  if (name === 'system') return SYSTEM_UI_FONT_STACK
  return `"${name}", ${SYSTEM_UI_FONT_STACK}`
}

export function monoFontStack(name: string | undefined): string {
  if (!name) return MONO_FONT_FALLBACK
  if (name === 'system') return SYSTEM_MONO_FONT_STACK
  return `"${name}", ${SYSTEM_MONO_FONT_STACK}`
}

/** 计算根节点应绑定的全部 CSS 变量。 */
export function computeUiCssVars(appearance: UIAppearance, fonts: Fonts, tone: Tone): Record<string, string> {
  const {primary, secondary} = effectiveBrand(appearance)
  return {
    ...deriveSurfaceVars(primary, tone, appearance.glassOpacity, appearance.glassBlur),
    ...deriveBrandVars(primary, secondary),
    '--font-sans': uiFontStack(fonts.uiFont),
    '--font-mono': monoFontStack(fonts.terminalFont),
    '--terminal-bg': appearance.mode === 'preset' ? (tone === 'dark' ? presetById(appearance.presetId)?.dark.background ?? defaultPreset().dark.background : presetById(appearance.presetId)?.light.background ?? defaultPreset().light.background) : '',
  }
}

export function isSystemToneQuery(tone: Tone): string {
  return tone === 'dark' ? '(prefers-color-scheme: dark)' : '(prefers-color-scheme: light)'
}

/** 由主题引擎写入 <html> 的动态 CSS 变量（卸载时需清除）。 */
const MANAGED_ROOT_VARS = [
  '--background', '--surface-1', '--surface-2', '--surface-3', '--surface-4',
  '--border', '--border-strong', '--border-subtle',
  '--theme-bg-main', '--theme-bg-sidebar', '--theme-bg-toolbar', '--theme-bg-surface',
  '--theme-glass-opacity', '--theme-glass-blur', '--theme-glass-tint',
  '--primary', '--primary-hover',
  '--primary-glow', '--primary-glow-soft', '--primary-weak', '--primary-border', '--primary-strong-border', '--primary-hover-bg',
  '--primary-foreground', '--terminal-bg',
  '--secondary', '--secondary-hover',
  '--secondary-glow', '--secondary-glow-soft', '--secondary-weak', '--secondary-border', '--secondary-hover-bg',
  '--font-sans', '--font-mono', '--foreground',
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
