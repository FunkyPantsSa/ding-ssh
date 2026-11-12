import {computed, onMounted, onBeforeUnmount, ref, watchEffect, type Ref} from 'vue'
import {useSettingsStore} from '../stores/settings'

export function useWorkspaceLayout(navCollapsed: Ref<boolean>, activeConnected: Ref<unknown>, sftpVisible: Ref<boolean>) {
const settings = useSettingsStore()
// ---- 侧栏宽度：左侧导航与右侧工具面板都可拖动，宽度记在 localStorage ----
// 宽度值一律是「布局（未缩放）像素」，与 CSS 变量、clientWidth 同一坐标系；
// 界面缩放走 .app-shell 的 zoom，鼠标增量需用 unzoom 折算（见 components/WidthResizer.vue）。
const NAV_WIDTH_KEY = 'ding-ssh:nav-width'
const TOOL_WIDTH_KEY = 'ding-ssh:tool-width'
const NAV_W_DEFAULT = 216
const NAV_W_MIN = 176
const NAV_W_MAX = 420
const TOOL_W_DEFAULT = 340
const TOOL_W_MIN = 260
const TOOL_W_MAX = 760
// 终端区最小可用宽度：拖动时按窗口可用空间收紧，保证终端不被挤没
const TERM_MIN_W = 420

function readStoredWidth(key: string, fallback: number, min: number, max: number): number {
  try {
    const raw = Number(localStorage.getItem(key))
    if (Number.isFinite(raw) && raw > 0) return clampWidth(raw, min, max)
  } catch {
    // 存储不可用时回落默认宽度
  }
  return fallback
}

function clampWidth(v: number, min: number, max: number): number {
  const lo = Math.min(min, max)
  const hi = Math.max(min, max)
  return Math.round(Math.min(Math.max(v, lo), hi))
}

const navWidth = ref(readStoredWidth(NAV_WIDTH_KEY, NAV_W_DEFAULT, NAV_W_MIN, NAV_W_MAX))
const toolWidth = ref(readStoredWidth(TOOL_WIDTH_KEY, TOOL_W_DEFAULT, TOOL_W_MIN, TOOL_W_MAX))
const resizing = ref(false)
const shellEl = ref<HTMLElement | {$el: HTMLElement} | null>(null)
const toolRowEl = ref<HTMLElement | null>(null)
// 窗口尺寸：与界面缩放、导航宽度一起构成下面的 layoutKey（clientWidth 本身不是响应式的）
const viewportW = ref(window.innerWidth)

// clientWidth 不是响应式的：把「会影响它的量」（窗口尺寸、界面缩放、导航宽度与折叠态）
// 聚成一个依赖键，上限计算时读一下它，就能在布局变化后重新按可用空间收紧。
// 只放原始输入（不放 effectiveXxx），否则会形成 toolMaxW ↔ effectiveNavW 的循环依赖。
const layoutKey = computed(
  () => `${viewportW.value}:${settings.uiScale}:${navWidth.value}:${navCollapsed.value}`,
)

// 右侧三个面板（SFTP / 看板 / 传输记录）共用一个显隐条件与宽度
const toolVisible = computed(() => !!activeConnected.value && sftpVisible.value)

const toolMaxW = computed(() => {
  void layoutKey.value // 依赖登记
  // 未挂载 / 工作区被 v-show 隐藏时 clientWidth 为 0：不收紧，直接用用户宽度
  const avail = toolRowEl.value?.clientWidth ?? 0
  if (!avail) return TOOL_W_MAX
  return Math.max(TOOL_W_MIN, Math.min(TOOL_W_MAX, avail - TERM_MIN_W))
})
const effectiveToolW = computed(() => clampWidth(toolWidth.value, TOOL_W_MIN, toolMaxW.value))

const navMaxW = computed(() => {
  void layoutKey.value // 依赖登记
  const shell = shellEl.value
  const avail = shell instanceof HTMLElement ? shell.clientWidth : shell?.$el?.clientWidth ?? 0
  if (!avail) return NAV_W_MAX
  // 右侧工具面板同样占宽，一并扣掉；窗口实在不够时至少让导航落在下限
  const reserved = toolVisible.value ? effectiveToolW.value : 0
  return Math.max(NAV_W_MIN, Math.min(NAV_W_MAX, avail - reserved - TERM_MIN_W))
})
const effectiveNavW = computed(() => clampWidth(navWidth.value, NAV_W_MIN, navMaxW.value))

// 拖动结束（而不是每帧）才写 localStorage，避免拖动过程中高频写盘
function persistWidth(key: string, w: number): void {
  try {
    localStorage.setItem(key, String(w))
  } catch {
    // 存储不可用：本次会话内仍然生效
  }
}

function onNavResize(w: number) {
  navWidth.value = w
}
function onToolResize(w: number) {
  toolWidth.value = w
}
function onNavDrag(active: boolean) {
  resizing.value = active
  if (!active) persistWidth(NAV_WIDTH_KEY, navWidth.value)
}
function onToolDrag(active: boolean) {
  resizing.value = active
  if (!active) persistWidth(TOOL_WIDTH_KEY, toolWidth.value)
}
function resetNavWidth() {
  navWidth.value = NAV_W_DEFAULT
  persistWidth(NAV_WIDTH_KEY, NAV_W_DEFAULT)
}
function resetToolWidth() {
  toolWidth.value = TOOL_W_DEFAULT
  persistWidth(TOOL_WIDTH_KEY, TOOL_W_DEFAULT)
}
function onWindowResize() {
  viewportW.value = window.innerWidth
}

// 宽度 → CSS 变量。写在 <html> 上：--nav-w 供 .shell 网格使用，同时被 Teleport 到 body 的
// 浮层（快速连接侧栏 / 遮罩）复用；--tool-w 供三个右侧面板共用的 .tool 使用。
watchEffect(() => {
  document.documentElement.style.setProperty('--nav-expanded-w', `${effectiveNavW.value}px`)
  document.documentElement.style.setProperty(
    '--nav-w',
    navCollapsed.value ? 'var(--nav-w-collapsed)' : `${effectiveNavW.value}px`,
  )
})
watchEffect(() => {
  document.documentElement.style.setProperty('--tool-w', `${effectiveToolW.value}px`)
})
// 界面缩放（.app-shell 的 zoom）同步到 <html> 上的 --ui-zoom：快速连接侧栏与遮罩
// Teleport 到 body，不在 .app-shell 内，若不单独继承 zoom，导航被放大后面板会停在
// 未缩放的导航宽度处，与放大后的导航右边界错位（面板盖住导航）。
watchEffect(() => {
  document.documentElement.style.setProperty('--ui-zoom', String((settings.uiScale || 100) / 100))
})

onMounted(() => window.addEventListener('resize', onWindowResize))
onBeforeUnmount(() => window.removeEventListener('resize', onWindowResize))
return {shellEl, toolRowEl, resizing, effectiveNavW, navMaxW, effectiveToolW, toolMaxW, toolVisible, NAV_W_MIN, TOOL_W_MIN, onNavResize, onToolResize, onNavDrag, onToolDrag, resetNavWidth, resetToolWidth}
}
