<script setup lang="ts">
defineProps<{collapsed?: boolean; resizing?: boolean}>()
</script>
<template>
  <section class="app-layout" :class="[{ 'layout-nav-collapsed': collapsed, resizing }]" data-ui="app-layout">
    <slot name="sidebar" />
    <slot name="resizer" />
    <section class="app-layout-content" data-ui="floating-canvas">
      <slot name="topbar" />
      <main class="app-layout-main" data-ui="main">
        <slot />
      </main>
      <slot name="statusbar" />
    </section>
  </section>
</template>
<style scoped>
.app-layout {
  height: 100%;
  min-height: 0;
  display: grid;
  grid-template-columns: var(--nav-w) minmax(0, 1fr);
  gap: 6px;
  padding: 0 6px 6px 6px;
  position: relative;
  background: transparent;
  transition: grid-template-columns var(--dur-slow) var(--ease);
}
.app-layout.layout-nav-collapsed {
  grid-template-columns: var(--nav-w-collapsed) minmax(0, 1fr);
}
.app-layout-content {
  grid-column: 2;
  grid-row: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: var(--glass-border);
  border-radius: 10px;
  background: transparent;
  backdrop-filter: blur(var(--glass-shell-blur, 16px));
  -webkit-backdrop-filter: blur(var(--glass-shell-blur, 16px));
  box-shadow: var(--shadow-canvas);
  position: relative;
}
.app-layout-main {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.app-layout.resizing {
  transition: none;
}
</style>
