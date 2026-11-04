<script lang="ts" setup>
import {computed, onMounted, ref} from 'vue'
import Icon from '../Icon.vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button, Select} from '../ui'
import {
  ClearLogs,
  ExportDiagnostics,
  GetLogInfo,
  OpenLogFolder,
  ReadLogTail,
  SetLogOptions,
} from '../../../wailsjs/go/main/App'
import {useSettingsStore} from '../../stores/settings'
import type {LogLevel} from '../../types'

const settings = useSettingsStore()
const saving = ref(false)
const logInfo = ref<Record<string, any> | null>(null)
const logTail = ref('')
const logTailLines = ref<number>(200)
const logTailLoading = ref(false)
const logBusy = ref(false)
const logMsg = ref('')
const logError = ref('')
const confirmClearLogs = ref(false)
const exportPath = ref('')

const LOG_LEVEL_OPTIONS = [
  {key: 'debug', label: 'debug'},
  {key: 'info', label: 'info'},
  {key: 'warn', label: 'warn'},
  {key: 'error', label: 'error'},
] as const
const LOG_TAIL_LINES = [100, 200, 500] as const
const LOG_RETENTION_FALLBACK = {maxBytes: 8 * 1024 * 1024, keepFiles: 20}

const logFiles = computed<Array<{name: string; size: number; modTime: number}>>(() =>
  Array.isArray(logInfo.value?.files) ? logInfo.value.files : [],
)
const logTotalSize = computed(() => Number(logInfo.value?.totalSize) || 0)
const logRetention = computed(() => ({
  maxBytes: Number(logInfo.value?.retention?.maxBytes) || LOG_RETENTION_FALLBACK.maxBytes,
  keepFiles: Number(logInfo.value?.retention?.keepFiles) || LOG_RETENTION_FALLBACK.keepFiles,
}))

