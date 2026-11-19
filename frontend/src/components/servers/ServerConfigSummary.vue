<script setup lang="ts">
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {computed, nextTick, onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from '../Icon.vue'
import {useSessionsStore} from '../../stores/sessions'
import {onSysInfoSnapshot, sysInfoService} from '../../services/sysinfo'
import {useSettingsStore} from '../../stores/settings'
import type {ServerNode} from '../../types'
import {getServerSnapshot} from '../../services/serverSnapshot'
const props = defineProps<{node: ServerNode; status: string}>()
const snap = computed(() => getServerSnapshot(props.node))
const pinned = ref(false)
const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const panel = ref<HTMLElement>()
const refreshMessage = ref('')
const refreshing = ref(false)
const dropUp = ref(false)
const panelMaxHeight = ref<number>()
const panelStyle = computed(() => panelMaxHeight.value ? {maxHeight: `${panelMaxHeight.value}px`} : undefined)
let activeTrigger: HTMLElement | null = null
let stopWaiting: (() => void) | null = null
let refreshTimeout: ReturnType<typeof setTimeout> | null = null

/** 面板只由点击状态控制：聚焦、悬停或点击别处都不会把它留在页面上。 */
function close(restoreFocus = false) {
  if (!pinned.value) return
  pinned.value = false
  if (restoreFocus) (activeTrigger ?? trigger.value)?.focus()
}

function open(event?: MouseEvent) {
  if (!refreshing.value) refreshMessage.value = ''
  activeTrigger = (event?.currentTarget as HTMLElement | null) ?? trigger.value ?? null
  dropUp.value = false
  panelMaxHeight.value = undefined
  pinned.value = true
  void nextTick().then(positionPanel)
}

function toggle(event?: MouseEvent) {
  if (pinned.value) close()
  else open(event)
}

/** 浮层跟随所在行滚动；空间不够时翻到行上方，并按可视区域收窄高度。 */
function positionPanel() {
  const anchor = root.value
  const el = panel.value
  if (!anchor || !el) return
  const scroller = anchor.closest('.server-list-panel')
  const row = (anchor.closest('.server-row') ?? anchor) as HTMLElement
  const bounds = (scroller ?? document.documentElement).getBoundingClientRect()
  const rect = row.getBoundingClientRect()
  if (bounds.height < 1) return
  const gap = 8
  const below = bounds.bottom - rect.bottom - gap - 2
  const above = rect.top - bounds.top - gap - 2
  const desired = Math.min(el.scrollHeight || 320, 360)
  dropUp.value = below < desired && above > below
  panelMaxHeight.value = Math.max(160, Math.min(360, dropUp.value ? above : below))
}

/** 点击面板和触发按钮之外的地方时关闭。 */
function onDocumentPointerDown(event: PointerEvent) {
  if (!pinned.value) return
  const target = event.target as Node | null
  if (target && root.value?.contains(target)) return
  close()
}

/** Esc 关闭面板；只有焦点还在面板或触发按钮上时才处理，避免抢走终端等处的 Esc。 */
function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !pinned.value) return
  const target = event.target as Node | null
  if (!target || !root.value?.contains(target)) return
  close(true)
}

function onWindowResize() {
  if (pinned.value) positionPanel()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onWindowResize)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onWindowResize)
  stopRefreshWait()
})

function stopRefreshWait() {
  stopWaiting?.()
  stopWaiting = null
  if (refreshTimeout) {
    clearTimeout(refreshTimeout)
    refreshTimeout = null
  }
}

function finishRefresh(message: string) {
  stopRefreshWait()
  refreshing.value = false
  refreshMessage.value = message
}

