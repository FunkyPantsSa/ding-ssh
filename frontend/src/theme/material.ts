/**
 * Dynamic color core for ding-ssh.
 *
 * A user picks a seed color; this module derives a complete, deterministic
 * application palette from Material Color Utilities' HCT model. It never maps
 * Material Design roles one-to-one onto the shell: ding-ssh has its own six
 * semantic regions (primary sidebar, secondary sidebar, workspace, surfaces,
 * interactions, text/status), each with its own tone and chroma rules.
 *
 * Everything here is a pure function of its input. No randomness, timers,
 * DOM access or store access: the Pinia store owns state, this module owns
 * color science, and `engine.ts` owns CSS application.
 */
import {Hct, SchemeTonalSpot, TonalPalette, argbFromHex, hexFromArgb} from '@material/material-color-utilities'

export type ThemeTone = 'light' | 'dark'
export type MaterialMode = 'solid' | 'soft' | 'clear'
export type ThemePlatform = 'macos' | 'windows' | 'linux' | 'web'
export type MaterialFallback = 'none' | 'unsupported-platform' | 'opaque'

export const DEFAULT_SEED = '#71b5d2'

/** Neutral seeds (black/white/gray) must not invent a colorful accent. */
const NEUTRAL_SEED_CHROMA = 2.5

export interface ThemeCapabilities {
  platform: ThemePlatform
  /** Wails runtime is present (production WebView, not a plain browser). */
  wails: boolean
  /** The native window actually provides a desktop backdrop on this platform. */
  nativeBackdrop: boolean
}

/**
 * Dark workspace ambient treatment. Kept internal (no user-facing slider) so
 * the three candidate strategies can be compared in tests and previews.
 */
export type WorkspaceGradientStyle = 'solid' | 'subtle' | 'corner'

export interface ColorRoleOptions {
  seed: string
  /** Preset secondary color, used only for the secondary accent. */
  secondarySeed?: string
  tone: ThemeTone
  /** 0–100: how far large surfaces may drift from neutral. */
  atmosphere?: number
  /** 0–100: how much the shell regions separate from each other. */
  separation?: number
  /** Dark-only workspace lighting strategy. Light mode keeps its existing ramp. */
  workspaceGradientStyle?: WorkspaceGradientStyle
}

/**
 * Ambient gradients are intentionally derived, never hand-authored.
 *
 * `stops` are opaque sRGB colours ordered from the top/left edge to the
 * bottom/right edge. The material layer applies region alpha when composing
 * glass, so the ramp itself stays independent from Solid / Soft / Clear.
 */
export interface GradientRamp {
  angle: number
  stops: string[]
  /** OKLCH lightness distance between the first and last stop. */
  lightnessDelta: number
}

export interface AmbientGradients {
  sidebarPrimary: GradientRamp
  workspace: GradientRamp
}

export interface ColorRoles {
  seed: string
  secondarySeed: string
  tone: ThemeTone
  seedChroma: number
  seedHue: number
  /** 0–100 effective separation used for the derived tones. */
  separation: number
  /** 0–100 effective atmosphere used for the derived chroma. */
  atmosphere: number
  /** Effective dark workspace lighting strategy. */
  workspaceGradientStyle: WorkspaceGradientStyle
  /** Dark-only theme-tinted light used by the corner ambient style. */
  workspaceGlowColor?: string

  appBg: string
  sidebarPrimaryBg: string
  sidebarSecondaryBg: string
  workspaceBg: string
  surfaceBg: string
  surfaceRaisedBg: string
  surfaceOverlayBg: string
  toolbarBg: string
  fieldBg: string

  hoverBg: string
  hoverStrongBg: string
  interactiveHoverBg: string
  interactiveActiveBg: string
  interactivePressedBg: string
  interactiveDisabledBg: string
  focusRing: string

  textPrimary: string
  textSecondary: string
  textMuted: string
  textDisabled: string
  textOnAccent: string

  borderSubtle: string
  borderDefault: string
  borderStrong: string

  accentBg: string
  accentHoverBg: string
  accentForeground: string
  accentWeakBg: string
  accentBorder: string
  accentStrongBorder: string

  secondaryBg: string
  secondaryHoverBg: string
  secondaryForeground: string

  statusSuccess: string
  statusSuccessBg: string
  statusSuccessBorder: string
  statusWarning: string
  statusWarningBg: string
  statusWarningBorder: string
  statusError: string
  statusErrorHover: string
  statusErrorBg: string
  statusErrorBorder: string
  statusInfo: string
  statusInfoBg: string
  statusInfoBorder: string

  terminalBg: string

  /** Derived ambient ramps for the two large shell regions. */
  gradients: AmbientGradients
}

export interface MaterialPlanOptions {
  mode: MaterialMode
  nativeBackdrop: boolean
  surfaceOpacity?: number
  blurPx?: number
  platform?: ThemePlatform
}

export interface MaterialPlan {
  requestedMode: MaterialMode
  mode: MaterialMode
  fallback: MaterialFallback
  nativeBackdrop: boolean
  platform: ThemePlatform
  /** Alpha applied to each tinted shell region (1 = opaque). */
  alpha: {
    sidebarPrimary: number
    sidebarSecondary: number
    workspace: number
    surface: number
    surfaceRaised: number
    surfaceOverlay: number
    toolbar: number
    field: number
  }
  /** Desktop blur is never simulated by the web layer; shell blur stays 0. */
  shellBlurPx: number
  /** Overlays may blur app content behind them (not the desktop). */
  overlayBlurPx: number
  surfaceOpacity: number
}

export interface ThemeMeta {
  tone: ThemeTone
  requestedMode: MaterialMode
  materialMode: MaterialMode
  fallback: MaterialFallback
  nativeBackdrop: boolean
  platform: ThemePlatform
}

// ---------------------------------------------------------------------------
// Small color primitives
// ---------------------------------------------------------------------------

export const clamp = (value: number, min: number, max: number): number =>
  Math.min(max, Math.max(min, value))

