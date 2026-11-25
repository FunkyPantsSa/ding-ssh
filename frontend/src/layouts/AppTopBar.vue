<script setup lang="ts">
import {t as $t} from '../i18n'

import Icon from '@/components/Icon.vue'
import {Button} from '@/components/ui'
import type {SessionTab} from '@/types'

defineProps<{
  title: string
  subtitle: string
  navCollapsed: boolean
  workspace: boolean
  onlineCount: number
  contextVisible: boolean
  tab?: SessionTab
  rightPanel?: string
}>()
const emit = defineEmits<{
  expandNav: []
  command: []
  toggleContext: []
  togglePanel: [panel: 'sftp' | 'sysinfo' | 'transfers']
}>()
</script>

<template>
  <header class="topbar" data-ui="topbar">
    <Button v-if="navCollapsed" variant="ghost" size="icon" :title="$t('展开导航')" :aria-label="$t('展开导航')" @click="emit('expandNav')">
      <Icon name="chevrons-right" :size="14" />
    </Button>

    <!-- 工作区有会话时：微型上下文状态 (标题 + 主机 + 状态指示) -->
    <div v-if="workspace && tab" class="flex items-center gap-2 min-w-0">
      <template v-if="tab.kind === 'local'">
        <span class="font-semibold text-[var(--foreground)] text-xs truncate max-w-[160px] md:max-w-[240px]">
          {{ tab.serverName || $t('本地终端') }}
        </span>
      </template>
      <template v-else-if="tab.serverName && tab.serverName !== tab.host">
        <span class="font-semibold text-[var(--foreground)] text-xs truncate max-w-[140px] md:max-w-[200px]">
          {{ tab.serverName }}
        </span>
        <span v-if="tab.host" class="text-[11px] text-[var(--muted-foreground)] font-mono truncate hidden sm:inline">
          {{ tab.user ? `${tab.user}@` : '' }}{{ tab.host }}{{ tab.node?.port ? `:${tab.node.port}` : '' }}
        </span>
      </template>
      <template v-else>
        <span class="font-semibold text-[var(--foreground)] text-xs font-mono truncate max-w-[220px] md:max-w-[320px]">
          {{ tab.user ? `${tab.user}@` : '' }}{{ tab.host || tab.serverName || title }}{{ tab.node?.port ? `:${tab.node.port}` : '' }}
        </span>
      </template>
      <span v-if="tab.status === 'connected'" class="px-1.5 py-0.5 rounded-[6px] text-[10px] bg-emerald-500/15 text-emerald-400 border border-emerald-500/20 font-mono shrink-0 flex items-center gap-1">
        <span class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
        {{ $t('已连接') }}
      </span>
      <span v-else-if="tab.status === 'connecting'" class="px-1.5 py-0.5 rounded-[6px] text-[10px] bg-amber-500/15 text-amber-400 border border-amber-500/20 font-mono shrink-0">
        {{ $t('连接中…') }}
      </span>
      <span v-else-if="tab.status === 'error'" class="px-1.5 py-0.5 rounded-[6px] text-[10px] bg-rose-500/15 text-rose-400 border border-rose-500/20 font-mono shrink-0">
        {{ $t('连接异常') }}
      </span>
    </div>

    <!-- 普通页面标题 -->
    <div v-else class="flex items-center gap-2 min-w-0">
      <h1>{{ title }}</h1>
      <span v-if="subtitle" class="topbar-sep"></span>
      <span v-if="subtitle" class="topbar-sub">{{ subtitle }}</span>
    </div>

    <!-- 居中全局搜索胶囊 -->
    <div class="flex-1 min-w-0 flex justify-center px-2">
      <button class="searchbtn topbar-search" data-ui="command-trigger" :aria-label="$t('打开命令面板')" :title="$t('打开命令面板')" @click="emit('command')">
        <Icon name="search" :size="13" extra-class="text-[var(--primary)] shrink-0" />
        <span class="grow truncate">{{ $t('搜索服务器、隧道与命令…') }}</span>
        <kbd class="topbar-kbd">⌘K</kbd>
      </button>
    </div>

    <!-- 右侧上下文操作胶囊 -->
    <div class="flex items-center gap-1.5 shrink-0">
      <button
        v-if="workspace"
        class="topbar-chip-btn"
        :class="contextVisible && (!rightPanel || rightPanel === 'sftp') ? 'active' : ''"
        :aria-label="$t('侧栏工具（SFTP / 运行状态）')"
        :title="$t('侧栏工具（SFTP / 运行状态）')"
        :aria-pressed="contextVisible && (!rightPanel || rightPanel === 'sftp')"
        @click="emit('toggleContext')"
      >
        <Icon name="folder" :size="12" />
        <span>{{ $t('SFTP 抽屉') }}</span>
      </button>

      <button
        v-if="workspace"
        class="topbar-chip-btn"
        :class="contextVisible && rightPanel === 'sysinfo' ? 'active' : ''"
        :aria-pressed="contextVisible && rightPanel === 'sysinfo'"
        :title="$t('监控看板')"
        @click="emit('togglePanel', 'sysinfo')"
      >
        <Icon name="activity" :size="12" />
        <span>{{ $t('监控看板') }}</span>
      </button>

      <span v-if="workspace && !tab" class="chip"><span class="dot"></span> {{ onlineCount }} {{ $t('会话在线') }}</span>
    </div>
  </header>
</template>

<style scoped>
.topbar-search {
  max-width: 320px;
  height: 26px;
  padding: 0 10px;
  gap: 6px;
  border-radius: var(--radius-control, 8px);
}
.topbar-kbd {
  font-size: 9px;
  font-family: var(--font-mono);
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--interactive-hover-bg, rgba(255, 255, 255, 0.08));
  color: var(--text-muted);
  border: var(--glass-border-subtle);
}
</style>
