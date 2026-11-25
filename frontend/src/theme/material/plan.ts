import {isDesktop} from '../../services/desktop-environment'
import {clamp} from './color'
import type {
  MaterialFallback,
  MaterialMode,
  MaterialPlan,
  MaterialPlanOptions,
  ThemeCapabilities,
  ThemePlatform,
} from './types'

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
 *
 * Honors prefers-reduced-transparency to fall back to solid when requested.
 */
export function detectCapabilities(): ThemeCapabilities {
  const platform = detectPlatform()
  const wails = typeof window !== 'undefined' && isDesktop()
  const reducedTransparency =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-transparency: reduce)')?.matches === true
  const reducedMotion =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches === true

  return {
    platform,
    wails,
    nativeBackdrop: wails && (platform === 'macos' || platform === 'windows') && !reducedTransparency,
    reducedTransparency,
    reducedMotion,
  }
}

export const SOFT_ALPHA = {
  sidebarPrimary: 0.65,
  sidebarSecondary: 0.70,
  workspace: 0.68,
  surface: 0.78,
  surfaceRaised: 0.82,
  surfaceOverlay: 0.88,
  toolbar: 0.72,
  field: 0.90,
} as const

export const CLEAR_ALPHA = {
  sidebarPrimary: 0.46,
  sidebarSecondary: 0.52,
  workspace: 0.48,
  surface: 0.62,
  surfaceRaised: 0.66,
  surfaceOverlay: 0.76,
  toolbar: 0.56,
  field: 0.82,
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
      sidebarPrimary: alpha(base.sidebarPrimary, 0.32),
      sidebarSecondary: alpha(base.sidebarSecondary, 0.36),
      workspace: alpha(base.workspace, 0.36),
      surface: alpha(base.surface, 0.45),
      surfaceRaised: alpha(base.surfaceRaised, 0.50),
      surfaceOverlay: alpha(base.surfaceOverlay, 0.60),
      toolbar: alpha(base.toolbar, 0.42),
      field: alpha(base.field, 0.68),
    },
    shellBlurPx: clamp(options.blurPx ?? 24, 0, 60),
    overlayBlurPx: clamp(options.blurPx ?? 24, 0, 60),
    surfaceOpacity,
  }
}
