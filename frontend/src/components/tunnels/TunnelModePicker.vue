<script setup lang="ts">
import Icon from '@/components/Icon.vue'

export type TunnelMode = 'local' | 'remote' | 'dynamic'
export type TunnelModeDetail = {title: string; summary: string; description: string; example: string}

defineProps<{
  modelValue: TunnelMode
  details: Record<TunnelMode, TunnelModeDetail>
}>()

const emit = defineEmits<{'update:modelValue': [mode: TunnelMode]}>()
const modes: TunnelMode[] = ['local', 'remote', 'dynamic']
</script>

<template>
  <div class="grid grid-cols-3 gap-3 mb-6" role="radiogroup" aria-label="隧道类型">
    <button
      v-for="mode in modes"
      :key="mode"
      class="relative neo-flat p-4 text-left transition-all"
      :class="modelValue === mode ? 'ring-1 ring-[var(--primary-hover)] bg-[var(--primary-weak)]' : 'hover:bg-surface-raised'"
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
