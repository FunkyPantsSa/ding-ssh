/**
 * Theme facade.
 *
 * Responsibilities are deliberately split:
 *  - `material.ts` — pure HCT color science, semantic roles, material plans.
 *  - `engine.ts`  — fonts, tone resolution, CSS application and compatibility
 *                   helpers used by the shell and terminal.
 *
 * Nothing in this file mutates the terminal palette. The terminal owns its
 * ANSI colors; only its background may coordinate with the app theme.
 */
import {
  MANAGED_ROOT_VARS,
  clamp,
  composite,
  contrastRatio,
  deriveColorRoles,
  detectCapabilities,
  detectPlatform,
  hexToRgb,
  normalizeSeed,
  planMaterial,
  rgba,
  rolesToCssVars,
  relativeLuminance,
} from './material'
import type {
  ColorRoleOptions,
  ColorRoles,
  MaterialMode,
  MaterialPlan,
  ThemeCapabilities,
  ThemeMeta,
  ThemePlatform,
  ThemeTone,
} from './material'
import type {Fonts, Theme, UIAppearance} from '../types'
import {defaultPreset, presetById} from './presets'
import {terminalFontFamilies} from './terminal-fonts'

export type Tone = ThemeTone
export {clamp, composite, contrastRatio, detectCapabilities, detectPlatform, hexToRgb, normalizeSeed, relativeLuminance, rgba}
export type {ColorRoles, MaterialMode, MaterialPlan, ThemeCapabilities, ThemeMeta, ThemePlatform}
export {MANAGED_ROOT_VARS}

// ---- Legacy color helpers (compatibility only) -----------------------------

export type Rgb = {r: number; g: number; b: number}

/** Simple sRGB mix used by legacy glow tokens and terminal cursor helpers. */
function mix(hex: string, target: string, ratio: number): string {
  const a = hexToRgb(hex)
  const b = hexToRgb(target)
  const amount = clamp(ratio, 0, 1)
  const channel = (x: number, y: number) => Math.round(x + (y - x) * amount)
  return `#${[channel(a.r, b.r), channel(a.g, b.g), channel(a.b, b.b)]
    .map((value) => value.toString(16).padStart(2, '0'))
    .join('')}`
}

export function lighten(hex: string, amount: number): string {
  return mix(hex, '#ffffff', amount)
}

export function darken(hex: string, amount: number): string {
  return mix(hex, '#000000', amount)
}

/** Legacy, non-WCAG helper retained for the terminal cursor accent. */
export function readableOn(hex: string): string {
  const {r, g, b} = hexToRgb(hex)
  return (r * 299 + g * 587 + b * 114) / 1000 > 160 ? '#09090b' : '#ffffff'
}

/**
 * Compatibility helper for code that still expects the old brand-variable
 * bundle. New code should consume semantic tokens from `deriveSurfaceVars`
 * / `computeUiCssVars` instead.
 */
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

// ---- Compatibility surface helpers ----------------------------------------

export interface SurfaceVarOptions {
  seed: string
  tone: ThemeTone
  atmosphere?: number
  separation?: number
}

/** Solid semantic variables for a seed, without any glass composition. */
export function deriveSurfaceVars(
  seed: string,
  tone: ThemeTone,
  glassOpacity = 85,
  glassBlur = 24,
  atmosphere = 75,
  separation = 65,
): Record<string, string> {
  const roles = deriveColorRoles({seed, tone, atmosphere, separation})
  const vars = rolesToCssVars(roles, planMaterial({mode: 'solid', nativeBackdrop: false, platform: 'web'}))
  // Preserve the historical helper contract: callers may still ask what the
  // glass sliders would have produced, even though the surfaces stay opaque.
  vars['--theme-glass-opacity'] = `${clamp(glassOpacity, 0, 100)}%`
  vars['--theme-glass-blur'] = `${clamp(glassBlur, 0, 60)}px`
  return vars
}

/** Opaque terminal surface derived from the current workspace tint. */
export function terminalSurfaceColor(
  seed: string,
  tone: ThemeTone,
  atmosphere = 75,
  separation = 65,
): string {
  return deriveColorRoles({seed, tone, atmosphere, separation}).terminalBg
}

/** Accent color used to coordinate terminal cursor/selection in follow mode. */
export function terminalAccentColor(
  seed: string,
  tone: ThemeTone,
  atmosphere = 75,
  separation = 65,
): string {
  return deriveColorRoles({seed, tone, atmosphere, separation}).accentBg
}

