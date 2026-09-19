<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from './Icon.vue'
import {useSessionsStore} from '../stores/sessions'
import {getElementRect} from '../utils/dom'
import type {SessionTab, SplitDirection} from '../types'

const sessions = useSessionsStore()

type TabDropPosition = 'before' | 'after'

interface TabDropState {
  clientId: string
  position: TabDropPosition
}

const statusDot: Record<string, string> = {
  connecting: 'bg-[var(--warn-500)]',
  connected: 'bg-[var(--signal-400)] shadow-[0_0_8px_var(--signal-glow)]',
  closed: 'bg-[var(--mist-400)]',
  error: 'bg-[var(--danger-500)]',
}

interface SplitGroupItem {
  key: string
  kind: 'split'
  tabIds: string[]
  title: string
  status: string
}
type TabBarItem = {key: string; kind: 'tab'; tab: SessionTab} | SplitGroupItem

/** 分屏组内所有叶子绑定的标签 id（分屏状态下这些标签在 TabBar 合并为一个标签）。 */
const splitTabIds = computed<Set<string>>(() => {
  if (!sessions.panes) return new Set<string>()
  const set = new Set<string>()
  for (const leaf of sessions.collectLeaves(sessions.panes)) {
    if (leaf.paneActiveId) set.add(leaf.paneActiveId)
  }
  return set
})

/** 分屏组标签（按 tabs 原顺序）。 */
const splitGroupTabs = computed<SessionTab[]>(() =>
  sessions.tabs.filter((t) => splitTabIds.value.has(t.clientId)),
)

function groupStatus(tabs: SessionTab[]): string {
  const order: SessionTab['status'][] = ['error', 'connecting', 'closed', 'connected']
  for (const s of order) {
    if (tabs.some((t) => t.status === s)) return s
  }
  return tabs[0]?.status ?? 'connecting'
}

/** 供模板渲染：分屏组折叠为单个合并标签，其余标签保持独立。 */
const tabItems = computed<TabBarItem[]>(() => {
  const ids = splitTabIds.value
  const items: TabBarItem[] = []
  let inserted = false
  for (const t of sessions.tabs) {
    if (ids.has(t.clientId)) {
      if (!inserted) {
        const group = splitGroupTabs.value
        items.push({
          key: 'split',
          kind: 'split',
          tabIds: group.map((x) => x.clientId),
          title: group.map((x) => x.serverName).join(' ｜ '),
          status: groupStatus(group),
        })
        inserted = true
      }
      continue
    }
    items.push({key: 'tab-' + t.clientId, kind: 'tab', tab: t})
  }
  return items
})

/** 分屏组标签是否处于「工作区正在显示分屏」状态（用于高亮合并标签）。 */
const splitActive = computed(() => sessions.splitShown)

/** 点击合并标签：回到分屏布局并聚焦当前焦点格。 */
function activateSplitGroup() {
  sessions.showSplit()
}

const tabMenu = ref<{x: number; y: number; clientId: string} | null>(null)
const splitSubmenu = ref(false)
const splitCloseTimer = ref<ReturnType<typeof setTimeout> | null>(null)
const draggingId = ref('')
const dropState = ref<TabDropState | null>(null)

function cancelSplitClose() {
  if (splitCloseTimer.value) {
    clearTimeout(splitCloseTimer.value)
    splitCloseTimer.value = null
  }
}

/** 鼠标离开分屏项后延迟关闭子菜单，给「移动到子菜单」留出时间；进入子菜单则取消关闭。 */
function scheduleSplitClose() {
  cancelSplitClose()
  splitCloseTimer.value = setTimeout(() => {
    splitSubmenu.value = false
    splitCloseTimer.value = null
  }, 150)
}

function close(clientId: string) {
  sessions.closeTab(clientId)
}

function openTabMenu(e: MouseEvent, clientId: string) {
  e.preventDefault()
  tabMenu.value = {x: Math.min(e.clientX, window.innerWidth - 160), y: Math.min(e.clientY, window.innerHeight - 160), clientId}
  splitSubmenu.value = false
}

function closeTabMenu() {
  cancelSplitClose()
  tabMenu.value = null
  splitSubmenu.value = false
}

