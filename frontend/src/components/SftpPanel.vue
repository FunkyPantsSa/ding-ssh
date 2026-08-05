<script lang="ts" setup>
import {computed, onMounted, ref, watch} from 'vue'
import {sshService} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import type {SFTPEntry, SessionTab} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()

const entries = ref<SFTPEntry[]>([])
const loading = ref(false)
const error = ref('')

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
    entries.value = []
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

onMounted(() => {
  if (props.tab.status === 'connected') void load(path.value)
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
</script>

<template>
  <div class="w-72 shrink-0 border-l border-slate-700/60 bg-slate-900/70 backdrop-blur-md flex flex-col">
    <div class="flex items-center justify-between px-3 py-2 border-b border-slate-800/60">
      <p class="text-xs font-semibold text-slate-200">SFTP</p>
      <button
        class="w-6 h-6 rounded text-slate-400 hover:bg-slate-800 hover:text-slate-200 text-xs"
        title="隐藏 SFTP 面板"
        @click="sessions.sftpVisible = false"
      >
        ✕
      </button>
    </div>

    <div class="flex items-center gap-1 px-2 py-1.5 border-b border-slate-800/60">
      <button
        class="w-7 h-7 rounded-md bg-slate-800/70 hover:bg-slate-700 text-slate-300 text-xs"
        title="上一级"
        @click="up"
      >
        ↑
      </button>
      <button
        class="w-7 h-7 rounded-md bg-slate-800/70 hover:bg-slate-700 text-slate-300 text-xs"
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
      <div v-if="loading" class="py-6 text-center text-xs text-slate-500">加载中…</div>
      <div v-else-if="error" class="px-2 py-4 text-center">
        <p class="text-xs text-rose-400 break-all">{{ error }}</p>
        <button class="mt-2 px-3 py-1 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs" @click="refresh">
          重试
        </button>
      </div>
      <div v-else-if="!entries.length" class="py-6 text-center text-xs text-slate-500">空目录</div>
      <div
        v-for="e in entries"
        :key="e.path"
        class="flex items-center gap-2 px-2 py-1.5 rounded-md hover:bg-slate-800/60 cursor-pointer text-[12px] text-slate-300"
        :title="e.isDir ? '进入目录' : e.name"
        @click="enter(e)"
      >
        <span class="shrink-0">{{ e.isDir ? '📁' : '📄' }}</span>
        <span class="min-w-0 truncate">{{ e.name }}</span>
        <span class="ml-auto shrink-0 text-[10px] text-slate-500">
          {{ e.isDir ? '目录' : fmtSize(e.size) }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.no-scrollbar::-webkit-scrollbar {
  display: none;
}
</style>
