<script setup lang="ts">
import {ref} from 'vue'
import type {Suggestion} from '../../completion/engine'

defineProps<{items: Suggestion[]; position: {left: number; top: number}; navigating: boolean; selectedIndex: number; hotkeyLabel: string; sourceLabel: Record<string, string>}>()
const emit = defineEmits<{ hover: [index: number]; accept: [index: number] }>()
const root = ref<HTMLElement | null>(null)
defineExpose({get offsetHeight() { return root.value?.offsetHeight ?? 0 }})
</script>

<template>
  <Teleport to="body">
    <div ref="root" class="fixed z-[60] min-w-[240px] max-w-[360px] neo p-1.5 text-xs completion-panel" data-testid="terminal-completion-panel" :style="{left: position.left + 'px', top: position.top + 'px'}" @mousedown.prevent>
      <button v-for="(item, i) in items" :key="item.source + ':' + item.command" type="button" class="w-full flex items-center gap-2 px-2.5 py-2 rounded-[6px] text-left font-mono" :class="navigating && i === selectedIndex ? 'bg-[var(--primary-weak)] shadow-[inset_0_0_0_1px_var(--primary-border)]' : 'hover:bg-[var(--hover)]'" @mouseenter="emit('hover', i)" @click="emit('accept', i)">
        <span class="flex-1 truncate">{{ item.command }}</span><span class="shrink-0 text-[12px] text-mist font-sans">{{ sourceLabel[item.source] || item.source }}<template v-if="item.count && item.count > 1"> · {{ item.count }}</template></span>
      </button>
      <div class="flex gap-3 px-2.5 pt-1.5 mt-1 text-[11px] text-mist inset-line-t"><span v-if="navigating"><span class="kbd">↑↓</span> 选择 · <span class="kbd">Tab</span> 采纳 · <span class="kbd">Esc</span> 关闭</span><span v-else><span class="kbd">{{ hotkeyLabel }}</span> 进入 · <span class="kbd">Esc</span> 关闭</span></div>
    </div>
  </Teleport>
</template>
