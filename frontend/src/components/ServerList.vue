<script lang="ts" setup>
import {computed, onMounted, reactive, ref, watch} from 'vue'
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
const showDialog = ref(false)
const editing = ref<ServerNode | null>(null)
const confirmNode = ref<ServerNode | null>(null)
const collapsed = reactive<Record<string, boolean>>(loadCollapsed())

function loadCollapsed(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem('sftp-collapsed') || '{}')
  } catch { return {} }
}

function saveCollapsed() {
  try { localStorage.setItem('sftp-collapsed', JSON.stringify(collapsed)) }
  catch {}
}

function toggleGroup(name: string) {
  collapsed[name] = !collapsed[name]
  saveCollapsed()
}

// 分组管理弹窗
const showGroupManager = ref(false)
const newGroupName = ref('')
const renameTarget = ref('')
const renameInput = ref('')
const confirmGroupDelete = ref('')
const groupError = ref('')

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return servers.servers
  return servers.servers.filter(
    (s) =>
      s.name.toLowerCase().includes(kw) ||
      s.host.toLowerCase().includes(kw) ||
      s.group.toLowerCase().includes(kw),
  )
})

interface GroupBucket {
  name: string
  items: ServerNode[]
}

// 按分组聚合：未分组在前，其余按名称排序；空分组也展示。
const grouped = computed<GroupBucket[]>(() => {
  const map = new Map<string, ServerNode[]>()
  for (const node of filtered.value) {
    const key = node.group.trim()
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(node)
  }
  const buckets: GroupBucket[] = []
  const keys = new Set<string>(groups.list)
  for (const k of map.keys()) keys.add(k)
  const sorted = [...keys].filter((k) => k !== '').sort((a, b) => a.localeCompare(b))
  if (map.has('')) buckets.push({name: '未分组', items: map.get('')!})
  for (const key of sorted) {
    buckets.push({name: key, items: map.get(key) ?? []})
  }
  return buckets
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
  sessions.openTab(node)
}

function nodeStatus(node: ServerNode): 'on' | 'err' | 'connecting' | '' {
  const tabs = sessions.tabs.filter((t) => t.node.id === node.id)
  if (tabs.some((t) => t.status === 'connected')) return 'on'
  if (tabs.some((t) => t.status === 'connecting')) return 'connecting'
  if (tabs.some((t) => t.status === 'error')) return 'err'
  return ''
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
  await servers.load()
}

async function removeGroup(name: string) {
  confirmGroupDelete.value = ''
  await groups.remove(name)
  await servers.load()
}

onMounted(() => {
  void servers.load()
  void groups.load()
})
</script>

<template>
  <div class="flex flex-col h-full min-h-0">
    <div class="p-4 flex flex-col gap-3" style="box-shadow: inset 0 -1px 0 rgba(255,255,255,0.05)">
      <div class="flex items-center justify-between">
        <h3 class="text-[12px] font-semibold tracking-[0.08em] uppercase text-mist">Nodes</h3>
        <div class="flex items-center gap-0.5">
          <button class="btn-icon btn-sm" title="管理分组" aria-label="管理分组" @click="openGroupManager">
            <Icon name="folder-cog" :size="14" />
          </button>
          <button class="btn-icon btn-sm" title="刷新" aria-label="刷新" @click="servers.load()">
            <Icon name="refresh" :size="14" />
          </button>
        </div>
      </div>
      <div class="search">
        <Icon name="search" :size="14" extra-class="search-ico" />
        <input
          v-model="keyword"
          type="text"
          class="input input-sm"
          placeholder="搜索主机 / 分组…"
          aria-label="搜索服务器"
        />
      </div>
    </div>

    <div class="flex-1 overflow-y-auto p-3 flex flex-col gap-2">
      <div v-if="servers.loading" class="space-y-2">
        <div v-for="i in 4" :key="i" class="flex items-center gap-2 px-2.5 py-2">
          <div class="skel w-2 h-2 rounded-full"></div>
          <div class="skel h-3 flex-1"></div>
        </div>
      </div>
      <div v-else-if="filtered.length === 0" class="px-3 py-8 text-center text-xs text-mist">
        暂无服务器，点击右上角新建
      </div>

      <div v-for="bucket in grouped" :key="bucket.name" class="flex flex-col gap-1">
        <button class="group-h" @click="toggleGroup(bucket.name)">
          <Icon
            name="chevron-down"
            :size="14"
            extra-class="transition-transform"
            :class="collapsed[bucket.name] ? '-rotate-90' : ''"
          />
          {{ bucket.name }} · {{ bucket.items.length }}
        </button>

        <div v-if="!collapsed[bucket.name]" class="flex flex-col gap-0.5">
          <div
            v-for="node in bucket.items"
            :key="node.id"
            class="server-item group"
            :class="isActiveNode(node) ? 'active' : ''"
            @click="connect(node)"
          >
            <span class="status" :class="nodeStatus(node)"></span>
            <div class="min-w-0">
              <div class="text-[13px] font-medium text-[var(--mist-100)] truncate">{{ node.name }}</div>
              <div class="font-mono text-[11px] text-mist truncate">{{ node.user }}@{{ node.host }}:{{ node.port }}</div>
            </div>
            <div class="hidden group-hover:flex gap-0.5">
              <button class="btn-icon btn-sm" title="连接" @click.stop="connect(node)">
                <Icon name="arrow-right" :size="14" />
              </button>
              <button class="btn-icon btn-sm" title="编辑" @click.stop="openEdit(node)">
                <Icon name="pencil" :size="14" />
              </button>
              <button class="btn-icon btn-sm" title="删除" @click.stop="confirmNode = node">
                <Icon name="trash" :size="14" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="p-3 flex gap-2" style="box-shadow: inset 0 1px 0 rgba(255,255,255,0.05)">
      <button class="btn btn-ghost btn-sm flex-1" @click="openGroupManager">分组</button>
      <button class="btn btn-primary btn-sm flex-1" @click="openNew">新建</button>
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
                    <span class="text-[11px] text-mist">删除后其中服务器将变为未分组</span>
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
