<script lang="ts" setup>
import {errorText} from '../i18n'

import {t as $t} from '../i18n'

import {t as translate} from '../i18n'

import {copyText as ClipboardSetText} from '../utils/clipboard'
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {onSftpTransfer, onSftpDirUpdated, sshService} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import {useMenuPlacement} from '../composables/useMenuPlacement'
import {useMenuKeyboard} from '../composables/useMenuKeyboard'
import {focusControl, navigateKeyboardFocus} from '../utils/keyboard-navigation'
import {useSettingsStore} from '../stores/settings'
import {useTransfersStore} from '../stores/transfers'
import {EventsOn} from '../services/desktop'
import type {SFTPEntry, SessionTab, TransferRecord} from '../types'
import {Button, Input} from './ui'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()
const transfersStore = useTransfersStore()

function sftpError(e: unknown): string {
  const message = errorText(e)
  return props.tab.node.jumpServerMode && message.includes('please select one of the assets')
    ? translate('请先进入目标资产和账号的文件目录，再进行操作（JumpServer 尚未选中资产）。')
    : message
}

function canWrite(): boolean {
  if (props.tab.node.jumpServerMode && (path.value === '/' || !!error.value || loading.value)) {
    error.value = translate('请先进入目标资产和账号的文件目录，再进行操作。')
    return false
  }
  return true
}

const entries = ref<SFTPEntry[]>([])
const loading = ref(false)
const error = ref('')

// 交互状态：选中 / 右键菜单 / 重命名 / 删除确认 / 新建文件夹 / 路径编辑
const selected = ref('')
const fileList = ref<HTMLElement | null>(null)
let menuTrigger: HTMLElement | null = null
let focusDirectoryAfterLoad = false
function restoreDirectoryFocus() {
  if (!focusDirectoryAfterLoad) return
  focusDirectoryAfterLoad = false
  void nextTick(() => {
    if (document.activeElement === document.body || fileList.value?.contains(document.activeElement)) {
      focusControl(fileList.value?.querySelector<HTMLElement>('[data-testid="sftp-entry"]') ?? fileList.value)
    }
  })
}
const uploadMenuOpen = ref(false)
const uploadMenuRoot = ref<HTMLElement>()
async function toggleUploadMenu() {
  uploadMenuOpen.value = !uploadMenuOpen.value
  if (uploadMenuOpen.value) { await nextTick(); uploadMenuRoot.value?.querySelector<HTMLElement>('[role="menuitem"]')?.focus() }
}
function uploadMenuKey(event: KeyboardEvent) {
  const buttons = Array.from(uploadMenuRoot.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])
  const index = buttons.indexOf(document.activeElement as HTMLElement)
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); uploadMenuOpen.value = false; uploadMenuRoot.value?.querySelector<HTMLElement>('[aria-haspopup]')?.focus() }
  else if (['ArrowDown','ArrowUp','Home','End'].includes(event.key)) {
    event.preventDefault()
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length
    buttons[next]?.focus()
  } else if (event.key === 'Tab') uploadMenuOpen.value = false
}
const menu = ref<{x: number; y: number; entry: SFTPEntry | null} | null>(null)
// 文件右键菜单：打开后按真实高度定位，贴底时上移，避免操作项被窗口裁掉
const {menuEl: fileMenuEl, menuStyle: fileMenuStyle, place: placeFileMenu} = useMenuPlacement()
const fileMenuKeyboard = useMenuKeyboard(fileMenuEl, closeMenu, {initialFocus: false, trigger: () => menuTrigger})
watch(menu, async value => {
  if (!value) return
  await nextTick()
  fileMenuKeyboard.focusAt(0)
})
const renaming = ref<{path: string; name: string} | null>(null)
const renameInput = ref('')
const confirmDeletePath = ref('')
const newFolderActive = ref(false)
const newFolderName = ref('')
const editingPath = ref(false)
const pathInput = ref('')

type FocusableInput = {focus: () => void; select: () => void}
const renameInputEl = ref<FocusableInput | null>(null)
const newFolderInputEl = ref<FocusableInput | null>(null)
const pathInputEl = ref<FocusableInput | null>(null)

