<script lang="ts" setup>
import {onBeforeUnmount, ref} from 'vue'
import {unzoom} from '../utils/dom'

const props = defineProps<{
  /** 面板在分隔条哪一侧：left = 面板在左（拖它右边框，如左侧导航）；right = 面板在右（拖它左边框，如右侧工具面板）。 */
  side: 'left' | 'right'
  /** 当前宽度（布局像素）。 */
  width: number
  /** 拖动范围（布局像素）；max 由父级按窗口可用空间算好。 */
  min: number
  max: number
  /** 分隔条在父容器里的定位值（CSS 长度，一般是 calc(宽度px - 4px)，见组件样式注释）。 */
  inset: string
  /** 无障碍名称，如「导航栏」。 */
  label: string
}>()

const emit = defineEmits<{
  (e: 'resize', width: number): void
  (e: 'reset'): void
  (e: 'drag', active: boolean): void
}>()

const handleEl = ref<HTMLElement | null>(null)
const dragging = ref(false)
let startX = 0
let startWidth = 0

function clampWidth(v: number): number {
  const lo = Math.min(props.min, props.max)
  const hi = Math.max(props.min, props.max)
  return Math.round(Math.min(Math.max(v, lo), hi))
}

function onMousedown(e: MouseEvent) {
  if (e.button !== 0) return
  // 阻止默认行为：避免拖动时在终端 / 列表上拉出文本选区
  e.preventDefault()
  dragging.value = true
  startX = e.clientX
  startWidth = props.width
  // 拖动期间全局保持 col-resize：xterm 会自带 cursor，光标划过终端时会变回文本光标
  document.body.classList.add('col-resizing')
  emit('drag', true)
  window.addEventListener('mousemove', onMousemove)
  window.addEventListener('mouseup', onMouseup)
}

function onMousemove(e: MouseEvent) {
  if (!dragging.value) return
  // clientX 处于渲染坐标系（含 .app-shell 的 zoom），而宽度是布局像素：
  // 先按缩放比折算回布局像素再累加，否则 uiScale≠100% 时手感会成倍漂移。
  const dx = unzoom(e.clientX - startX, handleEl.value)
  // left：面板在左，向右拖变宽；right：面板在右，向左拖变宽
  const next = clampWidth(startWidth + (props.side === 'left' ? dx : -dx))
  if (next !== props.width) emit('resize', next)
}

function onMouseup() {
  if (!dragging.value) return
  dragging.value = false
  document.body.classList.remove('col-resizing')
  emit('drag', false)
  window.removeEventListener('mousemove', onMousemove)
  window.removeEventListener('mouseup', onMouseup)
}

onBeforeUnmount(() => {
  if (dragging.value) {
    document.body.classList.remove('col-resizing')
    emit('drag', false)
  }
  window.removeEventListener('mousemove', onMousemove)
  window.removeEventListener('mouseup', onMouseup)
})
</script>

<template>
  <div
    ref="handleEl"
    class="width-resizer"
    :class="side"
    :data-dragging="dragging"
    :style="side === 'left' ? {left: inset} : {right: inset}"
    role="separator"
    aria-orientation="vertical"
    :aria-label="label + '宽度'"
    :title="'拖动调整' + label + '宽度 · 双击复位'"
    @mousedown="onMousedown"
    @dblclick="emit('reset')"
  >
    <span class="width-resizer-line"></span>
  </div>
</template>

<style scoped>
.width-resizer {
  position: absolute;
  top: 0;
  bottom: 0;
  /* 7px 命中区 + 居中 1px 细线：配合父级 inset = calc(宽度px - 4px)，
     细线正好落在面板自己的 1px 边框那一条像素上（不hover时无痕） */
  width: 7px;
  /* 高于左侧导航（.nav z-index 60），避免分隔条被导航的右边界盖住一半 */
  z-index: 61;
  display: flex;
  cursor: col-resize;
  touch-action: none;
}
.width-resizer-line {
  width: 1px;
  height: 100%;
  margin: 0 auto;
  /* 静止时不画线：面板自身已有 1px 边框，hover 时把这 1px 染成强调色即可 */
  background: transparent;
  transition: background var(--dur);
}
.width-resizer:hover .width-resizer-line,
.width-resizer[data-dragging='true'] .width-resizer-line {
  background: var(--signal-400, #3ec4b4);
}
</style>
