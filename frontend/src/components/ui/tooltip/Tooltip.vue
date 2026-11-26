<script setup lang="ts">
withDefaults(defineProps<{text: string; align?: 'center' | 'left' | 'right'}>(), {align: 'center'})
</script><template><span class="ui-tooltip" :class="`align-${align}`"><slot/><span role="tooltip">{{text}}</span></span></template><style scoped>
.ui-tooltip { position:relative; display:inline-flex }
.ui-tooltip > span {
  position:absolute; z-index:200; bottom:calc(100% + 6px);
  display:none;
  width:max-content; max-width:260px;
  padding:6px 8px;
  white-space:pre-line; text-align:left; line-height:1.5;
  font-size:var(--text-2xs); color:var(--foreground);
  background:var(--surface-overlay-bg, var(--surface-3));
  backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(160%); -webkit-backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(160%);
  border:1px solid color-mix(in srgb,var(--accent-border) 32%,var(--border-default)); border-radius:var(--radius-control);
  box-shadow:var(--shadow-pop);
  pointer-events:none;
}
.ui-tooltip.align-center > span { left:50%; transform:translateX(-50%) }
.ui-tooltip.align-left > span { left:0 }
.ui-tooltip.align-right > span { right:0 }
.ui-tooltip:hover > span, .ui-tooltip:focus-within > span { display:block }
</style>
