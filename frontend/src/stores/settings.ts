import {defineStore} from 'pinia'
import {DEFAULT_COMPLETION_NAV_HOTKEY} from '../completion/hotkey'
import {settingsService} from '../services/settings'
import {paletteToTheme, defaultPreset} from '../theme/presets'
import type {DebugSettings, Fonts, LogLevel, NavSectionOrder, RightClickAction, TabBarPlacement, TerminalColorMode, Theme, UIAppearance} from '../types'

// 默认终端主题（与 Go 端 models.DefaultTheme 保持一致，含 ANSI 16 色）。
export function defaultTheme(): Theme {
  return paletteToTheme(defaultPreset().dark)
}

// 默认 UI 外观（信号青绿预设 + 跟随系统明暗）。
export function defaultAppearance(): UIAppearance {
  return {
    mode: 'preset',
    presetId: 'signal',
    baseTone: 'auto',
    primary: '#3ec4b4',
    secondary: '#c97a4a',
    uiText: '#eef1f5',
  }
}

// 默认字体设置（与 Go 端 models.DefaultFonts 保持一致）。
export function defaultFonts(): Fonts {
  return {
    uiFont: 'Inter',
    terminalFont: 'IBM Plex Mono',
    terminalFontSize: 13,
  }
}

// 默认调试模式设置（与 Go 端 models.DefaultDebugSettings 保持一致）：
// 关闭、仅本机、默认端口；能力位里只有「终端输入」默认开启（read 永远允许）。
export function defaultDebug(): DebugSettings {
  return {
    enabled: false,
    port: 8765,
    bindLan: false,
    allowEval: false,
    allowSecrets: false,
    cdpEnabled: false,
    capTerminalInput: true,
    capUiWrite: false,
    capConfigWrite: false,
    capSecretWrite: false,
    capRemoteFsWrite: false,
    // sudo.credential：用应用里保存的密码执行 sudo -i（默认关；还需 allowSecrets）
    capSudoCredential: false,
    capLifecycle: false,
    sftpWriteAllowlist: [],
  }
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    logEnabled: false,
    copyOnSelect: false,
    webGLEnabled: true,
    completionEnabled: true,
    completionNavHotkey: DEFAULT_COMPLETION_NAV_HOTKEY,
    completionPanelLimit: 8,
    sftpToTerminalSync: true,
    terminalToSftpSync: true,
    uiScale: 100,
    theme: defaultTheme() as Theme,
    appearance: defaultAppearance() as UIAppearance,
    fonts: defaultFonts() as Fonts,
    autoReconnect: true,
    keepAliveEnabled: true,
    // 登录 SSH 服务器后自动采集系统指标（默认开启）。关闭后连接服务器不执行任何采集命令。
    sysInfoCollectEnabled: true,
    localShell: '',
    // 启动时自动重新连接「上次关闭时仍在打开的服务器」：默认关闭
    autoLoginLastServer: false,
    // 上次关闭应用时打开的服务器 ID 列表（由工作区在标签增删时写入）
    lastOpenServers: [] as string[],
    tabBarPlacement: 'side' as TabBarPlacement,
    rightClickAction: 'menu' as RightClickAction,
    navSectionOrder: 'nav,sessions,tabs' as NavSectionOrder,
    // 终端输出语义着色：默认「语义着色」（off 关 | auto 语义 | vivid 语义 + 强调）
    terminalColorMode: 'auto' as TerminalColorMode,
    debug: defaultDebug() as DebugSettings,
    // 日志页签（对应 Go 端 models.Settings 的日志字段）
    logToFile: false,
    logLevel: 'info' as LogLevel,
    logApiCalls: false,
    logTraceTabs: [] as string[],
    loaded: false,
  }),
  actions: {
    async load() {
      const settings = await settingsService.getSettings()
      this.logEnabled = settings.logEnabled
      this.copyOnSelect = settings.copyOnSelect ?? false
      this.webGLEnabled = settings.webGLEnabled ?? true
      this.completionEnabled = settings.completionEnabled ?? true
      this.completionNavHotkey = settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY
      this.completionPanelLimit = clampPanelLimit(settings.completionPanelLimit)
      this.sftpToTerminalSync = settings.sftpToTerminalSync ?? true
      this.terminalToSftpSync = settings.terminalToSftpSync ?? true
      this.uiScale = clampUIScale(settings.uiScale)
      this.theme = {...defaultTheme(), ...(settings.theme ?? {})}
      this.appearance = {...defaultAppearance(), ...(settings.appearance ?? {})}
      this.fonts = {...defaultFonts(), ...(settings.fonts ?? {})}
      this.autoReconnect = settings.autoReconnect ?? true
      this.keepAliveEnabled = settings.keepAliveEnabled ?? true
      // 登录后自动采集：缺省 / 非法值一律回落「开启」（与 Go 端默认一致）
      this.sysInfoCollectEnabled = settings.sysInfoCollectEnabled ?? true
      this.localShell = settings.localShell ?? ''
      this.autoLoginLastServer = settings.autoLoginLastServer ?? false
      this.lastOpenServers = normalizeLastOpenServers(settings.lastOpenServers)
      // 缺省 / 非法值一律回落「左侧导航」（新默认）
      this.tabBarPlacement = settings.tabBarPlacement === 'top' ? 'top' : 'side'
      // 缺省 / 非法值一律回落「打开选项栏」
      this.rightClickAction = settings.rightClickAction === 'paste' ? 'paste' : 'menu'
      // 缺省 / 非法排列一律回落「导航 → 会话 → 标签页」
      this.navSectionOrder = normalizeNavSectionOrder(settings.navSectionOrder)
      // 语义着色：缺值 / 非法值一律回落「语义着色」
      this.terminalColorMode = normalizeTerminalColorMode(settings.terminalColorMode)
      // 调试模式：缺省字段用默认值补齐（端口越界回落默认）
      this.debug = normalizeDebug({...defaultDebug(), ...(settings.debug ?? {})})
      // 日志：级别非法回落 info，追踪列表过滤掉非字符串项
      this.logToFile = settings.logToFile ?? false
      this.logLevel = normalizeLogLevel(settings.logLevel)
      this.logApiCalls = settings.logApiCalls ?? false
      this.logTraceTabs = normalizeLogTraceTabs(settings.logTraceTabs)
      this.loaded = true
    },
    async setLogEnabled(v: boolean) {
      this.logEnabled = v
      await this.save()
    },
    async setCopyOnSelect(v: boolean) {
      this.copyOnSelect = v
      await this.save()
    },
    async setWebGLEnabled(v: boolean) {
      this.webGLEnabled = v
      await this.save()
    },
    async setCompletionEnabled(v: boolean) {
      this.completionEnabled = v
      await this.save()
    },
    async setCompletionNavHotkey(v: string) {
      this.completionNavHotkey = v || DEFAULT_COMPLETION_NAV_HOTKEY
      await this.save()
    },
    async setCompletionPanelLimit(v: number) {
      this.completionPanelLimit = clampPanelLimit(v)
      await this.save()
    },
    async setSftpToTerminalSync(v: boolean) {
      this.sftpToTerminalSync = v
      await this.save()
    },
    async setTerminalToSftpSync(v: boolean) {
      this.terminalToSftpSync = v
      await this.save()
    },
    async setUIScale(v: number) {
      this.uiScale = clampUIScale(v)
      await this.save()
    },
    async setTheme(theme: Theme) {
      this.theme = theme
      await this.save()
    },
    async setAppearance(appearance: UIAppearance) {
      this.appearance = appearance
      await this.save()
    },
    async setFonts(fonts: Fonts) {
      this.fonts = fonts
      await this.save()
    },
    // 外观页批量应用：UI 外观 / 终端主题 / 字体一次持久化
    async applyLook(appearance: UIAppearance, theme: Theme, fonts: Fonts) {
      this.appearance = appearance
      this.theme = theme
      this.fonts = fonts
      await this.save()
    },
    async setAutoReconnect(v: boolean) {
      this.autoReconnect = v
      await this.save()
    },
    async setKeepAliveEnabled(v: boolean) {
      this.keepAliveEnabled = v
      await this.save()
    },
    async setSysInfoCollectEnabled(v: boolean) {
      this.sysInfoCollectEnabled = v
      await this.save()
    },
    async setLocalShell(v: string) {
      this.localShell = v
      await this.save()
    },
    async setAutoLoginLastServer(v: boolean) {
      this.autoLoginLastServer = v
      await this.save()
    },
    // 工作区标签增删时更新「上次打开的服务器」列表（去空白项，保留顺序与重复项）
    async setLastOpenServers(ids: string[]) {
      this.lastOpenServers = normalizeLastOpenServers(ids)
      await this.save()
    },
    async setTabBarPlacement(v: TabBarPlacement) {
      this.tabBarPlacement = v === 'top' ? 'top' : 'side'
      await this.save()
    },
    async setRightClickAction(v: RightClickAction) {
      this.rightClickAction = v === 'paste' ? 'paste' : 'menu'
      await this.save()
    },
    async setNavSectionOrder(v: NavSectionOrder) {
      this.navSectionOrder = normalizeNavSectionOrder(v)
      await this.save()
    },
    // 终端输出语义着色档位：局部更新立即持久化（非法值回落 auto）
    async setTerminalColorMode(v: TerminalColorMode) {
      this.terminalColorMode = normalizeTerminalColorMode(v)
      await this.save()
    },
    // 调试模式：局部更新后立即持久化（设置页每次改一个开关就调一次）
    async setDebug(patch: Partial<DebugSettings>) {
      this.debug = normalizeDebug({...this.debug, ...patch})
      await this.save()
    },
    // 日志：局部更新后立即持久化（与 setDebug 同风格）
    async setLog(
      patch: Partial<{logToFile: boolean; logLevel: LogLevel; logApiCalls: boolean; logTraceTabs: string[]}>,
    ) {
      if (patch.logToFile !== undefined) this.logToFile = patch.logToFile
      if (patch.logLevel !== undefined) this.logLevel = normalizeLogLevel(patch.logLevel)
      if (patch.logApiCalls !== undefined) this.logApiCalls = patch.logApiCalls
      if (patch.logTraceTabs !== undefined) this.logTraceTabs = normalizeLogTraceTabs(patch.logTraceTabs)
      await this.save()
    },
    async save() {
      await settingsService.saveSettings({
        logEnabled: this.logEnabled,
        copyOnSelect: this.copyOnSelect,
        webGLEnabled: this.webGLEnabled,
        completionEnabled: this.completionEnabled,
        completionNavHotkey: this.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY,
        completionPanelLimit: clampPanelLimit(this.completionPanelLimit),
        sftpToTerminalSync: this.sftpToTerminalSync,
        terminalToSftpSync: this.terminalToSftpSync,
        uiScale: clampUIScale(this.uiScale),
        theme: this.theme,
        appearance: this.appearance,
        fonts: this.fonts,
        autoReconnect: this.autoReconnect,
        keepAliveEnabled: this.keepAliveEnabled,
        sysInfoCollectEnabled: this.sysInfoCollectEnabled,
        localShell: this.localShell,
        autoLoginLastServer: this.autoLoginLastServer,
        lastOpenServers: normalizeLastOpenServers(this.lastOpenServers),
        tabBarPlacement: this.tabBarPlacement,
        rightClickAction: this.rightClickAction,
        navSectionOrder: this.navSectionOrder,
        terminalColorMode: normalizeTerminalColorMode(this.terminalColorMode),
        debug: this.debug,
        logToFile: this.logToFile,
        logLevel: normalizeLogLevel(this.logLevel),
        logApiCalls: this.logApiCalls,
        logTraceTabs: normalizeLogTraceTabs(this.logTraceTabs),
      })
    },
  },
})

