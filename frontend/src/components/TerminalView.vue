<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import {
  base64ToBytes,
  onSessionOutput,
  onSessionProgress,
  onSessionStatus,
  sshService,
} from '../services/ssh'
import {useSessionsStore} from '../stores/sessions'
import {useSettingsStore} from '../stores/settings'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import type {SessionTab} from '../types'

const props = defineProps<{tab: SessionTab}>()
const sessions = useSessionsStore()
const settings = useSettingsStore()

const container = ref<HTMLElement>()
const progress = ref('')
const menu = ref<{x: number; y: number} | null>(null)
const fontSize = ref(13)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let resizeObserver: ResizeObserver | null = null
const disposers: Array<() => void> = []
let disposed = false

function hexToRgba(hex: string, alpha: number): string {
  const m = hex.replace('#', '')
  const full = m.length === 3 ? m.split('').map((c) => c + c).join('') : m
  const n = parseInt(full, 16)
  if (Number.isNaN(n)) return hex
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}

// 背景图图层样式（模糊 + 轻微放大避免边缘露白）。
const bgImageStyle = computed(() => {
  const t = settings.theme
  if (!t.bgImage) return {}
  return {
    backgroundImage: `url("${t.bgImage}")`,
    filter: t.blurAmount > 0 ? `blur(${t.blurAmount}px) brightness(0.85)` : 'brightness(0.85)',
    transform: 'scale(1.06)',
  }
})

// 文字阴影通过 CSS 变量作用于 .xterm-rows。
const terminalStyle = computed(() => ({
  '--xterm-text-shadow': settings.theme.textShadow
    ? `0 1px 3px rgba(0, 0, 0, 0.8), 0 0 ${settings.theme.shadowBlur}px rgba(0, 0, 0, 0.5)`
    : 'none',
}))

function applyTheme() {
  if (!term) return
  const t = settings.theme
  const background = t.bgImage ? hexToRgba(t.background, 0.85) : t.background
  term.options.allowTransparency = !!t.bgImage
  term.options.theme = {
    background,
    foreground: t.foreground,
    cursor: t.cursor,
    cursorAccent: '#0b1120',
    selectionBackground: t.selection,
  }
  term.refresh(0, term.rows - 1)
}

function fit() {
  if (!term || !fitAddon || disposed) return
  fitAddon.fit()
  if (props.tab.sessionId) {
    sshService.resize(props.tab.sessionId, term.cols, term.rows).catch(() => {})
  }
}

function adjustFontSize(delta: number) {
  if (!term) return
  fontSize.value = Math.max(8, Math.min(32, fontSize.value + delta))
  term.options.fontSize = fontSize.value
  fit()
}

function toggleFullscreen() {
  const el = container.value?.closest('.terminal-bg') as HTMLElement | null
  if (!el) return
  if (document.fullscreenElement) {
    document.exitFullscreen()
  } else {
    el.requestFullscreen()
  }
}

function openMenu(e: MouseEvent) {
  e.preventDefault()
  menu.value = {x: Math.min(e.clientX, window.innerWidth - 160), y: Math.min(e.clientY, window.innerHeight - 160)}
}

function closeMenu() {
  menu.value = null
}

function doCopy() {
  if (!term) return
  const sel = term.getSelection()
  if (sel) ClipboardSetText(sel)
  closeMenu()
}

function doPaste() {
  closeMenu()
  navigator.clipboard.readText().then(text => {
    if (text && props.tab.sessionId) {
      sshService.write(props.tab.sessionId, btoa(text)).catch(() => {})
    }
  }).catch(() => {})
}

function doClear() {
  term?.clear()
  closeMenu()
}

function doSelectAll() {
  term?.selectAll()
  closeMenu()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && menu.value) {
    menu.value = null
    e.preventDefault()
    return
  }
  if (e.key === 'f' && (e.ctrlKey || e.metaKey)) {
    // Ctrl+F / Cmd+F: trigger browser find (default behavior, just prevent conflict)
    return
  }
  if ((e.ctrlKey || e.metaKey) && (e.key === '=' || e.key === '+')) {
    e.preventDefault()
    adjustFontSize(1)
  }
  if ((e.ctrlKey || e.metaKey) && e.key === '-') {
    e.preventDefault()
    adjustFontSize(-1)
  }
  if ((e.ctrlKey || e.metaKey) && e.key === '0') {
    e.preventDefault()
    fontSize.value = 13
    if (term) term.options.fontSize = 13
    fit()
  }
  if (e.key === 'F11') {
    e.preventDefault()
    toggleFullscreen()
  }
}

async function connect() {
  // 清理上一次连接注册的事件监听，避免重连后重复监听
  disposers.forEach((d) => d())
  disposers.length = 0
  sessions.setStatus(props.tab.clientId, 'connecting')
  progress.value = ''

  // 使用客户端 ID 作为会话 ID，可在连接建立前订阅进度/状态事件
  const sid = props.tab.clientId
  props.tab.sessionId = sid
  term?.reset()

  disposers.push(
    onSessionProgress(sid, (evt) => {
      progress.value = evt.step
    }),
  )
  disposers.push(
    onSessionStatus(sid, (evt) => {
      sessions.setStatus(props.tab.clientId, evt.status, evt.message)
    }),
  )

  try {
    const result = await sshService.connect(sid, props.tab.node, term?.cols ?? 80, term?.rows ?? 24)
    if (disposed) {
      sshService.disconnect(result.sessionId).catch(() => {})
      return
    }
    sessions.bindSession(props.tab.clientId, result.sessionId)
    sessions.setStatus(props.tab.clientId, 'connected')
    disposers.push(
      onSessionOutput(result.sessionId, (data) => {
        if (!disposed) term?.write(base64ToBytes(data))
      }),
    )
    fit()
  } catch (e) {
    sessions.setStatus(props.tab.clientId, 'error', String(e))
  }
}

