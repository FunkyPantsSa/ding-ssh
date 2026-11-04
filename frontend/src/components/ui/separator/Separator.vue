<script setup lang="ts">
withDefaults(defineProps<{orientation?: 'horizontal' | 'vertical'}>(), {orientation: 'horizontal'})
</script><template><div class="ui-separator" :class="orientation" role="separator" :aria-orientation="orientation"/></template><style scoped>.ui-separator{background:var(--border-subtle)}.horizontal{height:1px;width:100%}.vertical{height:100%;width:1px}</style>
