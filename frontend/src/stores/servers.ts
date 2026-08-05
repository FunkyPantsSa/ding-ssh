import {defineStore} from 'pinia'
import {sshService} from '../services/ssh'
import type {ServerNode} from '../types'

export const useServersStore = defineStore('servers', {
  state: () => ({
    servers: [] as ServerNode[],
    loading: false,
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
    },
  },
})
