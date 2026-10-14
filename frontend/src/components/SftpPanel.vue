<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {onSftpTransfer, onSftpDirUpdated, sshService} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {useTransfersStore} from '../stores/transfers'
import {OnFileDrop, OnFileDropOff} from '../../wailsjs/runtime/runtime'
import type {SFTPEntry, SessionTab, TransferRecord} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()
const transfersStore = useTransfersStore()

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
  /** 开始时间（毫秒）；-1 表示尚未开始（还没收到第一个进度事件）。 */
  startedAt: number
  finishedAt: number
  /** 最近一次进度事件的时间戳与字节数：用于估算「当前速度」。 */
  lastTs: number
  lastBytes: number
  /** 平滑后的当前速度（字节/秒）。 */
  speed: number
  /** 远端 / 本地路径：结束时写入历史记录。 */
  remotePath: string
  localPath: string
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
  go(entry.path)
}

// 所有面板内导航（进入目录 / 返回上层 / 面包屑 / 手输路径）统一走这里：
// 切换面板目录的同时把终端 cd 到同一目录（SFTP → 终端目录同步，受配置项控制）。
function go(dir: string) {
  path.value = dir
  if (settings.sftpToTerminalSync && props.tab.sessionId) {
    sshService.syncSftpToTerminal(props.tab.sessionId, dir).catch(() => {})
  }
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

function menuDownloadFolder() {
  if (menu.value?.entry.isDir) void downloadFolder(menu.value.entry)
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
  if (!n || n < 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

/** 速度文案（字节/秒 → 人类可读）。 */
function fmtSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return '—'
  if (bytesPerSec < 1024) return `${bytesPerSec.toFixed(0)} B/s`
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`
  return `${(bytesPerSec / 1024 / 1024).toFixed(2)} MB/s`
}

/** 已用时间文案（秒）。 */
function fmtElapsed(t: TransferItem): string {
  const start = t.startedAt >= 0 ? t.startedAt : Date.now()
  const end = t.done ? (t.finishedAt || Date.now()) : Date.now()
  const s = Math.max(0, (end - start) / 1000)
  if (s < 60) return `${s.toFixed(1)}s`
  const m = Math.floor(s / 60)
  return `${m}m${Math.round(s - m * 60)}s`
}

/** 剩余时间估算（仅在已知总大小、且有速度时给出）。 */
function fmtEta(t: TransferItem): string {
  if (t.done || !t.total || t.speed <= 0) return ''
  const rest = t.total - t.transferred
  if (rest <= 0) return ''
  const s = Math.round(rest / t.speed)
  if (s < 60) return `约 ${s}s`
  return `约 ${Math.floor(s / 60)}m${s % 60}s`
}

function percent(t: TransferItem): number {
  if (!t.total) return t.done && !t.error ? 100 : 0
  return Math.min(100, Math.round((t.transferred / t.total) * 100))
}

/**
 * 进度事件处理：累计字节数与速度。
 *
 * 速度用「相邻事件差分 + 指数平滑」估算：事件本身按 100ms 节流，
 * 直接用瞬时差分抖动很大，平滑后读数稳定且能反映实时速率变化。
 */
function onTransfer(evt: {direction: 'upload' | 'download'; name: string; transferred: number; total: number; done: boolean; error?: string}) {
  const key = `${evt.direction}:${evt.name}`
  let t = transfers.value.find((x) => x.key === key)
  if (!t) {
    if (evt.done && !evt.error) return
    t = newTransferItem(evt.direction, evt.name)
    transfers.value.push(t)
  }
  if (!evt.done && evt.transferred < t.transferred) return // 乱序的旧事件，丢弃
  const now = Date.now()
  if (t.startedAt < 0) t.startedAt = now
  // 速度采样：只在有实际增量时更新，避免 100ms 空转把速度拉低
  const deltaBytes = evt.transferred - t.lastBytes
  const deltaMs = now - t.lastTs
  if (deltaMs > 0 && deltaBytes > 0) {
    const instant = (deltaBytes * 1000) / deltaMs
    t.speed = t.speed > 0 ? t.speed * 0.6 + instant * 0.4 : instant
    t.lastTs = now
    t.lastBytes = evt.transferred
  } else if (t.lastTs === 0) {
    t.lastTs = now
  }
  t.transferred = evt.transferred
  t.total = evt.total || t.total
  if (evt.done) {
    t.done = true
    t.finishedAt = now
    if (t.total > 0) t.transferred = Math.max(t.transferred, t.total)
    if (evt.error) t.error = evt.error
  }
}

/** 新建一条进行中的传输项。 */
function newTransferItem(direction: 'upload' | 'download', name: string): TransferItem {
  return {
    key: `${direction}:${name}`,
    direction,
    name,
    transferred: 0,
    total: 0,
    done: false,
    error: '',
    startedAt: Date.now(),
    finishedAt: 0,
    lastTs: Date.now(),
    lastBytes: 0,
    speed: 0,
    remotePath: '',
    localPath: '',
  }
}

/** 传输结束时写入历史记录（成功 / 失败 / 取消都会记一条）。 */
function recordHistory(t: TransferItem, cancelled: boolean) {
  const finishedAt = t.finishedAt || Date.now()
  const durationMs = Math.max(0, finishedAt - t.startedAt)
  const record: TransferRecord = {
    id: `${finishedAt}-${Math.random().toString(36).slice(2, 8)}`,
    direction: t.direction,
    name: t.name,
    size: t.total,
    transferred: t.transferred,
    startedAt: t.startedAt,
    finishedAt,
    durationMs,
    avgBytesPerSec: durationMs > 0 ? (t.transferred * 1000) / durationMs : 0,
    serverName: props.tab.serverName,
    host: props.tab.host,
    remotePath: t.remotePath,
    localPath: t.localPath,
    status: t.error ? (cancelled ? 'cancelled' : 'error') : 'done',
    error: t.error,
  }
  transfersStore.add(record)
}

async function runTransfer(
  direction: 'upload' | 'download',
  name: string,
  paths: {remotePath: string; localPath: string},
  task: () => Promise<void>,
) {
  const key = `${direction}:${name}`
  if (transfers.value.some((t) => t.key === key && !t.done)) return
  const t = newTransferItem(direction, name)
  t.remotePath = paths.remotePath
  t.localPath = paths.localPath
  transfers.value.push(t)
  let cancelled = false
  try {
    await task()
    if (!t.error) {
      t.done = true
      t.finishedAt = Date.now()
      if (t.total > 0) t.transferred = t.total
      // 用服务端最终进度事件可能还没到，这里按实际字节数补齐速度展示
      const dur = t.finishedAt - t.startedAt
      if (dur > 0 && t.transferred > 0) t.speed = (t.transferred * 1000) / dur
    }
  } catch (e) {
    t.error = String(e)
    t.done = true
    t.finishedAt = Date.now()
  }
  // 取消的判定：错误文案由后端在取消时给出「已取消」
  if (t.error && t.error.includes('已取消')) cancelled = true
  recordHistory(t, cancelled)
  // 完成的行保留一小会儿再移除，让用户看到「完成 / 失败」状态；
  // 记录本身已经进入「传输记录」页签，不会丢。
  setTimeout(() => {
    const idx = transfers.value.findIndex((x) => x.key === key)
    if (idx >= 0 && !transfers.value[idx].error) transfers.value.splice(idx, 1)
    void refresh()
  }, 1200)
}

async function upload() {
  const files = await sshService.selectLocalFiles()
  if (!files.length) return
  for (const localPath of files) {
    const name = localPath.split(/[/\\]/).pop() || 'file'
    const remotePath = joinPath(path.value, name)
    await runTransfer('upload', name, {remotePath, localPath}, () =>
      sshService.sftpUpload(props.tab.sessionId, localPath, remotePath),
    )
  }
}

// 上传文件夹：选择本地文件夹后整体递归上传到当前远端目录，
// 后端保留文件夹名（当前目录/<本地文件夹名>/...），整条传输显示一个聚合进度。
async function uploadFolder() {
  if (!props.tab.sessionId) return
  const localDir = await sshService.selectLocalFolder()
  if (!localDir) return
  const name = localDir.split(/[/\\]/).filter(Boolean).pop() || 'folder'
  await runTransfer('upload', name, {remotePath: joinPath(path.value, name), localPath: localDir}, () =>
    sshService.sftpUploadFolder(props.tab.sessionId, localDir, path.value),
  )
}

// 拖拽上传：Wails OnFileDrop 回调拿到本地路径数组，逐个上传到当前目录。
// 路径是文件夹时走递归上传（保留文件夹名），是文件时沿用原来的单文件上传。
const dragOver = ref(false)

async function uploadPaths(localPaths: string[]) {
  if (!props.tab.sessionId || !localPaths.length) return
  for (const localPath of localPaths) {
    let isDir = false
    try {
      isDir = await sshService.localPathIsDir(localPath)
    } catch {
      // 路径不可访问：按文件交给后端，传输时报错会如实展示
    }
    const name = localPath.split(/[/\\]/).filter(Boolean).pop() || 'file'
    if (isDir) {
      await runTransfer('upload', name, {remotePath: joinPath(path.value, name), localPath}, () =>
        sshService.sftpUploadFolder(props.tab.sessionId, localPath, path.value),
      )
    } else {
      const remotePath = joinPath(path.value, name)
      await runTransfer('upload', name, {remotePath, localPath}, () =>
        sshService.sftpUpload(props.tab.sessionId, localPath, remotePath),
      )
    }
  }
}

async function download(entry: SFTPEntry) {
  const localPath = await sshService.selectSavePath(entry.name)
  if (!localPath) return
  await runTransfer('download', entry.name, {remotePath: entry.path, localPath}, () =>
    sshService.sftpDownload(props.tab.sessionId, entry.path, localPath),
  )
}

// 下载文件夹：选择本地保存位置后，远端文件夹整体递归下载到该目录下
// （后端保留文件夹名：所选目录/<远端文件夹名>/...），整条传输显示一个聚合进度。
async function downloadFolder(entry: SFTPEntry) {
  const localDir = await sshService.selectSaveFolder()
  if (!localDir) return
  await runTransfer('download', entry.name, {remotePath: entry.path, localPath: localDir}, () =>
    sshService.sftpDownloadFolder(props.tab.sessionId, entry.path, localDir),
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

// 调试/自动化入口：把面板上的传输动作挂到 window，便于用调试 API 驱动真实传输
// （与界面点「上传 / 下载」走同一条路径：同样的进度事件、同样的传输记录）。
// 只在浏览器上下文里注册，无副作用；未开启调试模式时也没人会调用它。
if (typeof window !== 'undefined') {
  ;(window as unknown as Record<string, unknown>).__dingSftpPanel = {
    uploadPaths,
    download,
    downloadFolder,
  }
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
      <button
        class="btn-icon btn-sm ml-auto"
        :title="transfersStore.records.length ? `传输记录（${transfersStore.records.length} 条）` : '传输记录'"
        aria-label="传输记录"
        @click="sessions.showRightPanel('transfers')"
      >
        <Icon name="clock" :size="14" />
      </button>
      <button class="btn-icon btn-sm" title="收起" @click="sessions.sftpVisible = false">
        <Icon name="close" :size="14" />
      </button>
    </div>

    <div class="flex items-center gap-1.5 p-3 inset-line-b">
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
      <button class="btn btn-ghost btn-sm" title="选择本地文件夹，整体递归上传到当前目录（保留文件夹名）" @click="uploadFolder">上传文件夹</button>
    </div>

    <div v-if="newFolderActive" class="flex items-center gap-2 px-3 py-2 inset-line-b">
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
      :class="dragOver ? 'bg-[var(--signal-weak)]' : ''"
      @dragenter.prevent="dragOver = true"
      @dragover.prevent="dragOver = true"
      @dragleave.prevent="dragOver = false"
      @drop.prevent="dragOver = false"
    >
      <div v-if="dragOver" class="absolute inset-2 rounded-[10px] border-2 border-dashed border-[var(--signal-400)] bg-[var(--signal-glow-soft)] grid place-items-center pointer-events-none z-10">
        <p class="text-xs text-[var(--signal-300)]">松开以上传到当前目录</p>
      </div>
      <div v-if="!entries.length && !loading && !error" class="py-6 text-center text-xs text-mist">空目录</div>
      <div
        v-for="e in entries"
        :key="e.path"
        class="grid grid-cols-[20px_1fr_auto] items-center gap-2 h-9 px-2 rounded-[6px] text-xs cursor-pointer"
        :class="selected === e.path ? 'bg-[var(--signal-weak)]' : 'hover:bg-[var(--hover)]'"
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

    <div v-if="transfers.length" class="shrink-0 p-3 flex flex-col gap-2 inset-line-t">
      <div v-for="t in transfers" :key="t.key" class="grid grid-cols-[1fr_auto] gap-1 text-[12px]">
        <span class="truncate text-[var(--mist-200)]">{{ t.direction === 'upload' ? '↑' : '↓' }} {{ t.name }}</span>
        <span class="flex items-center gap-2">
          <button v-if="!t.done && !t.error" class="btn btn-ghost btn-sm" @click="cancelTransfer(t)">取消</button>
          <span class="font-mono" :class="t.error ? 'text-danger' : 'text-signal'">
            {{ t.error ? (t.error.includes('已取消') ? '已取消' : '失败') : percent(t) + '%' }}
          </span>
        </span>
        <div class="prog col-span-2" :class="{indeterminate: !t.done && !t.error && percent(t) === 0}">
          <i :style="{width: (percent(t) === 0 && !t.done ? 30 : percent(t)) + '%'}"></i>
        </div>
        <div class="col-span-2 flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[11px] text-mist">
          <span>{{ t.total ? `${fmtSize(t.transferred)} / ${fmtSize(t.total)}` : t.done ? '完成' : fmtSize(t.transferred) }}</span>
          <span v-if="!t.error" :title="t.done ? '平均速度' : '当前速度'">
            {{ t.done ? '平均' : '' }}{{ fmtSpeed(t.speed) }}
          </span>
          <span>{{ fmtElapsed(t) }}</span>
          <span v-if="fmtEta(t)">{{ fmtEta(t) }}</span>
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
        <button v-if="menu.entry.isDir" @click="menuDownloadFolder">下载文件夹到本地</button>
        <button v-else @click="menuDownload">下载到本地</button>
        <button @click="startRename(menu.entry)">重命名</button>
        <button class="danger" @click="requestDelete(menu.entry)">删除</button>
      </div>
    </Teleport>
  </div>
</template>
