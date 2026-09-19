<script lang="ts" setup>
import {computed} from 'vue'
import SplitDivider from './SplitDivider.vue'
import {useSessionsStore} from '../stores/sessions'
import type {PaneLayout} from '../panes/layout'
import type {PaneRect, SessionTab} from '../types'

defineOptions({name: 'SplitView'})

const props = defineProps<{layout: PaneLayout}>()
const sessions = useSessionsStore()

// 叶子 rect 列表（供模板遍历）：依赖 props.layout，随 App 重算更新
const leafList = computed(() => Array.from(props.layout.leafRects.entries()).map(([paneId, rect]) => ({paneId, rect})))

function rectStyle(rect: PaneRect | undefined) {
  if (!rect) return {display: 'none'}
  return {
    left: rect.left + 'px',
    top: rect.top + 'px',
    width: rect.width + 'px',
    height: rect.height + 'px',
  }
}

function statusDot(status: string): string {
  const map: Record<string, string> = {
    connecting: 'bg-[var(--warn-500)]',
    connected: 'bg-[var(--signal-400)] shadow-[0_0_8px_var(--signal-glow)]',
    closed: 'bg-[var(--mist-400)]',
    error: 'bg-[var(--danger-500)]',
  }
  return map[status] ?? 'bg-[var(--mist-400)]'
}

/** 叶子当前绑定的标签（模板中按 paneId 取）。 */
function paneTab(paneId: string): SessionTab | undefined {
  const leaf = sessions.findPane(paneId)
  const id = leaf?.paneActiveId
  return id ? sessions.tabs.find((t) => t.clientId === id) : undefined
}

/** 拖拽格头标题到终端区域：改分屏方向（左右 ↔ 上下）。 */
function onHeaderDragStart(e: DragEvent) {
  sessions.draggingSplitGroup = true
  sessions.draggingTabId = ''
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', '__split-group__')
  }
}
</script>

<template>
  <div class="split-root">
    <!-- 叶子格子：纯标题栏格头（可拖拽改方向）+ 分隔条 -->
    <template v-for="leafItem in leafList" :key="leafItem.paneId">
      <div
        class="split-leaf"
        :class="sessions.focusedPaneId === leafItem.paneId ? 'focused' : ''"
        :style="rectStyle(leafItem.rect)"
      >
        <div
          class="split-pane-header"
          draggable="true"
          @click="sessions.setFocusedPane(leafItem.paneId)"
          @dragstart="onHeaderDragStart"
          @dragend="sessions.draggingSplitGroup = false"
        >
          <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="statusDot(paneTab(leafItem.paneId)?.status ?? 'closed')"></span>
          <span class="truncate" :title="paneTab(leafItem.paneId)?.serverName">{{ paneTab(leafItem.paneId)?.serverName ?? '（空）' }}</span>
        </div>
      </div>
    </template>

    <!-- 分隔条 -->
    <template v-for="d in layout.dividers" :key="d.key">
      <div
        class="split-divider"
        :class="d.direction === 'row' ? 'v' : 'h'"
        :style="{left: d.x + 'px', top: d.y + 'px', width: d.w + 'px', height: d.h + 'px'}"
      >
        <SplitDivider
          :direction="d.direction"
          :parent-id="d.parentId"
          :index="d.index"
          :axis-total="d.axisTotal"
        />
      </div>
    </template>
  </div>
</template>

<style scoped>
.split-root {
  position: absolute;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
}
.split-leaf {
  position: absolute;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-radius: 6px;
  pointer-events: none;
}
.split-leaf.focused {
  box-shadow: inset 0 0 0 1px var(--signal-border, rgba(62, 196, 180, 0.4));
}
.split-pane-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  background: rgba(0, 0, 0, 0.45);
  border-bottom: 1px solid rgba(255, 255, 255, 0.07);
  flex-shrink: 0;
  height: 28px;
  border-radius: 6px 6px 0 0;
  pointer-events: auto;
  font-size: 11px;
  color: var(--mist-200);
  cursor: grab;
  user-select: none;
}
.split-pane-header:hover { background: rgba(255, 255, 255, 0.06); }
.split-divider {
  position: absolute;
  z-index: 5;
  pointer-events: auto;
}
</style>