async function refresh() {
  if (refreshing.value) return
  const sessions = useSessionsStore()
  const settings = useSettingsStore()
  const tab = sessions.tabs.find(t => t.node.id === props.node.id && t.node.host === props.node.host && t.node.port === props.node.port && t.status === 'connected')
  if (!tab || !settings.sysInfoCollectEnabled) { refreshMessage.value = t("请先连接此服务器并开启自动采集"); return }
  refreshing.value = true
  refreshMessage.value = ''
  stopRefreshWait()
  try {
    await sysInfoService.stop(tab.sessionId)
    // 先停旧采集再订阅，并忽略停止前排队的旧快照，避免误把旧数据当成刷新结果。
    const subscribedAt = Date.now()
    stopWaiting = onSysInfoSnapshot(tab.sessionId, snap => {
      if (!snap.collectedAt || snap.collectedAt < subscribedAt) return
      finishRefresh(snap.error ? t('刷新失败') : t('刷新完成'))
    })
    refreshTimeout = setTimeout(() => finishRefresh(t('刷新超时，请稍后重试')), 15000)
    await sysInfoService.start(tab.sessionId)
    if (refreshing.value) refreshMessage.value = t('已请求刷新')
  } catch {
    finishRefresh(t('刷新失败'))
  }
}
const disks = computed(() => [...(snap.value?.diskUsage || [])].sort((a,b) => b.usagePct-a.usagePct))
const worst = computed(() => disks.value[0])
const local = computed(() => disks.value.filter((d, i, all) => !d.network && !d.uncertain && (!d.device || all.findIndex(other => other.device === d.device) === i)))
const total = computed(() => local.value.reduce((n,d) => n+(d.totalBytes === undefined ? d.totalGb : d.totalBytes / 1073741824),0))
const used = computed(() => local.value.reduce((n,d) => n+(d.usedBytes === undefined ? d.usedGb : d.usedBytes / 1073741824),0))
const size = (gb: number) => `${Number((gb >= 1024 ? gb/1024 : gb).toFixed(1))} ${gb >= 1024 ? 'TiB' : 'GiB'}`
</script>
<template>
 <div ref="root" class="summary" :class="{stale: status !== 'connected', pinned}">
  <span v-if="!snap" class="pending">{{ $t('配置待采集') }}</span>
  <template v-else>
   <button type="button" class="tag secondary summary-trigger" data-testid="config-os" :title="`${snap.osName || ''} ${snap.architecture || ''}`" :aria-label="$t('查看服务器配置信息')" :aria-expanded="pinned" aria-haspopup="dialog" @click.stop="toggle($event)" @keydown.esc.stop="close(true)">{{ snap.osName || 'Linux' }}</button>
   <button v-if="snap.cpuCount" type="button" class="tag secondary summary-trigger" data-testid="config-cpu" :title="$t('查看服务器配置信息')" :aria-label="$t('查看服务器配置信息')" :aria-expanded="pinned" aria-haspopup="dialog" @click.stop="toggle($event)" @keydown.esc.stop="close(true)">{{ snap.cpuCount }} {{ $t('核') }}</button>
   <button type="button" class="tag secondary summary-trigger" data-testid="config-memory" :title="$t('查看服务器配置信息')" :aria-label="$t('查看服务器配置信息')" :aria-expanded="pinned" aria-haspopup="dialog" @click.stop="toggle($event)" @keydown.esc.stop="close(true)">{{ size(snap.memTotalMb / 1024) }} {{ $t('内存') }}</button>
   <div class="disk">
    <button ref="trigger" type="button" class="tag config-trigger summary-trigger" data-testid="config-disk" :title="$t('查看服务器配置信息')" :aria-label="$t('查看服务器配置信息')" :aria-expanded="pinned" aria-haspopup="dialog" @click.stop="toggle($event)" @keydown.esc.stop="close(true)"> <span v-if="worst?.usagePct >= 85" :style="{color: worst.usagePct >= 95 ? 'var(--destructive)' : 'var(--warning)'}">●</span> <span class="expanded-label">{{ $t('磁盘') }} {{ size(used) }} / {{ size(total) }}</span><span class="collapsed-label">{{ $t('配置信息 ▾') }}</span> <span v-if="worst?.usagePct >= 95">{{ worst.mountPoint }} {{ worst.usagePct }}%</span></button>
    <div ref="panel" class="details" :class="{'drop-up': dropUp}" :style="panelStyle" role="dialog" :aria-label="$t('服务器配置信息')" @keydown.esc.stop="close(true)">
     <div class="details-header">
      <strong class="details-title">{{ $t('服务器配置信息') }}</strong>
      <button type="button" class="details-close" :title="$t('关闭')" :aria-label="$t('关闭')" @click.stop="close(true)"><Icon name="close" :size="14" /></button>
     </div>
     <dl class="config-grid">
      <dt>{{ $t('系统') }}</dt><dd>{{ snap.osName || t('未采集') }}</dd>
      <dt>{{ $t('架构') }}</dt><dd>{{ snap.architecture || t('未采集') }}</dd>
      <dt>CPU</dt><dd>{{ snap.cpuCount ? t('{0} 核（逻辑 CPU）', [snap.cpuCount]) : t('未采集') }}</dd>
      <dt>{{ $t('内存') }}</dt><dd>{{ snap.memTotalMb ? size(snap.memTotalMb / 1024) : t('未采集') }}</dd>
     </dl>
     <strong>{{ $t('本地文件系统：已用') }} {{ size(used) }} {{ $t('/ 总量') }} {{ size(total) }}</strong>
     <div v-for="d in disks" :key="d.mountPoint" class="partition" :title="`${d.device} ${d.fsType}`"><div>{{ d.mountPoint }} {{ d.network ? t('（网络存储）') : d.uncertain ? t('（共享池或特殊文件系统，不计入合计）') : "" }}　{{ size(d.usedBytes === undefined ? d.usedGb : d.usedBytes / 1073741824) }} / {{ size(d.totalBytes === undefined ? d.totalGb : d.totalBytes / 1073741824) }}　{{ d.usagePct }}%</div><progress :value="d.usagePct" max="100" /></div>
     <small>{{ $t('更新于') }} {{ new Date(snap.collectedAt).toLocaleString() }}<br>{{ snap.error ? t('数据暂未更新') : '' }}</small><div v-if="snap.diskIncomplete">{{ $t('统计不完整') }}</div><button class="tag" data-testid="refresh-config" :disabled="refreshing" @click.stop="refresh">{{ $t('刷新配置') }}</button><span role="status">{{ refreshMessage }}</span>
    </div>
   </div>
  </template>
 </div>
