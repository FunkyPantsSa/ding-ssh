// 分组路径工具：嵌套分组以完整路径作为唯一标识（例如 生产环境/华东/杭州）。
// 这里只负责展示层的建树、计数与过滤；路径合法性的最终裁决在后端。

export interface GroupNode {
  /** 完整路径，例如 生产环境/华东/杭州 */
  path: string
  /** 当前层名称，例如 杭州 */
  name: string
  /** 父路径；根分组为空串 */
  parent: string
  /** 层级，根分组为 1 */
  depth: number
  children: GroupNode[]
  /** 直接属于该分组的服务器数 */
  direct: number
  /** 该分组及所有子孙分组的服务器总数 */
  total: number
}

export interface GroupTreeNodeSource {
  group: string
}

/** 拆分路径（兼容旧数据里可能出现的空段；空段会被忽略）。 */
export function splitGroupPath(path: string): string[] {
  return (path || '')
    .split('/')
    .map((part) => part.trim())
    .filter(Boolean)
}

/** 规范化展示用路径：去掉空段与首尾斜杠。 */
export function normalizeGroupPath(path: string): string {
  return splitGroupPath(path).join('/')
}

/** 当前层名称；未分组返回空串。 */
export function groupName(path: string): string {
  const parts = splitGroupPath(path)
  return parts.length ? parts[parts.length - 1] : ''
}

/** 父路径；根分组返回空串。 */
export function groupParent(path: string): string {
  const parts = splitGroupPath(path)
  parts.pop()
  return parts.join('/')
}

/** 层级：根分组为 1，未分组为 0。 */
export function groupDepth(path: string): number {
  return splitGroupPath(path).length
}

/** 从根到父级的全部祖先路径（不含自身）。 */
export function groupAncestors(path: string): string[] {
  const parts = splitGroupPath(path)
  const result: string[] = []
  for (let i = 1; i < parts.length; i++) result.push(parts.slice(0, i).join('/'))
  return result
}

/** path 是否等于 ancestor 或是其子孙；ancestor 为空串时只匹配空串。 */
export function isGroupInSubtree(path: string, ancestor: string): boolean {
  const target = normalizeGroupPath(path)
  const base = normalizeGroupPath(ancestor)
  if (!base) return target === ''
  return target === base || target.startsWith(`${base}/`)
}

/** 服务器是否属于某个分组范围；path 为空串表示“未分组”，不是“全部”。 */
export function isServerInGroup(serverGroup: string, path: string): boolean {
  return isGroupInSubtree(serverGroup, path)
}

/** 相对路径：在父组视图里显示子组名，而不是重复整条路径。 */
export function relativeGroupPath(path: string, base: string): string {
  const target = normalizeGroupPath(path)
  const root = normalizeGroupPath(base)
  if (!root) return target
  if (target === root) return ''
  return target.startsWith(`${root}/`) ? target.slice(root.length + 1) : target
}

function compareGroupName(a: string, b: string): number {
  try {
    return a.localeCompare(b, 'zh-Hans-CN', {numeric: true, sensitivity: 'base'})
  } catch {
    return a.localeCompare(b)
  }
}

/**
 * 由分组列表与服务器列表构建分组树。
 *
 * - groups 里是显式创建的分组（可包含空分组）；
 * - servers[].group 里是实际归属；即使没有显式建过父分组，也会补出隐式祖先；
 * - 同一路径只出现一次，计数按完整路径累加。
 */
