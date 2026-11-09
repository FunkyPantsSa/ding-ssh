<script setup lang="ts">
import {computed, nextTick, onBeforeUnmount, ref, useId} from 'vue'
import Icon from '../../Icon.vue'
import Input from '../input/Input.vue'

defineOptions({inheritAttrs: false})
const props = defineProps<{
  modelValue: string | undefined
  options: readonly {value: string; label: string; keywords?: string}[]
  placeholder?: string
  class?: string
  disabled?: boolean
}>()
const emit = defineEmits<{'update:modelValue': [value: string]}>()
const listId = useId()
const root = ref<HTMLElement>()
const list = ref<HTMLElement>()
const listStyle = ref<Record<string, string>>({})
const open = ref(false)
const query = ref('')
const active = ref(-1)
const selected = computed(() => props.options.find((option) => option.value === props.modelValue)?.label)

const LIST_GAP = 4
const LIST_MAX_HEIGHT = 180
const matches = computed(() => {
  const words = query.value.trim().toLowerCase().split(/\s+/)
  return props.options.filter(({label, keywords}) => {
    const haystack = `${label} ${keywords ?? ''}`.toLowerCase()
    return words.every((word) => haystack.includes(word))
  })
})
const text = computed({
  get: () => open.value ? query.value : selected.value ?? '',
  set: (value: string | number) => {
    open.value = true
    query.value = String(value)
    active.value = 0
  },
})

function show() {
  if (open.value || props.disabled) return
  open.value = true
  active.value = matches.value.findIndex((option) => option.value === props.modelValue)
  positionList()
  bind()
  void nextTick(positionList)
}

function close() {
  open.value = false
  query.value = ''
  active.value = -1
  unbind()
}

/** 焦点移出时关闭；relatedTarget 为空一般是点了不可聚焦区域（含面板滚动条），交给 pointerdown 处理。 */
function onBlur(event: FocusEvent) {
  const next = event.relatedTarget as Node | null
  if (next && (root.value?.contains(next) || list.value?.contains(next))) return
  if (next) close()
}

/**
 * 选项面板挂在 body 上（fixed 定位），始终浮在表单/弹窗上层：
 * 既不会把对话框撑高，也不会被弹窗的 overflow 裁掉。位置跟随输入框，
 * 下方空间不足时向上翻转。
 */
function positionList() {
  const el = root.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const height = Math.min(LIST_MAX_HEIGHT, list.value?.scrollHeight || LIST_MAX_HEIGHT)
  const spaceBelow = window.innerHeight - rect.bottom - LIST_GAP
  const spaceAbove = rect.top - LIST_GAP
  const openUp = spaceBelow < height && spaceAbove > spaceBelow
  const available = openUp ? spaceAbove : spaceBelow
  listStyle.value = {
    left: `${Math.max(LIST_GAP, Math.min(rect.left, window.innerWidth - rect.width - LIST_GAP))}px`,
    width: `${rect.width}px`,
    maxHeight: `${Math.max(88, Math.min(height, available))}px`,
    top: openUp ? 'auto' : `${rect.bottom + LIST_GAP}px`,
    bottom: openUp ? `${window.innerHeight - rect.top + LIST_GAP}px` : 'auto',
  }
}

function onViewportChange() {
  if (open.value) positionList()
}

/** 点击组件外任何地方都关掉面板（输入框失焦不一定触发，例如点到不可聚焦区域）。 */
function onDocumentPointerDown(event: PointerEvent) {
  if (!open.value) return
  const target = event.target as Node | null
  if (!target) return
  if (root.value?.contains(target) || list.value?.contains(target)) return
  close()
}

function bind() {
  window.addEventListener('resize', onViewportChange)
  // 捕获阶段监听，弹窗内部滚动容器滚动时也能跟随重新定位
  window.addEventListener('scroll', onViewportChange, true)
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
}

function unbind() {
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
}

onBeforeUnmount(unbind)

function choose(value: string) {
  close()
  emit('update:modelValue', value)
}

function onKeydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!open.value) show()
    const count = matches.value.length
    if (!count) return
    const step = event.key === 'ArrowDown' ? 1 : -1
    active.value = active.value < 0 ? (step === 1 ? 0 : count - 1) : (active.value + step + count) % count
    void nextTick(() => list.value?.children[active.value]?.scrollIntoView?.({block: 'nearest'}))
  } else if (event.key === 'Enter' && open.value) {
    event.preventDefault()
    const option = matches.value[active.value]
    if (option) choose(option.value)
  } else if (event.key === 'Escape' && open.value) {
    event.preventDefault()
    event.stopPropagation()
    close()
  }
}
</script>

<template>
  <div ref="root" :class="['search-select', props.class]">
    <div class="search-select-input">
      <Input
        v-bind="$attrs"
        v-model="text"
        role="combobox"
        autocomplete="off"
        aria-autocomplete="list"
        :aria-expanded="open"
        :aria-controls="open ? listId : undefined"
        :aria-activedescendant="open && matches[active] ? `${listId}-${active}` : undefined"
        :placeholder="open ? '输入关键词搜索' : (placeholder ?? '请选择或搜索')"
        :disabled="disabled"
        @focus="show"
        @click="show"
        @blur="onBlur"
        @keydown="onKeydown"
      />
      <Icon name="chevron-down" :size="14" />
    </div>
    <Teleport to="body">
      <ul v-if="open" :id="listId" ref="list" class="search-select-list" role="listbox" :style="listStyle">
        <li
          v-for="(option, index) in matches"
          :id="`${listId}-${index}`"
          :key="option.value"
          role="option"
          :aria-selected="option.value === modelValue"
          :class="{active: index === active}"
          @mousedown.prevent
          @click="choose(option.value)"
        >{{ option.label }}</li>
        <li v-if="!matches.length" class="search-select-empty" role="presentation">没有匹配选项</li>
      </ul>
    </Teleport>
  </div>
</template>

<style scoped>
.search-select{min-width:0;width:100%;position:relative}
.search-select-input{position:relative}
.search-select-input :deep(input){padding-right:28px}
.search-select-input :deep(svg){position:absolute;right:9px;top:50%;transform:translateY(-50%);pointer-events:none;color:var(--muted-foreground)}
.search-select-list{position:fixed;z-index:1100;box-sizing:border-box;overflow:auto;overscroll-behavior:contain;margin:0;padding:4px;list-style:none;background:var(--surface-overlay-bg);border:1px solid var(--border-strong);border-radius:var(--radius-md);color:var(--foreground);box-shadow:var(--shadow-pop)}
.search-select-list li{padding:6px 8px;overflow-wrap:anywhere;border-radius:var(--radius-sm);cursor:pointer}
.search-select-list li:hover,.search-select-list li.active{background:var(--interactive-hover-bg)}
.search-select-list li[aria-selected='true']{color:var(--primary-hover)}
.search-select-list .search-select-empty{color:var(--muted-foreground);cursor:default}
</style>
