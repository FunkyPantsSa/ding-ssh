import {TonalPalette, hexFromArgb} from '@material/material-color-utilities'
import {clamp, composite, contrastRatio, hexToOklch, oklchToHex, rgba} from './color'
import type {AmbientGradients, GradientRamp, ThemeTone} from './types'

export interface ToneLayout {
  app: number
  sidebarPrimary: number
  sidebarSecondary: number
  workspace: number
  surface: number
  surfaceRaised: number
  surfaceOverlay: number
  toolbar: number
  /** Terminal is an ink layer below the toolbar, not a copy of the workspace. */
  terminal: number
  field: number
}

/**
 * Region tones. `separation` stretches the distance between the shell, the
 * workspace and the cards; `atmosphere` separately controls chroma.
 */
export function toneLayout(tone: ThemeTone, separation = 65, atmosphere = 75): ToneLayout {
  const s = clamp(separation, 0, 100) / 100
  const a = clamp(atmosphere, 0, 100) / 100
  if (tone === 'light') {
    const workspace = 96.4 - 1.2 * a
    const surface = Math.min(98.7 - 0.8 * a, workspace + 1.6 + 2.4 * s)
    const raised = Math.min(99.2 - 0.5 * a, surface + 0.5 + 0.4 * s)
    return {
      app: 96.9 - 1.2 * a,
      sidebarPrimary: workspace - (4.0 + 12 * s),
      sidebarSecondary: workspace - (2.6 + 6.5 * s),
      workspace,
      surface,
      surfaceRaised: raised,
      surfaceOverlay: Math.min(99.6 - 0.4 * a, raised + 0.4 + 0.2 * s),
      toolbar: workspace - (1.6 + 2.4 * s),
      terminal: Math.min(98.2, Math.max(97.5, surface + 0.2)),
      field: raised,
    }
  }

  const workspace = 23.9 + 1.2 * s
  const surface = 29.4 + 1.6 * s
  const raised = 32.0 + 1.6 * s
  // Dark mode: toolbar is stepped down between the workspace/sidebar and terminal ink.
  const toolbar = 18.8 + 0.8 * s
  return {
    app: 16.2 + 0.4 * s,
    sidebarPrimary: 32.6 + 1.8 * s,
    sidebarSecondary: 27.1 + 1.5 * s,
    workspace,
    surface,
    surfaceRaised: raised,
    surfaceOverlay: 35.0 + 1.6 * s,
    toolbar,
    terminal: 11.0,
    field: 30.8 + 1.6 * s,
  }
}

export function paletteForHueChroma(hue: number, chroma: number): TonalPalette {
  return TonalPalette.fromHueAndChroma(hue, Math.max(0, chroma))
}

export const LIGHT_TINT_LEVELS = {
  nav: {ratio: 0.82, max: 30},
  secondary: {ratio: 0.4, max: 14},
  toolbar: {ratio: 0.3, max: 10},
  workspace: {ratio: 0.26, max: 9},
  surface: {ratio: 0.1, max: 4},
  border: {ratio: 0.2, max: 8},
} as const

