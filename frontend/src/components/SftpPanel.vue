<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {onSftpTransfer, onSftpDirUpdated, sshService} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {OnFileDrop, OnFileDropOff} from '../../wailsjs/runtime/runtime'
import type {SFTPEntry, SessionTab} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()

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
  // SFTP → 终端目录同步（受配置项控制，默认开启）
  if (settings.sftpToTerminalSync && props.tab.sessionId) {
    sshService.syncSftpToTerminal(props.tab.sessionId, entry.path).catch(() => {})
  }
}

function go(dir: string) {
  path.value = dir
}

// 终端写入 tab.sftpPath 后，由 watch 加载目录；失败不打断用户
async function loadFromSync(dir: string) {
  if (!props.tab.sessionId || props.tab.status !== 'connected') return
  try {
    const list = await sshService.sftpList(props.tab.sessionId, dir)
    entries.value = list.sort((a, b) =>
      a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
    )
    error.value = ''
    console.info('[sftp-sync] 面板已切换', dir, '条目', list.length)
  } catch (e) {
    console.warn('[sftp-sync] 列出失败，保持当前列表', dir, e)
  }
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
  if (!t.total) return t.done && !t.error ? 100 : 0
  return Math.min(100, Math.round((t.transferred / t.total) * 100))
}

function onTransfer(evt: {direction: 'upload' | 'download'; name: string; transferred: number; total: number; done: boolean; error?: string}) {
  const key = `${evt.direction}:${evt.name}`
  let t = transfers.value.find((x) => x.key === key)
  if (!t) {
    if (evt.done && !evt.error) return
    t = {key, direction: evt.direction, name: evt.name, transferred: 0, total: 0, done: false, error: ''}
    transfers.value.push(t)
  }
  if (!evt.done && evt.transferred < t.transferred) return
  t.transferred = evt.transferred
  t.total = evt.total || t.total
  if (evt.done) {
    t.done = true
    if (t.total > 0) t.transferred = Math.max(t.transferred, t.total)
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
    if (!t.error) {
      t.done = true
      if (t.total > 0) t.transferred = t.total
    }
  } catch (e) {
    t.error = String(e)
    t.done = true
  }
  setTimeout(() => {
    const idx = transfers.value.findIndex((x) => x.key === key)
    if (idx >= 0 && !transfers.value[idx].error) transfers.value.splice(idx, 1)
    void refresh()
  }, 800)
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

// 拖拽上传：Wails OnFileDrop 回调拿到本地文件路径数组，逐个上传到当前目录
const dragOver = ref(false)

async function uploadPaths(localPaths: string[]) {
  if (!props.tab.sessionId || !localPaths.length) return
  for (const localPath of localPaths) {
    const name = localPath.split(/[/\\]/).pop() || 'file'
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

function bindSessionEvents(sessionId: string) {
  while (disposers.length) disposers.pop()?.()
  if (!sessionId) return
  disposers.push(onSftpTransfer(sessionId, onTransfer))
  disposers.push(onSftpDirUpdated(sessionId, (evt) => {
    if (evt.path === path.value || evt.path === path.value + '/') {
      entries.value = evt.entries.sort((a: SFTPEntry, b: SFTPEntry) =>
        a.isDir === b.isDir ? a.name.localeCompare(b.name) : a.isDir ? -1 : 1,
      )
    }
  }))
}

onMounted(() => {
  bindSessionEvents(props.tab.sessionId)
  window.addEventListener('click', closeMenu)
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('scroll', closeMenu, true)
  // 注册 Wails 文件拖放：仅在 SFTP 面板（带 --wails-drop-target 标识）内松开时上传
  OnFileDrop((_x, _y, paths) => {
    dragOver.value = false
    if (paths?.length) void uploadPaths(paths)
  }, true)
})

watch(
  () => props.tab.sessionId,
  (sid) => bindSessionEvents(sid),
)

watch(
  () => [props.tab.sftpPath, props.tab.status, props.tab.sessionId] as const,
  ([dir, status, sid]) => {
    if (!sid || status !== 'connected' || !dir) return
    void loadFromSync(dir)
  },
  {immediate: true},
)

watch(
  () => props.tab.status,
  (status) => {
    if (status === 'connecting') {
      entries.value = []
      error.value = ''
    }
  },
)

onBeforeUnmount(() => {
  disposers.forEach((d) => d())
  window.removeEventListener('click', closeMenu)
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('scroll', closeMenu, true)
  OnFileDropOff()
})
</script>

<template>
  <div class="tool">
    <div class="tool-head">
      <div class="seg">
        <button class="active">SFTP</button>
        <button @click="sessions.showRightPanel('sysinfo')">看板</button>
      </div>
      <button class="btn-icon btn-sm ml-auto" title="收起" @click="sessions.sftpVisible = false">
        <Icon name="close" :size="14" />
      </button>
    </div>

    <div class="flex items-center gap-1.5 p-3" style="box-shadow: inset 0 -1px 0 rgba(255,255,255,0.05)">
      <button class="btn-icon btn-sm" title="上级" @click="up">
        <Icon name="up" :size="14" />
      </button>
      <button class="btn-icon btn-sm" title="刷新" @click="refresh">
        <Icon name="refresh" :size="14" />
      </button>
      <input
        v-if="editingPath"
        ref="pathInputEl"
        v-model="pathInput"
        class="input input-sm flex-1 font-mono text-[12px]"
        spellcheck="false"
        @keyup.enter="doPathEdit"
        @keyup.esc="editingPath = false"
        @blur="doPathEdit"
      />
      <div
        v-else
        class="flex-1 min-w-0 flex items-center gap-0.5 overflow-x-auto no-scrollbar text-[12px] font-mono text-mist cursor-text"
        title="点击编辑完整路径"
        @click="startPathEdit"
      >
        <template v-for="(c, i) in crumbs" :key="c.path">
          <button class="shrink-0 hover:text-signal" @click.stop="go(c.path)">{{ c.label }}</button>
          <span v-if="i < crumbs.length - 1" class="shrink-0 opacity-40">/</span>
        </template>
      </div>
      <button class="btn btn-ghost btn-sm" @click="startNewFolder">新建</button>
      <button class="btn btn-ghost btn-sm" @click="upload">上传</button>
    </div>

    <div v-if="newFolderActive" class="flex items-center gap-2 px-3 py-2" style="box-shadow: inset 0 -1px 0 rgba(255,255,255,0.05)">
      <span class="text-[12px] text-mist shrink-0">名称</span>
      <input
        ref="newFolderInputEl"
        v-model="newFolderName"
        class="input input-sm flex-1"
        placeholder="新文件夹名称"
        @keyup.enter="doNewFolder"
        @keyup.esc="newFolderActive = false"
        @blur="doNewFolder"
      />
    </div>

    <div
      class="flex-1 overflow-y-auto px-2 py-1.5 relative transition-colors"
      style="--wails-drop-target: drop"
      :class="dragOver ? 'bg-[rgba(42,168,154,0.10)]' : ''"
      @dragenter.prevent="dragOver = true"
      @dragover.prevent="dragOver = true"
      @dragleave.prevent="dragOver = false"
      @drop.prevent="dragOver = false"
    >
      <div v-if="dragOver" class="absolute inset-2 rounded-[10px] border-2 border-dashed border-[var(--signal-400)] bg-[rgba(42,168,154,0.06)] grid place-items-center pointer-events-none z-10">
        <p class="text-xs text-[var(--signal-300)]">松开以上传到当前目录</p>
      </div>
      <div v-if="!entries.length && !loading && !error" class="py-6 text-center text-xs text-mist">空目录</div>
      <div
        v-for="e in entries"
        :key="e.path"
        class="grid grid-cols-[20px_1fr_auto] items-center gap-2 h-9 px-2 rounded-[6px] text-xs cursor-pointer"
        :class="selected === e.path ? 'bg-[rgba(42,168,154,0.12)]' : 'hover:bg-white/[0.04]'"
        @click="selected = e.path"
        @dblclick="onDblClick(e)"
        @contextmenu.prevent="openMenu($event, e)"
      >
        <Icon :name="e.isDir ? 'folder' : 'file'" :size="14" :extra-class="e.isDir ? 'text-copper' : 'text-mist'" />
        <template v-if="renaming?.path === e.path">
          <input
            ref="renameInputEl"
            v-model="renameInput"
            class="input input-sm col-span-2"
            spellcheck="false"
            @click.stop
            @keyup.enter="doRename"
            @keyup.esc="renaming = null"
            @blur="doRename"
          />
        </template>
        <template v-else-if="confirmDeletePath === e.path">
          <span class="text-[12px] text-danger truncate">删除「{{ e.name }}」？</span>
          <div class="flex gap-1">
            <button class="btn btn-danger btn-sm" @click.stop="doDelete">确认</button>
            <button class="btn btn-ghost btn-sm" @click.stop="confirmDeletePath = ''">取消</button>
          </div>
        </template>
        <template v-else>
          <span class="truncate text-[var(--mist-200)]">{{ e.name }}</span>
          <span class="font-mono text-[11px] text-mist">{{ e.isDir ? '—' : fmtSize(e.size) }}</span>
        </template>
      </div>
      <div v-if="loading" class="sticky bottom-0 flex items-center justify-center gap-2 py-2 text-[12px] text-mist">
        加载中…
      </div>
      <div v-if="error" class="sticky bottom-0 px-2 py-2 text-[12px] text-danger break-all">
        {{ error }}
        <button class="ml-1 underline hover:text-signal" @click="refresh">重试</button>
      </div>
    </div>

    <div v-if="transfers.length" class="shrink-0 p-3 flex flex-col gap-2" style="box-shadow: inset 0 1px 0 rgba(255,255,255,0.05)">
      <div v-for="t in transfers" :key="t.key" class="grid grid-cols-[1fr_auto] gap-1 text-[12px]">
        <span class="truncate text-[var(--mist-200)]">{{ t.direction === 'upload' ? '↑' : '↓' }} {{ t.name }}</span>
        <span class="flex items-center gap-2">
          <button v-if="!t.done && !t.error" class="btn btn-ghost btn-sm" @click="cancelTransfer(t)">取消</button>
          <span class="font-mono text-signal">{{ percent(t) }}%</span>
        </span>
        <div class="prog col-span-2" :class="{indeterminate: !t.done && !t.error && percent(t) === 0}">
          <i :style="{width: (percent(t) === 0 && !t.done ? 30 : percent(t)) + '%'}"></i>
        </div>
        <div class="col-span-2 font-mono text-[11px] text-mist">
          {{ t.total ? `${fmtSize(t.transferred)} / ${fmtSize(t.total)}` : t.done ? '完成' : '传输中…' }}
        </div>
        <div v-if="t.error" class="col-span-2 flex justify-between text-danger">
          <span class="break-all">{{ t.error }}</span>
          <button @click="dismissTransfer(t.key)"><Icon name="close" :size="12" /></button>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="menu"
        class="menu-pop neo fixed z-50"
        :style="{left: menu.x + 'px', top: menu.y + 'px'}"
        @contextmenu.prevent
        @click.stop
      >
        <button v-if="menu.entry.isDir" @click="menuEnter">进入目录</button>
        <button v-else @click="menuDownload">下载到本地</button>
        <button @click="startRename(menu.entry)">重命名</button>
        <button class="danger" @click="requestDelete(menu.entry)">删除</button>
      </div>
    </Teleport>
  </div>
</template>
