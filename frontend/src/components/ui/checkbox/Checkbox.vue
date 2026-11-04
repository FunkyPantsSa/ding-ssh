<script setup lang="ts">
const checked = defineModel<boolean>({default: false})
defineProps<{disabled?: boolean; label?: string}>()
</script>
<template><label class="ui-checkbox"><input v-model="checked" type="checkbox" :disabled="disabled"/><span><slot>{{ label }}</slot></span></label></template>
<style scoped>.ui-checkbox{display:inline-flex;align-items:center;gap:var(--space-2);color:var(--foreground-secondary);cursor:pointer}.ui-checkbox input{width:14px;height:14px;accent-color:var(--primary)}</style>
