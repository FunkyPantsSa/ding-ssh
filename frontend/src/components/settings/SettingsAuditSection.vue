<script lang="ts" setup>
import {t as $t} from '../../i18n'

import {t} from '../../i18n'

import {onMounted, ref} from 'vue'
import Icon from '../Icon.vue'
import SearchSelect from '../ui/select/SearchSelect.vue'
import {Button, Select} from '../ui'
import {GetCapabilities} from '../../../bindings/ding-ssh/internal/apphost/app'
import {useSettingsAudit} from '../../composables/useSettingsAudit'
import type {AuditRecordView, CapabilitiesView} from '../../types'

const capInfo = ref<CapabilitiesView | null>(null)
const auditSection = useSettingsAudit(capInfo)
const {
  loading: auditLoading,
  error: auditError,
  message: auditMsg,
  filter: auditFilter,
  cap: auditCap,
  limit: auditLimit,
  expanded: expandedAudit,
} = auditSection

const capOptions = auditSection.capOptions
const filteredAudit = auditSection.filtered

const refreshAudit = () => auditSection.refresh()
const undoAudit = (rec: AuditRecordView) => auditSection.undo(rec)
const formatAuditTime = (ts: number): string => {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
const AUDIT_SOURCE_LABEL: Record<string, string> = {mcp: 'MCP', http: 'HTTP', get ui() { return t("界面") }}
const auditSourceLabel = (source: string): string => AUDIT_SOURCE_LABEL[source] ?? source

onMounted(async () => {
  try {
    capInfo.value = (await GetCapabilities()) as CapabilitiesView
  } catch {
    capInfo.value = null
  }
  await auditSection.refresh()
  auditSection.subscribe()
})
</script>

<template>
  <section class="max-w-3xl space-y-6 fade-rise" data-section-content="audit" aria-labelledby="settings-audit-title">
    <div>
      <h3 id="settings-audit-title" class="text-[18px] font-semibold text-white tracking-tight">{{ $t('AI 记录') }}</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">{{ $t('调试接口与 MCP 的每次调用都会留一条记录（参数已脱敏，密码 / token / 私钥一律打码为') }} <span class="font-mono">***</span> {{ $t('）；每条记录同时通过') }} <span class="font-mono">debug.audit</span> {{ $t('事件实时推给本页。只保留最近') }} {{ auditLimit }} {{ $t('条以内的环形缓冲（上限 500 条）。') }}</p>
    </div>

    <div class="neo">
      <div class="px-5 py-4 flex flex-wrap items-center gap-2">
        <Button
          v-for="opt in [
            {key: 'all', label: $t('全部')},
            {key: 'write', label: $t('仅写操作')},
            {key: 'failed', label: $t('仅失败')},
          ]"
          :key="opt.key"
          size="sm"
          :variant="auditFilter === opt.key ? 'primary' : 'ghost'"
          :aria-pressed="auditFilter === opt.key"
          @click="auditFilter = opt.key as 'all' | 'write' | 'failed'"
        >
          {{ opt.label }}
        </Button>
        <SearchSelect v-model="auditCap" class="max-w-[16rem]" :aria-label="$t('按能力筛选')"
          :options="[{value: '', label: t('按能力筛选：全部')}, ...capOptions]" @update:model-value="refreshAudit" />
        <Select :model-value="auditLimit" class="max-w-[8rem]" :aria-label="$t('记录条数')" @update:model-value="(value) => { auditLimit = Number(value) || 50; refreshAudit() }">
          <option :value="20">{{ $t('20 条') }}</option>
          <option :value="50">{{ $t('50 条') }}</option>
          <option :value="200">{{ $t('200 条') }}</option>
        </Select>
        <Button variant="ghost" size="sm" :disabled="auditLoading" @click="refreshAudit">
          <Icon name="refresh" :size="14" />
          {{ auditLoading ? t('刷新中…') : t('刷新') }}
        </Button>
      </div>
      <div v-if="auditError || auditMsg" class="px-5 pb-4 text-xs space-y-1">
        <p v-if="auditError" class="text-rose-400 break-all" role="alert">{{ auditError }}</p>
        <p v-else-if="auditMsg" class="text-emerald-400 break-all" role="status">{{ auditMsg }}</p>
      </div>
    </div>

    <div class="neo">
      <div class="px-5 py-3 border-b border-slate-800/60 flex items-center justify-between">
        <span class="field-label">{{ $t('记录（') }} {{ filteredAudit.length }} {{ $t('条）') }}</span>
        <span class="text-[11px] text-slate-500">{{ $t('来源：MCP / HTTP / 界面') }}</span>
      </div>
      <p v-if="filteredAudit.length === 0" class="px-5 py-6 text-xs text-slate-500">{{ $t('暂无记录。开启调试模式并让 AI 调用一次工具后，这里会实时出现。') }}</p>
      <div
        v-for="rec in filteredAudit"
        :key="rec.id"
        class="px-5 py-3 border-b border-slate-800/60 text-xs space-y-1"
      >
        <div class="flex items-center gap-2 flex-wrap">
          <span class="font-mono text-slate-500">{{ formatAuditTime(rec.ts) }}</span>
          <span class="px-1.5 py-0.5 rounded bg-surface-raised text-slate-300">{{ auditSourceLabel(rec.source) }}</span>
          <span class="font-mono text-slate-200 break-all">{{ rec.tool }}</span>
          <span v-if="rec.cap" class="font-mono text-[11px] text-signal break-all">{{ rec.cap }}</span>
          <span class="text-slate-500">{{ rec.durationMs }}ms</span>
          <span v-if="rec.ok" class="text-emerald-400">{{ $t('成功') }}</span>
          <span v-else class="text-rose-400 break-all">{{ $t('失败：') }} {{ rec.error }}</span>
          <Button variant="ghost" size="sm" class="ml-auto" @click="expandedAudit = expandedAudit === rec.id ? '' : rec.id">
            {{ expandedAudit === rec.id ? t('收起') : t('参数') }}
          </Button>
          <Button v-if="rec.reversible" variant="ghost" size="sm" :title="rec.revertHint" @click="undoAudit(rec)">{{ $t('撤销') }}</Button>
          <Button v-else-if="rec.revertHint" variant="ghost" size="sm" disabled :title="rec.revertHint">{{ $t('撤销') }}</Button>
        </div>
        <pre
          v-if="expandedAudit === rec.id"
          class="mt-1 p-2 rounded bg-surface text-[11px] text-slate-300 whitespace-pre-wrap break-all"
        >{{ rec.args }}</pre>
      </div>
    </div>

    <div class="neo">
      <div class="px-5 py-4 text-xs text-slate-500 leading-relaxed">{{ $t('撤销说明：本波只对') }}<b>{{ $t('设置类改动') }}</b>{{ $t('（AI 调用') }} <span class="font-mono">update_settings</span> /
        <span class="font-mono">set_log_options</span> {{ $t('）提供「恢复为改动前值」，\n        快照只保留最近若干条；其余动作（删服务器、清日志、退出应用等）无法可靠回滚，\n        「撤销」按钮会置灰并给出原因，不会假装支持。') }}</div>
    </div>
  </section>
</template>
