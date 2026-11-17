<script setup lang="ts">
import {cn} from '@/lib/utils'
defineOptions({inheritAttrs: false})
const model = defineModel<string | number>()
const props = defineProps<{invalid?: boolean; class?: string}>()
</script>
<template><select v-model="model" v-bind="$attrs" :class="cn('ui-select', props.class)" :aria-invalid="invalid || undefined"><slot /></select></template>
<style scoped>
.ui-select{min-width:0;width:100%;height:34px;padding:0 34px 0 11px;color:var(--foreground);background-color:var(--input-bg);background-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='14' height='14' viewBox='0 0 24 24' fill='none' stroke='%238a94a3' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m7 10 5 5 5-5'/%3E%3C/svg%3E");background-repeat:no-repeat;background-position:right 10px center;border:1px solid var(--input-border);border-radius:var(--radius-md);appearance:none;-webkit-appearance:none;box-shadow:inset 0 1px 0 rgb(255 255 255 / 4%);transition:border-color var(--duration-base),box-shadow var(--duration-base),background-color var(--duration-base)}
.ui-select:hover:not(:disabled){border-color:var(--border-strong)}
.ui-select:focus-visible{outline:none;border-color:var(--border-focus);box-shadow:0 0 0 3px var(--primary-weak)}
.ui-select:disabled{opacity:.55;cursor:not-allowed}
.ui-select[aria-invalid='true']{border-color:var(--destructive)}
.ui-select option{color:var(--foreground);background:var(--surface-2)}
</style>