// 调试模式端口：0（自动）或 1024–65535，越界回落默认端口；白名单过滤空串/非字符串。
function normalizeDebug(d: DebugSettings): DebugSettings {
  const port = Number(d.port)
  if (!Number.isFinite(port) || (port !== 0 && (port < 1024 || port > 65535))) {
    d.port = 8765
  } else {
    d.port = Math.round(port)
  }
  d.sftpWriteAllowlist = normalizeAllowlist(d.sftpWriteAllowlist)
  // 不允许读取敏感数据时，凭据写入必然不成立（与 Go 端 NormalizeDebugSettings 一致）；
  // sudo 提权同样要读回保存的密码，因此一并回落 false。
  if (!d.allowSecrets) d.capSecretWrite = false
  if (!d.allowSecrets) d.capSudoCredential = false
  return d
}

// 远端可写路径白名单：只保留字符串项、去首尾空白与尾部斜杠、去重（空 = 禁止任何写入路径）。
function normalizeAllowlist(v: unknown): string[] {
  if (!Array.isArray(v)) return []
  const out: string[] = []
  for (const item of v) {
    if (typeof item !== 'string') continue
    const p = item.trim().replace(/\/+$/, '')
    if (!p || out.includes(p)) continue
    out.push(p)
  }
  return out
}

