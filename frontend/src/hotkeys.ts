import {t} from './i18n'
export type HotkeyScope = 'global' | 'workspace' | 'terminal'

export interface HotkeyDefinition {
  id: string
  keys: string
  scope: HotkeyScope
  label: string
}

/** Display and matching metadata share this single registry. */
export const HOTKEYS: readonly HotkeyDefinition[] = [
  {id: 'command-palette', keys: 'Mod+K', scope: 'global', get label() { return t("打开或关闭命令面板") }},
  {id: 'close-overlay', keys: 'Escape', scope: 'global', get label() { return t("关闭当前浮层") }},
  {id: 'terminal-search', keys: 'Mod+F', scope: 'terminal', get label() { return t('搜索终端输出') }},
  {id: 'close-tab', keys: 'Mod+W', scope: 'workspace', get label() { return t("关闭当前标签") }},
  ...Array.from({length: 9}, (_, index): HotkeyDefinition => ({
    id: `switch-tab-${index + 1}`,
    keys: `Mod+${index + 1}`,
    scope: 'global',
    get label() { return t("切换到第 {0} 个标签", [index + 1]) },
  })),
  {id: 'font-inc', keys: 'Mod+=', scope: 'terminal', get label() { return t("增大终端字号") }},
  {id: 'font-dec', keys: 'Mod+-', scope: 'terminal', get label() { return t("减小终端字号") }},
  {id: 'font-reset', keys: 'Mod+0', scope: 'terminal', get label() { return t("重置终端字号") }},
  {id: 'toggle-sidebar', keys: 'Mod+B', scope: 'global', get label() { return t("展开或收起左侧导航") }},
  {id: 'toggle-context', keys: 'Mod+Shift+B', scope: 'workspace', get label() { return t("展开或收起右侧工具栏") }},
]

export function hotkeyLabel(keys: string): string {
  const mod = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'
  return keys.replace('Mod', mod).replaceAll('+', ' + ')
}
