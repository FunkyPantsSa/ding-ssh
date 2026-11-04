<script setup lang="ts">
import Icon from '@/components/Icon.vue'
import {useToast} from '@/composables/useToast'
const toast = useToast()
</script>
<template>
  <Teleport to="body">
    <div class="toast-region" data-ui="toast" aria-live="polite" aria-relevant="additions removals">
      <TransitionGroup name="toast">
        <article v-for="item in toast.items" :key="item.id" class="toast-item" :data-state="item.tone" role="status">
          <Icon :name="item.tone === 'success' ? 'check' : item.tone === 'error' ? 'alert-circle' : 'info'" :size="16" />
          <div><strong>{{ item.title }}</strong><p v-if="item.detail">{{ item.detail }}</p></div>
          <button type="button" aria-label="关闭通知" @click="toast.dismiss(item.id)"><Icon name="x" :size="14" /></button>
        </article>
      </TransitionGroup>
    </div>
  </Teleport>
</template>
<style scoped>
.toast-region{position:fixed;right:16px;bottom:42px;z-index:200;display:grid;gap:8px;width:min(360px,calc(100vw - 32px));pointer-events:none}.toast-item{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:start;gap:10px;padding:12px;border:1px solid var(--border);border-radius:var(--radius-lg);background:var(--surface-2);box-shadow:var(--shadow-pop);color:var(--foreground-secondary);pointer-events:auto}.toast-item[data-state=success]>:first-child{color:var(--success)}.toast-item[data-state=error]>:first-child{color:var(--destructive)}.toast-item strong{display:block;color:var(--foreground);font-size:var(--text-sm);font-weight:600}.toast-item p{margin:3px 0 0;font-size:var(--text-xs);line-height:var(--leading-normal)}.toast-item button{color:var(--muted-foreground)}.toast-enter-active,.toast-leave-active{transition:opacity var(--duration-base),transform var(--duration-base)}.toast-enter-from,.toast-leave-to{opacity:0;transform:translateY(4px)}
</style>
