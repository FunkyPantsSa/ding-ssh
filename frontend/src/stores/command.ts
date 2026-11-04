import {defineStore} from 'pinia'

const RECENT_KEY = 'ding-ssh:command-recent'
const RECENT_LIMIT = 10

function loadRecent(): string[] {
  try {
    const value = JSON.parse(localStorage.getItem(RECENT_KEY) || '[]')
    return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string').slice(0, RECENT_LIMIT) : []
  } catch {
    return []
  }
}

export const useCommandStore = defineStore('command', {
  state: () => ({recentIds: loadRecent()}),
  actions: {
    record(id: string) {
      const key = id.trim()
      if (!key) return
      this.recentIds = [key, ...this.recentIds.filter((item) => item !== key)].slice(0, RECENT_LIMIT)
      this.persist()
    },
    clear() {
      this.recentIds = []
      this.persist()
    },
    persist() {
      try {
        localStorage.setItem(RECENT_KEY, JSON.stringify(this.recentIds))
      } catch {
        // Storage may be unavailable; recent commands still work for this session.
      }
    },
  },
})
