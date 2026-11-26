<script lang="ts" setup>
import {ResolveAppClose} from '../bindings/ding-ssh/internal/apphost/app'
import UpdatePanel from './components/UpdatePanel.vue'
import {checkUpdates} from './services/updates'
import {errorText} from './i18n'

import {t as $t} from './i18n'

import {t} from './i18n'

import {confirmAppClose} from './services/closeConfirmation'
import {EventsOn} from './services/desktop'
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './components/Icon.vue'
import {useWorkspaceLayout} from './composables/useWorkspaceLayout'
import SftpPanel from './components/SftpPanel.vue'
import SysInfoPanel from './components/SysInfoPanel.vue'
import TransferHistoryPanel from './components/TransferHistoryPanel.vue'
import ServerStatusBar from './components/ServerStatusBar.vue'
import TabBar from './components/TabBar.vue'
import TerminalView from './components/TerminalView.vue'
import SplitView from './components/SplitView.vue'
import WidthResizer from './components/WidthResizer.vue'
import {layoutPanes} from './panes/layout'
import type {PaneDirection, SplitDirection} from './types'
import QuickConnectPanel from './components/QuickConnectPanel.vue'
import {getStartupSecurityStatus, securityService} from './services/security'
import {getElementRect} from './utils/dom'
import {buildCommands} from './commands'
import type {AppCommand} from './commands'
import {useServersStore} from './stores/servers'
import {useSessionsStore} from './stores/sessions'
import {useSettingsStore} from './stores/settings'
import {useUIStore} from './stores/ui'
import {useCommandStore} from './stores/command'
import {useTheme} from './composables/useTheme'
import {useHotkeys} from './composables/useHotkeys'
import {HOTKEYS} from './hotkeys'
import {useToast} from './composables/useToast'
import {Button, Dialog, ToastProvider} from './components/ui'
import CommandPalette from './components/command/CommandPalette.vue'
import ServerList from './components/ServerList.vue'
import SettingsPage from './components/SettingsPage.vue'
import TunnelPage from './components/TunnelPage.vue'
import AppTopBar from './layouts/AppTopBar.vue'
import AppSidebar from './layouts/AppSidebar.vue'
import AppTitleBar from './layouts/AppTitleBar.vue'
import AppUnlock from './layouts/AppUnlock.vue'
import AppLayout from './layouts/AppLayout.vue'
import TerminalWorkspace from './views/TerminalWorkspace.vue'
import WorkspaceHome from './views/WorkspaceHome.vue'

const sessions = useSessionsStore()
const ui = useUIStore()
const settings = useSettingsStore()
let appEventDisposers: Array<() => void> = []

const queryParams = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : null
const detachedMode = ref(queryParams?.get('mode') || '')
const detachedSession = ref(queryParams?.get('session') || '')

function handleExternalUrl(urlString: string) {
  try {
    if (urlString.startsWith('ding-ssh://connect/')) {
      const serverId = urlString.replace('ding-ssh://connect/', '').trim()
      const server = servers.servers.find(s => s.id === serverId)
      if (server) {
        sessions.openTab(server)
        ui.showWorkspace()
        toast.show(t('已连接服务器: ') + (server.name || server.host), {tone: 'info'})
      } else {
        toast.error(t('未找到匹配的服务器 ID: ') + serverId)
      }
      return
    }

    if (urlString.startsWith('ssh://')) {
      const parsed = new URL(urlString)
      const host = parsed.hostname
      const port = Number(parsed.port) || 22
      const user = parsed.username || 'root'
      if (!host) return
      const existing = servers.servers.find(s => s.host === host && (s.port || 22) === port && (s.user || 'root') === user)
      if (existing) {
        sessions.openTab(existing)
      } else {
        sessions.openTab({
          id: 'temp-' + Date.now(),
          name: `${user}@${host}:${port}`,
          host,
          port,
          user,
          group: '',
          authType: 'password',
          bgImage: '',
          blurAmount: 0,
          envVars: {},
        })
      }
      ui.showWorkspace()
      toast.show(t('正在连接外部 URL: ') + `${user}@${host}:${port}`, {tone: 'info'})
    }
  } catch (err) {
    console.error('[App] Failed to parse external URL:', err)
  }
}

function handleImportFile(filePath: string) {
  toast.show(t('检测到配置包: ') + filePath, {tone: 'info'})
  ui.showSettings('migrate')
}

