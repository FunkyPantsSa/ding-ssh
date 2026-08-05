<script lang="ts" setup>
import {useSessionsStore} from '../stores/sessions'

const sessions = useSessionsStore()

const statusColor: Record<string, string> = {
  connecting: 'bg-amber-400',
  connected: 'bg-emerald-400',
  closed: 'bg-slate-500',
  error: 'bg-rose-500',
}

function close(clientId: string) {
  sessions.closeTab(clientId)
}
</script>

<template>
  <div class="flex items-center gap-1 px-2 py-1.5 border-b border-slate-700/60 bg-slate-900/60 overflow-x-auto no-scrollbar shrink-0">
    <div
      v-for="tab in sessions.tabs"
      :key="tab.clientId"
      class="group flex items-center gap-2 pl-3 pr-1.5 py-1 rounded-md text-[13px] cursor-pointer select-none shrink-0 transition-colors"
      :class="
        tab.clientId === sessions.activeId
          ? 'bg-slate-700/70 text-slate-100'
          : 'text-slate-400 hover:bg-slate-800/70 hover:text-slate-200'
      "
      @click="sessions.activeId = tab.clientId"
      @auxclick.middle="close(tab.clientId)"
    >
      <span class="w-2 h-2 rounded-full" :class="statusColor[tab.status] ?? 'bg-slate-500'"></span>
      <span class="max-w-40 truncate">{{ tab.serverName }}</span>
      <span
        v-if="tab.status === 'connecting'"
        class="w-3 h-3 border-2 border-slate-400 border-t-transparent rounded-full animate-spin"
      ></span>
      <button
        class="ml-1 px-1 rounded text-slate-500 hover:bg-slate-600/60 hover:text-slate-100"
        title="关闭 (中键)"
        @click.stop="close(tab.clientId)"
      >
        ×
      </button>
    </div>

    <div class="flex-1"></div>
    <button
      v-if="sessions.activeTab"
      class="px-2 py-1 rounded-md text-xs transition-colors"
      :class="sessions.activeTab.status === 'connected' ? 'text-slate-400 hover:text-sky-300' : 'text-slate-600 cursor-default'"
      :disabled="sessions.activeTab.status !== 'connected'"
      :title="sessions.activeTab.status === 'connected' ? '切换右侧 SFTP 面板' : '连接后可显示 SFTP 面板'"
      @click="sessions.toggleSftp()"
    >
      {{ sessions.sftpVisible ? '隐藏 SFTP' : '显示 SFTP' }}
    </button>
    <button
      v-if="sessions.tabs.length > 1"
      class="px-2 py-1 rounded-md text-xs text-slate-400 hover:text-rose-300 hover:bg-slate-800"
      title="关闭全部标签页"
      @click="sessions.closeAll()"
    >
      全部关闭
    </button>
  </div>
</template>

<style scoped>
.no-scrollbar::-webkit-scrollbar {
  display: none;
}
</style>
