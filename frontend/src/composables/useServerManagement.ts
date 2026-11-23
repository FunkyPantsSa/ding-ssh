import {t} from '../i18n'
import {copyText as ClipboardSetText} from '../utils/clipboard'
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {useGroupsStore} from '../stores/groups'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import {useToast} from './useToast'
import {
  ancestorsToExpand,
  buildGroupTree,
  collectGroupPaths,
  filterGroupTree,
  groupAncestors,
  groupName,
  groupParent,
  isGroupInSubtree,
  normalizeGroupPath,
  retargetGroupPath,
  validateGroupNameInput,
  type GroupNode,
} from '../utils/groupTree'

import type {GroupSelection, ServerNode} from '../types'

const EXPANDED_KEY = 'ding-ssh:group-expanded'
const GROUP_FILTER_MAX_DEPTH = 6

export interface GroupEditorState {
  mode: 'create' | 'rename'
  parentPath: string
  /** 重命名时的原路径；新建时为空 */
  path: string
  value: string
  error: string
  submitting: boolean
}

export interface GroupDeleteState {
  path: string
  keepChildren: boolean
  total: number
  direct: number
  childCount: number
  error: string
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  return t('操作失败，请重试')
}

function loadExpanded(): string[] | null {
  try {
    const raw = localStorage.getItem(EXPANDED_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string') : null
  } catch {
    return null
  }
}

export function useServerManagement() {
  const servers = useServersStore()
  const sessions = useSessionsStore()
  const groups = useGroupsStore()
  const ui = useUIStore()
  const toast = useToast()

  watch(
    () => ui.newServerTick,
    (n, old) => {
      if (n && n !== old) openNew()
    },
  )

  // 任意页面（如终端连接失败页）请求编辑服务器时，复用这里的编辑弹窗。
  watch(
    () => ui.editServerTick,
    async () => {
      const openRequested = () => {
        const node = servers.servers.find((s) => s.id === ui.editServerId)
        if (node) openEdit(node)
      }
      if (servers.servers.length) {
        openRequested()
        return
      }
      try {
        await servers.load()
        openRequested()
      } catch {
        /* 列表加载失败时保持原页面，由服务器页展示错误状态 */
      }
    },
  )

  const keyword = ref('')
  const selection = ref<GroupSelection>({kind: 'all'})
  const groupFilter = ref('')
  const showDialog = ref(false)
  const showSSHImport = ref(false)
  const editing = ref<ServerNode | null>(null)
  const confirmNode = ref<ServerNode | null>(null)
  const serverDialogGroup = ref('')

  // 分组管理弹窗
  const showGroupManager = ref(false)
  const groupEditor = ref<GroupEditorState | null>(null)
  const groupDeleteTarget = ref<GroupDeleteState | null>(null)

  // 展开状态：只持久化路径，不从服务器数据推导，避免删除分组后残留。
  const storedExpanded = loadExpanded()
  const expandedPaths = ref<string[]>(storedExpanded ?? [])
  let expansionInitialized = storedExpanded !== null

  const expandedSet = computed(() => new Set(expandedPaths.value))

  const groupTree = computed(() => buildGroupTree(groups.list, servers.servers))

  const treeByPath = computed(() => {
    const map = new Map<string, GroupNode>()
    const walk = (nodes: GroupNode[]) => {
      for (const node of nodes) {
        map.set(node.path, node)
        walk(node.children)
      }
    }
    walk(groupTree.value)
    return map
  })

  const filteredGroupTree = computed(() => filterGroupTree(groupTree.value, groupFilter.value))

  const sidebarExpanded = computed<Set<string>>(() => {
    if (!groupFilter.value.trim()) return expandedSet.value
    // 搜索时展开所有命中分支；清空搜索后恢复用户原来的展开状态。
    return new Set(collectGroupPaths(filteredGroupTree.value))
  })

  const selectionPath = computed(() => (selection.value.kind === 'group' ? selection.value.path : ''))

  const activeGroup = computed(() => {
    if (selection.value.kind === 'group') return selection.value.path
    return selection.value.kind === 'ungrouped' ? '' : 'all'
  })

  const selectedNode = computed(() =>
    selection.value.kind === 'group' ? treeByPath.value.get(selection.value.path) : undefined,
  )

  const allCount = computed(() => servers.servers.length)
  const ungroupedCount = computed(() => servers.servers.filter((s) => !s.group.trim()).length)

  const selectedTotal = computed(() => {
    if (selection.value.kind === 'all') return allCount.value
    if (selection.value.kind === 'ungrouped') return ungroupedCount.value
    return selectedNode.value?.total ?? 0
  })

  const selectedDirect = computed(() => {
    if (selection.value.kind === 'ungrouped') return ungroupedCount.value
    return selectedNode.value?.direct ?? selectedTotal.value
  })

  const selectedHasChildren = computed(() => (selectedNode.value?.children.length ?? 0) > 0)


  function saveExpanded() {
    try {
      localStorage.setItem(EXPANDED_KEY, JSON.stringify(expandedPaths.value))
    } catch {
      /* localStorage 不可用时只保留内存状态 */
    }
  }

  function expandPaths(paths: string[]) {
    const next = new Set(expandedPaths.value)
    for (const path of paths) {
      const value = normalizeGroupPath(path)
      if (value) next.add(value)
    }
    expandedPaths.value = [...next]
    saveExpanded()
  }

  function expandPath(path: string) {
    expandPaths([path])
  }

  function toggleExpanded(path: string) {
    const target = normalizeGroupPath(path)
    if (!target) return
    const next = new Set(expandedPaths.value)
    if (next.has(target)) next.delete(target)
    else next.add(target)
    expandedPaths.value = [...next]
    saveExpanded()
  }

  function collapseAll() {
    expandedPaths.value = []
    saveExpanded()
  }

  function expandAll() {
    const parents = collectGroupPaths(groupTree.value).filter((path) => (treeByPath.value.get(path)?.children.length ?? 0) > 0)
    expandedPaths.value = parents
    saveExpanded()
  }

  // 首次进入时展开根层级；之后使用本地保存的展开状态。
  watch(
    groupTree,
    (tree) => {
      if (expansionInitialized || !tree.length) return
      expansionInitialized = true
      // 默认展开根层级与一级子分组（通常已能看清主要分类），更深的层级按需展开。
      const defaults: string[] = []
      const walk = (nodes: GroupNode[]) => {
        for (const node of nodes) {
          if (node.children.length && node.depth <= 2) defaults.push(node.path)
          walk(node.children)
        }
      }
      walk(tree)
      expandedPaths.value = defaults
      saveExpanded()
    },
    {immediate: true},
  )

  // 外部数据变化（导入、删除、重命名）后，选中路径可能已不存在：回退到父级或“全部”。
  watch(
    groupTree,
    (tree) => {
      if (selection.value.kind !== 'group') return
      const paths = new Set(collectGroupPaths(tree))
      if (paths.has(selection.value.path)) return
      const parent = groupParent(selection.value.path)
      selection.value = parent && paths.has(parent) ? {kind: 'group', path: parent} : {kind: 'all'}
    },
    {flush: 'post'},
  )

  // 选中父级时自动展开祖先，避免面包屑指向一个看不见的深层分组。
  watch(
    () => (selection.value.kind === 'group' ? selection.value.path : ''),
    (path) => {
      if (path) expandPaths(ancestorsToExpand(path))
    },
  )

  function selectGroup(next: GroupSelection) {
    selection.value = next
    if (next.kind === 'group') expandPaths(ancestorsToExpand(next.path))
  }

  function selectPath(path: string) {
    selectGroup({kind: 'group', path: normalizeGroupPath(path)})
  }

  function groupCount(path: string): number {
    return treeByPath.value.get(normalizeGroupPath(path))?.total ?? 0
  }

  function matchesSelection(serverGroup: string, current: GroupSelection): boolean {
    const path = normalizeGroupPath(serverGroup)
    if (current.kind === 'all') return true
    if (current.kind === 'ungrouped') return path === ''
    return path === current.path || path.startsWith(`${current.path}/`)
  }

  const filtered = computed(() => {
    const kw = keyword.value.trim().toLowerCase()
    let list = servers.servers
    if (selection.value.kind !== 'all') {
      list = list.filter((s) => matchesSelection(s.group, selection.value))
    }
    if (kw) {
      list = list.filter(
        (s) =>
          s.name.toLowerCase().includes(kw) ||
          s.host.toLowerCase().includes(kw) ||
          s.user.toLowerCase().includes(kw) ||
          s.group.toLowerCase().includes(kw),
      )
    }
    return list
  })

  const onlineServers = computed(() => {
    const reachable = new Set(
      servers.servers.filter((s) => servers.testResults[s.id]?.reachable).map((s) => s.id),
    )
    return reachable.size
  })

  function ensureGroupsLoaded() {
    if (groups.loaded) return
    void groups.load().catch(() => {
      /* 分组列表加载失败不阻塞服务器弹窗；可选项会退回到服务器实际使用的分组 */
    })
  }

  function openNew() {
    ensureGroupsLoaded()
    editing.value = null
    serverDialogGroup.value = selectionPath.value
    showDialog.value = true
  }

  function openNewForGroup(path: string) {
    editing.value = null
    serverDialogGroup.value = normalizeGroupPath(path)
    showDialog.value = true
  }

  function openEdit(node: ServerNode) {
    ensureGroupsLoaded()
    editing.value = node
    serverDialogGroup.value = node.group.trim()
    showDialog.value = true
  }

  // ---- 复制主机 IP ----
  const copiedHostId = ref('')
  let copiedTimer: number | undefined

  /** 复制文本并让对应入口短暂显示「已复制」对勾（图标按钮与右键菜单共用）。 */
  async function copyText(text: string, markId: string, what: string) {
    if (!text) return
    try {
      await ClipboardSetText(text)
      copiedHostId.value = markId
      window.clearTimeout(copiedTimer)
      copiedTimer = window.setTimeout(() => {
        copiedHostId.value = ''
      }, 1600)
    } catch (e) {
      console.warn(t('[servers] 复制{0}失败', [what]), e)
    }
  }

  async function copyHost(node: ServerNode) {
    await copyText(node.host.trim(), node.id, t('主机地址'))
  }

  // ---- 服务器右键菜单 ----
  const serverMenu = ref<{x: number; y: number; node: ServerNode} | null>(null)
  const cloning = ref(false)

  function openRowMenu(e: MouseEvent, node: ServerNode) {
    e.preventDefault()
    // 原始点击坐标交给 ServerContextMenu：渲染后按真实尺寸贴边/上移
    serverMenu.value = {x: e.clientX, y: e.clientY, node}
  }

  function closeRowMenu() {
    serverMenu.value = null
  }

  function menuCopyHost() {
    const node = serverMenu.value?.node
    if (!node) return
    void copyHost(node)
    closeRowMenu()
  }

  function menuEdit() {
    const node = serverMenu.value?.node
    closeRowMenu()
    if (node) openEdit(node)
  }

  function menuTest() {
    const node = serverMenu.value?.node
    closeRowMenu()
    if (node) void servers.testOne(node)
  }

  /** 复制配置：新增一条「原名.new」并直接打开编辑弹窗，方便改 IP / 端口。 */
  async function menuDuplicate() {
    const node = serverMenu.value?.node
    closeRowMenu()
    if (!node || cloning.value) return
    cloning.value = true
    try {
      const clone = await servers.duplicate(node)
      if (clone.group) {
        selectPath(clone.group)
        expandPaths(ancestorsToExpand(clone.group))
      }
      // 直接跳到新配置继续修改（跳转 = 打开它的编辑弹窗）
      openEdit(clone)
    } catch (e) {
      console.warn(t('[servers] 复制配置失败'), e)
    } finally {
      cloning.value = false
    }
  }

  // ---- 拖拽服务器到分组：修改服务器所在分组 ----
  // TODO(nested-groups): 分组之间拖拽调整层级（MoveGroup）列入后续任务；
  // 本期只实现“服务器 → 分组”，分组移动先通过重命名/管理弹窗完成。
  const draggingServerId = ref('')
  // 当前悬停的拖放目标 key（'all' / '' / 路径）；null 表示没有目标。
  const groupDragOver = ref<string | null>(null)
  let dragOverTimer: number | undefined
  let dragExpandTimer: number | undefined
  let dragExpandPath = ''

  function selectionKey(target: GroupSelection): string {
    if (target.kind === 'all') return 'all'
    if (target.kind === 'ungrouped') return ''
    return target.path
  }

  /** 清掉拖放高亮（拖拽结束 / 落下 / 取消都要清，避免残留高亮）。 */
  function clearDragOver() {
    window.clearTimeout(dragOverTimer)
    window.clearTimeout(dragExpandTimer)
    dragExpandPath = ''
    groupDragOver.value = null
  }

  /** 从拖拽事件里取服务器 id：dataTransfer 优先（跨窗口/重排也可靠），回退到内部状态。 */
  function dragServerId(e: DragEvent): string {
    const raw = e.dataTransfer?.getData('text/ding-ssh-server') || e.dataTransfer?.getData('text/plain') || ''
    return raw || draggingServerId.value
  }

  function onRowDragStart(e: DragEvent, node: ServerNode) {
    closeRowMenu()
    closeGroupMenu()
    draggingServerId.value = node.id
    // text/plain 兜底：某些环境下自定义类型读不到
    e.dataTransfer?.setData('text/ding-ssh-server', node.id)
    e.dataTransfer?.setData('text/plain', node.id)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }

  function onRowDragEnd() {
    draggingServerId.value = ''
    clearDragOver()
  }

  /** 拖拽目标是否接受当前服务器：目标不能是全部/未分组之外的同一分组。 */
  function canDropInto(node: ServerNode | undefined, target: GroupSelection): boolean {
    if (!node || target.kind === 'all') return false
    const targetPath = target.kind === 'ungrouped' ? '' : target.path
    return normalizeGroupPath(node.group) !== normalizeGroupPath(targetPath)
  }

  function onGroupDragOver(e: DragEvent, target: GroupSelection) {
    const id = dragServerId(e)
    if (!id || !canDropInto(servers.servers.find((s) => s.id === id), target)) return
    e.preventDefault() // 只有 preventDefault 才允许 drop
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    groupDragOver.value = selectionKey(target)
    // 悬停在折叠父组上时自动展开，方便继续往深层拖。
    if (target.kind === 'group') {
      const node = treeByPath.value.get(target.path)
      if (node?.children.length && !expandedSet.value.has(target.path) && dragExpandPath !== target.path) {
        dragExpandPath = target.path
        window.clearTimeout(dragExpandTimer)
        dragExpandTimer = window.setTimeout(() => {
          expandPath(target.path)
          dragExpandPath = ''
        }, 550)
      }
    }
    // 兜底：dragend 偶尔收不到（例如在拖动中切换视图），高亮最多保留 3 秒
    window.clearTimeout(dragOverTimer)
    dragOverTimer = window.setTimeout(() => {
      groupDragOver.value = null
    }, 3000)
  }

  function onGroupDragLeave(target: GroupSelection) {
    if (groupDragOver.value === selectionKey(target)) clearDragOver()
  }

  async function onGroupDrop(e: DragEvent, target: GroupSelection) {
    e.preventDefault()
    const id = dragServerId(e)
    clearDragOver()
    draggingServerId.value = ''
    const node = servers.servers.find((s) => s.id === id)
    if (!node || !canDropInto(node, target)) return
    if (target.kind === 'ungrouped') await moveToGroup(node, '')
    else if (target.kind === 'group') await moveToGroup(node, target.path)
  }

  /** 把服务器保存到目标分组（空串 = 移出分组，变未分组）。 */
  async function moveToGroup(node: ServerNode, target: string) {
    const group = normalizeGroupPath(target)
    const next: ServerNode = {...node, group}
    try {
      await servers.save(next)
      if (group) expandPaths(groupAncestors(group))
      toast.success(group ? t('已移动到「{0}」', [group]) : t('已移出分组'))
    } catch (err) {
      console.warn(t('[servers] 移动分组失败'), err)
      toast.error(t('移动分组失败'), errorMessage(err))
    }
  }

  function connect(node: ServerNode) {
    ui.showWorkspace()
    sessions.openTab(node)
  }

  function nodeStatus(node: ServerNode): 'on' | 'err' | 'connecting' | '' {
    const tabs = sessions.tabs.filter((t) => t.node.id === node.id)
    if (tabs.some((t) => t.status === 'connected')) return 'on'
    if (tabs.some((t) => t.status === 'connecting')) return 'connecting'
    if (tabs.some((t) => t.status === 'error')) return 'err'
    return ''
  }

  // ---- 在线状态测试 ----
  function testBadge(node: ServerNode): {dot: string; text: string; textClass: string; tip?: string} | null {
    if (servers.isTesting(node)) {
      return {dot: 'bg-[var(--warning)] animate-pulse', get text() { return t('测试中') }, textClass: 'text-mist'}
    }
    const r = servers.testResults[node.id]
    if (!r) return null
    if (r.reachable) {
      return {dot: 'bg-[var(--success)]', text: `${r.latencyMs}ms`, textClass: 'text-[var(--success)]', get tip() { return t('SSH 端口畅通') }}
    }
    return {dot: 'bg-[var(--destructive)]', get text() { return t('不通') }, textClass: 'text-[var(--destructive)]', tip: r.error || t('SSH 端口不通')}
  }

  function isActiveNode(node: ServerNode): boolean {
    return sessions.activeTab?.node.id === node.id
  }

  async function remove() {
    if (!confirmNode.value) return
    const node = confirmNode.value
    confirmNode.value = null
    await servers.remove(node.id)
  }

  // ---- 分组右键菜单 ----
  const groupMenu = ref<{x: number; y: number; path: string} | null>(null)

  function openGroupMenu(e: MouseEvent, path: string) {
    e.preventDefault()
    e.stopPropagation()
    groupMenu.value = {x: e.clientX, y: e.clientY, path: normalizeGroupPath(path)}
  }

  function closeGroupMenu() {
    groupMenu.value = null
  }

  function menuCreateChild() {
    const path = groupMenu.value?.path ?? ''
    closeGroupMenu()
    if (path) openCreateGroup(path)
  }

  function menuNewServer() {
    const path = groupMenu.value?.path ?? ''
    closeGroupMenu()
    if (path) openNewForGroup(path)
  }

  function menuRenameGroup() {
    const path = groupMenu.value?.path ?? ''
    closeGroupMenu()
    if (path) openRenameGroup(path)
  }

  function menuRemoveGroup() {
    const path = groupMenu.value?.path ?? ''
    closeGroupMenu()
    if (path) requestRemoveGroup(path)
  }

  // ---- 分组管理 ----
  function openGroupManager() {
    groupEditor.value = null
    groupDeleteTarget.value = null
    showGroupManager.value = true
  }

  function openCreateGroup(parent?: string) {
    const parentPath = parent !== undefined
      ? normalizeGroupPath(parent)
      : selection.value.kind === 'group'
        ? selection.value.path
        : ''
    if (parentPath) expandPaths(groupAncestors(parentPath).concat(parentPath))
    groupEditor.value = {
      mode: 'create',
      parentPath,
      path: '',
      value: '',
      error: '',
      submitting: false,
    }
  }

  function openRenameGroup(path: string) {
    const target = normalizeGroupPath(path)
    if (!target) return
    groupEditor.value = {
      mode: 'rename',
      parentPath: groupParent(target),
      path: target,
      value: groupName(target),
      error: '',
      submitting: false,
    }
  }

  function closeGroupEditor() {
    groupEditor.value = null
  }

  /** 新建分组（供侧栏弹窗与管理弹窗共用）；返回错误文案，成功返回空串。 */
  async function createGroupAt(parentPath: string, name: string): Promise<string> {
    const parent = normalizeGroupPath(parentPath)
    const raw = name.trim()
    if (!raw) return t('请输入分组名称')
    const full = normalizeGroupPath([parent, raw].filter(Boolean).join('/'))
    const validation = validateGroupNameInput(full, GROUP_FILTER_MAX_DEPTH)
    if (validation) return validation
    if (treeByPath.value.has(full)) return t('目标分组已存在')
    try {
      await groups.add(full)
      expandPaths(groupAncestors(full).concat(full))
      selectGroup({kind: 'group', path: full})
      toast.success(t('已创建分组「{0}」', [full]))
      return ''
    } catch (error) {
      return errorMessage(error)
    }
  }

  /** 重命名分组（只改最后一段）；返回错误文案，成功返回空串。 */
  async function renameGroupAt(path: string, newLeaf: string): Promise<string> {
    const target = normalizeGroupPath(path)
    const leaf = newLeaf.trim()
    if (!target) return t('分组不存在')
    if (!leaf) return t('请输入分组名称')
    if (leaf.includes('/')) return t('重命名只支持修改当前层名称')
    if (leaf === '.' || leaf === '..') return t('分组名称不能是 . 或 ..')
    if ([...leaf].length > 32) return t('每级分组名称不能超过 32 个字符')
    if (/[\u0000-\u001f\u007f]/.test(leaf)) return t('分组名称不能包含控制字符')
    const parent = groupParent(target)
    const newPath = normalizeGroupPath([parent, leaf].filter(Boolean).join('/'))
    if (newPath === target) return ''
    if (treeByPath.value.has(newPath)) return t('目标分组已存在')
    try {
      await groups.rename(target, leaf)
      // 后端会级联更新服务器分组，重新拉取以保持一致。
      await Promise.all([servers.load(), groups.load()])
      expandedPaths.value = [...new Set(expandedPaths.value.map((item) => retargetGroupPath(item, target, newPath)))]
      saveExpanded()
      if (selection.value.kind === 'group') {
        const nextPath = retargetGroupPath(selection.value.path, target, newPath)
        selection.value = {kind: 'group', path: nextPath}
      }
      toast.success(t('已重命名分组「{0}」', [newPath]))
      return ''
    } catch (error) {
      return errorMessage(error)
    }
  }

  async function submitGroupEditor(value: string) {
    const editor = groupEditor.value
    if (!editor || editor.submitting) return
    editor.submitting = true
    editor.error = ''
    const error = editor.mode === 'create'
      ? await createGroupAt(editor.parentPath, value)
      : await renameGroupAt(editor.path, value)
    editor.submitting = false
    if (error) {
      editor.error = error
      return
    }
    groupEditor.value = null
  }

  function requestRemoveGroup(path: string) {
    const target = normalizeGroupPath(path)
    const node = treeByPath.value.get(target)
    if (!target || !node) return
    showGroupManager.value = false
    groupDeleteTarget.value = {
      path: target,
      keepChildren: false,
      total: node.total,
      direct: node.direct,
      childCount: node.children.length,
      error: '',
    }
  }

  /** 删除分组（供确认弹窗调用）；返回错误文案，成功返回空串。 */
  async function deleteGroupAt(path: string, keepChildren: boolean): Promise<string> {
    const target = normalizeGroupPath(path)
    if (!target) return t('分组不存在')
    const parent = groupParent(target)
    try {
      await groups.remove(target, keepChildren)
      await Promise.all([servers.load(), groups.load()])
      expandedPaths.value = expandedPaths.value.filter((item) => !isGroupInSubtree(item, target))
      saveExpanded()
      if (selection.value.kind === 'group' && isGroupInSubtree(selection.value.path, target)) {
        const moved = keepChildren ? retargetGroupPath(selection.value.path, target, parent) : ''
        const fallback = parent && treeByPath.value.has(parent) ? parent : ''
        selection.value = moved && treeByPath.value.has(moved)
          ? {kind: 'group', path: moved}
          : fallback
            ? {kind: 'group', path: fallback}
            : {kind: 'all'}
      }
      toast.success(t('已删除分组「{0}」', [target]))
      return ''
    } catch (error) {
      return errorMessage(error)
    }
  }

  async function confirmGroupDelete() {
    const target = groupDeleteTarget.value
    if (!target) return
    target.error = ''
    const error = await deleteGroupAt(target.path, target.keepChildren)
    if (error) {
      target.error = error
      return
    }
    groupDeleteTarget.value = null
  }

  function closeGroupDelete() {
    groupDeleteTarget.value = null
  }

  // 点击页面其它位置时关闭右键菜单（服务器 / 分组共用）。
  function closeMenus() {
    closeRowMenu()
    closeGroupMenu()
  }

  onMounted(() => {
    void servers.load()
    void groups.load()
    window.addEventListener('click', closeMenus)
  })

  onBeforeUnmount(() => {
    window.clearTimeout(copiedTimer)
    window.clearTimeout(dragOverTimer)
    window.clearTimeout(dragExpandTimer)
    window.removeEventListener('click', closeMenus)
  })

  return {
    servers,
    groups,
    keyword,
    selection,
    selectionPath,
    activeGroup,
    groupFilter,
    groupTree,
    filteredGroupTree,
    treeByPath,
    expandedSet,
    sidebarExpanded,
    selectedNode,
    filtered,
    onlineServers,
    selectedTotal,
    selectedDirect,
    selectedHasChildren,
    selectGroup,
    selectPath,
    expandPath,
    toggleExpanded,
    collapseAll,
    expandAll,
    groupCount,
    allCount,
    ungroupedCount,
    showDialog,
    showSSHImport,
    editing,
    confirmNode,
    serverDialogGroup,
    showGroupManager,
    groupEditor,
    groupDeleteTarget,
    openNew,
    openNewForGroup,
    openEdit,
    copiedHostId,
    copyHost,
    serverMenu,
    cloning,
    openRowMenu,
    closeRowMenu,
    menuCopyHost,
    menuEdit,
    menuTest,
    menuDuplicate,
    draggingServerId,
    groupDragOver,
    onRowDragStart,
    onRowDragEnd,
    onGroupDragOver,
    onGroupDragLeave,
    onGroupDrop,
    connect,
    nodeStatus,
    testBadge,
    isActiveNode,
    remove,
    groupMenu,
    openGroupMenu,
    closeGroupMenu,
    menuCreateChild,
    menuNewServer,
    menuRenameGroup,
    menuRemoveGroup,
    openGroupManager,
    openCreateGroup,
    openRenameGroup,
    closeGroupEditor,
    submitGroupEditor,
    createGroupAt,
    renameGroupAt,
    requestRemoveGroup,
    deleteGroupAt,
    confirmGroupDelete,
    closeGroupDelete,
  }
}
