import {defineStore} from 'pinia'
import {DEFAULT_COMPLETION_NAV_HOTKEY} from '../completion/hotkey'
import {settingsService} from '../services/settings'
import type {Theme} from '../types'

// 默认终端主题（与 Go 端 models.DefaultTheme 保持一致）。
export function defaultTheme(): Theme {
  return {
    background: '#0c1016',
    foreground: '#d4dae3',
    cursor: '#3ec4b4',
    selection: 'rgba(42, 168, 154, 0.28)',
    bgImage: '',
    blurAmount: 12,
    textShadow: false,
    shadowBlur: 3,
  }
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    logEnabled: false,
    copyOnSelect: false,
    webGLEnabled: true,
    completionEnabled: true,
    completionNavHotkey: DEFAULT_COMPLETION_NAV_HOTKEY,
    completionPanelLimit: 8,
    sftpToTerminalSync: true,
    terminalToSftpSync: true,
    uiScale: 100,
    theme: defaultTheme() as Theme,
    autoReconnect: true,
    keepAliveEnabled: true,
    loaded: false,
  }),
  actions: {
    async load() {
      const settings = await settingsService.getSettings()
      this.logEnabled = settings.logEnabled
      this.copyOnSelect = settings.copyOnSelect ?? false
      this.webGLEnabled = settings.webGLEnabled ?? true
      this.completionEnabled = settings.completionEnabled ?? true
      this.completionNavHotkey = settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY
      this.completionPanelLimit = clampPanelLimit(settings.completionPanelLimit)
      this.sftpToTerminalSync = settings.sftpToTerminalSync ?? true
      this.terminalToSftpSync = settings.terminalToSftpSync ?? true
      this.uiScale = clampUIScale(settings.uiScale)
      this.theme = {...defaultTheme(), ...(settings.theme ?? {})}
      this.autoReconnect = settings.autoReconnect ?? true
      this.keepAliveEnabled = settings.keepAliveEnabled ?? true
      this.loaded = true
    },
    async setLogEnabled(v: boolean) {
      this.logEnabled = v
      await this.save()
    },
    async setCopyOnSelect(v: boolean) {
      this.copyOnSelect = v
      await this.save()
    },
    async setWebGLEnabled(v: boolean) {
      this.webGLEnabled = v
      await this.save()
    },
    async setCompletionEnabled(v: boolean) {
      this.completionEnabled = v
      await this.save()
    },
    async setCompletionNavHotkey(v: string) {
      this.completionNavHotkey = v || DEFAULT_COMPLETION_NAV_HOTKEY
      await this.save()
    },
    async setCompletionPanelLimit(v: number) {
      this.completionPanelLimit = clampPanelLimit(v)
      await this.save()
    },
    async setSftpToTerminalSync(v: boolean) {
      this.sftpToTerminalSync = v
      await this.save()
    },
    async setTerminalToSftpSync(v: boolean) {
      this.terminalToSftpSync = v
      await this.save()
    },
    async setUIScale(v: number) {
      this.uiScale = clampUIScale(v)
      await this.save()
    },
    async setTheme(theme: Theme) {
      this.theme = theme
      await this.save()
    },
    async setAutoReconnect(v: boolean) {
      this.autoReconnect = v
      await this.save()
    },
    async setKeepAliveEnabled(v: boolean) {
      this.keepAliveEnabled = v
      await this.save()
    },
    async save() {
      await settingsService.saveSettings({
        logEnabled: this.logEnabled,
        copyOnSelect: this.copyOnSelect,
        webGLEnabled: this.webGLEnabled,
        completionEnabled: this.completionEnabled,
        completionNavHotkey: this.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY,
        completionPanelLimit: clampPanelLimit(this.completionPanelLimit),
        sftpToTerminalSync: this.sftpToTerminalSync,
        terminalToSftpSync: this.terminalToSftpSync,
        uiScale: clampUIScale(this.uiScale),
        theme: this.theme,
        autoReconnect: this.autoReconnect,
        keepAliveEnabled: this.keepAliveEnabled,
      })
    },
  },
})

function clampPanelLimit(v: number | undefined): number {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0) return 8
  return Math.max(3, Math.min(30, Math.round(n)))
}

function clampUIScale(v: number | undefined): number {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0) return 100
  return Math.max(80, Math.min(150, Math.round(n)))
}