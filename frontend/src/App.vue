<script lang="ts" setup>
import {computed, onMounted} from 'vue'
import ServerList from './components/ServerList.vue'
import SettingsPage from './components/SettingsPage.vue'
import SftpPanel from './components/SftpPanel.vue'
import TabBar from './components/TabBar.vue'
import TerminalView from './components/TerminalView.vue'
import {useSessionsStore} from './stores/sessions'
import {useSettingsStore} from './stores/settings'
import {useUIStore} from './stores/ui'

const sessions = useSessionsStore()
const ui = useUIStore()
const settings = useSettingsStore()

// 当前处于已连接状态的会话（用于右侧 SFTP 面板）。
const activeConnected = computed(() =>
  sessions.activeTab?.status === 'connected' ? sessions.activeTab : undefined,
)

onMounted(() => {
  // 启动即加载设置与凭证
  void settings.load()
})
</script>

<template>
  <div class="flex h-full">
    <!-- 左侧：服务器列表 / 设置导航 -->
    <aside
      class="w-64 shrink-0 border-r border-slate-700/60 bg-slate-900/70 backdrop-blur-md flex flex-col"
    >
      <div class="flex-1 min-h-0">
        <ServerList v-if="ui.view === 'workspace'" />
        <div
          v-else
          class="h-full flex flex-col items-center justify-center gap-3 px-4"
        >
          <p class="text-lg font-semibold text-slate-200">ding-ssh</p>
          <p class="text-xs text-slate-500">应用设置</p>
        </div>
      </div>
      <nav class="flex border-t border-slate-700/60">
        <button
          class="flex-1 py-2.5 text-xs tracking-wide transition-colors"
          :class="
            ui.view === 'workspace'
              ? 'text-sky-400 bg-slate-800/60'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
          "
          @click="ui.showWorkspace()"
        >
          ▣ 终端
        </button>
        <button
          class="flex-1 py-2.5 text-xs tracking-wide transition-colors"
          :class="
            ui.view === 'settings'
              ? 'text-sky-400 bg-slate-800/60'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
          "
          @click="ui.showSettings()"
        >
          ⚙ 设置
        </button>
      </nav>
    </aside>

    <!-- 右侧：设置页 / 标签 + 终端（v-show 保持 SSH 会话不中断） -->
    <main class="flex-1 min-w-0 flex flex-col">
      <SettingsPage v-show="ui.view === 'settings'" />

      <div v-show="ui.view === 'workspace'" class="flex-1 min-h-0 flex flex-col">
        <TabBar v-if="sessions.tabs.length" />

        <div class="flex-1 min-h-0 flex">
          <div class="flex-1 min-w-0 relative terminal-bg">
            <TerminalView
              v-for="tab in sessions.tabs"
              v-show="tab.clientId === sessions.activeId"
              :key="tab.clientId"
              :tab="tab"
            />

            <div
              v-if="!sessions.tabs.length"
              class="absolute inset-0 flex flex-col items-center justify-center gap-3"
            >
              <p class="text-2xl font-light text-slate-500">ding-ssh</p>
              <p class="text-sm text-slate-600">在左侧选择服务器，点击 ▶ 建立 SSH 连接</p>
            </div>
          </div>

          <!-- 右侧 SFTP 面板 -->
          <SftpPanel v-if="activeConnected && sessions.sftpVisible" :tab="activeConnected" />
        </div>
      </div>
    </main>
  </div>
</template>