/** Recognize backgrounds written by earlier ding-ssh theme engines. */
export function isManagedTerminalBackground(background: string, seed: string, tone: ThemeTone): boolean {
  const value = normalizeSeed(background)
  const other = tone === 'light' ? 'dark' : 'light'
  const known = new Set<string>([
    '#fafbfc',
    '#f5f6f7',
    '#eff5ff',
    '#223550',
    mix(seed, '#303b4b', 0.65),
    mix(seed, '#263449', 0.85),
    darken(seed, 0.72),
    mix(seed, '#f5f6f7', 0.97),
    mix(seed, '#ffffff', 0.94),
    mix(seed, '#ffffff', 0.91),
    terminalSurfaceColor(seed, tone),
    terminalSurfaceColor(seed, other),
  ])
  return known.has(value) || background === '#fafbfc' || background === '#f5f6f7'
}

/** Keep terminal ANSI colors; only coordinate its opaque background. */
export function deriveTerminalTheme(
  base: Theme,
  seed: string,
  tone: ThemeTone,
  atmosphere = 75,
  separation = 65,
): Theme {
  return {...base, background: terminalSurfaceColor(seed, tone, atmosphere, separation)}
}

/** Current UI seed/secondary (custom mode uses the user's chosen values). */
export function effectiveBrand(appearance: UIAppearance): {primary: string; secondary: string} {
  if (appearance.mode === 'preset') {
    const preset = presetById(appearance.presetId) ?? defaultPreset()
    return {primary: preset.primary, secondary: preset.secondary}
  }
  return {
    primary: normalizeSeed(appearance.primary || defaultPreset().primary),
    secondary: normalizeSeed(appearance.secondary || appearance.primary || defaultPreset().primary),
  }
}

// ---- Tone resolution -------------------------------------------------------

/** Resolve the actual tone (auto follows the OS). */
export function resolveTone(baseTone: UIAppearance['baseTone']): Tone {
  if (baseTone !== 'auto') return baseTone
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: light)').matches) {
    return 'light'
  }
  return 'dark'
}

export function isSystemToneQuery(tone: Tone): string {
  return tone === 'dark' ? '(prefers-color-scheme: dark)' : '(prefers-color-scheme: light)'
}

export function glassAlpha(opacity: number, tone: Tone): number {
  const amount = clamp(opacity / 100, 0.15, 1)
  return tone === 'light' ? 0.35 + amount * 0.65 : amount
}

// ---- Font stacks -----------------------------------------------------------

const SYSTEM_UI_FONT_STACK = '"PingFang SC", "Helvetica Neue", "Microsoft YaHei", system-ui, sans-serif'
const SYSTEM_MONO_FONT_STACK = '"Noto Sans SC", ui-monospace, "SF Mono", Menlo, Monaco, Consolas, "Cascadia Mono", monospace'

export const UI_FONT_FALLBACK = `"Geist Sans", ${SYSTEM_UI_FONT_STACK}`
export const MONO_FONT_FALLBACK = `"Geist Mono", ${SYSTEM_MONO_FONT_STACK}`

export function uiFontStack(name: string | undefined): string {
  if (!name) return UI_FONT_FALLBACK
  if (name === 'system') return SYSTEM_UI_FONT_STACK
  return `"${name}", ${SYSTEM_UI_FONT_STACK}`
}

export function monoFontStack(name: string | undefined): string {
  if (!name) return MONO_FONT_FALLBACK
  if (name === 'system') return 'ui-monospace, "SF Mono", Menlo, Consolas, "Noto Sans SC", monospace'
  const families = [...new Set([...terminalFontFamilies(name), 'Geist Mono'])]
  return `${families.map((family) => `"${family}"`).join(', ')}, ${SYSTEM_MONO_FONT_STACK}`
}

// ---- Full theme computation ------------------------------------------------

export function resolveMaterialMode(appearance: UIAppearance): MaterialMode {
  if (appearance.materialMode === 'soft' || appearance.materialMode === 'clear' || appearance.materialMode === 'solid') {
    return appearance.materialMode
  }
  // Legacy settings only had a boolean glass switch.
  return appearance.glassEnabled ? 'soft' : 'solid'
}

export interface ComputedTheme {
  vars: Record<string, string>
  meta: ThemeMeta
  roles: ColorRoles
  plan: MaterialPlan
}

/**
 * Computes every CSS variable the shell needs. Pure: the same appearance,
 * fonts, tone and capabilities always produce the same result.
 */
export function computeTheme(
  appearance: UIAppearance,
  fonts: Fonts,
  tone: Tone,
  capabilities?: ThemeCapabilities,
): ComputedTheme {
  const caps = capabilities ?? detectCapabilities()
  const {primary, secondary} = effectiveBrand(appearance)
  const atmosphere = clamp(appearance.atmosphere ?? appearance.colorIntensity ?? 75, 0, 100)
  const separation = clamp(appearance.surfaceSeparation ?? 65, 0, 100)
  const requestedMode = resolveMaterialMode(appearance)
  const roles = deriveColorRoles({
    seed: primary,
    secondarySeed: secondary,
    tone,
    atmosphere,
    separation,
  })
  const plan = planMaterial({
    mode: requestedMode,
    nativeBackdrop: caps.nativeBackdrop,
    surfaceOpacity: appearance.glassOpacity ?? 85,
    blurPx: appearance.glassBlur ?? 24,
    platform: caps.platform,
  })
  const vars = rolesToCssVars(roles, plan)
  vars['--font-sans'] = uiFontStack(fonts.uiFont)
  vars['--font-mono'] = monoFontStack(fonts.terminalFont)
  const meta: ThemeMeta = {
    tone,
    requestedMode,
    materialMode: plan.mode,
    fallback: plan.fallback,
    nativeBackdrop: plan.nativeBackdrop,
    platform: plan.platform,
  }
  return {vars, meta, roles, plan}
}

