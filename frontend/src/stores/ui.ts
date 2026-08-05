import {defineStore} from 'pinia'

export type ViewName = 'workspace' | 'settings'

// 主导航视图切换（左侧边栏底部导航）。
export const useUIStore = defineStore('ui', {
  state: () => ({
    view: 'workspace' as ViewName,
  }),
  actions: {
    showWorkspace() {
      this.view = 'workspace'
    },
    showSettings() {
      this.view = 'settings'
    },
  },
})
