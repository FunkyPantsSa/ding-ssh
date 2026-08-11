<script lang="ts" setup>
import {computed, onMounted, reactive, ref} from 'vue'
import Icon from './Icon.vue'
import {useGroupsStore} from '../stores/groups'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import ServerDialog from './ServerDialog.vue'
import type {ServerNode} from '../types'

const servers = useServersStore()
const sessions = useSessionsStore()
const groups = useGroupsStore()

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
  <div class="flex flex-col h-full">
    <div class="flex items-center justify-between px-3 py-2">
      <h1 class="text-sm font-semibold tracking-wide text-slate-200">服务器</h1>
      <div class="flex items-center gap-1">
        <button
          class="w-6 h-6 rounded-md bg-slate-700/60 hover:bg-slate-600 text-slate-300 text-xs"
          title="管理分组"
          aria-label="管理分组"
          @click="openGroupManager"
        >
          <Icon name="folder" size="14" />
        </button>
        <button
          class="w-6 h-6 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-sm leading-none font-bold transition-colors"
          title="新建服务器"
          aria-label="新建服务器"
          @click="openNew"
        >
          <Icon name="plus" size="14" />
        </button>
      </div>
    </div>

    <div class="px-3 pb-2">
      <input
        v-model="keyword"
        type="text"
        placeholder="搜索名称 / 地址 / 分组"
        aria-label="搜索服务器"
        class="w-full px-2.5 py-1 rounded-md bg-slate-800/80 border border-slate-700/60 text-xs text-slate-200 placeholder-slate-500 outline-none focus:border-sky-500/60"
      />
    </div>

    <div class="flex-1 overflow-y-auto px-2 pb-3 space-y-1">
      <div v-if="servers.loading" class="px-3 py-2 space-y-2">
        <div v-for="i in 4" :key="i" class="flex items-center gap-2 px-2 py-2 rounded-md animate-pulse">
          <div class="w-2 h-2 rounded-full bg-slate-700/60"></div>
          <div class="h-3 flex-1 rounded bg-slate-700/40"></div>
          <div class="w-8 h-3 rounded bg-slate-700/40"></div>
        </div>
      </div>
      <div v-else-if="filtered.length === 0" class="px-3 py-8 text-center text-xs text-slate-500">
        暂无服务器，点击右上角 + 添加
      </div>

      <div v-for="bucket in grouped" :key="bucket.name">
        <button
          class="w-full flex items-center gap-1.5 px-1.5 py-1 text-left rounded-md hover:bg-slate-800/50"
          @click="toggleGroup(bucket.name)"
        >
          <span class="text-[9px] text-slate-500 transition-transform" :class="collapsed[bucket.name] ? '' : 'rotate-90'">▶</span>
          <span class="text-[11px] font-medium text-slate-400 truncate">{{ bucket.name }}</span>
          <span class="ml-auto text-[10px] text-slate-600">{{ bucket.items.length }}</span>
        </button>

        <div v-if="!collapsed[bucket.name]" class="space-y-1 mt-0.5">
          <div
            v-for="node in bucket.items"
            :key="node.id"
            class="group rounded-md px-2 py-1.5 bg-slate-800/50 border border-slate-700/40 hover:border-sky-500/40 transition-colors"
          >
            <div class="flex items-center justify-between gap-2">
              <div class="min-w-0">
                <p class="text-xs font-medium text-slate-200 truncate leading-tight">{{ node.name }}</p>
                <p class="text-[10px] text-slate-500 truncate leading-tight mt-0.5">
                  {{ node.user }}@{{ node.host }}:{{ node.port }}
                </p>
              </div>
              <div class="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                <button
                  class="w-5 h-5 rounded bg-emerald-500/70 hover:bg-emerald-400 text-slate-900 text-[10px]"
                  title="连接"
                  @click="connect(node)"
                >
                  ▶
                </button>
                <button
                  class="w-5 h-5 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-[10px]"
                  title="编辑"
                  @click="openEdit(node)"
                >
                  ✎
                </button>
                <button
                  class="w-5 h-5 rounded bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-[10px]"
                  title="删除"
                  @click="confirmNode = node"
                >
                  ✕
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <ServerDialog v-model="showDialog" :editing="editing" />

    <!-- 删除服务器确认 -->
    <Teleport to="body">
      <div
        v-if="confirmNode"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
        @click.self="confirmNode = null"
      >
        <div class="w-[360px] rounded-xl border border-slate-700/60 bg-slate-900/95 p-5 shadow-2xl">
          <h3 class="text-sm font-semibold text-slate-100">删除服务器</h3>
          <p class="mt-2 text-xs text-slate-400 leading-relaxed break-all">
            确认删除服务器「{{ confirmNode.name }}」？此操作不可恢复。
          </p>
          <div class="mt-5 flex justify-end gap-2">
            <button
              class="px-4 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-xs"
              @click="confirmNode = null"
            >
              取消
            </button>
            <button
              class="px-4 py-1.5 rounded-md bg-rose-600/80 hover:bg-rose-500 text-white text-xs font-medium"
              @click="remove"
            >
              删除
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 分组管理 -->
    <Teleport to="body">
      <div
        v-if="showGroupManager"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
        @click.self="showGroupManager = false"
      >
        <div class="w-[400px] max-h-[80vh] flex flex-col rounded-xl border border-slate-700/60 bg-slate-900/95 shadow-2xl">
          <div class="flex items-center justify-between px-5 py-4 border-b border-slate-800/80">
            <h3 class="text-sm font-semibold text-slate-100">分组管理</h3>
            <button
              class="w-7 h-7 rounded-md text-slate-400 hover:bg-slate-800 hover:text-slate-200"
              @click="showGroupManager = false"
            >
              ✕
            </button>
          </div>

          <div class="flex-1 overflow-y-auto px-5 py-4 space-y-3">
            <div>
              <div class="flex gap-2">
                <input
                  v-model="newGroupName"
                  class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none focus:border-sky-500/60"
                  placeholder="新分组名称"
                  @keyup.enter="addGroup"
                />
                <button
                  class="px-3 py-1.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-xs font-medium"
                  @click="addGroup"
                >
                  添加
                </button>
              </div>
              <p v-if="groupError" class="mt-1.5 text-xs text-rose-400">{{ groupError }}</p>
            </div>

            <div v-if="!groups.list.length" class="text-xs text-slate-500 py-2 text-center">
              暂无手动分组，服务器节点使用的分组会自动出现。
            </div>

            <div
              v-for="g in groups.list"
              :key="g"
              class="flex items-center justify-between gap-2 px-3 py-2 rounded-md bg-slate-800/50 border border-slate-700/40"
            >
              <template v-if="renameTarget === g">
                <input
                  v-model="renameInput"
                  class="flex-1 min-w-0 px-2 py-1 rounded-md bg-slate-800 border border-sky-500/60 text-slate-200 text-xs outline-none"
                  @keyup.enter="doRename"
                />
                <button class="px-2 py-1 rounded bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-xs" @click="doRename">确定</button>
                <button class="px-2 py-1 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs" @click="renameTarget = ''">取消</button>
              </template>
              <template v-else>
                <span class="min-w-0 truncate text-[13px] text-slate-200">{{ g }}</span>
                <div class="flex items-center gap-1 shrink-0">
                  <button
                    v-if="confirmGroupDelete !== g"
                    class="px-2 py-1 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                    @click="startRename(g)"
                  >
                    重命名
                  </button>
                  <button
                    v-if="confirmGroupDelete !== g"
                    class="px-2 py-1 rounded bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-xs"
                    @click="confirmGroupDelete = g"
                  >
                    删除
                  </button>
                  <template v-else>
                    <span class="text-[11px] text-slate-400">删除后其中服务器将变为未分组</span>
                    <button class="px-2 py-1 rounded bg-rose-600/80 hover:bg-rose-500 text-white text-xs" @click="removeGroup(g)">确认</button>
                    <button class="px-2 py-1 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs" @click="confirmGroupDelete = ''">取消</button>
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