/** Backwards-compatible CSS-variable entry point. */
export function computeUiCssVars(
  appearance: UIAppearance,
  fonts: Fonts,
  tone: Tone,
  capabilities?: ThemeCapabilities,
): Record<string, string> {
  return computeTheme(appearance, fonts, tone, capabilities).vars
}

// ---- CSS application & startup cache --------------------------------------

const EXTRA_MANAGED_VARS = ['--font-sans', '--font-mono'] as const

function materialFromVars(vars: Record<string, string>): MaterialMode {
  const opacity = parseFloat(vars['--glass-opacity'] ?? '100')
  if (!Number.isFinite(opacity) || opacity >= 99.5) return 'solid'
  return opacity <= 82 ? 'clear' : 'soft'
}

/**
 * Writes the theme to <html>. `meta` is optional so older call sites and tests
 * can keep using `(tone, vars)`.
 */
export function applyRootTheme(tone: Tone, vars: Record<string, string>, meta?: ThemeMeta): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const material = meta?.materialMode ?? materialFromVars(vars)
  root.setAttribute('data-tone', tone)
  root.setAttribute('data-material', material)
  root.setAttribute('data-glass', material === 'solid' ? 'off' : 'on')
  root.setAttribute('data-material-requested', meta?.requestedMode ?? material)
  root.setAttribute('data-material-fallback', meta?.fallback ?? 'none')
  root.setAttribute('data-native-backdrop', meta?.nativeBackdrop ? 'on' : 'off')
  root.setAttribute('data-theme-platform', meta?.platform ?? detectPlatform())
  root.style.colorScheme = tone
  for (const key of [...MANAGED_ROOT_VARS, ...EXTRA_MANAGED_VARS]) {
    root.style.removeProperty(key)
  }
  for (const [key, value] of Object.entries(vars)) {
    if (value) root.style.setProperty(key, value)
  }
}

export function clearRootTheme(): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  for (const attr of [
    'data-tone',
    'data-glass',
    'data-material',
    'data-material-requested',
    'data-material-fallback',
    'data-native-backdrop',
    'data-theme-platform',
  ]) {
    root.removeAttribute(attr)
  }
  root.style.removeProperty('color-scheme')
  for (const key of [...MANAGED_ROOT_VARS, ...EXTRA_MANAGED_VARS]) {
    root.style.removeProperty(key)
  }
}

export const THEME_CACHE_KEY = 'ding-ssh:dynamic-theme-v1'

export interface ThemeCachePayload {
  version: 1
  tone: Tone
  material: MaterialMode
  requested: MaterialMode
  fallback: string
  platform: ThemePlatform
  nativeBackdrop: boolean
  vars: Record<string, string>
}

/**
 * Persists the last computed variables so index.html can paint the correct
 * theme before Vue mounts and settings finish loading (no default-theme flash).
 */
export function writeThemeCache(tone: Tone, computed: ComputedTheme): void {
  if (typeof localStorage === 'undefined') return
  const payload: ThemeCachePayload = {
    version: 1,
    tone,
    material: computed.meta.materialMode,
    requested: computed.meta.requestedMode,
    fallback: computed.meta.fallback,
    platform: computed.meta.platform,
    nativeBackdrop: computed.meta.nativeBackdrop,
    vars: computed.vars,
  }
  try {
    localStorage.setItem(THEME_CACHE_KEY, JSON.stringify(payload))
  } catch {
    // Storage is optional; a failed cache only costs a brief default paint.
  }
}

export function readThemeCache(): ThemeCachePayload | null {
  if (typeof localStorage === 'undefined') return null
  try {
    const raw = localStorage.getItem(THEME_CACHE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ThemeCachePayload
    if (parsed?.version !== 1 || !parsed.vars || typeof parsed.vars !== 'object') return null
    return parsed
  } catch {
    return null
  }
}

/** Contrast uses the effective terminal background, including its theme tint. */
export function terminalContrastForeground(foreground: string, background: string): string {
  if (contrastRatio(foreground, background) >= 4.5) return foreground
  return relativeLuminance(background) > 0.179 ? '#09090b' : '#ffffff'
}

/** Convenience for callers that only need roles (tests, previews, debug API). */
export function computeColorRoles(options: ColorRoleOptions): ColorRoles {
  return deriveColorRoles(options)
}
