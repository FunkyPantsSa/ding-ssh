// 封装分组管理的 Wails 绑定。
// 分组使用完整路径（例如 生产环境/华东）作为标识。
import {AddGroup, GetGroups, RemoveGroup, RenameGroup} from '../../wailsjs/go/main/App'

export const groupService = {
  list: (): Promise<string[]> => GetGroups(),
  /** 新建分组路径；后端会补齐祖先层级。 */
  add: (path: string): Promise<void> => AddGroup(path),
  /** 只修改最后一段名称；后端级联更新子孙分组与服务器。 */
  rename: (path: string, newLeaf: string): Promise<void> => RenameGroup(path, newLeaf),
  /** keepChildren=true 时保留子分组并上移一级，false 时递归删除。 */
  remove: (path: string, keepChildren = false): Promise<void> => RemoveGroup(path, keepChildren),
}
