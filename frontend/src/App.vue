<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import Icon from './components/Icon.vue'
import ServerList from './components/ServerList.vue'
import SettingsPage from './components/SettingsPage.vue'
import SftpPanel from './components/SftpPanel.vue'
import SysInfoPanel from './components/SysInfoPanel.vue'
import ServerStatusBar from './components/ServerStatusBar.vue'
import TabBar from './components/TabBar.vue'
import TerminalView from './components/TerminalView.vue'
import SplitView from './components/SplitView.vue'
import {layoutPanes} from './panes/layout'
import type {PaneDirection, SplitDirection} from './types'
import TunnelPage from './components/TunnelPage.vue'
import QuickConnectPanel from './components/QuickConnectPanel.vue'
import {securityService} from './services/security'
import {getElementRect} from './utils/dom'
import {useServersStore} from './stores/servers'
import {useSessionsStore} from './stores/sessions'
import {useSettingsStore} from './stores/settings'
import {useUIStore} from './stores/ui'
import {computeUiCssVars, applyRootTheme, clearRootTheme, resolveTone, type Tone} from './theme/engine'
import {defaultPreset, paletteToTheme, presetById} from './theme/presets'

const sessions = useSessionsStore()
const ui = useUIStore()
const settings = useSettingsStore()
const servers = useServersStore()

// 系统明暗监听：auto 模式下跟随 prefers-color-scheme
const systemTone = ref<Tone>(resolveTone('auto'))

// 当前实际明暗模式（auto → 跟随系统）
const tone = computed<Tone>(() => {
  if (settings.appearance.baseTone !== 'auto') return settings.appearance.baseTone
  return systemTone.value
})

// 根节点注入的 CSS 变量（品牌色 + 字体栈）
const cssVars = computed(() => computeUiCssVars(settings.appearance, settings.fonts, tone.value))

// 预设模式下，明暗切换自动同步对应终端色板
watch(tone, (t) => {
  if (settings.appearance.mode !== 'preset') return
  const preset = presetById(settings.appearance.presetId) ?? defaultPreset()
  void settings.setTheme(paletteToTheme(t === 'dark' ? preset.dark : preset.light))
})

function onSchemeChange() {
  systemTone.value = resolveTone('auto')
}

// 同步主题到 <html>：Teleport 到 body 的浮层（快速连接侧栏、对话框等）才能继承变量
watch([tone, cssVars], ([t, vars]) => {
  applyRootTheme(t, vars)
}, {immediate: true})

const needsUnlock = ref(false)
const unlockPassword = ref('')
const unlockError = ref('')
const unlocking = ref(false)
const cmdQuery = ref('')
const cmdIndex = ref(0)
const cmdInput = ref<HTMLInputElement>()

const pageMeta: Record<string, [string, string]> = {
  workspace: ['工作区', '终端 · 会话 · 文件'],
  servers: ['服务器管理', '节点 · 分组 · 在线状态'],
  tunnel: ['SSH 隧道', '本地 / 远程 / 动态转发'],
  settings: ['设置', '通用 · 主题 · 安全 · 迁移'],
}

const pageTitle = computed(() => pageMeta[ui.view]?.[0] ?? '工作区')
const pageSub = computed(() => pageMeta[ui.view]?.[1] ?? '')

const onlineCount = computed(() => sessions.tabs.filter((t) => t.status === 'connected').length)

const activeConnected = computed(() => {
  const tab = sessions.activeTab
  if (!tab || tab.status !== 'connected' || tab.kind === 'local') return undefined
  return tab
})

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
  () => [sessions.isSplit, sessions.sftpVisible] as const,
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
})

onBeforeUnmount(() => {
  paneRO?.disconnect()
  paneRO = null
})

/** 分屏布局（叶子 rect + 分隔条），由 App 统一计算供槽位与 SplitView 共用。 */
const paneLayout = computed(() => {
  if (!sessions.panes) return null
  return layoutPanes(sessions.panes, paneSize.value.w, paneSize.value.h)
})

/** 标签是否应显示：未分屏时 activeId；分屏时该标签需绑定在某叶子中。 */
function tabVisible(tabId: string): boolean {
  if (!sessions.panes) return tabId === sessions.activeId
  return sessions.leafByTabId(tabId) !== null
}

