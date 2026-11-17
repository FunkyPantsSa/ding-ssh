import {Hct, argbFromHex, hexFromArgb} from '@material/material-color-utilities'
import {describe, expect, it} from 'vitest'
import {
  contrastRatio,
  darkHueWeight,
  deriveColorRoles,
  detectCapabilities,
  hexToOklch,
  normalizeSeed,
  planMaterial,
  rolesToCssVars,
  toneLayout,
} from './material'
import type {ColorRoles, MaterialPlan, ThemeTone} from './material'

const SEEDS = ['#87CEEB', '#72B69B', '#A78BFA', '#FACC15', '#EF4444', '#A3A3A3', '#F8FAFC', '#111827']
const TONES: ThemeTone[] = ['light', 'dark']
const COLOR_SEEDS = ['#87CEEB', '#72B69B', '#A78BFA', '#FACC15', '#EF4444', '#F15E6A']

const chroma = (hex: string) => Hct.fromInt(argbFromHex(hex)).chroma
const hue = (hex: string) => Hct.fromInt(argbFromHex(hex)).hue
const hctTone = (hex: string) => Hct.fromInt(argbFromHex(hex)).tone

/** Shortest angular distance between two OKLCH/OKLCH-style hue angles. */
function hueDistance(a: number, b: number): number {
  const delta = Math.abs(a - b) % 360
  return Math.min(delta, 360 - delta)
}

function surfaces(roles: ColorRoles): string[] {
  return [
    roles.appBg,
    roles.sidebarPrimaryBg,
    roles.sidebarSecondaryBg,
    roles.workspaceBg,
    roles.surfaceBg,
    roles.surfaceRaisedBg,
    roles.surfaceOverlayBg,
    roles.toolbarBg,
    roles.fieldBg,
    roles.hoverBg,
    roles.hoverStrongBg,
  ]
}

function solidPlan(): MaterialPlan {
  return planMaterial({mode: 'solid', nativeBackdrop: false, platform: 'web'})
}

