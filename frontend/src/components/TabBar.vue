<script lang="ts" setup>
import {onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from './Icon.vue'
import {useSessionsStore} from '../stores/sessions'

const sessions = useSessionsStore()

const statusDot: Record<string, string> = {
  connecting: 'bg-[var(--warn-500)]',
  connected: 'bg-[var(--signal-400)] shadow-[0_0_8px_var(--signal-glow)]',
  closed: 'bg-[var(--mist-400)]',
  error: 'bg-[var(--danger-500)]',
}

const tabMenu = ref<{x: number; y: number; clientId: string} | null>(null)

function close(clientId: string) {
  sessions.closeTab(clientId)
}

function openTabMenu(e: MouseEvent, clientId: string) {
  e.preventDefault()
  tabMenu.value = {x: Math.min(e.clientX, window.innerWidth - 160), y: Math.min(e.clientY, window.innerHeight - 120), clientId}
}

function closeTabMenu() {
  tabMenu.value = null
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

onMounted(() => {
  window.addEventListener('click', closeTabMenu)
})

onBeforeUnmount(() => {
  window.removeEventListener('click', closeTabMenu)
})
</script>

<template>
  <div class="tabs no-scrollbar">
    <button
      v-for="tab in sessions.tabs"
      :key="tab.clientId"
      class="tab group"
      :class="tab.clientId === sessions.activeId ? 'active' : ''"
      @click="sessions.activeId = tab.clientId"
      @auxclick.middle="close(tab.clientId)"
      @contextmenu.prevent="openTabMenu($event, tab.clientId)"
    >
      <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="statusDot[tab.status] ?? 'bg-[var(--mist-400)]'"></span>
      <span class="truncate">{{ tab.serverName }}</span>
      <span
        class="w-4 h-4 rounded grid place-items-center opacity-0 group-hover:opacity-100 text-mist hover:bg-white/10 hover:text-[var(--mist-100)]"
        :class="tab.clientId === sessions.activeId ? '!opacity-100' : ''"
        title="关闭"
        @click.stop="close(tab.clientId)"
      >
        <Icon name="close" :size="10" />
      </span>
    </button>

    <div class="flex-1"></div>
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
      <button @click="sessions.activeId = tabMenu.clientId; closeTabMenu()">切换到此标签</button>
      <button @click="close(tabMenu.clientId); closeTabMenu()">关闭标签</button>
      <button @click="closeOthers(tabMenu.clientId)">关闭其他标签</button>
      <button class="danger" @click="closeAll">关闭全部标签</button>
    </div>
  </Teleport>
</template>
