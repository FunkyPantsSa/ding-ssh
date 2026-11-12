<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {useSettingsStore} from '../stores/settings'
import {Button} from './ui'
import {onSysInfoSnapshot, sysInfoService} from '../services/sysinfo'
import {useSessionsStore} from '../stores/sessions'
import type {SessionTab, SysInfoSnapshot} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()
const cards = ['cpu', 'memory', 'disk', 'load', 'processes'] as const
type Card = typeof cards[number]
const cardLabels: Record<Card, string> = {cpu:'CPU', memory:'内存', disk:'分区与挂载点', load:'系统负载', processes:'进程'}
const order = ref<Card[]>(loadOrder())
const arranging = ref(false)
function loadOrder(): Card[] {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem('sysinfo-card-order') || 'null')
    if (Array.isArray(saved)) return [...new Set(saved.filter((v): v is Card => cards.includes(v))), ...cards].filter((v,i,a) => a.indexOf(v) === i)
  } catch { /* use default order */ }
  return [...cards]
}
function moveCard(card: Card, direction: number) {
  const next = [...order.value]
  const from = next.indexOf(card), to = from + direction
  if (to < 0 || to >= next.length) return
  ;[next[from], next[to]] = [next[to]!, next[from]!]
  order.value = next
  try { localStorage.setItem('sysinfo-card-order', JSON.stringify(next)) } catch { /* optional preference */ }
}
const refreshing = ref(false)
async function refresh() {
  if (refreshing.value || props.tab.status !== 'connected' || !settings.sysInfoCollectEnabled) return
  refreshing.value = true
  try { await sysInfoService.start(props.tab.sessionId) } catch (e) { error.value = String(e); refreshing.value = false }
}
const search = ref('')
const sort = ref<'cpu' | 'memory'>('cpu')
const processes = computed(() => [...(snap.value?.processes ?? [])]
  .filter(p => `${p.pid} ${p.user} ${p.command}`.toLowerCase().includes(search.value.toLowerCase()))
  .sort((a,b) => b[sort.value] - a[sort.value]))
const cpuSegments = computed(() => [
  {key:'us', label:'应用计算', color:'var(--chart-compute)'},
  {key:'sy', label:'系统开销', color:'var(--chart-system)'},
  {key:'wa', label:'I/O 等待', color:'var(--chart-io)'},
  {key:'st', label:'虚拟化等待', color:'var(--chart-steal)'},
  {key:'ni', label:'低优先级', color:'var(--chart-nice)'},
  {key:'hi', label:'硬中断', color:'var(--chart-hardirq)'},
  {key:'si', label:'软中断', color:'var(--chart-softirq)'},
  {key:'id', label:'空闲', color:'var(--chart-idle)'},
].map(s => ({...s, value:snap.value?.cpuDetail?.[s.key] ?? 0})))
const summary = computed(() => {
  if (error.value) return '采集异常，以下可能为最后一次有效数据'
  if (!snap.value?.topAvailable) return '等待有效的 top 数据'
  if ((snap.value.cpuDetail?.wa ?? 0) > 15) return 'I/O 等待较高，建议检查存储或其他 I/O'
  if ((snap.value.cpuDetail?.st ?? 0) > 10) return '虚拟化等待较高，宿主机资源可能紧张'
  if (memPct.value > 90) return '内存占用较高，请关注可用内存和 Swap'
  if (snap.value.cpuUsage > 80) return (snap.value.cpuDetail?.sy ?? 0) > (snap.value.cpuDetail?.us ?? 0) ? 'CPU 较忙，系统开销较高' : 'CPU 较忙，主要来自应用计算'
  return '当前资源占用平稳'
})
function capacity(bytes?: number, gb = 0) {
  const n = bytes ?? gb * 1024 ** 3
  return n >= 1024 ** 3 ? `${(n / 1024 ** 3).toFixed(1)} GiB` : `${(n / 1024 ** 2).toFixed(1)} MiB`
}


const snap = ref<SysInfoSnapshot | null>(null)
const error = ref('')
const history = ref<{t: number; cpu: number; memPct: number}[]>([])
const MAX_POINTS = 40
let dispose: (() => void) | null = null

const memPct = computed(() => {
  if (!snap.value?.memTotalMb) return 0
  return Math.min(100, (snap.value.memUsedMb / snap.value.memTotalMb) * 100)
})

const cpuPoints = computed(() => sparkline(history.value.map((h) => h.cpu)))
const memPoints = computed(() => sparkline(history.value.map((h) => h.memPct)))
const cpuFill = computed(() => sparkFill(history.value.map((h) => h.cpu)))
const memFill = computed(() => sparkFill(history.value.map((h) => h.memPct)))