</template>
<style scoped>
.collapsed-label{display:none}.details-header{display:flex;align-items:flex-start;justify-content:space-between;gap:var(--space-2)}.details-title{display:block;margin-bottom:var(--space-3)}.details-close{display:inline-flex;flex:none;align-items:center;justify-content:center;width:24px;height:24px;margin:-4px -4px 0 0;padding:0;border:1px solid transparent;border-radius:var(--radius-small);background:transparent;color:var(--muted-foreground);cursor:pointer}.details-close:hover{background:var(--hover);color:var(--foreground)}.details-close:focus-visible{outline:2px solid var(--border-focus);outline-offset:1px}.config-grid{display:grid;grid-template-columns:fit-content(40%) minmax(0,1fr);gap:var(--space-2);margin:0 0 16px;padding-bottom:14px;border-bottom:1px solid var(--border)}.config-grid dt{min-width:0;overflow-wrap:anywhere;color:var(--muted-foreground)}.config-grid dd{min-width:0;margin:0;overflow-wrap:anywhere}
.summary{flex:0 1 auto;display:flex;align-items:center;gap:6px;min-width:0;margin:0 16px}.stale{opacity:.65}.pending{font-size:var(--text-2xs);color:var(--muted-foreground)}.tag{font-size:var(--text-2xs);white-space:nowrap;border:1px solid var(--border);background:var(--hover);color:var(--foreground);border-radius:var(--radius-small);padding:3px 7px;max-width:180px;overflow:hidden;text-overflow:ellipsis}.summary-trigger{cursor:pointer}.summary-trigger:hover{border-color:var(--border-focus);background:var(--background)}.summary-trigger[aria-expanded="true"]{border-color:var(--border-focus);background:var(--background)}.disk{position:static;flex-shrink:0}.details{display:none;position:absolute;top:calc(100% + 8px);left:clamp(16px,30%,max(16px,calc(100% - 436px)));z-index:50;width:min(420px,calc(100vw - 32px));max-width:min(calc(100vw - 32px),calc(100% - 24px));max-height:min(360px,calc(70vh - 16px));overflow-wrap:anywhere;overflow:auto;background:var(--background);color:var(--foreground);border:1px solid var(--border);border-radius:var(--radius-control);padding:var(--space-4);box-shadow:var(--shadow-overlay);font-size:var(--text-xs)}.details.drop-up{top:auto;bottom:calc(100% + 8px)}.summary.pinned .details{display:block}.details button:disabled{opacity:.6;cursor:default}.partition{margin:var(--space-3) 0}progress{width:100%;height:4px;accent-color:var(--border-focus)}small{color:var(--muted-foreground)}@container(max-width:1100px){.secondary:first-child{display:none}.expanded-label{display:none}.collapsed-label{display:inline}}@container(max-width:950px){.secondary{display:none}}@media(max-width:750px){.summary{margin:0 4px}.tag{max-width:120px}.details{top:calc(100% + 8px);left:8px;right:8px;width:auto;max-width:none}.details.drop-up{top:auto;bottom:calc(100% + 8px)}}
@container(max-width:750px){.summary{max-width:120px}.disk .tag{max-width:120px}}
@container(max-width:650px){.summary{margin:0 4px;flex-shrink:0}}
</style>
