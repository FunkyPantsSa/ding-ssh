<script setup lang="ts">
import {t as $t} from '../../i18n'

import Icon from '@/components/Icon.vue'
import {Button} from '@/components/ui'

defineProps<{
  groups: string[]
  activeGroup: string
  dragOver: string | null
  allCount: number
  ungroupedCount: number
  countFor: (group: string) => number
}>()

const emit = defineEmits<{
  select: [group: string]
  manage: []
  dragover: [event: DragEvent, group: string]
  dragleave: [group: string]
  drop: [event: DragEvent, group: string]
}>()
</script>

<template>
  <aside class="server-sidebar shrink-0 flex flex-col" :aria-label="$t('服务器分组')">
    <div class="flex items-center justify-between px-3 py-2.5 shrink-0 inset-line-b">
      <span class="text-[12px] font-semibold tracking-[0.08em] uppercase text-mist">{{ $t('分组') }}</span>
      <Button variant="ghost" size="icon" :title="$t('分组管理')" :aria-label="$t('分组管理')" @click="emit('manage')"><Icon name="folder-cog" :size="14" /></Button>
    </div>
    <div class="flex-1 min-h-0 overflow-y-auto p-1.5 flex flex-col gap-0.5">
      <button class="side-group-item" :class="[activeGroup === 'all' ? 'active' : '', dragOver === 'all' ? 'drop-target' : '']" @click="emit('select', 'all')" @dragover="emit('dragover', $event, 'all')" @dragleave="emit('dragleave', 'all')" @drop="emit('drop', $event, '')">
        <Icon name="server" :size="14" extra-class="text-mist" /><span class="flex-1 text-left truncate">{{ $t('全部服务器') }}</span><span class="count">{{ allCount }}</span>
      </button>
      <button v-if="ungroupedCount" class="side-group-item" :class="[activeGroup === '' ? 'active' : '', dragOver === '' ? 'drop-target' : '']" @click="emit('select', '')" @dragover="emit('dragover', $event, '')" @dragleave="emit('dragleave', '')" @drop="emit('drop', $event, '')">
        <Icon name="folder" :size="14" extra-class="text-mist" /><span class="flex-1 text-left truncate">{{ $t('未分组') }}</span><span class="count">{{ ungroupedCount }}</span>
      </button>
      <button v-for="group in groups" :key="group" class="side-group-item" :class="[activeGroup === group ? 'active' : '', dragOver === group ? 'drop-target' : '']" @click="emit('select', group)" @dragover="emit('dragover', $event, group)" @dragleave="emit('dragleave', group)" @drop="emit('drop', $event, group)">
        <Icon name="folder" :size="14" extra-class="text-mist" /><span class="flex-1 text-left truncate">{{ group }}</span><span class="count">{{ countFor(group) }}</span>
      </button>
    </div>
    <div class="p-2 shrink-0 inset-line-t"><p class="mb-1.5 px-1 text-[11px] leading-snug text-mist/70">{{ $t('把右侧服务器拖到分组上即可修改分组') }}</p><Button variant="ghost" size="sm" class="w-full" @click="emit('manage')"><Icon name="plus" :size="14" />{{ $t('新建分组') }}</Button></div>
  </aside>
</template>
