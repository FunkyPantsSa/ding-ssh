<script setup lang="ts">
import {computed} from 'vue'
import {cn} from '@/lib/utils'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'sm' | 'default' | 'icon'
    type?: 'button' | 'submit' | 'reset'
    loading?: boolean
    disabled?: boolean
  }>(),
  {variant: 'secondary', size: 'default', type: 'button'},
)
const classes = computed(() => cn('ui-button', `ui-button-${props.variant}`, `ui-button-${props.size}`))
</script>

<template>
  <button :type="type" :class="classes" :disabled="disabled || loading" :aria-busy="loading || undefined">
    <span v-if="loading" class="ui-button-spinner" aria-hidden="true"></span>
    <slot />
  </button>
</template>

<style scoped>
.ui-button{flex-shrink:0;white-space:nowrap;height:32px;display:inline-flex;align-items:center;justify-content:center;gap:var(--space-2);padding:0 var(--space-3);border:1px solid var(--border);border-radius:var(--radius-md);font-weight:500;transition:background var(--duration-base) var(--ease-out),border-color var(--duration-base) var(--ease-out)}
.ui-button:disabled{opacity:.5;cursor:not-allowed}
.ui-button-primary{background:var(--button-primary-bg);color:var(--button-primary-fg);border-color:transparent}
.ui-button-secondary{background:var(--surface-2);color:var(--foreground)}
.ui-button-ghost{background:transparent;color:var(--foreground-secondary);border-color:transparent}
.ui-button-danger{background:var(--destructive);color:var(--destructive-foreground);border-color:transparent}
.ui-button-ghost:hover:not(:disabled),.ui-button-secondary:hover:not(:disabled){background:var(--button-ghost-hover)}
.ui-button-icon{width:32px;padding:0}
.ui-button-sm{height:28px;font-size:var(--text-xs)}
.ui-button-spinner{flex-shrink:0;width:12px;height:12px;border:2px solid currentColor;border-right-color:transparent;border-radius:50%;animation:ui-button-spin .7s linear infinite}
@keyframes ui-button-spin{to{transform:rotate(360deg)}}
</style>
