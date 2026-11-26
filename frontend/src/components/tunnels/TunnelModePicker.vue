<script setup lang="ts">
import {t as $t} from '../../i18n'

import Icon from '@/components/Icon.vue'

export type TunnelMode = 'local' | 'remote' | 'dynamic' | 'reverse-dynamic'
export type TunnelModeDetail = {title: string; summary: string; description: string; example: string}

defineProps<{
  modelValue: TunnelMode
  details: Record<TunnelMode, TunnelModeDetail>
}>()

const emit = defineEmits<{'update:modelValue': [mode: TunnelMode]}>()
const modes: TunnelMode[] = ['local', 'remote', 'dynamic', 'reverse-dynamic']
</script>

<template>
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-6" role="radiogroup" :aria-label="$t('隧道类型')">
    <button
      v-for="mode in modes"
      :key="mode"
      class="tunnel-mode relative p-4 text-left transition-all"
      :class="{selected: modelValue === mode}"
      :aria-checked="modelValue === mode"
      role="radio"
      @click="emit('update:modelValue', mode)"
    >
      <span v-if="modelValue === mode" class="absolute top-2.5 right-2.5 w-4 h-4 rounded-full bg-[var(--primary-hover)] grid place-items-center">
        <Icon name="check" :size="10" extra-class="text-[var(--primary-foreground)] font-bold" />
      </span>
      <div class="text-[13px] font-semibold" :class="modelValue === mode ? 'text-[var(--primary-hover)]' : 'text-[var(--foreground)]'">{{ details[mode].title }}</div>
      <div class="text-[12px] text-mist mt-1">{{ details[mode].summary }}</div>
    </button>
  </div>
</template>

<style scoped>
.tunnel-mode{border:1px solid var(--border-subtle);border-radius:var(--radius-control);background:var(--surface-bg)}
.tunnel-mode:hover{background:var(--surface-raised-bg)}
.tunnel-mode.selected{border-color:var(--accent-border);background:var(--primary-weak)}
</style>
