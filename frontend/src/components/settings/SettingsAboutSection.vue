<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from '../Icon.vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button} from '../ui'
import {frontendRuntimeStatus} from '../../utils/runtime-status'
import {sshService} from '../../services/ssh'
import {APP_COMMIT, APP_VERSION, buildDateLabel, buildTimeLabel, detectPlatform, platformLabel} from '../../version'
import {GetRuntimeStatus} from '../../../wailsjs/go/main/App'
import {ClipboardSetText} from '../../../wailsjs/runtime/runtime'
import appLogo from '../../assets/images/ding-logo.png'

const props = withDefaults(defineProps<{active?: boolean}>(), {active: true})
const autoRefresh = ref(false)

const platform = ref('')
const copied = ref(false)
let copiedTimer: number | undefined

const status = ref<Record<string, number>>()
const frontend = ref<ReturnType<typeof frontendRuntimeStatus>>()
const cpuPercent = ref<number>()
let cpuSample: {cpuMs: number; timeMs: number} | undefined
const bridge = ref('等待检测')
const sampledAt = ref('')
const refreshing = ref(false)
let slowTimer: number | undefined

function formatBytes(bytes: number | undefined) {
  return bytes === undefined ? '不可用' : `${(bytes / 1048576).toFixed(1)} MiB`
}

const runtimeRows = computed(() => [
  {label: '应用桥接', value: bridge.value},
  {label: '采样时间', value: sampledAt.value || '—'},
  {label: '已运行', value: status.value ? `${status.value.uptimeSec} 秒` : '—'},
  {label: 'Go 协程', value: status.value?.goroutines ?? '—'},
  {label: '主进程 CPU（单核为 100%）', value: cpuPercent.value === undefined ? (status.value?.processCPUms === undefined ? '不可用' : '再次刷新计算') : `${cpuPercent.value.toFixed(1)}%`},
  {label: '主进程当前常驻内存', value: formatBytes(status.value?.processRSSBytes)},
  {label: '主进程峰值常驻内存', value: formatBytes(status.value?.processPeakRSSBytes)},
  {label: 'Go 运行时内存（估算）', value: formatBytes(status.value?.goMemoryBytes)},
  {label: 'Go 堆内存', value: formatBytes(status.value?.heapBytes)},
  {label: 'WebView JS 堆内存', value: formatBytes(frontend.value?.jsHeapBytes)},
  {label: '前端定时器延迟（最近 / 峰值）', value: frontend.value ? `${frontend.value.lastDelayMs} / ${frontend.value.maxDelayMs} ms` : '—'},
  {label: '终端渲染（实际）', value: frontend.value ? `${frontend.value.terminalCount} 个终端 · WebGL ${frontend.value.webglCount} · DOM ${frontend.value.domCount}` : '—'},
  {label: '单终端待解析输出峰值', value: frontend.value ? `${frontend.value.peakOutputBytes.toLocaleString()} B` : '—'},
  {label: '终端待解析输出', value: frontend.value ? `${frontend.value.pendingOutputBytes.toLocaleString()} B` : '—'},
])

async function refreshStatus() {
  if (!props.active || refreshing.value) return
  refreshing.value = true
  bridge.value = '检测中'
  frontend.value = frontendRuntimeStatus()
  const started = performance.now()
  slowTimer = window.setTimeout(() => {
    bridge.value = '超过 3 秒未收到后端回复'
  }, 3000)
  try {
    status.value = await GetRuntimeStatus()
    const cpuMs = status.value.processCPUms
    const timeMs = performance.now()
    if (cpuMs !== undefined) {
      if (cpuSample) cpuPercent.value = 100 * (cpuMs - cpuSample.cpuMs) / (timeMs - cpuSample.timeMs)
      cpuSample = {cpuMs, timeMs}
    }
    frontend.value = frontendRuntimeStatus()
    bridge.value = `已响应（${Math.round(performance.now() - started)} ms）`
    sampledAt.value = new Date().toLocaleString()
  } catch {
    bridge.value = '读取后端状态失败'
  } finally {
    window.clearTimeout(slowTimer)
    refreshing.value = false
  }
}

