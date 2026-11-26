<script setup lang="ts">
import {t as $t} from '../../i18n'
import {computed, inject, type ComputedRef} from 'vue'
import {isMenuItemAvailable} from '../../composables/useMenuKeyboard'
import {getDisplayRect} from '../../utils/dom'
import Icon from '../Icon.vue'
import type {GroupNode} from '../../utils/groupTree'
import type {GroupSelection} from '../../types'

const props = defineProps<{
  node: GroupNode
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

const keyboard = inject<{tabStopPath: ComputedRef<string>; focus: (path: string) => void}>('ding-group-tree-keyboard')

const hasChildren = computed(() => props.node.children.length > 0)
const isOpen = computed(() => props.expanded.has(props.node.path))
const isActive = computed(() => props.activePath === props.node.path)
const isDropTarget = computed(() => props.dragOver === props.node.path)
const target = (): GroupSelection => ({kind: 'group', path: props.node.path})

function select() {
  emit('select', target())
}

function toggle(event?: MouseEvent) {
  event?.stopPropagation()
  if (hasChildren.value) emit('toggle', props.node.path)
}

function onKeydown(event: KeyboardEvent) {
  if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.altKey || event.ctrlKey || event.metaKey) return
  // Buttons inside a row keep their native Enter/Space actions; row shortcuts apply only to the row itself.
  if (event.target !== event.currentTarget) return
  const row = event.currentTarget as HTMLElement
  const tree = row.closest('[role="tree"]')
  const rows = Array.from(tree?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? []).filter(isMenuItemAvailable)
  const index = rows.indexOf(row)
  const focus = (element?: HTMLElement) => { element?.focus(); element?.scrollIntoView?.({block: 'nearest'}) }
  const key = event.key
  if (!['Enter', ' ', 'ArrowDown', 'ArrowUp', 'Home', 'End', 'ArrowRight', 'ArrowLeft', 'F2', 'Delete', 'ContextMenu'].includes(key)
    && !(key === 'F10' && event.shiftKey)) return
  event.preventDefault()
  event.stopPropagation()
  switch (key) {
    case 'Enter':
    case ' ':
      if (!event.repeat) select()
      break
    case 'ArrowDown':
      focus(rows[Math.min(rows.length - 1, index + 1)])
      break
    case 'ArrowUp':
      focus(rows[Math.max(0, index - 1)])
      break
    case 'Home':
      focus(rows[0])
      break
    case 'End':
      focus(rows[rows.length - 1])
      break
    case 'ArrowRight':
      if (!hasChildren.value) return
      if (!isOpen.value) toggle()
      else focus(rows.find(item => item.dataset.groupPath === props.node.children[0]?.path))
      break
    case 'ArrowLeft':
      if (hasChildren.value && isOpen.value) toggle()
      else focus(rows.find(item => item.dataset.groupPath === props.node.parent))
      break
    case 'F2':
      if (!event.repeat) emit('rename', props.node.path)
      break
    case 'Delete':
      if (!event.repeat) emit('remove', props.node.path)
      break
    case 'ContextMenu':
    case 'F10': {
      const rect = getDisplayRect(row)
      emit('contextMenu', new MouseEvent('contextmenu', {clientX: rect.left, clientY: rect.bottom}), props.node.path)
      break
    }
  }
}

function onMore(event: MouseEvent) {
  event.stopPropagation()
  emit('contextMenu', event, props.node.path)
}

function onManageCreate(event: MouseEvent) {
  event.stopPropagation()
  emit('createChild', props.node.path)
}

function onManageRename(event: MouseEvent) {
  event.stopPropagation()
  emit('rename', props.node.path)
}

function onManageRemove(event: MouseEvent) {
  event.stopPropagation()
  emit('remove', props.node.path)
}

function onContextMenu(event: MouseEvent) {
  event.preventDefault()
  event.stopPropagation()
  emit('contextMenu', event, props.node.path)
}
</script>

