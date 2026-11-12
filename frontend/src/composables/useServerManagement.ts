import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {useGroupsStore} from '../stores/groups'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import type {ServerNode} from '../types'

export function useServerManagement() {
const servers = useServersStore()
const sessions = useSessionsStore()
const groups = useGroupsStore()
const ui = useUIStore()

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
const activeGroup = ref('all') // 'all' | ''（未分组）| 分组名
const showDialog = ref(false)
const showSSHImport = ref(false)
const editing = ref<ServerNode | null>(null)
const confirmNode = ref<ServerNode | null>(null)

// 分组管理弹窗
const showGroupManager = ref(false)
const newGroupName = ref('')
const renameTarget = ref('')
const renameInput = ref('')
const confirmGroupDelete = ref('')
const groupError = ref('')

// 左栏分组列表：全部 / 未分组 / 手动分组（含服务器中使用到的分组）
const sortedGroups = computed(() => {
  const set = new Set<string>(groups.list)
  for (const s of servers.servers) {
    const g = s.group.trim()
    if (g) set.add(g)
  }
  return [...set].sort((a, b) => a.localeCompare(b))
})

function groupCount(name: string): number {
  if (name === 'all') return servers.servers.length
  return servers.servers.filter((s) => s.group.trim() === name).length
}

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  let list = servers.servers
  if (activeGroup.value !== 'all') {
    list = list.filter((s) => s.group.trim() === activeGroup.value)
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

function openNew() {
  editing.value = null
  showDialog.value = true
}

function openEdit(node: ServerNode) {
  editing.value = node
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
    console.warn(`[servers] 复制${what}失败`, e)
  }
}

async function copyHost(node: ServerNode) {
  await copyText(node.host.trim(), node.id, '主机地址')
}

// ---- 服务器右键菜单 ----
const serverMenu = ref<{x: number; y: number; node: ServerNode} | null>(null)
const cloning = ref(false)

function openRowMenu(e: MouseEvent, node: ServerNode) {
  e.preventDefault()
  // 菜单高度约 250px：贴底时上移，避免被窗口裁掉
  serverMenu.value = {
    x: Math.min(e.clientX, window.innerWidth - 180),
    y: Math.min(e.clientY, Math.max(8, window.innerHeight - 250)),
    node,
  }
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
    if (clone.group) activeGroup.value = clone.group
    // 直接跳到新配置继续修改（跳转 = 打开它的编辑弹窗）
    openEdit(clone)
  } catch (e) {
    console.warn('[servers] 复制配置失败', e)
  } finally {
    cloning.value = false
  }
}

// ---- 拖拽服务器到分组：修改服务器所在分组 ----
const draggingServerId = ref('')
// 当前悬停的分组名（'all' / '' / 分组名）用于高亮；null 表示没有拖放目标。
// 不能用空串当「无目标」哨兵：'' 本身就是「未分组」，会让未分组条目误显示成拖放高亮。
const groupDragOver = ref<string | null>(null)
let dragOverTimer: number | undefined

/** 清掉拖放高亮（拖拽结束 / 落下 / 取消都要清，避免残留高亮）。 */
function clearDragOver() {
  window.clearTimeout(dragOverTimer)
  groupDragOver.value = null
}

/** 从拖拽事件里取服务器 id：dataTransfer 优先（跨窗口/重排也可靠），回退到内部状态。 */
function dragServerId(e: DragEvent): string {
  const raw = e.dataTransfer?.getData('text/ding-ssh-server') || e.dataTransfer?.getData('text/plain') || ''
  return raw || draggingServerId.value
}

