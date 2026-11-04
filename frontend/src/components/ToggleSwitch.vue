<script lang="ts" setup>
import Switch from './ui/switch/Switch.vue'
const props = defineProps<{modelValue: boolean; disabled?: boolean}>()
const emit = defineEmits<{(e: 'update:modelValue', v: boolean): void}>()
</script>

<template><Switch class="toggle" :model-value="props.modelValue" :disabled="disabled" @update:model-value="emit('update:modelValue', $event)" /></template>
