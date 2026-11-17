<script setup lang="ts">
import {t as $t} from '../i18n'

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
    <Button v-if="navCollapsed" variant="ghost" size="icon" :title="$t('展开导航')" :aria-label="$t('展开导航')" @click="emit('expandNav')"><Icon name="chevrons-right" :size="15" /></Button>
    <h1>{{ title }}</h1><span class="topbar-sep"></span><span class="topbar-sub">{{ subtitle }}</span>
    <div class="flex-1 min-w-0 flex justify-center px-2"><button class="searchbtn topbar-search" data-ui="command-trigger" :aria-label="$t('打开命令面板')" :title="$t('打开命令面板')" @click="emit('command')"><Icon name="search" :size="14" /><span class="grow">{{ $t('搜索服务器、隧道与命令…') }}</span></button></div>
    <div class="flex items-center gap-1.5 shrink-0">
      <span v-if="workspace" class="chip"><span class="dot"></span> {{ onlineCount }} {{ $t('会话在线') }}</span>
      <Button v-if="workspace" variant="ghost" size="icon" :class="contextVisible ? 'text-[var(--primary-hover)]' : ''" :aria-label="$t('侧栏工具（SFTP / 运行状态）')" :title="$t('侧栏工具（SFTP / 运行状态）')" :aria-pressed="contextVisible" @click="emit('toggleContext')"><Icon name="panel-right" :size="16" /></Button>
      <Button v-if="workspace || servers" variant="ghost" size="sm" :title="$t('新建服务器')" @click="emit('newServer')"><Icon name="plus" :size="14" />{{ $t('新建服务器') }}</Button>
    </div>
  </header>
</template>

<style scoped>
.topbar-search { max-width: 400px; }
</style>
