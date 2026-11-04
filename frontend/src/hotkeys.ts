export type HotkeyScope = 'global' | 'workspace' | 'terminal'

export interface HotkeyDefinition {
  id: string
  keys: string
  scope: HotkeyScope
  label: string
}

/** Display and matching metadata share this single registry. */
export const HOTKEYS: readonly HotkeyDefinition[] = [
  {id: 'command-palette', keys: 'Mod+K', scope: 'global', label: '打开或关闭命令面板'},
  {id: 'close-overlay', keys: 'Escape', scope: 'global', label: '关闭当前浮层'},
  {id: 'close-tab', keys: 'Mod+W', scope: 'workspace', label: '关闭当前标签'},
  ...Array.from({length: 9}, (_, index): HotkeyDefinition => ({
    id: `switch-tab-${index + 1}`,
    keys: `Mod+${index + 1}`,
    scope: 'global',
    label: `切换到第 ${index + 1} 个标签`,
  })),
  {id: 'font-inc', keys: 'Mod+=', scope: 'terminal', label: '增大终端字号'},
  {id: 'font-dec', keys: 'Mod+-', scope: 'terminal', label: '减小终端字号'},
  {id: 'font-reset', keys: 'Mod+0', scope: 'terminal', label: '重置终端字号'},
  {id: 'toggle-sidebar', keys: 'Mod+B', scope: 'global', label: '展开或收起左侧导航'},
  {id: 'toggle-context', keys: 'Mod+Shift+B', scope: 'workspace', label: '展开或收起右侧工具栏'},
]

export function hotkeyLabel(keys: string): string {
  const mod = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'
  return keys.replace('Mod', mod).replaceAll('+', ' + ')
}
