<script setup lang="ts">
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import ServerConfigSummary from './ServerConfigSummary.vue'
import Icon from '../Icon.vue'
import {getDisplayRect} from '../../utils/dom'
import type {ServerNode} from '../../types'
type Badge = {dot: string; text: string; textClass: string; tip?: string} | null
defineProps<{node: ServerNode; status: string; active: boolean; dragging: boolean; badge: Badge; copied: boolean; groupLabel?: string}>()
const emit = defineEmits<{connect: []; copy: []; test: []; edit: []; remove: []; menu: [event: MouseEvent]; dragstart: [event: DragEvent]; dragend: []}>()

/** 键盘打开右键菜单（Shift+F10 / ContextMenu），坐标取整行的中心。 */
function onMainKeydown(event: KeyboardEvent) {
  if (!(event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10'))) return
  event.preventDefault()
  const row = (event.currentTarget as HTMLElement | null)?.closest('.server-row') as HTMLElement | null
  // 合成事件的 clientX/Y 必须是显示坐标（菜单是 fixed 弹层），gBCR 在 WKWebView 下是布局坐标
  const rect = getDisplayRect(row)
  emit('menu', new MouseEvent('contextmenu', {
    bubbles: true,
    clientX: rect ? rect.left + Math.min(rect.width / 2, 160) : 0,
    clientY: rect ? rect.top + rect.height / 2 : 0,
  }))
}
</script>

<template>
  <div
    class="server-row group"
    :data-testid="`server-row-${node.id}`"
    :data-state="status"
    :class="[active ? 'active' : '', dragging ? 'dragging' : '']"
    draggable="true"
    @dragstart="emit('dragstart', $event)"
    @dragend="emit('dragend')"
    @contextmenu.prevent="emit('menu', $event)"
  >
    <button
      type="button"
      class="server-row-main"
      :title="t('连接 {0}（右键更多操作，可拖到左侧分组改分组）', [node.name])"
      :aria-label="t('连接 {0}，{1}@{2}:{3}', [node.name, node.user, node.host, node.port])"
      @click="emit('connect')"
      @keydown="onMainKeydown"
    >
      <span class="status shrink-0" :class="status" aria-hidden="true"></span>
      <span class="min-w-0 flex-1">
        <span class="server-name">{{ node.name }}</span>
        <span class="server-host font-mono">{{ node.user }}@{{ node.host }}:{{ node.port }}</span>
      </span>
    </button>
    <div class="server-row-meta">
      <span v-if="groupLabel" class="row-group-tag" :title="groupLabel">
        <Icon name="folder" :size="11" />
        <span class="row-group-label">{{ groupLabel }}</span>
      </span>
      <ServerConfigSummary :node="node" :status="status" />
    </div>
    <div class="server-row-operations">
    <button
      v-if="badge"
      type="button"
      class="latency shrink-0"
      :title="(badge.tip || '') + t('（点击重新测试）')"
      :aria-label="badge.tip ? t('{0}，{1}，点击重新测试', [badge.text, badge.tip]) : t('{0}，点击重新测试', [badge.text])"
      @click="emit('test')"
    >
      <span class="w-1.5 h-1.5 rounded-full" :class="badge.dot" aria-hidden="true"></span>
      <span class="text-[12px] leading-none" :class="badge.textClass">{{ badge.text }}</span>
    </button>
    <span v-else class="latency shrink-0 text-mist">{{ $t('未测试') }}</span>
    <div class="row-actions shrink-0">
      <button class="row-act" type="button" :title="$t('连接')" :aria-label="$t('连接')" :data-testid="`server-connect-${node.id}`" @click="emit('connect')"><Icon name="terminal" :size="14" /><span>{{ $t('连接') }}</span></button>
      <button class="row-act" type="button" :class="copied ? 'text-[var(--success)]' : ''" :title="copied ? t('已复制主机地址') : t('复制主机地址 {0}', [node.host])" :aria-label="$t('复制主机地址')" @click="emit('copy')"><Icon :name="copied ? 'check' : 'copy'" :size="14" /><span>{{ copied ? t('已复制') : t('复制') }}</span></button>
      <button class="row-act" type="button" :title="$t('测试在线状态')" :aria-label="$t('测试在线状态')" @click="emit('test')"><Icon name="activity" :size="14" /><span>{{ $t('测试') }}</span></button>
      <button class="row-act" type="button" :title="$t('编辑')" :aria-label="$t('编辑')" @click="emit('edit')"><Icon name="pencil" :size="14" /><span>{{ $t('编辑') }}</span></button>
      <button class="row-act row-act-danger" type="button" :title="$t('删除')" :aria-label="$t('删除')" :data-testid="`server-delete-${node.id}`" @click="emit('remove')"><Icon name="trash" :size="14" /><span>{{ $t('删除') }}</span></button>
    </div>
    </div>
  </div>
</template>

<style scoped>
.server-row {
  container-type: inline-size;
  display: grid;
  grid-template-columns: minmax(180px, .85fr) minmax(0, 1.4fr) max-content;
}
.server-row-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.row-group-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 190px;
  height: 20px;
  padding: 0 6px;
  flex: none;
  border-radius: var(--radius-small);
  background: var(--accent-weak-bg);
  color: var(--accent-bg);
  box-shadow: inset 0 0 0 1px var(--accent-border);
  font-size: 10.5px;
  font-weight: 500;
  white-space: nowrap;
}
.row-group-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
@container server-list (max-width: 760px) {
  .row-group-tag { max-width: 120px; }
}
@container server-list (max-width: 600px) {
  .row-group-tag { display: none; }
}
.server-row-operations {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  min-width: 0;
}
.server-row :deep(.summary) { margin: 0; flex-wrap: wrap; row-gap: 4px; }
.server-row :deep(.summary > .tag) { min-width: 0; }
@container server-list (max-width: 1050px) {
  .server-row { grid-template-columns: minmax(160px, 1fr) minmax(0, 1fr) max-content; }
  .server-row-operations { gap: 6px; }
  .row-act { padding-inline: 6px; }
  .row-act span { display: none; }
}
@container server-list (max-width: 650px) {
  .server-row { grid-template-columns: minmax(0, 1fr) max-content; row-gap: 6px; }
  .server-row-main { grid-column: 1; grid-row: 1; }
  .server-row :deep(.summary) { grid-column: 1; grid-row: 2; }
  .server-row-operations { grid-column: 2; grid-row: 1 / 3; flex-direction: column; align-items: flex-end; gap: 4px; }
}
@container server-list (max-width: 380px) {
  .server-row { grid-template-columns: minmax(0, 1fr); }
  .server-row-operations { grid-column: 1; grid-row: 3; flex-direction: row; justify-content: flex-end; flex-wrap: wrap; }
}
.server-row-main {
  min-width: 0;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0;
  color: inherit;
  text-align: left;
  background: transparent;
  border: 0;
}
.server-row-main:focus-visible {
  outline: 2px solid var(--border-focus);
  outline-offset: 2px;
  border-radius: var(--radius-sm);
}
.server-name {
  display: block;
  overflow: hidden;
  color: var(--foreground);
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.server-host {
  display: block;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.latency {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 4px;
  color: var(--muted-foreground);
  font-family: var(--font-mono);
  font-size: 11.5px;
  background: transparent;
  border: 0;
  border-radius: var(--radius-sm);
}
button.latency:hover { background: var(--hover); }
</style>
