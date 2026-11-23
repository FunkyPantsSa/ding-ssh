<script setup lang="ts">
import GroupTreeRow from './GroupTreeRow.vue'
import type {GroupNode} from '../../utils/groupTree'
import type {GroupSelection} from '../../types'

defineProps<{
  nodes: GroupNode[]
  activePath: string
  expanded: Set<string>
  dragOver: string | null
  compact?: boolean
  manage?: boolean
}>()

const emit = defineEmits<{
  select: [selection: GroupSelection]
  toggle: [path: string]
  dragOver: [event: DragEvent, target: GroupSelection]
  dragLeave: [target: GroupSelection]
  drop: [event: DragEvent, target: GroupSelection]
  contextMenu: [event: MouseEvent, path: string]
  createChild: [path: string]
  rename: [path: string]
  remove: [path: string]
}>()
</script>

<template>
  <div class="group-tree" role="tree" aria-label="分组树">
    <GroupTreeRow
      v-for="node in nodes"
      :key="node.path"
      :node="node"
      :active-path="activePath"
      :expanded="expanded"
      :drag-over="dragOver"
      :compact="compact"
      :manage="manage"
      @select="emit('select', $event)"
      @toggle="emit('toggle', $event)"
      @drag-over="(event, target) => emit('dragOver', event, target)"
      @drag-leave="emit('dragLeave', $event)"
      @drop="(event, target) => emit('drop', event, target)"
      @context-menu="(event, path) => emit('contextMenu', event, path)"
      @create-child="emit('createChild', $event)"
      @rename="emit('rename', $event)"
      @remove="emit('remove', $event)"
    />
  </div>
</template>
