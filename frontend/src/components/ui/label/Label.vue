<script setup lang="ts">
defineProps<{htmlFor?: string; required?: boolean}>()
</script><template><label class="ui-label" :for="htmlFor"><slot/><span v-if="required" aria-hidden="true"> *</span></label></template><style scoped>.ui-label{display:block;font-size:var(--text-xs);font-weight:500;color:var(--foreground-secondary)}.ui-label span{color:var(--destructive)}</style>