function onRowDragStart(e: DragEvent, node: ServerNode) {
  closeRowMenu()
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

/** 分组条目是否接受拖放：拖拽中、且目标分组与服务器当前分组不同。 */
function canDropInto(node: ServerNode | undefined, target: string): boolean {
  if (!node) return false
  return node.group.trim() !== target.trim()
}

function onGroupDragOver(e: DragEvent, target: string) {
  const id = dragServerId(e)
  if (!id || !canDropInto(servers.servers.find((s) => s.id === id), target)) return
  e.preventDefault() // 只有 preventDefault 才允许 drop
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  groupDragOver.value = target
  // 兜底：dragend 偶尔收不到（例如在拖动中切换视图），高亮最多保留 3 秒
  window.clearTimeout(dragOverTimer)
  dragOverTimer = window.setTimeout(() => {
    groupDragOver.value = null
  }, 3000)
}

function onGroupDragLeave(target: string) {
  if (groupDragOver.value === target) clearDragOver()
}

async function onGroupDrop(e: DragEvent, target: string) {
  e.preventDefault()
  const id = dragServerId(e)
  clearDragOver()
  draggingServerId.value = ''
  const node = servers.servers.find((s) => s.id === id)
  if (!node || !canDropInto(node, target)) return
  await moveToGroup(node, target)
}

/** 把服务器保存到目标分组（空串 = 移出分组，变未分组）。 */
async function moveToGroup(node: ServerNode, target: string) {
  const group = target.trim()
  const next: ServerNode = {...node, group}
  try {
    await servers.save(next)
    // 拖到某个分组后，顺手把左栏切到该分组，方便确认结果
    if (group) activeGroup.value = group
  } catch (err) {
    console.warn('[servers] 移动分组失败', err)
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
    return {dot: 'bg-[var(--warning)] animate-pulse', text: '测试中', textClass: 'text-mist'}
  }
  const r = servers.testResults[node.id]
  if (!r) return null
  if (r.reachable) {
    return {dot: 'bg-[var(--success)]', text: `${r.latencyMs}ms`, textClass: 'text-[var(--success)]', tip: 'SSH 端口畅通'}
  }
  return {dot: 'bg-[var(--destructive)]', text: '不通', textClass: 'text-[var(--destructive)]', tip: r.error || 'SSH 端口不通'}
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

// ---- 分组管理 ----
function openGroupManager() {
  newGroupName.value = ''
  renameTarget.value = ''
  renameInput.value = ''
  confirmGroupDelete.value = ''
  groupError.value = ''
  showGroupManager.value = true
}

async function addGroup() {
  const name = newGroupName.value.trim()
  if (!name) {
    groupError.value = '请输入分组名称'
    return
  }
  groupError.value = ''
  await groups.add(name)
  newGroupName.value = ''
}

function startRename(name: string) {
  renameTarget.value = name
  renameInput.value = name
  confirmGroupDelete.value = ''
}

async function doRename() {
  const newName = renameInput.value.trim()
  if (!renameTarget.value || !newName) return
  if (newName === renameTarget.value) {
    renameTarget.value = ''
    return
  }
  groupError.value = ''
  await groups.rename(renameTarget.value, newName)
  renameTarget.value = ''
  if (activeGroup.value === renameTarget.value) activeGroup.value = newName
  await servers.load()
}

async function removeGroup(name: string) {
  confirmGroupDelete.value = ''
  await groups.remove(name)
  if (activeGroup.value === name) activeGroup.value = 'all'
  await servers.load()
}

onMounted(() => {
  void servers.load()
  void groups.load()
  window.addEventListener('click', closeRowMenu)
})

onBeforeUnmount(() => {
  window.clearTimeout(copiedTimer)
  window.clearTimeout(dragOverTimer)
  window.removeEventListener('click', closeRowMenu)
})

return {servers, groups, keyword, activeGroup, showDialog, showSSHImport, editing, confirmNode, showGroupManager, newGroupName, renameTarget, renameInput, confirmGroupDelete, groupError, sortedGroups, groupCount, filtered, onlineServers, openNew, openEdit, copiedHostId, copyHost, serverMenu, cloning, openRowMenu, closeRowMenu, menuCopyHost, menuEdit, menuTest, menuDuplicate, draggingServerId, groupDragOver, onRowDragStart, onRowDragEnd, onGroupDragOver, onGroupDragLeave, onGroupDrop, connect, nodeStatus, testBadge, isActiveNode, remove, openGroupManager, addGroup, startRename, doRename, removeGroup}
}
