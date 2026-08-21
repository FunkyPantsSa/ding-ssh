<script lang="ts" setup>
import {computed, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {useGroupsStore} from '../stores/groups'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import ServerDialog from './ServerDialog.vue'
import type {ServerNode} from '../types'

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

const keyword = ref('')
const activeGroup = ref('all') // 'all' | ''（未分组）| 分组名
const showDialog = ref(false)
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
    return {dot: 'bg-[var(--warn-500)] animate-pulse', text: '测试中', textClass: 'text-mist'}
  }
  const r = servers.testResults[node.id]
  if (!r) return null
  if (r.reachable) {
    return {dot: 'bg-[var(--ok-500)]', text: `${r.latencyMs}ms`, textClass: 'text-[var(--ok-500)]', tip: 'SSH 端口畅通'}
  }
  return {dot: 'bg-[var(--danger-500)]', text: '不通', textClass: 'text-[var(--danger-500)]', tip: r.error || 'SSH 端口不通'}
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
})
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <div class="page-pad flex flex-col gap-4 min-h-0">
      <!-- 页面头：标题 + 在线统计 -->
      <div class="page-hero flex items-end justify-between gap-6 shrink-0">
        <div>
          <h2>服务器管理</h2>
          <p>添加、编辑与删除 SSH 节点，按分组组织并测试在线状态。</p>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <span class="chip">
            <span class="dot"></span>
            {{ onlineServers }}/{{ servers.servers.length }} 台在线
          </span>
        </div>
      </div>

      <div class="flex-1 min-h-0 flex gap-4">
        <!-- 左：分组栏 -->
        <aside class="server-sidebar shrink-0 flex flex-col">
          <div class="flex items-center justify-between px-3 py-2.5 shrink-0" style="box-shadow: inset 0 -1px 0 rgba(255,255,255,0.05)">
            <span class="text-[12px] font-semibold tracking-[0.08em] uppercase text-mist">分组</span>
            <button class="btn-icon btn-sm" title="分组管理" aria-label="分组管理" @click="openGroupManager">
              <Icon name="folder-cog" :size="14" />
            </button>
          </div>

          <div class="flex-1 min-h-0 overflow-y-auto p-1.5 flex flex-col gap-0.5">
            <button
              class="side-group-item"
              :class="activeGroup === 'all' ? 'active' : ''"
              @click="activeGroup = 'all'"
            >
              <Icon name="server" :size="14" extra-class="text-mist" />
              <span class="flex-1 text-left truncate">全部服务器</span>
              <span class="count">{{ groupCount('all') }}</span>
            </button>
            <button
              v-if="groupCount('')"
              class="side-group-item"
              :class="activeGroup === '' ? 'active' : ''"
              @click="activeGroup = ''"
            >
              <Icon name="folder" :size="14" extra-class="text-mist" />
              <span class="flex-1 text-left truncate">未分组</span>
              <span class="count">{{ groupCount('') }}</span>
            </button>
            <button
              v-for="g in sortedGroups"
              :key="g"
              class="side-group-item"
              :class="activeGroup === g ? 'active' : ''"
              @click="activeGroup = g"
            >
              <Icon name="folder" :size="14" extra-class="text-mist" />
              <span class="flex-1 text-left truncate">{{ g }}</span>
              <span class="count">{{ groupCount(g) }}</span>
            </button>
          </div>

          <div class="p-2 shrink-0" style="box-shadow: inset 0 1px 0 rgba(255,255,255,0.05)">
            <button class="btn btn-ghost btn-sm w-full" @click="openGroupManager">
              <Icon name="plus" :size="14" />
              新建分组
            </button>
          </div>
        </aside>

        <!-- 右：列表区域 -->
        <div class="flex-1 min-w-0 flex flex-col gap-3">
          <!-- 工具栏 -->
          <div class="flex items-center gap-2 shrink-0">
            <div class="search" style="max-width: 340px">
              <Icon name="search" :size="14" extra-class="search-ico" />
              <input
                v-model="keyword"
                type="text"
                class="input input-sm"
                placeholder="搜索名称 / 主机 / 分组…"
                aria-label="搜索服务器"
              />
            </div>
            <div class="ml-auto flex items-center gap-2">
              <button
                class="btn btn-ghost btn-sm"
                :title="servers.testingAll ? '正在测试全部服务器…' : '测试全部服务器在线状态'"
                :disabled="servers.testingAll || !servers.servers.length"
                @click="servers.testAll()"
              >
                <Icon name="activity" :size="14" :extra-class="servers.testingAll ? 'animate-spin' : ''" />
                测试全部
              </button>
              <button class="btn btn-ghost btn-sm" title="刷新" aria-label="刷新" @click="servers.load()">
                <Icon name="refresh" :size="14" />
                刷新
              </button>
              <button class="btn btn-copper btn-sm" @click="openNew">
                <Icon name="plus" :size="14" />
                新建服务器
              </button>
            </div>
          </div>

          <!-- 列表 -->
          <div class="flex-1 min-h-0 overflow-y-auto server-list-panel">
            <div v-if="servers.loading" class="flex flex-col">
              <div v-for="i in 6" :key="i" class="flex items-center gap-3 px-4 py-3">
                <div class="skel w-2 h-2 rounded-full shrink-0"></div>
                <div class="skel h-3 w-1/3"></div>
                <div class="skel h-3 w-24 ml-auto"></div>
              </div>
            </div>
            <div v-else-if="filtered.length === 0" class="py-14 text-center flex flex-col items-center gap-3">
              <Icon name="server" :size="32" extra-class="text-mist/40" />
              <div class="text-[14px] font-semibold text-[var(--mist-100)]">
                {{ servers.servers.length ? '没有匹配的服务器' : '暂无服务器' }}
              </div>
              <p class="text-[13px] text-mist">
                {{ servers.servers.length ? '换个关键词，或切换左侧分组试试。' : '点击「新建服务器」添加节点，之后即可在终端页快速连接。' }}
              </p>
              <button v-if="!servers.servers.length" class="btn btn-primary btn-sm" @click="openNew">
                <Icon name="plus" :size="14" />
                新建服务器
              </button>
            </div>

            <div
              v-for="node in filtered"
              :key="node.id"
              class="server-row group"
              :class="isActiveNode(node) ? 'active' : ''"
              :title="`连接 ${node.name}`"
              @click="connect(node)"
            >
              <span class="status shrink-0" :class="nodeStatus(node)"></span>

              <div class="min-w-0 flex-1">
                <div class="text-[13px] font-medium text-[var(--mist-100)] truncate">{{ node.name }}</div>
                <div class="font-mono text-[12px] text-mist truncate">{{ node.user }}@{{ node.host }}:{{ node.port }}</div>
              </div>

              <span
                v-if="testBadge(node)"
                class="latency shrink-0 cursor-pointer"
                :title="(testBadge(node)!.tip || '') + '（点击重新测试）'"
                @click.stop="servers.testOne(node)"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="testBadge(node)!.dot"></span>
                <span class="text-[12px] leading-none" :class="testBadge(node)!.textClass">{{ testBadge(node)!.text }}</span>
              </span>
              <span v-else class="latency shrink-0 text-mist">未测试</span>

              <div class="row-actions shrink-0">
                <button class="btn-icon btn-sm" title="连接" aria-label="连接" @click.stop="connect(node)">
                  <Icon name="arrow-right" :size="15" />
                </button>
                <button class="btn-icon btn-sm" title="测试在线状态" aria-label="测试在线状态" @click.stop="servers.testOne(node)">
                  <Icon name="activity" :size="15" />
                </button>
                <button class="btn-icon btn-sm" title="编辑" aria-label="编辑" @click.stop="openEdit(node)">
                  <Icon name="pencil" :size="15" />
                </button>
                <button class="btn-icon btn-sm" title="删除" aria-label="删除" @click.stop="confirmNode = node">
                  <Icon name="trash" :size="15" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <ServerDialog v-model="showDialog" :editing="editing" />

    <Teleport to="body">
      <div v-if="confirmNode" class="modal-root" @click.self="confirmNode = null">
        <div class="modal neo">
          <h3>删除服务器</h3>
          <p class="mdesc">确认删除服务器「{{ confirmNode.name }}」？此操作不可恢复。</p>
          <div class="flex justify-end gap-2">
            <button class="btn btn-ghost" @click="confirmNode = null">取消</button>
            <button class="btn btn-danger" @click="remove">删除</button>
          </div>
        </div>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="showGroupManager" class="modal-root" @click.self="showGroupManager = false">
        <div class="modal neo" style="width:min(400px,100%);padding:0;display:flex;flex-direction:column;max-height:80vh">
          <div class="flex items-center justify-between px-5 py-4" style="box-shadow: inset 0 -1px 0 rgba(255,255,255,0.05)">
            <h3 class="!mb-0">分组管理</h3>
            <button class="btn-icon btn-sm" @click="showGroupManager = false">
              <Icon name="close" :size="14" />
            </button>
          </div>
          <div class="flex-1 overflow-y-auto px-5 py-4 space-y-3">
            <div class="flex gap-2">
              <input v-model="newGroupName" class="input input-sm flex-1" placeholder="新分组名称" @keyup.enter="addGroup" />
              <button class="btn btn-primary btn-sm" @click="addGroup">添加</button>
            </div>
            <p v-if="groupError" class="text-xs text-danger">{{ groupError }}</p>
            <div v-if="!groups.list.length" class="text-xs text-mist py-2 text-center">
              暂无手动分组，服务器节点使用的分组会自动出现。
            </div>
            <div
              v-for="g in groups.list"
              :key="g"
              class="neo-flat flex items-center justify-between gap-2 px-3 py-2"
            >
              <template v-if="renameTarget === g">
                <input v-model="renameInput" class="input input-sm flex-1" @keyup.enter="doRename" />
                <button class="btn btn-primary btn-sm" @click="doRename">确定</button>
                <button class="btn btn-ghost btn-sm" @click="renameTarget = ''">取消</button>
              </template>
              <template v-else>
                <span class="min-w-0 truncate text-[13px] text-[var(--mist-100)]">{{ g }}</span>
                <div class="flex items-center gap-1 shrink-0">
                  <template v-if="confirmGroupDelete !== g">
                    <button class="btn btn-ghost btn-sm" @click="startRename(g)">重命名</button>
                    <button class="btn btn-ghost btn-sm" @click="confirmGroupDelete = g">删除</button>
                  </template>
                  <template v-else>
                    <span class="text-[12px] text-mist">删除后其中服务器将变为未分组</span>
                    <button class="btn btn-danger btn-sm" @click="removeGroup(g)">确认</button>
                    <button class="btn btn-ghost btn-sm" @click="confirmGroupDelete = ''">取消</button>
                  </template>
                </div>
              </template>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
