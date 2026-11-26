import {Hct, SchemeTonalSpot, type TonalPalette, argbFromHex, hexFromArgb} from '@material/material-color-utilities'
import {clamp, composite, contrastRatio, hexToOklch, normalizeSeed, oklchToHex, rgba} from './color'
import {
  DARK_TERMINAL_MAX_NEIGHBOUR_CHROMA,
  DARK_TINT,
  LIGHT_TINT_LEVELS,
  applyTint,
  darkHueWeight,
  deriveAmbientGradients,
  ensureTone,
  foregroundOn,
  paletteForHueChroma,
  statusStyle,
  toneHex,
  toneLayout,
} from './palette'
import {
  type ColorRoleOptions,
  type ColorRoles,
  NEUTRAL_SEED_CHROMA,
  type WorkspaceGradientStyle,
} from './types'

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
  const neutralSeed =
    source.chroma < NEUTRAL_SEED_CHROMA || source.tone <= 0.5 || source.tone >= 99.5

  const seedChroma = neutralSeed ? 0 : source.chroma
  const primaryHue = scheme.primaryPalette.hue
  const tertiaryHue = scheme.tertiaryPalette.hue
  const neutralHue = scheme.neutralPalette.hue
  const neutralVariantHue = scheme.neutralVariantPalette.hue

  const accentChroma = neutralSeed ? 0 : clamp(seedChroma * 0.9, 42, 72)
  const accentPalette = paletteForHueChroma(primaryHue, accentChroma)

  const secondarySource = Hct.fromInt(argbFromHex(secondarySeed))
  const secondaryAccentPalette = paletteForHueChroma(
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
  let terminalFg: string
  let workspaceGlowColor: string | undefined
  let darkToneSurface:
    | ((
        toneValue: number,
        level: {influence: number; hueWeightFloor: number; hueWeightSpan: number},
      ) => string)
    | undefined

  if (tone === 'light') {
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
    if (neutralSeed || seedChroma < NEUTRAL_SEED_CHROMA || a === 0) {
      terminalBg = hexFromArgb(Hct.from(neutralHue, 0, 98.0).toInt())
      terminalFg = hexFromArgb(Hct.from(neutralHue, 0, 16.0).toInt())
    } else {
      const baseChroma = clamp(seedChroma * 0.08, 3.2, 5.8)
      const termBgChroma = clamp(baseChroma * (0.85 + 0.15 * a), 3, 6)
      const termBgTone = clamp(layout.terminal, 97.0, 98.5)
      terminalBg = hexFromArgb(Hct.from(primaryHue, termBgChroma, termBgTone).toInt())
      const baseFgChroma = clamp(seedChroma * 0.08, 4.2, 5.8)
      const termFgChroma = clamp(baseFgChroma * (0.85 + 0.15 * a), 4, 6)
      const termFgTone = 16.0
      terminalFg = hexFromArgb(Hct.from(primaryHue, termFgChroma, termFgTone).toInt())
    }
  } else {
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
    if (neutralSeed || seedChroma < NEUTRAL_SEED_CHROMA || a === 0) {
      terminalBg = hexFromArgb(Hct.from(0, 0, 11.0).toInt())
      terminalFg = hexFromArgb(Hct.from(0, 0, 92.0).toInt())
    } else {
      const fullChroma = clamp(seedChroma * 0.16 * (0.6 + 0.4 * hueWeight), 8.0, 11.5)
      const termBgChroma = clamp(fullChroma * (0.7 + 0.3 * a) * a, 0, 12.0)
      const termBgTone = clamp(layout.terminal, 10.0, 12.0)
      terminalBg = hexFromArgb(Hct.from(primaryHue, termBgChroma, termBgTone).toInt())
      const baseFgChroma = clamp(seedChroma * 0.08, 4.2, 5.8)
      const termFgChroma = clamp(baseFgChroma * (0.7 + 0.3 * a) * a, 0, 6.0)
      const termFgTone = 91.5
      terminalFg = hexFromArgb(Hct.from(primaryHue, termFgChroma, termFgTone).toInt())
    }
  }

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
  // Selection and badges are a gentle wash on the content surface. Reserve
  // the full accent palette for primary actions, icons and focus indicators.
  const interactiveActiveBg = composite(accentBg, surfaceBg, tone === 'light' ? 0.12 : 0.16)
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
    terminalFg,
    gradients,
  }
}
