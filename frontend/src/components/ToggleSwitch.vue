<script lang="ts" setup>
defineProps<{modelValue: boolean; disabled?: boolean}>()
const emit = defineEmits<{(e: 'update:modelValue', v: boolean): void}>()
</script>

<template>
  <button
    role="switch"
    class="toggle"
    :class="modelValue ? 'on' : ''"
    :aria-checked="modelValue"
    :disabled="disabled"
    @click="emit('update:modelValue', !modelValue)"
  ></button>
</template>
