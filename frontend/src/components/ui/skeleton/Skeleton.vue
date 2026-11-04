<script setup lang="ts">
withDefaults(defineProps<{rows?: number}>(), {rows: 3})
</script>
<template><div class="skeleton" data-ui="skeleton" data-state="loading" aria-busy="true" aria-label="正在加载"><span v-for="row in rows" :key="row" :style="{width: `${100 - ((row - 1) % 3) * 12}%`}" /></div></template>
<style scoped>.skeleton{display:grid;gap:10px}.skeleton span{height:12px;border-radius:var(--radius-sm);background:linear-gradient(90deg,var(--surface-3),var(--surface-4),var(--surface-3));background-size:200% 100%;animation:shimmer 1.2s linear infinite}</style>
