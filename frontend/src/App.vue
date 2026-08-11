<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from './components/Icon.vue'
import ServerList from './components/ServerList.vue'
import SettingsPage from './components/SettingsPage.vue'
import SftpPanel from './components/SftpPanel.vue'
import TabBar from './components/TabBar.vue'
import TerminalView from './components/TerminalView.vue'
import TunnelPage from './components/TunnelPage.vue'
import {useSessionsStore} from './stores/sessions'
import {useSettingsStore} from './stores/settings'
import {useUIStore} from './stores/ui'

const sessions = useSessionsStore()
const ui = useUIStore()
const settings = useSettingsStore()

// 顶部标题栏副标题：按当前页面展示。
const pageSubtitle = computed(() => {
  if (ui.view === 'tunnel') return 'SSH 隧道'
  if (ui.view === 'settings') return '应用设置'
  return '服务器列表'
})

// 当前处于已连接状态的会话（用于右侧 SFTP 面板）。
const activeConnected = computed(() =>
  sessions.activeTab?.status === 'connected' ? sessions.activeTab : undefined,
)

function onGlobalKeydown(e: KeyboardEvent) {
  const meta = e.metaKey || e.ctrlKey
  if (!meta) return

  // Cmd+T / Ctrl+T: 激活左侧服务器列表搜索框
  if (e.key === 't') {
    // 默认浏览器行为是新建标签页，在桌面应用中不冲突
    // 聚焦到搜索框由 ServerList 自己处理 (通过事件或自动聚焦)
    return
  }

  // Cmd+W / Ctrl+W: 关闭当前标签
  if (e.key === 'w' && ui.view === 'workspace') {
    e.preventDefault()
    if (sessions.activeId) sessions.closeTab(sessions.activeId)
  }

  // Cmd+1~9: 切换到第 N 个标签
  const n = parseInt(e.key)
  if (n >= 1 && n <= 9 && sessions.tabs.length >= n) {
    e.preventDefault()
    sessions.activeId = sessions.tabs[n - 1].clientId
  }
}

onMounted(() => {
  void settings.load()
  window.addEventListener('keydown', onGlobalKeydown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
})
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <!-- 顶部标题栏：应用标题 + 页面副标题 + 主导航 -->
    <header
      class="h-12 shrink-0 flex items-center gap-3 px-4 border-b border-slate-700/60 bg-slate-900/70 backdrop-blur-md"
    >
      <p class="text-sm font-semibold text-slate-100">ding-ssh</p>
      <p class="text-[11px] text-slate-500">{{ pageSubtitle }}</p>
      <nav class="ml-auto flex items-center gap-1">
        <button
          class="px-3 py-1.5 rounded-md text-xs tracking-wide transition-colors"
          :class="
            ui.view === 'workspace'
              ? 'text-sky-400 bg-slate-800/60'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
          "
          @click="ui.showWorkspace()"
        >
          <Icon name="terminal" size="14" class="mr-1" /> 终端
        </button>
        <button
          class="px-3 py-1.5 rounded-md text-xs tracking-wide transition-colors"
          :class="
            ui.view === 'tunnel'
              ? 'text-sky-400 bg-slate-800/60'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
          "
          @click="ui.showTunnel()"
        >
          <Icon name="tunnel" size="14" class="mr-1" /> 隧道
        </button>
        <button
          class="px-3 py-1.5 rounded-md text-xs tracking-wide transition-colors"
          :class="
            ui.view === 'settings'
              ? 'text-sky-400 bg-slate-800/60'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/40'
          "
          @click="ui.showSettings()"
        >
          <Icon name="gear" size="14" class="mr-1" /> 设置
        </button>
      </nav>
    </header>

    <div class="flex-1 min-h-0 flex">
      <!-- 左侧：仅工作区显示服务器列表，隧道/设置页不占用左侧空间 -->
      <aside
        v-if="ui.view === 'workspace'"
        class="shrink-0 border-r border-slate-700/60 bg-slate-900/70 backdrop-blur-md flex flex-col relative"
        :style="{width: sidebarWidth + 'px'}"
      >
        <ServerList />
        <!-- 拖拽手柄 -->
        <div
          class="absolute top-0 right-0 w-1.5 h-full cursor-col-resize hover:bg-sky-500/40 active:bg-sky-500/60 transition-colors z-10"
          :class="resizing ? 'bg-sky-500/60' : ''"
          @mousedown.prevent="startResize"
        ></div>
      </aside>

      <!-- 右侧：设置页 / 隧道页 / 标签 + 终端（v-show 保持 SSH 会话不中断） -->
      <main class="flex-1 min-w-0 flex flex-col">
        <SettingsPage v-show="ui.view === 'settings'" />
        <TunnelPage v-show="ui.view === 'tunnel'" />

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
                class="absolute inset-0 flex flex-col items-center justify-center gap-4"
              >
                <div class="flex items-center justify-center w-16 h-16 rounded-2xl bg-slate-800/60 border border-slate-700/40">
                  <Icon name="terminal" size="32" class="text-slate-500" />
                </div>
                <p class="text-xl font-light text-slate-500">ding-ssh</p>
                <p class="text-sm text-slate-600">在左侧选择服务器，点击连接按钮建立 SSH 连接</p>
              </div>
            </div>

            <!-- 右侧 SFTP 面板 -->
            <SftpPanel v-if="activeConnected && sessions.sftpVisible" :tab="activeConnected" />
          </div>
        </div>
      </main>
    </div>
  </div>
</template>
