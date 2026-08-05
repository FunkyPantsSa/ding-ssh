import {defineStore} from 'pinia'
import {settingsService} from '../services/settings'
import type {Theme} from '../types'

// 默认终端主题（与 Go 端 models.DefaultTheme 保持一致）。
export function defaultTheme(): Theme {
  return {
    background: '#0b1120',
    foreground: '#dbe4f0',
    cursor: '#38bdf8',
    selection: 'rgba(56, 189, 248, 0.25)',
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
    theme: defaultTheme() as Theme,
    loaded: false,
  }),
  actions: {
    async load() {
      const settings = await settingsService.getSettings()
      this.logEnabled = settings.logEnabled
      this.copyOnSelect = settings.copyOnSelect ?? false
      this.theme = {...defaultTheme(), ...(settings.theme ?? {})}
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
    async setTheme(theme: Theme) {
      this.theme = theme
      await this.save()
    },
    async save() {
      await settingsService.saveSettings({
        logEnabled: this.logEnabled,
        copyOnSelect: this.copyOnSelect,
        theme: this.theme,
      })
    },
  },
})
