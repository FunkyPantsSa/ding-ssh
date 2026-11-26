<script setup lang="ts">
import {t as $t} from '../../i18n'
import {computed, provide, ref, watch} from 'vue'
import GroupTreeRow from './GroupTreeRow.vue'
import type {GroupNode} from '../../utils/groupTree'
import type {GroupSelection} from '../../types'

const props = defineProps<{
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

const focusedPath = ref('')
const visiblePaths = computed(() => {
  const paths: string[] = []
  function visit(nodes: GroupNode[]) {
    for (const node of nodes) {
      paths.push(node.path)
      if (props.expanded.has(node.path)) visit(node.children)
    }
  }
  visit(props.nodes)
  return paths
})
const tabStopPath = computed(() => visiblePaths.value.includes(focusedPath.value) ? focusedPath.value
  : visiblePaths.value.includes(props.activePath) ? props.activePath : visiblePaths.value[0] ?? '')
provide('ding-group-tree-keyboard', {tabStopPath, focus: (path: string) => { focusedPath.value = path }})
watch(visiblePaths, paths => {
  if (!paths.includes(focusedPath.value)) focusedPath.value = ''
})
</script>

<template>
  <div class="group-tree" role="tree" :aria-label="$t('分组树')">
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
