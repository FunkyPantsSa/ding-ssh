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
import type {NavSectionKey, PaneDirection, SplitDirection, Theme} from './types'
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

// 左侧导航折叠状态：同时把导航宽度写到 <html>，供 Teleport 到 body 的浮层
// （快速连接侧栏、遮罩等）正确定位。
const NAV_COLLAPSED_KEY = 'ding-ssh:nav-collapsed'

function readNavCollapsed(): boolean {
  try {
    return localStorage.getItem(NAV_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

const navCollapsed = ref(readNavCollapsed())
watch(
  navCollapsed,
  (v) => {
    try {
      localStorage.setItem(NAV_COLLAPSED_KEY, v ? '1' : '0')
    } catch {
      // 存储不可用时忽略：折叠状态仅影响本次会话
    }
    document.documentElement.style.setProperty(
      '--nav-w',
      v ? 'var(--nav-w-collapsed)' : 'var(--nav-w-expanded)',
    )
  },
  {immediate: true},
)

// 系统明暗监听：auto 模式下跟随 prefers-color-scheme
const systemTone = ref<Tone>(resolveTone('auto'))

// 当前实际明暗模式（auto → 跟随系统）
const tone = computed<Tone>(() => {
  if (settings.appearance.baseTone !== 'auto') return settings.appearance.baseTone
  return systemTone.value
})

// 根节点注入的 CSS 变量（品牌色 + 字体栈）
const cssVars = computed(() => computeUiCssVars(settings.appearance, settings.fonts, tone.value))

// 预设色板覆盖的字段（背景 / 前景 / 光标 / 选中 / ANSI 16 色）
const PALETTE_KEYS = [
  'background', 'foreground', 'cursor', 'selection',
  'black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow',
  'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite',
] as const

function paletteSignature(t: Theme): string {
  return PALETTE_KEYS.map((k) => t[k]).join('|')
}

/** 当前明暗 + 预设对应的终端色板（其余字段保留用户设置：背景图 / 模糊 / 阴影等）。 */
function presetPaletteTheme(base: Theme, t: Tone): Theme {
  const preset = presetById(settings.appearance.presetId) ?? defaultPreset()
  const palette = paletteToTheme(t === 'dark' ? preset.dark : preset.light)
  const next: Theme = {...base}
  for (const key of PALETTE_KEYS) next[key] = palette[key]
  return next
}

/**
 * 预设模式下终端色板跟随界面明暗。
 * 色板一致时不写回（避免每次启动都产生一次无谓保存）。
 */
function syncTerminalPalette() {
  if (!settings.loaded || settings.appearance.mode !== 'preset') return
  const target = presetPaletteTheme(settings.theme, tone.value)
  if (paletteSignature(target) === paletteSignature(settings.theme)) return
  void settings.setTheme(target)
}

// settings.loaded 变为 true（设置读取完成）与界面明暗变化时各同步一次：
// 启动时若系统是浅色、存量终端色板却是深色，这里会把终端切到浅色色板，
// 避免出现「浅色界面 + 深色终端」的割裂。
watch([tone, () => settings.loaded], syncTerminalPalette, {immediate: true})

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

// 标签页位置：side 时并入左侧导航，顶栏标签栏隐藏
const sideTabs = computed(() => settings.tabBarPlacement === 'side' && sessions.tabs.length > 0)

// 左侧导航区段（导航 / 会话 / 标签页）按用户配置的顺序渲染；「标签页」仅在侧栏模式下存在
const navSections = computed<NavSectionKey[]>(() => {
  const order = settings.navSectionOrder.split(',').filter(Boolean) as NavSectionKey[]
  return order.filter((k) => k !== 'tabs' || sideTabs.value)
})

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
    sessions.activateTab(sessions.tabs[n - 1].clientId)
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
    <div v-if="needsUnlock" class="absolute inset-0 z-50 grid place-items-center overflow-auto p-8">
      <div class="w-full max-w-[880px] grid grid-cols-1 md:grid-cols-[1.05fr_1fr] gap-10 items-center">
        <section class="hidden md:flex flex-col gap-5">
          <div class="flex items-center gap-3">
            <div class="brand-mark">
              <Icon name="zap" :size="18" extra-class="text-signal" />
            </div>
            <div>
              <div class="brand-name">ding<span>-ssh</span></div>
              <div class="text-[10.5px] tracking-[0.16em] uppercase text-mist mt-1">Grid Desk</div>
            </div>
          </div>
          <h1 class="text-[30px] font-semibold tracking-[-0.03em] leading-[1.15] text-[var(--mist-100)]">
            解锁工作台
          </h1>
          <p class="text-[13px] leading-relaxed text-mist max-w-[380px]">
            主密码已启用。输入后解密服务器节点、凭证与隧道配置，进入本机会话。
          </p>
          <div class="panel p-4 flex flex-col gap-3 max-w-[400px]">
            <div class="flex items-baseline justify-between gap-4">
              <span class="text-[10.5px] uppercase tracking-[0.1em] text-mist">加密算法</span>
              <span class="font-mono text-[12px] text-[var(--mist-200)]">AES-256-GCM</span>
            </div>
            <div class="flex items-baseline justify-between gap-4">
              <span class="text-[10.5px] uppercase tracking-[0.1em] text-mist">密钥派生</span>
              <span class="font-mono text-[12px] text-[var(--mist-200)]">Argon2id</span>
            </div>
            <div class="flex items-baseline justify-between gap-4">
              <span class="text-[10.5px] uppercase tracking-[0.1em] text-mist">密钥存储</span>
              <span class="font-mono text-[12px] text-[var(--mist-200)]">OS Keyring</span>
            </div>
          </div>
        </section>

        <form class="panel p-6 flex flex-col gap-4" @submit.prevent="doUnlock">
          <div class="md:hidden flex items-center gap-3">
            <div class="brand-mark">
              <Icon name="zap" :size="18" extra-class="text-signal" />
            </div>
            <div class="brand-name">ding<span>-ssh</span></div>
          </div>
          <div>
            <h2 class="text-[17px] font-semibold text-[var(--mist-100)]">输入主密码</h2>
            <p class="text-[12px] text-mist mt-1">解密本地保险库后进入工作台。</p>
          </div>
          <div class="field">
            <label for="masterPwd">主密码</label>
            <input
              id="masterPwd"
              v-model="unlockPassword"
              class="input"
              type="password"
              placeholder="输入主密码"
              autocomplete="current-password"
              autofocus
            />
          </div>
          <p class="text-[12px] text-danger min-h-4" role="alert">{{ unlockError }}</p>
          <button class="btn btn-primary w-full" style="height:36px" type="submit" :disabled="unlocking || !unlockPassword">
            {{ unlocking ? '解密中…' : '解锁并进入' }}
          </button>
        </form>
      </div>
    </div>

    <!-- 主壳 -->
    <div v-else class="shell" :class="navCollapsed ? 'nav-collapsed' : ''">
      <aside class="nav" aria-label="主导航">
        <div class="nav-brand">
          <button
            class="nav-mark"
            :title="navCollapsed ? '展开导航' : '返回工作区'"
            :aria-label="navCollapsed ? '展开导航' : '返回工作区'"
            @click="navCollapsed ? (navCollapsed = false) : ui.showWorkspace()"
          >
            <Icon name="zap" :size="15" />
          </button>
          <span class="nav-word">ding<span>-ssh</span></span>
          <button
            class="btn-icon btn-sm ml-auto"
            :title="navCollapsed ? '展开导航' : '收起导航'"
            :aria-expanded="!navCollapsed"
            aria-label="收起或展开导航"
            @click="navCollapsed = !navCollapsed"
          >
            <Icon name="chevrons-left" :size="14" />
          </button>
        </div>

        <!-- 三个区段按「设置 → 左侧导航区段顺序」排列：导航 / 会话 / 标签页 -->
        <nav class="nav-body">
          <section v-for="sec in navSections" :key="sec" class="nav-sec">
            <template v-if="sec === 'nav'">
              <div class="nav-label">导航</div>
              <button
                class="nav-item"
                :class="ui.view === 'workspace' ? 'active' : ''"
                :aria-current="ui.view === 'workspace' ? 'page' : undefined"
                title="工作区"
                @click="ui.showWorkspace()"
              >
                <Icon name="terminal" :size="15" extra-class="nav-ico" />
                <span class="nav-text">工作区</span>
                <span v-if="onlineCount" class="nav-count">{{ onlineCount }}</span>
              </button>
              <button
                class="nav-item"
                :class="ui.view === 'servers' ? 'active' : ''"
                :aria-current="ui.view === 'servers' ? 'page' : undefined"
                title="服务器管理"
                @click="ui.showServers()"
              >
                <Icon name="server" :size="15" extra-class="nav-ico" />
                <span class="nav-text">服务器</span>
                <span v-if="servers.servers.length" class="nav-count">{{ servers.servers.length }}</span>
              </button>
              <button
                class="nav-item"
                :class="ui.view === 'tunnel' ? 'active' : ''"
                :aria-current="ui.view === 'tunnel' ? 'page' : undefined"
                title="SSH 隧道"
                @click="ui.showTunnel()"
              >
                <Icon name="tunnel" :size="15" extra-class="nav-ico" />
                <span class="nav-text">隧道</span>
              </button>
              <button
                class="nav-item"
                :class="ui.view === 'settings' ? 'active' : ''"
                :aria-current="ui.view === 'settings' ? 'page' : undefined"
                title="设置"
                @click="ui.showSettings()"
              >
                <Icon name="settings" :size="15" extra-class="nav-ico" />
                <span class="nav-text">设置</span>
              </button>
            </template>

            <template v-else-if="sec === 'sessions'">
              <div class="nav-label">会话</div>
              <button class="nav-item" title="本地终端" @click="ui.showWorkspace(); sessions.openLocalTab()">
                <Icon name="monitor" :size="15" extra-class="nav-ico" />
                <span class="nav-text">本地终端</span>
              </button>
              <button
                class="nav-item"
                title="快速连接"
                :class="ui.terminalSidebarOpen && ui.view === 'workspace' ? 'active' : ''"
                :aria-expanded="ui.terminalSidebarOpen && ui.view === 'workspace'"
                @click="ui.toggleQuickConnect()"
              >
                <Icon name="panel-left" :size="15" extra-class="nav-ico" />
                <span class="nav-text">快速连接</span>
              </button>
            </template>

            <!-- 标签页并入左侧导航：纵向列表，与顶部标签栏互斥 -->
            <template v-else>
              <TabBar side />
            </template>
          </section>
        </nav>

        <div class="nav-foot">
          <button class="nav-item" title="命令面板 ⌘K" @click="openCmd">
            <Icon name="command" :size="15" extra-class="nav-ico" />
            <span class="nav-text">命令面板</span>
            <span class="kbd nav-kbd">⌘K</span>
          </button>
        </div>
      </aside>

      <div class="shell-main">
        <header class="topbar">
          <button
            v-if="navCollapsed"
            class="btn-icon"
            title="展开导航"
            aria-label="展开导航"
            @click="navCollapsed = false"
          >
            <Icon name="chevrons-right" :size="15" />
          </button>
          <h1>{{ pageTitle }}</h1>
          <span class="topbar-sep"></span>
          <span class="topbar-sub">{{ pageSub }}</span>

          <div class="flex-1 min-w-0 flex justify-center px-2">
            <button class="searchbtn" style="max-width: 400px" title="打开命令面板（⌘K）" @click="openCmd">
              <Icon name="search" :size="14" />
              <span class="grow">搜索服务器、隧道与命令…</span>
              <span class="kbd">⌘K</span>
            </button>
          </div>

          <div class="flex items-center gap-1.5 shrink-0">
            <span v-if="ui.view === 'workspace'" class="chip">
              <span class="dot"></span>
              {{ onlineCount }} 会话在线
            </span>
            <button
              v-if="ui.view === 'workspace'"
              class="btn-icon"
              :class="sessions.sftpVisible ? 'text-[var(--signal-300)]' : ''"
              title="侧栏工具（SFTP / 系统看板）"
              :aria-pressed="sessions.sftpVisible"
              @click="sessions.sftpVisible = !sessions.sftpVisible"
            >
              <Icon name="panel-right" :size="16" />
            </button>
            <button
              v-if="ui.view === 'workspace' || ui.view === 'servers'"
              class="btn btn-ghost btn-sm"
              title="新建服务器"
              @click="ui.requestNewServer()"
            >
              <Icon name="plus" :size="14" />
              新建服务器
            </button>
          </div>
        </header>

        <div class="flex-1 min-h-0 flex">
          <main class="flex-1 min-w-0 flex flex-col">
            <ServerList v-show="ui.view === 'servers'" />
            <SettingsPage v-show="ui.view === 'settings'" />
            <TunnelPage v-show="ui.view === 'tunnel'" />

            <div v-show="ui.view === 'workspace'" class="flex-1 min-h-0 flex flex-col">
              <TabBar v-if="sessions.tabs.length && !sideTabs" />

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
                  <SplitView v-if="sessions.splitShown" :layout="paneLayout!" />

                  <div v-if="!sessions.tabs.length" class="empty">
                    <div class="empty-inner">
                      <div
                        class="w-11 h-11 grid place-items-center"
                        style="border-radius: var(--radius-lg); background: var(--signal-weak); box-shadow: inset 0 0 0 1px var(--signal-border)"
                      >
                        <Icon name="terminal" :size="19" extra-class="text-signal" />
                      </div>
                      <h3>尚未打开会话</h3>
                      <p>从左侧服务器列表选择节点连接，或使用 ⌘K 快速搜索。连接后终端、SFTP 与系统看板将同步就绪。</p>
                      <div class="flex gap-2">
                        <button class="btn btn-primary" @click="ui.openTerminalSidebar()">
                          <Icon name="panel-left" :size="14" />
                          打开服务器列表
                        </button>
                        <button class="btn btn-ghost" @click="ui.showServers()">
                          <Icon name="server" :size="14" />
                          服务器管理
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
            </div>
          </main>
        </div>

        <ServerStatusBar :tab="sessions.activeTab" />
      </div>
    </div>

    <QuickConnectPanel v-if="!needsUnlock" />

    <!-- 命令面板 -->
    <div v-if="ui.cmdOpen" class="modal-root" @click.self="closeCmd">
      <div class="modal" style="width: min(560px, 100%); padding: 0; overflow: hidden" role="dialog" aria-label="命令面板">
        <div class="flex items-center gap-2.5 px-3" style="height: 46px; box-shadow: inset 0 -1px 0 var(--line)">
          <Icon name="search" :size="15" extra-class="text-mist" />
          <input
            ref="cmdInput"
            v-model="cmdQuery"
            class="flex-1 min-w-0 text-[13.5px]"
            style="border: 0; background: transparent; height: 100%"
            placeholder="连接服务器、打开隧道、跳转设置…"
            aria-label="搜索命令"
            @input="cmdIndex = 0"
          />
          <span class="kbd">ESC</span>
        </div>
        <div class="max-h-[320px] overflow-y-auto p-1.5">
          <button
            v-for="(item, i) in cmdItems"
            :key="item.id"
            class="w-full grid grid-cols-[1fr_auto] gap-3 items-center px-2.5 py-2 text-left text-[13px]"
            style="border-radius: var(--radius-sm)"
            :class="i === cmdIndex
              ? 'bg-[var(--hover-strong)] text-[var(--mist-100)]'
              : 'text-[var(--mist-200)] hover:bg-[var(--hover)]'"
            @mouseenter="cmdIndex = i"
            @click="runCmd(item)"
          >
            <span class="truncate">{{ item.label }}</span>
            <span class="text-mist text-[10.5px] uppercase tracking-[0.1em]">{{ item.hint }}</span>
          </button>
          <p v-if="!cmdItems.length" class="px-3 py-6 text-[12px] text-mist text-center">无匹配命令</p>
        </div>
      </div>
    </div>
  </div>
</template>
