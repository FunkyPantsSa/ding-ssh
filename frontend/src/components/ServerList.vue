<script lang="ts" setup>
import {computed, onMounted, reactive, ref} from 'vue'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import ServerDialog from './ServerDialog.vue'
import type {ServerNode} from '../types'

const servers = useServersStore()
const sessions = useSessionsStore()

const keyword = ref('')
const showDialog = ref(false)
const editing = ref<ServerNode | null>(null)
const confirmNode = ref<ServerNode | null>(null)
const collapsed = reactive<Record<string, boolean>>({})

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

// 按分组聚合：未分组在前，其余按名称排序。
const grouped = computed<GroupBucket[]>(() => {
  const map = new Map<string, ServerNode[]>()
  for (const node of filtered.value) {
    const key = node.group.trim()
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(node)
  }
  const buckets: GroupBucket[] = []
  if (map.has('')) buckets.push({name: '未分组', items: map.get('')!})
  for (const key of [...map.keys()].filter((k) => k !== '').sort((a, b) => a.localeCompare(b))) {
    buckets.push({name: key, items: map.get(key)!})
  }
  return buckets
})

function toggleGroup(name: string) {
  collapsed[name] = !collapsed[name]
}

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

onMounted(() => {
  void servers.load()
})
</script>

<template>
  <div class="flex flex-col h-full">
    <div class="flex items-center justify-between px-4 py-3">
      <h1 class="text-sm font-semibold tracking-wide text-slate-200">服务器</h1>
      <button
        class="w-7 h-7 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-lg leading-none font-bold transition-colors"
        title="新建服务器"
        @click="openNew"
      >
        +
      </button>
    </div>

    <div class="px-3 pb-2">
      <input
        v-model="keyword"
        type="text"
        placeholder="搜索名称 / 地址"
        class="w-full px-3 py-1.5 rounded-md bg-slate-800/80 border border-slate-700/60 text-[13px] text-slate-200 placeholder-slate-500 outline-none focus:border-sky-500/60"
      />
    </div>

    <div class="flex-1 overflow-y-auto px-2 pb-3 space-y-2">
      <div v-if="!servers.loading && filtered.length === 0" class="px-3 py-8 text-center text-xs text-slate-500">
        暂无服务器，点击右上角 + 添加
      </div>

      <div v-for="bucket in grouped" :key="bucket.name">
        <button
          class="w-full flex items-center gap-1.5 px-2 py-1.5 text-left rounded-md hover:bg-slate-800/50"
          @click="toggleGroup(bucket.name)"
        >
          <span class="text-[10px] text-slate-500 transition-transform" :class="collapsed[bucket.name] ? '' : 'rotate-90'">▶</span>
          <span class="text-[11px] font-medium text-slate-400 truncate">{{ bucket.name }}</span>
          <span class="ml-auto text-[10px] text-slate-600">{{ bucket.items.length }}</span>
        </button>

        <div v-if="!collapsed[bucket.name]" class="space-y-1.5 mt-1">
          <div
            v-for="node in bucket.items"
            :key="node.id"
            class="group rounded-lg px-3 py-2.5 bg-slate-800/50 border border-slate-700/40 hover:border-sky-500/40 transition-colors"
          >
            <div class="flex items-center justify-between gap-2">
              <div class="min-w-0">
                <p class="text-[13px] font-medium text-slate-200 truncate">{{ node.name }}</p>
                <p class="text-[11px] text-slate-500 truncate mt-0.5">
                  {{ node.user }}@{{ node.host }}:{{ node.port }}
                </p>
              </div>
              <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                  class="w-6 h-6 rounded bg-emerald-500/70 hover:bg-emerald-400 text-slate-900 text-xs"
                  title="连接"
                  @click="connect(node)"
                >
                  ▶
                </button>
                <button
                  class="w-6 h-6 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                  title="编辑"
                  @click="openEdit(node)"
                >
                  ✎
                </button>
                <button
                  class="w-6 h-6 rounded bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-xs"
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
  </div>
</template>