/** 该标签所在叶子 id（未分屏或不在格中返回 undefined）。 */
function paneIdForTab(tabId: string): string | undefined {
  if (!sessions.panes) return undefined
  return sessions.leafByTabId(tabId)?.id
}

/** 槽位定位：未分屏铺满全区域；分屏按叶子矩形（顶部让出格头高度）。 */
const PANE_HEADER_H = 28

function slotStyle(tabId: string) {
  if (!sessions.panes || !paneLayout.value) return {left: '0', top: '0', right: '0', bottom: '0'}
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
  if (!sessions.panes) return
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

interface CmdItem {
  id: string
  label: string
  hint: string
  run: () => void
}

const cmdItems = computed<CmdItem[]>(() => {
  const items: CmdItem[] = [
    {id: 'workspace', label: '打开工作区', hint: '导航', run: () => ui.showWorkspace()},
    {id: 'servers', label: '打开服务器管理', hint: '导航', run: () => ui.showServers()},
    {id: 'tunnel', label: '打开隧道页', hint: '导航', run: () => ui.showTunnel()},
    {id: 'settings', label: '打开设置', hint: '导航', run: () => ui.showSettings()},
    {id: 'new', label: '新建服务器', hint: '操作', run: () => ui.requestNewServer()},
    {id: 'local', label: '打开本地终端', hint: '工作区', run: () => { ui.showWorkspace(); sessions.openLocalTab() }},
  ]
  if (sessions.sftpVisible) {
    items.push({id: 'hide-tool', label: '收起侧栏工具', hint: '工作区', run: () => { sessions.sftpVisible = false }})
  } else {
    items.push({id: 'show-sftp', label: '打开 SFTP', hint: '工作区', run: () => sessions.showRightPanel('sftp')})
    items.push({id: 'show-sys', label: '打开系统看板', hint: '工作区', run: () => sessions.showRightPanel('sysinfo')})
  }
  for (const s of servers.servers) {
    items.push({
      id: 'connect-' + s.id,
      label: '连接 ' + (s.name || `${s.user}@${s.host}`),
      hint: '工作区',
      run: () => {
        ui.showWorkspace()
        sessions.openTab(s)
      },
    })
  }
  const q = cmdQuery.value.trim().toLowerCase()
  if (!q) return items
  return items.filter((x) => x.label.toLowerCase().includes(q) || x.hint.toLowerCase().includes(q))
})

function openCmd() {
  cmdQuery.value = ''
  cmdIndex.value = 0
  ui.openCommandPalette()
  requestAnimationFrame(() => cmdInput.value?.focus())
}

function closeCmd() {
  ui.closeCommandPalette()
}

function runCmd(item?: CmdItem) {
  const target = item ?? cmdItems.value[cmdIndex.value]
  closeCmd()
  target?.run()
}

function onGlobalKeydown(e: KeyboardEvent) {
  const meta = e.metaKey || e.ctrlKey
  if (meta && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    if (!needsUnlock.value) {
      if (ui.cmdOpen) closeCmd()
      else openCmd()
    }
    return
  }
  if (e.key === 'Escape') {
    closeCmd()
    if (ui.terminalSidebarOpen) ui.closeTerminalSidebar()
    return
  }
  if (ui.cmdOpen) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      cmdIndex.value = cmdItems.value.length ? (cmdIndex.value + 1) % cmdItems.value.length : 0
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      cmdIndex.value = cmdItems.value.length
        ? (cmdIndex.value - 1 + cmdItems.value.length) % cmdItems.value.length
        : 0
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      runCmd()
    }
    return
  }
  if (!meta) return
  if (e.key === 'w' && ui.view === 'workspace') {
    e.preventDefault()
    if (sessions.activeId) sessions.closeTab(sessions.activeId)
  }
  const n = parseInt(e.key)
  if (n >= 1 && n <= 9 && sessions.tabs.length >= n) {
    e.preventDefault()
    sessions.activeId = sessions.tabs[n - 1].clientId
  }
}

