<script setup lang="ts">
defineProps<{collapsed?: boolean; resizing?: boolean}>()
</script>
<template>
  <section class="app-layout" :class="[{ 'layout-nav-collapsed': collapsed, resizing }]" data-ui="app-layout">
    <slot name="sidebar" />
    <slot name="resizer" />
    <section class="app-layout-content"><slot name="topbar" /><main class="app-layout-main" data-ui="main"><slot /></main><slot name="statusbar" /></section>
  </section>
</template>
<style scoped>
.app-layout{height:100%;min-height:0;display:grid;grid-template-columns:var(--nav-w) minmax(0,1fr);gap:0;padding:0;position:relative;background:transparent}
.app-layout-content{grid-column:2;grid-row:1;min-width:0;min-height:0;display:flex;flex-direction:column;overflow:hidden;border:0;border-radius:var(--radius-layout);background:transparent;box-shadow:none}
.app-layout-main{flex:1;min-width:0;min-height:0;display:flex;flex-direction:column}
.app-layout.resizing{transition:none}
</style>
