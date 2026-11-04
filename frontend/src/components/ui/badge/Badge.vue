<script setup lang="ts">
import {computed} from 'vue'
const props = withDefaults(defineProps<{variant?: 'default' | 'success' | 'warning' | 'danger' | 'info'}>(), {variant: 'default'})
const cls = computed(() => `ui-badge ${props.variant}`)
</script>
<template><span :class="cls"><slot /></span></template><style scoped>.ui-badge{display:inline-flex;align-items:center;gap:4px;padding:2px 6px;border:1px solid var(--border);border-radius:var(--radius-sm);font-size:var(--text-2xs);color:var(--foreground-secondary);background:var(--surface-2)}.success{color:var(--success)}.warning{color:var(--warning)}.danger{color:var(--destructive)}.info{color:var(--info)}</style>
