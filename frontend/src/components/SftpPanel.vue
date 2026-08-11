<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {onSftpTransfer, onSftpSyncPath, onSftpDirUpdated, sshService} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import type {SFTPEntry, SessionTab} from '../types'

// 文件列表图标
const FolderIcon = '📁'
const FileIcon = '📄'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()

const entries = ref<SFTPEntry[]>([])
const loading = ref(false)
const error = ref('')

// 交互状态：选中 / 右键菜单 / 重命名 / 删除确认 / 新建文件夹 / 路径编辑
const selected = ref('')
const menu = ref<{x: number; y: number; entry: SFTPEntry} | null>(null)
const renaming = ref<{path: string; name: string} | null>(null)
const renameInput = ref('')
const confirmDeletePath = ref('')
const newFolderActive = ref(false)
const newFolderName = ref('')
const editingPath = ref(false)
const pathInput = ref('')

const renameInputEl = ref<HTMLInputElement>()
const newFolderInputEl = ref<HTMLInputElement>()
const pathInputEl = ref<HTMLInputElement>()

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

function joinPath(base: string, name: string): string {
  return base === '/' ? `/${name}` : `${base}/${name}`
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
  // Phase 2: SFTP → Shell 双向联动
  if (props.tab.sessionId) {
    sshService.syncSftpToTerminal(props.tab.sessionId, entry.path).catch(() => {})
  }
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

// ---- 右键菜单 ----
function openMenu(e: MouseEvent, entry: SFTPEntry) {
  selected.value = entry.path
  menu.value = {
    x: Math.min(e.clientX, window.innerWidth - 150),
    y: Math.min(e.clientY, window.innerHeight - 120),
    entry,
  }
}

function closeMenu() {
  menu.value = null
}

function menuEnter() {
  if (menu.value?.entry.isDir) enter(menu.value.entry)
  closeMenu()
}

function menuDownload() {
  if (menu.value && !menu.value.entry.isDir) void download(menu.value.entry)
  closeMenu()
}

// ---- 重命名 ----
function startRename(entry: SFTPEntry) {
  renaming.value = {path: entry.path, name: entry.name}
  renameInput.value = entry.name
  closeMenu()
  void nextTick(() => renameInputEl.value?.select())
}

async function doRename() {
  const target = renaming.value
  renaming.value = null
  if (!target) return
  const name = renameInput.value.trim()
  if (!name || name === target.name) return
  const newPath = joinPath(parentPath(target.path), name)
  try {
    await sshService.sftpRename(props.tab.sessionId, target.path, newPath)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 删除 ----
function requestDelete(entry: SFTPEntry) {
  confirmDeletePath.value = entry.path
  closeMenu()
}

async function doDelete() {
  const target = confirmDeletePath.value
  confirmDeletePath.value = ''
  if (!target) return
  try {
    await sshService.sftpRemove(props.tab.sessionId, target)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 新建文件夹 ----
function startNewFolder() {
  newFolderActive.value = true
  newFolderName.value = ''
  void nextTick(() => newFolderInputEl.value?.focus())
}

async function doNewFolder() {
  newFolderActive.value = false
  const name = newFolderName.value.trim()
  if (!name) return
  try {
    await sshService.sftpMkdir(props.tab.sessionId, joinPath(path.value, name))
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

// ---- 路径编辑 ----
function startPathEdit() {
  editingPath.value = true
  pathInput.value = path.value
  void nextTick(() => pathInputEl.value?.select())
}

function doPathEdit() {
  editingPath.value = false
  const p = pathInput.value.trim()
  if (!p || p === path.value) return
  go(p.startsWith('/') ? p : `/${p}`)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  menu.value = null
  renaming.value = null
  confirmDeletePath.value = ''
  newFolderActive.value = false
  editingPath.value = false
}

// ---- 双击：目录进入 / 文件下载 ----
function onDblClick(entry: SFTPEntry) {
  if (entry.isDir) enter(entry)
  else void download(entry)
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
  const files = await sshService.selectLocalFiles()
  if (!files.length) return
  for (const localPath of files) {
    const name = localPath.split('/').pop() || 'file'
    const remotePath = joinPath(path.value, name)
    await runTransfer('upload', name, () =>
      sshService.sftpUpload(props.tab.sessionId, localPath, remotePath),
    )
  }
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
  window.addEventListener('click', closeMenu)
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('scroll', closeMenu, true)
  // 监听 Shell→SFTP 目录同步事件
  if (props.tab.sessionId) {
    disposers.push(onSftpSyncPath(props.tab.sessionId, (newPath) => {
      if (newPath && newPath !== path.value) {
        go(newPath)
      }
    }))
    disposers.push(onSftpDirUpdated(props.tab.sessionId, (evt) => {
      // SWR 增量更新：只替换匹配路径的条目
      if (evt.path === path.value || evt.path === path.value + '/') {
        entries.value = evt.entries.sort((a: SFTPEntry, b: SFTPEntry) =>
          a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
        )
      }
    }))
  }
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
  window.removeEventListener('click', closeMenu)
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('scroll', closeMenu, true)
})
</script>

<template>
  <div class="w-72 shrink-0 border-l border-slate-700/60 bg-slate-900/70 backdrop-blur-md flex flex-col">
    <div class="flex items-center justify-between gap-2 px-3 py-2 border-b border-slate-800/60">
      <p class="text-xs font-semibold text-slate-200">SFTP</p>
      <div class="flex items-center gap-1">
        <button
          class="h-6 px-2.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-[11px]"
          title="在当前目录新建文件夹"
          aria-label="新建文件夹"
          @click="startNewFolder"
        >
          <Icon name="plus" :size="12" class="mr-0.5" /> 新建
        </button>
        <button
          class="h-6 px-2.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-[11px] font-medium"
          title="上传文件到当前目录"
          aria-label="上传文件"
          @click="upload"
        >
          <Icon name="upload" :size="12" class="mr-0.5" /> 上传
        </button>
        <button
          class="w-6 h-6 rounded text-slate-400 hover:bg-slate-800 hover:text-slate-200 text-xs"
          title="隐藏 SFTP 面板"
          aria-label="隐藏 SFTP 面板"
          @click="sessions.sftpVisible = false"
        >
          <Icon name="close" :size="12" />
        </button>
      </div>
    </div>

    <!-- 导航行：上一级 / 刷新 / 面包屑 -->
    <div class="flex items-center gap-1 px-2 py-1.5 border-b border-slate-800/60">
      <button
        class="w-7 h-7 shrink-0 rounded-md bg-slate-800/70 hover:bg-slate-700 text-slate-300 text-xs"
        title="上一级"
          aria-label="上级目录"
        @click="up"
      >
        <Icon name="up" :size="12" />
      </button>
      <button
        class="w-7 h-7 shrink-0 rounded-md bg-slate-800/70 hover:bg-slate-700 text-slate-300 text-xs"
        title="刷新"
          aria-label="刷新"
        @click="refresh"
      >
        <Icon name="refresh" :size="12" />
      </button>
      <div
        v-if="editingPath"
        class="flex-1 min-w-0 flex items-center px-1"
      >
        <input
          ref="pathInputEl"
          v-model="pathInput"
          class="w-full min-w-0 px-2 py-1 rounded-md bg-slate-800 border border-sky-500/60 text-slate-200 text-[11px] font-mono outline-none"
          spellcheck="false"
          @keyup.enter="doPathEdit"
          @keyup.esc="editingPath = false"
          @blur="doPathEdit"
        />
      </div>
      <div
        v-else
        class="flex-1 min-w-0 flex items-center gap-0.5 overflow-x-auto no-scrollbar px-1 text-[11px] text-slate-400 cursor-text"
        title="点击编辑完整路径"
        @click="startPathEdit"
      >
        <template v-for="(c, i) in crumbs" :key="c.path">
          <button class="shrink-0 hover:text-sky-400" @click="go(c.path)">{{ c.label }}</button>
          <span v-if="i < crumbs.length - 1" class="shrink-0 text-slate-600">/</span>
        </template>
        <span class="shrink-0 ml-auto pl-1 text-slate-600 group-hover:text-slate-400"><Icon name="settings" :size="10" /></span>
      </div>
    </div>

    <!-- 新建文件夹输入行 -->
    <div
      v-if="newFolderActive"
      class="flex items-center gap-2 px-3 py-1.5 border-b border-slate-800/60"
    >
      <span class="text-[11px] text-slate-400 shrink-0">名称</span>
      <input
        ref="newFolderInputEl"
        v-model="newFolderName"
        class="flex-1 min-w-0 px-2 py-1 rounded-md bg-slate-800 border border-sky-500/60 text-slate-200 text-[11px] outline-none"
        placeholder="新文件夹名称"
        @keyup.enter="doNewFolder"
        @keyup.esc="newFolderActive = false"
        @blur="doNewFolder"
      />
    </div>

    <div class="flex-1 overflow-y-auto px-1.5 py-1.5">
      <div v-if="!entries.length && !loading && !error" class="py-6 text-center text-xs text-slate-500">
        空目录
      </div>
      <div
        v-for="e in entries"
        :key="e.path"
        class="group flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer text-[12px] text-slate-300 transition-colors"
        :class="selected === e.path ? 'bg-sky-500/15' : 'hover:bg-slate-800/60'"
        :title="e.isDir ? '双击进入目录，右键更多操作' : '双击下载到本地，右键更多操作'"
        @click="selected = e.path"
        @dblclick="onDblClick(e)"
        @contextmenu.prevent="openMenu($event, e)"
      >
        <span class="shrink-0">{{ e.isDir ? FolderIcon : FileIcon }}</span>
        <template v-if="renaming?.path === e.path">
          <input
            ref="renameInputEl"
            v-model="renameInput"
            class="min-w-0 flex-1 px-1.5 py-0.5 rounded bg-slate-800 border border-sky-500/60 text-slate-200 text-[11px] outline-none"
            spellcheck="false"
            @click.stop
            @keyup.enter="doRename"
            @keyup.esc="renaming = null"
            @blur="doRename"
          />
        </template>
        <template v-else-if="confirmDeletePath === e.path">
          <span class="text-[11px] text-rose-300 truncate">删除「{{ e.name }}」？</span>
          <button
            class="shrink-0 px-2 py-0.5 rounded bg-rose-600/80 hover:bg-rose-500 text-white text-[10px]"
            @click.stop="doDelete"
          >
            确认
          </button>
          <button
            class="shrink-0 px-2 py-0.5 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-[10px]"
            @click.stop="confirmDeletePath = ''"
          >
            取消
          </button>
        </template>
        <template v-else>
          <span class="min-w-0 truncate">{{ e.name }}</span>
          <span class="ml-auto shrink-0 text-[10px] text-slate-500">
            {{ e.isDir ? '目录' : fmtSize(e.size) }}
          </span>
        </template>
        <button
          v-if="!e.isDir && renaming?.path !== e.path && confirmDeletePath !== e.path"
          class="shrink-0 opacity-0 group-hover:opacity-100 w-5 h-5 rounded bg-slate-700/70 hover:bg-sky-500/80 hover:text-slate-900 text-[10px] transition-opacity"
          title="下载到本地"
          aria-label="下载文件"
          @click.stop="download(e)"
        >
          <Icon name="download" :size="10" />
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

    <!-- 右键菜单 -->
    <Teleport to="body">
      <div
        v-if="menu"
        class="fixed z-50 min-w-[150px] rounded-lg border border-slate-700/60 bg-slate-900/95 py-1 shadow-2xl text-xs"
        :style="{left: menu.x + 'px', top: menu.y + 'px'}"
        @contextmenu.prevent
        @click.stop
      >
        <button
          v-if="menu.entry.isDir"
          class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800"
          @click="menuEnter"
        >
          进入目录
        </button>
        <button
          v-else
          class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800"
          @click="menuDownload"
        >
          下载到本地
        </button>
        <button
          class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800"
          @click="startRename(menu.entry)"
        >
          重命名
        </button>
        <button
          class="w-full text-left px-3 py-1.5 text-rose-300 hover:bg-rose-600/70"
          @click="requestDelete(menu.entry)"
        >
          删除
        </button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.no-scrollbar::-webkit-scrollbar {
  display: none;
}
</style>
