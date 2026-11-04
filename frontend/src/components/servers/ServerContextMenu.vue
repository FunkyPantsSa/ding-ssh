<script setup lang="ts">
import {nextTick, onMounted, ref} from 'vue'
import Icon from '../Icon.vue'
import type {ServerNode} from '../../types'

defineProps<{node: ServerNode; position: {x: number; y: number}; copied: boolean; cloning: boolean}>()
const emit = defineEmits<{edit: []; copy: []; duplicate: []; test: []; remove: []; close: []}>()
const root = ref<HTMLElement | null>(null)

function items(): HTMLElement[] {
  return Array.from(root.value?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])') ?? [])
}

function focusAt(index: number) {
  const list = items()
  if (!list.length) return
  list[(index + list.length) % list.length].focus()
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
      class="menu-pop neo fixed"
      role="menu"
      aria-label="服务器操作"
      data-ui="context-menu"
      data-testid="server-context-menu"
      :style="{left: position.x + 'px', top: position.y + 'px'}"
      @contextmenu.prevent
      @click.stop
      @keydown="onKeydown"
    >
      <div class="px-2.5 pt-1.5 pb-1 text-[11px] text-mist truncate" role="presentation">{{ node.name }}</div>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('edit')"><Icon name="pencil" :size="13" />编辑</button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('copy')"><Icon :name="copied ? 'check' : 'clipboard-copy'" :size="13" />{{ copied ? '已复制IP' : '复制IP' }}</button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" :disabled="cloning" @click="emit('duplicate')"><Icon name="copy" :size="13" />复制配置（生成 .new）</button>
      <button role="menuitem" class="!flex items-center gap-1.5" type="button" @click="emit('test')"><Icon name="activity" :size="13" />测试在线状态</button>
      <div class="divider-h my-1" role="separator"></div>
      <button role="menuitem" class="danger !flex items-center gap-1.5" type="button" @click="emit('remove')"><Icon name="trash" :size="13" />删除</button>
    </div>
  </Teleport>
</template>
