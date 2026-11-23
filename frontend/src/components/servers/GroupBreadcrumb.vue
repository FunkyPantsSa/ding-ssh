<script setup lang="ts">
import {computed} from 'vue'
import {t} from '../../i18n'
import {splitGroupPath} from '../../utils/groupTree'
import type {GroupSelection} from '../../types'

const props = defineProps<{
  selection: GroupSelection
  total: number
  direct: number
  hasChildren: boolean
}>()

const emit = defineEmits<{select: [selection: GroupSelection]}>()

interface Crumb {
  label: string
  selection: GroupSelection
  current: boolean
}

const crumbs = computed<Crumb[]>(() => {
  const root: Crumb = {label: t('全部服务器'), selection: {kind: 'all'}, current: props.selection.kind === 'all'}
  if (props.selection.kind === 'all') return [root]
  if (props.selection.kind === 'ungrouped') {
    return [root, {label: t('未分组'), selection: {kind: 'ungrouped'}, current: true}]
  }
  const parts = splitGroupPath(props.selection.path)
  return [
    root,
    ...parts.map((label, index) => ({
      label,
      selection: {kind: 'group', path: parts.slice(0, index + 1).join('/')} as GroupSelection,
      current: index === parts.length - 1,
    })),
  ]
})
</script>

<template>
  <nav class="group-breadcrumb" :aria-label="t('分组路径')">
    <template v-for="(crumb, index) in crumbs" :key="`${crumb.selection.kind}-${index}`">
      <span v-if="index" class="sep" aria-hidden="true">/</span>
      <button
        v-if="!crumb.current"
        type="button"
        class="crumb"
        @click="emit('select', crumb.selection)"
      >{{ crumb.label }}</button>
      <span v-else class="crumb current" aria-current="page">{{ crumb.label }}</span>
    </template>
    <span v-if="selection.kind === 'group'" class="chip" :title="t('本组 {0} 台 · 含子分组共 {1} 台', [String(direct), String(total)])">
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/></svg>
      <span v-if="hasChildren">{{ t('含子分组 · {0} 台', [String(total)]) }}</span>
      <span v-else>{{ t('{0} 台', [String(total)]) }}</span>
    </span>
    <span class="spacer"></span>
    <slot name="meta"></slot>
  </nav>
</template>

<style scoped>
.group-breadcrumb {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  font-size: 12px;
  color: var(--muted-foreground);
}
.sep {
  opacity: 0.55;
}
.crumb {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--foreground-secondary);
  border-radius: var(--radius-small);
  padding: 2px 3px;
  margin: 0 -3px;
  transition: color var(--dur), background var(--dur);
}
button.crumb:hover {
  color: var(--foreground);
  background: var(--interactive-hover-bg);
}
.crumb.current {
  color: var(--foreground);
  font-weight: 500;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 21px;
  max-width: 220px;
  padding: 0 7px;
  margin-left: 4px;
  border-radius: var(--radius-small);
  background: var(--accent-weak-bg);
  color: var(--accent-bg);
  box-shadow: inset 0 0 0 1px var(--accent-border);
  font-size: 10.5px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.spacer {
  flex: 1;
}
@media (max-width: 900px) {
  .crumb {
    max-width: 110px;
  }
}
</style>
