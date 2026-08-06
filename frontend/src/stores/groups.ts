import {defineStore} from 'pinia'
import {groupService} from '../services/groups'

export const useGroupsStore = defineStore('groups', {
  state: () => ({
    list: [] as string[],
    loaded: false,
  }),
  actions: {
    async load() {
      this.list = await groupService.list()
      this.loaded = true
    },
    async add(name: string) {
      await groupService.add(name)
      if (!this.list.includes(name)) this.list.push(name)
      this.list.sort((a, b) => a.localeCompare(b))
    },
    async rename(oldName: string, newName: string) {
      await groupService.rename(oldName, newName)
      const idx = this.list.indexOf(oldName)
      if (idx >= 0) this.list[idx] = newName
      else this.list.push(newName)
      this.list.sort((a, b) => a.localeCompare(b))
    },
    async remove(name: string) {
      await groupService.remove(name)
      this.list = this.list.filter((g) => g !== name)
    },
  },
})