interface TransferItem {
  transferId?: string
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
const transferTimers = new Set<ReturnType<typeof setTimeout>>()
let disposed = false

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

const visibleCrumbs = computed(() => crumbs.value.length > 4
  ? [crumbs.value[0], {...crumbs.value[crumbs.value.length - 3], label: '…'}, ...crumbs.value.slice(-2)]
  : crumbs.value)

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
    error.value = sftpError(e)
  } finally {
    loading.value = false
    restoreDirectoryFocus()
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
  if (!props.tab.node.jumpServerMode && settings.sftpToTerminalSync && props.tab.sessionId) {
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
    console.info(translate("[sftp-sync] 面板已切换"), dir, translate("条目"), list.length)
  } catch (e) {
    error.value = sftpError(e)
    console.warn(translate("[sftp-sync] 列出失败，保持当前列表"), dir, e)
  } finally {
    restoreDirectoryFocus()
  }
}

function up() {
  go(parentPath(path.value))
}

function refresh() {
  load(path.value)
}

// ---- 右键菜单 ----
function openMenu(e: MouseEvent, entry: SFTPEntry | null = null) {
  menuTrigger = e.currentTarget instanceof HTMLElement ? e.currentTarget : document.activeElement as HTMLElement | null
  selected.value = entry?.path || ''
  menu.value = {x: e.clientX, y: e.clientY, entry}
  void placeFileMenu(e.clientX, e.clientY)
}

async function copyMenuPath(entry = false) {
  const value = entry ? menu.value?.entry?.path : path.value
  closeMenu()
  if (!value) return
  try { await ClipboardSetText(value) } catch (e) { error.value = translate("复制路径失败：{0}", [sftpError(e)]) }
}
function menuAction(action: () => unknown) {
  closeMenu()
  void action()
}
function closeMenu() {
  if (fileMenuEl.value?.contains(document.activeElement)) focusControl(menuTrigger)
  uploadMenuOpen.value = false
  menu.value = null
}

function menuEnter() {
  if (menu.value?.entry?.isDir) {
    focusDirectoryAfterLoad = !!fileMenuEl.value?.contains(document.activeElement)
    enter(menu.value.entry)
  }
  closeMenu()
}

function menuDownload() {
  if (menu.value?.entry && !menu.value.entry.isDir) void download(menu.value.entry)
  closeMenu()
}

function menuDownloadFolder() {
  if (menu.value?.entry?.isDir) void downloadFolder(menu.value.entry)
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
  if (!canWrite()) return
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
    error.value = sftpError(e)
  }
}

// ---- 删除 ----
function requestDelete(entry: SFTPEntry) {
  confirmDeletePath.value = entry.path
  closeMenu()
  void nextTick(() => focusControl(fileList.value?.querySelector<HTMLElement>('[data-testid="sftp-delete-cancel"]')))
}

async function doDelete() {
  if (!canWrite()) return
  const target = confirmDeletePath.value
  confirmDeletePath.value = ''
  if (!target) return
  try {
    await sshService.sftpRemove(props.tab.sessionId, target)
    await refresh()
  } catch (e) {
    error.value = sftpError(e)
  }
}

// ---- 新建文件夹 ----
function startNewFolder() {
  newFolderActive.value = true
  newFolderName.value = ''
  void nextTick(() => newFolderInputEl.value?.focus())
}

async function doNewFolder() {
  if (!canWrite()) return
  newFolderActive.value = false
  const name = newFolderName.value.trim()
  if (!name) return
  try {
    await sshService.sftpMkdir(props.tab.sessionId, joinPath(path.value, name))
    await refresh()
  } catch (e) {
    error.value = sftpError(e)
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
  if (e.defaultPrevented || e.isComposing || e.key !== 'Escape') return
  if (!menu.value && !uploadMenuOpen.value && !renaming.value && !confirmDeletePath.value && !newFolderActive.value && !editingPath.value) return
  e.preventDefault()
  e.stopPropagation()
  uploadMenuOpen.value = false
  menu.value = null
  renaming.value = null
  confirmDeletePath.value = ''
  newFolderActive.value = false
  editingPath.value = false
}

function onEntryKey(event: KeyboardEvent, entry: SFTPEntry) {
  if (event.target !== event.currentTarget || event.isComposing || event.ctrlKey || event.metaKey || event.altKey) return
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    event.stopPropagation()
    selected.value = entry.path
    if (event.key === 'Enter' && !event.repeat) {
      focusDirectoryAfterLoad = entry.isDir
      onDblClick(entry)
    }
  } else if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
    event.preventDefault()
    event.stopPropagation()
    const element = event.currentTarget as HTMLElement
    const rect = element.getBoundingClientRect()
    openMenu(new MouseEvent('contextmenu', {clientX: rect.left + 16, clientY: rect.bottom}), entry)
  } else if (event.key === 'F2') {
    event.preventDefault()
    event.stopPropagation()
    startRename(entry)
  } else if (event.key === 'Delete') {
    event.preventDefault()
    event.stopPropagation()
    requestDelete(entry)
  }
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
  if (s < 60) return translate("约 {0}s", [s])
  return translate("约 {0}m{1}s", [Math.floor(s / 60), s % 60])
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
function onTransfer(evt: {transferId?: string; direction: 'upload' | 'download'; name: string; transferred: number; total: number; done: boolean; error?: string}) {
  let t = evt.transferId
    ? transfers.value.find(x => x.transferId === evt.transferId)
      ?? transfers.value.find(x => x.direction === evt.direction && x.name === evt.name && !x.done && !x.transferId)
    : transfers.value.find(x => x.direction === evt.direction && x.name === evt.name && !x.done)
  if (!t) {
    if (evt.done && !evt.error) return
    t = newTransferItem(evt.direction, evt.name)
    transfers.value.push(t)
  }
  if (evt.transferId) t.transferId = evt.transferId
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
    if (!evt.error && t.total > 0) t.transferred = Math.max(t.transferred, t.total)
    if (evt.error) t.error = evt.error
  }
}