async function disconnect() {
  if (props.tab.sessionId) {
    await sshService.disconnect(props.tab.sessionId).catch(() => {})
  }
}

function closeTab() {
  sessions.closeTab(props.tab.clientId)
}

onMounted(() => {
  term = new Terminal({
    allowProposedApi: true,
    allowTransparency: !!settings.theme.bgImage,
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'Menlo, Monaco, "Cascadia Mono", "JetBrains Mono", Consolas, monospace',
    theme: {
      background: settings.theme.background,
      foreground: settings.theme.foreground,
      cursor: settings.theme.cursor,
      cursorAccent: '#0b1120',
      selectionBackground: settings.theme.selection,
    },
    scrollback: 5000,
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(container.value!)
  term.onData((data) => {
    if (props.tab.sessionId) {
      sshService.write(props.tab.sessionId, btoa(data)).catch(() => {})
    }
  })
  // 选中即复制（受设置页开关控制）
  term.onSelectionChange(() => {
    if (!settings.copyOnSelect) return
    const selection = term?.getSelection()
    if (selection) void ClipboardSetText(selection)
  })
  resizeObserver = new ResizeObserver(() => fit())
  resizeObserver.observe(container.value!)
  applyTheme()
  container.value?.addEventListener('keydown', onKeydown)
  container.value?.addEventListener('contextmenu', openMenu)
  window.addEventListener('click', closeMenu)
  void connect()
})

watch(
  () => settings.theme,
  () => applyTheme(),
  {deep: true},
)

onBeforeUnmount(() => {
  disposed = true
  disposers.forEach((d) => d())
  resizeObserver?.disconnect()
  window.removeEventListener('click', closeMenu)
  if (props.tab.sessionId) {
    void sshService.disconnect(props.tab.sessionId).catch(() => {})
  }
  term?.dispose()
  term = null
})
</script>

<template>
  <div class="relative h-full overflow-hidden terminal-theme" :style="terminalStyle">
    <!-- 背景图图层 -->
    <div v-if="settings.theme.bgImage" class="absolute inset-0 bg-cover bg-center" :style="bgImageStyle"></div>

    <div ref="container" class="absolute inset-0" tabindex="0" @focus.self></div>

    <!-- 连接中：显示连接过程详细信息 -->
    <div
      v-if="tab.status === 'connecting'"
      class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-slate-950/70 backdrop-blur-sm"
    >
      <span class="w-6 h-6 border-2 border-sky-400 border-t-transparent rounded-full animate-spin"></span>
      <p class="text-sm text-slate-300">正在建立 SSH 连接…</p>
      <p class="text-xs text-slate-500 max-w-md text-center px-6 min-h-[16px]">
        {{ progress || '准备中…' }}
      </p>
      <button
        class="mt-2 px-3 py-1 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
        @click="closeTab"
      >
        关闭标签页
      </button>
    </div>

    <!-- 连接失败 / 已断开 遮罩 -->
    <div
      v-else-if="tab.status === 'error' || tab.status === 'closed'"
      class="absolute inset-0 flex flex-col items-center justify-center gap-4 bg-slate-950/70 backdrop-blur-sm"
    >
      <p class="text-sm text-slate-300 max-w-md text-center px-6 break-all">
        {{ tab.message || (tab.status === 'closed' ? '连接已断开' : '连接失败') }}
      </p>
      <p v-if="progress && tab.status === 'error'" class="text-xs text-slate-500 max-w-md text-center px-6 break-all">
        最后进度：{{ progress }}
      </p>
      <div class="flex gap-3">
        <button
          class="px-4 py-1.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 text-sm font-medium"
          @click="connect"
        >
          重新连接
        </button>
        <button
          class="px-4 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-sm"
          @click="closeTab"
        >
          关闭标签页
        </button>
      </div>
    </div>
  
    <!-- 右键菜单 -->
    <Teleport to="body">
      <div
        v-if="menu"
        class="fixed z-50 min-w-[140px] rounded-lg border border-slate-700/60 bg-slate-900/95 py-1 shadow-2xl text-xs"
        :style="{left: menu.x + 'px', top: menu.y + 'px'}"
        @contextmenu.prevent
        @click.stop
      >
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="doCopy">复制</button>
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="doPaste">粘贴</button>
        <div class="h-px bg-slate-700/60 my-1"></div>
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="doSelectAll">全选</button>
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="doClear">清除屏幕</button>
        <div class="h-px bg-slate-700/60 my-1"></div>
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="adjustFontSize(1)">放大 <span class="text-slate-500">Ctrl+=</span></button>
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="adjustFontSize(-1)">缩小 <span class="text-slate-500">Ctrl+-</span></button>
        <button class="w-full text-left px-3 py-1.5 text-slate-200 hover:bg-slate-800" @click="toggleFullscreen">全屏 <span class="text-slate-500">F11</span></button>
      </div>
    </Teleport>
  </div>
</template>

<style>
/* 文字阴影：作用于 xterm 内部渲染的 .xterm-rows */
.terminal-theme .xterm-rows {
  text-shadow: var(--xterm-text-shadow, none);
}
</style>