/** 右键分屏：把右键标签合并到当前活跃标签，direction 表示右键标签位于活跃标签的哪一侧。 */
function splitTab(clientId: string, dir: SplitDirection) {
  if (!sessions.tabs.some((t) => t.clientId === clientId)) {
    closeTabMenu()
    return
  }
  const targetId = sessions.activeId && sessions.activeId !== clientId
    ? sessions.activeId
    : sessions.tabs.find((t) => t.clientId !== clientId)?.clientId
  if (!targetId) {
    closeTabMenu()
    return
  }
  sessions.mergeTabsForSplit(clientId, targetId, dir)
  closeTabMenu()
}

function duplicateTab(clientId: string) {
  const tab = sessions.tabs.find((t) => t.clientId === clientId)
  if (!tab) {
    closeTabMenu()
    return
  }
  if (tab.kind === 'local') sessions.openLocalTab(tab.serverName)
  else sessions.openTab(tab.node)
  closeTabMenu()
}

function closeOthers(clientId: string) {
  if (sessions.tabs.length <= 1) { closeTabMenu(); return }
  for (const t of sessions.tabs) {
    if (t.clientId !== clientId) sessions.closeTab(t.clientId)
  }
  closeTabMenu()
}

function closeAll() {
  sessions.closeAll()
  closeTabMenu()
}

const submenuStyle = computed(() => {
  if (!tabMenu.value) return {}
  // 子菜单出现在主菜单右侧
  return {left: (tabMenu.value.x + 160) + 'px', top: tabMenu.value.y + 'px'}
})

function clearDragState() {
  draggingId.value = ''
  sessions.draggingTabId = ''
  sessions.draggingSplitGroup = false
  dropState.value = null
}

function onTabDragStart(e: DragEvent, clientId: string) {
  draggingId.value = clientId
  sessions.draggingTabId = clientId
  sessions.draggingSplitGroup = false
  dropState.value = null
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', clientId)
  }
}

function onTabDragOver(e: DragEvent, clientId: string) {
  if (!draggingId.value || draggingId.value === clientId) return
  const el = e.currentTarget as HTMLElement | null
  if (!el) return
  // 落点比较需在同一坐标系：clientX 含 zoom，rect 用 getElementRect 折算为布局像素。
  const rect = getElementRect(el)
  const position: TabDropPosition = e.clientX < rect.left + rect.width / 2 ? 'before' : 'after'
  dropState.value = {clientId, position}
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
}

function onTabDrop(e: DragEvent, clientId: string) {
  if (!draggingId.value) return
  const position = dropState.value?.clientId === clientId ? dropState.value.position : 'before'
  sessions.moveTab(draggingId.value, clientId, position)
  clearDragState()
}

/** 拖拽「合并标签」到 TabBar 空白处：取消分屏，恢复独立标签。 */
function onSplitGroupDragStart(e: DragEvent) {
  sessions.draggingSplitGroup = true
  draggingId.value = ''
  sessions.draggingTabId = ''
  dropState.value = null
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', '__split-group__')
  }
}

function onTabBarDrop() {
  if (sessions.draggingSplitGroup) {
    sessions.cancelSplit()
  } else if (draggingId.value) {
    sessions.moveTabToEnd(draggingId.value)
  }
  clearDragState()
}

onMounted(() => {
  window.addEventListener('click', closeTabMenu)
})

onBeforeUnmount(() => {
  cancelSplitClose()
  window.removeEventListener('click', closeTabMenu)
})
</script>

