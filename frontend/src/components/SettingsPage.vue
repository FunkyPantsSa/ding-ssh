<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {
  DEFAULT_COMPLETION_NAV_HOTKEY,
  formatHotkeyLabel,
  hotkeyFromEvent,
} from '../completion/hotkey'
import {historyService} from '../services/history'
import {securityService} from '../services/security'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import {useServersStore} from '../stores/servers'
import {defaultAppearance, defaultFonts, defaultTheme, useSettingsStore} from '../stores/settings'
import {ClearLogs, DebugInfo, ExportDiagnostics, GetAuditRecords, GetCapabilities, GetLogInfo, OpenLogFolder, ReadLogTail, SetCapability, SetLogOptions, UndoAuditRecord} from '../../wailsjs/go/main/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {defaultPreset, paletteToTheme, presetById, PRESETS} from '../theme/presets'
import {resolveTone} from '../theme/engine'
import {APP_COMMIT, APP_VERSION, buildDateLabel, buildTimeLabel, detectPlatform, platformLabel} from '../version'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import type {AuditRecordView, CapabilitiesView, Credential, DebugSettings, Fonts, LocalShellOption, LogLevel, NavSectionOrder, RightClickAction, SecurityStatus, TabBarPlacement, Theme, UIAppearance} from '../types'
import CredentialDialog from './CredentialDialog.vue'
import ToggleSwitch from './ToggleSwitch.vue'

const settings = useSettingsStore()
const credentials = useCredentialsStore()
const servers = useServersStore()
const saving = ref(false)

// 一级菜单：通用 / 外观 / 凭证 / 安全 / 调试模式 / 日志 / AI 记录 / 导入导出
const menuItems = [
  {key: 'general', label: '通用', icon: 'clock'},
  {key: 'theme', label: '外观', icon: 'palette'},
  {key: 'credentials', label: '保存的凭证', icon: 'key'},
  {key: 'security', label: '安全', icon: 'lock'},
  {key: 'debug', label: '调试模式', icon: 'terminal'},
  {key: 'logs', label: '日志', icon: 'file'},
  {key: 'audit', label: 'AI 记录', icon: 'activity'},
  {key: 'migrate', label: '导入导出', icon: 'package'},
  {key: 'about', label: '关于', icon: 'info'},
] as const
type SettingsSection = 'general' | 'theme' | 'credentials' | 'security' | 'debug' | 'logs' | 'audit' | 'migrate' | 'about'
const section = ref<SettingsSection>('general')
const themeForm = reactive<Theme>(defaultTheme())
const appearanceForm = reactive<UIAppearance>(defaultAppearance())
const fontsForm = reactive<Fonts>(defaultFonts())
/** 正在同步表单（避免 watcher 立即触发保存） */
let syncing = false

// 外观：可捆绑选择的字体（对应 style.css 中的 @fontsource 引入）
const UI_FONT_OPTIONS = ['Inter', 'Sora', 'Manrope', 'Source Sans 3', 'system'] as const
const TERMINAL_FONT_OPTIONS = ['IBM Plex Mono', 'JetBrains Mono', 'Fira Code', 'Source Code Pro', 'Cascadia Code', 'system'] as const
const TONE_OPTIONS = [
  {key: 'auto', label: '跟随系统'},
  {key: 'light', label: '浅色'},
  {key: 'dark', label: '深色'},
] as const
const MODE_OPTIONS = [
  {key: 'preset', label: '预设主题'},
  {key: 'custom', label: '自定义'},
] as const
// 会话标签页位置：顶部横向标签栏 / 左侧导航纵向列表
const TAB_PLACEMENT_OPTIONS = [
  {key: 'side', label: '左侧导航'},
  {key: 'top', label: '顶栏'},
] as const
// 终端鼠标右键行为：打开选项栏 / 直接粘贴剪贴板
const RIGHT_CLICK_OPTIONS = [
  {key: 'menu', label: '打开选项栏'},
  {key: 'paste', label: '直接粘贴'},
] as const
// 左侧导航区段顺序：导航 / 会话 / 标签页 三块的六种排列
const NAV_SECTION_ORDER_OPTIONS = [
  {value: 'nav,sessions,tabs', label: '导航 · 会话 · 标签页'},
  {value: 'nav,tabs,sessions', label: '导航 · 标签页 · 会话'},
  {value: 'sessions,nav,tabs', label: '会话 · 导航 · 标签页'},
  {value: 'sessions,tabs,nav', label: '会话 · 标签页 · 导航'},
  {value: 'tabs,nav,sessions', label: '标签页 · 导航 · 会话'},
  {value: 'tabs,sessions,nav', label: '标签页 · 会话 · 导航'},
] as const

// 日志级别下拉（与 Go 端一致：debug / info / warn / error）
const LOG_LEVEL_OPTIONS = [
  {key: 'debug', label: 'debug'},
  {key: 'info', label: 'info'},
  {key: 'warn', label: 'warn'},
  {key: 'error', label: 'error'},
] as const
// 「最近日志」可选行数
const LOG_TAIL_LINES = [100, 200, 500] as const
// 后端读不到保留策略时的兜底展示（与 Go 端默认值一致）
const LOG_RETENTION_FALLBACK = {maxBytes: 8 * 1024 * 1024, keepFiles: 20}


// ANSI 16 色表单字段（名称 → 中文标签）
type AnsiColorKey =
  | 'black' | 'red' | 'green' | 'yellow' | 'blue' | 'magenta' | 'cyan' | 'white'
  | 'brightBlack' | 'brightRed' | 'brightGreen' | 'brightYellow'
  | 'brightBlue' | 'brightMagenta' | 'brightCyan' | 'brightWhite'
const ANSI_FIELDS: {key: AnsiColorKey; label: string}[] = [
  {key: 'black', label: 'Black'},
  {key: 'red', label: 'Red'},
  {key: 'green', label: 'Green'},
  {key: 'yellow', label: 'Yellow'},
  {key: 'blue', label: 'Blue'},
  {key: 'magenta', label: 'Magenta'},
  {key: 'cyan', label: 'Cyan'},
  {key: 'white', label: 'White'},
  {key: 'brightBlack', label: '亮黑'},
  {key: 'brightRed', label: '亮红'},
  {key: 'brightGreen', label: '亮绿'},
  {key: 'brightYellow', label: '亮黄'},
  {key: 'brightBlue', label: '亮蓝'},
  {key: 'brightMagenta', label: '亮紫'},
  {key: 'brightCyan', label: '亮青'},
  {key: 'brightWhite', label: '亮白'},
]

const currentTone = computed(() => resolveTone(appearanceForm.baseTone))
const activePreset = computed(() => presetById(appearanceForm.presetId) ?? defaultPreset())

