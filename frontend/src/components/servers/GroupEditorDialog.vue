<script setup lang="ts">
import {t as $t} from '../../i18n'
import {computed, ref, watch} from 'vue'
import {Button, Dialog, Input} from '../ui'
import {normalizeGroupPath, splitGroupPath, validateGroupNameInput} from '../../utils/groupTree'

const props = defineProps<{
  modelValue: boolean
  mode: 'create' | 'rename'
  parentPath: string
  initialValue: string
  error: string
  submitting: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submit: [value: string]
  clearError: []
}>()

const value = ref('')
const localError = ref('')
const dirty = ref(false)

watch(
  () => [props.modelValue, props.initialValue, props.mode, props.parentPath] as const,
  ([open]) => {
    if (!open) return
    value.value = props.initialValue
    localError.value = ''
    dirty.value = false
  },
  {immediate: true},
)

watch(value, (next) => {
  localError.value = ''
  dirty.value = next !== props.initialValue
  if (props.error && next !== props.initialValue) emit('clearError')
})

const targetPath = computed(() => normalizeGroupPath([props.parentPath, value.value.trim()].filter(Boolean).join('/')))
const previewParts = computed(() => {
  if (props.mode === 'rename') {
    return [...splitGroupPath(props.parentPath), value.value.trim() || $t('分组名称')]
  }
  const parts = splitGroupPath(targetPath.value)
  if (!value.value.trim()) parts.push($t('分组名称'))
  return parts
})

const validation = computed(() => {
  if (props.mode === 'rename') {
    const leaf = value.value.trim()
    if (!leaf) return $t('请输入分组名称')
    if (leaf.includes('/')) return $t('重命名只支持修改当前层名称')
    if (leaf === '.' || leaf === '..') return $t('分组名称不能是 . 或 ..')
    if ([...leaf].length > 32) return $t('每级分组名称不能超过 32 个字符')
    if (/[\u0000-\u001f\u007f]/.test(leaf)) return $t('分组名称不能包含控制字符')
    return ''
  }
  const raw = value.value.trim()
  if (!raw) return $t('请输入分组名称')
  if (raw.startsWith('/') || raw.endsWith('/')) return $t('分组名称不能以 / 开头或结尾')
  if (raw.includes('//')) return $t('分组路径不能包含空层级')
  return validateGroupNameInput(targetPath.value, 6)
})

const message = computed(() => props.error || localError.value || (dirty.value ? validation.value : ''))

const open = computed({
  get: () => props.modelValue,
  set: (next: boolean) => emit('update:modelValue', next),
})

const title = computed(() => (props.mode === 'create' ? $t('新建子分组') : $t('重命名分组')))

function submit() {
  if (props.submitting) return
  if (validation.value) {
    localError.value = validation.value
    return
  }
  emit('submit', value.value.trim())
}
</script>

<template>
  <Dialog v-model="open" :title="title" :width="440" :close-disabled="submitting">
    <div class="group-editor">
      <label class="label" for="group-editor-input">{{ $t('分组名称') }}</label>
      <Input
        id="group-editor-input"
        v-model="value"
        autocomplete="off"
        spellcheck="false"
        :aria-invalid="!!message"
        :placeholder="$t('输入分组名称，支持 父组/子组')"
        @keyup.enter="submit"
      />
      <div class="preview" :aria-label="$t('分组路径预览')">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/></svg>
        <template v-for="(part, index) in previewParts" :key="index">
          <span v-if="index" class="slash">/</span>
          <span class="part">{{ part }}</span>
        </template>
      </div>
      <p v-if="mode === 'create'" class="hint">{{ $t('支持输入「父组/子组」一次创建多级；当前父级为「{0}」。', [parentPath || $t('根目录')]) }}</p>
      <p v-else class="hint">{{ $t('只修改当前层名称，子分组与服务器会自动跟随。') }}</p>
      <p v-if="message" class="error" role="alert">{{ message }}</p>
    </div>
    <template #footer>
      <Button variant="ghost" :disabled="submitting" @click="open = false">{{ $t('取消') }}</Button>
      <Button variant="primary" :disabled="submitting || !!validation" @click="submit">
        {{ submitting ? $t('保存中…') : $t('保存') }}
      </Button>
    </template>
  </Dialog>
</template>

<style scoped>
.group-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.label {
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--muted-foreground);
}
.preview {
  display: flex;
  align-items: center;
  gap: 5px;
  min-height: 28px;
  padding: 4px 8px;
  border-radius: var(--radius-small);
  background: var(--field-bg);
  box-shadow: inset 0 0 0 1px var(--line);
  color: var(--foreground-secondary);
  font-size: 12px;
  overflow: hidden;
}
.preview svg {
  flex: none;
  color: var(--accent-bg);
}
.part {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.part:last-child {
  color: var(--foreground);
  font-weight: 500;
}
.slash {
  color: var(--muted-foreground);
  opacity: 0.7;
}
.hint {
  margin: 0;
  color: var(--muted-foreground);
  font-size: 11.5px;
  line-height: 1.5;
}
.error {
  margin: 0;
  color: var(--destructive);
  font-size: 12px;
}
</style>
