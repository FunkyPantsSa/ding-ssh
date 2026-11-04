<script setup lang="ts">
const model = defineModel<string>({required: true})
defineProps<{items: {value: string; label: string; disabled?: boolean}[]}>()
</script><template><div class="ui-tabs" role="tablist"><button v-for="item in items" :key="item.value" role="tab" :aria-selected="model===item.value" :disabled="item.disabled" :class="model===item.value&&'active'" @click="model=item.value">{{item.label}}</button></div></template><style scoped>.ui-tabs{display:flex;gap:2px;padding:2px;background:var(--surface-2);border:1px solid var(--border);border-radius:var(--radius-control)}button{height:28px;padding:0 var(--space-2);border-radius:var(--radius-small);font-size:var(--text-xs);color:var(--muted-foreground)}button.active{color:var(--foreground);background:var(--surface-3)}</style>