onMounted(() => {
  if (detachedMode.value && detachedSession.value) {
    if (!sessions.tabs.some(t => t.sessionId === detachedSession.value || t.clientId === detachedSession.value)) {
      sessions.tabs.push({
        clientId: detachedSession.value,
        sessionId: detachedSession.value,
        serverName: detachedMode.value === 'detached-sftp' ? 'SFTP - ' + detachedSession.value : 'Terminal - ' + detachedSession.value,
        host: '',
        user: '',
        status: 'connected',
        createdAt: Date.now(),
        sftpPath: '',
        kind: 'ssh',
        node: {
          id: detachedSession.value,
          name: detachedSession.value,
          host: '',
          port: 22,
          user: '',
          group: '',
          authType: 'password',
          bgImage: '',
          blurAmount: 0,
          envVars: {},
        },
      })
      sessions.activeId = detachedSession.value
    }
  }

  appEventDisposers = [
    EventsOn('app:confirm-close', async () => {
      const confirmed = await confirmAppClose()
      await ResolveAppClose(confirmed)
    }),
    EventsOn('app:external-url', (url: string) => {
      handleExternalUrl(url)
    }),
    EventsOn('app:import-file', (path: string) => {
      handleImportFile(path)
    }),
    EventsOn('menu:new-connection', () => {
      ui.requestNewServer()
    }),
    EventsOn('menu:quick-connect', () => {
      ui.showWorkspace()
      ui.openTerminalSidebar()
    }),
    EventsOn('menu:import-config', () => {
      ui.showSettings('migrate')
    }),
    EventsOn('menu:export-config', () => {
      ui.showSettings('migrate')
    }),
    EventsOn('menu:reconnect-current', () => {
      window.dispatchEvent(new CustomEvent('terminal:reconnect-active'))
    }),
    EventsOn('menu:disconnect-current', () => {
      if (sessions.activeId) sessions.closeTab(sessions.activeId)
    }),
    EventsOn('menu:open-debug', () => {
      ui.showSettings('debug')
    }),
    EventsOn('menu:check-updates', () => {
      void checkUpdates(true)
    }),
    EventsOn('tray:open-session', (sessionId: string) => {
      if (detachedMode.value || !sessionId) return
      if (sessions.openSessionById(sessionId)) {
        ui.showWorkspace()
      }
    }),
    EventsOn('system:did-wake', () => {
      toast.show(t('系统已从休眠唤醒，正在校验连接状态'), {tone: 'info'})
    }),
    EventsOn('system:network-changed', () => {
      toast.show(t('系统网络状态已变更'), {tone: 'info'})
    }),
  ]
})

onBeforeUnmount(() => {
  appEventDisposers.forEach(d => d())
  appEventDisposers = []
})

watch(() => settings.smoothAnimations, value => document.documentElement.toggleAttribute('data-smooth-animations', value), {immediate: true})
const servers = useServersStore()
const commandStore = useCommandStore()
const toast = useToast()
const isMacOS = navigator.userAgent.includes('Macintosh')
const isWindows = navigator.userAgent.includes('Windows')
document.documentElement.style.setProperty('--window-titlebar-h', isMacOS || isWindows ? '32px' : '0px')

// 左侧导航折叠状态：同时把导航宽度写到 <html>，供 Teleport 到 body 的浮层
// （快速连接侧栏、遮罩等）正确定位。
const NAV_COLLAPSED_KEY = 'ding-ssh:nav-collapsed'
// 启动恢复确认框：本次应用启动已提示过（sessionStorage 随进程重启重置）。
const RESTORE_PROMPT_SESSION_KEY = 'ding-ssh:restore-prompt-settled'