/** 计算在色块上可读的文字颜色 */
function contrastFor(hex: string): string {
  const h = (hex || '').trim().replace('#', '')
  const n = parseInt(h.length === 3 ? h.split('').map((c) => c + c).join('') : h, 16)
  if (Number.isNaN(n)) return '#ffffff'
  const r = (n >> 16) & 255
  const g = (n >> 8) & 255
  const b = n & 255
  return (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.55 ? '#0c1016' : '#ffffff'
}

function syncForms() {
  syncing = true
  Object.assign(themeForm, settings.theme)
  Object.assign(appearanceForm, settings.appearance)
  Object.assign(fontsForm, settings.fonts)
  syncing = false
}

// 表单任意修改即保存（本地持久化开销小，预览即时生效）
watch([themeForm, appearanceForm, fontsForm], () => {
  if (syncing) return
  void persistLook()
}, {deep: true})

async function persistLook() {
  saving.value = true
  try {
    await settings.applyLook({...appearanceForm}, {...themeForm}, {...fontsForm})
  } finally {
    saving.value = false
  }
}

async function selectPreset(id: string) {
  syncing = true
  appearanceForm.mode = 'preset'
  appearanceForm.presetId = id
  const preset = presetById(id) ?? defaultPreset()
  Object.assign(themeForm, paletteToTheme(currentTone.value === 'light' ? preset.light : preset.dark))
  syncing = false
  await persistLook()
}

async function setTone(tone: UIAppearance['baseTone']) {
  syncing = true
  appearanceForm.baseTone = tone
  if (appearanceForm.mode === 'preset') {
    const preset = activePreset.value
    Object.assign(themeForm, paletteToTheme(tone === 'light' ? preset.light : preset.dark))
  }
  syncing = false
  await persistLook()
}

async function setMode(mode: UIAppearance['mode']) {
  syncing = true
  appearanceForm.mode = mode
  if (mode === 'preset') {
    const preset = activePreset.value
    Object.assign(themeForm, paletteToTheme(currentTone.value === 'light' ? preset.light : preset.dark))
  }
  syncing = false
  await persistLook()
}

async function resetTheme() {
  syncing = true
  Object.assign(themeForm, defaultTheme())
  syncing = false
  await persistLook()
}

async function resetAppearance() {
  syncing = true
  Object.assign(appearanceForm, defaultAppearance())
  Object.assign(fontsForm, defaultFonts())
  const preset = defaultPreset()
  Object.assign(themeForm, paletteToTheme(currentTone.value === 'light' ? preset.light : preset.dark))
  syncing = false
  await persistLook()
}

async function pickBgImage() {
  const path = await sshService.selectImageFile()
  if (path) themeForm.bgImage = path
}
const showCredDialog = ref(false)
const confirmCredId = ref('')
const editingCred = ref<Credential | null>(null)
/** 正在就地显示明文的密码类凭证 ID（私钥类走 revealKey 面板） */
const revealId = ref('')
/** 正在查看明文的私钥凭证（弹小面板展示全文） */
const revealKey = ref<Credential | null>(null)
const copiedCredId = ref('')
let copiedCredTimer: number | undefined

const security = ref<SecurityStatus | null>(null)
const secForm = reactive({password: '', confirm: '', oldPassword: '', newPassword: '', newConfirm: ''})
const secError = ref('')
const secMsg = ref('')
const secBusy = ref(false)

const packOverwrite = ref(false)
// 导出/导入各自独立的状态，避免提示串到对方的卡片里
const exportPass = ref('')
const exportPassConfirm = ref('')
const exportError = ref('')
const exportMsg = ref('')
const exportBusy = ref(false)
const importPass = ref('')
const importError = ref('')
const importMsg = ref('')
const importBusy = ref(false)

async function refreshSecurity() {
  try {
    security.value = await securityService.getStatus()
  } catch {
    security.value = null
  }
}

async function toggleCopy(v: boolean) {
  saving.value = true
  try {
    await settings.setCopyOnSelect(v)
  } finally {
    saving.value = false
  }
}

async function toggleWebGL(v: boolean) {
  saving.value = true
  try {
    await settings.setWebGLEnabled(v)
  } finally {
    saving.value = false
  }
}

async function toggleCompletion(v: boolean) {
  saving.value = true
  try {
    await settings.setCompletionEnabled(v)
  } finally {
    saving.value = false
  }
}

async function toggleSftpSync(v: boolean) {
  saving.value = true
  try {
    await settings.setSftpToTerminalSync(v)
  } finally {
    saving.value = false
  }
}

async function toggleTerminalSftpSync(v: boolean) {
  saving.value = true
  try {
    await settings.setTerminalToSftpSync(v)
  } finally {
    saving.value = false
  }
}

async function toggleAutoReconnect(v: boolean) {
  saving.value = true
  try {
    await settings.setAutoReconnect(v)
  } finally {
    saving.value = false
  }
}

async function toggleKeepAlive(v: boolean) {
  saving.value = true
  try {
    await settings.setKeepAliveEnabled(v)
  } finally {
    saving.value = false
  }
}

const platform = ref('')
const localShellOptions = ref<LocalShellOption[]>([])

async function loadLocalShellOptions() {
  try {
    platform.value = await sshService.getPlatform()
    localShellOptions.value = await sshService.getLocalShellOptions()
    if (!settings.localShell && localShellOptions.value.length) {
      // 仅填充 UI 默认展示，不立刻写库；用户改选时再保存
      settings.localShell = localShellOptions.value[0].value
    } else if (
      settings.localShell &&
      localShellOptions.value.length &&
      !localShellOptions.value.some((o) => o.value === settings.localShell)
    ) {
      settings.localShell = localShellOptions.value[0].value
    }
  } catch {
    platform.value = ''
    localShellOptions.value = []
  }
}

async function onLocalShellChange(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  saving.value = true
  try {
    await settings.setLocalShell(v)
  } finally {
    saving.value = false
  }
}

async function setUIScale(v: number) {
  saving.value = true
  try {
    await settings.setUIScale(v)
  } finally {
    saving.value = false
  }
}

async function setTabBarPlacement(v: TabBarPlacement) {
  if (settings.tabBarPlacement === v) return
  saving.value = true
  try {
    await settings.setTabBarPlacement(v)
  } finally {
    saving.value = false
  }
}

async function setRightClickAction(v: RightClickAction) {
  if (settings.rightClickAction === v) return
  saving.value = true
  try {
    await settings.setRightClickAction(v)
  } finally {
    saving.value = false
  }
}

async function setNavSectionOrder(v: NavSectionOrder) {
  if (settings.navSectionOrder === v) return
  saving.value = true
  try {
    await settings.setNavSectionOrder(v)
  } finally {
    saving.value = false
  }
}

async function onNavSectionOrderChange(e: Event) {
  await setNavSectionOrder((e.target as HTMLSelectElement).value as NavSectionOrder)
}

// ---- 调试模式 ----
const debugInfo = ref<Record<string, any> | null>(null)

async function refreshDebugInfo() {
  try {
    debugInfo.value = await DebugInfo()
  } catch {
    debugInfo.value = null
  }
}

async function setDebug(patch: Partial<DebugSettings>) {
  saving.value = true
  try {
    await settings.setDebug(patch)
    await refreshDebugInfo()
  } finally {
    saving.value = false
  }
}

async function onDebugPortChange(e: Event) {
  const raw = Number((e.target as HTMLInputElement).value)
  await setDebug({port: Number.isFinite(raw) ? raw : 8765})
}

// ---- MCP 能力（权限内核，对应 Go 端 internal/debugsrv 的能力位）----
//
// 列表顺序与 Go 端 debugsrv.AllCapabilities() 一致，说明文字来自后端的
// GetCapabilities().descriptions（单一事实来源，避免前后端各写一份）。
const CAP_ORDER = ['terminal.input', 'ui.write', 'config.write', 'secrets.write', 'fs.remote.write', 'sudo.credential', 'lifecycle'] as const
/** 打开前必须二次确认的能力（与后端 GetCapabilities().requiresConfirm 一致） */
const CAP_NEEDS_CONFIRM = new Set(['fs.remote.write', 'sudo.credential', 'lifecycle'])
const capInfo = ref<CapabilitiesView | null>(null)
/** 待二次确认的能力名（不为空时展示「确认开启 / 取消」） */
const pendingCap = ref('')
const capError = ref('')
const capMsg = ref('')
/** 白名单编辑区（多行文本，一行一个绝对路径前缀） */
const allowlistText = ref('')
const allowlistDirty = ref(false)

const capRows = computed(() => {
  const caps = capInfo.value?.caps ?? {}
  const labels = capInfo.value?.labels ?? {}
  const descs = capInfo.value?.descriptions ?? {}
  return CAP_ORDER.map((name) => ({
    name,
    label: labels[name] ?? name,
    desc: descs[name] ?? '',
    enabled: !!caps[name],
    needsConfirm: CAP_NEEDS_CONFIRM.has(name),
    // 前置条件：必须先开启「允许读取敏感数据」
    //（secrets.write 要读回凭据；sudo.credential 要读回该服务器保存的密码）
    blocked: (name === 'secrets.write' || name === 'sudo.credential') && !settings.debug.allowSecrets,
  }))
})

async function refreshCapabilities() {
  try {
    capInfo.value = (await GetCapabilities()) as CapabilitiesView
    if (!allowlistDirty.value) {
      allowlistText.value = (capInfo.value.sftpWriteAllowlist ?? []).join('\n')
    }
  } catch (e) {
    capInfo.value = null
    capError.value = String(e)
  }
}

/** 切换能力开关：敏感能力需要先点一次「确认开启」。 */
async function toggleCapability(name: string, next: boolean) {
  capError.value = ''
  capMsg.value = ''
  if (next && CAP_NEEDS_CONFIRM.has(name)) {
    pendingCap.value = name
    return
  }
  await applyCapability(name, next)
}

/** 真正写入能力开关（前端二次确认之后才走到这里）。 */
async function applyCapability(name: string, next: boolean) {
  saving.value = true
  capError.value = ''
  capMsg.value = ''
  try {
    await SetCapability(name, next)
    pendingCap.value = ''
    // 能力写进了 settings.debug，同步镜像到前端设置模型，重开设置页仍一致
    if (name === 'terminal.input') await settings.setDebug({capTerminalInput: next})
    else if (name === 'ui.write') await settings.setDebug({capUiWrite: next})
    else if (name === 'config.write') await settings.setDebug({capConfigWrite: next})
    else if (name === 'secrets.write') await settings.setDebug({capSecretWrite: next})
    else if (name === 'fs.remote.write') await settings.setDebug({capRemoteFsWrite: next})
    else if (name === 'sudo.credential') await settings.setDebug({capSudoCredential: next})
    else if (name === 'lifecycle') await settings.setDebug({capLifecycle: next})
    await refreshCapabilities()
    capMsg.value = next ? `已开启「${capLabelOf(name)}」` : `已关闭「${capLabelOf(name)}」`
  } catch (e) {
    capError.value = String(e)
  } finally {
    saving.value = false
  }
}

function capLabelOf(name: string): string {
  return capInfo.value?.labels?.[name] ?? name
}

/** 保存远端可写路径白名单（留空 = 禁止任何写入路径）。 */
async function saveAllowlist() {
  saving.value = true
  capError.value = ''
  capMsg.value = ''
  try {
    const list = allowlistText.value
      .split('\n')
      .map((s) => s.trim().replace(/\/+$/, ''))
      .filter((s, i, arr) => s.length > 0 && arr.indexOf(s) === i)
    await settings.setDebug({sftpWriteAllowlist: list})
    allowlistDirty.value = false
    await refreshCapabilities()
    capMsg.value = list.length > 0 ? `白名单已保存（${list.length} 条）` : '白名单已清空：将禁止任何远端写入路径'
  } catch (e) {
    capError.value = String(e)
  } finally {
    saving.value = false
  }
}

// ---- AI 记录（审计）----
const auditRecords = ref<AuditRecordView[]>([])
const auditLoading = ref(false)
const auditError = ref('')
const auditMsg = ref('')
/** 过滤器：全部 / 仅写操作 / 仅失败 / 按能力 */
const auditFilter = ref<'all' | 'write' | 'failed'>('all')
const auditCap = ref('')
const auditLimit = ref(50)
/** 展开查看已脱敏 args 的记录 id */
const expandedAudit = ref('')

const auditCapOptions = computed(() => {
  const caps = capInfo.value?.caps ?? {}
  const labels = capInfo.value?.labels ?? {}
  return Object.keys(caps).map((name) => ({value: name, label: `${labels[name] ?? name}（${name}）`}))
})

/** 审计记录按当前过滤器做前端筛选（后端只按 cap/source 过滤）。 */
const filteredAudit = computed(() => {
  let list = auditRecords.value
  if (auditFilter.value === 'failed') list = list.filter((r) => !r.ok)
  if (auditFilter.value === 'write') list = list.filter((r) => !!r.cap)
  return list
})

async function refreshAudit() {
  auditLoading.value = true
  auditError.value = ''
  try {
    const list = (await GetAuditRecords(auditLimit.value, auditCap.value, '')) as AuditRecordView[]
    auditRecords.value = Array.isArray(list) ? list : []
  } catch (e) {
    auditError.value = String(e)
    auditRecords.value = []
  } finally {
    auditLoading.value = false
  }
}

/** 实时追加：后端每条审计记录都会发一条 debug.audit 事件。 */
let auditOff: (() => void) | null = null

function subscribeAudit() {
  if (auditOff) return
  auditOff = EventsOn('debug.audit', (payload: AuditRecordView) => {
    if (!payload || !payload.id) return
    if (auditCap.value && payload.cap !== auditCap.value) return
    if (auditRecords.value.some((r) => r.id === payload.id)) return
    auditRecords.value = [payload, ...auditRecords.value].slice(0, Math.max(auditLimit.value, 50))
  })
}

function unsubscribeAudit() {
  if (auditOff) {
    auditOff()
    auditOff = null
  }
}

async function undoAudit(rec: AuditRecordView) {
  auditError.value = ''
  auditMsg.value = ''
  try {
    await UndoAuditRecord(rec.id, rec.tool, rec.args)
    auditMsg.value = `已撤销 ${rec.tool} 的改动（设置已恢复为改动前的值）`
    await refreshAudit()
  } catch (e) {
    auditError.value = String(e)
  }
}

function formatAuditTime(ts: number): string {
  const d = new Date(ts)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

const AUDIT_SOURCE_LABEL: Record<string, string> = {mcp: 'MCP', http: 'HTTP', ui: '界面'}

function auditSourceLabel(source: string): string {
  return AUDIT_SOURCE_LABEL[source] ?? source
}

onMounted(() => {
  void refreshDebugInfo()
  void refreshCapabilities()
})

// 切到「AI 记录」页签时加载审计并订阅实时事件；离开时退订（避免常驻监听）。
watch(section, (v) => {
  if (v === 'audit') {
    void refreshCapabilities()
    void refreshAudit()
    subscribeAudit()
  } else {
    unsubscribeAudit()
  }
})

onBeforeUnmount(() => {
  unsubscribeAudit()
})

// ---- 日志 ----
const logInfo = ref<Record<string, any> | null>(null)
const logTail = ref('')
const logTailLines = ref<number>(200)
const logTailLoading = ref(false)
const logBusy = ref(false)
const logMsg = ref('')
const logError = ref('')
/** 「清理日志」二次确认（参考「清理命令历史」卡片） */
const confirmClearLogs = ref(false)
/** 最近一次导出的诊断包路径 */
const exportPath = ref('')

const logFiles = computed<Array<{name: string; size: number; modTime: number}>>(() =>
  Array.isArray(logInfo.value?.files) ? logInfo.value.files : [],
)
const logTotalSize = computed(() => Number(logInfo.value?.totalSize) || 0)
// 保留策略：后端没给就用兜底默认值（卡片里会注明）
const logRetention = computed(() => ({
  maxBytes: Number(logInfo.value?.retention?.maxBytes) || LOG_RETENTION_FALLBACK.maxBytes,
  keepFiles: Number(logInfo.value?.retention?.keepFiles) || LOG_RETENTION_FALLBACK.keepFiles,
}))

function formatBytes(n: number): string {
  const v = Number(n) || 0
  if (v < 1024) return `${v} B`
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`
  if (v < 1024 * 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)} MB`
  return `${(v / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

async function refreshLogInfo() {
  try {
    logInfo.value = await GetLogInfo()
  } catch {
    logInfo.value = null
  }
}

/** 读一次日志尾部（只在打开页签 / 点刷新时调用，不做轮询）。 */
async function refreshLogTail() {
  logTailLoading.value = true
  logError.value = ''
  try {
    const r = await ReadLogTail(logTailLines.value)
    logTail.value = typeof r?.text === 'string' ? r.text : ''
  } catch (e) {
    logTail.value = ''
    logError.value = String(e)
  } finally {
    logTailLoading.value = false
  }
}

/**
 * 日志开关 / 级别：先镜像进设置模型持久化（前端设置页重开仍然一致），
 * 再用 SetLogOptions 让后端立即生效，最后刷新 GetLogInfo（与 setDebug 同风格）。
 */
async function applyLogOptions(
  patch: Partial<{consoleEnabled: boolean; fileEnabled: boolean; level: LogLevel; apiLogEnabled: boolean}>,
) {
  saving.value = true
  logError.value = ''
  try {
    const mirror: Partial<{logToFile: boolean; logLevel: LogLevel; logApiCalls: boolean}> = {}
    if (patch.fileEnabled !== undefined) mirror.logToFile = patch.fileEnabled
    if (patch.level !== undefined) mirror.logLevel = patch.level
    if (patch.apiLogEnabled !== undefined) mirror.logApiCalls = patch.apiLogEnabled
    if (patch.consoleEnabled !== undefined) {
      // 控制台日志对应既有的 logEnabled，保持原字段不动
      await settings.setLogEnabled(patch.consoleEnabled)
    }
    if (Object.keys(mirror).length) await settings.setLog(mirror)
    const opts: Record<string, any> = {}
    if (patch.consoleEnabled !== undefined) opts.consoleEnabled = patch.consoleEnabled
    if (patch.fileEnabled !== undefined) opts.fileEnabled = patch.fileEnabled
    if (patch.level !== undefined) opts.level = patch.level
    if (patch.apiLogEnabled !== undefined) opts.apiLogEnabled = patch.apiLogEnabled
    if (Object.keys(opts).length) await SetLogOptions(opts)
    await refreshLogInfo()
  } catch (e) {
    logError.value = String(e)
  } finally {
    saving.value = false
  }
}

async function onLogLevelChange(e: Event) {
  const v = (e.target as HTMLSelectElement).value as LogLevel
  if (settings.logLevel === v) return
  await applyLogOptions({level: v})
}

async function openLogFolder() {
  logError.value = ''
  try {
    await OpenLogFolder()
  } catch (e) {
    logError.value = String(e)
  }
}

async function clearLogs() {
  logError.value = ''
  logMsg.value = ''
  logBusy.value = true
  try {
    const r = await ClearLogs()
    confirmClearLogs.value = false
    const removed = Number(r?.removed) || 0
    logMsg.value = removed > 0 ? `已清理 ${removed} 个日志文件` : '暂无可清理的日志文件'
    await refreshLogInfo()
    await refreshLogTail()
  } catch (e) {
    logError.value = String(e)
  } finally {
    logBusy.value = false
  }
}

async function exportDiagnostics() {
  logError.value = ''
  logMsg.value = ''
  exportPath.value = ''
  logBusy.value = true
  try {
    const r = await ExportDiagnostics()
    exportPath.value = typeof r?.path === 'string' ? r.path : ''
    if (!exportPath.value) logMsg.value = '诊断包已生成'
  } catch (e) {
    logError.value = String(e)
  } finally {
    logBusy.value = false
  }
}

async function onLogTailLinesChange(e: Event) {
  const n = Number((e.target as HTMLSelectElement).value)
  logTailLines.value = (LOG_TAIL_LINES as readonly number[]).includes(n) ? n : 200
  await refreshLogTail()
}

async function onPanelLimitChange(e: Event) {
  const raw = Number((e.target as HTMLInputElement).value)
  saving.value = true
  try {
    await settings.setCompletionPanelLimit(raw)
  } finally {
    saving.value = false
  }
}

const capturingHotkey = ref(false)
const hotkeyCaptureError = ref('')

function startCaptureHotkey() {
  capturingHotkey.value = true
  hotkeyCaptureError.value = ''
}

async function onHotkeyCapture(e: KeyboardEvent) {
  if (!capturingHotkey.value) return
  e.preventDefault()
  e.stopPropagation()
  if (e.key === 'Escape') {
    capturingHotkey.value = false
    hotkeyCaptureError.value = ''
    return
  }
  if (['Control', 'Alt', 'Shift', 'Meta'].includes(e.key)) return
  const hk = hotkeyFromEvent(e)
  if (!hk) {
    hotkeyCaptureError.value = '请使用修饰键组合（如 Alt+↓），或方向键 / F 键'
    return
  }
  capturingHotkey.value = false
  hotkeyCaptureError.value = ''
  saving.value = true
  try {
    await settings.setCompletionNavHotkey(hk)
  } finally {
    saving.value = false
  }
}

async function resetNavHotkey() {
  saving.value = true
  try {
    await settings.setCompletionNavHotkey(DEFAULT_COMPLETION_NAV_HOTKEY)
  } finally {
    saving.value = false
  }
}

const clearingHistory = ref(false)
const historyMsg = ref('')
const historyError = ref('')
const confirmClearHistory = ref(false)

async function clearAllHistory() {
  historyError.value = ''
  historyMsg.value = ''
  clearingHistory.value = true
  try {
    await historyService.clear('')
    confirmClearHistory.value = false
    historyMsg.value = '已清空全部命令历史'
  } catch (e) {
    historyError.value = String(e)
  } finally {
    clearingHistory.value = false
  }
}

function openNewCred() {
  editingCred.value = null
  showCredDialog.value = true
}

function openEditCred(c: Credential) {
  editingCred.value = c
  showCredDialog.value = true
}

/** 密码就地显示明文；私钥弹小面板展示全文。 */
function toggleReveal(c: Credential) {
  if (c.authType === 'privateKey') {
    revealKey.value = c
    return
  }
  revealId.value = revealId.value === c.id ? '' : c.id
}

// ---- 关于：构建信息（版本 tag / 提交号 / 构建时间，编译期注入） ----
const aboutCopied = ref(false)
let aboutCopiedTimer: number | undefined

const aboutRows = computed(() => [
  {label: '版本标签', value: APP_VERSION || 'dev'},
  {label: '构建日期', value: buildDateLabel()},
  {label: '构建时间', value: buildTimeLabel()},
  {label: '代码提交', value: APP_COMMIT || '未知'},
  {label: '运行平台', value: platformLabel(platform.value || detectPlatform())},
])

/** 一键复制版本信息，方便反馈问题时附带环境。 */
async function copyAbout() {
  const text = ['ding-ssh', ...aboutRows.value.map((r) => `${r.label}: ${r.value}`)].join('\n')
  try {
    await ClipboardSetText(text)
    aboutCopied.value = true
    window.clearTimeout(aboutCopiedTimer)
    aboutCopiedTimer = window.setTimeout(() => {
      aboutCopied.value = false
    }, 1600)
  } catch {
    /* ignore */
  }
}

/** 复制密码 / 私钥明文到剪贴板。 */
async function copyCred(c: Credential) {
  const text = c.authType === 'privateKey' ? c.keyContent : c.password
  if (!text) return
  try {
    await ClipboardSetText(text)
    copiedCredId.value = c.id
    window.clearTimeout(copiedCredTimer)
    copiedCredTimer = window.setTimeout(() => {
      copiedCredId.value = ''
    }, 1600)
  } catch {
    /* ignore */
  }
}

async function removeCredential(id: string) {
  confirmCredId.value = ''
  await credentials.remove(id)
}

async function enableMaster() {
  secError.value = ''
  secMsg.value = ''
  if (!secForm.password || secForm.password !== secForm.confirm) {
    secError.value = '请填写并确认主密码'
    return
  }
  secBusy.value = true
  try {
    await securityService.enableMasterPassword(secForm.password)
    secForm.password = ''
    secForm.confirm = ''
    secMsg.value = '主密码已启用，下次启动需输入解锁'
    await refreshSecurity()
  } catch (e) {
    secError.value = String(e)
  } finally {
    secBusy.value = false
  }
}

async function disableMaster() {
  secError.value = ''
  secMsg.value = ''
  if (!secForm.oldPassword) {
    secError.value = '请输入当前主密码'
    return
  }
  secBusy.value = true
  try {
    await securityService.disableMasterPassword(secForm.oldPassword)
    secForm.oldPassword = ''
    secMsg.value = '已关闭主密码，改回系统钥匙串保管'
    await refreshSecurity()
  } catch (e) {
    secError.value = String(e)
  } finally {
    secBusy.value = false
  }
}

async function changeMaster() {
  secError.value = ''
  secMsg.value = ''
  if (!secForm.oldPassword || !secForm.newPassword || secForm.newPassword !== secForm.newConfirm) {
    secError.value = '请填写旧密码与一致的新密码'
    return
  }
  secBusy.value = true
  try {
    await securityService.changeMasterPassword(secForm.oldPassword, secForm.newPassword)
    secForm.oldPassword = ''
    secForm.newPassword = ''
    secForm.newConfirm = ''
    secMsg.value = '主密码已更新'
    await refreshSecurity()
  } catch (e) {
    secError.value = String(e)
  } finally {
    secBusy.value = false
  }
}

async function exportPack() {
  exportError.value = ''
  exportMsg.value = ''
  if (!exportPass.value || exportPass.value !== exportPassConfirm.value) {
    exportError.value = '请填写并确认导出密码'
    return
  }
  exportBusy.value = true
  try {
    const path = await securityService.exportConfig(exportPass.value)
    if (path) {
      exportMsg.value = `已导出到 ${path}`
      exportPass.value = ''
      exportPassConfirm.value = ''
    }
  } catch (e) {
    exportError.value = String(e)
  } finally {
    exportBusy.value = false
  }
}

async function importPack() {
  importError.value = ''
  importMsg.value = ''
  if (!importPass.value) {
    importError.value = '请填写导入密码'
    return
  }
  importBusy.value = true
  try {
    const r = await securityService.importConfig(importPass.value, packOverwrite.value)
    importMsg.value = `已导入服务器 ${r.servers}、凭证 ${r.credentials}、分组 ${r.groups}`
    importPass.value = ''
    await servers.load()
    await credentials.load()
  } catch (e) {
    importError.value = String(e)
  } finally {
    importBusy.value = false
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onHotkeyCapture, true)
  await Promise.all([settings.load(), credentials.load(), refreshSecurity()])
  Object.assign(themeForm, settings.theme)
  await loadLocalShellOptions()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onHotkeyCapture, true)
  window.clearTimeout(copiedCredTimer)
  window.clearTimeout(aboutCopiedTimer)
})

watch(
  () => section.value,
  (s) => {
    if (s === 'theme') syncForms()
    if (s === 'security') void refreshSecurity()
    // 打开「日志」页签时读一次目录信息与日志尾部（不轮询）
    if (s === 'logs') {
      void refreshLogInfo()
      void refreshLogTail()
    }
  },
)
</script>

<template>
  <div class="h-full flex min-h-0">
    <nav class="set-nav">
      <button
        v-for="item in menuItems"
        :key="item.key"
        :class="section === item.key ? 'active' : ''"
        @click="section = item.key"
      >
        <Icon :name="item.icon" :size="14" />
        {{ item.label }}
      </button>
    </nav>

    <div class="flex-1 min-w-0 overflow-y-auto px-8 py-6">
      <!-- 通用设置 -->
      <div v-if="section === 'general'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">通用</h3>
          <p class="text-[13px] text-mist mt-1.5 leading-relaxed">控制选中复制、鼠标右键行为、导航区段顺序、补全热键与命令历史。</p>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">选中内容自动复制</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">开启后在终端中选中文本，内容将自动复制到剪贴板。</p>
            </div>
            <ToggleSwitch :model-value="settings.copyOnSelect" :disabled="saving" @update:model-value="toggleCopy" />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">鼠标右键行为</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                在终端中单击鼠标右键时：「打开选项栏」弹出复制 / 粘贴 / 全屏等菜单；「直接粘贴」把剪贴板内容写入终端。
                键盘粘贴始终可用（Ctrl+V / Ctrl+Shift+V / Shift+Insert）。
              </p>
            </div>
            <div class="seg shrink-0">
              <button
                v-for="opt in RIGHT_CLICK_OPTIONS"
                :key="opt.key"
                type="button"
                :class="settings.rightClickAction === opt.key ? 'active' : ''"
                :aria-pressed="settings.rightClickAction === opt.key"
                :disabled="saving"
                @click="setRightClickAction(opt.key)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">WebGL 硬件加速</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                优先使用 GPU 渲染终端；不可用或上下文丢失时自动降级 Canvas。关闭后强制使用 Canvas。
              </p>
            </div>
            <ToggleSwitch :model-value="settings.webGLEnabled" :disabled="saving" @update:model-value="toggleWebGL" />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">智能命令补全</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                输入时展示历史 / 屏幕上下文 / 静态字典建议；热键或悬停进入导航后，↑↓ 切换、Tab/Enter 插入；未导航时 Tab/↑↓ 交给终端。密码场景可关闭本开关。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.completionEnabled"
              :disabled="saving"
              @update:model-value="toggleCompletion"
            />
          </div>
          <div
            v-if="settings.completionEnabled"
            class="px-5 py-4 border-t border-slate-800/60 space-y-3"
          >
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0">
                <p class="text-sm text-slate-300">补全导航热键</p>
                <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                  面板打开时用于开启 / 关闭导航；默认 Alt+↓。点击右侧后按下新组合键。
                </p>
              </div>
              <div class="shrink-0 flex items-center gap-2">
                <button
                  type="button"
                  class="min-w-[7.5rem] px-3 py-1.5 rounded-md border text-xs font-mono transition-colors"
                  :class="
                    capturingHotkey
                      ? 'border-[var(--signal-strong-border)] bg-[var(--signal-weak)] text-signal animate-pulse'
                      : 'border-[var(--field-border)] bg-[var(--field-bg)] text-slate-200 hover:border-[var(--signal-border)]'
                  "
                  :disabled="saving"
                  @click="startCaptureHotkey"
                >
                  {{
                    capturingHotkey
                      ? '按下组合键…'
                      : formatHotkeyLabel(settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY)
                  }}
                </button>
                <button
                  type="button"
                  class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                  :disabled="saving || capturingHotkey"
                  @click="resetNavHotkey"
                >
                  默认
                </button>
              </div>
            </div>
            <p v-if="hotkeyCaptureError" class="text-xs text-rose-400">{{ hotkeyCaptureError }}</p>
            <p v-else-if="capturingHotkey" class="text-[12px] text-slate-500">Esc 取消录制</p>
            <div class="flex items-start justify-between gap-4 pt-1">
              <div class="min-w-0">
                <p class="text-sm text-slate-300">补全面板条数</p>
                <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                  历史 / 屏幕 / 字典合计最多展示条数（3–30，默认 8）。
                </p>
              </div>
              <input
                type="number"
                min="3"
                max="30"
                step="1"
                class="w-16 shrink-0 input input-sm text-xs font-mono"
                :value="settings.completionPanelLimit || 8"
                :disabled="saving"
                @change="onPanelLimitChange"
              />
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">终端→SFTP 目录同步</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                终端执行 cd 或提示符路径变化时，SFTP 面板跟随跳转到同一目录。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.terminalToSftpSync"
              :disabled="saving"
              @update:model-value="toggleTerminalSftpSync"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">SFTP→终端目录同步</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                在 SFTP 面板进入目录时，自动向终端发送 cd 命令同步当前路径。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.sftpToTerminalSync"
              :disabled="saving"
              @update:model-value="toggleSftpSync"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">界面缩放</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                整体等比缩放界面（含布局与字号），适配不同屏幕尺寸；范围 80%–150%，默认 100%。
              </p>
            </div>
            <div class="shrink-0 flex items-center gap-2">
              <button
                type="button"
                class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                :disabled="saving || settings.uiScale <= 80"
                @click="setUIScale(settings.uiScale - 10)"
              >
                −
              </button>
              <span class="w-14 text-center text-xs font-mono text-slate-200">{{ settings.uiScale }}%</span>
              <button
                type="button"
                class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                :disabled="saving || settings.uiScale >= 150"
                @click="setUIScale(settings.uiScale + 10)"
              >
                +
              </button>
              <button
                type="button"
                class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                :disabled="saving || settings.uiScale === 100"
                @click="setUIScale(100)"
              >
                默认
              </button>
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">会话标签页位置</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                默认「左侧导航」：标签并入左侧栏的纵向列表，可留出更多终端高度；「顶栏」为终端上方的横向标签栏。
              </p>
            </div>
            <div class="seg shrink-0">
              <button
                v-for="opt in TAB_PLACEMENT_OPTIONS"
                :key="opt.key"
                type="button"
                :class="settings.tabBarPlacement === opt.key ? 'active' : ''"
                :aria-pressed="settings.tabBarPlacement === opt.key"
                :disabled="saving"
                @click="setTabBarPlacement(opt.key)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="grid grid-cols-[minmax(0,1fr)_11rem] items-center gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">左侧导航区段顺序</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                自定义左侧导航中「导航 / 会话 / 标签页」三块的排列顺序（「标签页」仅在设为侧栏时显示）。
              </p>
            </div>
            <select
              class="select"
              :value="settings.navSectionOrder"
              :disabled="saving"
              @change="onNavSectionOrderChange"
            >
              <option v-for="opt in NAV_SECTION_ORDER_OPTIONS" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">自动重连</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                检测到 SSH 连接断开后自动重新连接（网络抖动、NAT 超时等情况）。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.autoReconnect"
              :disabled="saving"
              @update:model-value="toggleAutoReconnect"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">终端保活心跳</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                每 15 秒向服务器发送心跳包，防止 NAT / 防火墙或服务端 ClientAliveInterval 超时断开终端。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.keepAliveEnabled"
              :disabled="saving"
              @update:model-value="toggleKeepAlive"
            />
          </div>
        </div>

        <div class="neo">
          <div class="grid grid-cols-[minmax(0,1fr)_11rem] items-center gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">本机终端 Shell</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                工作区「本地终端」使用的本机 Shell。macOS 可选 zsh / bash，Windows 可选 PowerShell / cmd，Linux 使用系统 $SHELL。
              </p>
            </div>
            <select
              class="select"
              :value="settings.localShell"
              :disabled="saving || localShellOptions.length <= 1"
              @change="onLocalShellChange"
            >
              <option v-for="opt in localShellOptions" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">清理命令历史</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                删除本地 SQLite 中记录的命令历史（不影响远程服务器 ~/.bash_history）。用于清除误记的控制字符、缺字命令或隐私命令。
              </p>
            </div>
            <div class="shrink-0 flex items-center gap-2">
              <template v-if="!confirmClearHistory">
                <button
                  type="button"
                  class="btn btn-ghost btn-sm"
                  :disabled="clearingHistory"
                  @click="confirmClearHistory = true"
                >
                  清空全部
                </button>
              </template>
              <template v-else>
                <button
                  type="button"
                  class="btn btn-danger btn-sm"
                  :disabled="clearingHistory"
                  @click="clearAllHistory"
                >
                  {{ clearingHistory ? '清理中…' : '确认清空' }}
                </button>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm"
                  :disabled="clearingHistory"
                  @click="confirmClearHistory = false"
                >
                  取消
                </button>
              </template>
            </div>
          </div>
          <div v-if="historyError || historyMsg" class="px-5 pb-3 text-xs">
            <p v-if="historyError" class="text-rose-400">{{ historyError }}</p>
            <p v-else class="text-emerald-400">{{ historyMsg }}</p>
          </div>
        </div>
      </div>

      <!-- 外观 -->
      <div v-else-if="section === 'theme'" class="max-w-3xl fade-rise">
        <div class="mb-6">
          <h3 class="text-[18px] font-semibold text-white tracking-tight">外观</h3>
          <p class="text-[13px] text-mist mt-1.5">
            选择预设主题一键套用整站配色；也可自定义界面、终端与字体。终端 ANSI 16 色会同步到 ls、vim、提示符等程序输出。
          </p>
        </div>

        <!-- 明暗模式 -->
        <div class="neo px-5 py-4 mb-4">
          <div class="flex items-center justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">界面明暗</p>
              <p class="text-xs text-slate-500 mt-0.5">「跟随系统」时，界面与预设终端色板随系统明暗自动切换。</p>
            </div>
            <div class="seg shrink-0">
              <button
                v-for="opt in TONE_OPTIONS"
                :key="opt.key"
                :class="appearanceForm.baseTone === opt.key ? 'active' : ''"
                @click="setTone(opt.key)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>
        </div>

        <!-- 预设 / 自定义 -->
        <div class="neo p-5 mb-4">
          <div class="flex items-center justify-between mb-4">
            <p class="text-sm font-medium text-slate-200">配色方案</p>
            <div class="seg shrink-0">
              <button
                v-for="m in MODE_OPTIONS"
                :key="m.key"
                :class="appearanceForm.mode === m.key ? 'active' : ''"
                @click="setMode(m.key)"
              >
                {{ m.label }}
              </button>
            </div>
          </div>

          <!-- 预设主题卡片 -->
          <div v-if="appearanceForm.mode === 'preset'" class="grid grid-cols-4 gap-3">
            <button
              v-for="p in PRESETS"
              :key="p.id"
              class="preset-card"
              :class="appearanceForm.presetId === p.id ? 'active' : ''"
              @click="selectPreset(p.id)"
            >
              <span
                class="preset-swatch"
                :style="{background: `linear-gradient(135deg, ${p.primary} 0%, ${p.secondary} 100%)`}"
              >
                <span class="preset-swatch-dark" :style="{background: p.dark.background}"></span>
                <span class="preset-swatch-cursor" :style="{background: p.dark.cursor}"></span>
              </span>
              <span class="preset-name">{{ p.name }}</span>
              <span class="preset-desc">{{ p.desc }}</span>
            </button>
          </div>

          <!-- 自定义：UI 品牌色 -->
          <div v-else class="grid grid-cols-3 gap-4">
            <label class="block">
              <span class="field-label">主色（界面强调）</span>
              <div class="mt-1 flex gap-2 items-center">
                <input v-model="appearanceForm.primary" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                <input v-model="appearanceForm.primary" class="input input-sm flex-1 font-mono" />
              </div>
            </label>
            <label class="block">
              <span class="field-label">辅色（点缀）</span>
              <div class="mt-1 flex gap-2 items-center">
                <input v-model="appearanceForm.secondary" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                <input v-model="appearanceForm.secondary" class="input input-sm flex-1 font-mono" />
              </div>
            </label>
            <label class="block">
              <span class="field-label">界面主文字色</span>
              <div class="mt-1 flex gap-2 items-center">
                <input v-model="appearanceForm.uiText" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                <input v-model="appearanceForm.uiText" class="input input-sm flex-1 font-mono" />
              </div>
            </label>
          </div>
        </div>

        <!-- 终端颜色 -->
        <div class="neo p-5 mb-4">
          <div class="flex items-center justify-between mb-1">
            <p class="text-sm font-medium text-slate-200">终端颜色</p>
            <span v-if="appearanceForm.mode === 'preset'" class="badge live">
              预设 · {{ currentTone === 'light' ? '浅色' : '深色' }}色板
            </span>
            <span v-else class="badge stop">自定义</span>
          </div>
          <p v-if="appearanceForm.mode === 'preset'" class="text-xs text-slate-500 mt-1 mb-4">
            预设模式下终端色板（含 ANSI 16 色）随明暗自动配置。切到「自定义」可完全手动调整。
          </p>

          <!-- 预设模式：终端色板只读预览 -->
          <div v-if="appearanceForm.mode === 'preset'">
            <div class="grid grid-cols-2 gap-3 text-[12px] text-slate-400">
              <div class="flex items-center gap-2">
                <span class="color-dot" :style="{background: themeForm.background}"></span>
                背景 {{ themeForm.background }}
              </div>
              <div class="flex items-center gap-2">
                <span class="color-dot" :style="{background: themeForm.foreground}"></span>
                前景 {{ themeForm.foreground }}
              </div>
            </div>
            <div class="flex flex-wrap gap-1.5 mt-3">
              <span
                v-for="f in ANSI_FIELDS"
                :key="f.key"
                class="ansi-chip"
                :style="{background: themeForm[f.key], color: contrastFor(themeForm[f.key])}"
              >
                {{ f.label }}
              </span>
            </div>
          </div>

          <!-- 自定义模式：完整编辑 -->
          <template v-else>
            <div class="grid grid-cols-2 gap-4 text-[13px]">
              <label class="block">
                <span class="field-label">背景色</span>
                <div class="mt-1 flex gap-2 items-center">
                  <input v-model="themeForm.background" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                  <input v-model="themeForm.background" class="input input-sm flex-1 font-mono" />
                </div>
              </label>
              <label class="block">
                <span class="field-label">文字颜色</span>
                <div class="mt-1 flex gap-2 items-center">
                  <input v-model="themeForm.foreground" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                  <input v-model="themeForm.foreground" class="input input-sm flex-1 font-mono" />
                </div>
              </label>
              <label class="block">
                <span class="field-label">光标颜色</span>
                <div class="mt-1 flex gap-2 items-center">
                  <input v-model="themeForm.cursor" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                  <input v-model="themeForm.cursor" class="input input-sm flex-1 font-mono" />
                </div>
              </label>
              <label class="block">
                <span class="field-label">选中背景色</span>
                <input v-model="themeForm.selection" class="mt-1 w-full input input-sm font-mono text-xs" placeholder="rgba(42, 168, 154, 0.28)" />
              </label>
            </div>

            <div class="mt-5">
              <p class="text-slate-400 text-xs mb-2">ANSI 16 色 · 程序输出（ls / vim / 提示符）</p>
              <div class="grid grid-cols-8 gap-2">
                <label v-for="f in ANSI_FIELDS" :key="f.key" class="block">
                  <span class="text-[10px] text-slate-400">{{ f.label }}</span>
                  <input
                    v-model="themeForm[f.key]"
                    type="color"
                    class="mt-0.5 w-full h-7 rounded-[5px] bg-slate-800 border border-slate-700/60 p-0.5 cursor-pointer"
                  />
                </label>
              </div>
            </div>

            <div class="mt-5">
              <span class="field-label">背景图</span>
              <div class="mt-1 flex gap-2">
                <input v-model="themeForm.bgImage" readonly class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none" placeholder="无（可选）" />
                <button class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-xs shrink-0" @click="pickBgImage">选择…</button>
                <button v-if="themeForm.bgImage" class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-xs shrink-0" @click="themeForm.bgImage = ''">清除</button>
              </div>
            </div>

            <label class="block mt-4">
              <span class="field-label">背景模糊：{{ themeForm.blurAmount }}px</span>
              <input v-model.number="themeForm.blurAmount" type="range" min="0" max="30" class="mt-2 w-full accent-[var(--signal-400)]" />
            </label>

            <div class="flex items-center justify-between gap-4 mt-4">
              <div>
                <p class="text-sm text-slate-300">文字阴影</p>
                <p class="text-xs text-slate-500 mt-0.5">为终端文字添加阴影，提升可读性。</p>
              </div>
              <ToggleSwitch v-model="themeForm.textShadow" />
            </div>

            <label class="block mt-4" :class="themeForm.textShadow ? '' : 'opacity-40 pointer-events-none'">
              <span class="field-label">阴影强度：{{ themeForm.shadowBlur }}px</span>
              <input v-model.number="themeForm.shadowBlur" type="range" min="0" max="10" class="mt-2 w-full accent-[var(--signal-400)]" />
            </label>
          </template>
        </div>

        <!-- 字体 -->
        <div class="neo p-5 mb-4">
          <p class="text-sm font-medium text-slate-200 mb-4">字体</p>
          <div class="grid grid-cols-2 gap-4">
            <label class="block">
              <span class="field-label">界面字体</span>
              <input
                v-model="fontsForm.uiFont"
                list="uiFontList"
                class="input input-sm mt-1 font-mono"
                placeholder="Inter / Sora / Manrope / system"
              />
              <datalist id="uiFontList">
                <option v-for="f in UI_FONT_OPTIONS" :key="f" :value="f" />
              </datalist>
            </label>
            <label class="block">
              <span class="field-label">终端等宽字体</span>
              <input
                v-model="fontsForm.terminalFont"
                list="termFontList"
                class="input input-sm mt-1 font-mono"
                placeholder="IBM Plex Mono / JetBrains Mono / system"
              />
              <datalist id="termFontList">
                <option v-for="f in TERMINAL_FONT_OPTIONS" :key="f" :value="f" />
              </datalist>
            </label>
          </div>
          <label class="block mt-4">
            <span class="field-label">终端字号：{{ fontsForm.terminalFontSize }}px</span>
            <input
              v-model.number="fontsForm.terminalFontSize"
              type="range"
              min="8"
              max="24"
              class="mt-2 w-full accent-[var(--signal-400)]"
            />
          </label>
          <p class="text-xs text-slate-500 mt-2">已捆绑的开源字体开箱即用；输入系统已安装字体名亦可生效（字体名需与系统一致）。</p>
        </div>

        <div class="flex justify-end gap-2">
          <button class="btn btn-ghost btn-sm" @click="resetAppearance">恢复默认</button>
        </div>
      </div>

      <!-- 保存的凭证 -->
      <div v-else-if="section === 'credentials'" class="max-w-2xl fade-rise">
        <div class="mb-6 flex items-start justify-between gap-4">
          <div>
            <h3 class="text-[18px] font-semibold text-white tracking-tight">保存的凭证</h3>
            <p class="text-[13px] text-mist mt-1.5">保存常用用户名密码或私钥，新建服务器时可直接选择自动填充。</p>
          </div>
          <button class="btn btn-primary btn-sm shrink-0" @click="openNewCred">
            <Icon name="plus" :size="14" />
            新增凭证
          </button>
        </div>

        <div class="neo">
          <div class="px-5 py-4 space-y-2">
            <div v-if="!credentials.list.length" class="text-xs text-slate-500 py-2">暂无凭证，点击右上角「新增凭证」添加。</div>
            <div
              v-for="c in credentials.list"
              :key="c.id"
              class="flex items-center gap-3 px-3 py-2 rounded-md bg-slate-800/50 border border-slate-700/40"
            >
              <div class="min-w-0 flex-1">
                <p class="text-[13px] text-slate-200 truncate">{{ c.name }}</p>
                <p class="text-[12px] text-slate-500 truncate">
                  {{ c.user }} · {{ c.authType === 'privateKey' ? 'RSA 私钥' : '密码' }}
                  <span class="font-mono text-slate-400">
                    {{ c.authType === 'privateKey' ? '••••••••' : revealId === c.id ? c.password || '' : '••••••••' }}
                  </span>
                </p>
              </div>
              <div v-if="confirmCredId !== c.id" class="flex items-center gap-1 shrink-0">
                <button class="btn-icon btn-sm" :title="revealId === c.id ? '隐藏' : '查看密码'" @click="toggleReveal(c)">
                  <Icon name="eye" :size="14" extra-class="text-slate-300" />
                </button>
                <button class="btn-icon btn-sm" :title="c.authType === 'privateKey' ? '复制私钥' : '复制密码'" @click="copyCred(c)">
                  <Icon :name="copiedCredId === c.id ? 'check' : 'copy'" :size="14" :extra-class="copiedCredId === c.id ? 'text-emerald-400' : 'text-slate-300'" />
                </button>
                <button class="btn-icon btn-sm" title="编辑" @click="openEditCred(c)">
                  <Icon name="pencil" :size="14" extra-class="text-slate-300" />
                </button>
                <button class="btn-icon btn-sm" title="删除" @click="confirmCredId = c.id">
                  <Icon name="trash" :size="14" extra-class="text-slate-300" />
                </button>
              </div>
              <div v-else class="flex items-center gap-1 shrink-0">
                <button class="px-2 py-1 rounded bg-rose-600/80 hover:bg-rose-500 text-white text-xs" @click="removeCredential(c.id)">确认</button>
                <button class="px-2 py-1 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs" @click="confirmCredId = ''">取消</button>
              </div>
            </div>
          </div>
        </div>

        <CredentialDialog v-model="showCredDialog" :editing="editingCred" />

        <!-- 私钥明文查看面板 -->
        <Teleport to="body">
          <div v-if="revealKey" class="modal-root" @click.self="revealKey = null">
            <div class="modal neo" style="width:min(560px,92vw);max-height:82vh;display:flex;flex-direction:column">
              <div class="flex items-start justify-between gap-4 px-6 pt-6 pb-4">
                <div>
                  <h3 class="!mb-1">RSA 私钥内容</h3>
                  <p class="mdesc !mb-0">「{{ revealKey.name }}」保存的私钥明文，仅保存在本机。</p>
                </div>
                <button class="btn-icon btn-sm" title="关闭" @click="revealKey = null">
                  <Icon name="close" :size="14" />
                </button>
              </div>
              <div class="flex-1 overflow-y-auto px-6 pb-2">
                <textarea
                  readonly
                  rows="10"
                  spellcheck="false"
                  class="w-full px-3 py-2 rounded-md bg-slate-900 border border-slate-700/60 text-slate-200 font-mono text-xs leading-relaxed outline-none resize-none"
                  :value="revealKey.keyContent || ''"
                ></textarea>
              </div>
              <div class="flex justify-end gap-2 px-6 py-5">
                <button class="btn btn-ghost" @click="revealKey = null">关闭</button>
                <button class="btn btn-primary" @click="copyCred(revealKey); revealKey = null">复制私钥</button>
              </div>
            </div>
          </div>
        </Teleport>
      </div>

      <!-- 安全 -->
      <div v-else-if="section === 'debug'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">调试模式</h3>
          <p class="text-[13px] text-mist mt-1.5 leading-relaxed">
            在本地开一个控制面（HTTP，后续提供 MCP），供 AI / 自动化读取状态、驱动终端、订阅输出。
            默认关闭；开启后仅监听本机（可切换局域网），token 每次启动重新生成。
          </p>
        </div>

        <div class="neo border border-amber-500/30">
          <div class="px-5 py-4 flex items-start gap-3">
            <div class="min-w-0 text-xs text-amber-200/90 leading-relaxed">
              调试模式允许调用方读写终端；「允许执行 JS」等于把整个界面交出去。请只在可信环境开启，
              不用时立刻关闭。token 与端口写在
              <span class="font-mono">{{ debugInfo?.infoFile || '%APPDATA%\\ding-ssh\\debug.json' }}</span>。
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">启用调试模式</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">关闭时完全不监听任何端口。修改后立即生效（无需重启）。</p>
            </div>
            <ToggleSwitch
              :model-value="settings.debug.enabled"
              :disabled="saving"
              @update:model-value="(v: boolean) => setDebug({enabled: v})"
            />
          </div>
          <div class="px-5 py-3 border-t border-slate-800/60 grid grid-cols-[minmax(0,1fr)_8rem] items-center gap-4">
            <span class="field-label">监听端口（0 = 自动挑空闲端口）</span>
            <input
              class="select"
              type="number"
              min="0"
              max="65535"
              :value="settings.debug.port"
              :disabled="saving"
              @change="onDebugPortChange"
            />
          </div>
          <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">允许局域网访问</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">关闭时只监听 127.0.0.1；开启后绑定 0.0.0.0（请配合 token 使用）。</p>
            </div>
            <ToggleSwitch
              :model-value="settings.debug.bindLan"
              :disabled="saving"
              @update:model-value="(v: boolean) => setDebug({bindLan: v})"
            />
          </div>
          <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">允许执行 JS（危险）</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                放开 <span class="font-mono">/v1/eval</span>：可在页面上下文执行任意脚本，等于完全控制界面。默认关闭。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.debug.allowEval"
              :disabled="saving"
              @update:model-value="(v: boolean) => setDebug({allowEval: v})"
            />
          </div>
          <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">允许读取敏感数据</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">放开设置 / 服务器配置中可能含密钥字段的读取。默认关闭。</p>
            </div>
            <ToggleSwitch
              :model-value="settings.debug.allowSecrets"
              :disabled="saving"
              @update:model-value="(v: boolean) => setDebug({allowSecrets: v})"
            />
          </div>
        </div>

        <!-- MCP 能力（权限内核）：AI 能做什么，由这里的开关决定 -->
        <div class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">MCP 能力</p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">
              逐项控制 AI 能做什么。读取类能力永远允许（敏感字段仍受「允许读取敏感数据」控制）；
              关闭的能力会让工具返回中文拒绝原因，不会影响其余功能。
            </p>
          </div>
          <div
            v-for="row in capRows"
            :key="row.name"
            class="px-5 py-4 border-b border-slate-800/60 flex items-start justify-between gap-4"
          >
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">
                {{ row.label }}
                <span class="font-mono text-[11px] text-slate-500 ml-1">{{ row.name }}</span>
                <span v-if="row.needsConfirm" class="ml-2 text-[11px] text-amber-300/90">高风险 · 需二次确认</span>
              </p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">{{ row.desc }}</p>
              <p v-if="row.blocked" class="text-[11px] text-amber-300/90 mt-1">
                需先开启「允许读取敏感数据」（由 secrets.read 控制）才能打开本能力。
              </p>
            </div>
            <!-- 敏感能力：先弹确认，再写入（关闭可直接关） -->
            <div v-if="pendingCap === row.name" class="flex items-center gap-2 shrink-0">
              <button class="btn btn-danger btn-sm" :disabled="saving" @click="applyCapability(row.name, true)">确认开启</button>
              <button class="btn btn-ghost btn-sm" :disabled="saving" @click="pendingCap = ''">取消</button>
            </div>
            <ToggleSwitch
              v-else
              :model-value="row.enabled"
              :disabled="saving"
              @update:model-value="(v: boolean) => toggleCapability(row.name, v)"
            />
          </div>

          <!-- 远端文件写入的路径白名单：留空 = 禁止任何写入路径 -->
          <div class="px-5 py-4 border-b border-slate-800/60 space-y-2">
            <p class="text-sm font-medium text-slate-200">远端可写路径白名单</p>
            <p class="text-xs text-slate-500 leading-relaxed">
              一行一个<b>绝对路径前缀</b>，只有命中前缀的远端路径才允许写入 / 删除。
              <b>留空 = 禁止任何写入路径</b>；仅「远端文件写入」能力开启时生效。
            </p>
            <textarea
              v-model="allowlistText"
              class="input font-mono text-xs"
              rows="3"
              spellcheck="false"
              placeholder="/srv/app&#10;/home/deploy/releases"
              @input="allowlistDirty = true"
            ></textarea>
            <div class="flex items-center gap-2">
              <button class="btn btn-primary btn-sm" :disabled="saving" @click="saveAllowlist">保存白名单</button>
              <span class="text-[11px] text-slate-500">当前生效：{{ (capInfo?.sftpWriteAllowlist ?? []).join('、') || '（空，禁止任何写入路径）' }}</span>
            </div>
          </div>

          <!-- 两段式确认：固定开启，不给开关 -->
          <div class="px-5 py-4">
            <p class="text-sm font-medium text-slate-200">破坏性操作两段式确认（固定开启）</p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">
              只对<b>不可逆</b>操作生效（删服务器 / 删凭据 / 改隧道 / SFTP 写删 / 改设置 / 清日志 / 退出重载 / 开启敏感能力）。
              AI / 脚本必须先调用
              <span class="font-mono">confirm.prepare</span>
              拿到一次性 token（默认 120 秒有效、绑定本次参数），再用
              <span class="font-mono">confirm.commit</span>
              或把 token 传给对应工具才会执行；缺少 token 一律拒绝。此项<b>不提供开关</b>。
            </p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">
              <b>可逆 / 高频操作不需要 token</b>：终端输入（
              <span class="font-mono">send_input</span>）、
              <span class="font-mono">eval_js</span>
              、界面写入只受上面的能力位约束 —— 否则 AI 管 SSH 与「改一点看一眼」的界面迭代都没法用。
            </p>
          </div>
          <div v-if="capError || capMsg" class="px-5 pb-4 text-xs space-y-1">
            <p v-if="capError" class="text-rose-400 break-all">{{ capError }}</p>
            <p v-else-if="capMsg" class="text-emerald-400 break-all">{{ capMsg }}</p>
          </div>
        </div>

        <div class="neo">
          <div class="px-5 py-3 border-b border-slate-800/60 flex items-center justify-between">
            <span class="field-label">运行状态</span>
            <button class="btn btn-ghost btn-sm" :disabled="saving" @click="refreshDebugInfo">刷新</button>
          </div>
          <div class="px-5 py-4 text-xs space-y-2">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full" :class="debugInfo?.running ? 'bg-emerald-400' : 'bg-slate-600'"></span>
              <span class="field-label">{{ debugInfo?.running ? '运行中' : '未运行' }}</span>
            </div>
            <div v-if="debugInfo?.url" class="font-mono break-all text-slate-300">{{ debugInfo.url }}</div>
            <div v-if="debugInfo?.lanUrl" class="font-mono break-all text-slate-500">局域网：{{ debugInfo.lanUrl }}</div>
            <div v-if="debugInfo?.token" class="font-mono break-all text-slate-500">token：{{ debugInfo.token }}</div>
            <p class="text-slate-500">{{ debugInfo?.hint }}</p>
            <p v-if="debugInfo?.url" class="text-slate-500">
              示例：<span class="font-mono break-all">curl -H "Authorization: Bearer &lt;token&gt;" {{ debugInfo.url }}/v1/terminals</span>
            </p>
          </div>
        </div>
      </div>

      <!-- AI 记录（审计） -->
      <div v-else-if="section === 'audit'" class="max-w-3xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">AI 记录</h3>
          <p class="text-[13px] text-mist mt-1.5 leading-relaxed">
            调试接口与 MCP 的每次调用都会留一条记录（参数已脱敏，密码 / token / 私钥一律打码为
            <span class="font-mono">***</span>）；每条记录同时通过
            <span class="font-mono">debug.audit</span> 事件实时推给本页。只保留最近
            {{ auditLimit }} 条以内的环形缓冲（上限 500 条）。
          </p>
        </div>

        <!-- 过滤器 -->
        <div class="neo">
          <div class="px-5 py-4 flex flex-wrap items-center gap-2">
            <button
              v-for="opt in [
                {key: 'all', label: '全部'},
                {key: 'write', label: '仅写操作'},
                {key: 'failed', label: '仅失败'},
              ]"
              :key="opt.key"
              class="btn btn-sm"
              :class="auditFilter === opt.key ? 'btn-primary' : 'btn-ghost'"
              @click="auditFilter = opt.key as 'all' | 'write' | 'failed'"
            >
              {{ opt.label }}
            </button>
            <select v-model="auditCap" class="select max-w-[16rem]" @change="refreshAudit">
              <option value="">按能力筛选：全部</option>
              <option v-for="c in auditCapOptions" :key="c.value" :value="c.value">{{ c.label }}</option>
            </select>
            <select v-model.number="auditLimit" class="select max-w-[8rem]" @change="refreshAudit">
              <option :value="20">20 条</option>
              <option :value="50">50 条</option>
              <option :value="200">200 条</option>
            </select>
            <button class="btn btn-ghost btn-sm" :disabled="auditLoading" @click="refreshAudit">
              <Icon name="refresh" :size="14" />
              {{ auditLoading ? '刷新中…' : '刷新' }}
            </button>
          </div>
          <div v-if="auditError || auditMsg" class="px-5 pb-4 text-xs space-y-1">
            <p v-if="auditError" class="text-rose-400 break-all">{{ auditError }}</p>
            <p v-else-if="auditMsg" class="text-emerald-400 break-all">{{ auditMsg }}</p>
          </div>
        </div>

        <!-- 记录列表 -->
        <div class="neo">
          <div class="px-5 py-3 border-b border-slate-800/60 flex items-center justify-between">
            <span class="field-label">记录（{{ filteredAudit.length }} 条）</span>
            <span class="text-[11px] text-slate-500">来源：MCP / HTTP / 界面</span>
          </div>
          <p v-if="filteredAudit.length === 0" class="px-5 py-6 text-xs text-slate-500">
            暂无记录。开启调试模式并让 AI 调用一次工具后，这里会实时出现。
          </p>
          <div
            v-for="rec in filteredAudit"
            :key="rec.id"
            class="px-5 py-3 border-b border-slate-800/60 text-xs space-y-1"
          >
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-mono text-slate-500">{{ formatAuditTime(rec.ts) }}</span>
              <span class="px-1.5 py-0.5 rounded bg-slate-800/70 text-slate-300">{{ auditSourceLabel(rec.source) }}</span>
              <span class="font-mono text-slate-200 break-all">{{ rec.tool }}</span>
              <span v-if="rec.cap" class="font-mono text-[11px] text-signal break-all">{{ rec.cap }}</span>
              <span class="text-slate-500">{{ rec.durationMs }}ms</span>
              <span v-if="rec.ok" class="text-emerald-400">成功</span>
              <span v-else class="text-rose-400 break-all">失败：{{ rec.error }}</span>
              <button class="btn btn-ghost btn-sm ml-auto" @click="expandedAudit = expandedAudit === rec.id ? '' : rec.id">
                {{ expandedAudit === rec.id ? '收起' : '参数' }}
              </button>
              <button
                v-if="rec.reversible"
                class="btn btn-ghost btn-sm"
                :title="rec.revertHint"
                @click="undoAudit(rec)"
              >
                撤销
              </button>
              <button
                v-else-if="rec.revertHint"
                class="btn btn-ghost btn-sm opacity-50 cursor-not-allowed"
                disabled
                :title="rec.revertHint"
              >
                撤销
              </button>
            </div>
            <pre
              v-if="expandedAudit === rec.id"
              class="mt-1 p-2 rounded bg-slate-900/70 text-[11px] text-slate-300 whitespace-pre-wrap break-all"
            >{{ rec.args }}</pre>
          </div>
        </div>

        <div class="neo">
          <div class="px-5 py-4 text-xs text-slate-500 leading-relaxed">
            撤销说明：本波只对<b>设置类改动</b>（AI 调用
            <span class="font-mono">update_settings</span> /
            <span class="font-mono">set_log_options</span>）提供「恢复为改动前值」，
            快照只保留最近若干条；其余动作（删服务器、清日志、退出应用等）无法可靠回滚，
            「撤销」按钮会置灰并给出原因，不会假装支持。
          </div>
        </div>
      </div>

      <!-- 日志 -->
      <div v-else-if="section === 'logs'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">日志</h3>
          <p class="text-[13px] text-mist mt-1.5 leading-relaxed">
            控制运行日志打到哪里、记多细；排障结束后可一键导出诊断包。日志只落在本机，token / 密码 / 私钥会自动打码。
          </p>
        </div>

        <!-- 输出目标：控制台 / 文件 -->
        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">运行日志（控制台）</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                开启后日志同时打印到 <span class="font-mono">wails dev</span> 的控制台（启动终端），开发调试时最直观；
                正式使用可以关掉它、改成写入日志文件。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.logEnabled"
              :disabled="saving"
              @update:model-value="(v: boolean) => applyLogOptions({consoleEnabled: v})"
            />
          </div>
          <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">写入日志文件</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                开启后日志写进本机文件并自动轮转，关掉应用也能翻旧账；遇到问题时把文件（或诊断包）发给开发者即可定位。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.logToFile"
              :disabled="saving"
              @update:model-value="(v: boolean) => applyLogOptions({fileEnabled: v})"
            />
          </div>
          <div class="px-5 py-3 border-t border-slate-800/60 flex items-start gap-3 text-xs">
            <span class="field-label shrink-0">日志目录</span>
            <span class="font-mono text-slate-300 break-all">{{ logInfo?.dir || '（暂不可用）' }}</span>
          </div>
          <div class="px-5 py-3 border-t border-slate-800/60 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs">
            <span class="field-label">
              当前总大小：<span class="font-mono text-slate-300">{{ formatBytes(logTotalSize) }}</span>
            </span>
            <span class="field-label">
              文件数：<span class="font-mono text-slate-300">{{ logFiles.length }}</span>
            </span>
            <button class="btn btn-ghost btn-sm ml-auto" :disabled="saving" @click="refreshLogInfo">刷新</button>
          </div>
        </div>

        <!-- 详细程度 -->
        <div class="neo">
          <div class="grid grid-cols-[minmax(0,1fr)_9rem] items-center gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">日志级别</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                debug 会记录逐条调用细节（参数、耗时），信息最全但文件增长最快；日常用 info，复现问题时临时切到 debug 更省事。
              </p>
            </div>
            <select class="select" :value="settings.logLevel" :disabled="saving" @change="onLogLevelChange">
              <option v-for="opt in LOG_LEVEL_OPTIONS" :key="opt.key" :value="opt.key">{{ opt.label }}</option>
            </select>
          </div>
          <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">记录 MCP / 调试 API 调用</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                记录每次 HTTP / MCP 调用的方法、工具名、状态码与耗时，方便核对 AI / 自动化到底做了什么；调用密集时日志体积增长较快，排障完记得关掉。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.logApiCalls"
              :disabled="saving"
              @update:model-value="(v: boolean) => applyLogOptions({apiLogEnabled: v})"
            />
          </div>
        </div>

        <!-- 维护动作 -->
        <div class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">维护</p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">打开目录手动查看，或清理 / 导出。</p>
          </div>
          <div class="px-5 py-4 flex flex-wrap items-center gap-2">
            <button class="btn btn-ghost btn-sm" :disabled="saving" @click="openLogFolder">
              <Icon name="folder" :size="14" />
              打开日志文件夹
            </button>
            <template v-if="!confirmClearLogs">
              <button class="btn btn-ghost btn-sm" :disabled="logBusy" @click="confirmClearLogs = true">清理日志</button>
            </template>
            <template v-else>
              <button class="btn btn-danger btn-sm" :disabled="logBusy" @click="clearLogs">
                {{ logBusy ? '清理中…' : '确认清理' }}
              </button>
              <button class="btn btn-ghost btn-sm" :disabled="logBusy" @click="confirmClearLogs = false">取消</button>
            </template>
            <button class="btn btn-primary btn-sm" :disabled="logBusy" @click="exportDiagnostics">
              <Icon name="package" :size="14" />
              {{ logBusy ? '处理中…' : '导出诊断包' }}
            </button>
          </div>
          <div v-if="logError || logMsg || exportPath" class="px-5 pb-4 text-xs space-y-1">
            <p v-if="logError" class="text-rose-400 break-all">{{ logError }}</p>
            <p v-else-if="logMsg" class="text-emerald-400 break-all">{{ logMsg }}</p>
            <p v-if="exportPath" class="text-emerald-400 break-all">
              已导出诊断包：<span class="font-mono">{{ exportPath }}</span>，可直接发给开发者。
            </p>
          </div>
        </div>

        <!-- 保留策略 + 敏感信息 -->
        <div class="neo">
          <div class="px-5 py-4 space-y-2">
            <p class="text-sm font-medium text-slate-200">保留策略</p>
            <p class="text-xs text-slate-500 leading-relaxed">
              单个日志文件写到 <span class="font-mono text-slate-300">{{ formatBytes(logRetention.maxBytes) }}</span> 自动轮转，
              只保留最近 <span class="font-mono text-slate-300">{{ logRetention.keepFiles }}</span> 个文件，更早的自动删除，不会越积越多占满磁盘。
            </p>
            <p v-if="!logInfo?.retention" class="text-[12px] text-slate-600">
              后端未返回保留策略，以上为默认值（单文件 8MB、保留最近 20 个）。
            </p>
            <p class="text-xs text-slate-500 leading-relaxed">
              敏感信息：日志中的 token / 密码 / 私钥会自动打码，固定开启、无法关闭；导出诊断包同样经过打码处理。
            </p>
          </div>
        </div>

        <!-- AI / MCP 读日志的入口说明（工具与资源名与后端一致） -->
        <p class="text-xs text-slate-500 leading-relaxed">AI / MCP 也能读取这些日志用于问题定位：工具 <span class="font-mono text-slate-300">list_logs</span>、<span class="font-mono text-slate-300">read_logs</span>（支持按级别/关键字过滤）、<span class="font-mono text-slate-300">export_diagnostics</span>，资源 <span class="font-mono text-slate-300">app://logs</span>、<span class="font-mono text-slate-300">app://logs/tail</span>。</p>

        <!-- 最近日志（只读，打开页签 / 点刷新时读一次，不轮询） -->
        <div class="neo">
          <div class="px-5 py-3 border-b border-slate-800/60 flex items-center justify-between gap-3">
            <span class="field-label">最近日志</span>
            <div class="shrink-0 flex items-center gap-2">
              <select
                class="select w-28"
                :value="logTailLines"
                :disabled="logTailLoading"
                @change="onLogTailLinesChange"
              >
                <option v-for="n in LOG_TAIL_LINES" :key="n" :value="n">最近 {{ n }} 行</option>
              </select>
              <button class="btn btn-ghost btn-sm" :disabled="logTailLoading" @click="refreshLogTail">
                {{ logTailLoading ? '读取中…' : '刷新' }}
              </button>
            </div>
          </div>
          <pre class="m-0 px-5 py-4 max-h-[420px] overflow-auto font-mono text-[12px] leading-relaxed text-slate-300 whitespace-pre-wrap break-all">{{ logTail || '暂无日志' }}</pre>
        </div>
      </div>

      <div v-else-if="section === 'security'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">安全</h3>
          <p class="text-[13px] text-mist mt-1.5">
            敏感字段 AES-256-GCM 加密；可选启动主密码（Argon2id）。
          </p>
        </div>

        <div class="rounded-xl border border-slate-700/60 bg-slate-900/60 px-5 py-4 text-xs space-y-2">
          <p class="text-slate-300">
            状态：
            <span :class="security?.unlocked ? 'text-emerald-400' : 'text-amber-400'">
              {{ security?.unlocked ? '已解锁' : '未解锁' }}
            </span>
          </p>
          <p class="text-slate-500">
            主密码：{{ security?.masterPasswordEnabled ? '已启用' : '未启用' }}
            · 钥匙串：{{ security?.keyringAvailable ? '可用' : '不可用（已回退本地密钥文件）' }}
          </p>
        </div>

        <div v-if="!security?.masterPasswordEnabled" class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">启用启动主密码</p>
            <p class="text-xs text-slate-500 mt-1">开启后每次启动需输入密码才能访问服务器与凭证。</p>
          </div>
          <div class="px-5 py-4 space-y-3">
            <input v-model="secForm.password" type="password" class="input input-sm" placeholder="新主密码" />
            <input v-model="secForm.confirm" type="password" class="input input-sm" placeholder="确认主密码" />
            <div class="flex items-center justify-between">
              <p v-if="secError" class="text-xs text-rose-400">{{ secError }}</p>
              <p v-else-if="secMsg" class="text-xs text-emerald-400">{{ secMsg }}</p>
              <button class="btn btn-primary btn-sm" :disabled="secBusy" @click="enableMaster">
                {{ secBusy ? '处理中…' : '启用' }}
              </button>
            </div>
          </div>
        </div>

        <template v-else>
          <div class="neo">
            <div class="px-5 py-4 border-b border-slate-800/60">
              <p class="text-sm font-medium text-slate-200">更换主密码</p>
            </div>
            <div class="px-5 py-4 space-y-3">
              <input v-model="secForm.oldPassword" type="password" class="input input-sm" placeholder="当前主密码" />
              <input v-model="secForm.newPassword" type="password" class="input input-sm" placeholder="新主密码" />
              <input v-model="secForm.newConfirm" type="password" class="input input-sm" placeholder="确认新主密码" />
              <div class="flex justify-end">
                <button class="btn btn-primary btn-sm" :disabled="secBusy" @click="changeMaster">更换</button>
              </div>
            </div>
          </div>

          <div class="neo">
            <div class="px-5 py-4 border-b border-slate-800/60">
              <p class="text-sm font-medium text-slate-200">关闭主密码</p>
              <p class="text-xs text-slate-500 mt-1">关闭后主密钥改存系统钥匙串（失败时回退本地密钥文件）。</p>
            </div>
            <div class="px-5 py-4 space-y-3">
              <input v-model="secForm.oldPassword" type="password" class="input input-sm" placeholder="当前主密码" />
              <div class="flex items-center justify-between">
                <p v-if="secError" class="text-xs text-rose-400">{{ secError }}</p>
                <p v-else-if="secMsg" class="text-xs text-emerald-400">{{ secMsg }}</p>
                <button class="ml-auto px-4 py-1.5 rounded-md bg-slate-700/70 hover:bg-rose-600/80 text-slate-200 text-xs" :disabled="secBusy" @click="disableMaster">关闭主密码</button>
              </div>
            </div>
          </div>
        </template>
      </div>

      <!-- 导入导出 -->
      <div v-else-if="section === 'migrate'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">导入导出</h3>
          <p class="text-[13px] text-mist mt-1.5">使用加密的 .dingpack 在设备间迁移服务器、凭证与设置。</p>
        </div>

        <div class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">导出 .dingpack</p>
          </div>
          <div class="px-5 py-4 space-y-3">
            <input v-model="exportPass" type="password" class="input input-sm" placeholder="导出密码" />
            <input v-model="exportPassConfirm" type="password" class="input input-sm" placeholder="确认导出密码" />
            <div class="flex items-center justify-between">
              <p v-if="exportError" class="text-xs text-rose-400 break-all">{{ exportError }}</p>
              <p v-else-if="exportMsg" class="text-xs text-emerald-400 break-all">{{ exportMsg }}</p>
              <button class="btn btn-primary btn-sm ml-auto" :disabled="exportBusy" @click="exportPack">
                {{ exportBusy ? '处理中…' : '导出…' }}
              </button>
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">导入 .dingpack</p>
          </div>
          <div class="px-5 py-4 space-y-3">
            <input v-model="importPass" type="password" class="input input-sm" placeholder="导入密码" />
            <label class="flex items-center gap-2 text-xs text-slate-400">
              <input v-model="packOverwrite" type="checkbox" class="rounded border-slate-600" />
              同 ID 覆盖已有服务器 / 凭证
            </label>
            <div class="flex items-center justify-between">
              <p v-if="importError" class="text-xs text-rose-400 break-all">{{ importError }}</p>
              <p v-else-if="importMsg" class="text-xs text-emerald-400 break-all">{{ importMsg }}</p>
              <button class="btn btn-primary btn-sm ml-auto" :disabled="importBusy" @click="importPack">
                {{ importBusy ? '处理中…' : '导入…' }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- 关于 -->
      <div v-else class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">关于</h3>
          <p class="text-[13px] text-mist mt-1.5">版本信息与构建详情，反馈问题时请一并附上。</p>
        </div>

        <div class="neo">
          <div class="flex items-center gap-3 px-5 py-4 border-b border-slate-800/60">
            <div class="brand-mark">
              <Icon name="zap" :size="20" extra-class="text-signal" />
            </div>
            <div class="min-w-0">
              <div class="brand-name">ding<span>-ssh</span></div>
              <p class="text-xs text-slate-500 mt-0.5">Local-first SSH workstation</p>
            </div>
            <span class="chip ml-auto">{{ aboutRows[0].value }}</span>
          </div>
          <div class="px-5 py-2">
            <div
              v-for="row in aboutRows"
              :key="row.label"
              class="flex items-center justify-between gap-4 py-2.5 border-b border-slate-800/40 last:border-b-0"
            >
              <span class="text-xs text-slate-500 shrink-0">{{ row.label }}</span>
              <span class="font-mono text-xs text-slate-200 text-right break-all">{{ row.value }}</span>
            </div>
          </div>
        </div>

        <div class="flex items-center gap-3">
          <button class="btn btn-ghost btn-sm" @click="copyAbout">
            <Icon name="copy" :size="14" />
            {{ aboutCopied ? '已复制' : '复制版本信息' }}
          </button>
          <p class="text-xs text-slate-500">构建时间取本次打包时的时间戳，可用于确认是否运行了最新版本。</p>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 预设主题卡片 */
.preset-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 12px;
  border-radius: var(--radius-md);
  background: var(--hover);
  box-shadow: inset 0 0 0 1px var(--line-strong);
  transition: box-shadow 160ms var(--ease), background 160ms var(--ease);
}
.preset-card:hover {
  background: var(--hover-strong);
}
.preset-card.active {
  background: var(--signal-weak);
  box-shadow: inset 0 0 0 1px var(--signal-strong-border);
}
.preset-swatch {
  position: relative;
  width: 100%;
  height: 44px;
  border-radius: var(--radius-sm);
  overflow: hidden;
  box-shadow: inset 0 0 0 1px rgba(255,255,255,0.12);
}
.preset-swatch-dark {
  position: absolute;
  left: 6px;
  top: 6px;
  width: 22px;
  height: 22px;
  border-radius: 4px;
  box-shadow: inset 0 0 0 1px rgba(255,255,255,0.18);
}
.preset-swatch-cursor {
  position: absolute;
  right: 6px;
  bottom: 6px;
  width: 10px;
  height: 10px;
  border-radius: 2px;
}
.preset-name {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--mist-100);
}
.preset-desc {
  font-size: 11px;
  color: var(--mist-400);
  line-height: 1.4;
}
/* 终端色板预览 */
.ansi-chip {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  box-shadow: inset 0 0 0 1px rgba(255,255,255,0.1);
}
.color-dot {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  flex-shrink: 0;
  box-shadow: inset 0 0 0 1px rgba(255,255,255,0.14);
}
</style>