describe('material color engine', () => {
  it('is deterministic for identical input', () => {
    const options = {seed: '#A78BFA', tone: 'dark' as const, atmosphere: 75, separation: 65}
    expect(deriveColorRoles(options)).toEqual(deriveColorRoles(options))
    const plan = planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos'})
    expect(plan).toEqual(planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos'}))
  })

  it('normalizes seeds and rejects malformed values safely', () => {
    expect(normalizeSeed('#ABC')).toBe('#aabbcc')
    expect(normalizeSeed('87CEEB')).toBe('#87ceeb')
    expect(normalizeSeed('#87CEEBFF')).toBe('#87ceeb')
    expect(normalizeSeed('not-a-color')).toMatch(/^#[0-9a-f]{6}$/)
    expect(normalizeSeed(undefined)).toBe('#71b5d2')
  })

  it('derives all semantic regions from every acceptance seed', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const [key, value] of Object.entries(roles)) {
          if (key === 'seed' || key === 'secondarySeed' || key === 'tone' || key === 'gradients' || key === 'workspaceGradientStyle') continue
          if (typeof value === 'string') expect(value, key).toMatch(/^#[0-9a-f]{6}$|^rgba\(/)
        }
        expect(roles.workspaceBg).not.toBe(roles.sidebarPrimaryBg)
        expect(roles.surfaceBg).not.toBe(roles.workspaceBg)
        expect(contrastRatio(roles.surfaceBg, roles.workspaceBg)).toBeGreaterThan(1.02)
      }
    }
  })

  it('keeps body, secondary and muted text at WCAG AA over every surface', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const bg of surfaces(roles)) {
          for (const text of [roles.textPrimary, roles.textSecondary, roles.textMuted]) {
            expect(contrastRatio(text, bg), `${seed} ${tone} ${text} on ${bg}`).toBeGreaterThanOrEqual(4.5)
          }
        }
      }
    }
  })

  it('keeps accent, focus ring and strong borders perceivable', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const bg of [roles.workspaceBg, roles.surfaceBg, roles.surfaceRaisedBg, roles.sidebarPrimaryBg]) {
          expect(contrastRatio(roles.accentBg, bg)).toBeGreaterThanOrEqual(3)
          expect(contrastRatio(roles.focusRing, bg)).toBeGreaterThanOrEqual(3)
        }
        expect(contrastRatio(roles.borderStrong, roles.workspaceBg)).toBeGreaterThanOrEqual(3)
        expect(contrastRatio(roles.accentForeground, roles.accentBg)).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(roles.accentBg, roles.interactiveActiveBg)).toBeGreaterThanOrEqual(3)
        expect(contrastRatio(roles.textPrimary, roles.interactiveActiveBg)).toBeGreaterThanOrEqual(4.5)
      }
    }
  })

  it('keeps status colors readable on both surfaces and tinted badges', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const [fg, bg] of [
          [roles.statusSuccess, roles.surfaceBg],
          [roles.statusWarning, roles.surfaceBg],
          [roles.statusError, roles.surfaceBg],
          [roles.statusInfo, roles.surfaceBg],
          [roles.statusSuccess, roles.statusSuccessBg],
          [roles.statusWarning, roles.statusWarningBg],
          [roles.statusError, roles.statusErrorBg],
          [roles.statusInfo, roles.statusInfoBg],
        ]) {
          expect(contrastRatio(fg, bg), `${seed} ${tone} ${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5)
        }
      }
    }
  })

  it('keeps semantic status hues independent from the chosen seed', () => {
    const reference = deriveColorRoles({seed: '#87CEEB', tone: 'dark'})
    for (const seed of SEEDS) {
      const roles = deriveColorRoles({seed, tone: 'dark'})
      expect(Math.abs(hue(roles.statusSuccess) - hue(reference.statusSuccess))).toBeLessThan(3)
      expect(Math.abs(hue(roles.statusWarning) - hue(reference.statusWarning))).toBeLessThan(3)
      expect(Math.abs(hue(roles.statusError) - hue(reference.statusError))).toBeLessThan(3)
      expect(Math.abs(hue(roles.statusInfo) - hue(reference.statusInfo))).toBeLessThan(3)
    }
  })

  it('does not let red or green seeds erase status distinctions', () => {
    for (const seed of ['#EF4444', '#22C55E']) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        expect(roles.statusError).not.toBe(roles.statusSuccess)
        expect(hue(roles.statusError)).toBeGreaterThan(10)
        expect(hue(roles.statusError)).toBeLessThan(40)
        expect(hue(roles.statusSuccess)).toBeGreaterThan(120)
        expect(hue(roles.statusSuccess)).toBeLessThan(170)
      }
    }
  })

  it('keeps large background regions low-chroma even for highly saturated seeds', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        expect(chroma(roles.sidebarPrimaryBg)).toBeLessThanOrEqual(33)
        expect(chroma(roles.workspaceBg)).toBeLessThanOrEqual(16)
        expect(chroma(roles.surfaceBg)).toBeLessThanOrEqual(14)
      }
    }
  })

  it('separates atmosphere from surface separation', () => {
    for (const tone of TONES) {
      const neutral = deriveColorRoles({seed: '#72B69B', tone, atmosphere: 0, separation: 65})
      const vivid = deriveColorRoles({seed: '#72B69B', tone, atmosphere: 100, separation: 65})
      expect(chroma(vivid.workspaceBg)).toBeGreaterThan(chroma(neutral.workspaceBg))
      if (tone === 'dark') {
        expect(hexToOklch(vivid.workspaceBg).c).toBeGreaterThanOrEqual(0.012)
        expect(hexToOklch(vivid.workspaceGlowColor!).c).toBeGreaterThan(
          hexToOklch(neutral.workspaceGlowColor!).c,
        )
      }
      expect(chroma(vivid.sidebarPrimaryBg)).toBeGreaterThan(chroma(neutral.sidebarPrimaryBg))
      // Interaction accent keeps its semantic strength regardless of atmosphere.
      expect(vivid.accentBg).toBe(neutral.accentBg)

      const flat = deriveColorRoles({seed: '#72B69B', tone, atmosphere: 75, separation: 0})
      const layered = deriveColorRoles({seed: '#72B69B', tone, atmosphere: 75, separation: 100})
      const navDistance = (roles: ColorRoles) => Math.abs(hctTone(roles.sidebarPrimaryBg) - hctTone(roles.workspaceBg))
      expect(navDistance(layered)).toBeGreaterThan(navDistance(flat))
      const cardDistance = (roles: ColorRoles) => Math.abs(hctTone(roles.surfaceBg) - hctTone(roles.workspaceBg))
      expect(cardDistance(layered)).toBeGreaterThan(cardDistance(flat))
    }
  })

  it('keeps the reported sky seed visibly blue at atmosphere 100 without tinting cards', () => {
    const roles = deriveColorRoles({seed: '#71B5D2', tone: 'light', atmosphere: 100, separation: 100})
    expect(chroma(roles.sidebarPrimaryBg)).toBeGreaterThan(25)
    expect(chroma(roles.sidebarSecondaryBg)).toBeGreaterThan(10)
    expect(chroma(roles.workspaceBg)).toBeGreaterThan(8)
    expect(chroma(roles.surfaceBg)).toBeGreaterThan(2.5)
    expect(chroma(roles.surfaceBg)).toBeLessThanOrEqual(4)
    expect(hctTone(roles.surfaceBg)).toBeGreaterThan(hctTone(roles.workspaceBg))
    expect(contrastRatio(roles.textPrimary, roles.workspaceBg)).toBeGreaterThanOrEqual(7)
  })

  it('assigns clearly different colour-expression levels in light mode', () => {
    for (const seed of COLOR_SEEDS) {
      const roles = deriveColorRoles({seed, tone: 'light', atmosphere: 100})
      const nav = chroma(roles.sidebarPrimaryBg)
      const secondary = chroma(roles.sidebarSecondaryBg)
      const workspace = chroma(roles.workspaceBg)
      const card = chroma(roles.surfaceBg)
      expect(nav, `${seed} nav/secondary`).toBeGreaterThan(secondary * 1.35)
      expect(secondary, `${seed} secondary/workspace`).toBeGreaterThan(workspace * 1.25)
      expect(workspace, `${seed} workspace/card`).toBeGreaterThan(card * 1.4)
      expect(workspace, `${seed} workspace perceptible`).toBeGreaterThanOrEqual(5)
      expect(card, `${seed} card restrained`).toBeLessThanOrEqual(4.5)
    }
  })

  it('keeps the dark tint hierarchy and neutral foundation in OKLCH', () => {
    for (const seed of COLOR_SEEDS) {
      const roles = deriveColorRoles({seed, tone: 'dark', atmosphere: 100})
      const nav = hexToOklch(roles.sidebarPrimaryBg).c
      const secondary = hexToOklch(roles.sidebarSecondaryBg).c
      const workspace = hexToOklch(roles.workspaceBg).c
      const card = hexToOklch(roles.surfaceBg).c
      const glow = hexToOklch(roles.workspaceGlowColor!).c
      expect(nav, `${seed} nav/secondary`).toBeGreaterThan(secondary * 1.15)
      expect(secondary, `${seed} secondary tint`).toBeGreaterThan(0.01)
      expect(workspace, `${seed} workspace tint`).toBeGreaterThanOrEqual(0.012)
      expect(workspace, `${seed} workspace restrained`).toBeLessThanOrEqual(0.04)
      expect(card, `${seed} card tint`).toBeGreaterThanOrEqual(0.012)
      expect(card, `${seed} card restrained`).toBeLessThanOrEqual(0.04)
      expect(glow, `${seed} corner glow carries the theme`).toBeGreaterThanOrEqual(0.02)
      expect(glow, `${seed} corner glow restrained`).toBeLessThanOrEqual(0.06)
    }
  })

  it('keeps the dark neutral foundation spacious and the cards one step brighter', () => {
    for (const seed of SEEDS) {
      const roles = deriveColorRoles({seed, tone: 'dark'})
      expect(hctTone(roles.workspaceBg), `${seed} workspace`).toBeGreaterThan(23)
      expect(hctTone(roles.workspaceBg), `${seed} workspace`).toBeLessThan(26.5)
      expect(hctTone(roles.sidebarPrimaryBg), `${seed} nav`).toBeGreaterThan(hctTone(roles.workspaceBg))
      expect(hctTone(roles.surfaceBg), `${seed} card`).toBeGreaterThan(hctTone(roles.workspaceBg) + 4)
      expect(hctTone(roles.terminalBg), `${seed} terminal`).toBeGreaterThan(16)
      expect(hctTone(roles.terminalBg), `${seed} terminal`).toBeLessThan(18.5)
      expect(hctTone(roles.terminalBg), `${seed} terminal step`).toBeLessThan(hctTone(roles.workspaceBg) - 4)
      expect(chroma(roles.toolbarBg), `${seed} toolbar tint`).toBeLessThanOrEqual(14)
      expect(hctTone(roles.toolbarBg), `${seed} toolbar step`).toBeLessThan(hctTone(roles.workspaceBg))
      expect(hctTone(roles.workspaceBg) - hctTone(roles.toolbarBg), `${seed} toolbar step size`).toBeGreaterThanOrEqual(0.5)
      const terminalStep = hctTone(roles.toolbarBg) - hctTone(roles.terminalBg)
      expect(terminalStep, `${seed} toolbar/terminal step`).toBeGreaterThan(5)
      expect(terminalStep, `${seed} toolbar/terminal step`).toBeLessThan(6.8)
      expect(chroma(roles.sidebarPrimaryBg), `${seed} nav HCT chroma`).toBeLessThanOrEqual(27)
    }
  })

  it('keeps dark surfaces restrained at atmosphere 100 and coordinates the terminal', () => {
    for (const seed of COLOR_SEEDS) {
      const relaxed = deriveColorRoles({seed, tone: 'dark', atmosphere: 75})
      const vivid = deriveColorRoles({seed, tone: 'dark', atmosphere: 100})
      expect(hctTone(vivid.sidebarPrimaryBg), `${seed} nav tone`).toBeGreaterThanOrEqual(31)
      expect(hctTone(vivid.sidebarPrimaryBg), `${seed} nav tone`).toBeLessThanOrEqual(36)
      expect(hctTone(vivid.workspaceBg), `${seed} workspace tone`).toBeGreaterThanOrEqual(23)
      expect(hctTone(vivid.workspaceBg), `${seed} workspace tone`).toBeLessThanOrEqual(26.5)
      expect(hctTone(vivid.surfaceBg), `${seed} card tone`).toBeGreaterThanOrEqual(28)
      expect(hctTone(vivid.surfaceBg), `${seed} card tone`).toBeLessThanOrEqual(32)
      expect(hctTone(vivid.surfaceBg) - hctTone(vivid.workspaceBg), `${seed} card step`).toBeGreaterThanOrEqual(4)
      expect(hctTone(vivid.sidebarPrimaryBg) - hctTone(vivid.workspaceBg), `${seed} nav step`).toBeLessThanOrEqual(10)
      expect(hexToOklch(vivid.workspaceBg).c, `${seed} workspace OKLCH chroma`).toBeGreaterThanOrEqual(0.012)
      expect(hexToOklch(vivid.workspaceBg).c, `${seed} workspace OKLCH chroma`).toBeLessThanOrEqual(0.04)
      expect(hexToOklch(vivid.surfaceBg).c, `${seed} card OKLCH chroma`).toBeGreaterThanOrEqual(0.012)
      expect(hexToOklch(vivid.surfaceBg).c, `${seed} card OKLCH chroma`).toBeLessThanOrEqual(0.04)
      const glow = hexToOklch(vivid.workspaceGlowColor!).c
      expect(glow, `${seed} glow OKLCH chroma`).toBeGreaterThanOrEqual(0.02)
      expect(glow, `${seed} glow OKLCH chroma`).toBeLessThanOrEqual(0.06)
      // Terminal is no longer pinned to one fixed L/C: Atmosphere reaches it,
      // but its chroma stays below the nav, toolbar and SFTP/surface levels.
      const ink = hexToOklch(vivid.terminalBg)
      const toolbar = hexToOklch(vivid.toolbarBg)
      const sftp = hexToOklch(vivid.surfaceBg)
      const nav = hexToOklch(vivid.sidebarPrimaryBg)
      expect(vivid.terminalBg, `${seed} terminal follows atmosphere`).not.toBe(relaxed.terminalBg)
      expect(ink.c, `${seed} terminal atmosphere tint`).toBeGreaterThan(hexToOklch(relaxed.terminalBg).c)
      expect(ink.c, `${seed} terminal below toolbar`).toBeLessThan(toolbar.c)
      expect(ink.c, `${seed} terminal below sftp`).toBeLessThan(sftp.c)
      expect(ink.c, `${seed} terminal below nav`).toBeLessThan(nav.c)
      expect(ink.c / sftp.c, `${seed} terminal/sftp chroma ratio`).toBeLessThan(0.85)
      expect(hueDistance(ink.h, toolbar.h), `${seed} terminal/toolbar hue`).toBeLessThan(12)
      expect(hctTone(vivid.terminalBg), `${seed} terminal tone`).toBeGreaterThan(16)
      expect(hctTone(vivid.terminalBg), `${seed} terminal tone`).toBeLessThan(18.5)
      expect(hctTone(vivid.terminalBg), `${seed} terminal tone step`).toBeLessThan(hctTone(vivid.workspaceBg) - 4)
    }
  })

  it('keeps neutral seeds colourless across the dark foundation', () => {
    const channels = (hex: string) => {
      const value = parseInt(hex.slice(1), 16)
      return [(value >> 16) & 255, (value >> 8) & 255, value & 255]
    }
    for (const seed of ['#A3A3A3', '#808080', '#000000', '#FFFFFF']) {
      for (const atmosphere of [0, 75, 100]) {
        const roles = deriveColorRoles({seed, tone: 'dark', atmosphere})
        for (const key of [
          'workspaceBg',
          'surfaceBg',
          'toolbarBg',
          'terminalBg',
          'sidebarSecondaryBg',
          'sidebarPrimaryBg',
        ] as const) {
          expect(new Set(channels(roles[key])).size, `${seed} ${atmosphere} ${key}`).toBe(1)
        }
      }
    }
  })

  it('keeps the terminal ink lower-chroma and same-hue than the toolbar and SFTP', () => {
    for (const seed of COLOR_SEEDS) {
      for (const atmosphere of [0, 20, 40, 75, 100]) {
        for (const separation of [0, 65, 100]) {
          const roles = deriveColorRoles({seed, tone: 'dark', atmosphere, separation})
          const terminal = hexToOklch(roles.terminalBg)
          const toolbar = hexToOklch(roles.toolbarBg)
          const sftp = hexToOklch(roles.surfaceBg)
          const nav = hexToOklch(roles.sidebarPrimaryBg)
          const label = `${seed} a${atmosphere} s${separation}`

          // A stable recessed ink step, not a fixed black: the terminal stays
          // below the toolbar and well below the SFTP panel in tone.
          expect(hctTone(roles.terminalBg), `${label} tone`).toBeGreaterThan(16)
          expect(hctTone(roles.terminalBg), `${label} tone`).toBeLessThan(18.5)
          const toolbarStep = hctTone(roles.toolbarBg) - hctTone(roles.terminalBg)
          expect(toolbarStep, `${label} toolbar step`).toBeGreaterThan(5)
          expect(toolbarStep, `${label} toolbar step`).toBeLessThan(6.8)
          const sftpStep = hctTone(roles.surfaceBg) - hctTone(roles.terminalBg)
          expect(sftpStep, `${label} sftp step`).toBeGreaterThan(12)
          expect(sftpStep, `${label} sftp step`).toBeLessThan(14.5)

          if (atmosphere === 0) {
            expect(terminal.c, `${label} neutral`).toBeLessThan(1e-6)
            expect(toolbar.c, `${label} toolbar neutral`).toBeLessThan(1e-6)
            expect(sftp.c, `${label} sftp neutral`).toBeLessThan(1e-6)
            continue
          }

          // Chroma is judged by its relationship to the neighbouring regions,
          // not by a single fixed ceiling.
          expect(terminal.c, `${label} terminal tint`).toBeGreaterThan(0)
          expect(terminal.c, `${label} terminal < toolbar`).toBeLessThan(toolbar.c)
          expect(terminal.c, `${label} terminal < sftp`).toBeLessThan(sftp.c)
          expect(terminal.c, `${label} terminal < nav`).toBeLessThan(nav.c)
          expect(terminal.c / toolbar.c, `${label} toolbar ratio`).toBeLessThan(0.85)
          expect(terminal.c / sftp.c, `${label} sftp ratio`).toBeLessThan(0.85)
          if (atmosphere >= 40) {
            // At a very low Atmosphere the absolute tint is already faint; the
            // ratio may shrink because the dark tone clips chroma harder.
            expect(terminal.c / toolbar.c, `${label} toolbar ratio`).toBeGreaterThan(0.3)
            expect(terminal.c / sftp.c, `${label} sftp ratio`).toBeGreaterThan(0.3)
          }
          expect(chroma(roles.terminalBg), `${label} HCT < toolbar`).toBeLessThan(chroma(roles.toolbarBg))
          expect(chroma(roles.terminalBg), `${label} HCT < sftp`).toBeLessThan(chroma(roles.surfaceBg))

          // Hue agreement only matters once the surface tint is perceivable.
          // Below that, sRGB quantisation makes hue angles noisy.
          if (terminal.c >= 0.008) {
            expect(hueDistance(terminal.h, toolbar.h), `${label} toolbar hue`).toBeLessThan(12)
            expect(hueDistance(terminal.h, sftp.h), `${label} sftp hue`).toBeLessThan(12)
            expect(hueDistance(terminal.h, nav.h), `${label} nav hue`).toBeLessThan(16)
          }
        }
      }
    }
  })

  it('keeps the terminal strictly below nav, toolbar and SFTP for every hue', () => {
    for (let hue = 0; hue < 360; hue += 20) {
      const seed = hexFromArgb(Hct.from(hue, 60, 48).toInt())
      for (const atmosphere of [10, 20, 40, 75, 100]) {
        for (const separation of [0, 25, 65, 100]) {
          const roles = deriveColorRoles({seed, tone: 'dark', atmosphere, separation})
          const terminal = hexToOklch(roles.terminalBg)
          const toolbar = hexToOklch(roles.toolbarBg)
          const sftp = hexToOklch(roles.surfaceBg)
          const nav = hexToOklch(roles.sidebarPrimaryBg)
          const label = `hue ${hue} a${atmosphere} s${separation}`

          // These are pixel-level comparisons: hex is converted back to OKLCH,
          // so sRGB quantisation cannot slip a more chromatic ink through.
          expect(terminal.c, `${label} < toolbar`).toBeLessThan(toolbar.c)
          expect(terminal.c, `${label} < sftp`).toBeLessThan(sftp.c)
          expect(terminal.c, `${label} < nav`).toBeLessThan(nav.c)

          if (terminal.c >= 0.008) {
            expect(hueDistance(terminal.h, toolbar.h), `${label} toolbar hue`).toBeLessThan(12)
            expect(hueDistance(terminal.h, sftp.h), `${label} sftp hue`).toBeLessThan(12)
          }
        }
      }
    }
  })

  it('carries theme warmth into the terminal instead of a fixed cool black', () => {
    const blue = deriveColorRoles({seed: '#87CEEB', tone: 'dark', atmosphere: 75})
    const pink = deriveColorRoles({seed: '#E89AB8', tone: 'dark', atmosphere: 75})
    const blueInk = hexToOklch(blue.terminalBg)
    const pinkInk = hexToOklch(pink.terminalBg)

    expect(hueDistance(blueInk.h, hexToOklch(blue.toolbarBg).h)).toBeLessThan(12)
    expect(hueDistance(pinkInk.h, hexToOklch(pink.toolbarBg).h)).toBeLessThan(12)
    expect(hueDistance(blueInk.h, pinkInk.h)).toBeGreaterThan(90)
    expect(pinkInk.h > 300 || pinkInk.h < 30).toBe(true)
    expect(blueInk.h).toBeGreaterThan(190)
    expect(blueInk.h).toBeLessThan(250)
    expect(pinkInk.c).toBeGreaterThan(0.008)
  })

  it('keeps the dark tint continuous into the terminal at low atmosphere', () => {
    for (const seed of COLOR_SEEDS) {
      for (const atmosphere of [20, 40]) {
        const roles = deriveColorRoles({seed, tone: 'dark', atmosphere})
        const label = `${seed} a${atmosphere}`
        // The old hard threshold snapped every low-chroma surface back to grey,
        // leaving a tinted nav next to a neutral terminal. All four neighbours
        // now keep the same generated hue.
        expect(hexToOklch(roles.terminalBg).c, `${label} terminal`).toBeGreaterThan(0)
        expect(hexToOklch(roles.toolbarBg).c, `${label} toolbar`).toBeGreaterThan(0)
        expect(hexToOklch(roles.surfaceBg).c, `${label} sftp`).toBeGreaterThan(0)
        expect(hctTone(roles.terminalBg), `${label} tone`).toBeGreaterThan(16)
        expect(hctTone(roles.terminalBg), `${label} tone`).toBeLessThan(18.5)
      }
    }
  })

  it('uses a continuous, warm-damped hue weight for dark tint budgets', () => {
    let previous = darkHueWeight(0)
    for (let hue = 1; hue <= 360; hue += 1) {
      const weight = darkHueWeight(hue)
      expect(Math.abs(weight - previous), `hue ${hue}`).toBeLessThan(0.03)
      previous = weight
    }
    expect(darkHueWeight(20)).toBeLessThan(darkHueWeight(210))
    expect(darkHueWeight(90)).toBeLessThan(darkHueWeight(210))
    expect(darkHueWeight(330)).toBeLessThan(darkHueWeight(210))
  })

  it('offers deterministic solid / subtle / corner dark workspace treatments', () => {
    const plan = solidPlan()
    const solid = rolesToCssVars(
      deriveColorRoles({seed: '#87CEEB', tone: 'dark', workspaceGradientStyle: 'solid'}),
      plan,
    )
    expect(solid['--workspace-gradient']).toBe(solid['--workspace-bg'])
    expect(solid['--workspace-gradient']).not.toContain('gradient(')

    const subtle = rolesToCssVars(
      deriveColorRoles({seed: '#87CEEB', tone: 'dark', workspaceGradientStyle: 'subtle'}),
      plan,
    )
    expect(subtle['--workspace-gradient']).toContain('linear-gradient(135deg')
    expect(subtle['--workspace-gradient']).toContain(subtle['--workspace-bg'])

    const corner = rolesToCssVars(
      deriveColorRoles({seed: '#87CEEB', tone: 'dark', workspaceGradientStyle: 'corner'}),
      plan,
    )
    expect(corner['--workspace-gradient']).toContain('radial-gradient')
    expect(corner['--workspace-gradient']).toContain(corner['--workspace-bg'])
  })

  it('derives subtle same-hue ambient gradients for the nav and workspace', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const [name, ramp] of Object.entries(roles.gradients)) {
          expect(ramp.angle, name).toBe(name === 'sidebarPrimary' ? 180 : 135)
          expect(ramp.stops.length, name).toBe(5)
          for (const stop of ramp.stops) expect(stop, name).toMatch(/^#[0-9a-f]{6}$/)
          expect(ramp.stops[0], name).not.toBe(ramp.stops[ramp.stops.length - 1])
          expect(ramp.lightnessDelta, name).toBeGreaterThan(0.01)
          expect(ramp.lightnessDelta, name).toBeLessThan(0.045)
          const first = hexToOklch(ramp.stops[0])
          const last = hexToOklch(ramp.stops[ramp.stops.length - 1])
          expect(Math.abs(first.h - last.h), `${name} hue drift`).toBeLessThanOrEqual(8)
        }
      }
    }
  })

  it('keeps every text role readable at both ends of every gradient', () => {
    for (const seed of SEEDS) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const stop of [...roles.gradients.sidebarPrimary.stops, ...roles.gradients.workspace.stops]) {
          for (const text of [roles.textPrimary, roles.textSecondary, roles.textMuted]) {
            expect(contrastRatio(text, stop), `${seed} ${tone} ${text} on gradient ${stop}`).toBeGreaterThanOrEqual(4.5)
          }
        }
      }
    }
  })

  it('keeps configured atmosphere visibly colorful for every acceptance hue', () => {
    for (const seed of COLOR_SEEDS) {
      const light = deriveColorRoles({seed, tone: 'light', atmosphere: 100, separation: 100})
      expect(chroma(light.sidebarPrimaryBg), `${seed} nav`).toBeGreaterThanOrEqual(20)
      expect(chroma(light.workspaceBg), `${seed} workspace`).toBeGreaterThanOrEqual(5)
      expect(contrastRatio(light.textPrimary, light.workspaceBg)).toBeGreaterThanOrEqual(4.5)
      const dark = deriveColorRoles({seed, tone: 'dark', atmosphere: 100, separation: 100})
      expect(chroma(dark.sidebarPrimaryBg), `${seed} dark nav`).toBeGreaterThanOrEqual(20)
      expect(hexToOklch(dark.workspaceBg).c, `${seed} dark workspace tint`).toBeGreaterThanOrEqual(0.012)
      expect(hexToOklch(dark.workspaceBg).c, `${seed} dark workspace restrained`).toBeLessThanOrEqual(0.04)
      expect(hexToOklch(dark.workspaceGlowColor!).c, `${seed} dark glow`).toBeGreaterThanOrEqual(0.02)
    }
  })

  it('creates a monotonic four-step hierarchy for both tones', () => {
    for (const seed of SEEDS) {
      const light = deriveColorRoles({seed, tone: 'light', atmosphere: 75, separation: 100})
      expect(hctTone(light.sidebarPrimaryBg)).toBeLessThan(hctTone(light.sidebarSecondaryBg))
      expect(hctTone(light.sidebarSecondaryBg)).toBeLessThan(hctTone(light.workspaceBg))
      expect(hctTone(light.workspaceBg)).toBeLessThan(hctTone(light.surfaceBg))
      const dark = deriveColorRoles({seed, tone: 'dark', atmosphere: 75, separation: 100})
      expect(hctTone(dark.sidebarPrimaryBg)).toBeGreaterThan(hctTone(dark.sidebarSecondaryBg))
      expect(hctTone(dark.sidebarSecondaryBg)).toBeGreaterThan(hctTone(dark.workspaceBg))
      expect(hctTone(dark.surfaceBg)).toBeGreaterThan(hctTone(dark.workspaceBg))
    }
  })

  it('places cards on the correct side of the workspace in each tone', () => {
    for (const seed of SEEDS) {
      const light = deriveColorRoles({seed, tone: 'light'})
      expect(hctTone(light.surfaceBg)).toBeGreaterThan(hctTone(light.workspaceBg))
      const dark = deriveColorRoles({seed, tone: 'dark'})
      expect(hctTone(dark.surfaceBg)).toBeGreaterThan(hctTone(dark.workspaceBg))
      expect(hctTone(dark.surfaceRaisedBg)).toBeGreaterThan(hctTone(dark.surfaceBg))
    }
  })

  it('keeps pure black/white seeds neutral instead of inventing a hue', () => {
    for (const seed of ['#000000', '#FFFFFF', '#808080']) {
      const roles = deriveColorRoles({seed, tone: 'dark'})
      const channels = (hex: string) => {
        const value = parseInt(hex.slice(1), 16)
        return [(value >> 16) & 255, (value >> 8) & 255, value & 255]
      }
      expect(new Set(channels(roles.workspaceBg)).size, roles.workspaceBg).toBe(1)
      expect(new Set(channels(roles.accentBg)).size, roles.accentBg).toBe(1)
    }
  })

  it('stretches region separation monotonically', () => {
    for (const tone of TONES) {
      let previous = -1
      for (const separation of [0, 25, 50, 75, 100]) {
        const layout = toneLayout(tone, separation)
        const distance = Math.abs(layout.sidebarPrimary - layout.workspace)
        expect(distance).toBeGreaterThanOrEqual(previous)
        previous = distance
      }
    }
  })
})

describe('material planning', () => {
  it('uses solid surfaces when no native backdrop is available', () => {
    for (const mode of ['soft', 'clear'] as const) {
      const plan = planMaterial({mode, nativeBackdrop: false, platform: 'linux'})
      expect(plan.mode).toBe('solid')
      expect(plan.fallback).toBe('unsupported-platform')
      expect(Object.values(plan.alpha).every((value) => value === 1)).toBe(true)
      expect(plan.shellBlurPx).toBe(0)
      expect(plan.overlayBlurPx).toBe(0)
    }
  })

  it('produces region-specific alphas for native glass', () => {
    const soft = planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos', surfaceOpacity: 85, blurPx: 24})
    const clear = planMaterial({mode: 'clear', nativeBackdrop: true, platform: 'windows', surfaceOpacity: 85, blurPx: 24})
    expect(soft.mode).toBe('soft')
    expect(soft.fallback).toBe('none')
    expect(soft.alpha.sidebarPrimary).toBeLessThan(1)
    expect(soft.alpha.workspace).toBeLessThan(soft.alpha.surface)
    expect(soft.alpha.field).toBeGreaterThan(soft.alpha.surface)
    expect(soft.shellBlurPx).toBe(0)
    expect(soft.overlayBlurPx).toBe(24)
    expect(clear.alpha.workspace).toBeLessThan(soft.alpha.workspace)
    expect(clear.alpha.surface).toBeLessThan(soft.alpha.surface)
  })

  it('lets the opacity slider change translucency without changing region identity', () => {
    const low = planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos', surfaceOpacity: 60})
    const high = planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos', surfaceOpacity: 100})
    expect(low.alpha.workspace).toBeLessThan(high.alpha.workspace)
    expect(high.alpha.surface).toBeLessThanOrEqual(0.995)
    expect(low.alpha.surface).toBeGreaterThan(low.alpha.workspace)
  })

  it('emits only opaque base surfaces in Solid mode', () => {
    const roles = deriveColorRoles({seed: '#71B5D2', tone: 'light', atmosphere: 100, separation: 100})
    const vars = rolesToCssVars(roles, solidPlan())
    for (const token of [
      '--app-bg',
      '--sidebar-primary-bg',
      '--sidebar-secondary-bg',
      '--workspace-bg',
      '--surface-bg',
      '--surface-raised-bg',
      '--surface-overlay-bg',
      '--toolbar-bg',
      '--field-bg',
      '--terminal-bg',
      '--background',
      '--surface-1',
      '--surface-2',
      '--surface-3',
      '--surface-4',
    ]) {
      expect(vars[token], token).toMatch(/^#[0-9a-f]{6}$/)
    }
  })

  it('covers the six acceptance seeds across both tones and all material modes', () => {
    const acceptance = ['#87CEEB', '#72B69B', '#A78BFA', '#FACC15', '#EF4444', '#A3A3A3']
    for (const seed of acceptance) {
      for (const tone of TONES) {
        const roles = deriveColorRoles({seed, tone})
        for (const mode of ['solid', 'soft', 'clear'] as const) {
          const plan = planMaterial({mode, nativeBackdrop: true, platform: 'macos', surfaceOpacity: 85})
          const vars = rolesToCssVars(roles, plan)
          if (tone === 'light') {
            expect(vars['--workspace-gradient'], `${seed} ${tone} ${mode}`).toContain('linear-gradient(135deg')
          } else {
            expect(vars['--workspace-gradient'], `${seed} ${tone} ${mode}`).toContain('radial-gradient')
          }
          expect(vars['--sidebar-primary-gradient'], `${seed} ${tone} ${mode}`).toContain('linear-gradient(180deg')
          expect(vars['--terminal-bg'], `${seed} ${tone} ${mode}`).toMatch(/^#[0-9a-f]{6}$/)
          if (mode === 'solid') {
            expect(vars['--workspace-bg']).toMatch(/^#[0-9a-f]{6}$/)
            expect(vars['--workspace-gradient']).toContain(vars['--workspace-bg'])
          } else {
            expect(vars['--workspace-bg']).toContain('rgba(')
            expect(vars['--workspace-gradient']).toContain(vars['--workspace-bg'])
          }
          for (const stop of [...roles.gradients.workspace.stops, ...roles.gradients.sidebarPrimary.stops]) {
            for (const text of [roles.textPrimary, roles.textSecondary, roles.textMuted]) {
              expect(contrastRatio(text, stop), `${seed} ${tone} ${mode}`).toBeGreaterThanOrEqual(4.5)
            }
          }
        }
      }
    }
  })

  it('composes ambient gradients with the active material alpha', () => {
    const roles = deriveColorRoles({seed: '#87CEEB', tone: 'light'})
    const solid = rolesToCssVars(roles, solidPlan())
    expect(solid['--sidebar-primary-gradient']).toContain('linear-gradient(180deg')
    expect(solid['--sidebar-primary-gradient']).not.toContain('rgba(')
    expect(solid['--workspace-gradient']).toContain('linear-gradient(135deg')
    expect(solid['--workspace-gradient']).not.toContain('rgba(')
    expect(solid['--sidebar-primary-gradient-start']).toMatch(/^#[0-9a-f]{6}$/)

    const glass = rolesToCssVars(
      roles,
      planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos'}),
    )
    expect(glass['--sidebar-primary-gradient']).toContain('rgba(')
    expect(glass['--workspace-gradient']).toContain('rgba(')
    expect(glass['--terminal-bg']).toMatch(/^#[0-9a-f]{6}$/)
  })

  it('emits opaque terminal and field tokens under every material plan', () => {
    const roles = deriveColorRoles({seed: '#FACC15', tone: 'dark'})
    for (const plan of [
      solidPlan(),
      planMaterial({mode: 'soft', nativeBackdrop: true, platform: 'macos'}),
      planMaterial({mode: 'clear', nativeBackdrop: true, platform: 'windows'}),
    ]) {
      const vars = rolesToCssVars(roles, plan)
      expect(vars['--terminal-bg']).toMatch(/^#[0-9a-f]{6}$/)
      if (plan.mode === 'solid') {
        expect(vars['--sidebar-primary-bg']).toMatch(/^#[0-9a-f]{6}$/)
        expect(vars['--surface-bg']).toMatch(/^#[0-9a-f]{6}$/)
      } else {
        expect(vars['--sidebar-primary-bg']).toContain('rgba(')
        expect(vars['--surface-bg']).toContain('rgba(')
      }
      for (const token of [
        '--app-bg',
        '--sidebar-primary-bg',
        '--sidebar-secondary-bg',
        '--workspace-bg',
        '--surface-bg',
        '--surface-raised-bg',
        '--surface-overlay-bg',
        '--interactive-hover-bg',
        '--interactive-active-bg',
        '--interactive-pressed-bg',
        '--focus-ring-color',
        '--text-primary',
        '--text-secondary',
        '--text-muted',
        '--text-on-accent',
        '--border-subtle',
        '--border-default',
        '--border-strong',
        '--accent-bg',
        '--accent-hover-bg',
        '--accent-foreground',
        '--status-success',
        '--status-warning',
        '--status-error',
        '--status-info',
        '--terminal-bg',
      ]) {
        expect(vars[token], token).toBeTruthy()
      }
    }
  })
})

describe('platform capability detection', () => {
  it('never assumes a native backdrop in a plain browser', () => {
    const caps = detectCapabilities()
    expect(caps.wails).toBe(false)
    expect(caps.nativeBackdrop).toBe(false)
  })
})
