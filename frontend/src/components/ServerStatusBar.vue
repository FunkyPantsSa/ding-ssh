<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {copyText} from '../utils/clipboard'
import {onSysInfoSnapshot, sysInfoService} from '../services/sysinfo'
import {getElementRect} from '../utils/dom'
import {useSettingsStore} from '../stores/settings'
import type {SessionTab, SysInfoSnapshot} from '../types'

const props = defineProps<{tab?: SessionTab}>()

const settings = useSettingsStore()

const snap = ref<SysInfoSnapshot | null>(null)
const diskKey = ref('')
const netKey = ref('')
const openMenu = ref<'disk' | 'net' | null>(null)
const menuStyle = ref<Record<string, string>>({})
const menuElement = ref<HTMLElement | null>(null)
let dispose: (() => void) | null = null

const storagePrefix = computed(() => `ding-ssh:statusbar:${props.tab?.node?.id || 'default'}`)

function loadPrefs() {
  try {
    const raw = localStorage.getItem(storagePrefix.value)
    if (!raw) return
    const o = JSON.parse(raw) as {disk?: string; net?: string}
    if (o.disk) diskKey.value = o.disk
    if (o.net) netKey.value = o.net
  } catch {
    /* ignore */
  }
}

function savePrefs() {
  try {
    localStorage.setItem(storagePrefix.value, JSON.stringify({disk: diskKey.value, net: netKey.value}))
  } catch {
    /* ignore */
  }
}

/** 错误包/空字段不覆盖已有成功数据 */
function mergeSnap(prev: SysInfoSnapshot | null, next: SysInfoSnapshot): SysInfoSnapshot {
  if (!prev) return next
  const hasMem = (next.memTotalMb ?? 0) > 0
  const hasDisk = (next.diskUsage?.length ?? 0) > 0
  const hasNet = (next.netIfaces?.length ?? 0) > 0
  if (next.error && !hasMem && !hasDisk && !hasNet) {
    return {
      ...prev,
      error: next.error,
      collectedAt: next.collectedAt || prev.collectedAt,
    }
  }
  return {
    ...prev,
    ...next,
    memUsedMb: hasMem ? next.memUsedMb : prev.memUsedMb,
    memTotalMb: hasMem ? next.memTotalMb : prev.memTotalMb,
    diskUsage: hasDisk ? next.diskUsage : prev.diskUsage,
    netIfaces: hasNet ? next.netIfaces : prev.netIfaces,
    uptime: next.uptime || prev.uptime,
    error: next.error || undefined,
  }
}

const memPct = computed(() => {
  if (!snap.value?.memTotalMb) return 0
  return Math.min(100, (snap.value.memUsedMb / snap.value.memTotalMb) * 100)
})

const disks = computed(() => snap.value?.diskUsage ?? [])
const nets = computed(() => snap.value?.netIfaces ?? [])

/** 默认优先选带 IP 的物理网卡（ens/eth…），避免 K8s 节点落到 kube-ipvs0。 */
function preferNetName(list: {name: string; ip?: string}[]): string {
  if (!list.length) return ''
  const phys = list.find((n) => /^(ens|enp|eno|eth|em|wlan|wlp|bond)/i.test(n.name) && n.ip)
  if (phys) return phys.name
  const anyPhys = list.find((n) => /^(ens|enp|eno|eth|em|wlan|wlp|bond)/i.test(n.name))
  if (anyPhys) return anyPhys.name
  const withIP = list.find((n) => n.ip)
  return withIP?.name || list[0].name
}

const hasCPU = computed(() => {
  if (!snap.value) return false
  return (
    (snap.value.memTotalMb ?? 0) > 0 ||
    (snap.value.diskUsage?.length ?? 0) > 0 ||
    (snap.value.netIfaces?.length ?? 0) > 0
  )
})

const selectedDisk = computed(() => {
  if (!disks.value.length) return null
  return disks.value.find((d) => d.mountPoint === diskKey.value) ?? disks.value[0]
})

const selectedNet = computed(() => {
  if (!nets.value.length) return null
  return nets.value.find((n) => n.name === netKey.value) ?? nets.value[0]
})