function sparkline(values: number[]): string {
  if (!values.length) return ''
  const w = 200
  const h = 36
  const max = Math.max(100, ...values)
  return values
    .map((v, i) => {
      const x = values.length === 1 ? 0 : (i / (values.length - 1)) * w
      const y = h - (v / max) * (h - 6) - 3
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

function sparkFill(values: number[]): string {
  const line = sparkline(values)
  if (!line) return ''
  return `${line} L200 36 L0 36 Z`
}

async function start() {
  if (!props.tab.sessionId || props.tab.status !== 'connected') return
  error.value = ''
  dispose?.()
  dispose = onSysInfoSnapshot(props.tab.sessionId, (s) => {
    refreshing.value = false
    if (s.error) error.value = s.error
    else error.value = ''
    // 仅 error、无指标时保留上一帧，避免整页被空包清空
    const empty =
      !(s.memTotalMb > 0) && !(s.diskUsage?.length > 0) && !(s.netIfaces?.length > 0)
    if (s.error && empty && snap.value) {
      snap.value = {...snap.value, error: s.error, collectedAt: s.collectedAt}
      return
    }
    snap.value = s
    if (empty || s.error || !s.topAvailable) return
    const mp = s.memTotalMb ? (s.memUsedMb / s.memTotalMb) * 100 : 0
    history.value = [...history.value, {t: s.collectedAt, cpu: s.cpuUsage, memPct: mp}].slice(-MAX_POINTS)
  })
  try {
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

async function stop() {
  dispose?.()
  dispose = null
  refreshing.value = false
  // 采集器由会话共享；关闭看板只取消本组件监听，避免停止状态栏的数据源。
}

watch(
  () => [props.tab.sessionId, props.tab.status] as const,
  async ([id, status]) => {
    await stop()
    snap.value = null
    history.value = []
    if (id && status === 'connected') await start()
  },
)

onMounted(() => {
  void start()
  document.addEventListener('visibilitychange', onVis)
})

function onVis() {
  if (props.tab.sessionId) void sysInfoService.setIdle(props.tab.sessionId, document.hidden)
}

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVis)
  void stop()
})
</script>

<template>
  <aside class="tool status-panel" data-ui="context-panel">
    <div class="tool-head">
      <div class="seg">
        <button @click="sessions.showRightPanel('sftp')">SFTP</Button>
        <button class="active">运行状态</button>
      </div>
      <Button variant="ghost" size="icon" class="ml-auto" title="收起" aria-label="收起侧栏" @click="sessions.sftpVisible = false">
        <Icon name="close" :size="14" />
      </Button>
    </div>

    <div class="status-content flex-1 min-h-0 overflow-y-auto p-4">
      <div v-if="error && !snap?.cpuUsage && !snap?.memTotalMb" class="neo-flat p-4 text-center">
        <p class="text-xs text-warn">{{ error }}</p>
        <p class="text-[12px] text-mist mt-2">仅 Linux 主机支持；也可改用 SFTP 面板。</p>
      </div>

      <template v-else>
        <div class="status-summary neo-flat p-4 text-xs">
          <div class="flex flex-wrap gap-3 mb-3">
            <button :disabled="refreshing || tab.status !== 'connected' || !settings.sysInfoCollectEnabled" @click="refresh">{{ refreshing ? '正在采集…' : '立即刷新' }}</button>
            <button :aria-expanded="arranging" @click="arranging = !arranging">调整卡片顺序</button>
          </div>
          <div v-if="arranging" class="card-order mb-3">
            <div v-for="(card,i) in order" :key="card" class="flex items-center gap-3 py-1">
              <span class="flex-1">{{ cardLabels[card] }}</span>
              <button :disabled="i===0" :aria-label="`上移${cardLabels[card]}`" @click="moveCard(card,-1)">上移</button>
              <button :disabled="i===order.length-1" :aria-label="`下移${cardLabels[card]}`" @click="moveCard(card,1)">下移</button>
            </div>
          </div>
          <p class="font-semibold">{{ summary }}</p>
          <p class="text-mist mt-2">来源：top · 磁盘：df · {{ snap?.collectedAt ? new Date(snap.collectedAt).toLocaleTimeString() : '等待采集' }}</p>
          <p class="text-mist mt-1">{{ settings.sysInfoRealtimeEnabled ? '前台约 3 秒刷新，后台约 10 秒' : '当前为单次快照' }}</p>
          <button class="mt-2 underline" @click="settings.setSysInfoRealtimeEnabled(!settings.sysInfoRealtimeEnabled)">{{ settings.sysInfoRealtimeEnabled ? '暂停自动刷新' : '开启自动刷新' }}</button>
          <p v-if="!settings.sysInfoCollectEnabled" class="text-warn mt-2">系统信息采集已关闭，请在设置中开启。</p>
          <p v-if="tab.status !== 'connected'" class="text-warn mt-2">连接已断开，当前为最后一次快照。</p>
        </div>
        <div class="sys-card neo-flat p-4 resource-card" :style="{order: order.indexOf('cpu')}">
          <h4 class="text-[12px] font-semibold tracking-widest uppercase text-mist mb-3">CPU</h4>
          <div class="text-[22px] font-semibold tracking-tight text-[var(--foreground)] tabular-nums">
            {{ snap?.topAvailable ? snap.cpuUsage.toFixed(1) : '—' }}<small class="text-xs font-medium text-mist ml-1">%</small>
          </div>
          <div v-if="snap?.topAvailable" class="mt-3">
            <div class="flex h-3 overflow-hidden rounded"><span v-for="s in cpuSegments" :key="s.key" :title="`${s.label} ${s.value}%`" :style="{width:s.value+'%', background:s.color}"></span></div>
            <div class="grid grid-cols-2 gap-2 mt-3 text-xs"><span v-for="s in cpuSegments" :key="s.key"><i class="inline-block w-2 h-2 mr-1 rounded" :style="{background:s.color}"></i>{{ s.label }} {{ s.value.toFixed(1) }}%</span></div>
            <p class="text-xs text-mist mt-2">I/O 等待并非磁盘使用率；CPU 总占用包含 I/O 等待。</p>
          </div>
          <svg viewBox="0 0 200 36" class="w-full h-9 mt-3" preserveAspectRatio="none">
            <defs>
              <linearGradient id="sparkGrad" x1="0" y1="0" x2="0" y2="1">
                <stop stop-color="var(--primary-hover)"/>
                <stop offset="1" stop-color="var(--primary-hover)" stop-opacity="0"/>
              </linearGradient>
            </defs>
            <path v-if="cpuFill" :d="cpuFill" fill="url(#sparkGrad)" opacity="0.35"/>
            <path v-if="cpuPoints" :d="cpuPoints" fill="none" stroke="var(--primary-hover)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </div>

        <div class="sys-card neo-flat p-4 resource-card" :style="{order: order.indexOf('memory')}">
          <h4 class="text-[12px] font-semibold tracking-widest uppercase text-mist mb-3">内存</h4>
          <div class="text-[22px] font-semibold tracking-tight text-[var(--foreground)] tabular-nums">
            {{ snap?.topAvailable ? snap.memUsedMb : '—' }}<small class="text-xs font-medium text-mist ml-1">/ {{ snap?.topAvailable ? snap.memTotalMb : '—' }} MiB</small>
            <small
              v-if="snap?.memTotalMb"
              class="text-xs font-medium ml-1"
              :class="memPct > 85 ? 'text-warn' : 'text-copper'"
            >({{ memPct.toFixed(0) }}%)</small>
          </div>
          <p class="text-xs text-mist mt-2">可用 {{ snap?.memAvailableMb?.toFixed(0) ?? '—' }} MiB · Swap {{ snap?.swapUsedMb?.toFixed(0) ?? '—' }} / {{ snap?.swapTotalMb?.toFixed(0) ?? '—' }} MiB</p>
          <svg viewBox="0 0 200 36" class="w-full h-9 mt-3" preserveAspectRatio="none">
            <path v-if="memFill" :d="memFill" fill="var(--secondary-glow)"/>
            <path v-if="memPoints" :d="memPoints" fill="none" stroke="var(--secondary-hover)" stroke-width="1.5" stroke-linecap="round"/>
          </svg>
        </div>

        <div class="sys-card neo-flat p-4 resource-card" :style="{order: order.indexOf('disk')}">
          <h4 class="text-[12px] font-semibold tracking-widest uppercase text-mist mb-3">分区与挂载点</h4>
          <p v-if="snap?.diskIncomplete" class="text-xs text-warn mb-2">部分文件系统容量不可用或属于共享存储池，请按挂载点查看。</p>
          <div v-if="!snap?.diskUsage?.length" class="text-[12px] text-mist">暂无数据</div>
          <div v-for="d in snap?.diskUsage ?? []" :key="d.mountPoint" class="mb-3 last:mb-0">
            <div class="flex justify-between text-[12px] mb-1">
              <span class="truncate font-mono">{{ d.mountPoint }}</span>
              <span class="text-mist shrink-0 ml-2">{{ capacity(d.usedBytes, d.usedGb) }} / {{ capacity(d.totalBytes, d.totalGb) }}</span>
            </div>
            <div class="text-[22px] font-semibold tracking-tight tabular-nums mb-2" :class="d.usagePct > 75 ? 'text-warn' : 'text-[var(--foreground)]'">
              {{ d.usagePct.toFixed(0) }}<small class="text-xs font-medium text-mist ml-1">%</small>
            </div>
            <p class="text-xs text-mist mb-2 break-all">{{ d.device }} · {{ d.fsType }}{{ d.network ? ' · 网络文件系统' : '' }}</p>
            <div class="prog" style="height:6px">
              <i :style="{width: Math.min(100, d.usagePct) + '%', background: d.usagePct > 75 ? 'var(--warning)' : undefined}"></i>
            </div>
          </div>
        </div>

        <div class="sys-card neo-flat p-4 resource-card" :style="{order: order.indexOf('load')}">
          <h4 class="text-xs font-semibold mb-3">系统负载</h4>
          <div class="flex justify-between"><span v-for="(v,i) in snap?.load ?? []" :key="i" class="text-center"><strong>{{ v.toFixed(2) }}</strong><small class="block text-mist">{{ [1,5,15][i] }} 分钟</small></span></div>
          <p class="text-xs text-mist mt-2">{{ snap?.cpuCount ?? '—' }} 个逻辑核心 · 负载包含运行及不可中断等待任务，不是 CPU 百分比。</p>
        </div>
        <div class="sys-card neo-flat p-4 process-card" :style="{order: order.indexOf('processes')}">
          <h4 class="text-xs font-semibold mb-3">进程 · {{ snap?.processes?.length ?? 0 }}</h4>
          <input v-model="search" aria-label="搜索进程" placeholder="搜索 PID、用户或命令" class="w-full bg-transparent border rounded px-2 py-1 text-xs mb-3" />
          <div class="process-scroll" tabindex="0" aria-label="进程表，可横向滚动"><table class="process-table text-xs"><thead><tr><th>PID / 用户</th><th><button @click="sort='cpu'">CPU % {{ sort === 'cpu' ? '↓' : '' }}</button></th><th><button @click="sort='memory'">内存 % {{ sort === 'memory' ? '↓' : '' }}</button></th><th>驻留内存</th><th>状态</th><th>累计 CPU 时间</th><th>命令</th></tr></thead>
          <tbody><tr v-for="p in processes" :key="p.pid"><td class="pr-3 py-2">{{ p.pid }}<small class="block text-mist">{{ p.user }}</small></td><td class="pr-3">{{ p.cpu.toFixed(1) }}</td><td class="pr-3">{{ p.memory.toFixed(1) }}</td><td class="pr-3">{{ p.resident }}</td><td class="pr-3">{{ p.state }}</td><td class="pr-3">{{ p.time }}</td><td><details><summary class="max-w-48 truncate cursor-pointer" :title="p.command">{{ p.command }}</summary><p class="whitespace-normal max-w-96 break-all">{{ p.command }}</p></details></td></tr></tbody></table></div>
          <p v-if="!processes.length" class="text-xs text-mist mt-2">暂无匹配进程</p>
          <p class="text-xs text-mist mt-3">进程 CPU 可超过 100%；驻留内存沿用 top 单位，无后缀时为 KiB。状态 R 运行、S 休眠、D 等待、Z 僵尸。</p>
        </div>
        <div v-if="snap?.uptime" class="status-footer text-[12px] text-mist px-1">
          uptime · {{ snap.uptime }}
        </div>
        <p v-if="error" class="status-footer text-[12px] text-warn px-1">{{ error }}</p>
      </template>
    </div>
  </aside>
</template>

<style scoped>
.status-panel { min-width: 0; }
.status-content { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 250px), 1fr)); grid-auto-rows: max-content; gap: 12px; align-content: start; }
.status-summary { grid-column: 1 / -1; order: -1; }
.sys-card { min-width: 0; align-self: start; }
.resource-card { min-height: 190px; height: auto; }
.resource-card svg { height: 36px; width: 100%; margin-top: 12px; }
.process-card { grid-column: 1 / -1; }
.status-footer { grid-column: 1 / -1; order: 100; }
.process-scroll { max-width: 100%; overflow-x: auto; max-height: 520px; }
.process-table { width: 100%; min-width: 780px; border-collapse: separate; border-spacing: 0; text-align: left; white-space: nowrap; }
.process-table th { position: sticky; top: 0; background: var(--background); z-index: 1; font-weight: 600; }
.process-table th, .process-table td { padding: var(--space-2) var(--space-3); border-bottom: 1px solid var(--border); }
.process-table th:first-child, .process-table td:first-child { padding-left: 0; }
.process-table th button { white-space: nowrap; }
.process-table tbody tr:hover { background: var(--secondary-glow); }
button:disabled { opacity: .45; cursor: default; }
</style>
