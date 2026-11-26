<script setup lang="ts">
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {computed, nextTick, onBeforeUnmount, ref, watch} from 'vue'
import {focusableControls, focusControl} from '../../utils/keyboard-navigation'
import Icon from '@/components/Icon.vue'
import type {AppCommand} from '@/commands'

const open = defineModel<boolean>({required: true})
const props = defineProps<{commands: AppCommand[]; recentIds: string[]}>()
const emit = defineEmits<{run: [command: AppCommand]; close: []; clearRecent: []}>()
const query = ref('')
const settledQuery = ref('')
const index = ref(0)
const input = ref<HTMLInputElement>()
const dialog = ref<HTMLElement>()
let previousFocus: HTMLElement | null = null
let focusRevision = 0
let debounceTimer: number | undefined

type GroupName = string
type CommandGroup = {name: GroupName; items: AppCommand[]; overflow?: number}

function groupName(command: AppCommand): GroupName {
  if (command.id.startsWith('connect-')) return t("服务器")
  if (command.section === t("导航")) return t("导航")
  return t("操作")
}

function matchScore(command: AppCommand, raw: string): number {
  if (!raw) return 0
  const q = raw.toLowerCase()
  const title = command.title.toLowerCase()
  const subtitle = (command.subtitle || '').toLowerCase()
  const id = command.id.toLowerCase()
  if (title.startsWith(q) || id.startsWith(q)) return 10
  if (title.includes(q) || id.includes(q)) return 20
  if (subtitle.includes(q)) return command.id.startsWith('connect-') ? 30 : 25
  return Number.POSITIVE_INFINITY
}

const groups = computed<CommandGroup[]>(() => {
  const q = settledQuery.value.trim()
  const recentOrder = new Map(props.recentIds.map((id, position) => [id, position]))
  const matches = props.commands
    .map((command, position) => ({command, position, score: matchScore(command, q)}))
    .filter((entry) => Number.isFinite(entry.score))
    .sort((a, b) => {
      const ar = recentOrder.get(a.command.id)
      const br = recentOrder.get(b.command.id)
      if (ar !== undefined || br !== undefined) return (ar ?? 999) - (br ?? 999)
      return a.score - b.score || a.position - b.position
    })
    .map((entry) => entry.command)

  const result: CommandGroup[] = []
  const used = new Set<string>()
  const recent = props.recentIds
    .map((id) => matches.find((command) => command.id === id))
    .filter((command): command is AppCommand => !!command)
    .slice(0, 10)
  if (recent.length) {
    result.push({get name() { return t("最近") }, items: recent})
    recent.forEach((command) => used.add(command.id))
  }
  for (const name of [t("服务器"), t("操作"), t("导航")] as const) {
    const all = matches.filter((command) => !used.has(command.id) && groupName(command) === name)
    const cap = name === t("服务器") ? 50 : 60
    if (all.length) result.push({name, items: all.slice(0, cap), overflow: Math.max(0, all.length - cap)})
  }
  let remaining = 60
  return result.flatMap((group) => {
    if (remaining <= 0) return []
    const items = group.items.slice(0, remaining)
    remaining -= items.length
    return items.length ? [{...group, items, overflow: (group.overflow || 0) + group.items.length - items.length}] : []
  })
})

const flatItems = computed(() => groups.value.flatMap((group) => group.items))
const activeId = computed(() => flatItems.value[index.value]?.id)

function close() { open.value = false; emit('close') }
function run(command?: AppCommand) {
  const target = command ?? flatItems.value[index.value]
  if (!target) return
  emit('run', target)
  close()
}
function select(command: AppCommand) { index.value = flatItems.value.findIndex((item) => item.id === command.id) }
function settleQuery() {
  window.clearTimeout(debounceTimer)
  if (settledQuery.value !== query.value) { settledQuery.value = query.value; index.value = 0 }
}

function keydown(event: KeyboardEvent) {
  if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.altKey || event.ctrlKey || event.metaKey) return
  if (event.key === 'Tab') {
    const controls = dialog.value ? focusableControls(dialog.value) : []
    const current = controls.indexOf(document.activeElement as HTMLElement)
    if (current < 0 || event.shiftKey && current === 0 || !event.shiftKey && current === controls.length - 1) {
      event.preventDefault()
      event.stopPropagation()
      focusControl(event.shiftKey ? controls.at(-1) : controls[0])
    }
    return
  }
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); return }
  const option = event.target instanceof Element ? event.target.closest<HTMLElement>('[role="option"]') : null
  if (event.target !== input.value && !option) return
  const navigation = ['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)
  const activate = event.key === 'Enter' || event.key === ' ' && !!option
  if (!navigation && !activate) return
  event.preventDefault()
  event.stopPropagation()
  settleQuery()
  const items = flatItems.value
  if (event.key === 'ArrowDown') index.value = items.length ? (index.value + 1) % items.length : 0
  else if (event.key === 'ArrowUp') index.value = items.length ? (index.value - 1 + items.length) % items.length : 0
  else if (event.key === 'Home') index.value = 0
  else if (event.key === 'End') index.value = Math.max(0, items.length - 1)
  else if (!event.repeat) run()
  if (navigation && option) void nextTick(() => focusControl(document.getElementById(`command-${activeId.value}`)))
}

function scrollToActive() {
  nextTick(() => {
    if (!activeId.value) return
    const el = document.getElementById(`command-${activeId.value}`)
    el?.scrollIntoView?.({block: 'nearest', inline: 'nearest'})
  })
}

watch(activeId, () => {
  scrollToActive()
})

