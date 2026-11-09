<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from '@/components/Icon.vue'
import type {TunnelInfo} from '@/types'
import {Button} from '@/components/ui'

const props = defineProps<{
  tunnel: TunnelInfo
  address: string
  addressLabel: string
  target: string
  copyOptions: string[]
  copied: boolean
  busy: boolean
}>()

const emit = defineEmits<{copy: [address: string]; loadAddresses: []; start: []; stop: []; restart: []; edit: []; remove: []}>()
const running = computed(() => props.tunnel.status === 'running')
const copyMenu = ref<HTMLDetailsElement>()
const moreMenu = ref<HTMLDetailsElement>()

function copy(address: string) {
  closeMenus()
  emit('copy', address)
}

function runMore(action: 'restart' | 'remove') {
  closeMenus()
  if (action === 'restart') emit('restart')
  else emit('remove')
}

function closeMenus() {
  for (const menu of [copyMenu.value, moreMenu.value]) {
    if (menu?.open) menu.open = false
  }
}

/** 同一时刻只展开一个菜单，避免「复制」和「更多」叠在一起。 */
function onMenuToggle(menu: HTMLDetailsElement | undefined) {
  if (!menu?.open) return
  for (const other of [copyMenu.value, moreMenu.value]) {
    if (other && other !== menu) other.open = false
  }
  if (menu === copyMenu.value) emit('loadAddresses')
}

/** 原生 <details> 不会自动收起：点击菜单外或按 Esc 时关闭。 */
function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target as Node | null
  if (!target) return
  for (const menu of [copyMenu.value, moreMenu.value]) {
    if (menu?.open && !menu.contains(target)) menu.open = false
  }
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') closeMenus()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  document.addEventListener('keydown', onDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  document.removeEventListener('keydown', onDocumentKeydown)
})
</script>

<template>
  <article class="neo neo-hover p-5 flex flex-col gap-3">
    <div class="flex items-start justify-between gap-3">
      <h3 class="text-sm font-semibold text-[var(--foreground)] min-w-0 truncate">{{ tunnel.name }}</h3>
      <span class="tunnel-status" :class="tunnel.status">
        <span class="status-dot" aria-hidden="true"></span>
        {{ running ? '运行中' : tunnel.status === 'error' ? '异常' : '已停止' }}
      </span>
    </div>

    <div class="neo-flat flex items-center gap-3 px-3 py-3">
      <div class="min-w-0 flex-1">
        <div class="text-[11px] text-mist">{{ addressLabel }}</div>
        <div class="font-mono text-[13px] text-[var(--foreground)] break-all">{{ address }}</div>
        <div v-if="!running" class="mt-1 text-[11px] text-mist">启动后可访问</div>
      </div>
      <details v-if="copyOptions.length" ref="copyMenu" class="tunnel-menu" @toggle="onMenuToggle(copyMenu)">
        <summary class="tunnel-menu-trigger">
          <Icon :name="copied ? 'check' : 'copy'" :size="14" />
          {{ copied ? '已复制' : '复制' }}
          <Icon name="chevron-down" :size="12" />
        </summary>
        <div class="tunnel-menu-panel">
          <div class="px-2 py-1 text-[11px] text-mist">选择客户端使用的地址</div>
          <Button v-for="option in copyOptions" :key="option" size="sm" variant="ghost" class="font-mono" @click="copy(option)">{{ option }}</Button>
        </div>
      </details>
      <Button v-else variant="ghost" size="sm" :title="`复制 ${address}`" @click="copy(address)">
        <Icon :name="copied ? 'check' : 'copy'" :size="14" />
        {{ copied ? '已复制' : '复制' }}
      </Button>
    </div>

    <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-xs">
      <dt class="text-mist">转发目标</dt>
      <dd class="m-0 break-all font-mono">{{ target }}</dd>
      <dt class="text-mist">经由 SSH</dt>
      <dd class="m-0 break-all font-mono" :title="tunnel.serverName">{{ tunnel.serverHost || tunnel.serverName }}</dd>
    </dl>
    <p v-if="tunnel.status === 'error' && tunnel.message" class="text-xs text-danger break-all">{{ tunnel.message }}</p>

    <div class="mt-auto flex items-center flex-wrap gap-1.5">
      <Button v-if="running" variant="secondary" size="sm" class="stop-button" :disabled="busy" @click="emit('stop')">停止</Button>
      <Button v-else variant="primary" size="sm" :disabled="busy" @click="emit('start')">启动</Button>
      <Button variant="ghost" size="sm" :disabled="busy" @click="emit('edit')">编辑</Button>
      <details ref="moreMenu" class="tunnel-menu" @toggle="onMenuToggle(moreMenu)">
        <summary class="tunnel-menu-trigger">更多 <Icon name="chevron-down" :size="12" /></summary>
        <div class="tunnel-menu-panel">
          <Button v-if="running" variant="ghost" size="sm" :disabled="busy" @click="runMore('restart')">重启</Button>
          <Button variant="ghost" size="sm" :disabled="busy" @click="runMore('remove')">删除</Button>
        </div>
      </details>
    </div>
  </article>
</template>

<style scoped>
.tunnel-status{display:inline-flex;align-items:center;gap:5px;white-space:nowrap;padding:3px 8px;border-radius:var(--radius-pill);font-size:11px;color:var(--muted-foreground);background:var(--hover)}
.tunnel-status.running{color:var(--status-success);background:var(--status-success-bg)}
.tunnel-status.error{color:var(--status-error);background:var(--status-error-bg)}
.status-dot{width:5px;height:5px;border-radius:50%;background:currentColor}
.stop-button{background:transparent}
.tunnel-menu{position:relative;flex-shrink:0}
.tunnel-menu-trigger{display:flex;align-items:center;gap:5px;cursor:pointer;list-style:none;padding:0 8px;height:28px;font-size:12px;border-radius:var(--radius-md);color:var(--foreground-secondary)}
.tunnel-menu-trigger::-webkit-details-marker{display:none}
.tunnel-menu-trigger:hover{background:var(--button-ghost-hover)}
.tunnel-menu-panel{position:absolute;z-index:5;right:0;top:calc(100% + 4px);display:grid;min-width:max-content;padding:4px;background:var(--surface-overlay-bg);border:1px solid var(--border-strong);border-radius:var(--radius-md);box-shadow:var(--shadow-pop)}
</style>
