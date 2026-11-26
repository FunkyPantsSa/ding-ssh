<script setup lang="ts">
import {t} from '../i18n'
import Icon from '@/components/Icon.vue'
import {WindowClose, WindowMinimise, WindowToggleMaximise} from '@/services/desktop'

defineProps<{windows: boolean; onlineCount: number}>()
</script>

<template>
  <header class="window-drag-region" data-ui="window-titlebar" @dblclick="WindowToggleMaximise">
    <div class="window-drag-title flex items-center gap-2">
      <span class="text-[11px] font-mono tracking-wide opacity-50">ding-ssh · fluid glass workspace</span>
    </div>
    <div v-if="onlineCount > 0" class="window-drag-status flex items-center gap-1.5 text-[11px] text-[var(--muted-foreground)]">
      <span class="inline-block w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
      <span>{{ t('已连接 {0} 台节点', [onlineCount]) }}</span>
    </div>
    <div v-if="windows" class="window-controls">
      <button type="button" :title="t('最小化')" :aria-label="t('最小化')" @dblclick.stop @click.stop="WindowMinimise"><Icon name="minus" :size="14" /></button>
      <button type="button" :title="t('最大化或还原')" :aria-label="t('最大化或还原')" @dblclick.stop @click.stop="WindowToggleMaximise"><Icon name="square" :size="12" /></button>
      <button type="button" :title="t('关闭窗口')" :aria-label="t('关闭窗口')" @dblclick.stop @click.stop="WindowClose"><Icon name="close" :size="16" /></button>
    </div>
  </header>
</template>
