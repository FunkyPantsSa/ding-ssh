<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {onSftpTransfer, sshService} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import type {SFTPEntry, SessionTab} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()

const entries = ref<SFTPEntry[]>([])
const loading = ref(false)
const error = ref('')

interface TransferItem {
  key: string
  direction: 'upload' | 'download'
  name: string
  transferred: number
  total: number
  done: boolean
  error: string
}
const transfers = ref<TransferItem[]>([])
const disposers: Array<() => void> = []

const path = computed({
  get: () => props.tab.sftpPath || '/',
  set: (v: string) => {
    props.tab.sftpPath = v
  },
})

const crumbs = computed(() => {
  const parts = path.value.split('/').filter(Boolean)
  const out: Array<{label: string; path: string}> = [{label: '/', path: '/'}]
  let cur = ''
  for (const p of parts) {
    cur += '/' + p
    out.push({label: p, path: cur})
  }
  return out
})

function parentPath(p: string): string {
  if (p === '/') return '/'
  const i = p.lastIndexOf('/')
  return i <= 0 ? '/' : p.slice(0, i)
}

async function load(dir: string) {
  if (!props.tab.sessionId) return
  loading.value = true
  error.value = ''
  try {
    const list = await sshService.sftpList(props.tab.sessionId, dir)
    entries.value = list.sort((a, b) =>
      a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
    )
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

function enter(entry: SFTPEntry) {
  if (!entry.isDir) return
  path.value = entry.path
  load(entry.path)
}

function go(dir: string) {
  path.value = dir
  load(dir)
}

function up() {
  go(parentPath(path.value))
}

function refresh() {
  load(path.value)
}

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function percent(t: TransferItem): number {
  if (!t.total) return 0
  return Math.min(100, Math.round((t.transferred / t.total) * 100))
}

function onTransfer(evt: {direction: 'upload' | 'download'; name: string; transferred: number; total: number; done: boolean; error?: string}) {
  const key = `${evt.direction}:${evt.name}`
  let t = transfers.value.find((x) => x.key === key)
  if (!t) {
    if (evt.done) return
    t = {key, direction: evt.direction, name: evt.name, transferred: 0, total: 0, done: false, error: ''}
    transfers.value.push(t)
  }
  t.transferred = evt.transferred
  t.total = evt.total
  if (evt.done) {
    t.done = true
    if (evt.error) t.error = evt.error
  }
}

async function runTransfer(direction: 'upload' | 'download', name: string, task: () => Promise<void>) {
  const key = `${direction}:${name}`
  if (transfers.value.some((t) => t.key === key && !t.done)) return
  const t: TransferItem = {key, direction, name, transferred: 0, total: 0, done: false, error: ''}
  transfers.value.push(t)
  try {
    await task()
  } catch (e) {
    t.error = String(e)
    t.done = true
  }
  // 等待后端最终进度事件后清理并刷新目录
  setTimeout(() => {
    const idx = transfers.value.findIndex((x) => x.key === key)
    if (idx >= 0 && !transfers.value[idx].error) transfers.value.splice(idx, 1)
    void refresh()
  }, 500)
}

async function upload() {
  const localPath = await sshService.selectLocalFile()
  if (!localPath) return
  const name = localPath.split('/').pop() || 'file'
  const remotePath = path.value === '/' ? `/${name}` : `${path.value}/${name}`
  await runTransfer('upload', name, () =>
    sshService.sftpUpload(props.tab.sessionId, localPath, remotePath),
  )
}

async function download(entry: SFTPEntry) {
  const localPath = await sshService.selectSavePath(entry.name)
  if (!localPath) return
  await runTransfer('download', entry.name, () =>
    sshService.sftpDownload(props.tab.sessionId, entry.path, localPath),
  )
}

function dismissTransfer(key: string) {
  transfers.value = transfers.value.filter((t) => t.key !== key)
}

async function cancelTransfer(t: TransferItem) {
  if (t.done) return
  try {
    await sshService.sftpCancelTransfer(props.tab.sessionId, t.direction, t.name)
  } catch {
    // 传输可能已结束，忽略取消失败
  }
}

onMounted(() => {
  if (props.tab.status === 'connected') void load(path.value)
  disposers.push(onSftpTransfer(props.tab.sessionId, onTransfer))
})

watch(
  () => props.tab.status,
  (status) => {
    if (status === 'connecting') {
      entries.value = []
      error.value = ''
    } else if (status === 'connected') {
      void load(path.value)
    }
  },
)

watch(
  () => props.tab.sessionId,
  (sid, old) => {
    if (sid && sid !== old) {
      path.value = '/'
      void load('/')
    }
  },
)

onBeforeUnmount(() => {
  disposers.forEach((d) => d())
})
</script>

<template>
  <div class="w-72 shrink-0 border-l border-slate-700/60 bg-slate-900/70 backdrop-blur-md flex flex-col">
    <div class="flex items-center justify-between gap-2 px-3 py-2 border-b border-slate-800/60">
      <p class="text-xs font-semibold text-slate-200">SFTP</p>
      <div class="flex items-center gap-1">
        <button
          class="h-6 px-2.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-[11px] font-medium"
          title="上传文件到当前目录"
          @click="upload"
        >
          ⬆ 上传
        </button>
        <button
          class="w-6 h-6 rounded text-slate-400 hover:bg-slate-800 hover:text-slate-200 text-xs"
          title="隐藏 SFTP 面板"
          @click="sessions.sftpVisible = false"
        >
          ✕
        </button>
      </div>
    </div>

    <!-- 导航行：上一级 / 刷新 / 面包屑 -->
    <div class="flex items-center gap-1 px-2 py-1.5 border-b border-slate-800/60">
      <button
        class="w-7 h-7 shrink-0 rounded-md bg-slate-800/70 hover:bg-slate-700 text-slate-300 text-xs"
        title="上一级"
        @click="up"
      >
        ↑
      </button>
      <button
        class="w-7 h-7 shrink-0 rounded-md bg-slate-800/70 hover:bg-slate-700 text-slate-300 text-xs"
        title="刷新"
        @click="refresh"
      >
        ⟳
      </button>
      <div class="flex-1 min-w-0 flex items-center gap-0.5 overflow-x-auto no-scrollbar px-1 text-[11px] text-slate-400">
        <template v-for="(c, i) in crumbs" :key="c.path">
          <button class="shrink-0 hover:text-sky-400" @click="go(c.path)">{{ c.label }}</button>
          <span v-if="i < crumbs.length - 1" class="shrink-0 text-slate-600">/</span>
        </template>
      </div>
    </div>

    <div class="flex-1 overflow-y-auto px-1.5 py-1.5">
      <div v-if="!entries.length && !loading && !error" class="py-6 text-center text-xs text-slate-500">
        空目录
      </div>
      <div
        v-for="e in entries"
        :key="e.path"
        class="group flex items-center gap-2 px-2 py-1.5 rounded-md hover:bg-slate-800/60 cursor-pointer text-[12px] text-slate-300"
        :title="e.isDir ? '进入目录' : e.name"
        @click="enter(e)"
      >
        <span class="shrink-0">{{ e.isDir ? '📁' : '📄' }}</span>
        <span class="min-w-0 truncate">{{ e.name }}</span>
        <span class="ml-auto shrink-0 text-[10px] text-slate-500">
          {{ e.isDir ? '目录' : fmtSize(e.size) }}
        </span>
        <button
          v-if="!e.isDir"
          class="shrink-0 opacity-0 group-hover:opacity-100 w-5 h-5 rounded bg-slate-700/70 hover:bg-sky-500/80 hover:text-slate-900 text-[10px] transition-opacity"
          title="下载到本地"
          @click.stop="download(e)"
        >
          ⬇
        </button>
      </div>

      <!-- 加载提示置底，不遮挡列表 -->
      <div
        v-if="loading"
        class="sticky bottom-0 flex items-center justify-center gap-2 py-2 text-[11px] text-slate-400 bg-slate-900/80"
      >
        <span class="w-3 h-3 border-2 border-sky-400 border-t-transparent rounded-full animate-spin"></span>
        加载中…
      </div>
      <div v-if="error" class="sticky bottom-0 px-2 py-2 text-[11px] text-rose-400 break-all bg-slate-900/80">
        {{ error }}
        <button class="ml-1 underline hover:text-sky-300" @click="refresh">重试</button>
      </div>
    </div>

    <!-- 传输进度（面板最底部） -->
    <div v-if="transfers.length" class="shrink-0 border-t border-slate-800/60 divide-y divide-slate-800/60">
      <div v-for="t in transfers" :key="t.key" class="px-3 py-2">
        <div class="flex items-center justify-between gap-2 text-[11px]">
          <span class="text-slate-300 truncate">
            {{ t.direction === 'upload' ? '上传' : '下载' }} · {{ t.name }}
          </span>
          <span class="flex items-center gap-2 shrink-0">
            <button
              v-if="!t.done && !t.error"
              class="px-2 py-0.5 rounded bg-rose-500/70 hover:bg-rose-500 text-white text-[10px]"
              title="取消传输"
              @click="cancelTransfer(t)"
            >
              取消
            </button>
            <span class="text-slate-500">{{ percent(t) }}%</span>
          </span>
        </div>
        <div class="mt-1 h-1.5 rounded-full bg-slate-800 overflow-hidden">
          <div class="h-full bg-sky-500 transition-all" :style="{width: percent(t) + '%'}"></div>
        </div>
        <div v-if="t.error" class="mt-1 flex items-start justify-between gap-2 text-[11px] text-rose-400 break-all">
          <span>{{ t.error }}</span>
          <button class="shrink-0 text-slate-500 hover:text-slate-300" @click="dismissTransfer(t.key)">✕</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.no-scrollbar::-webkit-scrollbar {
  display: none;
}
</style>