function formatBytes(n: number): string {
  const v = Number(n) || 0
  if (v < 1024) return `${v} B`
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`
  if (v < 1024 * 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)} MB`
  return `${(v / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

async function refreshLogInfo() {
  try {
    logInfo.value = await GetLogInfo()
  } catch {
    logInfo.value = null
  }
}

/** 读一次日志尾部（只在打开页签 / 点刷新时调用，不做轮询）。 */
async function refreshLogTail() {
  logTailLoading.value = true
  logError.value = ''
  try {
    const r = await ReadLogTail(logTailLines.value)
    logTail.value = typeof r?.text === 'string' ? r.text : ''
  } catch (e) {
    logTail.value = ''
    logError.value = String(e)
  } finally {
    logTailLoading.value = false
  }
}

async function applyLogOptions(
  patch: Partial<{consoleEnabled: boolean; fileEnabled: boolean; level: LogLevel; apiLogEnabled: boolean}>,
) {
  saving.value = true
  logError.value = ''
  try {
    const mirror: Partial<{logToFile: boolean; logLevel: LogLevel; logApiCalls: boolean}> = {}
    if (patch.fileEnabled !== undefined) mirror.logToFile = patch.fileEnabled
    if (patch.level !== undefined) mirror.logLevel = patch.level
    if (patch.apiLogEnabled !== undefined) mirror.logApiCalls = patch.apiLogEnabled
    if (patch.consoleEnabled !== undefined) {
      await settings.setLogEnabled(patch.consoleEnabled)
    }
    if (Object.keys(mirror).length) await settings.setLog(mirror)
    const opts: Record<string, any> = {}
    if (patch.consoleEnabled !== undefined) opts.consoleEnabled = patch.consoleEnabled
    if (patch.fileEnabled !== undefined) opts.fileEnabled = patch.fileEnabled
    if (patch.level !== undefined) opts.level = patch.level
    if (patch.apiLogEnabled !== undefined) opts.apiLogEnabled = patch.apiLogEnabled
    if (Object.keys(opts).length) await SetLogOptions(opts)
    await refreshLogInfo()
  } catch (e) {
    logError.value = String(e)
  } finally {
    saving.value = false
  }
}

const onLogLevelChange = (value?: string | number) => {
  const v = String(value) as LogLevel
  if (settings.logLevel === v) return
  return applyLogOptions({level: v})
}

async function openLogFolder() {
  logError.value = ''
  try {
    await OpenLogFolder()
  } catch (e) {
    logError.value = String(e)
  }
}

async function clearLogs() {
  logError.value = ''
  logMsg.value = ''
  logBusy.value = true
  try {
    const r = await ClearLogs()
    confirmClearLogs.value = false
    const removed = Number(r?.removed) || 0
    logMsg.value = removed > 0 ? `已清理 ${removed} 个日志文件` : '暂无可清理的日志文件'
    await refreshLogInfo()
    await refreshLogTail()
  } catch (e) {
    logError.value = String(e)
  } finally {
    logBusy.value = false
  }
}

async function exportDiagnostics() {
  logError.value = ''
  logMsg.value = ''
  exportPath.value = ''
  logBusy.value = true
  try {
    const r = await ExportDiagnostics()
    exportPath.value = typeof r?.path === 'string' ? r.path : ''
    if (!exportPath.value) logMsg.value = '诊断包已生成'
  } catch (e) {
    logError.value = String(e)
  } finally {
    logBusy.value = false
  }
}

const onLogTailLinesChange = (value?: string | number) => {
  const n = Number(value)
  logTailLines.value = (LOG_TAIL_LINES as readonly number[]).includes(n) ? n : 200
  return refreshLogTail()
}

onMounted(() => {
  void refreshLogInfo()
  void refreshLogTail()
})
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="logs" aria-labelledby="settings-logs-title">
    <div>
      <h3 id="settings-logs-title" class="text-[18px] font-semibold text-white tracking-tight">日志</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">
        控制运行日志打到哪里、记多细；排障结束后可一键导出诊断包。日志只落在本机，token / 密码 / 私钥会自动打码。
      </p>
    </div>

    <div class="neo">
      <div class="flex items-center justify-between gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">运行日志（控制台）</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            开启后日志同时打印到 <span class="font-mono">wails dev</span> 的控制台（启动终端），开发调试时最直观；
            正式使用可以关掉它、改成写入日志文件。
          </p>
        </div>
        <ToggleSwitch :model-value="settings.logEnabled" :disabled="saving" @update:model-value="(v: boolean) => applyLogOptions({consoleEnabled: v})" />
      </div>
      <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">写入日志文件</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            开启后日志写进本机文件并自动轮转，关掉应用也能翻旧账；遇到问题时把文件（或诊断包）发给开发者即可定位。
          </p>
        </div>
        <ToggleSwitch :model-value="settings.logToFile" :disabled="saving" @update:model-value="(v: boolean) => applyLogOptions({fileEnabled: v})" />
      </div>
      <div class="px-5 py-3 border-t border-slate-800/60 flex items-start gap-3 text-xs">
        <span class="field-label shrink-0">日志目录</span>
        <span class="font-mono text-slate-300 break-all">{{ logInfo?.dir || '（暂不可用）' }}</span>
      </div>
      <div class="px-5 py-3 border-t border-slate-800/60 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs">
        <span class="field-label">当前总大小：<span class="font-mono text-slate-300">{{ formatBytes(logTotalSize) }}</span></span>
        <span class="field-label">文件数：<span class="font-mono text-slate-300">{{ logFiles.length }}</span></span>
        <Button variant="ghost" size="sm" class="ml-auto" :disabled="saving" @click="refreshLogInfo">刷新</Button>
      </div>
    </div>

    <div class="neo">
      <div class="grid grid-cols-[minmax(0,1fr)_9rem] items-center gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">日志级别</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            debug 会记录逐条调用细节（参数、耗时），信息最全但文件增长最快；日常用 info，复现问题时临时切到 debug 更省事。
          </p>
        </div>
        <Select :model-value="settings.logLevel" aria-label="日志级别" :disabled="saving" @update:model-value="onLogLevelChange">
          <option v-for="opt in LOG_LEVEL_OPTIONS" :key="opt.key" :value="opt.key">{{ opt.label }}</option>
        </Select>
      </div>
      <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">记录 MCP / 调试 API 调用</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            记录每次 HTTP / MCP 调用的方法、工具名、状态码与耗时，方便核对 AI / 自动化到底做了什么；调用密集时日志体积增长较快，排障完记得关掉。
          </p>
        </div>
        <ToggleSwitch :model-value="settings.logApiCalls" :disabled="saving" @update:model-value="(v: boolean) => applyLogOptions({apiLogEnabled: v})" />
      </div>
    </div>

    <div class="neo">
      <div class="px-5 py-4 border-b border-slate-800/60">
        <p class="text-sm font-medium text-slate-200">维护</p>
        <p class="text-xs text-slate-500 mt-1 leading-relaxed">打开目录手动查看，或清理 / 导出。</p>
      </div>
      <div class="px-5 py-4 flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="sm" :disabled="saving" @click="openLogFolder">
          <Icon name="folder" :size="14" />
          打开日志文件夹
        </Button>
        <template v-if="!confirmClearLogs">
          <Button variant="ghost" size="sm" :disabled="logBusy" @click="confirmClearLogs = true">清理日志</Button>
        </template>
        <template v-else>
          <Button variant="danger" size="sm" :loading="logBusy" @click="clearLogs">{{ logBusy ? '清理中…' : '确认清理' }}</Button>
          <Button variant="ghost" size="sm" :disabled="logBusy" @click="confirmClearLogs = false">取消</Button>
        </template>
        <Button variant="primary" size="sm" :loading="logBusy" data-testid="settings-export-diagnostics" @click="exportDiagnostics">
          <Icon name="package" :size="14" />
          {{ logBusy ? '处理中…' : '导出诊断包' }}
        </Button>
      </div>
      <div v-if="logError || logMsg || exportPath" class="px-5 pb-4 text-xs space-y-1">
        <p v-if="logError" class="text-rose-400 break-all" role="alert">{{ logError }}</p>
        <p v-else-if="logMsg" class="text-emerald-400 break-all" role="status">{{ logMsg }}</p>
        <p v-if="exportPath" class="text-emerald-400 break-all">
          已导出诊断包：<span class="font-mono">{{ exportPath }}</span>，可直接发给开发者。
        </p>
      </div>
    </div>

    <div class="neo">
      <div class="px-5 py-4 space-y-2">
        <p class="text-sm font-medium text-slate-200">保留策略</p>
        <p class="text-xs text-slate-500 leading-relaxed">
          单个日志文件写到 <span class="font-mono text-slate-300">{{ formatBytes(logRetention.maxBytes) }}</span> 自动轮转，
          只保留最近 <span class="font-mono text-slate-300">{{ logRetention.keepFiles }}</span> 个文件，更早的自动删除，不会越积越多占满磁盘。
        </p>
        <p v-if="!logInfo?.retention" class="text-[12px] text-slate-600">
          后端未返回保留策略，以上为默认值（单文件 8MB、保留最近 20 个）。
        </p>
        <p class="text-xs text-slate-500 leading-relaxed">
          敏感信息：日志中的 token / 密码 / 私钥会自动打码，固定开启、无法关闭；导出诊断包同样经过打码处理。
        </p>
      </div>
    </div>

    <p class="text-xs text-slate-500 leading-relaxed">AI / MCP 也能读取这些日志用于问题定位：工具 <span class="font-mono text-slate-300">list_logs</span>、<span class="font-mono text-slate-300">read_logs</span>（支持按级别/关键字过滤）、<span class="font-mono text-slate-300">export_diagnostics</span>，资源 <span class="font-mono text-slate-300">app://logs</span>、<span class="font-mono text-slate-300">app://logs/tail</span>。</p>

    <div class="neo">
      <div class="px-5 py-3 border-b border-slate-800/60 flex items-center justify-between gap-3">
        <span class="field-label">最近日志</span>
        <div class="shrink-0 flex items-center gap-2">
          <Select :model-value="logTailLines" aria-label="最近日志行数" class="w-28" :disabled="logTailLoading" @update:model-value="onLogTailLinesChange">
            <option v-for="n in LOG_TAIL_LINES" :key="n" :value="n">最近 {{ n }} 行</option>
          </Select>
          <Button variant="ghost" size="sm" :disabled="logTailLoading" @click="refreshLogTail">{{ logTailLoading ? '读取中…' : '刷新' }}</Button>
        </div>
      </div>
      <pre class="m-0 px-5 py-4 max-h-[420px] overflow-auto font-mono text-[12px] leading-relaxed text-slate-300 whitespace-pre-wrap break-all">{{ logTail || '暂无日志' }}</pre>
    </div>
  </section>
</template>
