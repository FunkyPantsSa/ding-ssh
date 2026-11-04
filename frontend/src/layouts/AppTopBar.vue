<script setup lang="ts">
import Icon from '@/components/Icon.vue'
import {Button} from '@/components/ui'

defineProps<{
  title: string
  subtitle: string
  navCollapsed: boolean
  workspace: boolean
  servers: boolean
  onlineCount: number
  contextVisible: boolean
}>()
const emit = defineEmits<{
  expandNav: []
  command: []
  toggleContext: []
  newServer: []
}>()
</script>

<template>
  <header class="topbar" data-ui="topbar">
    <Button v-if="navCollapsed" variant="ghost" size="icon" title="展开导航" aria-label="展开导航" @click="emit('expandNav')"><Icon name="chevrons-right" :size="15" /></Button>
    <h1>{{ title }}</h1><span class="topbar-sep"></span><span class="topbar-sub">{{ subtitle }}</span>
    <div class="flex-1 min-w-0 flex justify-center px-2"><button class="searchbtn" style="max-width:400px" title="打开命令面板（⌘K）" @click="emit('command')"><Icon name="search" :size="14" /><span class="grow">搜索服务器、隧道与命令…</span><span class="kbd">⌘K</span></button></div>
    <div class="flex items-center gap-1.5 shrink-0">
      <span v-if="workspace" class="chip"><span class="dot"></span>{{ onlineCount }} 会话在线</span>
      <Button v-if="workspace" variant="ghost" size="icon" :class="contextVisible ? 'text-[var(--primary-hover)]' : ''" title="侧栏工具（SFTP / 系统看板）" :aria-pressed="contextVisible" @click="emit('toggleContext')"><Icon name="panel-right" :size="16" /></Button>
      <Button v-if="workspace || servers" variant="ghost" size="sm" title="新建服务器" @click="emit('newServer')"><Icon name="plus" :size="14" />新建服务器</Button>
    </div>
  </header>
</template>
