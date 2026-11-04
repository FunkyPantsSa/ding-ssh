<script setup lang="ts">
import {cn} from '@/lib/utils'
defineOptions({inheritAttrs: false})
const model = defineModel<string | number>()
const props = defineProps<{invalid?: boolean; class?: string}>()
</script>
<template><select v-model="model" v-bind="$attrs" :class="cn('ui-select', props.class)" :aria-invalid="invalid || undefined"><slot /></select></template>
<style scoped>
.ui-select{width:100%;height:32px;padding:0 var(--space-2);color:var(--foreground);background:var(--input-bg);border:1px solid var(--input-border);border-radius:var(--radius-md);transition:border-color var(--duration-base),box-shadow var(--duration-base)}
.ui-select:hover:not(:disabled){border-color:var(--border-strong)}
.ui-select:focus-visible{outline:none;border-color:var(--border-focus);box-shadow:0 0 0 3px var(--primary-weak)}
.ui-select:disabled{opacity:.55;cursor:not-allowed}
.ui-select[aria-invalid='true']{border-color:var(--destructive)}
</style>
