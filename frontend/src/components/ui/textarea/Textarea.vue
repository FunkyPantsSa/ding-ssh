<script setup lang="ts">
import {cn} from '@/lib/utils'
defineOptions({inheritAttrs: false})
const model = defineModel<string>()
const props = defineProps<{invalid?: boolean; class?: string}>()
</script>
<template><textarea v-model="model" v-bind="$attrs" :class="cn('ui-textarea', props.class)" :aria-invalid="invalid || undefined" /></template>
<style scoped>
.ui-textarea{min-width:0;width:100%;min-height:72px;padding:var(--space-2);color:var(--foreground);background:var(--input-bg);border:1px solid var(--input-border);border-radius:var(--radius-md);resize:vertical;transition:border-color var(--duration-base),box-shadow var(--duration-base)}
.ui-textarea:hover:not(:disabled){border-color:var(--border-strong)}
.ui-textarea:focus-visible{outline:none;border-color:var(--border-focus);box-shadow:0 0 0 3px var(--primary-weak)}
.ui-textarea:disabled{opacity:.55;cursor:not-allowed}
.ui-textarea[aria-invalid='true']{border-color:var(--destructive)}
</style>
