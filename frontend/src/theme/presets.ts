// 预设主题库：7 套精选配色，每套含 UI 主/辅色与深/浅两套终端色板。
// 终端色板用于映射 ANSI 16 色，让 ls / vim / 提示符等程序输出跟随主题。
import type {Theme} from '../types'

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
    id: 'signal',
    name: '信号青绿',
    desc: '经典默认 · 连接即锁合',
    primary: '#3ec4b4',
    secondary: '#c97a4a',
    dark: {
      background: '#0c1016',
      foreground: '#d4dae3',
      cursor: '#3ec4b4',
      selection: 'rgba(42, 168, 154, 0.28)',
      ansi: ['#0c1016', '#d45a5a', '#4caf7a', '#d4a04a', '#5c8fe6', '#b26cd9', '#3ec4b4', '#d4dae3'],
      bright: ['#6b7684', '#f08080', '#7ad8a3', '#e8c070', '#8fb0f2', '#d39cf0', '#6dd9cb', '#ffffff'],
    },
    light: {
      background: '#f4f6f8',
      foreground: '#22303e',
      cursor: '#2aa89a',
      selection: 'rgba(42, 168, 154, 0.22)',
      ansi: ['#22303e', '#c23b3b', '#2e8f5e', '#b07c1f', '#3a66c9', '#8d4bb8', '#1c8f84', '#e8ecf1'],
      bright: ['#5d6b7c', '#d45a5a', '#3aa76d', '#c9921f', '#4f7de0', '#a05fd4', '#2aa89a', '#ffffff'],
    },
  },
  {
    id: 'copper',
    name: '日落铜橙',
    desc: '暖色系 · 运维如坐席',
    primary: '#d97b3f',
    secondary: '#c9a24a',
    dark: {
      background: '#120e0c',
      foreground: '#e0d6cc',
      cursor: '#e08a4a',
      selection: 'rgba(217, 123, 63, 0.30)',
      ansi: ['#120e0c', '#d45a4a', '#9caf5c', '#d4a04a', '#b98a5c', '#c97a6a', '#e08a4a', '#e0d6cc'],
      bright: ['#6e6357', '#f0806a', '#b8cd7a', '#e8c070', '#d4a86e', '#e09a8a', '#f0b07a', '#fff5ec'],
    },
    light: {
      background: '#f7f3ee',
      foreground: '#3a2e22',
      cursor: '#c97030',
      selection: 'rgba(217, 123, 63, 0.22)',
      ansi: ['#3a2e22', '#b23f2e', '#7f9250', '#b07c1f', '#9a6b3e', '#a34f3f', '#c97030', '#efe6da'],
      bright: ['#6e6357', '#d45a4a', '#9cb763', '#c9921f', '#b98951', '#c97a6a', '#e08a4a', '#ffffff'],
    },
  },
  {
    id: 'ocean',
    name: '深海蓝',
    desc: '沉稳冷静 · 深蓝之海',
    primary: '#4a8fe0',
    secondary: '#3ec4b4',
    dark: {
      background: '#0a0f16',
      foreground: '#cdd8e8',
      cursor: '#6fb0f0',
      selection: 'rgba(74, 143, 224, 0.28)',
      ansi: ['#0a0f16', '#d45a6a', '#4caf8a', '#d4a04a', '#5c8fe6', '#a07ce0', '#4a9bd4', '#cdd8e8'],
      bright: ['#5d6e8a', '#f07a8a', '#7ad8b0', '#e8c070', '#8fb0f2', '#c0a0f2', '#6fb0f0', '#ffffff'],
    },
    light: {
      background: '#f2f6fc',
      foreground: '#1f2c40',
      cursor: '#3a7bd4',
      selection: 'rgba(74, 143, 224, 0.22)',
      ansi: ['#1f2c40', '#b23b4e', '#2e8f6e', '#b07c1f', '#3a66c9', '#7a5cc0', '#3580c4', '#e8eef7'],
      bright: ['#5d6e8a', '#d45a6a', '#3aa78a', '#c9921f', '#4f7de0', '#9a7ce0', '#4a9bd4', '#ffffff'],
    },
  },
  {
    id: 'violet',
    name: '紫罗兰',
    desc: '神秘优雅 · 电子紫调',
    primary: '#9b7ce0',
    secondary: '#e08ab8',
    dark: {
      background: '#100d18',
      foreground: '#dcd5ea',
      cursor: '#b39ff0',
      selection: 'rgba(155, 124, 224, 0.28)',
      ansi: ['#100d18', '#d45a8a', '#6fc99a', '#d4a04a', '#8f6ae0', '#b26cd9', '#7aa8d4', '#dcd5ea'],
      bright: ['#6e6690', '#f07a9a', '#8adcb0', '#e8c070', '#a88ff0', '#d39cf0', '#9ac8e8', '#ffffff'],
    },
    light: {
      background: '#f7f4fc',
      foreground: '#32283f',
      cursor: '#7a5cd0',
      selection: 'rgba(155, 124, 224, 0.22)',
      ansi: ['#32283f', '#b23b66', '#4aa878', '#b07c1f', '#6a4ac0', '#8d4bb8', '#5c8ac0', '#ece6f4'],
      bright: ['#6e6690', '#d45a8a', '#5cc090', '#c9921f', '#8a6ce0', '#a05fd4', '#7aa8d4', '#ffffff'],
    },
  },
  {
    id: 'rose',
    name: '玫瑰红',
    desc: '热情醒目 · 玫瑰之光',
    primary: '#e05a7a',
    secondary: '#d4a04a',
    dark: {
      background: '#160c0f',
      foreground: '#ecd6db',
      cursor: '#f07a98',
      selection: 'rgba(224, 90, 122, 0.28)',
      ansi: ['#160c0f', '#d45a5a', '#6fc98a', '#d4a04a', '#a878c9', '#d45a8a', '#5ca8c9', '#ecd6db'],
      bright: ['#7a5d66', '#f07a7a', '#8adcb0', '#e8c070', '#c0a0e8', '#f07aa8', '#84c4e8', '#ffffff'],
    },
    light: {
      background: '#fcf5f6',
      foreground: '#3a2530',
      cursor: '#c23b5e',
      selection: 'rgba(224, 90, 122, 0.22)',
      ansi: ['#3a2530', '#b23b3b', '#4aa878', '#b07c1f', '#8a66b0', '#b23b66', '#4688b0', '#f0e8ea'],
      bright: ['#7a5d66', '#d45a5a', '#5cc090', '#c9921f', '#a888c9', '#d45a8a', '#5ca8c9', '#ffffff'],
    },
  },
  {
    id: 'forest',
    name: '森林绿',
    desc: '清爽自然 · 松林之息',
    primary: '#4ab07a',
    secondary: '#d4a04a',
    dark: {
      background: '#0b120e',
      foreground: '#d2e0d5',
      cursor: '#5ccf8c',
      selection: 'rgba(74, 176, 122, 0.28)',
      ansi: ['#0b120e', '#d4705a', '#4caf7a', '#d4a04a', '#6a9ad4', '#b26cd9', '#4ac0a8', '#d2e0d5'],
      bright: ['#5d7a66', '#f08a74', '#7ad8a3', '#e8c070', '#8fb0f2', '#d39cf0', '#6fe0c8', '#ffffff'],
    },
    light: {
      background: '#f2f7f3',
      foreground: '#22332a',
      cursor: '#2f925f',
      selection: 'rgba(74, 176, 122, 0.22)',
      ansi: ['#22332a', '#b04a3a', '#2e8f5e', '#b07c1f', '#4a78c0', '#8d4bb8', '#2f92a8', '#e4eee6'],
      bright: ['#5d7a66', '#d46a4a', '#3aa76d', '#c9921f', '#6a9ad4', '#a05fd4', '#4ac0a8', '#ffffff'],
    },
  },
  {
    id: 'graphite',
    name: '石墨黑白',
    desc: '极简克制 · 工业灰调',
    primary: '#9aa8b8',
    secondary: '#7a8ba0',
    dark: {
      background: '#0e1115',
      foreground: '#d0d6de',
      cursor: '#aeb8c6',
      selection: 'rgba(154, 168, 184, 0.28)',
      ansi: ['#0e1115', '#c96868', '#68b88a', '#c9a34a', '#7a90c0', '#9a78c0', '#7aa0b8', '#d0d6de'],
      bright: ['#6b7684', '#e07a7a', '#84d0a0', '#e0c070', '#90a8e0', '#b090d8', '#90b8d0', '#ffffff'],
    },
    light: {
      background: '#f5f6f8',
      foreground: '#2a323c',
      cursor: '#6b7684',
      selection: 'rgba(154, 168, 184, 0.25)',
      ansi: ['#2a323c', '#b04a4a', '#3a8a5e', '#a07c1f', '#4a68b0', '#7a5ab0', '#4a80a0', '#e8ecf1'],
      bright: ['#6b7684', '#c95a5a', '#4aaa78', '#b08a1f', '#6880d0', '#9070c9', '#6098b8', '#ffffff'],
    },
  },
]

export function presetById(id: string): ThemePreset | undefined {
  return PRESETS.find((p) => p.id === id)
}

export function defaultPreset(): ThemePreset {
  return PRESETS[0]
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
