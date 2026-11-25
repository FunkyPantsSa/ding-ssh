export type ThemeTone = 'light' | 'dark'
export type MaterialMode = 'solid' | 'soft' | 'clear'
export type ThemePlatform = 'macos' | 'windows' | 'linux' | 'web'
export type MaterialFallback = 'none' | 'unsupported-platform' | 'opaque'

export const DEFAULT_SEED = '#71b5d2'

/** Neutral seeds (black/white/gray) must not invent a colorful accent. */
export const NEUTRAL_SEED_CHROMA = 2.5

export interface ThemeCapabilities {
  platform: ThemePlatform
  /** Wails runtime is present (production WebView, not a plain browser). */
  wails: boolean
  /** The native window actually provides a desktop backdrop on this platform. */
  nativeBackdrop: boolean
  /** Whether the user/system requested reduced transparency. */
  reducedTransparency?: boolean
  /** Whether the user/system requested reduced motion. */
  reducedMotion?: boolean
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
  terminalFg: string

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
  /** Arc Space preset id (e.g. "arc-velvet") — written to html[data-preset] */
  presetId?: string
}

export interface Rgb {
  r: number
  g: number
  b: number
}

export interface Oklch {
  /** 0–1 perceptual lightness. */
  l: number
  /** Chroma; 0 is neutral. */
  c: number
  /** Hue in degrees, 0–360. */
  h: number
}
