import {defineStore} from 'pinia'

export type ViewName = 'workspace' | 'servers' | 'tunnel' | 'settings'

// 主导航视图切换（左侧导航轨）。
export const useUIStore = defineStore('ui', {
  state: () => ({
    view: 'workspace' as ViewName,
    cmdOpen: false,
    newServerTick: 0,
    // 终端页左侧「快速连接」侧边栏是否展开
    terminalSidebarOpen: false,
  }),
  actions: {
    showWorkspace() {
      this.view = 'workspace'
      this.terminalSidebarOpen = false
    },
    showServers() {
      this.view = 'servers'
      this.terminalSidebarOpen = false
    },
    showTunnel() {
      this.view = 'tunnel'
      this.terminalSidebarOpen = false
    },
    showSettings() {
      this.view = 'settings'
      this.terminalSidebarOpen = false
    },
    openCommandPalette() {
      this.cmdOpen = true
    },
    closeCommandPalette() {
      this.cmdOpen = false
    },
    requestNewServer() {
      this.showServers()
      this.newServerTick++
    },
    openTerminalSidebar() {
      this.terminalSidebarOpen = true
    },
    closeTerminalSidebar() {
      this.terminalSidebarOpen = false
    },
    toggleTerminalSidebar() {
      this.terminalSidebarOpen = !this.terminalSidebarOpen
    },
  },
})
