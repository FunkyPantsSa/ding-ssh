<script setup lang="ts">
/**
 * Stable workspace boundary. Its default slot deliberately preserves the
 * existing TerminalView nodes so moving this boundary never destroys a live
 * xterm instance or SSH session.
 */
defineProps<{active: boolean}>()
</script>

<template>
  <section v-show="active" class="terminal-workspace" data-ui="workspace">
    <slot />
  </section>
</template>

<style scoped>.terminal-workspace{flex:1;min-height:0;display:flex;flex-direction:column}</style>
