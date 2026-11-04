<script setup lang="ts">
import {ref} from 'vue'
import {cn} from '@/lib/utils'
defineOptions({inheritAttrs: false})
const model = defineModel<string | number>()
const props = defineProps<{invalid?: boolean; class?: string}>()
const el = ref<HTMLInputElement | null>(null)
defineExpose({
  focus: () => el.value?.focus(),
  select: () => el.value?.select(),
  get element() { return el.value },
})
</script>
<template><input ref="el" v-model="model" v-bind="$attrs" :class="cn('ui-input', props.class)" :aria-invalid="invalid || undefined" /></template>
<style scoped>
.ui-input{width:100%;height:32px;padding:0 var(--space-2);color:var(--foreground);background:var(--input-bg);border:1px solid var(--input-border);border-radius:var(--radius-md);transition:border-color var(--duration-base),box-shadow var(--duration-base)}
.ui-input:hover:not(:disabled){border-color:var(--border-strong)}
.ui-input:focus-visible{outline:none;border-color:var(--border-focus);box-shadow:0 0 0 3px var(--primary-weak)}
.ui-input:disabled{opacity:.55;cursor:not-allowed}
.ui-input[aria-invalid='true']{border-color:var(--destructive)}
</style>
