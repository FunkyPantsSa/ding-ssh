<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {onSysInfoSnapshot, sysInfoService} from '../services/sysinfo'
import {useSessionsStore} from '../stores/sessions'
import type {SessionTab, SysInfoSnapshot} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()

const snap = ref<SysInfoSnapshot | null>(null)
const error = ref('')
const history = ref<{t: number; cpu: number; memPct: number}[]>([])
const MAX_POINTS = 40
let dispose: (() => void) | null = null
let started = false

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
    if (empty) return
    const mp = s.memTotalMb ? (s.memUsedMb / s.memTotalMb) * 100 : 0
    history.value = [...history.value, {t: s.collectedAt, cpu: s.cpuUsage, memPct: mp}].slice(-MAX_POINTS)
  })
  try {
    await sysInfoService.start(props.tab.sessionId)
    started = true
  } catch (e) {
    error.value = String(e)
  }
}

async function stop() {
  dispose?.()
  dispose = null
  if (started && props.tab.sessionId) {
    try {
      await sysInfoService.stop(props.tab.sessionId)
    } catch {
      /* ignore */
    }
  }
  started = false
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
  <aside class="tool">
    <div class="tool-head">
      <div class="seg">
        <button @click="sessions.showRightPanel('sftp')">SFTP</button>
        <button class="active">看板</button>
      </div>
      <button class="btn-icon btn-sm ml-auto" title="收起" @click="sessions.sftpVisible = false">
        <Icon name="close" :size="14" />
      </button>
    </div>

    <div class="flex-1 min-h-0 overflow-y-auto p-4 flex flex-col gap-3">
      <div v-if="error && !snap?.cpuUsage && !snap?.memTotalMb" class="neo-flat p-4 text-center">
        <p class="text-xs text-warn">{{ error }}</p>
        <p class="text-[12px] text-mist mt-2">仅 Linux 主机支持；也可改用 SFTP 面板。</p>
      </div>

      <template v-else>
        <div class="sys-card neo-flat p-4">
          <h4 class="text-[12px] font-semibold tracking-widest uppercase text-mist mb-3">CPU</h4>
          <div class="text-[22px] font-semibold tracking-tight text-[var(--mist-100)] tabular-nums">
            {{ (snap?.cpuUsage ?? 0).toFixed(1) }}<small class="text-xs font-medium text-mist ml-1">%</small>
          </div>
          <svg viewBox="0 0 200 36" class="w-full h-9 mt-3" preserveAspectRatio="none">
            <defs>
              <linearGradient id="sparkGrad" x1="0" y1="0" x2="0" y2="1">
                <stop stop-color="#3ec4b4"/>
                <stop offset="1" stop-color="#3ec4b4" stop-opacity="0"/>
              </linearGradient>
            </defs>
            <path v-if="cpuFill" :d="cpuFill" fill="url(#sparkGrad)" opacity="0.35"/>
            <path v-if="cpuPoints" :d="cpuPoints" fill="none" stroke="#3ec4b4" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        </div>

        <div class="sys-card neo-flat p-4">
          <h4 class="text-[12px] font-semibold tracking-widest uppercase text-mist mb-3">Memory</h4>
          <div class="text-[22px] font-semibold tracking-tight text-[var(--mist-100)] tabular-nums">
            {{ snap?.memUsedMb ?? 0 }}<small class="text-xs font-medium text-mist ml-1">/ {{ snap?.memTotalMb ?? 0 }} MB</small>
          </div>
          <svg viewBox="0 0 200 36" class="w-full h-9 mt-3" preserveAspectRatio="none">
            <path v-if="memFill" :d="memFill" fill="rgba(201,122,74,0.2)"/>
            <path v-if="memPoints" :d="memPoints" fill="none" stroke="#e0925e" stroke-width="1.5" stroke-linecap="round"/>
          </svg>
        </div>

        <div class="sys-card neo-flat p-4">
          <h4 class="text-[12px] font-semibold tracking-widest uppercase text-mist mb-3">Disk</h4>
          <div v-if="!snap?.diskUsage?.length" class="text-[12px] text-mist">暂无数据</div>
          <div v-for="d in snap?.diskUsage ?? []" :key="d.mountPoint" class="mb-3 last:mb-0">
            <div class="flex justify-between text-[12px] mb-1">
              <span class="truncate font-mono">{{ d.mountPoint }}</span>
              <span class="text-mist shrink-0 ml-2">{{ d.usedGb }}/{{ d.totalGb }} GB</span>
            </div>
            <div class="text-[22px] font-semibold tracking-tight tabular-nums mb-2" :class="d.usagePct > 75 ? 'text-warn' : 'text-[var(--mist-100)]'">
              {{ d.usagePct.toFixed(0) }}<small class="text-xs font-medium text-mist ml-1">%</small>
            </div>
            <div class="prog" style="height:6px">
              <i :style="{width: Math.min(100, d.usagePct) + '%', background: d.usagePct > 75 ? 'linear-gradient(90deg,#d4a04a,#e0925e)' : undefined}"></i>
            </div>
          </div>
        </div>

        <div v-if="snap?.uptime" class="text-[12px] text-mist px-1">
          uptime · {{ snap.uptime }}
        </div>
        <p v-if="error" class="text-[12px] text-warn px-1">{{ error }}</p>
      </template>
    </div>
  </aside>
</template>
