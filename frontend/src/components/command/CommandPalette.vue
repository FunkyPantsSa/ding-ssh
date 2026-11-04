<script setup lang="ts">
import {computed, nextTick, ref, watch} from 'vue'
import Icon from '@/components/Icon.vue'
import type {AppCommand} from '@/commands'

const open = defineModel<boolean>({required: true})
const props = defineProps<{commands: AppCommand[]; recentIds: string[]}>()
const emit = defineEmits<{run: [command: AppCommand]; close: []; clearRecent: []}>()
const query = ref('')
const settledQuery = ref('')
const index = ref(0)
const input = ref<HTMLInputElement>()
let debounceTimer: number | undefined

type GroupName = '最近' | '服务器' | '操作' | '导航'
type CommandGroup = {name: GroupName; items: AppCommand[]; overflow?: number}

function groupName(command: AppCommand): GroupName {
  if (command.id.startsWith('connect-')) return '服务器'
  if (command.section === '导航') return '导航'
  return '操作'
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
    result.push({name: '最近', items: recent})
    recent.forEach((command) => used.add(command.id))
  }
  for (const name of ['服务器', '操作', '导航'] as const) {
    const all = matches.filter((command) => !used.has(command.id) && groupName(command) === name)
    const cap = name === '服务器' ? 50 : 60
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
function keydown(event: KeyboardEvent) {
  const items = flatItems.value
  if (event.key === 'Tab') event.preventDefault()
  if (event.key === 'Escape') { event.preventDefault(); close() }
  if (event.key === 'ArrowDown') { event.preventDefault(); index.value = items.length ? (index.value + 1) % items.length : 0 }
  if (event.key === 'ArrowUp') { event.preventDefault(); index.value = items.length ? (index.value - 1 + items.length) % items.length : 0 }
  if (event.key === 'Enter') { event.preventDefault(); run() }
}

watch(query, (value) => {
  window.clearTimeout(debounceTimer)
  debounceTimer = window.setTimeout(() => { settledQuery.value = value; index.value = 0 }, props.commands.length > 200 ? 80 : 0)
})
watch(open, async (value) => {
  if (!value) return
  query.value = ''; settledQuery.value = ''; index.value = 0
  await nextTick(); input.value?.focus()
})
</script>

<template>
  <div v-if="open" class="modal-root" data-ui="modal" @click.self="close">
    <div class="modal command-palette" role="dialog" aria-modal="true" aria-label="命令面板" data-ui="command-palette">
      <div class="command-search">
        <Icon name="search" :size="15" extra-class="text-mist" />
        <input ref="input" v-model="query" aria-label="搜索命令或服务器" role="combobox" aria-controls="command-results" :aria-activedescendant="activeId ? `command-${activeId}` : undefined" :aria-expanded="open" placeholder="搜索命令、服务器或操作…" @keydown="keydown" />
        <span class="kbd">ESC</span>
      </div>
      <div id="command-results" class="command-list" role="listbox">
        <section v-for="group in groups" :key="group.name" class="command-group">
          <header><span>{{ group.name }}</span><button v-if="group.name === '最近'" type="button" @click="emit('clearRecent')">清除</button></header>
          <button v-for="command in group.items" :id="`command-${command.id}`" :key="command.id" type="button" role="option" :aria-selected="command.id === activeId" :class="command.id === activeId && 'active'" :data-testid="`command-item-${command.id}`" @mouseenter="select(command)" @click="run(command)">
            <span><strong>{{ command.title }}</strong><small v-if="command.subtitle">{{ command.subtitle }}</small></span><small>{{ command.section }}</small>
          </button>
          <p v-if="group.overflow" class="command-more">还有 {{ group.overflow }} 项，继续输入以筛选</p>
        </section>
        <div v-if="!flatItems.length" class="command-empty" data-ui="empty-state"><strong>无匹配命令</strong><span>请检查拼写，或搜索“服务器”打开服务器管理。</span></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.command-palette{width:min(600px,100%);padding:0;overflow:hidden}.command-search{height:48px;display:flex;align-items:center;gap:10px;padding:0 12px;box-shadow:inset 0 -1px 0 var(--border)}.command-search input{flex:1;min-width:0;height:100%;border:0;background:transparent;outline:0}.command-list{max-height:420px;overflow:auto;padding:6px}.command-group+.command-group{margin-top:5px}.command-group header{height:28px;display:flex;align-items:center;justify-content:space-between;padding:0 10px;color:var(--muted-foreground);font-size:var(--text-2xs);font-weight:600;letter-spacing:.1em;text-transform:uppercase}.command-group header button{font-size:var(--text-2xs);color:var(--primary)}.command-group>button{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;width:100%;gap:12px;padding:8px 10px;border-radius:var(--radius-sm);font-size:var(--text-sm);text-align:left;color:var(--foreground-secondary);transition:background var(--duration-fast),color var(--duration-fast)}.command-group>button:hover,.command-group>button.active{background:var(--hover-strong);color:var(--foreground)}.command-group strong{display:block;font-weight:500}.command-group small{display:block;margin-top:2px;font-size:var(--text-2xs);color:var(--muted-foreground)}.command-more{padding:7px 10px;margin:0;color:var(--muted-foreground);font-size:var(--text-2xs)}.command-empty{display:grid;gap:5px;padding:34px 12px;text-align:center;color:var(--muted-foreground);font-size:var(--text-xs)}.command-empty strong{color:var(--foreground-secondary);font-size:var(--text-sm)}
</style>
