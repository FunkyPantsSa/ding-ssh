<script setup lang="ts">
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {t} from '../../i18n'
import {Button, Input} from '../ui'
import Icon from '../Icon.vue'
import type {SudoPromptEvent} from '../../types'
import {placeTerminalPopover, type CursorAnchor} from '../../utils/terminal-popover'
import {shouldFocusTerminal} from '../../utils/terminal-focus'

const props = defineProps<{
  prompt: SudoPromptEvent
  anchor: CursorAnchor | null
  bounds: {width: number; height: number}
  serverName: string
  automatic: boolean
  submitting: boolean
  error: string
}>()
const password = defineModel<string>('password', {required: true})
const emit = defineEmits<{submit: []; cancel: []; manual: [initial?: string]; interrupt: []}>()
const root = ref<HTMLElement>()
const input = ref<{focus: () => void}>()
const size = ref({width: 320, height: 175})
let observer: ResizeObserver | undefined
const position = computed(() => placeTerminalPopover(props.anchor ?? {left: 8, top: 8, height: 0}, props.bounds, size.value))

async function focus() {
  await nextTick()
  if (!shouldFocusTerminal({root: root.value?.closest<HTMLElement>('.terminal-theme') ?? root.value, active: true, workspace: true, intent: 'restore', restoreFrom: [root.value]})) return
  if (props.automatic) root.value?.focus()
  else input.value?.focus()
}

function onKey(event: KeyboardEvent) {
  if (event.defaultPrevented || event.isComposing) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); emit('cancel') }
  else if (event.ctrlKey && event.key.toLowerCase() === 'c') { event.preventDefault(); event.stopPropagation(); emit('interrupt') }
  else if (event.target instanceof HTMLButtonElement) return
  else if (event.key === 'Enter') { event.preventDefault(); event.stopPropagation(); emit('submit') }
  else if (props.automatic && event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey && !(event.target instanceof HTMLButtonElement)) {
    event.preventDefault(); event.stopPropagation(); emit('manual', event.key)
  }
}

watch(() => [props.prompt.requestId, props.automatic], () => { void focus() })
watch(() => !!props.anchor, ready => { if (ready) void focus() })
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined' && root.value) {
    observer = new ResizeObserver(() => { if (root.value) size.value = {width: root.value.offsetWidth, height: root.value.offsetHeight} })
    observer.observe(root.value)
  }
  void focus()
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="root" class="sudo-popover" data-testid="terminal-sudo-popover" role="dialog" :aria-label="t('sudo 身份验证')" tabindex="-1"
    :style="{left: position.left + 'px', top: position.top + 'px', maxWidth: Math.max(0, bounds.width - 16) + 'px', maxHeight: Math.max(0, bounds.height - 16) + 'px', visibility: anchor ? 'visible' : 'hidden'}"
    @keydown="onKey" @pointerdown.stop @click.stop>
    <div class="flex items-center gap-2 font-medium"><Icon name="lock" :size="14" /><span>{{ t('sudo 身份验证') }}</span></div>
    <p class="sudo-identity" :title="`${prompt.user} · ${serverName}`">{{ prompt.user }} · {{ serverName }}</p>
    <p v-if="automatic" class="sudo-hint" role="status">{{ t('将使用保存的登录密码，1 秒后自动提交') }}</p>
    <template v-else>
      <p v-if="prompt.retry" class="sudo-hint">{{ t('密码未通过验证，请重新输入') }}</p>
      <Input ref="input" v-model="password" type="password" autocomplete="off" :clearable="false" :disabled="submitting" :aria-label="t('sudo 密码')" :placeholder="t('输入密码，仅用于本次验证')" />
    </template>
    <p v-if="error" class="sudo-error" role="alert">{{ error }}</p>
    <div class="sudo-actions">
      <Button size="sm" variant="ghost" :disabled="submitting" @click="emit('cancel')">{{ t('取消') }}</Button>
      <Button v-if="automatic" size="sm" variant="ghost" :disabled="submitting" @click="emit('manual')">{{ t('手动输入') }}</Button>
      <Button size="sm" variant="primary" :loading="submitting" :disabled="submitting || (!automatic && !password)" @click="emit('submit')">{{ automatic ? t('立即提交') : t('提交') }}</Button>
    </div>
  </div>
</template>

<style scoped>
.sudo-popover{position:absolute;z-index:65;width:320px;padding:12px;overflow:auto;border:1px solid color-mix(in srgb,var(--accent-border) 32%,var(--border-default));border-radius:var(--radius-lg);background:var(--surface-overlay-bg);backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(160%);-webkit-backdrop-filter:blur(var(--glass-overlay-blur,0px)) saturate(160%);color:var(--foreground);box-shadow:var(--shadow-floating);font-size:12px;outline:none}
.sudo-popover:focus-visible{outline:2px solid var(--focus-ring-color);outline-offset:2px}
.sudo-identity{margin:5px 0 10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted-foreground)}
.sudo-hint{margin:0 0 10px;line-height:1.65;color:var(--foreground-secondary)}
.sudo-error{margin-top:8px;color:var(--status-error);overflow-wrap:anywhere}
.sudo-actions{display:flex;justify-content:flex-end;gap:4px;margin-top:10px}
</style>
