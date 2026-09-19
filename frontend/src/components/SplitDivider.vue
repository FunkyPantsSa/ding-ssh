<script lang="ts" setup>
import {onBeforeUnmount, ref} from 'vue'
import {useSessionsStore} from '../stores/sessions'
import {currentZoom} from '../utils/dom'
import type {PaneDirection} from '../types'

const props = defineProps<{
  direction: PaneDirection
  parentId: string
  index: number
  axisTotal: number // 父方向总长度（像素），用于把位移换算为比例
}>()

const sessions = useSessionsStore()
const dragging = ref(false)
const dividerEl = ref<HTMLElement | null>(null)

let startX = 0
let startY = 0
let startSize = 0

function onMousedown(e: MouseEvent) {
  if (e.button !== 0) return
  e.preventDefault()
  e.stopPropagation()
  dragging.value = true
  startX = e.clientX
  startY = e.clientY
  const parent = sessions.findPane(props.parentId)
  const prev = parent?.children[props.index - 1]
  startSize = prev?.size ?? 0.5

  window.addEventListener('mousemove', onMousemove)
  window.addEventListener('mouseup', onMouseup)
}

function onMousemove(e: MouseEvent) {
  if (!dragging.value) return
  const total = props.axisTotal
  if (total <= 0) return
  // e.clientX/Y 相对 viewport（渲染坐标系，含 app-shell 的 zoom）；axisTotal 是布局像素。
  // 先把鼠标增量折算回布局像素，比例才能与叶子矩形/分隔条定位一致（否则 uiScale≠100 时拖拽比例偏大）。
  const z = currentZoom(dividerEl.value)
  const rawDelta = (props.direction === 'row' ? e.clientX - startX : e.clientY - startY) / z
  // 降低灵敏度：加 2px 死区并按 0.6 阻尼，避免小幅度移动导致比例跳变。
  const deadzone = 2
  const delta = Math.abs(rawDelta) <= deadzone ? 0 : rawDelta - Math.sign(rawDelta) * deadzone
  // delta 为正 → 分隔条向右/下移 → 前一个子节点增大
  sessions.resizePane(props.parentId, props.index, (delta * 0.6) / total)
}

function onMouseup() {
  dragging.value = false
  window.removeEventListener('mousemove', onMousemove)
  window.removeEventListener('mouseup', onMouseup)
}

function onDblclick() {
  const parent = sessions.findPane(props.parentId)
  if (!parent || parent.children.length !== 2) return
  parent.children[0].size = 0.5
  parent.children[1].size = 0.5
}

onBeforeUnmount(() => {
  window.removeEventListener('mousemove', onMousemove)
  window.removeEventListener('mouseup', onMouseup)
})
</script>

<template>
  <div
    ref="dividerEl"
    class="split-divider-inner"
    :class="direction === 'row' ? 'v' : 'h'"
    :data-dragging="dragging"
    @mousedown="onMousedown"
    @dblclick="onDblclick"
  >
    <div class="split-divider-hit"></div>
  </div>
</template>

<style scoped>
.split-divider-inner {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--ink-900);
}
.split-divider-inner.v { cursor: col-resize; }
.split-divider-inner.h { cursor: row-resize; }
.split-divider-hit {
  background: var(--line-strong);
}
.v .split-divider-hit { width: 1px; height: 100%; }
.h .split-divider-hit { height: 1px; width: 100%; }
.split-divider-inner:hover .split-divider-hit,
.split-divider-inner[data-dragging='true'] .split-divider-hit {
  background: var(--signal-400, #3ec4b4);
}
</style>