const menuItems = computed(() => {
  if (openMenu.value === 'disk') {
    return disks.value.map((d) => ({value: d.mountPoint, label: d.mountPoint, detail: `${d.usagePct.toFixed(0)}%`}))
  }
  if (openMenu.value === 'net') {
    return nets.value.map((n) => ({
      value: n.name,
      label: n.name, detail: n.ip || '无 IP',
    }))
  }
  return []
})

const menuActive = computed(() => (openMenu.value === 'disk' ? diskKey.value : netKey.value))

function closeMenu() {
  openMenu.value = null
}

async function toggleMenu(kind: 'disk' | 'net', ev: MouseEvent) {
  if (openMenu.value === kind) {
    closeMenu()
    return
  }
  const btn = ev.currentTarget as HTMLElement
  // 菜单 Teleport 到 body 且用绝对定位（未缩放坐标），按钮 rect 需折算为布局像素，
  // 避免 app-shell zoom 导致菜单整体偏移/错位。
  const rect = getElementRect(btn)
  openMenu.value = kind
  await nextTick()
  if (openMenu.value !== kind) return
  const menuRect = menuElement.value ? getElementRect(menuElement.value) : null
  const menuH = menuRect?.height || 220
  const menuWidth = menuRect?.width || 140
  const spaceBelow = window.innerHeight - rect.bottom
  const openUp = spaceBelow < menuH + 8
  menuStyle.value = {
    left: `${Math.max(8, Math.min(rect.left, window.innerWidth - menuWidth - 8))}px`,
    ...(openUp
      ? {bottom: `${window.innerHeight - rect.top + 4}px`, top: 'auto'}
      : {top: `${rect.bottom + 4}px`, bottom: 'auto'}),
  }
}

function pickItem(value: string) {
  if (openMenu.value === 'disk') diskKey.value = value
  if (openMenu.value === 'net') {
    netKey.value = value
    const ip = nets.value.find(n => n.name === value)?.ip
    if (ip) void copyText(ip).catch(() => {})
  }
  closeMenu()
}

function onDocPointer(e: PointerEvent) {
  if (!openMenu.value) return
  const t = e.target as HTMLElement | null
  if (t?.closest?.('[data-statusbar-menu]') || t?.closest?.('[data-statusbar-trigger]')) return
  closeMenu()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') closeMenu()
}

function bind(sessionId: string) {
  dispose?.()
  dispose = null
  snap.value = null
  closeMenu()
  if (!sessionId) return
  loadPrefs()
  dispose = onSysInfoSnapshot(sessionId, (s) => {
    snap.value = mergeSnap(snap.value, s)
    if (!diskKey.value && snap.value.diskUsage?.length) {
      diskKey.value = snap.value.diskUsage[0].mountPoint
    }
    const ifaces = snap.value.netIfaces ?? []
    if (ifaces.length) {
      const stillValid = netKey.value && ifaces.some((n) => n.name === netKey.value)
      if (!stillValid) {
        netKey.value = preferNetName(ifaces)
      }
    }
  })
  // 「登录后自动采集」开关关闭时，不启动远端采集（底部状态栏因此显示占位提示）。
  if (settings.sysInfoCollectEnabled) {
    void sysInfoService.start(sessionId).catch(() => {})
  }
}

watch(
  () => props.tab?.sessionId,
  (id) => bind(id || ''),
  {immediate: true},
)

watch([diskKey, netKey], () => savePrefs())

function onVis() {
  if (props.tab?.sessionId) void sysInfoService.setIdle(props.tab.sessionId, document.hidden)
}

onMounted(() => {
  document.addEventListener('visibilitychange', onVis)
  document.addEventListener('pointerdown', onDocPointer, true)
  document.addEventListener('keydown', onKey)
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVis)
  document.removeEventListener('pointerdown', onDocPointer, true)
  document.removeEventListener('keydown', onKey)
  dispose?.()
})

function fmtMbps(v: number): string {
  if (!v || v < 0.01) return '0'
  if (v < 1) return v.toFixed(2)
  if (v < 100) return v.toFixed(1)
  return Math.round(v).toString()
}
</script>

