<script lang="ts" setup>
import {computed, onBeforeUnmount, ref} from 'vue'
import Icon from './Icon.vue'
import {sshService} from '../services/ssh'
import {useTransfersStore} from '../stores/transfers'
import {useSessionsStore} from '../stores/sessions'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import type {TransferRecord} from '../types'

const sessions = useSessionsStore()
const transfers = useTransfersStore()

/** all = 全部；upload / download = 只看一个方向。 */
const filter = ref<'all' | 'upload' | 'download'>('all')
const error = ref('')
const copiedId = ref('')
let copiedTimer: number | undefined

const visible = computed(() => {
  if (filter.value === 'all') return transfers.records
  return transfers.records.filter((r) => r.direction === filter.value)
})

const uploadCount = computed(() => transfers.records.filter((r) => r.direction === 'upload').length)
const downloadCount = computed(() => transfers.records.filter((r) => r.direction === 'download').length)

function fmtSize(n: number): string {
  if (!n || n < 0) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

/** 速度文案：B/s → KB/s → MB/s。 */
function fmtSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return '—'
  if (bytesPerSec < 1024) return `${bytesPerSec.toFixed(0)} B/s`
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`
  return `${(bytesPerSec / 1024 / 1024).toFixed(2)} MB/s`
}

function fmtDuration(ms: number): string {
  if (!ms || ms < 0) return '—'
  if (ms < 1000) return `${Math.round(ms)} ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)} s`
  const m = Math.floor(s / 60)
  return `${m} 分 ${Math.round(s - m * 60)} 秒`
}

function fmtTime(ts: number): string {
  if (!ts) return '—'
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function statusMeta(r: TransferRecord): {text: string; cls: string} {
  if (r.status === 'error') return {text: '失败', cls: 'text-[var(--danger-500)]'}
  if (r.status === 'cancelled') return {text: '已取消', cls: 'text-[var(--warn-500)]'}
  return {text: '完成', cls: 'text-[var(--ok-500)]'}
}

/** 下载完成后在系统文件管理器中打开本地文件所在文件夹（Windows 会同时选中该文件）。 */
async function openFolder(r: TransferRecord) {
  error.value = ''
  const target = (r.localPath || '').trim()
  if (!target) {
    error.value = '该记录没有本地路径（历史数据可能不完整）'
    return
  }
  try {
    await sshService.revealLocalPath(target)
  } catch (e) {
    error.value = String(e)
  }
}

async function copyPath(text: string, id: string) {
  if (!text) return
  try {
    await ClipboardSetText(text)
    copiedId.value = id
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copiedId.value = ''
    }, 1600)
  } catch (e) {
    error.value = String(e)
  }
}

onBeforeUnmount(() => window.clearTimeout(copiedTimer))
</script>

<template>
  <div class="tool">
    <div class="tool-head">
      <div class="seg">
        <button @click="sessions.showRightPanel('sftp')">SFTP</button>
        <button @click="sessions.showRightPanel('sysinfo')">看板</button>
      </div>
      <button class="btn-icon btn-sm ml-auto" title="收起" @click="sessions.sftpVisible = false">
        <Icon name="close" :size="14" />
      </button>
    </div>

    <div class="flex items-center gap-1.5 p-3 inset-line-b">
      <span class="text-[12.5px] font-medium text-[var(--mist-100)]">传输记录</span>
      <span class="text-[11.5px] text-mist">
        {{ transfers.records.length ? `共 ${transfers.records.length} 条` : '暂无' }}
      </span>
      <button
        v-if="transfers.records.length"
        class="btn btn-ghost btn-sm ml-auto"
        title="清空全部传输记录"
        @click="transfers.clear()"
      >
        <Icon name="trash" :size="13" />
        清空
      </button>
      <button class="btn btn-ghost btn-sm" :class="transfers.records.length ? '' : 'ml-auto'" @click="sessions.showRightPanel('sftp')">
        返回
      </button>
    </div>

    <!-- 方向筛选 -->
    <div class="flex items-center gap-1 px-3 py-2 inset-line-b">
      <button class="btn btn-ghost btn-sm" :class="filter === 'all' ? 'text-signal' : ''" @click="filter = 'all'">
        全部 {{ transfers.records.length }}
      </button>
      <button class="btn btn-ghost btn-sm" :class="filter === 'upload' ? 'text-signal' : ''" @click="filter = 'upload'">
        ↑ 上传 {{ uploadCount }}
      </button>
      <button class="btn btn-ghost btn-sm" :class="filter === 'download' ? 'text-signal' : ''" @click="filter = 'download'">
        ↓ 下载 {{ downloadCount }}
      </button>
    </div>

    <div class="flex-1 overflow-y-auto px-2 py-1.5">
      <div v-if="!visible.length" class="py-10 text-center text-xs text-mist">
        {{ transfers.records.length ? '该方向暂无记录' : '还没有上传 / 下载记录，传输完成后会自动出现在这里。' }}
      </div>

      <div
        v-for="r in visible"
        :key="r.id"
        class="px-2 py-2 rounded-[6px] hover:bg-[var(--hover)] flex flex-col gap-1"
      >
        <div class="flex items-center gap-2 min-w-0">
          <span class="shrink-0 text-[12px]" :class="r.direction === 'upload' ? 'text-copper' : 'text-signal'">
            {{ r.direction === 'upload' ? '↑' : '↓' }}
          </span>
          <span class="flex-1 min-w-0 truncate text-[12.5px] text-[var(--mist-200)]" :title="r.name">{{ r.name }}</span>
          <span class="shrink-0 text-[11px] font-mono" :class="statusMeta(r).cls">{{ statusMeta(r).text }}</span>
        </div>

        <div class="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-mist font-mono">
          <span>{{ fmtSize(r.size || r.transferred) }}</span>
          <span>{{ fmtSpeed(r.avgBytesPerSec) }}</span>
          <span>{{ fmtDuration(r.durationMs) }}</span>
          <span>{{ fmtTime(r.finishedAt) }}</span>
        </div>

        <div v-if="r.error" class="text-[11px] text-[var(--danger-500)] break-all">{{ r.error }}</div>

        <div class="flex items-center gap-1.5 text-[11px] text-mist">
          <span v-if="r.serverName" class="shrink-0" :title="r.host">{{ r.serverName }}</span>
          <span class="min-w-0 flex-1 truncate font-mono" :title="r.direction === 'download' ? r.localPath : r.remotePath">
            {{ r.direction === 'download' ? r.localPath : r.remotePath }}
          </span>
        </div>

        <div class="flex items-center gap-1.5">
          <button
            v-if="r.direction === 'download' && r.localPath"
            class="btn btn-ghost btn-sm"
            title="在系统文件管理器中打开（Windows 会选中该文件）"
            @click="openFolder(r)"
          >
            <Icon name="folder" :size="13" />
            打开所在文件夹
          </button>
          <button
            class="btn btn-ghost btn-sm"
            :title="`复制路径：${r.direction === 'download' ? r.localPath : r.remotePath}`"
            @click="copyPath(r.direction === 'download' ? r.localPath : r.remotePath, r.id)"
          >
            <Icon :name="copiedId === r.id ? 'check' : 'copy'" :size="13" />
            复制路径
          </button>
          <button class="btn btn-ghost btn-sm ml-auto" title="从记录中移除" @click="transfers.remove(r.id)">
            <Icon name="close" :size="13" />
          </button>
        </div>
      </div>

      <p v-if="error" class="sticky bottom-0 px-2 py-2 text-[12px] text-danger break-all">{{ error }}</p>
    </div>
  </div>
</template>
