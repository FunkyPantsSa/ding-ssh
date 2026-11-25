import {Hct, argbFromHex} from '@material/material-color-utilities'
import {
  arcBaseBackground,
  clamp,
  composite,
  hexToOklch,
  oklchToHex,
  rgba,
} from './color'
import {foregroundOn, paletteForHueChroma, toneHex} from './palette'
import {
  type ColorRoles,
  type GradientRamp,
  type MaterialPlan,
  NEUTRAL_SEED_CHROMA,
} from './types'

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

  let sidebarPrimaryGradient: string
  let sidebarPrimaryStart: string
  let sidebarPrimaryEnd: string
  let workspaceGradient: string
  let workspaceStart: string
  let workspaceEnd: string

  const atmo = clamp((roles.atmosphere ?? 75) / 100, 0.2, 1)

  let navTop: string
  let navMid: string
  let navBottom: string

  if (roles.seedChroma < NEUTRAL_SEED_CHROMA) {
    if (light) {
      navTop = '#f8fafc'
      navMid = '#f1f5f9'
      navBottom = '#e2e8f0'
    } else {
      navTop = '#22252a'
      navMid = '#1a1d22'
      navBottom = '#141619'
    }
  } else {
    const primaryPalette = paletteForHueChroma(roles.seedHue, Math.max(roles.seedChroma, 35))
    const secondarySource = Hct.fromInt(argbFromHex(roles.secondarySeed || roles.seed))
    const secondaryPalette = paletteForHueChroma(secondarySource.hue, Math.max(secondarySource.chroma, 30))

    if (light) {
      navTop = toneHex(primaryPalette, Math.round(clamp(82 - atmo * 4, 76, 86)))
      navMid = toneHex(primaryPalette, Math.round(clamp(88 - atmo * 3, 84, 91)))
      navBottom = toneHex(secondaryPalette, Math.round(clamp(93 - atmo * 2, 90, 96)))
    } else {
      navTop = toneHex(primaryPalette, Math.round(clamp(22 + atmo * 3, 18, 28)))
      navMid = toneHex(primaryPalette, Math.round(clamp(16 + atmo * 2, 13, 19)))
      navBottom = toneHex(secondaryPalette, Math.round(clamp(11 + atmo * 2, 9, 14)))
    }
  }

  sidebarPrimaryGradient = `linear-gradient(180deg, ${surface(navTop, alpha.sidebarPrimary)} 0%, ${surface(navMid, alpha.sidebarPrimary)} 50%, ${surface(navBottom, alpha.sidebarPrimary)} 100%)`
  sidebarPrimaryStart = surface(navTop, alpha.sidebarPrimary)
  sidebarPrimaryEnd = surface(navBottom, alpha.sidebarPrimary)

  if (light) {
    workspaceGradient = gradientLayer(roles.gradients.workspace, alpha.workspace)
    workspaceStart = gradientEdge(roles.gradients.workspace, alpha.workspace, 'start')
    workspaceEnd = gradientEdge(roles.gradients.workspace, alpha.workspace, 'end')
  } else {
    const workspaceBase = surface(roles.workspaceBg, alpha.workspace)
    workspaceStart = workspaceBase
    workspaceEnd = workspaceBase
    if (roles.workspaceGradientStyle === 'solid') {
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

  const arcBase = arcBaseBackground(roles.seedHue, roles.seedChroma, light, roles.atmosphere)

  let primaryGlow: string
  let secondaryGlow: string
  if (roles.seedChroma < NEUTRAL_SEED_CHROMA) {
    primaryGlow = light ? '#e2e8f0' : '#27272a'
    secondaryGlow = light ? '#cbd5e1' : '#1e1e24'
  } else {
    const primaryPalette = paletteForHueChroma(roles.seedHue, Math.max(roles.seedChroma, 35))
    primaryGlow = toneHex(primaryPalette, light ? 68 : 65)
    const secondarySource = Hct.fromInt(argbFromHex(roles.secondarySeed || roles.seed))
    const secondaryPalette = paletteForHueChroma(secondarySource.hue, Math.max(secondarySource.chroma, 30))
    secondaryGlow = toneHex(secondaryPalette, light ? 70 : 60)
  }

  const lightGlow1 = rgba(primaryGlow, clamp(0.36 + 0.22 * atmo, 0.25, 0.60))
  const lightGlow2 = rgba(secondaryGlow, clamp(0.30 + 0.20 * atmo, 0.20, 0.52))
  const lightGlow3 = rgba(primaryGlow, clamp(0.18 + 0.12 * atmo, 0.10, 0.32))

  const darkGlow1 = rgba(primaryGlow, clamp(0.26 + 0.18 * atmo, 0.16, 0.46))
  const darkGlow2 = rgba(secondaryGlow, clamp(0.20 + 0.15 * atmo, 0.12, 0.36))

  const shellBase = plan.mode === 'solid' ? arcBase : rgba(arcBase, clamp(0.18 + 0.12 * atmo, 0.10, 0.32))
  const arcAmbientGradient = light
    ? `radial-gradient(ellipse 75% 65% at 5% 10%, ${lightGlow1} 0%, transparent 55%), radial-gradient(ellipse 75% 65% at 95% 90%, ${lightGlow2} 0%, transparent 60%), radial-gradient(circle at 50% 50%, ${lightGlow3} 0%, transparent 50%), ${shellBase}`
    : `radial-gradient(ellipse 65% 55% at 5% 15%, ${darkGlow1} 0%, transparent 55%), radial-gradient(ellipse 70% 60% at 95% 85%, ${darkGlow2} 0%, transparent 60%), ${shellBase}`

  const arcAmbientBg = light
    ? `radial-gradient(ellipse 75% 65% at 5% 10%, ${lightGlow1} 0%, transparent 55%), radial-gradient(ellipse 75% 65% at 95% 90%, ${lightGlow2} 0%, transparent 60%), radial-gradient(circle at 50% 50%, ${lightGlow3} 0%, transparent 50%), ${shellBase}`
    : `radial-gradient(circle at 10% 20%, ${darkGlow1} 0%, transparent 40%), radial-gradient(circle at 90% 80%, ${darkGlow2} 0%, transparent 45%), ${shellBase}`

  return {
    // Semantic surface roles.
    '--app-bg': plan.mode === 'solid' ? arcBase : 'transparent',
    '--sidebar-primary-bg': sidebarPrimary,
    '--sidebar-secondary-bg': sidebarSecondary,
    '--workspace-bg': workspace,
    '--surface-bg': surfaceBg,
    '--surface-raised-bg': surfaceRaised,
    '--surface-overlay-bg': surfaceOverlay,
    '--toolbar-bg': toolbar,

    // Ambient gradient roles.
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

    // Protected terminal surface & inset canvas roles.
    '--terminal-bg': roles.terminalBg,
    '--terminal-fg': roles.terminalFg,
    '--terminal-border': light
      ? '1px solid color-mix(in srgb, var(--accent-bg) 20%, rgba(0, 0, 0, 0.07))'
      : '1px solid color-mix(in srgb, var(--accent-bg) 14%, rgba(255, 255, 255, 0.06))',
    '--terminal-shadow': light
      ? '0 4px 16px rgba(0, 0, 0, 0.05), 0 1px 2px rgba(0, 0, 0, 0.04)'
      : 'inset 0 1px 0 rgba(255, 255, 255, 0.04), 0 8px 24px rgba(0, 0, 0, 0.26)',
    '--terminal-glow': light
      ? `radial-gradient(ellipse 65% 55% at 0% 0%, ${rgba(roles.accentBg, clamp(0.08 + 0.06 * atmo, 0.08, 0.14))} 0%, transparent 70%)`
      : `radial-gradient(ellipse 65% 55% at 0% 0%, ${rgba(roles.accentBg, clamp(0.10 + 0.08 * atmo, 0.10, 0.18))} 0%, transparent 70%)`,

    // Material descriptions consumed by the CSS material layer.
    '--glass-shell-blur': `${plan.shellBlurPx}px`,
    '--glass-overlay-blur': `${plan.overlayBlurPx}px`,
    '--glass-opacity': `${Math.round(alpha.workspace * 100)}%`,
    '--glass-tint': rgba(roles.sidebarPrimaryBg, 0.2),

    // Arc color system & Ambient glow roles
    '--arc-ambient-gradient': arcAmbientGradient,
    '--arc-ambient-bg': arcAmbientBg,
    '--arc-active-tab-gradient': `linear-gradient(135deg, ${rgba(primaryGlow, 0.28)}, ${rgba(secondaryGlow, 0.14)})`,
    '--arc-active-tab-border': rgba(primaryGlow, 0.45),
    '--glass-border': light ? '1px solid rgba(0, 0, 0, 0.08)' : '1px solid rgba(255, 255, 255, 0.09)',
    '--glass-border-subtle': light ? '1px solid rgba(0, 0, 0, 0.04)' : '1px solid rgba(255, 255, 255, 0.05)',
    '--glass-border-strong': light ? '1px solid rgba(0, 0, 0, 0.14)' : '1px solid rgba(255, 255, 255, 0.18)',
    '--glass-highlight': light ? 'none' : 'inset 0 1px 0 rgba(255, 255, 255, 0.12)',

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

    // Differentiated shadows: lighter blur, subtle edge for light; deep blur for dark
    '--shadow-sm': light ? '0 1px 2px 0 rgb(0 0 0 / 0.05)' : '0 1px 2px 0 rgb(0 0 0 / 0.25)',
    '--shadow-md': light
      ? '0 4px 12px -2px rgb(0 0 0 / 0.08), 0 2px 6px -1px rgb(0 0 0 / 0.04)'
      : '0 4px 6px -1px rgb(0 0 0 / 0.3), 0 2px 4px -2px rgb(0 0 0 / 0.24)',
    '--shadow-lg': light
      ? '0 12px 24px -4px rgb(0 0 0 / 0.10), 0 4px 10px -2px rgb(0 0 0 / 0.05)'
      : '0 10px 15px -3px rgb(0 0 0 / 0.35), 0 4px 6px -4px rgb(0 0 0 / 0.28)',
    '--shadow-overlay': light ? '0 8px 24px -4px rgb(9 12 16 / 12%)' : '0 8px 24px rgb(0 0 0 / 30%)',
    '--shadow-canvas': light
      ? '0 12px 32px -8px rgba(0, 0, 0, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.8)'
      : '0 16px 40px -8px rgba(0, 0, 0, 0.4), inset 0 1px 0 rgba(255, 255, 255, 0.12)',
    '--shadow-floating': light
      ? '0 16px 36px -10px rgba(0, 0, 0, 0.12), inset 0 1px 0 rgba(255, 255, 255, 0.9)'
      : '0 20px 48px -10px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.15)',
    '--shadow-pop': light
      ? '0 1px 3px rgb(9 12 16 / 6%), 0 10px 24px rgb(9 12 16 / 10%)'
      : '0 1px 2px rgb(0 0 0 / 40%), 0 12px 32px rgb(0 0 0 / 50%)',
    '--shadow-side-panel': light ? '12px 0 32px rgb(9 12 16 / 8%)' : '16px 0 40px rgb(0 0 0 / 28%)',
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
  '--secondary-bg',
  '--secondary-hover-bg',
  '--secondary-foreground',
  '--status-success',
  '--status-success-bg',
  '--status-success-border',
  '--status-warning',
  '--status-warning-bg',
  '--status-warning-border',
  '--status-error',
  '--status-error-hover',
  '--status-error-bg',
  '--status-error-border',
  '--status-info',
  '--status-info-bg',
  '--status-info-border',
  '--terminal-bg',
  '--terminal-fg',
  '--terminal-border',
  '--terminal-shadow',
  '--terminal-glow',
  '--glass-shell-blur',
  '--glass-overlay-blur',
  '--glass-opacity',
  '--glass-tint',
  '--arc-ambient-gradient',
  '--arc-ambient-bg',
  '--arc-active-tab-gradient',
  '--arc-active-tab-border',
  '--glass-border',
  '--glass-border-subtle',
  '--glass-border-strong',
  '--glass-highlight',
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
  '--shadow-sm',
  '--shadow-md',
  '--shadow-lg',
  '--shadow-overlay',
  '--shadow-canvas',
  '--shadow-floating',
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