<template>
  <div class="tabs no-scrollbar">
    <template v-for="item in tabItems" :key="item.key">
      <!-- 独立标签：点击切换、拖拽排序或拖到另一个标签上重合分屏 -->
      <button
        v-if="item.kind === 'tab'"
        class="tab group"
        :class="[
          item.tab.clientId === sessions.activeId ? 'active' : '',
          draggingId === item.tab.clientId ? 'dragging' : '',
          dropState?.clientId === item.tab.clientId && dropState.position === 'before' ? 'drop-before' : '',
          dropState?.clientId === item.tab.clientId && dropState.position === 'after' ? 'drop-after' : '',
        ]"
        @click="sessions.activateTab(item.tab.clientId)"
        @auxclick.middle="close(item.tab.clientId)"
        @contextmenu.prevent="openTabMenu($event, item.tab.clientId)"
        draggable="true"
        @dragstart="onTabDragStart($event, item.tab.clientId)"
        @dragend="clearDragState"
        @dragover.prevent="onTabDragOver($event, item.tab.clientId)"
        @drop.prevent="onTabDrop($event, item.tab.clientId)"
      >
        <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="statusDot[item.tab.status] ?? 'bg-[var(--mist-400)]'"></span>
        <span class="truncate">{{ item.tab.serverName }}</span>
        <span
          class="w-4 h-4 rounded grid place-items-center opacity-0 group-hover:opacity-100 text-mist hover:bg-white/10 hover:text-[var(--mist-100)]"
          :class="item.tab.clientId === sessions.activeId ? '!opacity-100' : ''"
          title="关闭"
          @click.stop="close(item.tab.clientId)"
        >
          <Icon name="close" :size="10" />
        </span>
      </button>

      <!-- 合并标签：代表整个分屏组；拖到空白处取消分屏 -->
      <button
        v-else
        class="tab group split-tab"
        :class="[splitActive ? 'active' : '']"
        title="拖到空白处取消分屏"
        @click="activateSplitGroup"
        @contextmenu.prevent
        draggable="true"
        @dragstart="onSplitGroupDragStart"
        @dragend="clearDragState"
      >
        <Icon name="columns" :size="12" class="shrink-0 text-signal" />
        <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="statusDot[item.status] ?? 'bg-[var(--mist-400)]'"></span>
        <span class="truncate">{{ item.title }}</span>
        <span
          class="w-4 h-4 rounded grid place-items-center opacity-0 group-hover:opacity-100 text-mist hover:bg-white/10 hover:text-[var(--mist-100)]"
          :class="splitActive ? '!opacity-100' : ''"
          title="关闭分屏组"
          @click.stop="sessions.closeSplitGroup()"
        >
          <Icon name="close" :size="10" />
        </span>
      </button>
    </template>

    <div class="flex-1" @dragover.prevent @drop.prevent="onTabBarDrop"></div>
  </div>
  <Teleport to="body">
    <div
      v-if="tabMenu"
      class="menu-pop neo fixed"
      :style="{left: tabMenu.x + 'px', top: tabMenu.y + 'px'}"
      @contextmenu.prevent
      @click.stop
    >
      <button class="!flex items-center gap-1.5" @click="duplicateTab(tabMenu.clientId)">
        <Icon name="copy" :size="13" />
        复制终端
      </button>
      <div class="divider-h my-1"></div>
      <!-- 分屏子菜单 -->
      <div class="relative" @mouseenter="cancelSplitClose(); splitSubmenu = true" @mouseleave="scheduleSplitClose">
        <button class="!flex items-center gap-1.5 justify-between w-full">
          <span class="flex items-center gap-1.5">
            <Icon name="columns" :size="13" />
            分屏
          </span>
          <Icon name="chevron-right" :size="12" class="text-mist" />
        </button>
        <div
          v-if="splitSubmenu"
          class="menu-pop neo fixed split-submenu"
          :style="submenuStyle"
          @mouseenter="cancelSplitClose()"
          @mouseleave="scheduleSplitClose"
        >
          <button @click="splitTab(tabMenu.clientId, 'left')">分屏到左侧</button>
          <button @click="splitTab(tabMenu.clientId, 'right')">分屏到右侧</button>
          <button @click="splitTab(tabMenu.clientId, 'up')">分屏到上方</button>
          <button @click="splitTab(tabMenu.clientId, 'down')">分屏到下方</button>
        </div>
      </div>
      <div class="divider-h my-1"></div>
      <button @click="sessions.activateTab(tabMenu.clientId); closeTabMenu()">切换到此标签</button>
      <button @click="close(tabMenu.clientId); closeTabMenu()">关闭标签</button>
      <button @click="closeOthers(tabMenu.clientId)">关闭其他标签</button>
      <button class="danger" @click="closeAll">关闭全部标签</button>
    </div>
  </Teleport>
</template>
