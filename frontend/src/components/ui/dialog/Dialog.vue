<script setup lang="ts">
import {t as $t} from '../../../i18n'

import {t} from '../../../i18n'

import {nextTick, onBeforeUnmount, ref, useId, watch} from 'vue'
import {focusableControls, focusControl, isAvailableControl, navigateKeyboardFocus} from '../../../utils/keyboard-navigation'

const open = defineModel<boolean>({default: false})
const props = withDefaults(defineProps<{title?: string; closeOnOverlay?: boolean; closeOnEscape?: boolean; showClose?: boolean; closeDisabled?: boolean; width?: number | string; initialFocus?: string; footerNavigation?: boolean}>(), {
  closeOnOverlay: true,
  closeOnEscape: true,
  showClose: true,
  closeDisabled: false,
  width: 560,
})
const emit = defineEmits<{close: []}>()
const panel = ref<HTMLElement | null>(null)
const titleId = `ui-dialog-title-${useId()}`
let lastActive: HTMLElement | null = null

function focusables(): HTMLElement[] {
  return panel.value ? focusableControls(panel.value) : []
}

function isTopDialog() {
  const dialogs = Array.from(document.querySelectorAll<HTMLElement>('[role="dialog"][aria-modal="true"]')).filter(isAvailableControl)
  return dialogs.at(-1) === panel.value
}

async function focusInitial() {
  await nextTick()
  if (!open.value || !isTopDialog()) return
  const explicit = props.initialFocus ? panel.value?.querySelector<HTMLElement>(props.initialFocus) : null
  // 默认把焦点放到第一个表单控件；没有表单时才落到关闭按钮 / 第一个可交互元素。
  const items = focusables()
  const formControl = items.find(el => el.matches('input,select,textarea,[autofocus]'))
  const target = (explicit && isAvailableControl(explicit) ? explicit : null) ?? formControl ?? items[0] ?? panel.value
  focusControl(target)
}

function close() {
  if (props.closeDisabled) return
  open.value = false
  emit('close')
}

function onKey(e: KeyboardEvent) {
  if (!open.value || !isTopDialog() || e.defaultPrevented) return
  if (e.key === 'Escape') {
    if (e.isComposing || (e.target instanceof HTMLElement && e.target.closest('select,[role="menu"],[role="listbox"],[role="combobox"][aria-expanded="true"]'))) return
    e.preventDefault()
    e.stopPropagation()
    if (props.closeOnEscape) close()
    return
  }
  const footerOnly = props.footerNavigation && !focusables().some(el => el.matches('input,select,textarea'))
  if (props.footerNavigation && panel.value && !e.isComposing
    && (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key) || (footerOnly && e.key === 'Tab'))) {
    const buttons = focusableControls(panel.value).filter(el => el.matches('footer button'))
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
    if (index >= 0 && buttons.length) {
      e.preventDefault()
      e.stopPropagation()
      const previous = e.key === 'ArrowLeft' || e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)
      focusControl(buttons[(index + (previous ? -1 : 1) + buttons.length) % buttons.length])
      return
    }
  }
  if (e.key !== 'Tab' || !panel.value) return
  const items = focusables()
  if (!items.length) {
    e.preventDefault()
    panel.value.focus()
    return
  }
  const first = items[0]
  const last = items[items.length - 1]
  const active = document.activeElement as HTMLElement | null
  // 焦点在弹窗外（例如被脚本移走）时先拉回第一个可交互元素。
  if (!active || !panel.value.contains(active)) {
    e.preventDefault()
    first.focus()
    return
  }
  if (e.shiftKey && active === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && active === last) {
    e.preventDefault()
    first.focus()
  }
}

function cleanup() {
  document.removeEventListener('keydown', onKey, true)
  if (lastActive && (!document.activeElement || document.activeElement === document.body || panel.value?.contains(document.activeElement))) {
    focusControl(lastActive)
  }
  lastActive = null
}

watch(
  open,
  async (visible) => {
    if (visible) {
      lastActive = document.activeElement instanceof HTMLElement ? document.activeElement : null
      document.addEventListener('keydown', onKey, true)
      await focusInitial()
    } else {
      cleanup()
    }
  },
  {immediate: true},
)

onBeforeUnmount(cleanup)
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="ui-dialog-root" @mousedown.self="closeOnOverlay && close()">
      <section
        ref="panel"
        class="ui-dialog"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="title ? titleId : undefined"
        :aria-label="title ? undefined : t('对话框')"
        tabindex="-1"
        data-keyboard-nav="grid"
        @keydown="navigateKeyboardFocus($event, panel!)"
        :style="{width: typeof width === 'number' ? `min(${width}px, 100%)` : width}"
      >
        <header v-if="title || $slots.header">
          <slot name="header"><h2 :id="titleId">{{ title }}</h2></slot>
          <button v-if="showClose" type="button" class="ui-dialog-close" :aria-label="$t('关闭对话框')" @click="close">×</button>
        </header>
        <div class="ui-dialog-body"><slot /></div>
        <footer v-if="$slots.footer" data-keyboard-nav="horizontal"><slot name="footer" /></footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.ui-dialog-root{position:fixed;inset:0;z-index:1000;display:grid;place-items:center;padding:var(--space-4);background:var(--overlay)}
.ui-dialog{width:min(560px,100%);max-height:calc(100vh - 32px);overflow:auto;padding:var(--space-5);background:var(--surface-overlay-bg);backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(135%);-webkit-backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(135%);border:1px solid color-mix(in srgb,var(--accent-border) 32%,var(--border-default));border-radius:var(--radius-lg);box-shadow:var(--shadow-pop)}
.ui-dialog:focus{outline:none}
.ui-dialog:focus-visible{outline:2px solid var(--focus-ring-color);outline-offset:2px}
header,footer{display:flex;align-items:center;justify-content:space-between;gap:var(--space-3)}
footer{justify-content:flex-end;margin-top:var(--space-5)}
h2{margin:0;font-size:var(--text-md);color:var(--foreground)}
.ui-dialog-close{width:26px;height:26px;display:grid;place-items:center;border-radius:var(--radius-sm);color:var(--muted-foreground);font-size:18px;line-height:1}
.ui-dialog-close:hover{background:var(--interactive-hover-bg);color:var(--text-primary)}
.ui-dialog-body{margin-top:var(--space-3)}
</style>

<style scoped>
@supports not ((backdrop-filter:blur(1px)) or (-webkit-backdrop-filter:blur(1px))){.ui-dialog{background:var(--surface-overlay-bg);backdrop-filter:none;-webkit-backdrop-filter:none}}
</style>
