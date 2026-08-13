// 命令历史 Wails 绑定封装。
import {AddCommandHistory, ClearCommandHistory, QueryCommandHistory} from '../../wailsjs/go/main/App'
import type {CommandSuggestion} from '../types'

export const historyService = {
  add: (serverId: string, command: string): void => {
    try {
      void AddCommandHistory(serverId, command)
    } catch {
      // 写库失败静默降级
    }
  },
  query: async (serverId: string, prefix: string, limit = 8): Promise<CommandSuggestion[]> => {
    try {
      const list = await QueryCommandHistory(serverId, prefix, limit)
      return list ?? []
    } catch {
      return []
    }
  },
  /** serverId 为空则清空全部历史 */
  clear: async (serverId = ''): Promise<void> => {
    await ClearCommandHistory(serverId)
  },
}
