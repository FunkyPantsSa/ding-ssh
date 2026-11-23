import {defineStore} from 'pinia'
import {groupService} from '../services/groups'
import {groupAncestors, groupParent, normalizeGroupPath, retargetGroupPath} from '../utils/groupTree'

function sortPaths(list: string[]): string[] {
  return [...new Set(list)].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN', {numeric: true}))
}

/** 把 path 及其所有祖先加入列表（后端也会补齐，这里用于乐观更新）。 */
function withAncestors(list: string[], path: string): string[] {
  const next = [...list]
  for (const ancestor of groupAncestors(path)) next.push(ancestor)
  next.push(path)
  return next
}

export const useGroupsStore = defineStore('groups', {
  state: () => ({
    list: [] as string[],
    loaded: false,
  }),
  actions: {
    async load() {
      this.list = sortPaths(await groupService.list())
      this.loaded = true
    },
    async add(path: string) {
      const target = normalizeGroupPath(path)
      if (!target) return
      await groupService.add(target)
      this.list = sortPaths(withAncestors(this.list, target))
    },
    async rename(path: string, newLeaf: string) {
      const oldPath = normalizeGroupPath(path)
      const parent = groupParent(oldPath)
      const newPath = normalizeGroupPath([parent, newLeaf.trim()].filter(Boolean).join('/'))
      if (!oldPath || !newPath) return
      await groupService.rename(oldPath, newLeaf.trim())
      this.list = sortPaths(
        this.list.map((item) => retargetGroupPath(item, oldPath, newPath)),
      )
    },
    async remove(path: string, keepChildren = false) {
      const target = normalizeGroupPath(path)
      if (!target) return
      await groupService.remove(target, keepChildren)
      const parent = groupParent(target)
      const next = this.list
        .filter((item) => item !== target)
        .map((item) => {
          if (!keepChildren || !item.startsWith(`${target}/`)) return item
          const moved = `${parent}/${item.slice(target.length + 1)}`.replace(/^\/+/, '')
          return normalizeGroupPath(moved)
        })
      this.list = sortPaths(next)
    },
    /** 用后端结果整体校准（重命名/删除后调用，避免本地前缀计算遗漏）。 */
    replace(list: string[]) {
      this.list = sortPaths(list)
      this.loaded = true
    },
  },
})
