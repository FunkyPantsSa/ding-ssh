<script lang="ts" setup>
import {t as $t} from '../i18n'

import {t} from '../i18n'

import {computed, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {useGroupsStore} from '../stores/groups'
import {useServersStore} from '../stores/servers'
import {useSessionsStore} from '../stores/sessions'
import {useUIStore} from '../stores/ui'
import type {ServerNode} from '../types'
import {buildGroupTree, type GroupNode} from '../utils/groupTree'
import {Button, Input} from './ui'

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
  // 只在首次展开时拉取：面板是常驻 DOM，若每次展开都重新 load()，
  // 列表会先切成骨架屏再整表重渲染，正好落在展开动画期间 → 掉帧 + 闪烁。
  if (v && !servers.loading && !servers.loadedOnce) {
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

const searching = computed(() => !!keyword.value.trim())

// 分组树：未分组单独置顶；分组节点按完整路径建树，服务器挂在“直接所属”的分组下。
// 搜索时只保留有命中的服务器，避免把空分组也铺出来。
const tree = computed(() => buildGroupTree(searching.value ? [] : groups.list, filtered.value))
const ungroupedServers = computed(() => filtered.value.filter((node) => !node.group.trim()))
const serversByPath = computed(() => {
  const map = new Map<string, ServerNode[]>()
  for (const node of filtered.value) {
    const key = node.group.trim()
    if (!key) continue
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(node)
  }
  return map
})

type QuickRow =
  | {kind: 'ungrouped'; count: number}
  | {kind: 'group'; node: GroupNode; depth: number; open: boolean}
  | {kind: 'server'; node: ServerNode; depth: number}

const rows = computed<QuickRow[]>(() => {
  const out: QuickRow[] = []
  const showAll = searching.value
  if (ungroupedServers.value.length) {
    const open = showAll || !collapsed['']
    out.push({kind: 'ungrouped', count: ungroupedServers.value.length})
    if (open) {
      for (const node of ungroupedServers.value) out.push({kind: 'server', node, depth: 1})
    }
  }
  const walk = (nodes: GroupNode[], depth: number) => {
    for (const node of nodes) {
      const open = showAll || !collapsed[node.path]
      out.push({kind: 'group', node, depth, open})
      if (!open) continue
      for (const server of serversByPath.value.get(node.path) ?? []) {
        out.push({kind: 'server', node: server, depth: depth + 1})
      }
      walk(node.children, depth + 1)
    }
  }
  walk(tree.value, 0)
  return out
})

function indent(depth: number): string {
  return `${8 + depth * 12}px`
}

function groupHasChildren(node: GroupNode): boolean {
  return node.children.length > 0 || node.total > 0
}

function rowKey(row: QuickRow): string {
  if (row.kind === 'server') return `server-${row.node.id}`
  if (row.kind === 'group') return `group-${row.node.path}`
  return 'ungrouped'
}

function isCollapsed(key: string): boolean {
  return !searching.value && !!collapsed[key]
}

function connect(node: ServerNode) {
  // 已收起（连点防抖）：面板收起后按钮仍短暂存在于 DOM，避免同一次连点开出多个标签
  if (!open.value) return
  // 先收面板，下一帧再创建终端标签。
  // TerminalView 挂载（xterm + WebGL 初始化）会同步占用主线程上百毫秒，
  // 若与关闭动画同一帧执行，关闭动画要先被冻结一下再滑动，看起来「卡顿」。
  // 关闭动画（transform/opacity）由合成器驱动，先启动后不受主线程阻塞影响。
  ui.closeTerminalSidebar()
  requestAnimationFrame(() => sessions.openTab(node))
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
    <!-- 入口在左侧导航「快速连接」，此处仅负责浮层与遮罩。
         用 v-show 而非 v-if：面板常驻 DOM，开合只做位移/透明度合成，
         避免每次展开都重建（收起时销毁）整份服务器列表造成掉帧。 -->
    <Transition name="fade">
      <div v-show="open" class="qconn-mask" @click="ui.closeTerminalSidebar()"></div>
    </Transition>

    <Transition name="qconn">
      <aside v-show="open" class="qconn" :aria-label="$t('快速连接')" data-ui="quick-connect">
        <div class="px-4 h-12 flex items-center justify-between shrink-0 inset-line-b">
          <div class="flex items-center gap-2">
            <Icon name="server" :size="16" extra-class="text-signal" />
            <span class="text-[13px] font-semibold text-[var(--foreground)]">{{ $t('服务器') }}</span>
            <span class="text-[12px] text-mist">{{ servers.servers.length }}</span>
          </div>
          <Button variant="ghost" size="icon" :title="$t('收起')" :aria-label="$t('收起')" @click="ui.closeTerminalSidebar()">
            <Icon name="close" :size="14" />
          </Button>
        </div>

        <div class="px-2.5 pt-2.5 pb-2 shrink-0">
          <div class="search">
            <Icon name="search" :size="14" extra-class="search-ico" />
            <Input
              v-model="keyword"
              type="text"
              class="qconn-search-input"
              :placeholder="$t('搜索名称 / 主机 / 分组…')"
              :aria-label="$t('搜索服务器')"
            />
          </div>
        </div>

        <div class="flex-1 min-h-0 overflow-y-auto p-2.5 pt-0 flex flex-col gap-1.5">
          <div v-if="servers.loading && !servers.servers.length" class="space-y-2">
            <div v-for="i in 4" :key="i" class="flex items-center gap-2 px-2.5 py-2">
              <div class="skel w-2 h-2 rounded-full"></div>
              <div class="skel h-3 flex-1"></div>
            </div>
          </div>
          <div v-else-if="!servers.servers.length" class="px-3 py-8 text-center text-xs text-mist flex flex-col items-center gap-3">
            <span>{{ $t('暂无服务器') }}</span>
            <Button variant="primary" size="sm" @click="ui.showServers()">{{ $t('去添加服务器') }}</Button>
          </div>
          <div v-if="servers.servers.length && !rows.length" class="px-3 py-8 text-center text-xs text-mist">{{ $t('无匹配服务器') }}</div>
          <template v-for="row in rows" :key="rowKey(row)">
            <button
              v-if="row.kind === 'ungrouped'"
              class="group-h qc-group"
              :style="{paddingLeft: indent(0)}"
              @click="toggleGroup('')"
            >
              <Icon name="chevron-down" :size="14" extra-class="transition-transform" :class="isCollapsed('') ? '-rotate-90' : ''" />
              <span class="qc-group-label">{{ $t('未分组') }}</span>
              <span class="qc-count">{{ row.count }}</span>
            </button>
            <button
              v-else-if="row.kind === 'group'"
              class="group-h qc-group"
              :style="{paddingLeft: indent(row.depth)}"
              :title="row.node.path"
              @click="toggleGroup(row.node.path)"
            >
              <Icon
                v-if="groupHasChildren(row.node)"
                name="chevron-down"
                :size="14"
                extra-class="transition-transform"
                :class="isCollapsed(row.node.path) ? '-rotate-90' : ''"
              />
              <span v-else class="qc-chevron-empty"></span>
              <span class="qc-group-label">{{ row.node.name }}</span>
              <span class="qc-count">{{ row.node.total }}</span>
            </button>
            <button
              v-else
              class="qconn-item group"
              :class="isActiveNode(row.node) ? 'active' : ''"
              :style="{paddingLeft: indent(row.depth)}"
              :title="t('连接 {0}', [row.node.name])"
              @click="connect(row.node)"
            >
              <span class="status" :class="nodeStatus(row.node)"></span>
              <span class="min-w-0 flex-1 text-left">
                <span class="block text-[13px] font-medium text-[var(--foreground)] truncate">{{ row.node.name }}</span>
                <span class="block font-mono text-[12px] text-mist truncate">{{ row.node.user }}@{{ row.node.host }}:{{ row.node.port }}</span>
              </span>
              <Icon name="arrow-right" :size="14" extra-class="text-mist opacity-0 group-hover:opacity-100 transition-opacity" />
            </button>
          </template>
        </div>

        <div class="p-2.5 shrink-0 inset-line-t">
          <Button variant="ghost" size="sm" class="w-full" @click="ui.showServers()">
            <Icon name="server" :size="14" />{{ $t('管理服务器') }}</Button>
        </div>
      </aside>
    </Transition>
  </Teleport>
</template>
<style scoped>
.qconn-search-input{height:30px}
.search :deep(input.qconn-search-input){padding-left:32px}
.qc-group{display:flex;align-items:center;gap:6px;width:100%;height:28px;padding:0 8px;border-radius:var(--radius-small);font-size:11.5px;font-weight:500;letter-spacing:0;text-transform:none;color:var(--foreground-secondary)}
.qc-group:hover{background:var(--interactive-hover-bg);color:var(--foreground)}
.qc-group-label{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;text-align:left}
.qc-count{min-width:17px;height:17px;padding:0 5px;display:grid;place-items:center;border-radius:var(--radius-small);background:var(--hover);color:var(--muted-foreground);font-family:var(--font-mono);font-size:10px;font-weight:400}
.qc-chevron-empty{width:14px;height:14px;flex:none}
.qconn-item{padding-left:8px}
</style>
