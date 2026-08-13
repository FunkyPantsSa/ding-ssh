import {defineStore} from 'pinia'

export type ViewName = 'workspace' | 'tunnel' | 'settings'

// 主导航视图切换（左侧导航轨）。
export const useUIStore = defineStore('ui', {
  state: () => ({
    view: 'workspace' as ViewName,
    cmdOpen: false,
    newServerTick: 0,
  }),
  actions: {
    showWorkspace() {
      this.view = 'workspace'
    },
    showTunnel() {
      this.view = 'tunnel'
    },
    showSettings() {
      this.view = 'settings'
    },
    openCommandPalette() {
      this.cmdOpen = true
    },
    closeCommandPalette() {
      this.cmdOpen = false
    },
    requestNewServer() {
      this.view = 'workspace'
      this.newServerTick++
    },
  },
})
