// 封装分组管理的 Wails 绑定。
import {AddGroup, GetGroups, RemoveGroup, RenameGroup} from '../../wailsjs/go/main/App'

export const groupService = {
  list: (): Promise<string[]> => GetGroups(),
  add: (name: string): Promise<void> => AddGroup(name),
  rename: (oldName: string, newName: string): Promise<void> => RenameGroup(oldName, newName),
  remove: (name: string): Promise<void> => RemoveGroup(name),
}
