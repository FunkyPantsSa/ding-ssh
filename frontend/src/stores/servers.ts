import {defineStore} from 'pinia'
import {sshService} from '../services/ssh'
import type {ServerNode, ServerTestResult} from '../types'

export const useServersStore = defineStore('servers', {
  state: () => ({
    servers: [] as ServerNode[],
    loading: false,
    // 在线状态测试结果缓存：nodeId -> 最近一次测试结果（常驻显示在节点行内）
    testResults: {} as Record<string, ServerTestResult>,
    // 正在测试的节点 ID 集合
    testing: {} as Record<string, boolean>,
    testingAll: false,
  }),
  actions: {
    async load() {
      this.loading = true
      try {
        this.servers = await sshService.getServers()
      } finally {
        this.loading = false
      }
    },
    async save(node: ServerNode) {
      const saved = await sshService.saveServer(node)
      const idx = this.servers.findIndex((s) => s.id === saved.id)
      if (idx >= 0) this.servers[idx] = saved
      else this.servers.push(saved)
      return saved
    },
    async remove(id: string) {
      await sshService.deleteServer(id)
      this.servers = this.servers.filter((s) => s.id !== id)
      delete this.testResults[id]
      delete this.testing[id]
    },
    async testOne(node: ServerNode) {
      this.testing[node.id] = true
      try {
        const res = await sshService.testServer(node)
        this.testResults[node.id] = res
      } finally {
        this.testing[node.id] = false
      }
    },
    async testAll() {
      if (!this.servers.length || this.testingAll) return
      this.testingAll = true
      try {
        const results = await sshService.testServers(this.servers)
        for (const r of results) {
          this.testResults[r.nodeId] = r
        }
      } finally {
        this.testingAll = false
      }
    },
    isTesting(node: ServerNode): boolean {
      return this.testingAll || !!this.testing[node.id]
    },
  },
})