function readNavCollapsed(): boolean {
  try {
    return localStorage.getItem(NAV_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

const navCollapsed = ref(readNavCollapsed())
// 折叠状态落盘；导航宽度本身由下面的宽度逻辑驱动（--nav-w 见 watchEffect）
watch(navCollapsed, (v) => {
  try {
    localStorage.setItem(NAV_COLLAPSED_KEY, v ? '1' : '0')
  } catch {
    // 存储不可用时忽略：折叠状态仅影响本次会话
  }
})

const {onSchemeChange} = useTheme(settings)

const needsUnlock = ref(false)
const initializationError = ref('')
const unlockPassword = ref('')
const unlockError = ref('')
const unlocking = ref(false)

watch(() => sessions.activeId, id => { if (id) ui.showWorkspace() })

const pageMeta: Record<string, [string, string]> = {
  home: [t("首页"), t("连接 · 会话 · 概览")],
  workspace: [t("终端"), t("终端 · 会话 · 文件")],
  servers: [t("服务器管理"), t("节点 · 分组 · 在线状态")],
  tunnel: [t("SSH 隧道"), t("本地 / 远程 / 动态转发")],
  settings: [t("设置"), t("通用 · 连接说明 · 外观 · 安全 · 迁移")],
}

const pageTitle = computed(() => pageMeta[ui.view]?.[0] ?? t("工作区"))
const pageSub = computed(() => pageMeta[ui.view]?.[1] ?? '')

const onlineCount = computed(() => sessions.tabs.filter((t) => t.status === 'connected').length)

// 标签页位置：side 时并入左侧导航，顶栏标签栏隐藏
const sideTabs = computed(() => settings.tabBarPlacement === 'side' && sessions.tabs.length > 0)

const activeConnected = computed(() => {
  const tab = sessions.activeTab
  if (!tab || tab.status !== 'connected' || tab.kind === 'local') return undefined
  return tab
})

const {shellEl, toolRowEl, resizing, effectiveNavW, navMaxW, effectiveToolW, toolMaxW, toolVisible, NAV_W_MIN, TOOL_W_MIN, onNavResize, onToolResize, onNavDrag, onToolDrag, resetNavWidth, resetToolWidth} = useWorkspaceLayout(navCollapsed, activeConnected, computed(() => sessions.sftpVisible))

// ---- 分屏：槽位显隐与定位 ----
// 所有标签的 TerminalView 常驻（不随分屏卸载/重挂，避免重连/断会话）。
// 未分屏：仅 activeId 可见（现状 v-show 逻辑）。
// 分屏：仅「当前绑定到某叶子」的标签可见，并按叶子矩形定位。
const terminalArea = ref<HTMLElement | null>(null)
const paneSize = ref({w: 0, h: 0})

function measurePanes() {
  const el = terminalArea.value
  if (!el) return
  // getElementRect 返回「布局坐标系」尺寸（已除以 app-shell 的 zoom）。
  // 若直接取 getBoundingClientRect，WebKit 下量出的是含 zoom 的渲染尺寸（偏大 z 倍），
  // 而 slotStyle/SplitView 的绝对定位按未缩放坐标解释 → 分屏格底部/右侧会空一截。
  const r = getElementRect(el)
  paneSize.value = {w: r.width, h: r.height}
}

// 分屏 / 右侧工具面板切换会改变终端区宽度，等 DOM 更新后强制重测，避免布局沿用旧尺寸。
watch(
  () => [sessions.splitShown, sessions.sftpVisible] as const,
  async () => {
    await nextTick()
    requestAnimationFrame(() => measurePanes())
  },
)

let paneRO: ResizeObserver | null = null

onMounted(() => {
  measurePanes()
  if (terminalArea.value) {
    paneRO = new ResizeObserver(measurePanes)
    paneRO.observe(terminalArea.value)
  }
  // 窗口变窄时按可用空间重新收紧侧栏宽度（存的值保留，窗口变大后自动还原）

})

onBeforeUnmount(() => {
  paneRO?.disconnect()
  paneRO = null

})

/** 分屏布局（叶子 rect + 分隔条），由 App 统一计算供槽位与 SplitView 共用。 */
const paneLayout = computed(() => {
  const panes = sessions.panes
  if (!panes || !sessions.splitShown) return null
  return layoutPanes(panes, paneSize.value.w, paneSize.value.h)
})

/** 标签是否应显示：显示分屏时该标签需绑定在某叶子中；否则只有 activeId 可见。 */
function tabVisible(tabId: string): boolean {
  if (!sessions.splitShown) return tabId === sessions.activeId
  return sessions.leafByTabId(tabId) !== null
}

/** 该标签所在叶子 id（未显示分屏或不在格中返回 undefined）。 */
function paneIdForTab(tabId: string): string | undefined {
  if (!sessions.splitShown) return undefined
  return sessions.leafByTabId(tabId)?.id
}

/** 槽位定位：未分屏铺满全区域；分屏按叶子矩形（顶部让出格头高度）。 */
const PANE_HEADER_H = 28

function slotStyle(tabId: string) {
  if (!sessions.splitShown || !paneLayout.value) return {left: '0', top: '0', right: '0', bottom: '0'}
  const leaf = sessions.leafByTabId(tabId)
  if (!leaf) return {display: 'none'}
  const rect = paneLayout.value.leafRects.get(leaf.id)
  if (!rect) return {display: 'none'}
  return {
    left: rect.left + 'px',
    top: (rect.top + PANE_HEADER_H) + 'px',
    width: rect.width + 'px',
    height: Math.max(0, rect.height - PANE_HEADER_H) + 'px',
  }
}

/** 分屏下点击某格终端区域时，同步该格为焦点格（覆盖层已对终端区域穿透）。 */
function onPaneMousedown(tabId: string) {
  if (!sessions.splitShown) return
  const leaf = sessions.leafByTabId(tabId)
  if (leaf) sessions.setFocusedPane(leaf.id)
}

/** 拖拽标签到终端区域时的落点方向（用于像浏览器一样按区域分屏/改方向）。 */
const terminalDropDir = ref<SplitDirection | null>(null)

function terminalDropDirection(e: DragEvent): SplitDirection | null {
  const el = terminalArea.value
  if (!el) return null
  // 与 measurePanes 同理：落点判定需在未缩放（布局）坐标系中进行，
  // 否则 clientX/clientY（含 zoom）相对 rect（含 zoom）的分割偏移会被整体缩放。
  const rect = getElementRect(el)
  const dx = e.clientX - (rect.left + rect.width / 2)
  const dy = e.clientY - (rect.top + rect.height / 2)
  return Math.abs(dx) >= Math.abs(dy)
    ? (dx < 0 ? 'left' : 'right')
    : (dy < 0 ? 'up' : 'down')
}

function onTerminalDragOver(e: DragEvent) {
  if (!sessions.draggingTabId && !sessions.draggingSplitGroup) return
  e.preventDefault()
  terminalDropDir.value = terminalDropDirection(e)
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
}

function onTerminalDragLeave() {
  terminalDropDir.value = null
}

function onTerminalDrop(e: DragEvent) {
  e.preventDefault()
  const dir = terminalDropDirection(e)
  if (dir) {
    if (sessions.draggingSplitGroup) {
      const direction: PaneDirection = dir === 'left' || dir === 'right' ? 'row' : 'column'
      sessions.reorientSplit(direction)
    } else if (sessions.draggingTabId) {
      const sourceId = sessions.draggingTabId
      let targetId = sessions.activeId
      if (sourceId === targetId) {
        targetId = sessions.tabs.find((t) => t.clientId !== sourceId)?.clientId ?? ''
      }
      if (targetId) sessions.mergeTabsForSplit(sourceId, targetId, dir)
    }
  }
  terminalDropDir.value = null
}

const commands = computed(() => buildCommands({sessions, servers, ui}))

function openCmd() {
  ui.openCommandPalette()
}

function closeCmd() {
  ui.closeCommandPalette()
}

async function runCmd(command: AppCommand) {
  try {
    await command.run()
    commandStore.record(command.id)
  } catch (error) {
    console.error(`[command] ${command.id} failed`, error)
    toast.error(t("命令执行失败"), error instanceof Error ? error.message : errorText(error), `command:${command.id}`)
  }
}

const hotkeyById = (id: string) => {
  const definition = HOTKEYS.find((item) => item.id === id)
  if (!definition) throw new Error(`Missing hotkey definition: ${id}`)
  return definition
}

useHotkeys([
  {...hotkeyById('command-palette'), enabled: () => !needsUnlock.value, run: (event) => { event.preventDefault(); ui.cmdOpen ? closeCmd() : openCmd() }},
  {...hotkeyById('close-overlay'), run: () => { closeCmd(); ui.closeTerminalSidebar() }},
  {...hotkeyById('close-tab'), enabled: () => ui.view === 'workspace' && !!sessions.activeId, run: (event) => { event.preventDefault(); sessions.closeTab(sessions.activeId) }},
  ...Array.from({length: 9}, (_, index) => ({
    ...hotkeyById(`switch-tab-${index + 1}`),
    enabled: () => sessions.tabs.length > index,
    run: (event: KeyboardEvent) => { event.preventDefault(); sessions.activateTab(sessions.tabs[index].clientId) },
  })),
  {...hotkeyById('toggle-sidebar'), enabled: () => !needsUnlock.value, run: (event) => { event.preventDefault(); navCollapsed.value = !navCollapsed.value }},
  {...hotkeyById('toggle-context'), enabled: () => ui.view === 'workspace' && !!activeConnected.value, run: (event) => { event.preventDefault(); sessions.sftpVisible = !sessions.sftpVisible }},
])

// ---- 启动时恢复「上次关闭时仍在打开的服务器」 ----
// 工作区标签增删时（防抖）把当前打开的服务器 ID 写入设置；下次启动根据开关提示并恢复。
// restoreSettled 在启动恢复决策落定前为 false：避免启动瞬间（尚未提示）把历史列表误清空。
let restoreSettled = false
let lastServersSyncTimer: ReturnType<typeof setTimeout> | null = null

/** 当前打开的 SSH 服务器标签对应的节点 ID（保持标签顺序，重复标签 = 重复 ID）。 */
function currentOpenServerIds(): string[] {
  const ids: string[] = []
  for (const t of sessions.tabs) {
    if (t.kind === 'ssh' && t.node?.id) ids.push(t.node.id)
  }
  return ids
}

function sameIds(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false
  return true
}

/** 标签增删 → 防抖写入「上次打开的服务器列表」。 */
function scheduleSyncOpenServers() {
  if (!settings.loaded || !restoreSettled) return
  if (lastServersSyncTimer !== null) clearTimeout(lastServersSyncTimer)
  lastServersSyncTimer = setTimeout(() => {
    lastServersSyncTimer = null
    void syncOpenServers()
  }, 600)
}

async function syncOpenServers() {
  const ids = currentOpenServerIds()
  if (sameIds(ids, settings.lastOpenServers)) return
  await settings.setLastOpenServers(ids)
}

watch(() => sessions.tabs, scheduleSyncOpenServers, {deep: true})

const restorePromptVisible = ref(false)
const restorePromptCount = ref(0)

/**
 * 启动（或解锁）完成后调用：开关开启且有可恢复的服务器时弹确认框。
 * 已删除的服务器静默跳过；无任何可恢复项则不提示。
 * sessionStorage 标记保证「每次应用启动只提示一次」：开发时 HMR / 页面刷新不重复弹。
 */
async function maybeRestoreLastServers() {
  restoreSettled = true
  if (lastServersSyncTimer !== null) {
    clearTimeout(lastServersSyncTimer)
    lastServersSyncTimer = null
  }
  if (!settings.loaded || !settings.autoLoginLastServer) return
  try {
    if (sessionStorage.getItem(RESTORE_PROMPT_SESSION_KEY) === '1') return
  } catch {
    /* 存储不可用时忽略：本次仍提示 */
  }
  const ids = settings.lastOpenServers
  if (!ids.length) return
  const recoverable = ids.filter((id) => servers.servers.some((s) => s.id === id))
  if (!recoverable.length) return
  restorePromptCount.value = recoverable.length
  restorePromptVisible.value = true
}

/** 用户确认：按上次顺序逐个重新打开并连接（TerminalView 挂载即自动连接）。 */
function confirmRestore() {
  restorePromptVisible.value = false
  markRestorePromptSettled()
  ui.showWorkspace()
  for (const id of settings.lastOpenServers) {
    const node = servers.servers.find((s) => s.id === id)
    if (node) sessions.openTab(node)
  }
}

/** 用户取消：本次不恢复（历史列表保留，下次启动仍会提示）。 */
function cancelRestore() {
  restorePromptVisible.value = false
  markRestorePromptSettled()
}

/** 标记本次启动已处理过恢复提示（下次应用进程启动时自动重置）。 */
function markRestorePromptSettled() {
  try {
    sessionStorage.setItem(RESTORE_PROMPT_SESSION_KEY, '1')
  } catch {
    /* 存储不可用时忽略 */
  }
}

onMounted(async () => {
  const st = await getStartupSecurityStatus()
  initializationError.value = st.initializationError ?? ''
  needsUnlock.value = !st.unlocked || st.needsUnlock || !!initializationError.value
  if (!needsUnlock.value) {
    await settings.load()
    await servers.load()
    await maybeRestoreLastServers()
  }
  window.matchMedia?.('(prefers-color-scheme: light)').addEventListener?.('change', onSchemeChange)
})

async function doUnlock() {
  if (initializationError.value) return
  unlockError.value = ''
  unlocking.value = true
  try {
    await securityService.unlock(unlockPassword.value)
    needsUnlock.value = false
    unlockPassword.value = ''
    await settings.load()
    await servers.load()
    await maybeRestoreLastServers()
  } catch (e) {
    unlockError.value = errorText(e)
  } finally {
    unlocking.value = false
  }
}

function handleTogglePanel(panel: 'sftp' | 'sysinfo' | 'transfers') {
  if (!sessions.sftpVisible) {
    sessions.sftpVisible = true
    sessions.rightPanel = panel
  } else if (sessions.rightPanel === panel) {
    sessions.sftpVisible = false
  } else {
    sessions.rightPanel = panel
  }
}

onBeforeUnmount(() => {
  window.matchMedia?.('(prefers-color-scheme: light)').removeEventListener?.('change', onSchemeChange)
})
</script>

<template>
  <UpdatePanel />
  <div class="app-shell" :class="{'smooth-animations': settings.smoothAnimations, 'platform-macos': isMacOS, 'platform-windows': isWindows}" :style="{zoom: (settings.uiScale || 100) / 100}" data-ui="app" :data-view="ui.view">
    <AppTitleBar v-if="isMacOS || isWindows" :windows="isWindows" :online-count="onlineCount" />

    <!-- 独立终端窗口模式 -->
    <div v-if="detachedMode === 'detached-terminal'" class="w-full h-full bg-[#0f172a] overflow-hidden flex flex-col p-1">
      <div v-for="tab in sessions.tabs" :key="tab.clientId" class="flex-1 w-full h-full min-h-0">
        <TerminalView v-if="tab.clientId === detachedSession || tab.sessionId === detachedSession" :tab="tab" />
      </div>
    </div>

    <!-- 独立 SFTP 窗口模式 -->
    <div v-else-if="detachedMode === 'detached-sftp'" class="w-full h-full bg-[#0f172a] overflow-hidden flex flex-col p-2">
      <SftpPanel v-if="sessions.activeTab" :tab="sessions.activeTab" class="flex-1 w-full h-full min-h-0" />
    </div>

    <!-- 解锁页 -->
    <AppUnlock v-else-if="needsUnlock" v-model="unlockPassword" :unlocking="unlocking" :error="unlockError" :initialization-error="initializationError" @submit="doUnlock" />

    <!-- 主壳 -->
    <AppLayout
      v-else
      ref="shellEl"
      class="shell"
      :collapsed="navCollapsed"
      :resizing="resizing"
    >
      <template #sidebar>
        <AppSidebar v-model:collapsed="navCollapsed" :online-count="onlineCount" />
      </template>

      <!-- 导航宽度分隔条：拖动改宽度，双击复位；折叠态与快速连接面板打开时不显示 -->
      <template #resizer>
      <WidthResizer
        v-if="!navCollapsed && !ui.terminalSidebarOpen"
        side="left"
        :inset="`calc(${effectiveNavW}px - 4px)`"
        :width="effectiveNavW"
        :min="NAV_W_MIN"
        :max="navMaxW"
        :label="$t('导航栏')"
        @resize="onNavResize"
        @drag="onNavDrag"
        @reset="resetNavWidth"
      />
      </template>

      <template #topbar>
        <AppTopBar
          :title="pageTitle"
          :subtitle="pageSub"
          :nav-collapsed="navCollapsed"
          :workspace="ui.view === 'workspace'"
          :online-count="onlineCount"
          :context-visible="sessions.sftpVisible"
          :tab="sessions.activeTab"
          :right-panel="sessions.rightPanel"
          @expand-nav="navCollapsed = false"
          @command="openCmd"
          @toggle-context="handleTogglePanel('sftp')"
          @toggle-panel="handleTogglePanel"
        />
      </template>

      <div class="flex-1 min-h-0 flex">
          <section class="flex-1 min-w-0 min-h-0 overflow-hidden flex flex-col" data-ui="main">
            <ServerList v-show="ui.view === 'servers'" />
            <SettingsPage v-show="ui.view === 'settings'" />
            <TunnelPage v-show="ui.view === 'tunnel'" />

                  <WorkspaceHome
                    v-show="ui.view === 'home' || (ui.view === 'workspace' && !sessions.tabs.length)"
                    @quick-connect="ui.showWorkspace(); ui.openTerminalSidebar()"
                    @manage-servers="ui.showServers()"
                  />
            <TerminalWorkspace :active="ui.view === 'workspace' && sessions.tabs.length > 0">
              <TabBar v-if="sessions.tabs.length && !sideTabs" />

              <div ref="toolRowEl" class="flex-1 min-h-0 flex relative">
                <div
                  ref="terminalArea"
                  class="flex-1 min-w-0 relative terminal-bg terminal-drop"
                  data-ui="terminal"
                  :class="terminalDropDir ? 'drop-' + terminalDropDir : ''"
                  @dragover="onTerminalDragOver"
                  @dragleave="onTerminalDragLeave"
                  @drop="onTerminalDrop"
                >
                  <!-- 终端槽位：所有标签的 TerminalView 常驻；分屏时按叶子 rect 摆位 -->
                  <template v-for="tab in sessions.tabs" :key="tab.clientId">
                    <div
                      v-show="tabVisible(tab.clientId)"
                      class="terminal-slot"
                      :data-tab-id="tab.clientId"
                      :data-state="tab.status"
                      :class="paneIdForTab(tab.clientId) ? 'split' : ''"
                      :style="slotStyle(tab.clientId)"
                      @mousedown="onPaneMousedown(tab.clientId)"
                    >
                      <TerminalView :tab="tab" :pane-id="paneIdForTab(tab.clientId)" />
                    </div>
                  </template>

                  <!-- 分屏覆盖层：格头 + 分隔条 + 拖放热区 -->
                  <SplitView v-if="sessions.splitShown" :layout="paneLayout!" />


                </div>

                <SftpPanel
                  v-if="activeConnected && sessions.sftpVisible"
                  v-show="sessions.rightPanel === 'sftp'"
                  :key="activeConnected.clientId"
                  :tab="activeConnected"
                />
                <SysInfoPanel
                  v-if="activeConnected && sessions.sftpVisible && sessions.rightPanel === 'sysinfo'"
                  :tab="activeConnected"
                />
                <!-- 传输记录：上传 / 下载历史（下载可从记录里直接打开本地文件夹）。
                     与 SFTP 面板同为常驻挂载 + v-show 切换，避免切换页签时重建面板
                     （重建会丢掉「进行中的传输」进度行）。 -->
                <TransferHistoryPanel
                  v-if="activeConnected && sessions.sftpVisible"
                  v-show="sessions.rightPanel === 'transfers'"
                />

                <!-- 右侧面板宽度分隔条：SFTP / 看板 / 传输记录 共用一个宽度 -->
                <WidthResizer
                  v-if="toolVisible && !ui.terminalSidebarOpen"
                  side="right"
                  :inset="`calc(${effectiveToolW}px - 4px)`"
                  :width="effectiveToolW"
                  :min="TOOL_W_MIN"
                  :max="toolMaxW"
                  :label="$t('右侧面板')"
                  @resize="onToolResize"
                  @drag="onToolDrag"
                  @reset="resetToolWidth"
                />
              </div>
            </TerminalWorkspace>
          </section>
      </div>

      <template #statusbar>
        <ServerStatusBar :tab="sessions.activeTab" />
      </template>
    </AppLayout>

    <QuickConnectPanel v-if="!needsUnlock" />

    <!-- 启动恢复确认框 -->
    <Dialog :close-on-overlay="false" :close-on-escape="false" :show-close="false" v-model="restorePromptVisible" :title="$t('恢复上次的会话')" :width="460" @close="cancelRestore">
      <p class="text-[12.5px] text-mist leading-relaxed">{{ $t('检测到上次关闭应用时有') }} {{ restorePromptCount }} {{ $t('台服务器仍在连接，是否自动重新连接？') }}</p>
      <template #footer>
        <Button variant="ghost" size="sm" @click="cancelRestore">{{ $t('取消') }}</Button>
        <Button variant="primary" size="sm" @click="confirmRestore">{{ $t('自动连接') }}</Button>
      </template>
    </Dialog>

    <CommandPalette
      v-model="ui.cmdOpen"
      :commands="commands"
      :recent-ids="commandStore.recentIds"
      @close="closeCmd"
      @run="runCmd"
      @clear-recent="commandStore.clear()"
    />
    <ToastProvider />
  </div>
</template>