<template>
  <footer class="status-bar" data-ui="statusbar">
    <template v-if="tab?.status === 'connected' && tab.sessionId && settings.sysInfoCollectEnabled">
      <div class="metric">
        <span>CPU</span>
        <strong>{{ hasCPU ? `${(snap?.cpuUsage ?? 0).toFixed(0)}%` : '—' }}</strong>
        <span class="bar"><i :style="{width: hasCPU ? `${Math.min(100, snap?.cpuUsage ?? 0)}%` : '0'}"></i></span>
      </div>
      <div class="metric" :class="memPct > 85 ? 'warn' : ''">
        <span>MEM</span>
        <strong>{{ snap?.memTotalMb ? `${memPct.toFixed(0)}%` : '—' }}</strong>
        <span class="bar"><i :style="{width: memPct + '%'}"></i></span>
      </div>
      <div class="metric" :class="(selectedDisk?.usagePct ?? 0) > 75 ? 'warn' : ''">
        <span>DISK</span>
        <template v-if="disks.length">
          <button type="button" data-statusbar-trigger class="statusbar-pick" aria-label="选择分区" :aria-expanded="openMenu === 'disk'" @click="toggleMenu('disk', $event)">
            <span class="truncate max-w-16">{{ selectedDisk?.mountPoint }}</span>
            <Icon name="chevron-down" :size="10" />
          </button>
          <strong>{{ (selectedDisk?.usagePct ?? 0).toFixed(0) }}%</strong>
          <span class="bar"><i :style="{width: (selectedDisk?.usagePct ?? 0) + '%'}"></i></span>
        </template>
        <strong v-else>—</strong>
      </div>
      <div class="metric">
        <span>NET</span>
        <template v-if="nets.length">
          <button type="button" data-statusbar-trigger class="statusbar-pick" aria-label="选择网卡" :aria-expanded="openMenu === 'net'" @click="toggleMenu('net', $event)">
            <span class="truncate max-w-16">{{ selectedNet?.name }}</span>
            <Icon name="chevron-down" :size="10" />
          </button>
          <button v-if="selectedNet?.ip" type="button" class="statusbar-pick" title="点击复制 IP 地址" @click="copyText(selectedNet.ip).catch(() => {})">{{ selectedNet.ip }}</button>
          <span class="text-signal">↓{{ fmtMbps(selectedNet?.rxMbps ?? 0) }}Mb/s</span>
          <span class="text-copper">↑{{ fmtMbps(selectedNet?.txMbps ?? 0) }}Mb/s</span>
        </template>
        <strong v-else>—</strong>
      </div>
      <div class="flex-1"></div>
      <span v-if="snap?.error" class="text-warn truncate max-w-[14rem]" :title="snap.error">{{ snap.error }}</span>
    </template>
    <template v-else>
      <span v-if="settings.sysInfoCollectEnabled">连接服务器后显示 CPU / 内存 / 磁盘 / 网卡状态</span>
      <span v-else class="text-warn">「登录后自动采集」已关闭：登录服务器不执行任何采集命令</span>
    </template>
  </footer>

  <Teleport to="body">
    <div v-if="openMenu" ref="menuElement" data-statusbar-menu data-ui="context-menu" class="menu-pop statusbar-menu neo fixed z-[80]" :style="menuStyle">
      <button
        v-for="item in menuItems"
        :key="item.value"
        :title="openMenu === 'net' && item.detail !== '无 IP' ? '点击选择并复制 IP 地址' : item.label"
        type="button"
        :class="item.value === menuActive ? 'is-active' : ''"
        @click="pickItem(item.value)"
      >
        <span class="statusbar-menu-label" :title="item.label">{{ item.label }}</span>
        <span class="statusbar-menu-detail">{{ item.detail }}</span>
      </button>
    </div>
  </Teleport>
</template>

<style scoped>
.statusbar-menu {
  width: max-content;
  min-width: 140px;
  max-width: calc(100vw - 16px);
  max-height: min(220px, calc(100vh - 16px));
  overflow-y: auto;
}
.statusbar-menu button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  width: 100%;
}
.statusbar-menu-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.statusbar-menu-detail {
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  color: inherit;
  opacity: .75;
}

.statusbar-pick {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: 22px;
  padding: 0 6px;
  border-radius:var(--radius-small);
  background: var(--hover);
  box-shadow: inset 0 0 0 1px var(--line-strong);
  color: var(--foreground);
  font-family: var(--font-mono);
  font-size: 11px;
  cursor: pointer;
}
.statusbar-pick:focus-visible,
.statusbar-menu button:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: -2px;
}
.statusbar-pick:hover {
  background: var(--hover-strong);
}
</style>