export function buildGroupTree(paths: Iterable<string>, servers: GroupTreeNodeSource[] = []): GroupNode[] {
  const canonical = new Set<string>()
  const addWithAncestors = (raw: string) => {
    const path = normalizeGroupPath(raw)
    if (!path) return
    for (const ancestor of groupAncestors(path)) canonical.add(ancestor)
    canonical.add(path)
  }

  for (const path of paths) addWithAncestors(path)

  const directByPath = new Map<string, number>()
  for (const server of servers) {
    const path = normalizeGroupPath(server.group)
    if (!path) continue
    addWithAncestors(path)
    directByPath.set(path, (directByPath.get(path) ?? 0) + 1)
  }

  const nodes = new Map<string, GroupNode>()
  for (const path of canonical) {
    const parent = groupParent(path)
    nodes.set(path, {
      path,
      name: groupName(path),
      parent,
      depth: groupDepth(path),
      children: [],
      direct: directByPath.get(path) ?? 0,
      total: 0,
    })
  }

  const roots: GroupNode[] = []
  for (const node of nodes.values()) {
    const parent = node.parent ? nodes.get(node.parent) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }

  const sortAndSum = (list: GroupNode[]): number => {
    list.sort((a, b) => compareGroupName(a.name, b.name))
    let total = 0
    for (const node of list) {
      node.total = node.direct + sortAndSum(node.children)
      total += node.total
    }
    return total
  }
  sortAndSum(roots)
  return roots
}

/**
 * 按关键字过滤树：
 * - 节点自身命中：保留该节点及完整子树；
 * - 只有子孙命中：保留祖先链与命中分支；
 * - 关键字为空：返回原树。
 */
export function filterGroupTree(nodes: GroupNode[], keyword: string): GroupNode[] {
  const kw = keyword.trim().toLowerCase()
  if (!kw) return nodes
  const visit = (node: GroupNode): GroupNode | null => {
    const selfMatch = node.name.toLowerCase().includes(kw) || node.path.toLowerCase().includes(kw)
    const children = node.children.map(visit).filter((child): child is GroupNode => !!child)
    if (!selfMatch && !children.length) return null
    return {...node, children: selfMatch ? node.children : children}
  }
  return nodes.map(visit).filter((node): node is GroupNode => !!node)
}

/** 收集树中所有路径，按深度优先顺序返回。 */
export function collectGroupPaths(nodes: GroupNode[]): string[] {
  const result: string[] = []
  const walk = (list: GroupNode[]) => {
    for (const node of list) {
      result.push(node.path)
      walk(node.children)
    }
  }
  walk(nodes)
  return result
}

/** 把树展开成带层级的扁平列表，便于管理弹窗渲染。 */
export function flattenGroupTree(nodes: GroupNode[], expanded: ReadonlySet<string> = new Set()): GroupNode[] {
  const result: GroupNode[] = []
  const walk = (list: GroupNode[]) => {
    for (const node of list) {
      result.push(node)
      if (node.children.length && expanded.has(node.path)) walk(node.children)
    }
  }
  walk(nodes)
  return result
}

/** 搜索命中某个路径时，需要展开的祖先路径。 */
export function ancestorsToExpand(path: string): string[] {
  return groupAncestors(path)
}

/** 选中态在重命名/移动后的跟随路径。 */
export function retargetGroupPath(path: string, oldPath: string, newPath: string): string {
  const target = normalizeGroupPath(path)
  const oldBase = normalizeGroupPath(oldPath)
  const newBase = normalizeGroupPath(newPath)
  if (target === oldBase) return newBase
  if (oldBase && target.startsWith(`${oldBase}/`)) return `${newBase}${target.slice(oldBase.length)}`
  return target
}

/** 判断分组路径是否合法（仅用于前端即时校验，最终以后端为准）。 */
export function validateGroupNameInput(raw: string, maxDepth = 6, maxSegment = 32): string {
  const value = raw.trim()
  if (!value) return '请输入分组名称'
  if (value.startsWith('/') || value.endsWith('/')) return '分组名称不能以 / 开头或结尾'
  if (value.includes('//')) return '分组路径不能包含空层级'
  const parts = value.split('/')
  if (parts.length > maxDepth) return `分组层级不能超过 ${maxDepth} 级`
  for (const part of parts) {
    const segment = part.trim()
    if (!segment) return '分组路径不能包含空层级'
    if (segment === '.' || segment === '..') return '分组名称不能是 . 或 ..'
    if ([...segment].length > maxSegment) return `每级分组名称不能超过 ${maxSegment} 个字符`
    if (/[\u0000-\u001f\u007f]/.test(segment)) return '分组名称不能包含控制字符'
  }
  return ''
}