watch(query, (value) => {
  window.clearTimeout(debounceTimer)
  debounceTimer = window.setTimeout(() => { settledQuery.value = value; index.value = 0 }, props.commands.length > 200 ? 80 : 0)
})
function restoreFocus() {
  if (!dialog.value?.contains(document.activeElement) && document.activeElement !== document.body) return
  focusControl(previousFocus)
}

watch(open, async (value) => {
  const revision = ++focusRevision
  if (!value) {
    window.clearTimeout(debounceTimer)
    await nextTick()
    if (!open.value && revision === focusRevision) restoreFocus()
    return
  }
  previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  window.clearTimeout(debounceTimer)
  query.value = ''; settledQuery.value = ''; index.value = 0
  await nextTick()
  if (!open.value || revision !== focusRevision) return
  input.value?.focus()
  scrollToActive()
}, {immediate: true})
onBeforeUnmount(() => { ++focusRevision; window.clearTimeout(debounceTimer); if (open.value) restoreFocus() })
</script>

<template>
  <div v-if="open" class="modal-root" data-ui="modal" @click.self="close">
    <div ref="dialog" class="modal command-palette" role="dialog" @keydown="keydown" aria-modal="true" :aria-label="$t('命令面板')" data-ui="command-palette">
      <div class="command-search">
        <Icon name="search" :size="15" extra-class="text-mist" />
        <input ref="input" v-model="query" :aria-label="$t('搜索命令或服务器')" role="combobox" aria-controls="command-results" :aria-activedescendant="activeId ? `command-${activeId}` : undefined" :aria-expanded="open" aria-autocomplete="list" :placeholder="$t('搜索命令、服务器或操作…')" />
        <span class="kbd">ESC</span>
      </div>
      <div id="command-results" class="command-list" role="listbox">
        <section v-for="group in groups" :key="group.name" class="command-group">
          <header><span>{{ group.name }}</span><button v-if="group.name === t('最近')" type="button" @click="emit('clearRecent')">{{ $t('清除') }}</button></header>
          <button v-for="command in group.items" :id="`command-${command.id}`" :key="command.id" type="button" role="option" tabindex="-1" :aria-selected="command.id === activeId" :class="command.id === activeId && 'active'" :data-testid="`command-item-${command.id}`" @focus="select(command)" @mouseenter="select(command)" @click="run(command)">
            <span><strong>{{ command.title }}</strong><small v-if="command.subtitle">{{ command.subtitle }}</small></span><small>{{ command.section }}</small>
          </button>
          <p v-if="group.overflow" class="command-more">{{ $t('还有') }} {{ group.overflow }} {{ $t('项，继续输入以筛选') }}</p>
        </section>
        <div v-if="!flatItems.length" class="command-empty" data-ui="empty-state"><strong>{{ $t('无匹配命令') }}</strong><span>{{ $t('请检查拼写，或搜索“服务器”打开服务器管理。') }}</span></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.command-palette {
  width: min(620px, 92vw);
  padding: 0;
  overflow: hidden;
  border-radius: var(--radius-card, 16px);
  border: 1px solid color-mix(in srgb, var(--accent-border) 32%, var(--border-default));
  background: var(--surface-overlay-bg);
  backdrop-filter: blur(var(--glass-overlay-blur, 28px)) saturate(160%);
  -webkit-backdrop-filter: blur(var(--glass-overlay-blur, 28px)) saturate(160%);
  box-shadow: var(--shadow-floating);
}
.command-search {
  height: 50px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 16px;
  background: var(--interactive-hover-bg);
  border-bottom: 1px solid var(--border-subtle);
}
.command-search input {
  flex: 1;
  min-width: 0;
  height: 100%;
  border: 0;
  background: transparent;
  font-size: 14px;
  color: var(--text-primary);
}
.command-search input:focus-visible {
  outline: 2px solid var(--focus-ring-color);
  outline-offset: -2px;
  border-radius: var(--radius-control);
}
.command-list {
  max-height: 420px;
  overflow: auto;
  padding: 8px;
}
.command-group + .command-group {
  margin-top: 6px;
}
.command-group header {
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 10px;
  color: var(--muted-foreground);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: .12em;
  text-transform: uppercase;
}
.command-group header button {
  font-size: 10px;
  color: var(--primary);
}
.command-group > button {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  width: 100%;
  gap: 12px;
  padding: 9px 12px;
  border-radius: var(--radius-control, 10px);
  font-size: 13px;
  text-align: left;
  color: var(--foreground-secondary);
  border: 1px solid transparent;
  transition: all var(--dur) var(--ease);
}
.command-group > button:hover {
  background: var(--interactive-hover-bg);
  border-color: var(--border-subtle);
  color: var(--foreground);
}
.command-group > button.active {
  background: var(--arc-active-tab-gradient, var(--interactive-active-bg));
  color: var(--text-primary);
  border-color: var(--arc-active-tab-border, var(--accent-border));
  box-shadow: var(--shadow-sm);
}
.command-group strong {
  display: block;
  font-weight: 500;
}
.command-group small {
  display: block;
  margin-top: 2px;
  font-size: 11px;
  color: var(--muted-foreground);
}
.command-more {
  padding: 7px 10px;
  margin: 0;
  color: var(--muted-foreground);
  font-size: 11px;
}
.command-empty {
  display: grid;
  gap: 6px;
  padding: 36px 12px;
  text-align: center;
  color: var(--muted-foreground);
  font-size: 12px;
}
.command-empty strong {
  color: var(--foreground-secondary);
  font-size: 13px;
}
</style>
