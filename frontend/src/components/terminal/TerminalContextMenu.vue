<script setup lang="ts">
import {nextTick, onMounted, ref} from 'vue'

defineProps<{ position: {x: number; y: number}; logTracking: boolean }>()
const emit = defineEmits<{ copy: []; paste: []; selectAll: []; clear: []; font: [delta: number]; fullscreen: []; logTrace: []; close: [] }>()
const root = ref<HTMLElement | null>(null)

function items(): HTMLElement[] {
  return Array.from(root.value?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])') ?? [])
}

function focusAt(index: number) {
  const list = items()
  if (!list.length) return
  const next = (index + list.length) % list.length
  list[next].focus()
}

function onKeydown(event: KeyboardEvent) {
  const list = items()
  const current = list.indexOf(document.activeElement as HTMLElement)
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
  } else if (event.key === 'ArrowDown') {
    event.preventDefault()
    focusAt(current + 1)
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    focusAt(current - 1)
  } else if (event.key === 'Home') {
    event.preventDefault()
    focusAt(0)
  } else if (event.key === 'End') {
    event.preventDefault()
    focusAt(list.length - 1)
  }
}

onMounted(async () => {
  await nextTick()
  focusAt(0)
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="root"
      class="menu-pop neo fixed z-50"
      role="menu"
      aria-label="终端操作"
      data-ui="context-menu"
      data-testid="terminal-context-menu"
      :style="{left: position.x + 'px', top: position.y + 'px'}"
      @contextmenu.prevent
      @click.stop
      @keydown="onKeydown"
    >
      <button role="menuitem" data-testid="terminal-copy" @click="emit('copy')">复制 <span class="text-mist">Ctrl+Shift+C</span></button>
      <button role="menuitem" data-testid="terminal-paste" @click="emit('paste')">粘贴 <span class="text-mist">Ctrl+V</span></button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" @click="emit('selectAll')">全选</button><button role="menuitem" @click="emit('clear')">清除屏幕</button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" @click="emit('font', 1)">放大 <span class="text-mist">Ctrl+=</span></button><button role="menuitem" @click="emit('font', -1)">缩小 <span class="text-mist">Ctrl+-</span></button><button role="menuitem" @click="emit('fullscreen')">全屏 <span class="text-mist">F11</span></button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" @click="emit('logTrace')">{{ logTracking ? '停止跟踪此会话日志' : '开始跟踪此会话日志' }}</button>
    </div>
  </Teleport>
</template>
