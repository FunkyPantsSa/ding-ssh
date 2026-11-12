<script setup lang="ts">
import {computed, ref} from 'vue'
import {useSessionsStore} from '../../stores/sessions'
import {sysInfoService} from '../../services/sysinfo'
import {useSettingsStore} from '../../stores/settings'
import type {ServerNode} from '../../types'
import {getServerSnapshot} from '../../services/serverSnapshot'
const props = defineProps<{node: ServerNode; status: string}>()
const snap = computed(() => getServerSnapshot(props.node))
const pinned = ref(false)
const refreshMessage = ref('')
async function refresh() {
 const sessions = useSessionsStore()
 const settings = useSettingsStore()
 const tab = sessions.tabs.find(t => t.node.id === props.node.id && t.node.host === props.node.host && t.node.port === props.node.port && t.status === 'connected')
 if (!tab || !settings.sysInfoCollectEnabled) { refreshMessage.value = '请先连接此服务器并开启自动采集'; return }
 try { await sysInfoService.stop(tab.sessionId); await sysInfoService.start(tab.sessionId); refreshMessage.value = '已请求刷新' } catch { refreshMessage.value = '刷新失败' }
}
const disks = computed(() => [...(snap.value?.diskUsage || [])].sort((a,b) => b.usagePct-a.usagePct))
const worst = computed(() => disks.value[0])
const local = computed(() => disks.value.filter(d => !d.network && !d.uncertain))
const total = computed(() => local.value.reduce((n,d) => n+(d.totalBytes === undefined ? d.totalGb : d.totalBytes / 1073741824),0))
const used = computed(() => local.value.reduce((n,d) => n+(d.usedBytes === undefined ? d.usedGb : d.usedBytes / 1073741824),0))
const size = (gb: number) => `${Number((gb >= 1024 ? gb/1024 : gb).toFixed(1))} ${gb >= 1024 ? 'TiB' : 'GiB'}`
</script>
<template>
 <div class="summary" :class="{stale: status !== 'connected'}">
  <span v-if="!snap" class="pending">配置待采集</span>
  <template v-else>
   <span class="tag secondary" :title="`${snap.osName || ''} ${snap.architecture || ''}`">{{ snap.osName || 'Linux' }}</span>
   <span v-if="snap.cpuCount" class="tag secondary">{{ snap.cpuCount }} 核</span>
   <span class="tag secondary">{{ size(snap.memTotalMb / 1024) }} 内存</span>
   <div class="disk" :class="{pinned}">
    <button class="tag config-trigger" aria-label="查看服务器配置信息" :aria-expanded="pinned" @click.stop="pinned = !pinned" @keydown.esc="pinned = false"> <span v-if="worst?.usagePct >= 85" :style="{color: worst.usagePct >= 95 ? '#ef4444' : '#f59e0b'}">●</span> <span class="expanded-label">磁盘 {{ size(used) }} / {{ size(total) }}</span><span class="collapsed-label">配置信息 ▾</span> <span v-if="worst?.usagePct >= 95">{{ worst.mountPoint }} {{ worst.usagePct }}%</span></button>
    <div class="details" @keydown.esc="pinned = false">
     <strong class="details-title">服务器配置信息</strong>
     <dl class="config-grid">
      <dt>系统</dt><dd>{{ snap.osName || '未采集' }}</dd>
      <dt>架构</dt><dd>{{ snap.architecture || '未采集' }}</dd>
      <dt>CPU</dt><dd>{{ snap.cpuCount ? `${snap.cpuCount} 核（逻辑 CPU）` : '未采集' }}</dd>
      <dt>内存</dt><dd>{{ snap.memTotalMb ? size(snap.memTotalMb / 1024) : '未采集' }}</dd>
     </dl>
     <strong>本地文件系统：已用 {{ size(used) }} / 总量 {{ size(total) }}</strong>
     <div v-for="d in disks" :key="d.mountPoint" class="partition" :title="`${d.device} ${d.fsType}`"><div>{{ d.mountPoint }} {{ d.network ? "（网络存储）" : d.uncertain ? "（共享池或特殊文件系统，不计入合计）" : "" }}　{{ size(d.usedBytes === undefined ? d.usedGb : d.usedBytes / 1073741824) }} / {{ size(d.totalBytes === undefined ? d.totalGb : d.totalBytes / 1073741824) }}　{{ d.usagePct }}%</div><progress :value="d.usagePct" max="100" /></div>
     <small>更新于 {{ new Date(snap.collectedAt).toLocaleString() }}<br>{{ snap.error ? '数据暂未更新' : '' }}</small><div v-if="snap.diskIncomplete">统计不完整</div><button class="tag" @click.stop="refresh">刷新配置</button>{{ refreshMessage }}
    </div>
   </div>
  </template>
 </div>
</template>
<style scoped>
.collapsed-label{display:none}.details-title{display:block;margin-bottom:12px}.config-grid{display:grid;grid-template-columns:48px minmax(0,1fr);gap:8px;margin:0 0 16px;padding-bottom:14px;border-bottom:1px solid var(--border)}.config-grid dt{color:var(--muted-foreground)}.config-grid dd{margin:0;overflow-wrap:anywhere}
.summary{flex:0 1 auto;display:flex;align-items:center;gap:6px;min-width:0;margin:0 16px}.stale{opacity:.65}.pending{font-size:11px;color:var(--muted-foreground)}.tag{font-size:11px;white-space:nowrap;border:1px solid var(--border);background:var(--hover);color:var(--foreground);border-radius:5px;padding:3px 7px;max-width:180px;overflow:hidden;text-overflow:ellipsis}.disk{position:relative;flex-shrink:0}.details{display:none;position:fixed;top:30%;left:30%;z-index:50;width:420px;max-height:360px;overflow:auto;background:var(--background);color:var(--foreground);border:1px solid var(--border);border-radius:8px;padding:16px;box-shadow:0 8px 24px #0003;font-size:12px}.disk:hover .details,.disk:focus-within .details,.pinned .details{display:block}.partition{margin:12px 0}progress{width:100%;height:4px;accent-color:var(--border-focus)}small{color:var(--muted-foreground)}@container(max-width:1100px){.secondary:first-child{display:none}.expanded-label{display:none}.collapsed-label{display:inline}}@container(max-width:950px){.secondary{display:none}}@media(max-width:750px){.summary{margin:0 4px}.tag{max-width:120px}.details{position:fixed;top:20%;left:5%;right:5%;width:auto}}
@container(max-width:750px){.summary{max-width:120px}.disk .tag{max-width:120px}}
@container(max-width:650px){.summary{margin:0 4px;flex-shrink:0}}
</style>
