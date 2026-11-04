<script setup lang="ts">
import Button from '../button/Button.vue'
defineProps<{title: string; description: string; detail?: string}>()
defineEmits<{retry: []}>()
</script>
<template>
  <section class="error-state" data-ui="error-state" role="alert">
    <strong>{{ title }}</strong>
    <p>{{ description }}</p>
    <details v-if="detail"><summary>技术详情</summary><pre>{{ detail }}</pre></details>
    <Button variant="ghost" size="sm" @click="$emit('retry')">重试</Button>
  </section>
</template>
<style scoped>
.error-state{display:grid;justify-items:start;gap:8px;padding:20px;border:1px solid color-mix(in srgb,var(--destructive) 30%,var(--border));border-radius:var(--radius-lg);background:var(--surface-2);color:var(--foreground-secondary)}
.error-state strong{color:var(--foreground)}
.error-state p{margin:0;font-size:var(--text-sm)}
.error-state details{max-width:100%;font-size:var(--text-xs)}
.error-state pre{max-width:100%;overflow:auto;white-space:pre-wrap;color:var(--muted-foreground)}
</style>