/** Normalizes #rgb / #rrggbb (and tolerates #rrggbbaa) to lowercase #rrggbb. */
export function normalizeSeed(seed: string | undefined): string {
  const raw = (seed ?? '').trim().replace(/^#/, '')
  let value = raw
  if (/^[0-9a-f]{3}$/i.test(value)) {
    value = value
      .split('')
      .map((char) => char + char)
      .join('')
  }
  if (/^[0-9a-f]{6}[0-9a-f]{2}$/i.test(value)) value = value.slice(0, 6)
  if (!/^[0-9a-f]{6}$/i.test(value)) return DEFAULT_SEED
  return `#${value.toLowerCase()}`
}

export interface Rgb {
  r: number
  g: number
  b: number
}

export function hexToRgb(hex: string): Rgb {
  const value = normalizeSeed(hex)
  const n = parseInt(value.slice(1), 16)
  return {r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255}
}

export function rgbToHex(rgb: Rgb): string {
  const channel = (value: number) => clamp(Math.round(value), 0, 255).toString(16).padStart(2, '0')
  return `#${channel(rgb.r)}${channel(rgb.g)}${channel(rgb.b)}`
}

export function rgba(hex: string, alpha: number): string {
  const {r, g, b} = hexToRgb(hex)
  return `rgba(${r}, ${g}, ${b}, ${clamp(alpha, 0, 1)})`
}

function linearChannel(value: number): number {
  const n = value / 255
  return n <= 0.04045 ? n / 12.92 : ((n + 0.055) / 1.055) ** 2.4
}

export function relativeLuminance(hex: string): number {
  const {r, g, b} = hexToRgb(hex)
  return 0.2126 * linearChannel(r) + 0.7152 * linearChannel(g) + 0.0722 * linearChannel(b)
}

/** WCAG 2.x contrast ratio of two sRGB colors. */
export function contrastRatio(foreground: string, background: string): number {
  const a = relativeLuminance(foreground)
  const b = relativeLuminance(background)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}

/** Flattens a translucent foreground over an opaque background. */
export function composite(foreground: string, background: string, alpha: number): string {
  const f = hexToRgb(foreground)
  const b = hexToRgb(background)
  const amount = clamp(alpha, 0, 1)
  // `foreground` is layered over `background` at `alpha`.
  return rgbToHex({
    r: b.r + (f.r - b.r) * amount,
    g: b.g + (f.g - b.g) * amount,
    b: b.b + (f.b - b.b) * amount,
  })
}

// ---------------------------------------------------------------------------
// OKLCH helpers (ambient gradients only)
//
// The semantic palette is still derived with Material Color Utilities / HCT.
// Gradients intentionally move along OKLCH lightness and chroma because those
// axes produce a calmer, more even large-area ramp than HCT tone steps. Every
// endpoint is gamut-mapped back into sRGB before it reaches CSS.
// ---------------------------------------------------------------------------

export interface Oklch {
  /** 0–1 perceptual lightness. */
  l: number
  /** Chroma; 0 is neutral. */
  c: number
  /** Hue in degrees, 0–360. */
  h: number
}

function srgbToLinear(channel: number): number {
  const value = channel / 255
  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
}

function linearToSrgb(channel: number): number {
  const value = channel <= 0.0031308 ? 12.92 * channel : 1.055 * channel ** (1 / 2.4) - 0.055
  return clamp(value, 0, 1) * 255
}

export function hexToOklch(hex: string): Oklch {
  const {r, g, b} = hexToRgb(hex)
  const lr = srgbToLinear(r)
  const lg = srgbToLinear(g)
  const lb = srgbToLinear(b)

  const l = 0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb
  const m = 0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb
  const s = 0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb

  const lRoot = Math.cbrt(l)
  const mRoot = Math.cbrt(m)
  const sRoot = Math.cbrt(s)

  const lightness = 0.2104542553 * lRoot + 0.793617785 * mRoot - 0.0040720468 * sRoot
  const a = 1.9779984951 * lRoot - 2.428592205 * mRoot + 0.4505937099 * sRoot
  const bb = 0.0259040371 * lRoot + 0.7827717662 * mRoot - 0.808675766 * sRoot

  const hue = (Math.atan2(bb, a) * 180) / Math.PI
  return {
    l: clamp(lightness, 0, 1),
    c: Math.max(0, Math.hypot(a, bb)),
    h: hue < 0 ? hue + 360 : hue,
  }
}

function oklchToLinearRgb({l, c, h}: Oklch): {r: number; g: number; b: number} {
  const radians = (h * Math.PI) / 180
  const a = c * Math.cos(radians)
  const b = c * Math.sin(radians)

  const lRoot = l + 0.3963377774 * a + 0.2158037573 * b
  const mRoot = l - 0.1055613458 * a - 0.0638541728 * b
  const sRoot = l - 0.0894841775 * a - 1.291485548 * b

  const l3 = lRoot ** 3
  const m3 = mRoot ** 3
  const s3 = sRoot ** 3

  return {
    r: 4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3,
    g: -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3,
    b: -0.0041960863 * l3 - 0.7034186147 * m3 + 1.707614701 * s3,
  }
}

function inSrgbGamut(rgb: {r: number; g: number; b: number}): boolean {
  const epsilon = 0.0001
  return [rgb.r, rgb.g, rgb.b].every((channel) => channel >= -epsilon && channel <= 1 + epsilon)
}

/**
 * Maps an OKLCH colour into sRGB by reducing chroma only. Lightness and hue
 * are preserved, which is what keeps the ramp from shifting hue at its edges.
 */
export function oklchToHex(color: Oklch): string {
  const lightness = clamp(color.l, 0, 1)
  const hue = ((color.h % 360) + 360) % 360
  let chroma = Math.max(0, color.c)
  let linear = oklchToLinearRgb({l: lightness, c: chroma, h: hue})

  if (!inSrgbGamut(linear)) {
    let low = 0
    let high = chroma
    for (let i = 0; i < 20; i += 1) {
      const mid = (low + high) / 2
      const candidate = oklchToLinearRgb({l: lightness, c: mid, h: hue})
      if (inSrgbGamut(candidate)) low = mid
      else high = mid
    }
    chroma = low
    linear = oklchToLinearRgb({l: lightness, c: chroma, h: hue})
  }

  return rgbToHex({
    r: linearToSrgb(linear.r),
    g: linearToSrgb(linear.g),
    b: linearToSrgb(linear.b),
  })
}

function shiftHue(hue: number, degrees: number): number {
  return ((hue + degrees) % 360 + 360) % 360
}

/**
 * Builds a deterministic same-hue ramp around a semantic base surface.
 *
 * The start is slightly lighter than the base and the end slightly darker.
 * Chroma is held almost constant (a tiny decrease toward the end) so the
 * gradient reads as light falling across one material, not as two colours.
 */
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
    // Ease the ramp slightly: the middle stays close to the semantic base and
    // the extremes carry the ambient drift. This avoids a visible "band" where
    // the gradient meets the solid cards.
    const eased = t * t * (3 - 2 * t)
    const lightness = clamp(base.l + config.delta / 2 - config.delta * eased, 0, 1)
    // Dark surfaces keep chroma constant across the ramp: only a small
    // lightness change is allowed, so the workspace never gets a colour wash.
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

function toneHex(palette: TonalPalette, tone: number): string {
  return hexFromArgb(palette.tone(clamp(tone, 0, 100)))
}

/**
 * Finds the closest tone to `preferred` (searching in one direction) whose
 * resulting sRGB color satisfies `target` against every background.
 *
 * Direction is darkening in light themes and lightening in dark themes, which
 * matches how text and borders normally gain contrast.
 */
function ensureTone(
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

function foregroundOn(background: string, target = 4.5): string {
  const light = '#ffffff'
  const dark = '#0b0e12'
  const lightRatio = contrastRatio(light, background)
  const darkRatio = contrastRatio(dark, background)
  if (lightRatio >= target || darkRatio >= target) return lightRatio >= darkRatio ? light : dark
  return lightRatio >= darkRatio ? light : dark
}

// ---------------------------------------------------------------------------
// Ding-ssh tone layout
// ---------------------------------------------------------------------------

interface ToneLayout {
  app: number
  sidebarPrimary: number
  sidebarSecondary: number
  workspace: number
  surface: number
  surfaceRaised: number
  surfaceOverlay: number
  toolbar: number
  field: number
}

/**
 * Region tones. `separation` stretches the distance between the shell, the
 * workspace and the cards; `atmosphere` separately controls chroma.
 *
 * Light: cards are brighter than the workspace, the primary sidebar is the
 * most colored and darkest region. Dark: cards are slightly brighter than the
 * workspace and the primary sidebar is the most colored, brighter region.
 */
export function toneLayout(tone: ThemeTone, separation = 65, atmosphere = 75): ToneLayout {
  const s = clamp(separation, 0, 100) / 100
  const a = clamp(atmosphere, 0, 100) / 100
  if (tone === 'light') {
    // Light hierarchy: nav (most coloured / darkest) > secondary nav >
    // toolbar > workspace > cards (near-neutral, brightest).
    //
    // The workspace is kept high enough to read as a tinted paper surface, not
    // as a blue block. Higher atmosphere lowers it slightly so HCT can render
    // the requested chroma without clipping it into white.
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
      field: raised,
    }
  }
  // Dark hierarchy is a neutral foundation first, not a tinted reproduction of
  // the light theme. The wide tone range keeps the interface from collapsing
  // into one mid-grey band:
  //
  //   terminal ink < workspace < toolbar < card < raised < overlay
  //
  // The primary sidebar is the only strongly coloured shell surface; it sits
  // between the card and raised tones so the step from nav to workspace stays
  // readable but not abrupt. Atmosphere no longer moves these tones: it only
  // controls the small theme tint applied afterwards.
  const workspace = 19.8 + 1.2 * s
  const surface = 24.8 + 1.6 * s
  const raised = 27.4 + 1.6 * s
  return {
    app: 12.2 + 0.4 * s,
    sidebarPrimary: 26.6 + 1.8 * s,
    sidebarSecondary: 23.2 + 1.5 * s,
    workspace,
    surface,
    surfaceRaised: raised,
    surfaceOverlay: 30.4 + 1.6 * s,
    toolbar: 21.0 + 1.2 * s,
    field: 26.2 + 1.6 * s,
  }
}

function paletteForHueChroma(hue: number, chroma: number): TonalPalette {
  return TonalPalette.fromHueAndChroma(hue, Math.max(0, chroma))
}

/**
 * Three colour-expression levels.
 *
 * 1. Expression (tier 1): primary sidebar, selected states, primary actions.
 * 2. Ambience (tier 2): secondary sidebar, toolbar, workspace.
 * 3. Content (tier 3): cards, fields, dialogs, tables and terminal.
 *
 * Every large region is derived from the same primary hue; only chroma and
 * tone change. This is what prevents the "everything is the theme colour"
 * effect without falling back to a fixed white/grey shell.
 */
const LIGHT_TINT_LEVELS = {
  nav: {ratio: 0.82, max: 30},
  secondary: {ratio: 0.4, max: 14},
  toolbar: {ratio: 0.3, max: 10},
  workspace: {ratio: 0.26, max: 9},
  surface: {ratio: 0.1, max: 4},
  border: {ratio: 0.2, max: 8},
} as const

/**
 * Dark tint influence map.
 *
 * The dark foundation is neutral; theme colour is a *generated* fraction of a
 * capped tint target, never a CSS overlay. `influence` is therefore a chroma
 * budget, not opacity. The primary sidebar keeps a strong identity while the
 * workspace and cards stay almost neutral.
 *
 * `hueWeightFloor/Span` feed a continuous hue-aware weight: cool hues can use
 * more chroma in the dark mid-tones, while warm hues (which turn muddy more
 * easily) are scaled down smoothly. There are no hue segments or per-colour
 * branches.
 */
const DARK_TINT = {
  target: {ratio: 0.75, max: 24},
  nav: {influence: 1, hueWeightFloor: 0.85, hueWeightSpan: 0.15},
  secondary: {influence: 0.72, hueWeightFloor: 0.72, hueWeightSpan: 0.28},
  toolbar: {influence: 0.6, hueWeightFloor: 0.75, hueWeightSpan: 0.25},
  workspace: {influence: 0.62, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  surface: {influence: 0.6, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  field: {influence: 0.5, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  border: {influence: 0.3, hueWeightFloor: 0.7, hueWeightSpan: 0.3},
  glow: {influence: 0.8, hueWeightFloor: 0.65, hueWeightSpan: 0.35},
} as const

/** Dark terminal is an Ink surface measured directly in HCT chroma, not a tint blend. */
const DARK_TERMINAL_INK = {ratio: 0.03, maxHctChroma: 0.5}

/**
 * Continuous hue weight for dark tinted neutrals. 1.0 around blue/cyan,
 * lower around red/yellow and their neighbouring hues. A cosine keeps the
 * function smooth across the entire hue circle; neutral seeds bypass it.
 */
export function darkHueWeight(hue: number): number {
  const normalized = ((hue % 360) + 360) % 360
  const cosine = Math.cos(((normalized - 210) * Math.PI) / 180)
  return clamp(0.18 + 0.82 * (0.5 + 0.5 * cosine), 0.18, 1)
}

/**
 * Applies a theme tint to a neutral foundation colour in OKLCH.
 *
 * Lightness comes from the neutral role (so the dark hierarchy is stable),
 * chroma is a generated fraction of the tint target, and hue comes from the
 * theme. This is a final-colour generation step, not a translucent overlay.
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
  if (chroma < 0.02) return neutralHex
  return oklchToHex({l: neutral.l, c: chroma, h: tint.h})
}

function statusStyle(
  palette: TonalPalette,
  preferred: number,
  backgrounds: string[],
  direction: -1 | 1,
  surface: string,
): {color: string; bg: string; border: string} {
  let color = ensureTone(palette, preferred, backgrounds, 4.5, direction)
  let bg = composite(color, surface, 0.12)
  // The tinted badge background must keep the status text readable too.
  if (contrastRatio(color, bg) < 4.5) {
    color = ensureTone(palette, preferred, [...backgrounds, bg], 4.5, direction)
    bg = composite(color, surface, 0.12)
  }
  return {color, bg, border: rgba(color, 0.34)}
}

// ---------------------------------------------------------------------------
// Role derivation
// ---------------------------------------------------------------------------

/**
 * Derives the complete ding-ssh palette. Deterministic for a given input.
 */
export function deriveColorRoles(options: ColorRoleOptions): ColorRoles {
  const tone = options.tone
  const seed = normalizeSeed(options.seed)
  const secondarySeed = normalizeSeed(options.secondarySeed || seed)
  const atmosphere = clamp(options.atmosphere ?? 75, 0, 100)
  const separation = clamp(options.separation ?? 65, 0, 100)
  const a = atmosphere / 100
  const s = separation / 100

  const source = Hct.fromInt(argbFromHex(seed))
  const scheme = new SchemeTonalSpot(source, tone === 'dark', 0)
  // HCT reports a small residual chroma for pure white; treat tone extremes as
  // neutral so a white/black seed never invents a colorful accent.
  const neutralSeed =
    source.chroma < NEUTRAL_SEED_CHROMA || source.tone <= 0.5 || source.tone >= 99.5

  // Derive each region's chroma once from the seed, then apply atmosphere a
  // single time. Do not multiply by a second low-chroma factor or by the
  // low-chroma Secondary / Neutral Variant palettes: that is what made
  // saturated seeds look grey. Role caps keep large regions comfortable.
  const seedChroma = neutralSeed ? 0 : source.chroma
  // Tonal Spot supplies all six M3 palettes. Ding-ssh uses their hues but
  // deliberately replaces the low M3 chroma with role-specific chroma, so a
  // saturated seed is not flattened by Secondary / Neutral Variant defaults.
  const primaryHue = scheme.primaryPalette.hue
  const tertiaryHue = scheme.tertiaryPalette.hue
  const neutralHue = scheme.neutralPalette.hue
  const neutralVariantHue = scheme.neutralVariantPalette.hue

  const accentChroma = neutralSeed ? 0 : clamp(seedChroma * 0.9, 42, 72)
  const accentPalette = paletteForHueChroma(primaryHue, accentChroma)

  const secondarySource = Hct.fromInt(argbFromHex(secondarySeed))
  const secondaryAccentPalette = paletteForHueChroma(
    // A neutral preset secondary has no useful hue; use Tonal Spot's related
    // tertiary hue so the secondary accent still belongs to the scheme.
    secondarySource.chroma < NEUTRAL_SEED_CHROMA ? tertiaryHue : secondarySource.hue,
    secondarySource.chroma < NEUTRAL_SEED_CHROMA ? 24 : clamp(secondarySource.chroma * 0.7, 24, 48),
  )

  const layout = toneLayout(tone, separation, atmosphere)
  const direction: -1 | 1 = tone === 'light' ? -1 : 1
  const workspaceGradientStyle: WorkspaceGradientStyle = options.workspaceGradientStyle ?? 'corner'

  let neutralPalette: TonalPalette
  let borderPalette: TonalPalette
  let appBg: string
  let sidebarPrimaryBg: string
  let sidebarSecondaryBg: string
  let workspaceBg: string
  let surfaceBg: string
  let surfaceRaisedBg: string
  let surfaceOverlayBg: string
  let toolbarBg: string
  let fieldBg: string
  let terminalBg: string
  let workspaceGlowColor: string | undefined
  let darkToneSurface:
    | ((
        toneValue: number,
        level: {influence: number; hueWeightFloor: number; hueWeightSpan: number},
      ) => string)
    | undefined

  if (tone === 'light') {
    // Light mode keeps its existing, validated mapping unchanged.
    const tint = LIGHT_TINT_LEVELS
    const roleChroma = (level: {ratio: number; max: number}) =>
      Math.min(level.max, seedChroma * level.ratio) * a
    const navPalette = paletteForHueChroma(primaryHue, roleChroma(tint.nav))
    const secondaryPalette = paletteForHueChroma(primaryHue, roleChroma(tint.secondary))
    const toolbarPalette = paletteForHueChroma(primaryHue, roleChroma(tint.toolbar))
    const workspacePalette = paletteForHueChroma(primaryHue, roleChroma(tint.workspace))
    const surfacePalette = paletteForHueChroma(primaryHue, roleChroma(tint.surface))
    neutralPalette = paletteForHueChroma(neutralHue, Math.min(2, seedChroma * 0.05) * a)
    borderPalette = paletteForHueChroma(neutralVariantHue, roleChroma(tint.border))

    appBg = toneHex(neutralPalette, layout.app)
    sidebarPrimaryBg = toneHex(navPalette, layout.sidebarPrimary)
    sidebarSecondaryBg = toneHex(secondaryPalette, layout.sidebarSecondary)
    workspaceBg = toneHex(workspacePalette, layout.workspace)
    surfaceBg = toneHex(surfacePalette, layout.surface)
    surfaceRaisedBg = toneHex(surfacePalette, layout.surfaceRaised)
    surfaceOverlayBg = toneHex(surfacePalette, layout.surfaceOverlay)
    toolbarBg = toneHex(toolbarPalette, layout.toolbar)
    fieldBg = toneHex(surfacePalette, layout.field)
    terminalBg = toneHex(surfacePalette, Math.min(99, layout.surface + 0.2))
  } else {
    // Dark mode starts from a neutral foundation. The theme hue is a generated
    // chroma budget applied afterwards; no seed hue is used for the base.
    neutralPalette = paletteForHueChroma(0, 0)
    const hueWeight = neutralSeed ? 0 : darkHueWeight(primaryHue)
    const tintTargetChroma = neutralSeed
      ? 0
      : Math.min(DARK_TINT.target.max, seedChroma * DARK_TINT.target.ratio) * a
    const tintPalette = paletteForHueChroma(primaryHue, tintTargetChroma)
    const levelWeight = (level: {hueWeightFloor: number; hueWeightSpan: number}) =>
      clamp(level.hueWeightFloor + level.hueWeightSpan * hueWeight, 0, 1)
    darkToneSurface = (
      toneValue: number,
      level: {
        influence: number
        hueWeightFloor: number
        hueWeightSpan: number
      },
    ) => {
      const neutral = toneHex(neutralPalette, toneValue)
      if (neutralSeed || tintTargetChroma === 0) return neutral
      const influence = level.influence * levelWeight(level)
      if (influence <= 0) return neutral
      return applyTint(neutral, toneHex(tintPalette, toneValue), influence)
    }

    const darkSurface = darkToneSurface
    appBg = toneHex(neutralPalette, layout.app)
    sidebarPrimaryBg = darkSurface(layout.sidebarPrimary, DARK_TINT.nav)
    sidebarSecondaryBg = darkSurface(layout.sidebarSecondary, DARK_TINT.secondary)
    workspaceBg = darkSurface(layout.workspace, DARK_TINT.workspace)
    surfaceBg = darkSurface(layout.surface, DARK_TINT.surface)
    surfaceRaisedBg = darkSurface(layout.surfaceRaised, DARK_TINT.surface)
    surfaceOverlayBg = darkSurface(layout.surfaceOverlay, DARK_TINT.surface)
    toolbarBg = darkSurface(layout.toolbar, DARK_TINT.toolbar)
    fieldBg = darkSurface(layout.field, DARK_TINT.field)
    workspaceGlowColor = darkSurface(layout.workspace + 7, DARK_TINT.glow)
    borderPalette = paletteForHueChroma(
      primaryHue,
      neutralSeed ? 0 : tintTargetChroma * DARK_TINT.border.influence * levelWeight(DARK_TINT.border),
    )
    const terminalChroma = neutralSeed
      ? 0
      : Math.min(DARK_TERMINAL_INK.maxHctChroma, seedChroma * DARK_TERMINAL_INK.ratio) * a
    terminalBg = toneHex(paletteForHueChroma(primaryHue, terminalChroma), 15.2 + 0.4 * s)
  }

  // Ambient gradients are generated from the semantic base surfaces, never
  // from the raw seed. Their stops participate in text contrast checks so the
  // lightest and darkest points of every ramp remain readable.
  const gradients = deriveAmbientGradients({
    tone,
    sidebarPrimaryBg,
    workspaceBg,
  })
  let gradientBackgrounds = [...gradients.sidebarPrimary.stops]
  if (tone === 'light') {
    gradientBackgrounds.push(...gradients.workspace.stops)
  } else {
    gradientBackgrounds.push(workspaceBg)
    if (workspaceGradientStyle === 'corner' && workspaceGlowColor) {
      gradientBackgrounds.push(composite(workspaceGlowColor, workspaceBg, 0.18))
    } else if (workspaceGradientStyle === 'subtle') {
      const base = hexToOklch(workspaceBg)
      const light = oklchToHex({l: clamp(base.l + 0.012, 0, 1), c: base.c, h: base.h})
      const dark = oklchToHex({l: clamp(base.l - 0.008, 0, 1), c: base.c, h: base.h})
      gradientBackgrounds.push(composite(light, workspaceBg, 0.1), composite(dark, workspaceBg, 0.08))
    }
  }

  // Accent first: text contrast is later validated against selected states too.
  const accentPreferred = tone === 'light' ? 42 : 80
  let accentBg = ensureTone(
    accentPalette,
    accentPreferred,
    [workspaceBg, surfaceBg, surfaceRaisedBg, fieldBg, ...gradientBackgrounds],
    3,
    direction,
  )
  let accentHoverBg = ensureTone(
    accentPalette,
    accentPreferred + (tone === 'light' ? -6 : 6),
    [workspaceBg, surfaceBg, surfaceRaisedBg, ...gradientBackgrounds],
    3,
    direction,
  )
  const accentForeground = foregroundOn(accentBg)
  const interactiveActiveBg = toneHex(accentPalette, tone === 'light' ? 90 : 30)
  // If the selected row background swallows the accent marker, push the accent.
  if (contrastRatio(accentBg, interactiveActiveBg) < 3) {
    accentBg = ensureTone(
      accentPalette,
      accentPreferred,
      [workspaceBg, surfaceBg, interactiveActiveBg, ...gradientBackgrounds],
      3,
      direction,
    )
    accentHoverBg = ensureTone(
      accentPalette,
      accentPreferred + (tone === 'light' ? -6 : 6),
      [workspaceBg, surfaceBg, interactiveActiveBg, ...gradientBackgrounds],
      3,
      direction,
    )
  }

  const hoverBg =
    tone === 'light'
      ? toneHex(neutralPalette, layout.workspace - 3.5)
      : darkToneSurface!(layout.workspace + 3.5, DARK_TINT.workspace)
  const hoverStrongBg =
    tone === 'light'
      ? toneHex(neutralPalette, layout.workspace - 6.5)
      : darkToneSurface!(layout.workspace + 6.5, DARK_TINT.workspace)

  const textBackgrounds = [
    appBg,
    workspaceBg,
    surfaceBg,
    surfaceRaisedBg,
    surfaceOverlayBg,
    sidebarPrimaryBg,
    sidebarSecondaryBg,
    toolbarBg,
    fieldBg,
    interactiveActiveBg,
    hoverBg,
    hoverStrongBg,
    ...gradientBackgrounds,
  ]
  const textPrimary = ensureTone(
    neutralPalette,
    tone === 'light' ? 14 : 96,
    textBackgrounds,
    7,
    direction,
  )
  const textSecondary = ensureTone(
    neutralPalette,
    tone === 'light' ? 38 : 82,
    textBackgrounds,
    4.5,
    direction,
  )
  const textMuted = ensureTone(
    neutralPalette,
    tone === 'light' ? 52 : 70,
    textBackgrounds,
    4.5,
    direction,
  )
  const textDisabled = composite(textMuted, surfaceBg, 0.5)

  const borderSubtle = toneHex(borderPalette, tone === 'light' ? 90 : 23.5)
  const borderDefault = toneHex(borderPalette, tone === 'light' ? 82 : 30.0)
  const borderStrong = ensureTone(
    borderPalette,
    tone === 'light' ? 68 : 40.0,
    [workspaceBg, surfaceBg, sidebarPrimaryBg, ...gradientBackgrounds],
    3,
    direction,
  )
  const focusRing = ensureTone(
    accentPalette,
    accentPreferred,
    [workspaceBg, surfaceBg, sidebarPrimaryBg, sidebarSecondaryBg, ...gradientBackgrounds],
    3,
    direction,
  )

  // Status palettes are intentionally independent of the seed hue.
  const successPalette = paletteForHueChroma(145, 54)
  const warningPalette = paletteForHueChroma(78, 72)
  const errorPalette = scheme.errorPalette
  const infoPalette = paletteForHueChroma(250, 60)
  const statusPreferred = tone === 'light' ? 36 : 84
  const statusBackgrounds = [workspaceBg, surfaceBg, surfaceRaisedBg, fieldBg]
  const success = statusStyle(successPalette, statusPreferred, statusBackgrounds, direction, surfaceBg)
  const warning = statusStyle(warningPalette, statusPreferred, statusBackgrounds, direction, surfaceBg)
  const error = statusStyle(errorPalette, statusPreferred, statusBackgrounds, direction, surfaceBg)
  const errorHover = ensureTone(
    errorPalette,
    statusPreferred + (tone === 'light' ? -6 : 6),
    statusBackgrounds,
    3,
    direction,
  )
  const info = statusStyle(infoPalette, statusPreferred, statusBackgrounds, direction, surfaceBg)

  const secondaryPreferred = tone === 'light' ? 46 : 78
  const secondaryBg = ensureTone(
    secondaryAccentPalette,
    secondaryPreferred,
    [workspaceBg, surfaceBg, surfaceRaisedBg, ...gradientBackgrounds],
    3,
    direction,
  )
  const secondaryHoverBg = ensureTone(
    secondaryAccentPalette,
    secondaryPreferred + (tone === 'light' ? -6 : 6),
    [workspaceBg, surfaceBg, surfaceRaisedBg, ...gradientBackgrounds],
    3,
    direction,
  )
  const secondaryForeground = foregroundOn(secondaryBg)

  return {
    seed,
    secondarySeed,
    tone,
    seedChroma: source.chroma,
    seedHue: source.hue,
    separation,
    atmosphere,
    workspaceGradientStyle,
    workspaceGlowColor,
    appBg,
    sidebarPrimaryBg,
    sidebarSecondaryBg,
    workspaceBg,
    surfaceBg,
    surfaceRaisedBg,
    surfaceOverlayBg,
    toolbarBg,
    fieldBg,
    hoverBg,
    hoverStrongBg,
    interactiveHoverBg: tone === 'light' ? 'rgba(9, 12, 16, 0.055)' : 'rgba(255, 255, 255, 0.06)',
    interactiveActiveBg,
    interactivePressedBg: tone === 'light' ? 'rgba(9, 12, 16, 0.1)' : 'rgba(255, 255, 255, 0.11)',
    interactiveDisabledBg: tone === 'light' ? 'rgba(9, 12, 16, 0.035)' : 'rgba(255, 255, 255, 0.04)',
    focusRing,
    textPrimary,
    textSecondary,
    textMuted,
    textDisabled,
    textOnAccent: accentForeground,
    borderSubtle,
    borderDefault,
    borderStrong,
    accentBg,
    accentHoverBg,
    accentForeground,
    accentWeakBg: interactiveActiveBg,
    accentBorder: rgba(accentBg, 0.3),
    accentStrongBorder: rgba(accentBg, 0.52),
    secondaryBg,
    secondaryHoverBg,
    secondaryForeground,
    statusSuccess: success.color,
    statusSuccessBg: success.bg,
    statusSuccessBorder: success.border,
    statusWarning: warning.color,
    statusWarningBg: warning.bg,
    statusWarningBorder: warning.border,
    statusError: error.color,
    statusErrorHover: errorHover,
    statusErrorBg: error.bg,
    statusErrorBorder: error.border,
    statusInfo: info.color,
    statusInfoBg: info.bg,
    statusInfoBorder: info.border,
    terminalBg,
    gradients,
    // Chroma is intentionally kept low on large regions to avoid eye fatigue.
    // Note: `s` participates through toneLayout; `a` through every tint.
    // Gradients are a third dimension: they never alter the base surfaces.
  }
}

// ---------------------------------------------------------------------------
// Material planning
// ---------------------------------------------------------------------------

/** Coarse platform detection; the Go side still owns native window setup. */
export function detectPlatform(): ThemePlatform {
  if (typeof navigator === 'undefined') return 'web'
  const ua = navigator.userAgent
  if (/Macintosh|Mac OS X/i.test(ua)) return 'macos'
  if (/Windows/i.test(ua)) return 'windows'
  if (/Linux/i.test(ua)) return 'linux'
  return 'web'
}

/**
 * Native desktop backdrops are only available inside Wails on macOS/Windows.
 * A plain browser (vite dev) or Linux compositor must not receive translucent
 * surfaces that could expose an arbitrary desktop wallpaper.
 */
export function detectCapabilities(): ThemeCapabilities {
  const platform = detectPlatform()
  const wails =
    typeof window !== 'undefined' && !!(window as unknown as {runtime?: unknown}).runtime
  return {
    platform,
    wails,
    nativeBackdrop: wails && (platform === 'macos' || platform === 'windows'),
  }
}

const SOFT_ALPHA = {
  sidebarPrimary: 0.86,
  sidebarSecondary: 0.9,
  workspace: 0.88,
  surface: 0.94,
  surfaceRaised: 0.96,
  surfaceOverlay: 0.98,
  toolbar: 0.92,
  field: 0.985,
} as const

const CLEAR_ALPHA = {
  sidebarPrimary: 0.72,
  sidebarSecondary: 0.78,
  workspace: 0.74,
  surface: 0.88,
  surfaceRaised: 0.91,
  surfaceOverlay: 0.95,
  toolbar: 0.82,
  field: 0.97,
} as const

export function planMaterial(options: MaterialPlanOptions): MaterialPlan {
  const requestedMode = options.mode
  const platform = options.platform ?? detectPlatform()
  const surfaceOpacity = clamp(options.surfaceOpacity ?? 85, 60, 100)
  const nativeBackdrop = options.nativeBackdrop
  const unavailable = requestedMode !== 'solid' && !nativeBackdrop
  const mode: MaterialMode = unavailable ? 'solid' : requestedMode
  const fallback: MaterialFallback = unavailable
    ? 'unsupported-platform'
    : requestedMode === 'solid'
      ? 'opaque'
      : 'none'
  if (mode === 'solid') {
    return {
      requestedMode,
      mode,
      fallback,
      nativeBackdrop,
      platform,
      alpha: {
        sidebarPrimary: 1,
        sidebarSecondary: 1,
        workspace: 1,
        surface: 1,
        surfaceRaised: 1,
        surfaceOverlay: 1,
        toolbar: 1,
        field: 1,
      },
      shellBlurPx: 0,
      overlayBlurPx: 0,
      surfaceOpacity,
    }
  }

  const base = mode === 'clear' ? CLEAR_ALPHA : SOFT_ALPHA
  const factor = surfaceOpacity / 85
  const alpha = (value: number, floor: number) => clamp(value * factor, floor, 0.995)
  return {
    requestedMode,
    mode,
    fallback,
    nativeBackdrop,
    platform,
    alpha: {
      sidebarPrimary: alpha(base.sidebarPrimary, 0.6),
      sidebarSecondary: alpha(base.sidebarSecondary, 0.62),
      workspace: alpha(base.workspace, 0.62),
      surface: alpha(base.surface, 0.78),
      surfaceRaised: alpha(base.surfaceRaised, 0.8),
      surfaceOverlay: alpha(base.surfaceOverlay, 0.86),
      toolbar: alpha(base.toolbar, 0.68),
      field: alpha(base.field, 0.9),
    },
    shellBlurPx: 0,
    overlayBlurPx: clamp(options.blurPx ?? 24, 0, 60),
    surfaceOpacity,
  }
}

// ---------------------------------------------------------------------------
// CSS token mapping
// ---------------------------------------------------------------------------

function surface(hex: string, alpha: number): string {
  return alpha >= 0.999 ? hex : rgba(hex, alpha)
}

function gradientLayer(ramp: GradientRamp, alpha: number): string {
  const stops = ramp.stops.map((stop, index) => {
    const position = Math.round((index / (ramp.stops.length - 1)) * 100)
    return `${surface(stop, alpha)} ${position}%`
  })
  return `linear-gradient(${ramp.angle}deg, ${stops.join(', ')})`
}

function gradientEdge(ramp: GradientRamp, alpha: number, edge: 'start' | 'end'): string {
  return surface(edge === 'start' ? ramp.stops[0] : ramp.stops[ramp.stops.length - 1], alpha)
}

/**
 * Maps roles + material plan onto the public semantic token set, plus the
 * compatibility aliases still consumed by older components.
 */
export function rolesToCssVars(roles: ColorRoles, plan: MaterialPlan): Record<string, string> {
  const {alpha} = plan
  const light = roles.tone === 'light'
  const sidebarPrimary = surface(roles.sidebarPrimaryBg, alpha.sidebarPrimary)
  const sidebarSecondary = surface(roles.sidebarSecondaryBg, alpha.sidebarSecondary)
  const workspace = surface(roles.workspaceBg, alpha.workspace)
  const surfaceBg = surface(roles.surfaceBg, alpha.surface)
  const surfaceRaised = surface(roles.surfaceRaisedBg, alpha.surfaceRaised)
  const surfaceOverlay = surface(roles.surfaceOverlayBg, alpha.surfaceOverlay)
  const toolbar = surface(roles.toolbarBg, alpha.toolbar)
  const field = surface(roles.fieldBg, alpha.field)
  const appBg = plan.mode === 'solid' ? roles.appBg : 'transparent'

  // Light keeps the existing opaque ramp. Dark uses a neutral base plus a very
  // low-alpha light overlay; the base is composed as the final background
  // layer so Solid stays opaque and Glass keeps its own region alpha.
  let sidebarPrimaryGradient: string
  let sidebarPrimaryStart: string
  let sidebarPrimaryEnd: string
  let workspaceGradient: string
  let workspaceStart: string
  let workspaceEnd: string

  if (light) {
    sidebarPrimaryGradient = gradientLayer(roles.gradients.sidebarPrimary, alpha.sidebarPrimary)
    sidebarPrimaryStart = gradientEdge(roles.gradients.sidebarPrimary, alpha.sidebarPrimary, 'start')
    sidebarPrimaryEnd = gradientEdge(roles.gradients.sidebarPrimary, alpha.sidebarPrimary, 'end')
    workspaceGradient = gradientLayer(roles.gradients.workspace, alpha.workspace)
    workspaceStart = gradientEdge(roles.gradients.workspace, alpha.workspace, 'start')
    workspaceEnd = gradientEdge(roles.gradients.workspace, alpha.workspace, 'end')
  } else {
    const navBase = surface(roles.sidebarPrimaryBg, alpha.sidebarPrimary)
    const navOklch = hexToOklch(roles.sidebarPrimaryBg)
    const navLight = oklchToHex({
      l: clamp(navOklch.l + 0.01, 0, 1),
      c: navOklch.c,
      h: navOklch.h,
    })
    const navDark = oklchToHex({
      l: clamp(navOklch.l - 0.008, 0, 1),
      c: navOklch.c,
      h: navOklch.h,
    })
    sidebarPrimaryGradient = `linear-gradient(180deg, ${surface(navLight, 0.1)} 0%, transparent 55%, ${surface(navDark, 0.08)} 100%), ${navBase}`
    sidebarPrimaryStart = surface(navLight, alpha.sidebarPrimary)
    sidebarPrimaryEnd = surface(navDark, alpha.sidebarPrimary)

    const workspaceBase = surface(roles.workspaceBg, alpha.workspace)
    workspaceStart = workspaceBase
    workspaceEnd = workspaceBase
    if (roles.workspaceGradientStyle === 'solid') {
      // A plain colour in the same slot: no visual gradient, same fallback path.
      workspaceGradient = workspaceBase
    } else if (roles.workspaceGradientStyle === 'corner') {
      const glow = roles.workspaceGlowColor ?? roles.workspaceBg
      workspaceStart = surface(glow, alpha.workspace)
      workspaceGradient = `radial-gradient(ellipse 65% 45% at 8% 0%, ${surface(glow, 0.18)} 0%, ${surface(glow, 0.05)} 32%, transparent 62%), ${workspaceBase}`
    } else {
      const base = hexToOklch(roles.workspaceBg)
      const light = oklchToHex({l: clamp(base.l + 0.012, 0, 1), c: base.c, h: base.h})
      const dark = oklchToHex({l: clamp(base.l - 0.008, 0, 1), c: base.c, h: base.h})
      workspaceGradient = `linear-gradient(135deg, ${surface(light, 0.1)} 0%, transparent 48%, ${surface(dark, 0.08)} 100%), ${workspaceBase}`
      workspaceStart = surface(light, alpha.workspace)
      workspaceEnd = surface(dark, alpha.workspace)
    }
  }

  return {
    // Semantic surface roles.
    '--app-bg': appBg,
    '--sidebar-primary-bg': sidebarPrimary,
    '--sidebar-secondary-bg': sidebarSecondary,
    '--workspace-bg': workspace,
    '--surface-bg': surfaceBg,
    '--surface-raised-bg': surfaceRaised,
    '--surface-overlay-bg': surfaceOverlay,
    '--toolbar-bg': toolbar,

    // Ambient gradient roles. Base surfaces and gradient ramps stay separate:
    // removing a gradient token makes the component fall back to its base bg.
    '--sidebar-primary-gradient': sidebarPrimaryGradient,
    '--sidebar-primary-gradient-start': sidebarPrimaryStart,
    '--sidebar-primary-gradient-end': sidebarPrimaryEnd,
    '--workspace-gradient': workspaceGradient,
    '--workspace-gradient-start': workspaceStart,
    '--workspace-gradient-end': workspaceEnd,

    // Interaction roles.
    '--interactive-hover-bg': roles.interactiveHoverBg,
    '--interactive-active-bg': roles.interactiveActiveBg,
    '--interactive-pressed-bg': roles.interactivePressedBg,
    '--interactive-disabled-bg': roles.interactiveDisabledBg,
    '--focus-ring-color': roles.focusRing,

    // Text roles.
    '--text-primary': roles.textPrimary,
    '--text-secondary': roles.textSecondary,
    '--text-muted': roles.textMuted,
    '--text-disabled': roles.textDisabled,
    '--text-on-accent': roles.textOnAccent,

    // Border roles.
    '--border-subtle': roles.borderSubtle,
    '--border-default': roles.borderDefault,
    '--border-strong': roles.borderStrong,

    // Accent roles.
    '--accent-bg': roles.accentBg,
    '--accent-hover-bg': roles.accentHoverBg,
    '--accent-foreground': roles.accentForeground,
    '--accent-weak-bg': roles.accentWeakBg,
    '--accent-border': roles.accentBorder,
    '--accent-strong-border': roles.accentStrongBorder,

    // Status roles.
    '--status-success': roles.statusSuccess,
    '--status-success-bg': roles.statusSuccessBg,
    '--status-success-border': roles.statusSuccessBorder,
    '--status-warning': roles.statusWarning,
    '--status-warning-bg': roles.statusWarningBg,
    '--status-warning-border': roles.statusWarningBorder,
    '--status-error': roles.statusError,
    '--status-error-bg': roles.statusErrorBg,
    '--status-error-border': roles.statusErrorBorder,
    '--status-info': roles.statusInfo,
    '--status-info-bg': roles.statusInfoBg,
    '--status-info-border': roles.statusInfoBorder,

    // Protected terminal surface.
    '--terminal-bg': roles.terminalBg,

    // Material descriptions consumed by the CSS material layer.
    '--glass-shell-blur': `${plan.shellBlurPx}px`,
    '--glass-overlay-blur': `${plan.overlayBlurPx}px`,
    '--glass-opacity': `${Math.round(alpha.workspace * 100)}%`,
    '--glass-tint': rgba(roles.sidebarPrimaryBg, 0.2),

    // ---------- Compatibility layer (old token names) ----------
    '--background': workspace,
    '--surface-1': sidebarSecondary,
    '--surface-2': surfaceBg,
    '--surface-3': surfaceRaised,
    '--surface-4': surfaceOverlay,
    '--border': roles.borderDefault,
    '--border-focus': roles.focusRing,
    '--foreground': roles.textPrimary,
    '--foreground-secondary': roles.textSecondary,
    '--muted-foreground': roles.textMuted,
    '--primary': roles.accentBg,
    '--primary-foreground': roles.accentForeground,
    '--primary-hover': roles.accentHoverBg,
    '--primary-weak': roles.accentWeakBg,
    '--primary-border': roles.accentBorder,
    '--primary-glow': rgba(roles.accentBg, 0.32),
    '--primary-glow-soft': rgba(roles.accentBg, 0.15),
    '--primary-strong-border': roles.accentStrongBorder,
    '--primary-hover-bg': rgba(roles.accentBg, 0.4),
    '--secondary': roles.secondaryBg,
    '--secondary-hover': roles.secondaryHoverBg,
    '--secondary-foreground': roles.secondaryForeground,
    '--secondary-glow': rgba(roles.secondaryBg, 0.28),
    '--secondary-glow-soft': rgba(roles.secondaryBg, 0.09),
    '--secondary-weak': rgba(roles.secondaryBg, 0.12),
    '--secondary-border': rgba(roles.secondaryBg, 0.25),
    '--secondary-hover-bg': rgba(roles.secondaryBg, 0.4),
    '--success': roles.statusSuccess,
    '--warning': roles.statusWarning,
    '--destructive': roles.statusError,
    '--destructive-hover': roles.statusErrorHover,
    '--destructive-foreground': foregroundOn(roles.statusError),
    '--destructive-weak': roles.statusErrorBg,
    '--destructive-weak-hover': composite(roles.statusError, roles.surfaceBg, 0.18),
    '--destructive-border': roles.statusErrorBorder,
    '--success-border': roles.statusSuccessBorder,
    '--info': roles.statusInfo,
    '--hover': roles.hoverBg,
    '--hover-strong': roles.hoverStrongBg,
    '--field-bg': field,
    '--field-border': roles.borderDefault,
    '--overlay': light ? 'rgba(18, 21, 26, 0.38)' : 'rgba(2, 4, 6, 0.62)',
    '--selection': rgba(roles.accentBg, 0.26),
    '--inset-shadow': light ? 'rgba(9, 12, 16, 0.08)' : 'rgba(0, 0, 0, 0.5)',
    '--shadow-pop': light
      ? '0 1px 2px rgb(9 12 16 / 8%), 0 12px 32px rgb(9 12 16 / 16%)'
      : '0 1px 2px rgb(0 0 0 / 40%), 0 12px 32px rgb(0 0 0 / 50%)',
    '--shadow-side-panel': light ? '16px 0 40px rgb(9 12 16 / 16%)' : '16px 0 40px rgb(0 0 0 / 28%)',
    '--terminal-text-shadow': light
      ? '0 1px 3px rgb(0 0 0 / 72%), 0 0 var(--terminal-shadow-blur, 3px) rgb(0 0 0 / 45%)'
      : '0 1px 3px rgb(0 0 0 / 80%), 0 0 var(--terminal-shadow-blur, 3px) rgb(0 0 0 / 50%)',
    '--control-thumb': '#ffffff',
    '--preview-border': light ? 'rgb(9 12 16 / 10%)' : 'rgb(255 255 255 / 12%)',
    '--preview-border-strong': light ? 'rgb(9 12 16 / 16%)' : 'rgb(255 255 255 / 18%)',

    // Old shell-region aliases kept for the migration period.
    '--theme-bg-main': workspace,
    '--theme-bg-sidebar': sidebarPrimary,
    '--theme-bg-toolbar': toolbar,
    '--theme-bg-surface': surfaceBg,
    '--theme-glass-opacity': `${Math.round(alpha.workspace * 100)}%`,
    '--theme-glass-blur': `${plan.overlayBlurPx}px`,
    '--theme-glass-tint': rgba(roles.sidebarPrimaryBg, 0.2),
  }
}

/** All keys the engine may write to `:root`; used to clean up on unmount. */
export const MANAGED_ROOT_VARS = [
  '--app-bg',
  '--sidebar-primary-bg',
  '--sidebar-secondary-bg',
  '--workspace-bg',
  '--surface-bg',
  '--surface-raised-bg',
  '--surface-overlay-bg',
  '--toolbar-bg',
  '--sidebar-primary-gradient',
  '--sidebar-primary-gradient-start',
  '--sidebar-primary-gradient-end',
  '--workspace-gradient',
  '--workspace-gradient-start',
  '--workspace-gradient-end',
  '--interactive-hover-bg',
  '--interactive-active-bg',
  '--interactive-pressed-bg',
  '--interactive-disabled-bg',
  '--focus-ring-color',
  '--text-primary',
  '--text-secondary',
  '--text-muted',
  '--text-disabled',
  '--text-on-accent',
  '--border-subtle',
  '--border-default',
  '--border-strong',
  '--accent-bg',
  '--accent-hover-bg',
  '--accent-foreground',
  '--accent-weak-bg',
  '--accent-border',
  '--accent-strong-border',
  '--status-success',
  '--status-success-bg',
  '--status-success-border',
  '--status-warning',
  '--status-warning-bg',
  '--status-warning-border',
  '--status-error',
  '--status-error-bg',
  '--status-error-border',
  '--status-info',
  '--status-info-bg',
  '--status-info-border',
  '--terminal-bg',
  '--glass-shell-blur',
  '--glass-overlay-blur',
  '--glass-opacity',
  '--glass-tint',
  '--background',
  '--surface-1',
  '--surface-2',
  '--surface-3',
  '--surface-4',
  '--border',
  '--border-focus',
  '--foreground',
  '--foreground-secondary',
  '--muted-foreground',
  '--primary',
  '--primary-foreground',
  '--primary-hover',
  '--primary-weak',
  '--primary-border',
  '--primary-glow',
  '--primary-glow-soft',
  '--primary-strong-border',
  '--primary-hover-bg',
  '--secondary',
  '--secondary-hover',
  '--secondary-foreground',
  '--secondary-glow',
  '--secondary-glow-soft',
  '--secondary-weak',
  '--secondary-border',
  '--secondary-hover-bg',
  '--success',
  '--warning',
  '--destructive',
  '--destructive-hover',
  '--destructive-foreground',
  '--destructive-weak',
  '--destructive-weak-hover',
  '--destructive-border',
  '--success-border',
  '--info',
  '--hover',
  '--hover-strong',
  '--field-bg',
  '--field-border',
  '--overlay',
  '--selection',
  '--inset-shadow',
  '--shadow-pop',
  '--shadow-side-panel',
  '--terminal-text-shadow',
  '--control-thumb',
  '--preview-border',
  '--preview-border-strong',
  '--theme-bg-main',
  '--theme-bg-sidebar',
  '--theme-bg-toolbar',
  '--theme-bg-surface',
  '--theme-glass-opacity',
  '--theme-glass-blur',
  '--theme-glass-tint',
] as const
