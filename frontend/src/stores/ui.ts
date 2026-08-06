import {defineStore} from 'pinia'

export type ViewName = 'workspace' | 'tunnel' | 'settings'

// 主导航视图切换（左侧边栏底部导航）。
export const useUIStore = defineStore('ui', {
  state: () => ({
    view: 'workspace' as ViewName,
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
  },
})
