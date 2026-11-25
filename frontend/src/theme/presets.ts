import {t} from '../i18n'
import {Hct, argbFromHex, hexFromArgb} from '@material/material-color-utilities'
import {deriveColorRoles, deriveHarmonizedSecondary, normalizeSeed, rgba} from './material'
// 预设主题库：精选配色，每套含 UI 主/辅色与深/浅两套终端色板。
// 终端色板用于映射 ANSI 16 色，让 ls / vim / 提示符等程序输出跟随主题。
import type {Theme, UIAppearance} from '../types'

export interface TerminalPalette {
  background: string
  foreground: string
  cursor: string
  selection: string
  /** ANSI 30–37：black..white */
  ansi: [string, string, string, string, string, string, string, string]
  /** ANSI 90–97：brightBlack..brightWhite */
  bright: [string, string, string, string, string, string, string, string]
}

export interface ThemePreset {
  id: string
  name: string
  desc: string
  primary: string // UI 主色
  secondary: string // UI 辅色
  dark: TerminalPalette
  light: TerminalPalette
}

export const PRESETS: ThemePreset[] = [
  {
    id: 'arc-velvet',
    get name() { return t("Arc 丝绒玫瑰") },
    get desc() { return t("Arc · 柔粉玫瑰与流光紫") },
    primary: '#F19BC0',
    secondary: '#A78BFA',
    dark: {
      background: '#181216', foreground: '#f5e9f0', cursor: '#f472b6', selection: 'rgba(241, 155, 192, 0.32)',
      ansi: ['#09090b', '#f43f5e', '#34d399', '#fbbf24', '#939dfb', '#c084fc', '#38bdf8', '#fafafa'],
      bright: ['#52525b', '#fb7185', '#6ee7b7', '#fde047', '#b4c1fc', '#e879f9', '#7dd3fc', '#ffffff'],
    },
    light: {
      background: '#fdf7fa', foreground: '#1f131a', cursor: '#db2777', selection: 'rgba(219, 39, 119, 0.22)',
      ansi: ['#1f131a', '#e11d48', '#059669', '#d97706', '#4338ca', '#7e22ce', '#0284c7', '#f4f4f5'],
      bright: ['#4b5563', '#be123c', '#047857', '#b45309', '#3730a3', '#6b21a8', '#0369a1', '#ffffff'],
    },
  },
  {
    id: 'arc-ocean',
    get name() { return t("Arc 海雾苍蓝") },
    get desc() { return t("Arc · 苍穹水蓝与冰川天青") },
    primary: '#71B5D2',
    secondary: '#38BDF8',
    dark: {
      background: '#0f171d', foreground: '#e5eef3', cursor: '#38bdf8', selection: 'rgba(113, 181, 210, 0.32)',
      ansi: ['#09090b', '#ef4444', '#22c55e', '#f59e0b', '#60a5fa', '#c084fc', '#22d3ee', '#fafafa'],
      bright: ['#52525b', '#f87171', '#4ade80', '#fbbf24', '#93c5fd', '#d8b4fe', '#67e8f9', '#ffffff'],
    },
    light: {
      background: '#f4f8fb', foreground: '#0c1a24', cursor: '#0284c7', selection: 'rgba(2, 132, 199, 0.22)',
      ansi: ['#0c1a24', '#dc2626', '#16a34a', '#d97706', '#1d4ed8', '#7e22ce', '#0e7490', '#f4f4f5'],
      bright: ['#4b5563', '#b91c1c', '#15803d', '#b45309', '#1e40af', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
  {
    id: 'arc-sage',
    get name() { return t("Arc 赛博薄荷") },
    get desc() { return t("Arc · 翡翠薄荷与浅叶绿") },
    primary: '#34D399',
    secondary: '#6EE7B7',
    dark: {
      background: '#0e1713', foreground: '#e6f2eb', cursor: '#34d399', selection: 'rgba(52, 211, 153, 0.32)',
      ansi: ['#09090b', '#ef4444', '#34d399', '#fbbf24', '#60a5fa', '#c084fc', '#2dd4bf', '#fafafa'],
      bright: ['#52525b', '#f87171', '#6ee7b7', '#fde047', '#93c5fd', '#d8b4fe', '#5eead4', '#ffffff'],
    },
    light: {
      background: '#f4faf7', foreground: '#0b1c15', cursor: '#059669', selection: 'rgba(5, 150, 105, 0.22)',
      ansi: ['#0b1c15', '#dc2626', '#059669', '#d97706', '#1d4ed8', '#7e22ce', '#0f766e', '#f4f4f5'],
      bright: ['#4b5563', '#b91c1c', '#047857', '#b45309', '#1e40af', '#6b21a8', '#115e59', '#ffffff'],
    },
  },
  {
    id: 'arc-sunset',
    get name() { return t("Arc 落日暖阳") },
    get desc() { return t("Arc · 暮色暖橙与落日绯红") },
    primary: '#FB923C',
    secondary: '#F43F5E',
    dark: {
      background: '#18120e', foreground: '#f4eae5', cursor: '#fb923c', selection: 'rgba(251, 146, 60, 0.32)',
      ansi: ['#09090b', '#f43f5e', '#22c55e', '#fb923c', '#689df6', '#e879f9', '#38bdf8', '#fafafa'],
      bright: ['#52525b', '#fb7185', '#4ade80', '#fdba74', '#93c5fd', '#f0abfc', '#7dd3fc', '#ffffff'],
    },
    light: {
      background: '#fdf8f6', foreground: '#24130c', cursor: '#ea580c', selection: 'rgba(234, 88, 12, 0.22)',
      ansi: ['#24130c', '#e11d48', '#16a34a', '#ea580c', '#1d4ed8', '#7e22ce', '#0284c7', '#f4f4f5'],
      bright: ['#4b5563', '#be123c', '#15803d', '#c2410c', '#1e40af', '#6b21a8', '#0369a1', '#ffffff'],
    },
  },
  {
    id: 'sky',
    get name() { return t("天空蓝") },
    get desc() { return t("默认 · 清朗天空蓝") },
    primary: '#71B5D2',
    secondary: '#71B5D2',
    dark: {
      background: '#12181b', foreground: '#e6edf1', cursor: '#71B5D2', selection: 'rgba(113, 181, 210, 0.28)',
      ansi: ['#09090b', '#ef4444', '#22c55e', '#f59e0b', '#60a5fa', '#c084fc', '#22d3ee', '#fafafa'],
      bright: ['#52525b', '#f87171', '#4ade80', '#fbbf24', '#93c5fd', '#d8b4fe', '#67e8f9', '#ffffff'],
    },
    light: {
      background: '#f5f7f8', foreground: '#18181b', cursor: '#2563eb', selection: 'rgba(37, 99, 235, 0.22)',
      ansi: ['#18181b', '#dc2626', '#16a34a', '#d97706', '#1d4ed8', '#7e22ce', '#0e7490', '#f4f4f5'],
      bright: ['#4b5563', '#b91c1c', '#15803d', '#b45309', '#1e40af', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
  {
    id: 'geist',
    name: 'Geist',
    get desc() { return t("默认 · 中性深色") },
    primary: '#3b82f6',
    secondary: '#a1a1aa',
    dark: {
      background: '#141820', foreground: '#e6ebf2', cursor: '#60a5fa', selection: 'rgba(59, 130, 246, 0.28)',
      ansi: ['#09090b', '#ef4444', '#22c55e', '#f59e0b', '#60a5fa', '#c084fc', '#22d3ee', '#fafafa'],
      bright: ['#52525b', '#f87171', '#4ade80', '#fbbf24', '#93c5fd', '#d8b4fe', '#67e8f9', '#ffffff'],
    },
    light: {
      background: '#f5f6f7', foreground: '#18181b', cursor: '#2563eb', selection: 'rgba(37, 99, 235, 0.22)',
      ansi: ['#18181b', '#dc2626', '#16a34a', '#d97706', '#1d4ed8', '#7e22ce', '#0e7490', '#f4f4f5'],
      bright: ['#4b5563', '#b91c1c', '#15803d', '#b45309', '#1e40af', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
  {
    id: 'monochrome', get name() { return t("黑白") }, get desc() { return t("黑白灰 · 简洁清晰") },
    primary: '#A3A3A3', secondary: '#737373',
    dark: {
      background: '#171717', foreground: '#e5e5e5', cursor: '#ffffff', selection: 'rgba(255, 255, 255, 0.22)',
      ansi: ['#171717', '#a3a3a3', '#d4d4d4', '#bdbdbd', '#c8c8c8', '#d0d0d0', '#e5e5e5', '#f5f5f5'],
      bright: ['#737373', '#d4d4d4', '#ededed', '#e0e0e0', '#e0e0e0', '#ebebeb', '#f5f5f5', '#ffffff'],
    },
    light: {
      background: '#fafafa', foreground: '#171717', cursor: '#171717', selection: 'rgba(0, 0, 0, 0.16)',
      ansi: ['#171717', '#404040', '#262626', '#525252', '#323232', '#383838', '#303030', '#e5e5e5'],
      bright: ['#4b5563', '#333333', '#262626', '#444444', '#252525', '#333333', '#222222', '#ffffff'],
    },
  },
  {
    id: 'signal',
    get name() { return t("信号青绿") },
    get desc() { return t("经典默认 · 连接即锁合") },
    primary: '#3ec4b4',
    secondary: '#c97a4a',
    dark: {
      background: '#0e1514',
      foreground: '#d6e4e1',
      cursor: '#3ec4b4',
      selection: 'rgba(42, 168, 154, 0.28)',
      ansi: ['#0c1016', '#d45a5a', '#4caf7a', '#d4a04a', '#689df6', '#b26cd9', '#3ec4b4', '#d4dae3'],
      bright: ['#6b7684', '#f08080', '#7ad8a3', '#e8c070', '#8fb0f2', '#d39cf0', '#6dd9cb', '#ffffff'],
    },
    light: {
      background: '#f4f6f8',
      foreground: '#1b2734',
      cursor: '#2aa89a',
      selection: 'rgba(42, 168, 154, 0.22)',
      ansi: ['#1b2734', '#b91c1c', '#15803d', '#b45309', '#1d4ed8', '#7e22ce', '#0f766e', '#e8ecf1'],
      bright: ['#4b5563', '#991b1b', '#166534', '#92400e', '#1e40af', '#6b21a8', '#115e59', '#ffffff'],
    },
  },
  {
    id: 'copper',
    get name() { return t("日落铜橙") },
    get desc() { return t("暖色系 · 运维如坐席") },
    primary: '#d97b3f',
    secondary: '#c9a24a',
    dark: {
      background: '#17120f',
      foreground: '#e6ded6',
      cursor: '#e08a4a',
      selection: 'rgba(217, 123, 63, 0.30)',
      ansi: ['#120e0c', '#d45a4a', '#9caf5c', '#d4a04a', '#caa174', '#c97a6a', '#e08a4a', '#e0d6cc'],
      bright: ['#6e6357', '#f0806a', '#b8cd7a', '#e8c070', '#e0b882', '#e09a8a', '#f0b07a', '#fff5ec'],
    },
    light: {
      background: '#f7f3ee',
      foreground: '#32251a',
      cursor: '#c97030',
      selection: 'rgba(217, 123, 63, 0.22)',
      ansi: ['#32251a', '#b91c1c', '#4d7c0f', '#b45309', '#854d0e', '#9a3412', '#c2410c', '#efe6da'],
      bright: ['#4b5563', '#991b1b', '#3f6212', '#92400e', '#713f12', '#7c2d12', '#9a3412', '#ffffff'],
    },
  },
  {
    id: 'ocean',
    get name() { return t("深海蓝") },
    get desc() { return t("沉稳冷静 · 深蓝之海") },
    primary: '#4a8fe0',
    secondary: '#3ec4b4',
    dark: {
      background: '#0d141d',
      foreground: '#d4dfed',
      cursor: '#6fb0f0',
      selection: 'rgba(74, 143, 224, 0.28)',
      ansi: ['#0a0f16', '#d45a6a', '#4caf8a', '#d4a04a', '#70a6ff', '#c084fc', '#4a9bd4', '#cdd8e8'],
      bright: ['#5d6e8a', '#f07a8a', '#7ad8b0', '#e8c070', '#9ec3ff', '#d8b4fe', '#6fb0f0', '#ffffff'],
    },
    light: {
      background: '#f2f6fc',
      foreground: '#182333',
      cursor: '#3a7bd4',
      selection: 'rgba(74, 143, 224, 0.22)',
      ansi: ['#182333', '#b91c1c', '#15803d', '#b45309', '#1d4ed8', '#7e22ce', '#0e7490', '#e8eef7'],
      bright: ['#4b5563', '#991b1b', '#166534', '#92400e', '#1e40af', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
  {
    id: 'violet',
    get name() { return t("紫罗兰") },
    get desc() { return t("神秘优雅 · 电子紫调") },
    primary: '#9b7ce0',
    secondary: '#e08ab8',
    dark: {
      background: '#15111c',
      foreground: '#e0d8ee',
      cursor: '#b39ff0',
      selection: 'rgba(155, 124, 224, 0.28)',
      ansi: ['#100d18', '#d45a8a', '#6fc99a', '#d4a04a', '#a582f7', '#d39cf0', '#7aa8d4', '#dcd5ea'],
      bright: ['#6e6690', '#f07a9a', '#8adcb0', '#e8c070', '#be9ffc', '#e2b8fc', '#9ac8e8', '#ffffff'],
    },
    light: {
      background: '#f7f4fc',
      foreground: '#291f36',
      cursor: '#7a5cd0',
      selection: 'rgba(155, 124, 224, 0.22)',
      ansi: ['#291f36', '#b91c1c', '#15803d', '#b45309', '#4338ca', '#7e22ce', '#0e7490', '#ece6f4'],
      bright: ['#4b5563', '#991b1b', '#166534', '#92400e', '#3730a3', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
  {
    id: 'rose',
    get name() { return t("玫瑰红") },
    get desc() { return t("热情醒目 · 玫瑰之光") },
    primary: '#e05a7a',
    secondary: '#d4a04a',
    dark: {
      background: '#191014',
      foreground: '#f0dce1',
      cursor: '#f07a98',
      selection: 'rgba(224, 90, 122, 0.28)',
      ansi: ['#160c0f', '#d45a5a', '#6fc98a', '#d4a04a', '#689df6', '#e07aa8', '#5ca8c9', '#ecd6db'],
      bright: ['#7a5d66', '#f07a7a', '#8adcb0', '#e8c070', '#93c5fd', '#f492be', '#84c4e8', '#ffffff'],
    },
    light: {
      background: '#fcf5f6',
      foreground: '#321c27',
      cursor: '#c23b5e',
      selection: 'rgba(224, 90, 122, 0.22)',
      ansi: ['#321c27', '#b91c1c', '#15803d', '#b45309', '#1d4ed8', '#7e22ce', '#0e7490', '#f0e8ea'],
      bright: ['#4b5563', '#991b1b', '#166534', '#92400e', '#1e40af', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
  {
    id: 'forest',
    get name() { return t("森林绿") },
    get desc() { return t("清爽自然 · 松林之息") },
    primary: '#4ab07a',
    secondary: '#d4a04a',
    dark: {
      background: '#0f1712',
      foreground: '#d8e5dc',
      cursor: '#5ccf8c',
      selection: 'rgba(74, 176, 122, 0.28)',
      ansi: ['#0b120e', '#d4705a', '#4caf7a', '#d4a04a', '#689df6', '#c084fc', '#4ac0a8', '#d2e0d5'],
      bright: ['#5d7a66', '#f08a74', '#7ad8a3', '#e8c070', '#93c5fd', '#d8b4fe', '#6fe0c8', '#ffffff'],
    },
    light: {
      background: '#f2f7f3',
      foreground: '#1b2a21',
      cursor: '#2f925f',
      selection: 'rgba(74, 176, 122, 0.22)',
      ansi: ['#1b2a21', '#b91c1c', '#15803d', '#b45309', '#1d4ed8', '#7e22ce', '#0f766e', '#e4eee6'],
      bright: ['#4b5563', '#991b1b', '#166534', '#92400e', '#1e40af', '#6b21a8', '#115e59', '#ffffff'],
    },
  },
  {
    id: 'graphite',
    get name() { return t("石墨黑白") },
    get desc() { return t("极简克制 · 工业灰调") },
    primary: '#9aa8b8',
    secondary: '#7a8ba0',
    dark: {
      background: '#121519',
      foreground: '#d6dce3',
      cursor: '#aeb8c6',
      selection: 'rgba(154, 168, 184, 0.28)',
      ansi: ['#0e1115', '#c96868', '#68b88a', '#c9a34a', '#90abde', '#9a78c0', '#7aa0b8', '#d0d6de'],
      bright: ['#6b7684', '#e07a7a', '#84d0a0', '#e0c070', '#adbfeb', '#b090d8', '#90b8d0', '#ffffff'],
    },
    light: {
      background: '#f5f6f8',
      foreground: '#202730',
      cursor: '#6b7684',
      selection: 'rgba(154, 168, 184, 0.25)',
      ansi: ['#202730', '#b91c1c', '#15803d', '#b45309', '#1d4ed8', '#7e22ce', '#0e7490', '#e8ecf1'],
      bright: ['#4b5563', '#991b1b', '#166534', '#92400e', '#1e40af', '#6b21a8', '#155e75', '#ffffff'],
    },
  },
]

export function presetById(id: string): ThemePreset | undefined {
  return PRESETS.find((p) => p.id === id)
}

export function defaultPreset(): ThemePreset {
  return presetById('sky')!
}

/** The pre-UI2 default, retained solely to identify untouched configurations. */
export function legacyDefaultPreset(): ThemePreset {
  return PRESETS.find((p) => p.id === 'signal') ?? PRESETS[0]
}

/** 把终端色板转换为持久化的 Theme（含 ANSI 16 色）。 */
export function paletteToTheme(p: TerminalPalette): Theme {
  const [black, red, green, yellow, blue, magenta, cyan, white] = p.ansi
  const [brightBlack, brightRed, brightGreen, brightYellow, brightBlue, brightMagenta, brightCyan, brightWhite] = p.bright
  return {
    background: p.background,
    foreground: p.foreground,
    cursor: p.cursor,
    selection: p.selection,
    bgImage: '',
    blurAmount: 12,
    textShadow: false,
    shadowBlur: 3,
    black,
    red,
    green,
    yellow,
    blue,
    magenta,
    cyan,
    white,
    brightBlack,
    brightRed,
    brightGreen,
    brightYellow,
    brightBlue,
    brightMagenta,
    brightCyan,
    brightWhite,
  }
}

/**
 * 自动根据主辅色与明暗生成 Arc 级别的终端配色板（包含背景、前景色、光标、高亮选区与 ANSI 16 色）。
 * 浅色模式生成清新通透、对比度极佳的纸质感底色；深色模式生成深邃如丝绒的暗调底色。
 */
export function generateArcTerminalPalette(
  primaryHex: string,
  secondaryHex: string,
  tone: 'light' | 'dark',
): TerminalPalette {
  const seed = normalizeSeed(primaryHex)
  const source = Hct.fromInt(argbFromHex(seed))
  const isNeutral = source.chroma < 2.5 || source.tone <= 0.5 || source.tone >= 99.5
  const isLight = tone === 'light'

  if (isNeutral) {
    const mono = PRESETS.find((p) => p.id === 'monochrome') ?? PRESETS[0]
    return isLight ? mono.light : mono.dark
  }

  const bgHct = isLight
    ? Hct.from(source.hue, Math.min(Math.max(source.chroma * 0.08, 3), 6), 98.0)
    : Hct.from(source.hue, Math.min(Math.max(source.chroma * 0.18, 8), 12), 11.0)
  const background = hexFromArgb(bgHct.toInt())

  const fgHct = isLight
    ? Hct.from(source.hue, Math.min(Math.max(source.chroma * 0.08, 4), 6), 16.0)
    : Hct.from(source.hue, Math.min(Math.max(source.chroma * 0.08, 4), 6), 91.5)
  const foreground = hexFromArgb(fgHct.toInt())

  const cursor = isLight
    ? hexFromArgb(Hct.from(source.hue, Math.max(35, source.chroma), 42).toInt())
    : hexFromArgb(Hct.from(source.hue, Math.max(35, source.chroma), 78).toInt())
  const selection = rgba(primaryHex, isLight ? 0.22 : 0.32)

  const ansi: [string, string, string, string, string, string, string, string] = isLight
    ? [foreground, '#b91c1c', '#15803d', '#b45309', '#1d4ed8', '#7e22ce', '#0e7490', '#374151']
    : ['#09090b', '#ef4444', '#22c55e', '#f59e0b', '#60a5fa', '#c084fc', '#22d3ee', '#fafafa']

  const bright: [string, string, string, string, string, string, string, string] = isLight
    ? ['#4b5563', '#dc2626', '#16a34a', '#d97706', '#2563eb', '#9333ea', '#0891b2', '#1f2937']
    : ['#52525b', '#f87171', '#4ade80', '#fbbf24', '#93c5fd', '#d8b4fe', '#67e8f9', '#ffffff']

  return {
    background,
    foreground,
    cursor,
    selection,
    ansi,
    bright,
  }
}

/**
 * 获取当前外观设置对应的活动终端色板。预设使用精修预设色板；自定义颜色实时自动派生 Arc 级配色。
 * 背景与前景色统一由 HCT 动态色板驱动，与主题色相、亮暗模式实时联动。
 */
export function resolveActiveTerminalPalette(
  appearance: UIAppearance,
  tone: 'light' | 'dark',
): TerminalPalette {
  const primary = normalizeSeed(appearance.primary || defaultPreset().primary)
  const secondary = appearance.secondary && appearance.secondary !== appearance.primary
    ? normalizeSeed(appearance.secondary)
    : deriveHarmonizedSecondary(primary)
  const atmo = appearance.atmosphere ?? appearance.colorIntensity ?? 75
  const sep = appearance.surfaceSeparation ?? 65
  const roles = deriveColorRoles({seed: primary, tone, atmosphere: atmo, separation: sep})

  if (appearance.mode === 'preset' && appearance.presetId) {
    const p = presetById(appearance.presetId)
    if (p) {
      const pal = tone === 'light' ? p.light : p.dark
      return {
        ...pal,
        background: roles.terminalBg,
        foreground: roles.terminalFg,
      }
    }
  }

  const arc = generateArcTerminalPalette(primary, secondary, tone)
  return {
    ...arc,
    background: roles.terminalBg,
    foreground: roles.terminalFg,
  }
}
