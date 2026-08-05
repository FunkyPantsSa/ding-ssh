import {defineStore} from 'pinia'
import {credentialService} from '../services/credentials'
import type {Credential} from '../types'

export const useCredentialsStore = defineStore('credentials', {
  state: () => ({
    list: [] as Credential[],
    loaded: false,
  }),
  actions: {
    async load() {
      this.list = await credentialService.list()
      this.loaded = true
    },
    async save(c: Credential) {
      const saved = await credentialService.save(c)
      const idx = this.list.findIndex((x) => x.id === saved.id)
      if (idx >= 0) this.list[idx] = saved
      else this.list.push(saved)
      return saved
    },
    async remove(id: string) {
      await credentialService.remove(id)
      this.list = this.list.filter((x) => x.id !== id)
    },
  },
})
