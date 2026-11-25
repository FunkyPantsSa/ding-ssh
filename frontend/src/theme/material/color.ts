import {Hct, argbFromHex, hexFromArgb} from '@material/material-color-utilities'
import {DEFAULT_SEED, NEUTRAL_SEED_CHROMA, type Oklch, type Rgb} from './types'

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

/**
 * Automatically derives a harmonious Arc-style secondary accent color
 * from a primary seed color.
 */
export function deriveHarmonizedSecondary(seedHex: string): string {
  const seed = normalizeSeed(seedHex)
  const source = Hct.fromInt(argbFromHex(seed))
  if (source.chroma < NEUTRAL_SEED_CHROMA || source.tone <= 0.5 || source.tone >= 99.5) {
    const nextTone = source.tone > 50 ? clamp(source.tone - 20, 20, 80) : clamp(source.tone + 20, 20, 80)
    return hexFromArgb(Hct.from(0, 0, nextTone).toInt())
  }
  let hueDelta = 35
  if (source.hue >= 330 || source.hue < 20) {
    hueDelta = -55 // Rose / Crimson -> Violet / Lavender
  } else if (source.hue >= 20 && source.hue <= 90) {
    hueDelta = -35 // Orange / Yellow -> Rose / Coral
  } else if (source.hue > 90 && source.hue <= 180) {
    hueDelta = 30  // Green / Mint -> Teal / Aqua
  } else if (source.hue > 180 && source.hue <= 270) {
    hueDelta = 40  // Sky / Slate -> Indigo / Periwinkle
  } else {
    hueDelta = 40  // Violet / Purple -> Rose / Magenta
  }
  const targetHue = (source.hue + hueDelta + 360) % 360
  const targetChroma = clamp(source.chroma * 0.95, 26, 65)
  const targetTone = clamp(source.tone, 55, 80)
  return hexFromArgb(Hct.from(targetHue, targetChroma, targetTone).toInt())
}

/**
 * Derives the base canvas color for the Arc ambient backdrop.
 */
export function arcBaseBackground(hue: number, chroma: number, light: boolean, atmosphere = 75): string {
  if (chroma < NEUTRAL_SEED_CHROMA) {
    return light ? '#fafafa' : '#121214'
  }
  const atmoFactor = clamp(atmosphere / 100, 0.25, 1)
  const targetChroma = Math.min(chroma * 0.35, light ? 16 : 10) * atmoFactor
  const targetTone = light ? clamp(96 - atmoFactor * 2.5, 92, 97) : clamp(7 + atmoFactor * 2, 6, 12)
  return hexFromArgb(Hct.from(hue, targetChroma, targetTone).toInt())
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
  return rgbToHex({
    r: b.r + (f.r - b.r) * amount,
    g: b.g + (f.g - b.g) * amount,
    b: b.b + (f.b - b.b) * amount,
  })
}

// ---------------------------------------------------------------------------
// OKLCH helpers
// ---------------------------------------------------------------------------

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
