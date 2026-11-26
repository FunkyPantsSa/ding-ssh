<script lang="ts" setup>
import {computed, onBeforeUnmount, ref} from 'vue'
import {t} from '../i18n'
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
const currentSize = computed(() => sessions.findPane(props.parentId)?.children[props.index - 1]?.size ?? 0.5)

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

function onKeydown(event: KeyboardEvent) {
  if (event.altKey || event.ctrlKey || event.metaKey || dragging.value) return
  if (event.key === 'Enter') {
    event.preventDefault()
    event.stopPropagation()
    onDblclick()
    return
  }
  const backward = props.direction === 'row' ? 'ArrowLeft' : 'ArrowUp'
  const forward = props.direction === 'row' ? 'ArrowRight' : 'ArrowDown'
  const step = event.shiftKey ? 0.1 : 0.02
  let delta: number
  if (event.key === backward) delta = -step
  else if (event.key === forward) delta = step
  else if (event.key === 'Home') delta = 0.1 - currentSize.value
  else if (event.key === 'End') delta = 0.9 - currentSize.value
  else return
  event.preventDefault()
  event.stopPropagation()
  sessions.resizePane(props.parentId, props.index, delta)
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
    role="separator"
    tabindex="0"
    :aria-orientation="direction === 'row' ? 'vertical' : 'horizontal'"
    :aria-label="t('调整分屏比例')"
    :aria-valuemin="10"
    :aria-valuemax="90"
    :aria-valuenow="Math.round(currentSize * 100)"
    :aria-valuetext="Math.round(currentSize * 100) + '%'"
    :aria-keyshortcuts="(direction === 'row' ? 'ArrowLeft ArrowRight' : 'ArrowUp ArrowDown') + ' Home End Enter'"
    @mousedown="onMousedown"
    @dblclick="onDblclick"
    @keydown="onKeydown"
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
  background: var(--surface-1);
}
.split-divider-inner.v { cursor: col-resize; }
.split-divider-inner.h { cursor: row-resize; }
.split-divider-hit {
  background: var(--line-strong);
}
.v .split-divider-hit { width: 1px; height: 100%; }
.h .split-divider-hit { height: 1px; width: 100%; }
.split-divider-inner:hover .split-divider-hit,
.split-divider-inner:focus-visible .split-divider-hit,
.split-divider-inner[data-dragging='true'] .split-divider-hit {
  background: var(--primary-hover);
}
</style>
