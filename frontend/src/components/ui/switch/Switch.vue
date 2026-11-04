<script setup lang="ts">
const checked = defineModel<boolean>({default: false})
defineProps<{disabled?: boolean; label?: string}>()
</script>
<template><button type="button" class="ui-switch" :class="checked && 'on'" role="switch" :aria-checked="checked" :aria-label="label" :disabled="disabled" @click="checked=!checked"><span /></button></template>
<style scoped>.ui-switch{width:32px;height:18px;padding:2px;border-radius:999px;background:var(--border-strong);transition:background var(--duration-base) var(--ease-out)}.ui-switch span{display:block;width:14px;height:14px;border-radius:50%;background:var(--control-thumb);transition:transform var(--duration-base) var(--ease-out)}.ui-switch.on{background:var(--primary)}.ui-switch.on span{transform:translateX(14px)}</style>
