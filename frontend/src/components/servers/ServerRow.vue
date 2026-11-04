<script setup lang="ts">
import Icon from '../Icon.vue'
import type {ServerNode} from '../../types'
type Badge = {dot: string; text: string; textClass: string; tip?: string} | null
defineProps<{node: ServerNode; status: string; active: boolean; dragging: boolean; badge: Badge; copied: boolean}>()
const emit = defineEmits<{connect: []; copy: []; test: []; edit: []; remove: []; menu: [event: MouseEvent]; dragstart: [event: DragEvent]; dragend: []}>()

/** 键盘打开右键菜单（Shift+F10 / ContextMenu），坐标取整行的中心。 */
function onMainKeydown(event: KeyboardEvent) {
  if (!(event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10'))) return
  event.preventDefault()
  const row = (event.currentTarget as HTMLElement | null)?.closest('.server-row') as HTMLElement | null
  const rect = row?.getBoundingClientRect()
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
      :title="`连接 ${node.name}（右键更多操作，可拖到左侧分组改分组）`"
      :aria-label="`连接 ${node.name}，${node.user}@${node.host}:${node.port}`"
      @click="emit('connect')"
      @keydown="onMainKeydown"
    >
      <span class="status shrink-0" :class="status" aria-hidden="true"></span>
      <span class="min-w-0 flex-1">
        <span class="server-name">{{ node.name }}</span>
        <span class="server-host font-mono">{{ node.user }}@{{ node.host }}:{{ node.port }}</span>
      </span>
    </button>
    <button
      v-if="badge"
      type="button"
      class="latency shrink-0"
      :title="(badge.tip || '') + '（点击重新测试）'"
      :aria-label="badge.tip ? `${badge.text}，${badge.tip}，点击重新测试` : `${badge.text}，点击重新测试`"
      @click="emit('test')"
    >
      <span class="w-1.5 h-1.5 rounded-full" :class="badge.dot" aria-hidden="true"></span>
      <span class="text-[12px] leading-none" :class="badge.textClass">{{ badge.text }}</span>
    </button>
    <span v-else class="latency shrink-0 text-mist">未测试</span>
    <div class="row-actions shrink-0">
      <button class="row-act" type="button" title="连接" aria-label="连接" :data-testid="`server-connect-${node.id}`" @click="emit('connect')"><Icon name="terminal" :size="14" /><span>连接</span></button>
      <button class="row-act" type="button" :class="copied ? 'text-[var(--success)]' : ''" :title="copied ? '已复制主机地址' : `复制主机地址 ${node.host}`" aria-label="复制主机地址" @click="emit('copy')"><Icon :name="copied ? 'check' : 'copy'" :size="14" /><span>{{ copied ? '已复制' : '复制' }}</span></button>
      <button class="row-act" type="button" title="测试在线状态" aria-label="测试在线状态" @click="emit('test')"><Icon name="activity" :size="14" /><span>测试</span></button>
      <button class="row-act" type="button" title="编辑" aria-label="编辑" @click="emit('edit')"><Icon name="pencil" :size="14" /><span>编辑</span></button>
      <button class="row-act row-act-danger" type="button" title="删除" aria-label="删除" :data-testid="`server-delete-${node.id}`" @click="emit('remove')"><Icon name="trash" :size="14" /><span>删除</span></button>
    </div>
  </div>
</template>

<style scoped>
.server-row-main {
  min-width: 0;
  flex: 1;
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