/** 新建一条进行中的传输项。 */
function newTransferItem(direction: 'upload' | 'download', name: string): TransferItem {
  return {
    key: `${direction}:${name}:${crypto.randomUUID()}`,
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
  if (transfers.value.some((t) => t.direction === direction && t.name === name && !t.done)) return
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
    t.error = sftpError(e)
    t.done = true
    t.finishedAt = Date.now()
  }
  // 取消的判定：错误文案由后端在取消时给出「已取消」
  if (t.error && t.error.includes(translate("已取消"))) cancelled = true
  recordHistory(t, cancelled)
  if (disposed) return
  // 完成的行保留一小会儿再移除，让用户看到「完成 / 失败」状态；
  // 记录本身已经进入「传输记录」页签，不会丢。
  const timer = setTimeout(() => {
    transferTimers.delete(timer)
    const idx = transfers.value.findIndex((x) => x.key === t.key)
    if (idx >= 0 && !transfers.value[idx].error) transfers.value.splice(idx, 1)
    void refresh()
  }, 1200)
  transferTimers.add(timer)
}

async function upload() {
  if (!canWrite()) return
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
  if (!canWrite()) return
  if (!props.tab.sessionId) return
  const localDir = await sshService.selectLocalFolder()
  if (!localDir) return
  const name = localDir.split(/[/\\]/).filter(Boolean).pop() || 'folder'
  await runTransfer('upload', name, {remotePath: joinPath(path.value, name), localPath: localDir}, () =>
    sshService.sftpUploadFolder(props.tab.sessionId, localDir, path.value),
  )
}

// 拖拽上传：Wails 文件拖放事件拿到本地路径数组，逐个上传到当前目录。
// 路径是文件夹时走递归上传（保留文件夹名），是文件时沿用原来的单文件上传。
const dragOver = ref(false)
const dropTarget = computed(() => 'sftp-drop-' + props.tab.clientId)
let disposeFileDrop: (() => void) | undefined

async function uploadPaths(localPaths: string[]) {
  if (!canWrite()) return
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
    await sshService.sftpCancelTransfer(props.tab.sessionId, t.direction, t.transferId ? `@id:${t.transferId}` : t.name)
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
  window.addEventListener('scroll', closeMenu, true)
  disposeFileDrop = EventsOn('sftp:files-dropped', ({target, paths}: {target: string; paths: string[]}) => {
    if (target !== dropTarget.value) return
    dragOver.value = false
    if (paths.length) void uploadPaths(paths)
  })
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
  disposed = true
  transferTimers.forEach(clearTimeout)
  transferTimers.clear()
  disposers.forEach((d) => d())
  window.removeEventListener('click', closeMenu)
  window.removeEventListener('scroll', closeMenu, true)
  disposeFileDrop?.()
})
</script>