// 日志级别白名单：非法值一律回落 info。
const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const

function normalizeLogLevel(v: string | undefined): LogLevel {
  return (LOG_LEVELS as readonly string[]).includes(v ?? '') ? (v as LogLevel) : 'info'
}

// 上次打开的服务器列表：只保留非空字符串项，保持原始顺序与重复项
//（重复 = 同一台服务器同时开了多个标签，恢复时逐项重开才能还原标签数量）。
function normalizeLastOpenServers(v: unknown): string[] {
  if (!Array.isArray(v)) return []
  const out: string[] = []
  for (const item of v) {
    if (typeof item !== 'string') continue
    const id = item.trim()
    if (id) out.push(id)
  }
  return out
}

// 追踪列表：只保留字符串项（防止后端返回脏数据）。
function normalizeLogTraceTabs(v: unknown): string[] {
  if (!Array.isArray(v)) return []
  return v.filter((t): t is string => typeof t === 'string')
}

// 导航区段顺序白名单：必须是 nav / sessions / tabs 的排列，其余回落默认。
const NAV_SECTION_KEYS = ['nav', 'sessions', 'tabs'] as const

function normalizeNavSectionOrder(v: string | undefined): NavSectionOrder {
  const parts = (v ?? '').split(',').filter(Boolean)
  if (
    parts.length === 3 &&
    new Set(parts).size === 3 &&
    parts.every((p) => (NAV_SECTION_KEYS as readonly string[]).includes(p))
  ) {
    return parts.join(',') as NavSectionOrder
  }
  return 'nav,sessions,tabs'
}

// 语义着色档位白名单：非法值一律回落 auto（与 Go 端 NormalizeTerminalColorMode 一致）。
const TERMINAL_COLOR_MODES = ['off', 'auto', 'vivid'] as const

function normalizeTerminalColorMode(v: string | undefined): TerminalColorMode {
  return (TERMINAL_COLOR_MODES as readonly string[]).includes(v ?? '')
    ? (v as TerminalColorMode)
    : 'auto'
}

function clampPanelLimit(v: number | undefined): number {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0) return 8
  return Math.max(3, Math.min(30, Math.round(n)))
}

function clampUIScale(v: number | undefined): number {
  const n = Number(v)
  if (!Number.isFinite(n) || n <= 0) return 100
  return Math.max(80, Math.min(150, Math.round(n)))
}