onMounted(async () => {
  try {
    const st = await securityService.getStatus()
    needsUnlock.value = st.needsUnlock
  } catch {
    needsUnlock.value = false
  }
  if (!needsUnlock.value) {
    void settings.load()
    void servers.load()
  }
  window.addEventListener('keydown', onGlobalKeydown)
  window.matchMedia?.('(prefers-color-scheme: light)').addEventListener?.('change', onSchemeChange)
})

async function doUnlock() {
  unlockError.value = ''
  unlocking.value = true
  try {
    await securityService.unlock(unlockPassword.value)
    needsUnlock.value = false
    unlockPassword.value = ''
    await settings.load()
    await servers.load()
  } catch (e) {
    unlockError.value = String(e)
  } finally {
    unlocking.value = false
  }
}

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
  window.matchMedia?.('(prefers-color-scheme: light)').removeEventListener?.('change', onSchemeChange)
  clearRootTheme()
})
</script>

<template>
  <div class="app-shell" :style="{zoom: (settings.uiScale || 100) / 100}">
    <!-- 解锁页 -->
    <div v-if="needsUnlock" class="absolute inset-0 z-50 grid place-items-center p-8">
      <div class="w-full max-w-[920px] grid grid-cols-1 md:grid-cols-2 gap-8 items-center">
        <div class="relative h-[280px] md:h-[420px] grid place-items-center" aria-hidden="true">
          <div
            class="absolute w-[280px] h-[280px] rounded-full"
            style="background: radial-gradient(circle, var(--signal-glow), transparent 68%); animation: pulseSoft 4.5s ease-in-out infinite"
          ></div>
          <svg width="340" height="300" viewBox="0 0 340 300" fill="none">
            <defs>
              <linearGradient id="gRing" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0%" stop-color="var(--signal-400)"/>
                <stop offset="100%" stop-color="var(--copper-400)"/>
              </linearGradient>
              <linearGradient id="gBody" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#2a3444"/>
                <stop offset="100%" stop-color="#121820"/>
              </linearGradient>
            </defs>
            <circle cx="170" cy="150" r="118" stroke="url(#gRing)" stroke-opacity="0.18" stroke-width="1"/>
            <circle cx="170" cy="150" r="96" stroke="url(#gRing)" stroke-opacity="0.28" stroke-width="1.2" stroke-dasharray="4 8"/>
            <circle cx="170" cy="150" r="72" stroke="var(--signal-400)" stroke-opacity="0.35" stroke-width="1.5"/>
            <rect x="118" y="98" width="104" height="104" rx="22" fill="url(#gBody)" stroke="rgba(255,255,255,0.12)" stroke-width="1"/>
            <rect x="128" y="108" width="84" height="56" rx="10" fill="#0a0e14" stroke="var(--signal-border)"/>
            <path d="M138 122h28M138 132h44M138 142h34" stroke="var(--signal-400)" stroke-width="1.6" stroke-linecap="round" opacity="0.85"/>
            <path d="M170 168v18" stroke="var(--copper-400)" stroke-width="3" stroke-linecap="round"/>
            <circle cx="170" cy="196" r="10" fill="var(--copper-500)" stroke="var(--copper-300)" stroke-width="1.5"/>
            <circle cx="170" cy="196" r="3.5" fill="#1a1008"/>
            <path d="M214 120c14 8 22 22 22 38s-8 30-22 38" stroke="var(--signal-400)" stroke-width="1.5" stroke-linecap="round" opacity="0.5"/>
            <path d="M126 120c-14 8-22 22-22 38s8 30 22 38" stroke="var(--copper-400)" stroke-width="1.5" stroke-linecap="round" opacity="0.4"/>
            <circle cx="78" cy="86" r="4" fill="var(--signal-400)" opacity="0.7"/>
            <circle cx="268" cy="210" r="3.5" fill="var(--copper-400)" opacity="0.7"/>
          </svg>
        </div>
        <div class="neo p-8 max-w-[400px] w-full mx-auto">
          <div class="flex items-center gap-3 mb-5">
            <div class="brand-mark">
              <Icon name="zap" :size="22" extra-class="text-signal" />
            </div>
            <div>
              <div class="brand-name">ding<span>-ssh</span></div>
              <div class="text-[12px] tracking-widest text-mist">SIGNAL DESK</div>
            </div>
          </div>
          <h1 class="text-[28px] font-semibold tracking-tight text-white leading-tight mb-2">解锁工作台</h1>
          <p class="text-[13px] leading-relaxed text-mist mb-6">
            主密码已启用。输入后解密服务器节点、凭证与隧道配置，进入本机会话。
          </p>
          <form class="flex flex-col gap-4" @submit.prevent="doUnlock">
            <div class="field">
              <label for="masterPwd">主密码</label>
              <input
                id="masterPwd"
                v-model="unlockPassword"
                class="input"
                type="password"
                placeholder="输入主密码"
                autofocus
              />
            </div>
            <p class="text-[12px] text-danger min-h-4">{{ unlockError }}</p>
            <button class="btn btn-primary w-full" style="height:42px" type="submit" :disabled="unlocking || !unlockPassword">
              {{ unlocking ? '解密中…' : '解锁并进入' }}
            </button>
            <div class="flex justify-between items-center text-[12px] text-mist">
              <span>AES-256-GCM · Keyring</span>
            </div>
          </form>
        </div>
      </div>
    </div>

    <!-- 主壳 -->
    <div v-else class="flex-1 min-h-0 grid" style="grid-template-columns: var(--rail-w) 1fr">
      <aside class="rail" aria-label="主导航">
        <button class="rail-logo" title="ding-ssh" aria-label="首页" @click="ui.showWorkspace()">
          <Icon name="zap" :size="20" extra-class="text-signal" />
        </button>
        <nav class="flex flex-col gap-1.5 w-full items-center flex-1">
          <button
            class="rail-btn"
            :class="ui.view === 'workspace' ? 'active' : ''"
            title="工作区"
            @click="ui.showWorkspace()"
          >
            <Icon name="activity" :size="18" />
          </button>
          <button
            class="rail-btn"
            :class="ui.view === 'servers' ? 'active' : ''"
            title="服务器管理"
            @click="ui.showServers()"
          >
            <Icon name="server" :size="18" />
          </button>
          <button
            class="rail-btn"
            :class="ui.view === 'tunnel' ? 'active' : ''"
            title="隧道"
            @click="ui.showTunnel()"
          >
            <Icon name="tunnel" :size="18" />
          </button>
          <button
            class="rail-btn"
            :class="ui.view === 'settings' ? 'active' : ''"
            title="设置"
            @click="ui.showSettings()"
          >
            <Icon name="settings" :size="18" />
          </button>
        </nav>
        <div class="mt-auto flex flex-col gap-1.5 items-center">
          <button class="rail-btn" title="命令面板 ⌘K" @click="openCmd">
            <Icon name="command" :size="18" />
          </button>
        </div>
      </aside>

      <div class="min-w-0 min-h-0 flex flex-col">
        <header class="titlebar">
          <h2>{{ pageTitle }}</h2>
          <span class="sub">{{ pageSub }}</span>
          <div class="ml-auto flex items-center gap-2">
            <span v-if="ui.view === 'workspace'" class="chip">
              <span class="dot"></span>
              {{ onlineCount }} 会话在线
            </span>
            <button
              v-if="ui.view === 'workspace'"
              class="btn btn-ghost btn-sm"
              @click="sessions.sftpVisible = !sessions.sftpVisible"
            >
              <Icon name="panel-right" :size="14" />
              侧栏工具
            </button>
            <button
              v-if="ui.view === 'workspace'"
              class="btn btn-ghost btn-sm"
              title="打开本机终端"
              @click="sessions.openLocalTab()"
            >
              <Icon name="terminal" :size="14" />
              本地终端
            </button>
            <button
              v-if="ui.view === 'workspace'"
              class="btn btn-copper btn-sm"
              @click="ui.requestNewServer()"
            >
              <Icon name="plus" :size="14" />
              新建服务器
            </button>
          </div>
        </header>

        <div class="flex-1 min-h-0 flex">
          <main class="flex-1 min-w-0 flex flex-col fade-rise">
            <ServerList v-show="ui.view === 'servers'" />
            <SettingsPage v-show="ui.view === 'settings'" />
            <TunnelPage v-show="ui.view === 'tunnel'" />

            <div v-show="ui.view === 'workspace'" class="flex-1 min-h-0 flex flex-col">
              <TabBar v-if="sessions.tabs.length" />

              <div class="flex-1 min-h-0 flex">
                <div
                  ref="terminalArea"
                  class="flex-1 min-w-0 relative terminal-bg terminal-drop"
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
                      :class="paneIdForTab(tab.clientId) ? 'split' : ''"
                      :style="slotStyle(tab.clientId)"
                      @mousedown="onPaneMousedown(tab.clientId)"
                    >
                      <TerminalView :tab="tab" :pane-id="paneIdForTab(tab.clientId)" />
                    </div>
                  </template>

                  <!-- 分屏覆盖层：格头 + 分隔条 + 拖放热区 -->
                  <SplitView v-if="sessions.panes" :layout="paneLayout!" />

                  <div v-if="!sessions.tabs.length" class="empty">
                    <div class="empty-inner">
                      <svg class="empty-art" viewBox="0 0 280 160" fill="none" aria-hidden="true">
                        <defs>
                          <linearGradient id="eg1" x1="0" y1="0" x2="1" y2="1">
                            <stop stop-color="var(--signal-400)" stop-opacity="0.5"/>
                            <stop offset="1" stop-color="var(--copper-400)" stop-opacity="0.4"/>
                          </linearGradient>
                        </defs>
                        <rect x="40" y="36" width="200" height="100" rx="16" fill="var(--ink-850)" stroke="url(#eg1)" stroke-width="1.2"/>
                        <rect x="52" y="50" width="176" height="52" rx="8" fill="#0a0e14"/>
                        <path d="M64 64h40M64 76h72M64 88h56" stroke="var(--signal-400)" stroke-width="1.5" stroke-linecap="round" opacity="0.45"/>
                        <circle cx="220" cy="118" r="18" fill="none" stroke="var(--copper-400)" stroke-width="1.5" stroke-dasharray="3 4" opacity="0.7"/>
                        <path d="M214 118h12M220 112v12" stroke="var(--copper-400)" stroke-width="1.5" stroke-linecap="round"/>
                      </svg>
                      <h3>尚未打开会话</h3>
                      <p>点击左侧箭头展开服务器列表快速连接，或在服务器管理页新建节点。连接后终端、SFTP 与系统看板将同步就绪。</p>
                      <div class="flex gap-2">
                        <button class="btn btn-primary" @click="ui.openTerminalSidebar()">
                          <Icon name="panel-left" :size="14" />
                          打开服务器列表
                        </button>
                      </div>
                    </div>
                  </div>
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
              </div>

              <ServerStatusBar :tab="sessions.activeTab" />
            </div>
          </main>
        </div>
      </div>
    </div>

    <QuickConnectPanel />

    <!-- 命令面板 -->
    <div v-if="ui.cmdOpen" class="modal-root" @click.self="closeCmd">
      <div class="modal neo" style="width:min(520px,100%);padding:12px;">
        <div class="search mb-2">
          <Icon name="search" :size="14" extra-class="search-ico" />
          <input
            ref="cmdInput"
            v-model="cmdQuery"
            class="input"
            placeholder="连接服务器、打开隧道、跳转设置…"
            @input="cmdIndex = 0"
          />
        </div>
        <div class="max-h-72 overflow-y-auto">
          <button
            v-for="(item, i) in cmdItems"
            :key="item.id"
            class="w-full grid grid-cols-[1fr_auto] gap-2 items-center px-2.5 py-2 rounded-[6px] font-mono text-xs text-left"
            :class="i === cmdIndex ? 'bg-[var(--signal-weak)] shadow-[inset_0_0_0_1px_var(--signal-border)]' : 'hover:bg-[var(--hover)]'"
            @mouseenter="cmdIndex = i"
            @click="runCmd(item)"
          >
            <span>{{ item.label }}</span>
            <span class="text-mist font-sans text-[12px]">{{ item.hint }}</span>
          </button>
          <p v-if="!cmdItems.length" class="px-2.5 py-4 text-xs text-mist text-center">无匹配命令</p>
        </div>
      </div>
    </div>
  </div>
</template>