<template>
  <div class="tool sftp-panel" data-ui="context-panel" data-keyboard-nav="grid" @keydown="onKeydown">
    <div class="tool-head tool-head-light">
      <div class="flex items-center gap-1.5 font-medium text-xs text-[var(--foreground)]">
        <Icon name="folder" :size="13" class="text-[var(--primary)]" />
        <span>{{ $t('SFTP 文件管理') }}</span>
      </div>
      <Button
        variant="ghost"
        size="icon"
        class="ml-auto"
        :title="transfersStore.records.length ? translate('传输记录（{0} 条）', [transfersStore.records.length]) : translate('传输记录')"
        :aria-label="$t('传输记录')"
        @click="sessions.showRightPanel('transfers')"
      >
        <Icon name="clock" :size="14" />
      </Button>
      <Button variant="ghost" size="icon" :title="$t('收起')" :aria-label="$t('收起侧栏')" @click="sessions.sftpVisible = false">
        <Icon name="close" :size="14" />
      </Button>
    </div>

    <!-- 导航独占一行，操作区单独排列，避免按钮挤压路径。 -->
    <div data-testid="sftp-navigation" class="sftp-navigation flex items-center gap-1 px-3 py-1">
      <Button variant="ghost" size="icon" :title="$t('上级')" :aria-label="$t('返回上级目录')" @click="up">
        <Icon name="up" :size="14" />
      </Button>
      <Button variant="ghost" size="icon" :title="$t('刷新')" :aria-label="$t('刷新目录')" @click="refresh">
        <Icon name="refresh" :size="14" />
      </Button>
      <Input
        v-if="editingPath"
        ref="pathInputEl"
        v-model="pathInput"
        class="flex-1 font-mono text-[12px]"
        spellcheck="false"
        :aria-label="$t('远程路径')"
        @keyup.enter="doPathEdit"
        @keyup.esc="editingPath = false"
        @blur="doPathEdit"
      />
      <div
        v-else
        data-testid="sftp-breadcrumbs" class="flex-1 min-w-0 flex items-center gap-0.5 overflow-x-auto no-scrollbar text-[12px] font-mono text-mist cursor-text"
        :title="path"
        :aria-label="`${translate('远程路径')}: ${path}`"
        @click="startPathEdit"
      >
        <template v-for="(c, i) in visibleCrumbs" :key="c.path">
          <button class="sftp-crumb" :class="{'sftp-crumb-current': i === visibleCrumbs.length - 1}" :title="path" @click.stop="go(c.path)">{{ c.label }}</button>
          <span v-if="i > 0 && i < visibleCrumbs.length - 1" class="shrink-0 opacity-40">/</span>
        </template>
      </div>
    </div>
    <div data-testid="sftp-actions" class="sftp-actions flex items-center gap-1.5 px-3 pt-0.5 pb-2">
      <div ref="uploadMenuRoot" class="sftp-upload-group" @click.stop @keydown="uploadMenuKey">
        <Button class="sftp-upload-primary" variant="secondary" data-testid="sftp-upload-menu" aria-haspopup="menu" :aria-expanded="uploadMenuOpen" @click="toggleUploadMenu" @keydown.down.prevent="uploadMenuOpen || toggleUploadMenu()">
          {{ $t('上传') }}<Icon name="chevron-down" :size="13" />
        </Button>
        <div v-if="uploadMenuOpen" role="menu" :aria-label="$t('上传')" class="menu-pop sftp-upload-menu">
          <button role="menuitem" data-testid="sftp-upload" @click="menuAction(upload)">{{ $t('上传文件') }}</button>
          <button role="menuitem" data-testid="sftp-upload-folder" @click="menuAction(uploadFolder)">{{ $t('上传文件夹') }}</button>
        </div>
      </div>
      <Button class="sftp-create-folder" variant="ghost" data-testid="sftp-create-folder" @click="startNewFolder"><Icon name="folder" :size="14" />{{ $t('新建文件夹') }}</Button>
    </div>

    <div v-if="newFolderActive" class="flex items-center gap-2 px-3 py-2 inset-line-b">
      <span class="text-[12px] text-mist shrink-0">{{ $t('名称') }}</span>
      <Input
        ref="newFolderInputEl"
        v-model="newFolderName"
        class="flex-1"
        :placeholder="$t('新文件夹名称')"
        :aria-label="$t('新文件夹名称')"
        @keyup.enter="doNewFolder"
        @keyup.esc="newFolderActive = false"
        @blur="doNewFolder"
      />
    </div>

    <div
      ref="fileList"
      class="group flex-1 overflow-y-auto px-2 py-1.5 relative transition-colors [&.file-drop-target-active]:bg-[var(--primary-weak)]"
      data-file-drop-target
      :id="dropTarget"
      :class="dragOver ? 'bg-[var(--primary-weak)]' : ''"
      data-testid="sftp-file-list"
      data-keyboard-nav="vertical"
      role="group"
      tabindex="0"
      :aria-label="$t('SFTP 文件管理')"
      @keydown="navigateKeyboardFocus($event, fileList!)"
      @contextmenu.prevent="openMenu($event)"
      @dragenter.prevent="dragOver = true"
      @dragover.prevent="dragOver = true"
      @dragleave.prevent="dragOver = false"
      @drop.prevent="dragOver = false"
    >
      <div :class="dragOver ? 'grid' : 'hidden group-[.file-drop-target-active]:grid'" class="absolute inset-2 rounded-[10px] border-2 border-dashed border-[var(--primary-hover)] bg-[var(--primary-glow-soft)] place-items-center pointer-events-none z-10">
        <p class="text-xs text-[var(--primary-hover)]">{{ $t('松开以上传到当前目录') }}</p>
      </div>
      <p v-if="props.tab.node.jumpServerMode" class="px-2 py-2 text-xs text-mist">{{ $t('JumpServer 模式：请逐级进入资产、账号的文件目录；终端目录不会与此处同步。') }}</p>
      <div v-if="!entries.length && !loading && !error" class="py-6 text-center text-xs text-mist">{{ $t('空目录') }}</div>
      <div
        v-for="e in entries"
        :key="e.path"
        data-testid="sftp-entry"
        tabindex="0"
        role="button"
        :aria-label="e.name"
        :aria-pressed="selected === e.path"
        :data-path="e.path"
        :data-dir="e.isDir ? 'true' : 'false'"
        class="grid grid-cols-[20px_1fr_auto] items-center gap-2 h-9 px-2 rounded-[6px] text-xs cursor-pointer"
        :class="selected === e.path ? 'sftp-entry-selected' : 'hover:bg-[var(--hover)]'"
        @click="selected = e.path"
        @focus="selected = e.path"
        @keydown="onEntryKey($event, e)"
        @dblclick="onDblClick(e)"
        @contextmenu.stop.prevent="openMenu($event, e)"
      >
        <Icon :name="e.isDir ? 'folder' : 'file'" :size="14" extra-class="text-mist" />
        <template v-if="renaming?.path === e.path">
          <Input
            ref="renameInputEl"
            v-model="renameInput"
            class="col-span-2"
            spellcheck="false"
            :aria-label="$t('重命名条目')"
            @click.stop
            @keyup.enter="doRename"
            @keyup.esc="renaming = null"
            @blur="doRename"
          />
        </template>
        <template v-else-if="confirmDeletePath === e.path">
          <span class="text-[12px] text-danger truncate">{{ $t('删除「') }} {{ e.name }}」？</span>
          <div class="flex gap-1" data-keyboard-nav="horizontal">
            <Button variant="danger" size="sm" @click.stop="doDelete">{{ $t('确认') }}</Button>
            <Button variant="ghost" size="sm" data-testid="sftp-delete-cancel" @click.stop="confirmDeletePath = ''">{{ $t('取消') }}</Button>
          </div>
        </template>
        <template v-else>
          <span class="truncate text-[var(--foreground)]">{{ e.name }}</span>
          <span class="font-mono text-[11px] text-mist">{{ e.isDir ? '—' : fmtSize(e.size) }}</span>
        </template>
      </div>
      <div v-if="loading" class="sticky bottom-0 flex items-center justify-center gap-2 py-2 text-[12px] text-mist">{{ $t('加载中…') }}</div>
      <div v-if="error" class="sticky bottom-0 px-2 py-2 text-[12px] text-danger break-all">
        {{ error }}
        <button class="ml-1 underline hover:text-signal" @click="refresh">{{ $t('重试') }}</button>
      </div>
    </div>

    <div v-if="transfers.length" class="shrink-0 p-3 flex flex-col gap-2 inset-line-t">
      <div v-for="t in transfers" :key="t.key" class="grid grid-cols-[1fr_auto] gap-1 text-[12px]">
        <span class="truncate text-[var(--foreground)]">{{ t.direction === 'upload' ? '↑' : '↓' }} {{ t.name }}</span>
        <span class="flex items-center gap-2">
          <Button v-if="!t.done && !t.error" variant="ghost" size="sm" @click="cancelTransfer(t)">{{ $t('取消') }}</Button>
          <span class="font-mono" :class="t.error ? 'text-danger' : 'text-signal'">
            {{ t.error ? (t.error.includes(translate('已取消')) ? translate('已取消') : translate('失败')) : percent(t) + '%' }}
          </span>
        </span>
        <div class="prog col-span-2" :class="{indeterminate: !t.done && !t.error && percent(t) === 0}">
          <i :style="{width: (percent(t) === 0 && !t.done ? 30 : percent(t)) + '%'}"></i>
        </div>
        <div class="col-span-2 flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[11px] text-mist">
          <span>{{ t.total ? `${fmtSize(t.transferred)} / ${fmtSize(t.total)}` : t.done ? translate('完成') : fmtSize(t.transferred) }}</span>
          <span v-if="!t.error" :title="t.done ? translate('平均速度') : translate('当前速度')">
            {{ t.done ? translate('平均') : '' }} {{ fmtSpeed(t.speed) }}
          </span>
          <span>{{ fmtElapsed(t) }}</span>
          <span v-if="fmtEta(t)">{{ fmtEta(t) }}</span>
        </div>
        <div v-if="t.error" class="col-span-2 flex justify-between text-danger">
          <span class="break-all">{{ t.error }}</span>
          <Button variant="ghost" size="icon" :aria-label="$t('关闭')" @click="dismissTransfer(t.key)"><Icon name="close" :size="12" /></Button>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="menu"
        ref="fileMenuEl"
        class="menu-pop neo fixed z-50"
        data-ui="context-menu"
        role="menu"
        :aria-label="$t('SFTP 文件管理')"
        @keydown="fileMenuKeyboard.onKeydown"
        :style="fileMenuStyle"
        @contextmenu.prevent
        @click.stop
      >
        <button role="menuitem" @click="menuAction(refresh)">{{ $t('刷新') }}</button>
        <button role="menuitem" @click="copyMenuPath()">{{ $t('复制当前目录路径') }}</button>
        <template v-if="menu.entry">
          <button role="menuitem" @click="copyMenuPath(true)">{{ $t('复制完整路径') }}</button>
          <button role="menuitem" v-if="menu.entry.isDir" @click="menuEnter">{{ $t('进入目录') }}</button>
          <button role="menuitem" v-if="menu.entry.isDir" @click="menuDownloadFolder">{{ $t('下载文件夹到本地') }}</button>
          <button role="menuitem" v-else @click="menuDownload">{{ $t('下载到本地') }}</button>
          <button role="menuitem" @click="startRename(menu.entry)">{{ $t('重命名') }}</button>
          <button role="menuitem" class="danger" @click="requestDelete(menu.entry)">{{ $t('删除') }}</button>
        </template>
        <template v-else>
          <button role="menuitem" @click="menuAction(up)">{{ $t('返回上级目录') }}</button>
          <button role="menuitem" @click="menuAction(startNewFolder)">{{ $t('新建文件夹') }}</button>
          <button role="menuitem" @click="menuAction(upload)">{{ $t('上传文件') }}</button>
          <button role="menuitem" @click="menuAction(uploadFolder)">{{ $t('上传文件夹') }}</button>
        </template>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sftp-navigation{gap:2px;padding-top:6px;padding-bottom:2px}
