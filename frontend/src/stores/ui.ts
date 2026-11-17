import {defineStore} from 'pinia'

export type ViewName = 'home' | 'workspace' | 'servers' | 'tunnel' | 'settings'

export type SettingsSection = 'general' | 'hotkeys' | 'connectionInfo' | 'theme' | 'credentials' | 'security' | 'debug' | 'logs' | 'audit' | 'migrate' | 'about'

// 主导航视图切换（左侧导航轨）。
export const useUIStore = defineStore('ui', {
  state: () => ({
    view: 'home' as ViewName,
    cmdOpen: false,
    settingsSection: 'general' as SettingsSection,
    newServerTick: 0,
    // 从任意页面请求编辑某台服务器：ServerList 常驻，由其打开编辑弹窗
    editServerId: '',
    editServerTick: 0,
    // 终端页左侧「快速连接」侧边栏是否展开
    terminalSidebarOpen: false,
  }),
  actions: {
    showHome() { this.view = 'home'; this.terminalSidebarOpen = false },
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
    showSettings(section?: SettingsSection) {
      if (section) this.settingsSection = section
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
    requestEditServer(id: string) {
      if (!id) return
      this.editServerId = id
      this.editServerTick++
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
    // 快速连接入口：不在工作区时切回工作区并展开，已在工作区时切换展开/收起
    toggleQuickConnect() {
      if (this.view !== 'workspace') {
        this.view = 'workspace'
        this.terminalSidebarOpen = true
        return
      }
      this.terminalSidebarOpen = !this.terminalSidebarOpen
    },
  },
})
