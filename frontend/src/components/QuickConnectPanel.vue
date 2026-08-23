<script lang="ts" setup>
import {computed, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {useGroupsStore} from '../stores/groups'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import type {ServerNode} from '../types'

const servers = useServersStore()
const sessions = useSessionsStore()
const groups = useGroupsStore()
const ui = useUIStore()

const open = computed(() => ui.terminalSidebarOpen)
const keyword = ref('')
const collapsed = reactive<Record<string, boolean>>(loadCollapsed())

function loadCollapsed(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem('qconn-collapsed') || '{}')
  } catch { return {} }
}

function saveCollapsed() {
  try { localStorage.setItem('qconn-collapsed', JSON.stringify(collapsed)) }
  catch {}
}

function toggleGroup(name: string) {
  collapsed[name] = !collapsed[name]
  saveCollapsed()
}

watch(open, (v) => {
  if (v && !servers.loading) {
    void servers.load()
    void groups.load()
  }
})

// 模糊搜索：名称 / 主机 / 分组
const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return servers.servers
  return servers.servers.filter(
    (s) =>
      s.name.toLowerCase().includes(kw) ||
      s.host.toLowerCase().includes(kw) ||
      s.user.toLowerCase().includes(kw) ||
      s.group.toLowerCase().includes(kw),
  )
})

// 按分组聚合：未分组在前，其余按名称排序。
const grouped = computed(() => {
  const map = new Map<string, ServerNode[]>()
  for (const node of filtered.value) {
    const key = node.group.trim()
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(node)
  }
  const buckets: {name: string; items: ServerNode[]}[] = []
  const keys = new Set<string>(groups.list)
  for (const k of map.keys()) keys.add(k)
  const sorted = [...keys].filter((k) => k !== '').sort((a, b) => a.localeCompare(b))
  if (map.has('')) buckets.push({name: '未分组', items: map.get('')!})
  for (const key of sorted) {
    buckets.push({name: key, items: map.get(key) ?? []})
  }
  return buckets
})

function connect(node: ServerNode) {
  sessions.openTab(node)
  ui.closeTerminalSidebar()
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
</script>

<template>
  <Teleport to="body">
    <!-- 左侧贴边箭头：收起时贴导航轨右缘，展开后跟随侧边栏右缘 -->
    <button
      v-if="ui.view === 'workspace'"
      class="qconn-toggle"
      :class="open ? 'active' : ''"
      :title="open ? '收起服务器列表' : '打开服务器列表'"
      :aria-label="open ? '收起服务器列表' : '打开服务器列表'"
      @click="ui.toggleTerminalSidebar()"
    >
      <Icon :name="open ? 'chevron-left' : 'chevron-right'" :size="14" />
    </button>

    <Transition name="fade">
      <div v-if="open" class="qconn-mask" @click="ui.closeTerminalSidebar()"></div>
    </Transition>

    <Transition name="qconn">
      <aside v-if="open" class="qconn" aria-label="快速连接">
        <div class="px-4 h-[52px] flex items-center justify-between shrink-0 inset-line-b">
          <div class="flex items-center gap-2">
            <Icon name="server" :size="16" extra-class="text-signal" />
            <span class="text-[13px] font-semibold text-[var(--mist-100)]">服务器</span>
            <span class="text-[12px] text-mist">{{ servers.servers.length }}</span>
          </div>
          <button class="btn-icon btn-sm" title="收起" aria-label="收起" @click="ui.closeTerminalSidebar()">
            <Icon name="close" :size="14" />
          </button>
        </div>

        <div class="px-2.5 pb-2 shrink-0">
          <div class="search">
            <Icon name="search" :size="14" extra-class="search-ico" />
            <input
              v-model="keyword"
              type="text"
              class="input input-sm"
              placeholder="搜索名称 / 主机 / 分组…"
              aria-label="搜索服务器"
            />
          </div>
        </div>

        <div class="flex-1 min-h-0 overflow-y-auto p-2.5 pt-0 flex flex-col gap-1.5">
          <div v-if="servers.loading" class="space-y-2">
            <div v-for="i in 4" :key="i" class="flex items-center gap-2 px-2.5 py-2">
              <div class="skel w-2 h-2 rounded-full"></div>
              <div class="skel h-3 flex-1"></div>
            </div>
          </div>
          <div v-else-if="!servers.servers.length" class="px-3 py-8 text-center text-xs text-mist flex flex-col items-center gap-3">
            <span>暂无服务器</span>
            <button class="btn btn-primary btn-sm" @click="ui.showServers()">去添加服务器</button>
          </div>
          <div v-else-if="!filtered.length" class="px-3 py-8 text-center text-xs text-mist">
            无匹配服务器
          </div>

          <div v-for="bucket in grouped" :key="bucket.name" class="flex flex-col gap-0.5">
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
              <button
                v-for="node in bucket.items"
                :key="node.id"
                class="qconn-item group"
                :class="isActiveNode(node) ? 'active' : ''"
                :title="`连接 ${node.name}`"
                @click="connect(node)"
              >
                <span class="status" :class="nodeStatus(node)"></span>
                <span class="min-w-0 flex-1 text-left">
                  <span class="block text-[13px] font-medium text-[var(--mist-100)] truncate">{{ node.name }}</span>
                  <span class="block font-mono text-[12px] text-mist truncate">{{ node.user }}@{{ node.host }}:{{ node.port }}</span>
                </span>
                <Icon name="arrow-right" :size="14" extra-class="text-mist opacity-0 group-hover:opacity-100 transition-opacity" />
              </button>
            </div>
          </div>
        </div>

        <div class="p-2.5 shrink-0 inset-line-t">
          <button class="btn btn-ghost btn-sm w-full" @click="ui.showServers()">
            <Icon name="server" :size="14" />
            管理服务器
          </button>
        </div>
      </aside>
    </Transition>
  </Teleport>
</template>