<template>
  <div class="group-tree-node">
    <div
      class="side-group-item group-tree-row"
      :class="[isActive ? 'active' : '', isDropTarget ? 'drop-target' : '', compact ? 'compact' : '']"
      :style="{'--group-level': node.depth - 1}"
      role="treeitem"
      :aria-level="node.depth"
      :aria-selected="isActive"
      :aria-expanded="hasChildren ? isOpen : undefined"
      :aria-label="$t('{0}，共 {1} 台服务器', [node.path, node.total])"
      :data-group-path="node.path"
      :title="$t('{0} · 本组 {1} 台 · 含子分组共 {2} 台', [node.path, node.direct, node.total])"
      :tabindex="!keyboard || keyboard.tabStopPath.value === node.path ? 0 : -1"
      @focus="keyboard?.focus(node.path)"
      @click="select"
      @keydown="onKeydown"
      @contextmenu="onContextMenu"
      @dragover="emit('dragOver', $event, target())"
      @dragleave="emit('dragLeave', target())"
      @drop="emit('drop', $event, target())"
    >
      <Icon :name="hasChildren && isOpen ? 'folder-open' : 'folder'" :size="14" extra-class="group-tree-folder" />
      <span class="group-tree-label">{{ node.name }}</span>
      <button
        v-if="hasChildren"
        type="button"
        class="group-tree-disclosure"
        :aria-label="$t(isOpen ? '收起分组' : '展开分组')"
        :aria-expanded="isOpen"
        :title="$t(isOpen ? '收起分组' : '展开分组')"
        tabindex="-1"
        @click="toggle($event)"
      >
        <Icon name="chevron-right" :size="13" :class="isOpen ? 'rotate-90' : ''" />
      </button>
      <span class="count group-tree-count">{{ node.total }}</span>
      <template v-if="manage">
        <span class="group-tree-actions">
          <button type="button" class="group-tree-action" :title="$t('新建子分组')" :aria-label="$t('新建子分组')" @click="onManageCreate">
            <Icon name="folder-plus" :size="13" />
          </button>
          <button type="button" class="group-tree-action" :title="$t('重命名')" :aria-label="$t('重命名')" @click="onManageRename">
            <Icon name="pencil" :size="13" />
          </button>
          <button type="button" class="group-tree-action danger" :title="$t('删除')" :aria-label="$t('删除')" @click="onManageRemove">
            <Icon name="trash" :size="13" />
          </button>
        </span>
      </template>
      <button v-else type="button" class="group-tree-more" :title="$t('分组操作')" :aria-label="$t('分组操作')" @click="onMore">
        <Icon name="ellipsis" :size="14" />
      </button>
    </div>

    <div
      v-if="hasChildren && isOpen"
      class="group-tree-children"
      :style="{'--group-level': node.depth - 1}"
      role="group"
    >
      <GroupTreeRow
        v-for="child in node.children"
        :key="child.path"
        :node="child"
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
  </div>
</template>

<style scoped>
.group-tree-row {
  position: relative;
  padding-left: calc(8px + var(--group-level, 0) * 12px);
  outline: none;
}
.group-tree-row.compact {
  height: 28px;
  font-size: 12px;
}
.group-tree-row:focus-visible {
  outline: 2px solid var(--focus-ring-color);
  outline-offset: -2px;
}
.group-tree-disclosure {
  width: 20px;
  height: 20px;
  display: grid;
  place-items: center;
  flex: none;
  border-radius: var(--radius-small);
  color: var(--muted-foreground);
  transition: color var(--dur), background var(--dur);
}
.group-tree-disclosure:hover {
  color: var(--foreground);
  background: var(--hover);
}
.group-tree-disclosure:focus-visible {
  outline: 2px solid var(--focus-ring-color);
  outline-offset: -2px;
}
.group-tree-disclosure :deep(svg) {
  transition: transform 120ms cubic-bezier(0.22, 1, 0.36, 1);
}
.group-tree-folder {
  color: var(--muted-foreground);
}
.group-tree-row.active .group-tree-folder {
  color: var(--accent-bg);
}
.group-tree-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.group-tree-count {
  flex: none;
}
.group-tree-more,
.group-tree-action {
  width: 20px;
  height: 20px;
  display: grid;
  place-items: center;
  flex: none;
  border-radius: var(--radius-small);
  color: var(--muted-foreground);
  opacity: 0;
  transition: opacity var(--dur), color var(--dur), background var(--dur);
}
.group-tree-row:hover .group-tree-more,
.group-tree-row:focus-within .group-tree-more,
.group-tree-row:hover .group-tree-action,
.group-tree-row:focus-within .group-tree-action {
  opacity: 1;
}
.group-tree-more:hover,
.group-tree-action:hover {
  color: var(--foreground);
  background: var(--hover-strong);
}
.group-tree-action.danger:hover {
  color: var(--destructive);
}
.group-tree-actions {
  display: inline-flex;
  align-items: center;
  gap: 1px;
  flex: none;
}
.group-tree-row:hover .group-tree-count:has(~ .group-tree-more),
.group-tree-row:focus-within .group-tree-count:has(~ .group-tree-more) {
  display: none;
}
.group-tree-children {
  position: relative;
}
.group-tree-children::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 12px;
  left: calc(15px + var(--group-level, 0) * 12px);
  width: 1px;
  pointer-events: none;
  background: color-mix(in srgb, var(--muted-foreground) 26%, transparent);
  mask-image: linear-gradient(to bottom, #000 0, #000 calc(100% - 14px), transparent calc(100% - 2px));
  -webkit-mask-image: linear-gradient(to bottom, #000 0, #000 calc(100% - 14px), transparent calc(100% - 2px));
}
@media (prefers-reduced-motion: reduce) {
  .group-tree-disclosure :deep(svg) {
    transition: none;
  }
}
</style>
