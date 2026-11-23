<script setup lang="ts">
import {t as $t} from '../../i18n'
import Icon from '../Icon.vue'
import {Button, Input} from '../ui'
import GroupTree from './GroupTree.vue'
import type {GroupNode} from '../../utils/groupTree'
import type {GroupSelection} from '../../types'

const props = defineProps<{
  tree: GroupNode[]
  selection: GroupSelection
  dragOver: string | null
  allCount: number
  ungroupedCount: number
  filter: string
  expanded: Set<string>
}>()

const emit = defineEmits<{
  select: [selection: GroupSelection]
  toggle: [path: string]
  manage: []
  createRoot: []
  createChild: []
  collapseAll: []
  'update:filter': [value: string]
  dragOver: [event: DragEvent, target: GroupSelection]
  dragLeave: [target: GroupSelection]
  drop: [event: DragEvent, target: GroupSelection]
  contextMenu: [event: MouseEvent, path: string]
}>()

function isActive(kind: GroupSelection['kind']): boolean {
  return props.selection.kind === kind
}

function ungroupedTarget(): GroupSelection {
  return {kind: 'ungrouped'}
}
</script>

<template>
  <aside class="server-sidebar shrink-0 flex flex-col" :aria-label="$t('服务器分组')">
    <div class="flex items-center justify-between px-3 py-2.5 shrink-0 inset-line-b">
      <span class="text-[12px] font-semibold tracking-[0.08em] uppercase text-mist">{{ $t('分组') }}</span>
      <div class="flex items-center gap-0.5">
        <Button variant="ghost" size="icon" :title="$t('折叠全部分组')" :aria-label="$t('折叠全部分组')" @click="emit('collapseAll')"><Icon name="chevrons-down-up" :size="14" /></Button>
        <Button variant="ghost" size="icon" :title="$t('分组管理')" :aria-label="$t('分组管理')" @click="emit('manage')"><Icon name="folder-cog" :size="14" /></Button>
        <Button variant="ghost" size="icon" :title="$t('新建分组')" :aria-label="$t('新建分组')" @click="emit('createChild')"><Icon name="plus" :size="14" /></Button>
      </div>
    </div>

    <div class="px-2 pt-2 pb-1 shrink-0">
      <div class="search group-search">
        <Icon name="search" :size="13" extra-class="search-ico" />
        <Input
          :model-value="filter"
          class="group-search-input"
          :placeholder="$t('筛选分组…')"
          :aria-label="$t('筛选分组')"
          @update:model-value="emit('update:filter', String($event))"
        />
      </div>
    </div>

    <div class="flex-1 min-h-0 overflow-y-auto p-1.5 pt-1 flex flex-col gap-0.5">
      <button
        type="button"
        class="side-group-item pinned-group"
        :class="[isActive('all') ? 'active' : '']"
        @click="emit('select', {kind: 'all'})"
      >
        <Icon name="server" :size="14" extra-class="text-mist" />
        <span class="flex-1 text-left truncate">{{ $t('全部服务器') }}</span>
        <span class="count">{{ allCount }}</span>
      </button>
      <button
        v-if="ungroupedCount"
        type="button"
        class="side-group-item pinned-group"
        :class="[isActive('ungrouped') ? 'active' : '', dragOver === '' ? 'drop-target' : '']"
        @click="emit('select', ungroupedTarget())"
        @dragover="emit('dragOver', $event, ungroupedTarget())"
        @dragleave="emit('dragLeave', ungroupedTarget())"
        @drop="emit('drop', $event, ungroupedTarget())"
      >
        <Icon name="folder" :size="14" extra-class="text-mist" />
        <span class="flex-1 text-left truncate">{{ $t('未分组') }}</span>
        <span class="count">{{ ungroupedCount }}</span>
      </button>

      <div v-if="tree.length || filter" class="group-tree-wrap">
        <GroupTree
          :nodes="tree"
          :active-path="selection.kind === 'group' ? selection.path : ''"
          :expanded="expanded"
          :drag-over="dragOver"
          @select="emit('select', $event)"
          @toggle="emit('toggle', $event)"
          @drag-over="(event, target) => emit('dragOver', event, target)"
          @drag-leave="emit('dragLeave', $event)"
          @drop="(event, target) => emit('drop', event, target)"
          @context-menu="(event, path) => emit('contextMenu', event, path)"
        />
        <p v-if="filter && !tree.length" class="px-2 py-3 text-[11.5px] text-mist text-center">{{ $t('没有匹配的分组') }}</p>
      </div>
      <p v-else class="px-2 py-3 text-[11.5px] leading-relaxed text-mist/80">{{ $t('还没有分组，点击上方 + 新建。') }}</p>
    </div>

    <div class="p-2 shrink-0 inset-line-t">
      <p class="mb-1.5 px-1 text-[11px] leading-snug text-mist/70">{{ $t('把右侧服务器拖到分组上即可修改分组') }}</p>
      <Button variant="ghost" size="sm" class="w-full" @click="emit('createRoot')">
        <Icon name="plus" :size="14" />{{ $t('新建分组') }}
      </Button>
    </div>
  </aside>
</template>

<style scoped>
.server-sidebar {
  width: 236px;
}
.group-search :deep(.ui-input) {
  height: 28px;
  font-size: 12px;
  padding-left: 30px;
}
.group-search :deep(.search-ico) {
  z-index: 1;
  position: absolute;
  left: 9px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--muted-foreground);
  pointer-events: none;
}
.group-tree-wrap {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin-top: 1px;
}
.group-tree-wrap :deep(.group-tree) {
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.pinned-group {
  font-weight: 500;
}
@media (max-width: 1100px) {
  .server-sidebar {
    width: 208px;
  }
}
</style>
