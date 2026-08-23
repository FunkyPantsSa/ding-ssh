import {defineStore} from 'pinia'
import type {ServerNode, SessionTab} from '../types'

let seq = 0

export const useSessionsStore = defineStore('sessions', {
  state: () => ({
    tabs: [] as SessionTab[],
    activeId: '',
    sftpVisible: true, // 右侧 SFTP 面板显隐
    rightPanel: 'sftp' as 'sftp' | 'sysinfo', // 右侧面板模式
  }),
  getters: {
    activeTab(): SessionTab | undefined {
      return this.tabs.find((t) => t.clientId === this.activeId)
    },
  },
  actions: {
    openTab(node: ServerNode): SessionTab {
      const tab: SessionTab = {
        clientId: `tab-${Date.now()}-${seq++}`,
        sessionId: '',
        serverName: node.name || `${node.user}@${node.host}`,
        host: node.host,
        user: node.user,
        node,
        status: 'connecting',
        createdAt: Date.now(),
        sftpPath: '/',
        kind: 'ssh',
      }
      this.tabs.push(tab)
      this.activeId = tab.clientId
      return tab
    },
    openLocalTab(shellLabel = '本机'): SessionTab {
      const node: ServerNode = {
        id: '__local__',
        name: shellLabel,
        group: '',
        host: 'localhost',
        port: 0,
        user: '',
        authType: 'password',
        bgImage: '',
        blurAmount: 0,
        envVars: {},
      }
      const tab: SessionTab = {
        clientId: `tab-${Date.now()}-${seq++}`,
        sessionId: '',
        serverName: shellLabel,
        host: 'localhost',
        user: '',
        node,
        status: 'connecting',
        createdAt: Date.now(),
        sftpPath: '/',
        kind: 'local',
      }
      this.tabs.push(tab)
      this.activeId = tab.clientId
      return tab
    },
    bindSession(clientId: string, sessionId: string) {
      const tab = this.tabs.find((t) => t.clientId === clientId)
      if (tab) tab.sessionId = sessionId
    },
    setStatus(clientId: string, status: SessionTab['status'], message?: string) {
      const tab = this.tabs.find((t) => t.clientId === clientId)
      if (tab) {
        tab.status = status
        if (message !== undefined) tab.message = message
      }
    },
    setSftpPath(clientId: string, dir: string) {
      const tab = this.tabs.find((t) => t.clientId === clientId)
      if (tab) tab.sftpPath = dir
    },
    closeTab(clientId: string) {
      const idx = this.tabs.findIndex((t) => t.clientId === clientId)
      if (idx < 0) return
      this.tabs.splice(idx, 1)
      if (this.activeId === clientId) {
        this.activeId = this.tabs[idx]?.clientId ?? this.tabs[idx - 1]?.clientId ?? ''
      }
    },
    closeAll() {
      this.tabs = []
      this.activeId = ''
    },
    toggleSftp() {
      this.sftpVisible = !this.sftpVisible
    },
    showRightPanel(panel: 'sftp' | 'sysinfo') {
      this.rightPanel = panel
      this.sftpVisible = true
    },
  },
})