const rows = computed(() => [
  {label: '版本标签', value: APP_VERSION || 'dev'},
  {label: '构建日期', value: buildDateLabel()},
  {label: '构建时间', value: buildTimeLabel()},
  {label: '代码提交', value: APP_COMMIT || '未知'},
  {label: '运行平台', value: platformLabel(platform.value || detectPlatform())},
])

async function copyAbout() {
  const text = ['ding-ssh', ...[...rows.value, ...runtimeRows.value].map((r) => `${r.label}: ${r.value}`)].join('\n')
  try {
    await ClipboardSetText(text)
    copied.value = true
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copied.value = false
    }, 1600)
  } catch {
    /* clipboard unavailable */
  }
}

watch(() => props.active, (active) => {
  if (!active) autoRefresh.value = false
})

watch(autoRefresh, (enabled, _, onCleanup) => {
  if (!enabled || !props.active) return
  void refreshStatus()
  const timer = window.setInterval(refreshStatus, 5000)
  onCleanup(() => window.clearInterval(timer))
})

onMounted(async () => {
  void refreshStatus()
  try {
    platform.value = await sshService.getPlatform()
  } catch {
    platform.value = ''
  }
})

onBeforeUnmount(() => {
  window.clearTimeout(copiedTimer)
  window.clearTimeout(slowTimer)
})
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="about" aria-labelledby="settings-about-title">
    <div>
      <h3 id="settings-about-title" class="text-[18px] font-semibold text-white tracking-tight">关于</h3>
      <p class="text-[13px] text-mist mt-1.5">版本信息与构建详情，反馈问题时请一并附上。</p>
    </div>

    <div class="neo">
      <div class="flex items-center gap-3 px-5 py-4 border-b border-slate-800/60">
        <div class="brand-mark">
          <img :src="appLogo" alt="" class="brand-logo" />
        </div>
        <div class="min-w-0">
          <div class="brand-name">ding<span>-ssh</span></div>
          <p class="text-xs text-slate-500 mt-0.5">Local-first SSH workstation</p>
        </div>
        <span class="chip ml-auto">{{ rows[0].value }}</span>
      </div>
      <div class="px-5 py-2">
        <div
          v-for="row in rows"
          :key="row.label"
          class="flex items-center justify-between gap-4 py-2.5 border-b border-slate-800/40 last:border-b-0"
        >
          <span class="text-xs text-slate-500 shrink-0">{{ row.label }}</span>
          <span class="font-mono text-xs text-slate-200 text-right break-all">{{ row.value }}</span>
        </div>
      </div>
    </div>

    <div class="neo px-5 py-3">
      <div class="flex items-center justify-between gap-4 py-1">
        <h4 class="text-sm font-medium text-slate-200">当前运行状态</h4>
        <Button variant="ghost" size="sm" :disabled="refreshing" @click="refreshStatus">刷新状态</Button>
      </div>
      <div class="flex items-center justify-between gap-4 border-b border-slate-800/40 py-3">
        <div class="min-w-0">
          <p id="runtime-auto-refresh-label" class="text-sm font-medium text-slate-200">自动刷新</p>
          <p class="mt-1 text-xs leading-relaxed text-slate-500">每 5 秒刷新一次，离开此页后自动关闭。</p>
        </div>
        <ToggleSwitch v-model="autoRefresh" aria-labelledby="runtime-auto-refresh-label" />
      </div>
      <p class="py-2 text-xs text-slate-500">主进程数据仅覆盖应用本体。WebView 子进程、GPU 和其他辅助进程单独计量，页面无法汇总。JS 堆仅在 WebView 提供指标时显示。</p>
      <div
        v-for="row in runtimeRows"
        :key="row.label"
        class="flex items-center justify-between gap-4 py-2.5 border-b border-slate-800/40 last:border-b-0"
      >
        <span class="text-xs text-slate-500">{{ row.label }}</span>
        <span class="font-mono text-xs text-slate-200 text-right">{{ row.value }}</span>
      </div>
    </div>

    <div class="flex items-center gap-3">
      <Button variant="ghost" size="sm" @click="copyAbout">
        <Icon name="copy" :size="14" />
        {{ copied ? '已复制' : '复制版本和运行状态' }}
      </Button>
      <p class="text-xs text-slate-500">构建时间取本次打包时的时间戳，可用于确认是否运行了最新版本。</p>
    </div>
  </section>
</template>
