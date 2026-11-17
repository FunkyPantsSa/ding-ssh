<script setup lang="ts">
import {computed, ref, useAttrs} from 'vue'
import {cn} from '@/lib/utils'
import {t} from '../../../i18n'
defineOptions({inheritAttrs: false})
const model = defineModel<string | number>()
const props = withDefaults(defineProps<{invalid?: boolean; class?: string; clearable?: boolean}>(), {clearable: true})
const attrs = useAttrs()
const el = ref<HTMLInputElement | null>(null)
const canClear = computed(() => props.clearable !== false
  && !['number', 'range', 'color', 'file', 'checkbox', 'radio', 'hidden'].includes(String(attrs.type ?? 'text'))
  && !(attrs.disabled === '' || attrs.disabled === true)
  && !(attrs.readonly === '' || attrs.readonly === true)
  && model.value !== undefined && model.value !== null && String(model.value).length > 0)
function clear() {
  if (!canClear.value) return
  // Use the native input event so v-model modifiers and input listeners stay consistent.
  if (el.value) {
    el.value.value = ''
    el.value.dispatchEvent(new Event('input', {bubbles: true}))
    el.value.focus()
  }
}
defineExpose({
  focus: () => el.value?.focus(),
  select: () => el.value?.select(),
  get element() { return el.value },
})
</script>
<template>
  <span :class="cn('ui-input-wrap', props.class)">
    <input ref="el" v-model="model" v-bind="$attrs" :class="cn('ui-input', props.class, {'has-clear': canClear})" :aria-invalid="invalid || undefined" />
    <button v-if="canClear" class="ui-input-clear" type="button" :aria-label="t('清除')" :title="t('清除')" @mousedown.prevent @click.stop="clear">{{ t('清除') }}</button>
  </span>
</template>
<style scoped>
.ui-input-wrap{position:relative;display:inline-flex;min-width:0;width:100%;vertical-align:middle}
.ui-input{min-width:0;width:100%;height:32px;padding:0 var(--space-2);color:var(--foreground);background:var(--input-bg);border:1px solid var(--input-border);border-radius:var(--radius-control)}
.ui-input:focus{outline:none;background:var(--input-bg);border-color:var(--input-border);box-shadow:0 0 0 3px var(--primary-weak)}
.ui-input.has-clear{padding-right:52px}
.ui-input-clear{position:absolute;right:5px;top:50%;transform:translateY(-50%);height:24px;padding:0 6px;border:0;border-radius:var(--radius-sm);background:var(--input-bg);color:var(--muted-foreground);font-size:var(--text-2xs);white-space:nowrap;cursor:pointer}
.ui-input-clear:hover{color:var(--foreground);background:var(--interactive-hover-bg)}
.ui-input-clear:focus-visible{outline:2px solid var(--border-focus);outline-offset:1px}
.ui-input[type=search]::-webkit-search-cancel-button{-webkit-appearance:none}
</style>