.sftp-navigation :deep(.ui-button-icon){height:28px;width:28px;color:var(--muted-foreground)}
.sftp-crumb{flex:0 1 auto;min-width:12px;max-width:92px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:3px 2px;color:var(--muted-foreground)}
.sftp-crumb:hover{background:var(--hover);border-radius:var(--radius-sm);color:var(--foreground)}
.sftp-crumb-current{flex-shrink:1;max-width:none;color:var(--foreground-secondary)}
.sftp-actions{padding-left:12px;padding-top:2px;padding-bottom:8px;border-bottom:1px solid var(--border-subtle)}
.sftp-actions :deep(.ui-button){height:32px;border-radius:var(--radius-sm);padding:0 9px;font-size:13px;font-weight:500;gap:5px}
.sftp-actions :deep(.sftp-upload-primary){background:transparent;color:var(--foreground-secondary);border-color:transparent}
.sftp-actions :deep(.sftp-upload-primary:hover){background:color-mix(in srgb,#6897b4 8%,var(--surface-2))}
.sftp-actions :deep(.sftp-create-folder){color:var(--foreground-secondary);border-color:transparent;background:transparent;font-size:13px}
.sftp-actions :deep(.sftp-create-folder:hover){background:color-mix(in srgb,#6897b4 8%,var(--surface-2));color:var(--foreground-secondary)}
.sftp-entry-selected{background:var(--arc-active-tab-gradient, var(--interactive-active-bg));border-radius:var(--radius-control, 10px);color:var(--text-primary)}
.sftp-upload-group{position:relative}
.sftp-upload-menu{position:absolute;left:0;top:calc(100% + 4px);z-index:45;min-width:152px}
</style>
