<script setup lang="ts">
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {nextTick, onMounted} from 'vue'
import {useMenuPlacement} from '../../composables/useMenuPlacement'

const props = defineProps<{ position: {x: number; y: number}; logTracking: boolean }>()
const emit = defineEmits<{ copy: []; paste: []; selectAll: []; clear: []; font: [delta: number]; fullscreen: []; logTrace: []; close: [] }>()
// 打开后按真实高度定位，贴底/贴右时自动上移或翻转，避免被窗口裁掉
const {menuEl, menuStyle, place} = useMenuPlacement()

function items(): HTMLElement[] {
  return Array.from(menuEl.value?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])') ?? [])
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
  void place(props.position.x, props.position.y)
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="menuEl"
      class="menu-pop neo fixed z-50"
      role="menu"
      :aria-label="$t('终端操作')"
      data-ui="context-menu"
      data-testid="terminal-context-menu"
      :style="menuStyle"
      @contextmenu.prevent
      @click.stop
      @keydown="onKeydown"
    >
      <button role="menuitem" data-testid="terminal-copy" @click="emit('copy')">{{ $t('复制') }} <span class="text-mist">Ctrl+Shift+C</span></button>
      <button role="menuitem" data-testid="terminal-paste" @click="emit('paste')">{{ $t('粘贴') }} <span class="text-mist">Ctrl+V</span></button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" @click="emit('selectAll')">{{ $t('全选') }}</button><button role="menuitem" @click="emit('clear')">{{ $t('清除屏幕') }}</button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" @click="emit('font', 1)">{{ $t('放大') }} <span class="text-mist">Ctrl+=</span></button><button role="menuitem" @click="emit('font', -1)">{{ $t('缩小') }} <span class="text-mist">Ctrl+-</span></button><button role="menuitem" @click="emit('fullscreen')">{{ $t('全屏') }} <span class="text-mist">F11</span></button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" @click="emit('logTrace')">{{ logTracking ? t('停止跟踪此会话日志') : t('开始跟踪此会话日志') }}</button>
    </div>
  </Teleport>
</template>
