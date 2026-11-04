<script setup lang="ts">
defineProps<{text: string}>()
</script><template><span class="ui-tooltip"><slot/><span role="tooltip">{{text}}</span></span></template><style scoped>.ui-tooltip{position:relative;display:inline-flex}.ui-tooltip>span{position:absolute;z-index:10;bottom:calc(100% + 6px);left:50%;display:none;transform:translateX(-50%);padding:4px 6px;white-space:nowrap;font-size:var(--text-2xs);color:var(--foreground);background:var(--surface-3);border:1px solid var(--border);border-radius:var(--radius-sm)}.ui-tooltip:hover>span,.ui-tooltip:focus-within>span{display:block}</style>