export const DARK_TINT = {
  target: {ratio: 0.75, max: 24},
  nav: {influence: 1, hueWeightFloor: 0.85, hueWeightSpan: 0.15},
  secondary: {influence: 0.72, hueWeightFloor: 0.72, hueWeightSpan: 0.28},
  toolbar: {influence: 0.65, hueWeightFloor: 0.75, hueWeightSpan: 0.25},
  workspace: {influence: 0.62, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  surface: {influence: 0.6, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  terminal: {influence: 0.4, hueWeightFloor: 0.75, hueWeightSpan: 0.25},
  field: {influence: 0.5, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  border: {influence: 0.3, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  glow: {influence: 0.8, hueWeightFloor: 0.65, hueWeightSpan: 0.35},
} as const

export const DARK_TERMINAL_MAX_NEIGHBOUR_CHROMA = 0.72

/**
 * Continuous hue weight for dark tinted neutrals. 1.0 around blue/cyan,
 * lower around red/yellow and their neighbouring hues.
 */
export function darkHueWeight(hue: number): number {
  const normalized = ((hue % 360) + 360) % 360
  const cosine = Math.cos(((normalized - 210) * Math.PI) / 180)
  return clamp(0.18 + 0.82 * (0.5 + 0.5 * cosine), 0.18, 1)
}

/**
 * Applies a theme tint to a neutral foundation colour in OKLCH.
 */
export function applyTint(
  neutralHex: string,
  tintHex: string,
  influence: number,
  maxChroma = Number.POSITIVE_INFINITY,
): string {
  if (influence <= 0) return neutralHex
  const neutral = hexToOklch(neutralHex)
  const tint = hexToOklch(tintHex)
  const chroma = Math.min(tint.c * influence, maxChroma)
  if (chroma < 0.001) return neutralHex
  return oklchToHex({l: neutral.l, c: chroma, h: tint.h})
}

function shiftHue(hue: number, degrees: number): number {
  return ((hue + degrees) % 360 + 360) % 360
}

function ambientRamp(baseHex: string, tone: ThemeTone, kind: 'sidebarPrimary' | 'workspace'): GradientRamp {
  const base = hexToOklch(baseHex)
  const config =
    kind === 'sidebarPrimary'
      ? tone === 'light'
        ? {delta: 0.03, hueShift: 1.2, angle: 180}
        : {delta: 0.018, hueShift: 0, angle: 180}
      : tone === 'light'
        ? {delta: 0.024, hueShift: -1.4, angle: 135}
        : {delta: 0.012, hueShift: 0, angle: 135}

  const steps = 5
  const stops: string[] = []
  for (let index = 0; index < steps; index += 1) {
    const t = index / (steps - 1)
    const eased = t * t * (3 - 2 * t)
    const lightness = clamp(base.l + config.delta / 2 - config.delta * eased, 0, 1)
    const chroma = Math.max(0, base.c * (tone === 'light' ? 1.01 - 0.055 * eased : 1))
    const hue = shiftHue(base.h, config.hueShift * eased)
    stops.push(oklchToHex({l: lightness, c: chroma, h: hue}))
  }

  return {
    angle: config.angle,
    stops,
    lightnessDelta: Math.abs(hexToOklch(stops[stops.length - 1]).l - hexToOklch(stops[0]).l),
  }
}

/** Derives both ambient ramps from already-derived semantic surfaces. */
export function deriveAmbientGradients(roles: {
  tone: ThemeTone
  sidebarPrimaryBg: string
  workspaceBg: string
}): AmbientGradients {
  return {
    sidebarPrimary: ambientRamp(roles.sidebarPrimaryBg, roles.tone, 'sidebarPrimary'),
    workspace: ambientRamp(roles.workspaceBg, roles.tone, 'workspace'),
  }
}

export function toneHex(palette: TonalPalette, tone: number): string {
  return hexFromArgb(palette.tone(clamp(tone, 0, 100)))
}

export function ensureTone(
  palette: TonalPalette,
  preferred: number,
  backgrounds: string[],
  target: number,
  direction: -1 | 1,
): string {
  const start = clamp(preferred, 0, 100)
  const startHex = toneHex(palette, start)
  if (backgrounds.every((background) => contrastRatio(startHex, background) >= target)) return startHex
  for (let step = 0.5; step <= 100; step += 0.5) {
    const tone = clamp(start + direction * step, 0, 100)
    const hex = toneHex(palette, tone)
    if (backgrounds.every((background) => contrastRatio(hex, background) >= target)) return hex
    if ((direction < 0 && tone <= 0) || (direction > 0 && tone >= 100)) break
  }
  return direction < 0 ? '#000000' : '#ffffff'
}

export function foregroundOn(background: string, target = 4.5): string {
  const light = '#ffffff'
  const dark = '#0b0e12'
  const lightRatio = contrastRatio(light, background)
  const darkRatio = contrastRatio(dark, background)
  if (lightRatio >= target || darkRatio >= target) return lightRatio >= darkRatio ? light : dark
  return lightRatio >= darkRatio ? light : dark
}

export function statusStyle(
  palette: TonalPalette,
  preferred: number,
  backgrounds: string[],
  direction: -1 | 1,
  surface: string,
): {color: string; bg: string; border: string} {
  let color = ensureTone(palette, preferred, backgrounds, 4.5, direction)
  let bg = composite(color, surface, 0.12)
  if (contrastRatio(color, bg) < 4.5) {
    color = ensureTone(palette, preferred, [...backgrounds, bg], 4.5, direction)
    bg = composite(color, surface, 0.12)
  }
  return {color, bg, border: rgba(color, 0.34)}
}
