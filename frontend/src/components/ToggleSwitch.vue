<script lang="ts" setup>
defineProps<{modelValue: boolean; disabled?: boolean}>()
const emit = defineEmits<{(e: 'update:modelValue', v: boolean): void}>()
</script>

<template>
  <button
    role="switch"
    :aria-checked="modelValue"
    :disabled="disabled"
    class="relative w-11 h-6 shrink-0 rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-400/60 disabled:opacity-50"
    :class="modelValue ? 'bg-sky-500' : 'bg-slate-700'"
    @click="emit('update:modelValue', !modelValue)"
  >
    <span
      class="absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform"
      :class="modelValue ? 'translate-x-5' : ''"
    ></span>
  </button>
</template>
